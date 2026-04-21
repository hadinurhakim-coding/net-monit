package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows/registry"
)

const (
	ollamaBaseURL        = "http://localhost:11434"
	ollamaModelFallback  = "deepseek-r1:7b"
	ollamaRegistryKey    = `Software\NetMonit`
	ollamaRegistryValue  = "SelectedModel"
	chatMaxHistory       = 10
	contextWindow5m      = 5 * time.Minute
	agentMaxIter         = 5
	toolDiagTimeout      = 20 * time.Second
	toolSpeedtestTimeout = 90 * time.Second
)

// modelSupportsTools returns true only for models known to support Ollama's tool-calling API.
// Uses a whitelist because some large models (e.g. deepseek-r1) still return HTTP 400 with tools.
func modelSupportsTools(model string) bool {
	supported := []string{"llama3", "mistral", "qwen2.5"}
	for _, name := range supported {
		if strings.Contains(model, name) {
			return true
		}
	}
	return false
}

// resolveOllamaModel reads the model selected by the installer from the registry.
// Falls back to the 1.5b model if the key is absent (manual install / dev mode).
func resolveOllamaModel() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, ollamaRegistryKey, registry.QUERY_VALUE)
	if err != nil {
		return ollamaModelFallback
	}
	defer k.Close()
	val, _, err := k.GetStringValue(ollamaRegistryValue)
	if err != nil || val == "" {
		return ollamaModelFallback
	}
	return val
}

const systemPromptTemplate = `You are NetMonit Assistant, an AI network diagnostics agent embedded in a desktop app called NetMonit.

## Language
Always reply in the same language the user writes in. If the user writes in Indonesian (Bahasa Indonesia), reply in Indonesian. If in English, reply in English. Match their language naturally.

## Current Network Context (averaged over last 5 minutes)
%s

## Classification
%s

## Behavior Rules

### When the user asks about their network or internet:
- Use the network context above and call tools if you need fresh data.
- Reference specific hop numbers, latency values, and packet loss percentages.
- Format with markdown: tables for hop data, bullet points for steps, code blocks for commands.

### When the user makes small talk (greetings, asking how you are, etc.):
- Respond briefly and warmly, then gently steer back to network topics.
- Example: if user says "halo, apa kabar?" reply naturally but keep it short (1–2 sentences).

### When the user asks something completely unrelated to networking or this app:
- Politely decline and redirect. Example: "Saya hanya bisa membantu soal jaringan dan konektivitas internet. Ada yang ingin dicek?"
- Do NOT answer questions about cooking, politics, entertainment, coding unrelated to networking, etc.

### Tool use:
- Call run_diagnostics when user wants to check/test connectivity to a host.
- Call run_speedtest when user wants to measure internet speed.
- Call get_network_info when user asks about their ISP or IP.
- Do NOT call tools for general knowledge questions.`

// ── Wails-exposed types ───────────────────────────────────────────────────────

// ChatChunk is emitted via "chat:chunk" event for each streaming token.
type ChatChunk struct {
	SessionID string `json:"session_id"`
	Delta     string `json:"delta"`
	Done      bool   `json:"done"`
	Error     string `json:"error,omitempty"`
}

// OllamaStatus is returned by CheckOllamaStatus and emitted via "chat:ollama_status".
type OllamaStatus struct {
	Available     bool   `json:"available"`
	ModelReady    bool   `json:"model_ready"`
	ModelName     string `json:"model_name"`
	SupportsTools bool   `json:"supports_tools"`
	Error         string `json:"error,omitempty"`
}

// PullProgress is emitted via "chat:pull_progress" during model download.
type PullProgress struct {
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
}

// ── Ollama API types (internal) ───────────────────────────────────────────────

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
}

