package main

import (
	"context"
	"fmt"
	"math"
	"net"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ── Shared types ─────────────────────────────────────────────────────────────

type HopStats struct {
	Nr    int
	Host  string
	Sent  int
	Recv  int
	Best  int64 // math.MaxInt64 until first reply
	Worst int64
	Sum   int64
	Last  int64 // -1 if last probe timed out
}

type HopUpdate struct {
	Nr    int     `json:"nr"`
	Host  string  `json:"host"`
	Loss  float64 `json:"loss"`
	Sent  int     `json:"sent"`
	Recv  int     `json:"recv"`
	Best  int64   `json:"best"`
	Avg   int64   `json:"avg"`
	Worst int64   `json:"worst"`
	Last  int64   `json:"last"`
}

type DiagnosticsUpdate struct {
	Hops  []HopUpdate `json:"hops"`
	Done  bool        `json:"done"`
	Error string      `json:"error,omitempty"`
}

// ── Windows Native IcmpSendEcho (iphlpapi.dll) ───────────────────────────────

var (
	modiphlpapi         = syscall.NewLazyDLL("iphlpapi.dll")
	procIcmpCreateFile  = modiphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle = modiphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho    = modiphlpapi.NewProc("IcmpSendEcho")
)

type ipOptionInformation struct {
	Ttl         uint8
	Tos         uint8
	Flags       uint8
	OptionsSize uint8
	OptionsData uintptr
}

type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	Options       ipOptionInformation
}

const (
	IP_SUCCESS             = 0
	IP_REQ_TIMED_OUT       = 11010
	IP_TTL_EXPIRED_TRANSIT = 11013
)

func icmpCreateFile() (syscall.Handle, error) {
	ret, _, err := procIcmpCreateFile.Call()
	if ret == 0 {
		return 0, err
	}
	return syscall.Handle(ret), nil
}

func icmpCloseHandle(h syscall.Handle) {
	procIcmpCloseHandle.Call(uintptr(h))
}

type echoResult struct {
	ttl    int
	addr   string
	status uint32
	rtt    int64
}

func pingWinMTR(h syscall.Handle, destIP uint32, ttl int, timeoutMs uint32) echoResult {
	reqData := []byte("NetMon")
	reqSize := uintptr(len(reqData))

	opts := ipOptionInformation{
		Ttl: uint8(ttl),
	}

	replySize := unsafe.Sizeof(icmpEchoReply{}) + uintptr(reqSize) + 8
	replyBuf := make([]byte, replySize)

	ret, _, _ := procIcmpSendEcho.Call(
		uintptr(h),
		uintptr(destIP),
		uintptr(unsafe.Pointer(&reqData[0])),
		reqSize,
		uintptr(unsafe.Pointer(&opts)),
		uintptr(unsafe.Pointer(&replyBuf[0])),
		uintptr(len(replyBuf)),
		uintptr(timeoutMs),
	)

	if ret == 0 {
		return echoResult{ttl: ttl, status: IP_REQ_TIMED_OUT, rtt: -1}
	}

	reply := (*icmpEchoReply)(unsafe.Pointer(&replyBuf[0]))

	var addr string
	if reply.Address != 0 {
		ipBytes := (*[4]byte)(unsafe.Pointer(&reply.Address))
		addr = net.IPv4(ipBytes[0], ipBytes[1], ipBytes[2], ipBytes[3]).String()
	}

	return echoResult{
		ttl:    ttl,
		addr:   addr,
		status: reply.Status,
		rtt:    int64(reply.RoundTripTime),
	}
}

// ── MTRRunner ─────────────────────────────────────────────────────────────────

type MTRRunner struct {
	ctx  context.Context
	host string
	mu   sync.RWMutex
	hops []HopStats
	emit func(DiagnosticsUpdate)
}

func NewMTRRunner(ctx context.Context, host string, emit func(DiagnosticsUpdate)) *MTRRunner {
	return &MTRRunner{ctx: ctx, host: host, emit: emit}
}

func (m *MTRRunner) Snapshot() []HopStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make([]HopStats, len(m.hops))
	copy(cp, m.hops)
	return cp
}

