package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx         context.Context
	storage     *Storage
	mtrMu       sync.Mutex
	mtrRunner   *MTRRunner
	mtrCancel   context.CancelFunc
	stMu        sync.Mutex
	stRunner    *SpeedtestRunner
	stCancel    context.CancelFunc
	networkInfo NetworkInfo
	chatMu      sync.Mutex
	chatCancel  context.CancelFunc
	classifier  *Classifier
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	var err error
	a.storage, err = NewStorage()
	if err != nil {
		runtime.LogError(ctx, "storage init failed: "+err.Error())
	}
	go func() {
		info := FetchNetworkInfo()
		a.stMu.Lock()
		a.networkInfo = info
		a.stMu.Unlock()
	}()

	if c, err := NewClassifier(); err == nil {
		a.classifier = c
	} else {
		runtime.LogWarning(ctx, "DeBERTa classifier unavailable: "+err.Error())
	}
}

func (a *App) domReady(ctx context.Context) {
	a.initSnapLayout()
}

func (a *App) shutdown(ctx context.Context) {
	a.StopDiagnostics()
	a.StopSpeedtest()
	a.StopChatStream()
	if a.classifier != nil {
		a.classifier.Close()
	}
	if a.storage != nil {
		a.storage.Close()
	}
}

// StartDiagnostics starts an MTR run to the given host.
func (a *App) StartDiagnostics(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("host cannot be empty")
	}

	a.StopDiagnostics()

	if a.storage != nil {
		if err := a.storage.SaveHost(host); err != nil {
			runtime.LogWarning(a.ctx, "SaveHost failed: "+err.Error())
		}
	}

	ctx, cancel := context.WithCancel(a.ctx)
	startedAt := time.Now()

	// Declare runner before the closure so the closure can capture the variable.
	var runner *MTRRunner

	emitFn := func(update DiagnosticsUpdate) {
		runtime.EventsEmit(a.ctx, "diagnostics:update", update)

		if update.Done && a.storage != nil && runner != nil {
			snapshot := runner.Snapshot()
			hops := make([]HopResult, len(snapshot))
			for i, s := range snapshot {
				var avg int64 = -1
				if s.Recv > 0 {
					avg = s.Sum / int64(s.Recv)
				}
				var loss float64
				if s.Sent > 0 {
					loss = float64(s.Sent-s.Recv) / float64(s.Sent) * 100
				}
				best := s.Best
				if s.Recv == 0 {
					best = -1
				}
				hops[i] = HopResult{
					Nr:    s.Nr,
					Host:  s.Host,
					Loss:  loss,
					Sent:  s.Sent,
					Recv:  s.Recv,
					Best:  best,
					Avg:   avg,
					Worst: s.Worst,
					Last:  s.Last,
				}
			}
			session := DiagSession{
				ID:        fmt.Sprintf("%d", startedAt.UnixNano()),
				Host:      host,
				StartedAt: startedAt,
				EndedAt:   time.Now(),
				Hops:      hops,
			}
			if err := a.storage.SaveSession(session); err != nil {
				runtime.LogWarning(a.ctx, "SaveSession failed: "+err.Error())
			}
		}
	}

	runner = NewMTRRunner(ctx, host, emitFn)

	a.mtrMu.Lock()
	a.mtrRunner = runner
	a.mtrCancel = cancel
	a.mtrMu.Unlock()

	go runner.Run()
	return nil
}

// StopDiagnostics stops the active MTR run if any.
func (a *App) StopDiagnostics() {
	a.mtrMu.Lock()
	defer a.mtrMu.Unlock()
	if a.mtrCancel != nil {
		a.mtrCancel()
		a.mtrCancel = nil
		a.mtrRunner = nil
	}
}

// GetDiagnosticsStatus returns true if a diagnostics run is currently active.
func (a *App) GetDiagnosticsStatus() bool {
	a.mtrMu.Lock()
	defer a.mtrMu.Unlock()
	return a.mtrRunner != nil
}

// GetHistory returns recently used hosts for the dropdown.
func (a *App) GetHistory() ([]string, error) {
	if a.storage == nil {
		return nil, nil
	}
	return a.storage.GetHosts()
}

// DeleteHost removes a host from the history.
func (a *App) DeleteHost(host string) error {
	if a.storage == nil {
		return nil
	}
	return a.storage.DeleteHost(host)
}

// GetSessions returns completed diagnostic sessions.
func (a *App) GetSessions() ([]DiagSession, error) {
	if a.storage == nil {
		return nil, nil
	}
	return a.storage.GetSessions()
}

// GetServers returns the list of available speed test servers.
func (a *App) GetServers() []SpeedServer {
	return GetAvailableServers()
}

// GetLibreSpeedServers fetches the public LibreSpeed server list and returns
// only servers that respond to a ping check.
func (a *App) GetLibreSpeedServers() ([]LibreSpeedServer, error) {
	return FetchAndFilterLibreSpeedServers()
}