type ollamaChatResponse struct {
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

// ── Agent / tool-use types ────────────────────────────────────────────────────

type ollamaToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolCallFunc struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ollamaToolCall struct {
	Function ollamaToolCallFunc `json:"function"`
}

// AgentToolEvent is emitted via "chat:tool_call" to show agent progress in the UI.
type AgentToolEvent struct {
	SessionID string `json:"session_id"`
	ToolName  string `json:"tool_name"`
	Args      string `json:"args"`
	Result    string `json:"result,omitempty"`
	IsResult  bool   `json:"is_result"`
}

// agentTools is the set of capabilities the LLM can invoke autonomously.
var agentTools = []ollamaTool{
	{
		Type: "function",
		Function: ollamaToolFunction{
			Name:        "run_diagnostics",
			Description: "Run MTR network diagnostics to a target host. Returns hop-by-hop latency and packet loss. Use when the user asks to check, test, or diagnose connectivity to a specific host.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"host": map[string]any{
						"type":        "string",
						"description": "Target hostname or IP address (e.g. '8.8.8.8' or 'google.com')",
					},
				},
				"required": []string{"host"},
			},
		},
	},
	{
		Type: "function",
		Function: ollamaToolFunction{
			Name:        "run_speedtest",
			Description: "Run an internet speed test. Returns download/upload speed in Mbps, ping, and jitter. Use when the user asks to test or measure internet speed.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	},
	{
		Type: "function",
		Function: ollamaToolFunction{
			Name:        "get_network_info",
			Description: "Get the user's current network: ISP/provider name, public IP address, and geographic location.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	},
}

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

type ollamaPullRequest struct {
	Name   string `json:"name"`
	Stream bool   `json:"stream"`
}

type ollamaPullResponse struct {
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
}

// ── Bound methods ─────────────────────────────────────────────────────────────

// SendChatMessage is the main chat entry point called from the frontend.
// It assembles network context, runs DeBERTa classification, then streams
// a DeepSeek R1 response via Ollama back to the UI.
func (a *App) SendChatMessage(sessionID, userMessage string) error {
	a.chatMu.Lock()
	if a.chatCancel != nil {
		a.chatCancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.chatCancel = cancel
	a.chatMu.Unlock()

	// Load or create session
	session, err := a.storage.GetChatSession(sessionID)
	if err != nil || session == nil {
		now := time.Now().UTC()
		session = &ChatSession{
			ID:        sessionID,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	// Append user message
	session.Messages = append(session.Messages, ChatMessage{
		ID:        uuid.New().String(),
		Role:      RoleUser,
		Content:   userMessage,
		Timestamp: time.Now().UTC(),
	})
	session.UpdatedAt = time.Now().UTC()
	_ = a.storage.SaveChatSession(*session)

	// Build context + classification
	netCtx := a.buildNetworkContext()
	classification := ClassificationResult{Summary: "No classification available"}
	if a.classifier != nil {
		classification = a.classifier.Classify(netCtx)
	}

	// Build Ollama messages array
	systemPrompt := fmt.Sprintf(systemPromptTemplate, netCtx, classification.Summary)
	ollamaMsgs := []ollamaMessage{{Role: "system", Content: systemPrompt}}

	// Include last N messages from history
	history := session.Messages
	if len(history) > chatMaxHistory {
		history = history[len(history)-chatMaxHistory:]
	}
	for _, m := range history {
		role := string(m.Role)
		if role == string(RoleSystem) {
			continue
		}
		ollamaMsgs = append(ollamaMsgs, ollamaMessage{Role: role, Content: m.Content})
	}

	go func() {
		defer func() {
			a.chatMu.Lock()
			a.chatCancel = nil
			a.chatMu.Unlock()
		}()

		fullResponse, err := a.runAgentLoop(ctx, sessionID, ollamaMsgs)
		if err != nil {
			if ctx.Err() != nil {
				runtime.EventsEmit(a.ctx, "chat:chunk", ChatChunk{
					SessionID: sessionID, Done: true, Error: "cancelled",
				})
				return
			}
			runtime.EventsEmit(a.ctx, "chat:chunk", ChatChunk{
				SessionID: sessionID, Done: true, Error: err.Error(),
			})
			return
		}

		// Persist assistant response
		session.Messages = append(session.Messages, ChatMessage{
			ID:        uuid.New().String(),
			Role:      RoleAssistant,
			Content:   fullResponse,
			Timestamp: time.Now().UTC(),
		})
		session.UpdatedAt = time.Now().UTC()
		_ = a.storage.SaveChatSession(*session)

		runtime.EventsEmit(a.ctx, "chat:chunk", ChatChunk{
			SessionID: sessionID, Done: true,
		})
	}()

	return nil
}

// GetChatSessions returns all persisted chat sessions, sorted newest-first.
func (a *App) GetChatSessions() ([]ChatSession, error) {
	return a.storage.GetChatSessions()
}

// GetChatSession returns a single session with all messages.
func (a *App) GetChatSession(sessionID string) (*ChatSession, error) {
	return a.storage.GetChatSession(sessionID)
}

// DeleteChatSession removes a session from storage.
func (a *App) DeleteChatSession(sessionID string) error {
	return a.storage.DeleteChatSession(sessionID)
}

// StopChatStream cancels any in-flight streaming request.
func (a *App) StopChatStream() {
	a.chatMu.Lock()
	defer a.chatMu.Unlock()
	if a.chatCancel != nil {
		a.chatCancel()
		a.chatCancel = nil
	}
}

// CheckOllamaStatus probes Ollama and checks whether the model is available.
func (a *App) CheckOllamaStatus() OllamaStatus {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(ollamaBaseURL + "/api/tags")
	if err != nil {
		status := OllamaStatus{Available: false, Error: err.Error()}
		runtime.EventsEmit(a.ctx, "chat:ollama_status", status)
		return status
	}
	defer resp.Body.Close()

	model := resolveOllamaModel()

	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		status := OllamaStatus{Available: true, ModelReady: false, ModelName: model, SupportsTools: modelSupportsTools(model)}
		runtime.EventsEmit(a.ctx, "chat:ollama_status", status)
		return status
	}

	modelReady := false
	for _, m := range tags.Models {
		if strings.HasPrefix(m.Name, strings.Split(model, ":")[0]) {
			modelReady = true
			break
		}
	}

	status := OllamaStatus{Available: true, ModelReady: modelReady, ModelName: model, SupportsTools: modelSupportsTools(model)}
	runtime.EventsEmit(a.ctx, "chat:ollama_status", status)
	return status
}

// StartOllama launches `ollama serve` as a background process.
// Returns an error if the ollama executable cannot be found.
func (a *App) StartOllama() error {
	candidates := []string{
		os.Getenv("LOCALAPPDATA") + `\Programs\Ollama\ollama.exe`,
		`C:\Program Files\Ollama\ollama.exe`,
	}

	var exePath string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			exePath = p
			break
		}
	}
	if exePath == "" {
		return fmt.Errorf("Ollama not found; install it from https://ollama.com")
	}

	cmd := exec.Command(exePath, "serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start Ollama: %w", err)
	}
	// Detach — we don't wait for it
	go func() { _ = cmd.Wait() }()
	return nil
}

// PullDeepSeekModel pulls the DeepSeek R1 7B model via Ollama and streams progress.
func (a *App) PullDeepSeekModel() error {
	body, _ := json.Marshal(ollamaPullRequest{Name: resolveOllamaModel(), Stream: true})
	resp, err := http.Post(ollamaBaseURL+"/api/pull", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to start pull: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var p ollamaPullResponse
		if json.Unmarshal(scanner.Bytes(), &p) == nil {
			runtime.EventsEmit(a.ctx, "chat:pull_progress", PullProgress{
				Status:    p.Status,
				Completed: p.Completed,
				Total:     p.Total,
			})
		}
	}
	return scanner.Err()
}

// ── Agent loop ────────────────────────────────────────────────────────────────

// runAgentLoop drives the tool-use loop.
// Each iteration streams one Ollama response; if the model requests tool calls
// those are executed and fed back before the next iteration.
// The final text answer is streamed via "chat:chunk" events.
// Does NOT emit the terminal done chunk — SendChatMessage does that.
func (a *App) runAgentLoop(ctx context.Context, sessionID string, messages []ollamaMessage) (string, error) {
	for i := range agentMaxIter {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		// First round includes tools so the model can decide to act.
		// After tool results are in the history, let it answer freely.
		// Small models (1.5b) don't support tool calling — pass nil to avoid HTTP 400.
		var tools []ollamaTool
		if i == 0 && modelSupportsTools(resolveOllamaModel()) {
			tools = agentTools
		}

		content, toolCalls, err := a.streamOllamaRound(ctx, sessionID, messages, tools)
		if err != nil {
			return content, err
		}

		if len(toolCalls) == 0 {
			// No tool calls — final answer was already streamed.
			return content, nil
		}

		// Append assistant's tool-call decision to conversation history.
		messages = append(messages, ollamaMessage{
			Role:      "assistant",
			Content:   content,
			ToolCalls: toolCalls,
		})

		for _, tc := range toolCalls {
			argsJSON, _ := json.Marshal(tc.Function.Arguments)
			argsStr := string(argsJSON)

			runtime.EventsEmit(a.ctx, "chat:tool_call", AgentToolEvent{
				SessionID: sessionID,
				ToolName:  tc.Function.Name,
				Args:      argsStr,
			})

			result := a.executeTool(ctx, tc.Function.Name, tc.Function.Arguments)

			runtime.EventsEmit(a.ctx, "chat:tool_call", AgentToolEvent{
				SessionID: sessionID,
				ToolName:  tc.Function.Name,
				Args:      argsStr,
				Result:    result,
				IsResult:  true,
			})

			messages = append(messages, ollamaMessage{Role: "tool", Content: result})
		}
	}
	return "", fmt.Errorf("agent exceeded %d tool-call iterations", agentMaxIter)
}

// streamOllamaRound streams one Ollama response, emitting "chat:chunk" for content tokens.
// It does NOT emit the terminal done event. Returns accumulated content and any tool calls.
func (a *App) streamOllamaRound(ctx context.Context, sessionID string, messages []ollamaMessage, tools []ollamaTool) (string, []ollamaToolCall, error) {
	reqBody, err := json.Marshal(ollamaChatRequest{
		Model:    resolveOllamaModel(),
		Messages: messages,
		Stream:   true,
		Tools:    tools,
	})
	if err != nil {
		return "", nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		ollamaBaseURL+"/api/chat", bytes.NewReader(reqBody))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("Ollama unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("Ollama returned status %d", resp.StatusCode)
	}

	var sb strings.Builder
	var toolCalls []ollamaToolCall
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return sb.String(), toolCalls, ctx.Err()
		}
		var chunk ollamaChatResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		if delta := chunk.Message.Content; delta != "" {
			sb.WriteString(delta)
			runtime.EventsEmit(a.ctx, "chat:chunk", ChatChunk{
				SessionID: sessionID,
				Delta:     delta,
			})
		}
		if len(chunk.Message.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.Message.ToolCalls...)
		}
		if chunk.Done {
			break
		}
	}
	return sb.String(), toolCalls, scanner.Err()
}