func (m *MTRRunner) Run() error {
	dst, err := net.ResolveIPAddr("ip4", m.host)
	if err != nil {
		m.emit(DiagnosticsUpdate{Done: true, Error: fmt.Sprintf("resolve %s: %v", m.host, err)})
		return err
	}

	// Network byte order cast
	ip4 := dst.IP.To4()
	if ip4 == nil {
		m.emit(DiagnosticsUpdate{Done: true, Error: fmt.Sprintf("no IPv4 address for %s", m.host)})
		return fmt.Errorf("no ipv4")
	}
	destIP := *(*uint32)(unsafe.Pointer(&ip4[0]))

	icmpHandle, err := icmpCreateFile()
	if err != nil {
		m.emit(DiagnosticsUpdate{Done: true, Error: fmt.Sprintf("IcmpCreateFile error: %v", err)})
		return err
	}
	defer icmpCloseHandle(icmpHandle)

	maxTTL := 30
	m.mu.Lock()
	m.hops = make([]HopStats, maxTTL)
	for i := 0; i < maxTTL; i++ {
		m.hops[i] = HopStats{Nr: i + 1, Host: "???", Best: math.MaxInt64, Last: -1}
	}
	m.mu.Unlock()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Initial emit
	m.mu.RLock()
	m.emit(buildUpdate(m.hops, false, ""))
	m.mu.RUnlock()

	var wg sync.WaitGroup
	var maxTTLLock sync.RWMutex

	for {
		select {
		case <-m.ctx.Done():
			// Don't leak goroutines, wait for syscalls
			go func() { wg.Wait() }() 
			m.mu.RLock()
			m.emit(buildUpdate(m.hops, true, ""))
			m.mu.RUnlock()
			return nil
		case <-ticker.C:
			maxTTLLock.RLock()
			curMax := maxTTL
			maxTTLLock.RUnlock()

			for ttl := 1; ttl <= curMax; ttl++ {
				// Record sent
				m.mu.Lock()
				if ttl <= len(m.hops) {
					m.hops[ttl-1].Sent++
				}
				m.mu.Unlock()

				wg.Add(1)
				go func(t int) {
					defer wg.Done()

					// WinMTR uses ~2000ms timeout traditionally, we will use 1500 to keep it snappy
					res := pingWinMTR(icmpHandle, destIP, t, 1500)

					select {
					case <-m.ctx.Done():
						return // abandoned
					default:
					}

					m.mu.Lock()
					if t <= len(m.hops) {
						hop := &m.hops[t-1]

						// IP_SUCCESS (0) => reached target
						// IP_TTL_EXPIRED_TRANSIT (11013) => intermediate hop
						if res.status == IP_SUCCESS || res.status == IP_TTL_EXPIRED_TRANSIT {
							if res.addr != "" && (hop.Host == "???" || hop.Host == "") {
								hop.Host = res.addr
							}
							hop.Recv++
							hop.Last = res.rtt
							hop.Sum += res.rtt
							if res.rtt < hop.Best {
								hop.Best = res.rtt
							}
							if res.rtt > hop.Worst {
								hop.Worst = res.rtt
							}
						} else {
							// Timed out or other error
							hop.Last = -1
						}
					}
					m.mu.Unlock()

					// If we hit target, truncate maxTTL and slice
					if res.status == IP_SUCCESS {
						maxTTLLock.Lock()
						if t < maxTTL {
							maxTTL = t
						}
						maxTTLLock.Unlock()

						m.mu.Lock()
						if t < len(m.hops) {
							m.hops = m.hops[:t]
						}
						m.mu.Unlock()
					}
				}(ttl)
				
				// Optional: tiny sleep to keep Windows Network Queue happy
				time.Sleep(5 * time.Millisecond)
			}

			m.mu.RLock()
			m.emit(buildUpdate(m.hops, false, ""))
			m.mu.RUnlock()
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func buildUpdate(hops []HopStats, done bool, errMsg string) DiagnosticsUpdate {
	updates := make([]HopUpdate, len(hops))
	for i, h := range hops {
		var loss float64
		if h.Sent > 0 {
			loss = float64(h.Sent-h.Recv) / float64(h.Sent) * 100
		}
		var avg, best, worst int64
		if h.Recv > 0 {
			avg = h.Sum / int64(h.Recv)
			best = h.Best
			worst = h.Worst
		} else {
			avg, best, worst = -1, -1, -1
		}
		updates[i] = HopUpdate{
			Nr:    h.Nr,
			Host:  h.Host,
			Loss:  loss,
			Sent:  h.Sent,
			Recv:  h.Recv,
			Best:  best,
			Avg:   avg,
			Worst: worst,
			Last:  h.Last,
		}
	}
	return DiagnosticsUpdate{Hops: updates, Done: done, Error: errMsg}
}
