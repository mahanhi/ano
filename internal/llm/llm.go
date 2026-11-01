// Package llm asks a local AI Inference model, through an OpenAI-compatible server
// such as Ollama, whether a collected device name should be dropped or allowed.
// The model never sits on the packet path.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Settings is the local model endpoint. The defaults match Ollama on this machine.
type Settings struct {
	Kind  string
	URL   string
	Model string
}

// FromEnv reads HBAR_QWEN_URL and HBAR_QWEN_MODEL. Empty values use Ollama and qwen2.5:0.5b.
func FromEnv() Settings {
	url := strings.TrimRight(os.Getenv("HBAR_QWEN_URL"), "/")
	if url == "" {
		url = "http://127.0.0.1:11434/v1"
	}
	model := strings.TrimSpace(os.Getenv("HBAR_QWEN_MODEL"))
	if model == "" {
		model = "qwen2.5:0.5b"
	}
	kind := strings.TrimSpace(os.Getenv("HBAR_QWEN_KIND"))
	if kind == "" {
		kind = "ollama"
	}
	return Settings{Kind: kind, URL: url, Model: model}
}

// Brief is the metadata the model is allowed to see.
type Brief struct {
	DeviceName  string
	Kind        string
	Room        string
	Destination string
	Category    string
	Evidence    string
	Lookups     int
	Bytes       int
	Cadence     string
	SendTime    string
	Similar     string
	Protocol    string
}

// Verdict is a drop or an allow, plus what AI Inference inferred from the past day.
type Verdict struct {
	Action   string
	Reason   string
	Cadence  string
	SendTime string
	Similar  string
	Protocol string
}

// Client calls the chat completions API.
type Client struct {
	Settings Settings
	HTTP     *http.Client
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 90 * time.Second}
}

// Ready checks that the configured model is listed by the server.
func (c Client) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Settings.URL+"/models", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("AI Inference is not reachable at %s", c.Settings.URL)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("AI Inference models returned %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), c.Settings.Model) {
		return fmt.Errorf("model %s is not loaded", c.Settings.Model)
	}
	return nil
}

// Decide asks AI Inference for drop or allow. Allow is returned as allow_direct.
func (c Client) Decide(ctx context.Context, brief Brief) (Verdict, error) {
	prompt := fmt.Sprintf(`Device: %s (%s, %s)
Destination: %s
Protocol: %s
Category: %s
Evidence: %s
Live lookups: %d
Live bytes: %d
Cadence: %s
Send time: %s
Similar traffic of this kind from this device: %s
Judge only this device.`, brief.DeviceName, brief.Kind, brief.Room, brief.Destination, brief.Protocol, brief.Category, brief.Evidence, brief.Lookups, brief.Bytes, brief.Cadence, brief.SendTime, brief.Similar)
	raw, err := c.complete(ctx, prompt)
	if err != nil {
		return Verdict{}, err
	}
	return parseVerdict(raw)
}

const systemPrompt = `You are Ano. Infer drop or allow for this one home device from its own past 24 hours of send times. You do not see packet contents. Another device's sends are not this device's.
Drop a name that, like other names of the same kind on this device, goes out on a steady automatic timer across many hours.
Allow a name that sends rarely, in a few clusters, and not on that same timer.
Return JSON with keys action, reason, cadence, sendTime, similar, protocol.
action is drop or allow.
reason names the device, the destination, and the inference.
cadence says how often this device sends this name and similar names.
sendTime says which hours this device's sends fall in.
similar compares this name with the other names of the same kind from this device.
protocol is the protocol this device used for these sends.`

func (c Client) complete(ctx context.Context, prompt string) (string, error) {
	if c.Settings.Kind == "ollama" {
		return c.completeOllama(ctx, prompt)
	}
	payload := map[string]any{
		"model":       c.Settings.Model,
		"temperature": 0,
		"max_tokens":  220,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": prompt},
		},
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Settings.URL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("AI Inference is not reachable at %s", c.Settings.URL)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI Inference HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("AI Inference returned invalid JSON")
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("AI Inference returned no choices")
	}
	text := contentText(parsed.Choices[0].Message.Content)
	if text == "" {
		return "", fmt.Errorf("AI Inference returned an empty completion")
	}
	return text, nil
}

func (c Client) completeOllama(ctx context.Context, prompt string) (string, error) {
	origin := strings.TrimSuffix(c.Settings.URL, "/v1")
	payload := map[string]any{
		"model":  c.Settings.Model,
		"stream": false,
		"format": "json",
		"options": map[string]any{
			"temperature": 0,
			"num_predict": 220,
		},
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": prompt},
		},
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("AI Inference is not reachable at %s", origin)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI Inference HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("AI Inference returned invalid JSON")
	}
	text := strings.TrimSpace(parsed.Message.Content)
	if text == "" {
		return "", fmt.Errorf("AI Inference returned an empty completion")
	}
	return text, nil
}

func contentText(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var b strings.Builder
		for _, part := range v {
			obj, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := obj["text"].(string); ok {
				b.WriteString(text)
			}
		}
		return strings.TrimSpace(b.String())
	default:
		return ""
	}
}

func parseVerdict(raw string) (Verdict, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return Verdict{}, fmt.Errorf("AI Inference did not return a JSON verdict")
	}
	var body struct {
		Action   string `json:"action"`
		Reason   string `json:"reason"`
		Cadence  string `json:"cadence"`
		SendTime string `json:"sendTime"`
		Similar  string `json:"similar"`
		Protocol string `json:"protocol"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &body); err != nil {
		return Verdict{}, fmt.Errorf("AI Inference verdict was not JSON")
	}
	action := strings.ToLower(strings.TrimSpace(body.Action))
	switch action {
	case "drop":
	case "allow", "allow_direct":
		action = "allow_direct"
	default:
		return Verdict{}, fmt.Errorf("AI Inference action %q is not drop or allow", body.Action)
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = "No reason given."
	}
	return Verdict{
		Action:   action,
		Reason:   reason,
		Cadence:  strings.TrimSpace(body.Cadence),
		SendTime: strings.TrimSpace(body.SendTime),
		Similar:  strings.TrimSpace(body.Similar),
		Protocol: strings.TrimSpace(body.Protocol),
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