// StartLibreSpeedTest starts a speed test against a LibreSpeed server.
func (a *App) StartLibreSpeedTest(name, country, baseURL, dlURL, ulURL string) error {
	a.stMu.Lock()
	if a.stRunner != nil {
		a.stMu.Unlock()
		return fmt.Errorf("speed test already running")
	}
	cfg := speedServerConfig{
		SpeedServer:  SpeedServer{ID: "ls-" + name, Name: name, Location: country, Flag: ""},
		isLibreSpeed: true,
		lsBaseURL:    baseURL,
		lsDlURL:      dlURL,
		lsUlURL:      ulURL,
	}
	ctx, cancel := context.WithCancel(a.ctx)
	var runner *SpeedtestRunner
	runner = NewSpeedtestRunnerWithConfig(ctx, cfg, func(update SpeedtestUpdate) {
		runtime.EventsEmit(a.ctx, "speedtest:update", update)
		if (update.Phase == PhaseDone || update.Phase == PhaseFailed) && a.storage != nil {
			sess := runner.Result
			if err := a.storage.SaveSpeedtestSession(sess); err != nil {
				runtime.LogWarning(a.ctx, "SaveSpeedtestSession failed: "+err.Error())
			}
		}
	})
	a.stRunner = runner
	a.stCancel = cancel
	a.stMu.Unlock()

	go func() {
		runner.Run()
		a.stMu.Lock()
		a.stRunner = nil
		a.stCancel = nil
		a.stMu.Unlock()
	}()
	return nil
}

// StartSpeedtest starts a speed test against the given serverID.
// Empty serverID falls back to "cloudflare-auto".
func (a *App) StartSpeedtest(serverID string) error {
	if serverID == "" {
		serverID = "cloudflare-auto"
	}
	a.stMu.Lock()
	if a.stRunner != nil {
		a.stMu.Unlock()
		return fmt.Errorf("speed test already running")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	var runner *SpeedtestRunner
	runner = NewSpeedtestRunner(ctx, serverID, func(update SpeedtestUpdate) {
		runtime.EventsEmit(a.ctx, "speedtest:update", update)
		if (update.Phase == PhaseDone || update.Phase == PhaseFailed) && a.storage != nil {
			sess := runner.Result
			if err := a.storage.SaveSpeedtestSession(sess); err != nil {
				runtime.LogWarning(a.ctx, "SaveSpeedtestSession failed: "+err.Error())
			}
		}
	})
	a.stRunner = runner
	a.stCancel = cancel
	a.stMu.Unlock()

	go func() {
		runner.Run()
		a.stMu.Lock()
		a.stRunner = nil
		a.stCancel = nil
		a.stMu.Unlock()
	}()
	return nil
}

// StopSpeedtest cancels an active speed test.
func (a *App) StopSpeedtest() {
	a.stMu.Lock()
	defer a.stMu.Unlock()
	if a.stCancel != nil {
		a.stCancel()
		a.stCancel = nil
		a.stRunner = nil
	}
}

// GetSpeedtestHistory returns saved speed test sessions.
func (a *App) GetSpeedtestHistory() ([]SpeedtestSession, error) {
	if a.storage == nil {
		return nil, nil
	}
	return a.storage.GetSpeedtestSessions()
}

// GetNetworkInfo returns cached network info fetched at startup.
func (a *App) GetNetworkInfo() NetworkInfo {
	a.stMu.Lock()
	defer a.stMu.Unlock()
	return a.networkInfo
}

// ExportToFile opens a native save dialog and writes an MTR-style text report.
func (a *App) ExportToFile() error {
	a.mtrMu.Lock()
	runner := a.mtrRunner
	a.mtrMu.Unlock()

	if runner == nil {
		return fmt.Errorf("no active diagnostic run")
	}

	snapshot := runner.Snapshot()
	if len(snapshot) == 0 {
		return fmt.Errorf("no data to export yet")
	}

	text := formatReport(snapshot)

	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: fmt.Sprintf("mtr-%s-%d.txt", runner.host, time.Now().Unix()),
		Filters: []runtime.FileFilter{
			{DisplayName: "Text Files", Pattern: "*.txt"},
		},
	})
	if err != nil || path == "" {
		return err
	}

	return os.WriteFile(path, []byte(text), 0644)
}

func formatReport(hops []HopStats) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-4s %-40s %7s %6s %6s %8s %8s %8s %8s\n",
		"Nr", "Hostname", "Loss%", "Sent", "Recv", "Best", "Avg", "Worst", "Last"))
	sb.WriteString(strings.Repeat("-", 100) + "\n")
	for _, h := range hops {
		var loss float64
		if h.Sent > 0 {
			loss = float64(h.Sent-h.Recv) / float64(h.Sent) * 100
		}
		sb.WriteString(fmt.Sprintf("%-4d %-40s %6.2f%% %6d %6d %8s %8s %8s %8s\n",
			h.Nr, h.Host, loss, h.Sent, h.Recv,
			fmtMsReport(h.Best, h.Recv),
			fmtAvgReport(h.Sum, h.Recv),
			fmtMsReport(h.Worst, h.Recv),
			fmtLastReport(h.Last),
		))
	}
	return sb.String()
}

func fmtMsReport(ms int64, recv int) string {
	if recv == 0 || ms < 0 || ms == 9223372036854775807 {
		return "-"
	}
	return fmt.Sprintf("%dms", ms)
}

func fmtAvgReport(sum int64, recv int) string {
	if recv == 0 {
		return "-"
	}
	return fmt.Sprintf("%dms", sum/int64(recv))
}

func fmtLastReport(last int64) string {
	if last < 0 {
		return "-"
	}
	return fmt.Sprintf("%dms", last)
}
