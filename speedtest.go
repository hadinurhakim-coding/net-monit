package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// ── Constants ─────────────────────────────────────────────────────────────────

const (
	dlDuration    = 10 * time.Second
	ulDuration    = 10 * time.Second
	warmupDur     = 2 * time.Second  // excluded from final avg (TCP slow-start)
	parallelConns = 4                // parallel HTTP connections
	emitInterval  = 500 * time.Millisecond
	rollingWindow = 3 * time.Second  // window for live speed display
	pingProbes    = 15
	pingInterval  = 300 * time.Millisecond
	dlChunkBytes  = 25_000_000       // 25MB per download request loop
	ulChunkBytes  = 25_000_000       // 25MB per upload request loop
)

// ── Types ─────────────────────────────────────────────────────────────────────

type SpeedtestPhase string

const (
	PhasePing     SpeedtestPhase = "ping"
	PhaseDownload SpeedtestPhase = "download"
	PhaseUpload   SpeedtestPhase = "upload"
	PhaseDone     SpeedtestPhase = "done"
	PhaseFailed   SpeedtestPhase = "failed"
)

type SpeedtestUpdate struct {
	Phase    SpeedtestPhase `json:"phase"`
	Speed    float64        `json:"speed"`
	Ping     int64          `json:"ping"`
	Jitter   float64        `json:"jitter"`
	Loss     float64        `json:"loss"`
	Download float64        `json:"download"`
	Upload   float64        `json:"upload"`
	Error    string         `json:"error,omitempty"`
}