// ── Tool executors ────────────────────────────────────────────────────────────

func (a *App) executeTool(ctx context.Context, name string, args map[string]any) string {
	switch name {
	case "run_diagnostics":
		host, _ := args["host"].(string)
		if host == "" {
			return `{"error": "host argument is required"}`
		}
		return a.toolRunDiagnostics(ctx, host)
	case "run_speedtest":
		return a.toolRunSpeedtest(ctx)
	case "get_network_info":
		return a.toolGetNetworkInfo()
	default:
		return fmt.Sprintf(`{"error": "unknown tool %q"}`, name)
	}
}

func (a *App) toolRunDiagnostics(parentCtx context.Context, host string) string {
	tctx, cancel := context.WithTimeout(parentCtx, toolDiagTimeout)
	defer cancel()

	done := make(chan struct{}, 1)
	runner := NewMTRRunner(tctx, host, func(DiagnosticsUpdate) {})
	go func() {
		runner.Run()
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-tctx.Done():
	}

	hops := runner.Snapshot()
	if len(hops) == 0 {
		return fmt.Sprintf("No hops found for %s — host may be unreachable", host)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "MTR diagnostics to %s (%d hops):\n", host, len(hops))
	for _, h := range hops {
		var loss float64
		if h.Sent > 0 {
			loss = float64(h.Sent-h.Recv) / float64(h.Sent) * 100
		}
		avg := int64(-1)
		if h.Recv > 0 {
			avg = h.Sum / int64(h.Recv)
		}
		fmt.Fprintf(&sb, "  Hop %2d  %-40s  loss=%.0f%%  avg=%dms  best=%dms  worst=%dms\n",
			h.Nr, h.Host, loss, avg, h.Best, h.Worst)
	}
	return sb.String()
}

