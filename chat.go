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
)

const (
	ollamaBaseURL   = "http://localhost:11434"
	ollamaModel     = "deepseek-r1:7b"
	chatMaxHistory  = 10  // max messages sent to Ollama per request
	contextWindow5m = 5 * time.Minute
)

const systemPromptTemplate = `You are NetMonit Assistant, an expert network diagnostics AI embedded in a desktop app.

Current network context (averaged over last 5 minutes):
%s

Classification: %s

Rules:
- Answer concisely and technically. Reference specific hop numbers, latency values, and packet loss percentages when relevant.
- If the user asks about their network, use the context above.
- For general networking questions, answer from your knowledge.
- Format responses with markdown when it improves clarity (tables for hop data, bullet points for steps).`

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
	Available  bool   `json:"available"`
	ModelReady bool   `json:"model_ready"`
	Error      string `json:"error,omitempty"`
}

// PullProgress is emitted via "chat:pull_progress" during model download.
type PullProgress struct {
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
}

// ── Ollama API types (internal) ───────────────────────────────────────────────

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type ollamaChatResponse struct {
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
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

		fullResponse, err := a.streamOllama(ctx, sessionID, ollamaMsgs)
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

	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		status := OllamaStatus{Available: true, ModelReady: false}
		runtime.EventsEmit(a.ctx, "chat:ollama_status", status)
		return status
	}

	modelReady := false
	for _, m := range tags.Models {
		if strings.HasPrefix(m.Name, "deepseek-r1") {
			modelReady = true
			break
		}
	}

	status := OllamaStatus{Available: true, ModelReady: modelReady}
	runtime.EventsEmit(a.ctx, "chat:ollama_status", status)
	return status
}

// StartOllama launches `ollama serve` as a background process.
// Returns an error if the ollama executable cannot be found.
func (a *App) StartOllama() error {
	candidates := []string{
		os.Getenv("LOCALAPPDATA") + `\Programs\Ollama\ollama.exe`,
		`C:\Program Files\Ollama\ollama.exe`,
		"ollama", // rely on PATH
	}

	var exePath string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			exePath = p
			break
		}
	}
	if exePath == "" {
		exePath = "ollama" // last resort: PATH
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
	body, _ := json.Marshal(ollamaPullRequest{Name: ollamaModel, Stream: true})
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

// ── Private helpers ───────────────────────────────────────────────────────────

// streamOllama POSTs to Ollama's chat API, reads the NDJSON stream,
// emits each token via "chat:chunk", and returns the full response text.
func (a *App) streamOllama(ctx context.Context, sessionID string, messages []ollamaMessage) (string, error) {
	reqBody, err := json.Marshal(ollamaChatRequest{
		Model:    ollamaModel,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		ollamaBaseURL+"/api/chat", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Ollama unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama returned status %d", resp.StatusCode)
	}

	var sb strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return sb.String(), ctx.Err()
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
		if chunk.Done {
			break
		}
	}
	return sb.String(), scanner.Err()
}

// buildNetworkContext assembles a compact text summary of network metrics
// averaged over the last 5 minutes. Falls back to the single most recent
// entry if no data exists within that window, preventing false "all clear"
// conclusions when the user isn't actively testing.
func (a *App) buildNetworkContext() string {
	var sb strings.Builder
	cutoff := time.Now().Add(-contextWindow5m)

	// Network info
	sb.WriteString(fmt.Sprintf("Network: %s, %s, %s\n",
		a.networkInfo.Provider, a.networkInfo.IP, a.networkInfo.City))

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
		sb.WriteString(fmt.Sprintf(
			"Speedtest avg (%d tests): DL %.1f Mbps, UL %.1f Mbps, ping %d ms, jitter %.1f ms\n",
			len(recentST), sumDL/count, sumUL/count, sumPing/int64(count), sumJitter/count,
		))
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
			sb.WriteString(fmt.Sprintf(
				"Diagnostics avg (%d runs): %.1f%% loss, %d ms avg latency, %d hops\n",
				runCount, totalLoss/float64(hopCount), totalAvg/int64(hopCount), hopCount/runCount,
			))
		}
	}

	result := strings.TrimSpace(sb.String())
	if result == "" {
		return "No network data available yet. Run a speed test or diagnostics first."
	}
	return result
}
