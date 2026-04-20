package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	ort "github.com/yalue/onnxruntime_go"
)

// ClassificationResult holds the structured output of the DeBERTa classifier.
// When the ONNX model is unavailable, the rule-based fallback populates these fields.
type ClassificationResult struct {
	LatencyClass string  `json:"latency_class"` // "good" | "moderate" | "high" | "critical"
	LossSeverity string  `json:"loss_severity"` // "none" | "minor" | "severe"
	JitterLevel  string  `json:"jitter_level"`  // "stable" | "variable" | "unstable"
	Confidence   float32 `json:"confidence"`
	Summary      string  `json:"summary"`
}

const ortSeqLen = 128

// Classifier wraps ONNX Runtime for local DeBERTa inference.
// Falls back to rule-based classification if the model file is absent.
type Classifier struct {
	session   *ort.AdvancedSession
	inputIDs  *ort.Tensor[int64]
	attMask   *ort.Tensor[int64]
	logits    *ort.Tensor[float32]
	ready     bool
}

// debertaModelPath returns the expected path for the ONNX model file.
// The installer places it at %APPDATA%\net-monit\models\netmonit-classifier.onnx
func debertaModelPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "net-monit", "models", "netmonit-classifier.onnx"), nil
}

// debertaLibPath returns the expected path for onnxruntime.dll,
// placed alongside the executable by the NSIS installer.
func debertaLibPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "onnxruntime.dll"), nil
}

// NewClassifier initialises the ONNX Runtime session.
// Returns a valid Classifier even on failure — Classify() will use the rule-based fallback.
func NewClassifier() (*Classifier, error) {
	c := &Classifier{}

	modelPath, err := debertaModelPath()
	if err != nil {
		return c, fmt.Errorf("cannot resolve model path: %w", err)
	}

	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return c, fmt.Errorf("ONNX model not found at %s", modelPath)
	}

	libPath, err := debertaLibPath()
	if err != nil {
		return c, fmt.Errorf("cannot resolve onnxruntime.dll path: %w", err)
	}
	if _, err := os.Stat(libPath); os.IsNotExist(err) {
		return c, fmt.Errorf("onnxruntime.dll not found at %s", libPath)
	}

	ort.SetSharedLibraryPath(libPath)
	if err := ort.InitializeEnvironment(); err != nil {
		return c, fmt.Errorf("ONNX Runtime init failed: %w", err)
	}

	inputShape := ort.NewShape(1, ortSeqLen)
	outputShape := ort.NewShape(1, 9)

	inputIDs, err := ort.NewEmptyTensor[int64](inputShape)
	if err != nil {
		return c, fmt.Errorf("failed to create input_ids tensor: %w", err)
	}
	attMask, err := ort.NewEmptyTensor[int64](inputShape)
	if err != nil {
		inputIDs.Destroy()
		return c, fmt.Errorf("failed to create attention_mask tensor: %w", err)
	}
	logits, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		inputIDs.Destroy()
		attMask.Destroy()
		return c, fmt.Errorf("failed to create output tensor: %w", err)
	}

	session, err := ort.NewAdvancedSession(
		modelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"logits"},
		[]ort.Value{inputIDs, attMask},
		[]ort.Value{logits},
		nil,
	)
	if err != nil {
		inputIDs.Destroy()
		attMask.Destroy()
		logits.Destroy()
		return c, fmt.Errorf("ONNX session creation failed: %w", err)
	}

	c.session = session
	c.inputIDs = inputIDs
	c.attMask = attMask
	c.logits = logits
	c.ready = true
	return c, nil
}

// Classify classifies the network context string.
// Uses ONNX inference when ready, otherwise falls back to rule-based heuristics.
func (c *Classifier) Classify(networkContext string) ClassificationResult {
	if c.ready {
		if result, err := c.classifyONNX(networkContext); err == nil {
			return result
		}
	}
	return classifyRuleBased(networkContext)
}

// classifyONNX runs inference through the loaded ONNX model.
func (c *Classifier) classifyONNX(text string) (ClassificationResult, error) {
	tokens := simpleTokenize(text, ortSeqLen)

	idData := c.inputIDs.GetData()
	maskData := c.attMask.GetData()
	for i := range idData {
		idData[i] = 0
		maskData[i] = 0
	}
	for i, t := range tokens {
		idData[i] = t
		if t != 0 {
			maskData[i] = 1
		}
	}

	if err := c.session.Run(); err != nil {
		return ClassificationResult{}, err
	}

	return decodeLogits(c.logits.GetData()), nil
}