func (a *App) toolRunSpeedtest(parentCtx context.Context) string {
	tctx, cancel := context.WithTimeout(parentCtx, toolSpeedtestTimeout)
	defer cancel()

	ch := make(chan struct{}, 1)
	runner := NewSpeedtestRunner(tctx, "cloudflare-auto", func(SpeedtestUpdate) {})
	go func() {
		runner.Run()
		ch <- struct{}{}
	}()

	select {
	case <-ch:
	case <-tctx.Done():
		return "Speed test timed out or was cancelled"
	}

	r := runner.Result
	if r.Failed {
		return fmt.Sprintf("Speed test failed: %s", r.FailReason)
	}
	return fmt.Sprintf(
		"Speed test: download=%.1f Mbps, upload=%.1f Mbps, ping=%d ms, jitter=%.1f ms, server=%s",
		r.Download, r.Upload, r.Ping, r.Jitter, r.Server,
	)
}

func (a *App) toolGetNetworkInfo() string {
	ni := a.GetNetworkInfo()
	return fmt.Sprintf("Network: ISP=%s, IP=%s, City=%s, Country=%s",
		ni.Provider, ni.IP, ni.City, ni.Country)
}

// buildNetworkContext assembles a compact text summary of network metrics
// averaged over the last 5 minutes. Falls back to the single most recent
// entry if no data exists within that window, preventing false "all clear"
// conclusions when the user isn't actively testing.
func (a *App) buildNetworkContext() string {
	var sb strings.Builder
	cutoff := time.Now().Add(-contextWindow5m)

	// Network info
	fmt.Fprintf(&sb, "Network: %s, %s, %s\n",
		a.networkInfo.Provider, a.networkInfo.IP, a.networkInfo.City)

	// Speedtest context
	allST, _ := a.storage.GetSpeedtestSessions()
	var recentST []SpeedtestSession
	for _, s := range allST {
		if s.StartedAt.After(cutoff) {
			recentST = append(recentST, s)
		}
	}
	if len(recentST) == 0 && len(allST) > 0 {
		recentST = allST[:1] // fallback: use most recent
	}
	if len(recentST) > 0 {
		var sumDL, sumUL, sumJitter float64
		var sumPing int64
		count := float64(len(recentST))
		for _, s := range recentST {
			sumDL += s.Download
			sumUL += s.Upload
			sumPing += s.Ping
			sumJitter += s.Jitter
		}
		fmt.Fprintf(&sb,
			"Speedtest avg (%d tests): DL %.1f Mbps, UL %.1f Mbps, ping %d ms, jitter %.1f ms\n",
			len(recentST), sumDL/count, sumUL/count, sumPing/int64(count), sumJitter/count)
	}

	// Diagnostics context
	allDiag, _ := a.storage.GetSessions()
	var recentDiag []DiagSession
	for _, d := range allDiag {
		if d.StartedAt.After(cutoff) {
			recentDiag = append(recentDiag, d)
		}
	}
	if len(recentDiag) == 0 && len(allDiag) > 0 {
		recentDiag = allDiag[:1] // fallback: use most recent
	}
	if len(recentDiag) > 0 {
		var totalLoss float64
		var totalAvg int64
		var hopCount int
		runCount := 0
		for _, d := range recentDiag {
			runCount++
			for _, h := range d.Hops {
				totalLoss += h.Loss
				if h.Avg > 0 {
					totalAvg += h.Avg
				}
				hopCount++
			}
		}
		if hopCount > 0 {
			fmt.Fprintf(&sb,
				"Diagnostics avg (%d runs): %.1f%% loss, %d ms avg latency, %d hops\n",
				runCount, totalLoss/float64(hopCount), totalAvg/int64(hopCount), hopCount/runCount)
		}
	}

	result := strings.TrimSpace(sb.String())
	if result == "" {
		return "No network data available yet. Run a speed test or diagnostics first."
	}
	return result
}