type NetworkInfo struct {
	Provider string  `json:"provider"`
	IP       string  `json:"ip"`
	City     string  `json:"city"`
	Country  string  `json:"country"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
}

// ── Server Registry ───────────────────────────────────────────────────────────

type SpeedServer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Flag     string `json:"flag"`
}

type speedServerConfig struct {
	SpeedServer
	downloadFmt  string // fmt.Sprintf format: %d = bytes requested
	uploadURL    string
	fileURL      string // for fixed-file downloads (Hetzner)
	isFileDL     bool
	isLibreSpeed bool
	lsBaseURL    string
	lsDlURL      string
	lsUlURL      string
}

var speedServers = []speedServerConfig{
	{SpeedServer: SpeedServer{ID: "cloudflare-auto", Name: "Cloudflare", Location: "Nearest (Auto)", Flag: "🌐"},
		downloadFmt: "https://speed.cloudflare.com/__down?bytes=%d",
		uploadURL:   "https://speed.cloudflare.com/__up"},
	{SpeedServer: SpeedServer{ID: "sg-cloudflare", Name: "Cloudflare", Location: "Singapore", Flag: "🇸🇬"},
		downloadFmt: "https://sg.speed.cloudflare.com/__down?bytes=%d",
		uploadURL:   "https://sg.speed.cloudflare.com/__up"},
	{SpeedServer: SpeedServer{ID: "jp-cloudflare", Name: "Cloudflare", Location: "Tokyo, JP", Flag: "🇯🇵"},
		downloadFmt: "https://tyo.speed.cloudflare.com/__down?bytes=%d",
		uploadURL:   "https://tyo.speed.cloudflare.com/__up"},
	{SpeedServer: SpeedServer{ID: "us-cloudflare", Name: "Cloudflare", Location: "New York, US", Flag: "🇺🇸"},
		downloadFmt: "https://jfk.speed.cloudflare.com/__down?bytes=%d",
		uploadURL:   "https://jfk.speed.cloudflare.com/__up"},
	{SpeedServer: SpeedServer{ID: "de-cloudflare", Name: "Cloudflare", Location: "Frankfurt, EU", Flag: "🇩🇪"},
		downloadFmt: "https://fra.speed.cloudflare.com/__down?bytes=%d",
		uploadURL:   "https://fra.speed.cloudflare.com/__up"},
	{SpeedServer: SpeedServer{ID: "au-cloudflare", Name: "Cloudflare", Location: "Sydney, AU", Flag: "🇦🇺"},
		downloadFmt: "https://syd.speed.cloudflare.com/__down?bytes=%d",
		uploadURL:   "https://syd.speed.cloudflare.com/__up"},
	{SpeedServer: SpeedServer{ID: "id-unair", Name: "UNAIR", Location: "Surabaya, ID", Flag: "🇮🇩"},
		isLibreSpeed: true,
		lsBaseURL:    "https://speedtest.unair.ac.id",
		lsDlURL:      "backend/garbage.php",
		lsUlURL:      "backend/empty.php"},
	{SpeedServer: SpeedServer{ID: "id-siber", Name: "Sibertech", Location: "Indonesia", Flag: "🇮🇩"},
		isLibreSpeed: true,
		lsBaseURL:    "https://speedtest.siber.net.id",
		lsDlURL:      "backend/garbage.php",
		lsUlURL:      "backend/empty.php"},
	{SpeedServer: SpeedServer{ID: "id-gayuh", Name: "GayuhNet", Location: "Indonesia", Flag: "🇮🇩"},
		isLibreSpeed: true,
		lsBaseURL:    "https://speedtest.gayuh.net.id",
		lsDlURL:      "backend/garbage.php",
		lsUlURL:      "backend/empty.php"},
	{SpeedServer: SpeedServer{ID: "hetzner-de", Name: "Hetzner", Location: "Nuremberg, EU", Flag: "🇩🇪"},
		isFileDL: true,
		fileURL:  "https://speed.hetzner.de/10GB.bin",
		uploadURL: "https://speed.cloudflare.com/__up"},
	{SpeedServer: SpeedServer{ID: "hetzner-sg", Name: "Hetzner", Location: "Singapore", Flag: "🇸🇬"},
		isFileDL: true,
		fileURL:  "https://hel1-speed.hetzner.com/10GB.bin",
		uploadURL: "https://speed.cloudflare.com/__up"},
}

func getServerConfig(id string) speedServerConfig {
	for _, s := range speedServers {
		if s.ID == id {
			return s
		}
	}
	return speedServers[0]
}

func GetAvailableServers() []SpeedServer {
	out := make([]SpeedServer, len(speedServers))
	for i, s := range speedServers {
		out[i] = s.SpeedServer
	}
	return out
}

type SpeedtestRunner struct {
	ctx    context.Context
	emit   func(SpeedtestUpdate)
	server speedServerConfig
	Result SpeedtestSession
}

func NewSpeedtestRunner(ctx context.Context, serverID string, emit func(SpeedtestUpdate)) *SpeedtestRunner {
	return &SpeedtestRunner{ctx: ctx, server: getServerConfig(serverID), emit: emit}
}

func NewSpeedtestRunnerWithConfig(ctx context.Context, cfg speedServerConfig, emit func(SpeedtestUpdate)) *SpeedtestRunner {
	return &SpeedtestRunner{ctx: ctx, server: cfg, emit: emit}
}

// ── Chunk tracking helpers ────────────────────────────────────────────────────

type chunkRecord struct {
	t time.Time
	n int64
}

// rollingMbps computes Mbps over the last `window` of chunk records.
func rollingMbps(chunks []chunkRecord, window time.Duration, now time.Time) float64 {
	cutoff := now.Add(-window)
	var bytes int64
	for i := len(chunks) - 1; i >= 0; i-- {
		if chunks[i].t.Before(cutoff) {
			break
		}
		bytes += chunks[i].n
	}
	return float64(bytes) * 8 / 1e6 / window.Seconds()
}

// finalMbps returns the average Mbps excluding the warmup period.
func finalMbps(chunks []chunkRecord, startTime time.Time, testDur time.Duration) float64 {
	warmupEnd := startTime.Add(warmupDur)
	var bytes int64
	for _, c := range chunks {
		if c.t.After(warmupEnd) {
			bytes += c.n
		}
	}
	effective := testDur - warmupDur
	if effective <= 0 || bytes == 0 {
		return 0
	}
	return float64(bytes) * 8 / 1e6 / effective.Seconds()
}

// ── Ping Phase ────────────────────────────────────────────────────────────────

func (r *SpeedtestRunner) runPing() (pingMs int64, jitter float64, loss float64, err error) {
	destIP, err := resolveIPv4("8.8.8.8")
	if err != nil {
		return 0, 0, 0, fmt.Errorf("resolve: %w", err)
	}
	h, err := icmpCreateFile()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("IcmpCreateFile: %w", err)
	}
	defer icmpCloseHandle(h)

	var rtts []int64
	sent := 0
	cancelled := false

	for i := 0; i < pingProbes; i++ {
		select {
		case <-r.ctx.Done():
			cancelled = true
		default:
		}
		if cancelled {
			break
		}

		sent++
		res := pingWinMTR(h, destIP, 64, 2000)
		if res.status == IP_SUCCESS && res.rtt >= 0 {
			rtts = append(rtts, res.rtt)
		}

		// Emit live progress so UI updates during ping phase
		if len(rtts) > 0 {
			r.emit(SpeedtestUpdate{Phase: PhasePing, Ping: rtts[len(rtts)-1]})
		}

		if i < pingProbes-1 {
			select {
			case <-r.ctx.Done():
				cancelled = true
			case <-time.After(pingInterval):
			}
		}
	}

	if cancelled && sent == 0 {
		return 0, 0, 100, context.Canceled
	}

	recv := len(rtts)
	loss = float64(sent-recv) / float64(sent) * 100
	if recv == 0 {
		return 0, 0, loss, nil
	}

	var sum int64
	for _, rtt := range rtts {
		sum += rtt
	}
	avg := sum / int64(recv)

	var variance float64
	for _, rtt := range rtts {
		d := float64(rtt - avg)
		variance += d * d
	}
	variance /= float64(recv)

	return avg, math.Sqrt(variance), loss, nil
}

// ── Download Phase ────────────────────────────────────────────────────────────

func (r *SpeedtestRunner) runDownload() (mbps float64, err error) {
	// Child context that auto-cancels after dlDuration
	dlCtx, cancel := context.WithTimeout(r.ctx, dlDuration)
	defer cancel()

	var mu sync.Mutex
	var chunks []chunkRecord
	startTime := time.Now()

	addBytes := func(n int) {
		rec := chunkRecord{t: time.Now(), n: int64(n)}
		mu.Lock()
		chunks = append(chunks, rec)
		mu.Unlock()
	}

	// 4 parallel download goroutines, each loops 25MB chunks until context expires
	var wg sync.WaitGroup
	for range parallelConns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{}
			buf := make([]byte, 32*1024)
			for {
				select {
				case <-dlCtx.Done():
					return
				default:
				}
				var url string
				switch {
				case r.server.isLibreSpeed:
					dlPath := strings.TrimLeft(r.server.lsDlURL, "/")
					url = r.server.lsBaseURL + "/" + dlPath + "?ckSize=25"
				case r.server.isFileDL:
					url = r.server.fileURL
				default:
					url = fmt.Sprintf(r.server.downloadFmt, dlChunkBytes)
				}
				req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
				if err != nil {
					return
				}
				resp, err := client.Do(req)
				if err != nil {
					return
				}
				for {
					n, readErr := resp.Body.Read(buf)
					if n > 0 {
						addBytes(n)
					}
					if readErr != nil {
						break
					}
				}
				resp.Body.Close()
			}
		}()
	}

	// Emit live rolling speed every 500ms
	emitDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(emitInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				cs := make([]chunkRecord, len(chunks))
				copy(cs, chunks)
				mu.Unlock()
				speed := rollingMbps(cs, rollingWindow, time.Now())
				r.emit(SpeedtestUpdate{Phase: PhaseDownload, Speed: speed})
			case <-emitDone:
				return
			}
		}
	}()

	wg.Wait()
	close(emitDone)

	mu.Lock()
	cs := make([]chunkRecord, len(chunks))
	copy(cs, chunks)
	mu.Unlock()

	return finalMbps(cs, startTime, dlDuration), nil
}

// ── Upload Phase ──────────────────────────────────────────────────────────────

// cyclicReader produces data indefinitely from a fixed buffer, cycling through it.
// It checks the context on every Read call.
type cyclicReader struct {
	buf []byte
	pos int
	ctx context.Context
}

func (c *cyclicReader) Read(p []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
	}
	n := copy(p, c.buf[c.pos:])
	c.pos = (c.pos + n) % len(c.buf)
	return n, nil
}

func (r *SpeedtestRunner) runUpload() (mbps float64, err error) {
	ulCtx, cancel := context.WithTimeout(r.ctx, ulDuration)
	defer cancel()

	// Pre-generate 1MB non-compressible pattern, reused by all goroutines
	const patternSize = 1 * 1024 * 1024
	pattern := make([]byte, patternSize)
	for i := range pattern {
		pattern[i] = byte(i*7+13) & 0xFF
	}

	var mu sync.Mutex
	var chunks []chunkRecord
	startTime := time.Now()

	var wg sync.WaitGroup
	for range parallelConns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{}

			for {
				select {
				case <-ulCtx.Done():
					return
				default:
				}

				// Use a pipe so we can track bytes AND cancel mid-upload
				pr, pw := io.Pipe()

				// Writer: cycles through pattern until context expires or ulChunkBytes written
				go func() {
					var written int64
					buf := make([]byte, 32*1024)
					pos := 0
					for written < ulChunkBytes {
						select {
						case <-ulCtx.Done():
							pw.CloseWithError(ulCtx.Err())
							return
						default:
						}
						n := copy(buf, pattern[pos:])
						pos = (pos + n) % len(pattern)
						wn, werr := pw.Write(buf[:n])
						if wn > 0 {
							written += int64(wn)
							rec := chunkRecord{t: time.Now(), n: int64(wn)}
							mu.Lock()
							chunks = append(chunks, rec)
							mu.Unlock()
						}
						if werr != nil {
							return
						}
					}
					pw.Close()
				}()

				var uploadURL string
				if r.server.isLibreSpeed {
					ulPath := strings.TrimLeft(r.server.lsUlURL, "/")
					uploadURL = r.server.lsBaseURL + "/" + ulPath
				} else {
					uploadURL = r.server.uploadURL
					if uploadURL == "" {
						uploadURL = "https://speed.cloudflare.com/__up"
					}
				}
				req, err := http.NewRequestWithContext(ulCtx, http.MethodPost,
					uploadURL, pr)
				if err != nil {
					pr.Close()
					return
				}
				req.ContentLength = -1 // chunked transfer encoding
				req.Header.Set("Content-Type", "application/octet-stream")

				resp, _ := client.Do(req)
				if resp != nil {
					resp.Body.Close()
				}
				pr.Close()
			}
		}()
	}

	// Emit live rolling speed
	emitDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(emitInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				cs := make([]chunkRecord, len(chunks))
				copy(cs, chunks)
				mu.Unlock()
				speed := rollingMbps(cs, rollingWindow, time.Now())
				r.emit(SpeedtestUpdate{Phase: PhaseUpload, Speed: speed})
			case <-emitDone:
				return
			}
		}
	}()

	wg.Wait()
	close(emitDone)

	mu.Lock()
	cs := make([]chunkRecord, len(chunks))
	copy(cs, chunks)
	mu.Unlock()

	return finalMbps(cs, startTime, ulDuration), nil
}

// ── Run Orchestrator ──────────────────────────────────────────────────────────

func (r *SpeedtestRunner) Run() {
	startedAt := time.Now()

	pingMs, jitter, loss, err := r.runPing()
	if err != nil && r.ctx.Err() != nil {
		r.emit(SpeedtestUpdate{Phase: PhaseFailed, Error: "cancelled"})
		return
	}
	r.emit(SpeedtestUpdate{Phase: PhasePing, Ping: pingMs, Jitter: jitter, Loss: loss})

	dl, err := r.runDownload()
	if err != nil && r.ctx.Err() != nil {
		r.emit(SpeedtestUpdate{Phase: PhaseFailed, Error: "cancelled"})
		return
	}
	if err != nil {
		r.emit(SpeedtestUpdate{Phase: PhaseFailed, Error: "download failed: " + err.Error()})
		r.Result = SpeedtestSession{
			ID: fmt.Sprintf("%d", startedAt.UnixNano()), StartedAt: startedAt,
			Download: -1, Upload: -1, Ping: pingMs, Jitter: jitter, Loss: loss,
			Failed: true, FailReason: err.Error(),
		}
		return
	}

	up, err := r.runUpload()
	if err != nil && r.ctx.Err() != nil {
		r.emit(SpeedtestUpdate{Phase: PhaseFailed, Error: "cancelled"})
		return
	}
	if err != nil {
		up = 0
	}

	r.Result = SpeedtestSession{
		ID:        fmt.Sprintf("%d", startedAt.UnixNano()),
		StartedAt: startedAt,
		Download:  dl,
		Upload:    up,
		Ping:      pingMs,
		Jitter:    jitter,
		Loss:      loss,
		Server:    r.server.Name + " — " + r.server.Location,
	}

	r.emit(SpeedtestUpdate{
		Phase:    PhaseDone,
		Download: dl,
		Upload:   up,
		Ping:     pingMs,
		Jitter:   jitter,
		Loss:     loss,
	})
}

// ── Network Info ──────────────────────────────────────────────────────────────

func FetchNetworkInfo() NetworkInfo {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://ipinfo.io/json")
	if err != nil {
		return NetworkInfo{Provider: "Unknown", IP: "Unknown", City: "Unknown", Country: "Unknown"}
	}
	defer resp.Body.Close()

	var data struct {
		IP      string `json:"ip"`
		Org     string `json:"org"`
		City    string `json:"city"`
		Country string `json:"country"`
		Loc     string `json:"loc"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return NetworkInfo{Provider: "Unknown", IP: "Unknown", City: "Unknown", Country: "Unknown"}
	}

	provider := data.Org
	if idx := strings.Index(provider, " "); idx > 0 {
		provider = provider[idx+1:]
	}
	if provider == "" {
		provider = "Unknown"
	}

	orElse := func(s, fallback string) string {
		if s == "" {
			return fallback
		}
		return s
	}

	var lat, lon float64
	if parts := strings.SplitN(data.Loc, ",", 2); len(parts) == 2 {
		lat, _ = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		lon, _ = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	}

	return NetworkInfo{
		Provider: provider,
		IP:       orElse(data.IP, "Unknown"),
		City:     orElse(data.City, "Unknown"),
		Country:  orElse(data.Country, "Unknown"),
		Lat:      lat,
		Lon:      lon,
	}
}

// ── Helper ────────────────────────────────────────────────────────────────────

func resolveIPv4(host string) (uint32, error) {
	addrs, err := net.LookupHost(host)
	if err != nil || len(addrs) == 0 {
		return 0, fmt.Errorf("cannot resolve %s", host)
	}
	ip := net.ParseIP(addrs[0]).To4()
	if ip == nil {
		return 0, fmt.Errorf("no IPv4 for %s", host)
	}
	return *(*uint32)(unsafe.Pointer(&ip[0])), nil
}