// decodeLogits converts raw logits [9] into ClassificationResult.
// Layout: [0..2] latency, [3..5] loss, [6..8] jitter
func decodeLogits(logits []float32) ClassificationResult {
	latencyLabels := []string{"good", "moderate", "critical"}
	lossLabels := []string{"none", "minor", "severe"}
	jitterLabels := []string{"stable", "variable", "unstable"}

	latencyIdx := argmax(logits[0:3])
	lossIdx := argmax(logits[3:6])
	jitterIdx := argmax(logits[6:9])

	result := ClassificationResult{
		LatencyClass: latencyLabels[latencyIdx],
		LossSeverity: lossLabels[lossIdx],
		JitterLevel:  jitterLabels[jitterIdx],
		Confidence:   softmaxMax(logits[0:3]),
	}
	result.Summary = fmt.Sprintf("Latency: %s, Loss: %s, Jitter: %s (conf %.0f%%)",
		result.LatencyClass, result.LossSeverity, result.JitterLevel,
		float64(result.Confidence)*100)
	return result
}

// classifyRuleBased is the fallback when ONNX is unavailable.
// Parses numeric metrics directly from the network context string.
func classifyRuleBased(context string) ClassificationResult {
	avgLatency := extractFloat(context, `avg\s+(\d+(?:\.\d+)?)\s*ms`)
	worstLatency := extractFloat(context, `worst\s+(\d+(?:\.\d+)?)\s*ms`)
	lossPCT := extractFloat(context, `(\d+(?:\.\d+)?)\s*%\s*loss`)
	jitter := extractFloat(context, `jitter\s+(\d+(?:\.\d+)?)\s*ms`)

	latency := avgLatency
	if worstLatency > 0 {
		latency = (avgLatency + worstLatency) / 2
	}

	var latencyClass string
	switch {
	case latency < 50:
		latencyClass = "good"
	case latency < 150:
		latencyClass = "moderate"
	case latency < 300:
		latencyClass = "high"
	default:
		latencyClass = "critical"
	}

	var lossSeverity string
	switch {
	case lossPCT <= 0:
		lossSeverity = "none"
	case lossPCT < 2:
		lossSeverity = "minor"
	default:
		lossSeverity = "severe"
	}

	var jitterLevel string
	switch {
	case jitter <= 0:
		jitterLevel = "stable"
	case jitter < 20:
		jitterLevel = "stable"
	case jitter < 50:
		jitterLevel = "variable"
	default:
		jitterLevel = "unstable"
	}

	result := ClassificationResult{
		LatencyClass: latencyClass,
		LossSeverity: lossSeverity,
		JitterLevel:  jitterLevel,
		Confidence:   0.75,
	}
	result.Summary = fmt.Sprintf("Latency: %s, Loss: %s, Jitter: %s (rule-based)",
		result.LatencyClass, result.LossSeverity, result.JitterLevel)
	return result
}

// Close releases ONNX Runtime resources.
func (c *Classifier) Close() {
	if c.session != nil {
		c.session.Destroy()
		c.session = nil
	}
	if c.inputIDs != nil {
		c.inputIDs.Destroy()
		c.inputIDs = nil
	}
	if c.attMask != nil {
		c.attMask.Destroy()
		c.attMask = nil
	}
	if c.logits != nil {
		c.logits.Destroy()
		c.logits = nil
	}
	if c.ready {
		_ = ort.DestroyEnvironment()
		c.ready = false
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// simpleTokenize converts text to a fixed-length int64 token slice.
// Uses UTF-8 byte values offset by 100 — sufficient for network context strings
// (mostly ASCII digits, units, and keywords).
func simpleTokenize(text string, maxLen int) []int64 {
	tokens := make([]int64, maxLen)
	tokens[0] = 1 // [CLS]
	i := 1
	for _, b := range []byte(text) {
		if i >= maxLen-1 {
			break
		}
		tokens[i] = int64(b) + 100
		i++
	}
	if i < maxLen {
		tokens[i] = 2 // [SEP]
	}
	return tokens
}

func extractFloat(text, pattern string) float64 {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

func argmax(vals []float32) int {
	idx := 0
	for i := 1; i < len(vals); i++ {
		if vals[i] > vals[idx] {
			idx = i
		}
	}
	return idx
}

// softmaxMax returns the softmax probability of the highest-scoring class.
func softmaxMax(vals []float32) float32 {
	sum := float64(0)
	maxVal := vals[argmax(vals)]
	for _, v := range vals {
		sum += math.Exp(float64(v - maxVal))
	}
	return float32(1.0 / sum)
}
