// Package agent is Ano. It collects device metadata and asks the local model
// for a drop or allow. Passive mode stores the verdict. Active mode enforces it.
package agent

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/hbar/hbar/internal/flow"
	"github.com/hbar/hbar/internal/llm"
	"github.com/hbar/hbar/internal/store"
)

// Status is what the dashboard reads about the agent and the model.
type Status struct {
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	Collecting bool   `json:"collecting"`
	Reviewed   int    `json:"reviewed"`
	Detail     string `json:"detail"`
	Model      Model  `json:"model"`
}

// Model describes the local Qwen endpoint.
type Model struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Ready  bool   `json:"ready"`
	Detail string `json:"detail"`
}

// Agent is the on-machine reviewer.
type Agent struct {
	Store     *store.Store
	Model     llm.Client
	OnEnforce func() error

	mu       sync.Mutex
	meter    flow.Meter
	reviewed int
	detail   string
	ready    bool
	modelErr string
}

func suggestionText(verdict llm.Verdict, item store.ReviewItem) string {
	cadence := strings.TrimSpace(verdict.Cadence)
	if cadence == "" {
		cadence = item.Cadence
	}
	sendTime := strings.TrimSpace(verdict.SendTime)
	if sendTime == "" {
		sendTime = item.SendTime
	}
	similar := strings.TrimSpace(verdict.Similar)
	if similar == "" {
		similar = item.Similar
	}
	protocol := strings.TrimSpace(verdict.Protocol)
	if protocol == "" {
		protocol = item.Protocol
	}
	return strings.Join([]string{
		clause(verdict.Reason),
		"Cadence: " + clause(cadence),
		"Send time: " + clause(sendTime),
		"Similar traffic: " + clause(similar),
		"Protocol: " + clause(protocol),
	}, ". ") + "."
}

func clause(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), ". ")
}

// Review asks Qwen about names whose counts have moved, then enforces only in active mode.
func (a *Agent) Review(ctx context.Context) (int, error) {
	return a.review(ctx, 40)
}

func (a *Agent) review(ctx context.Context, minNew int) (int, error) {
	if err := a.Model.Ready(ctx); err != nil {
		a.mu.Lock()
		a.ready = false
		a.modelErr = err.Error()
		a.mu.Unlock()
		return 0, err
	}
	a.mu.Lock()
	a.ready = true
	a.modelErr = ""
	a.mu.Unlock()

	items, err := a.Store.Pending(minNew)
	if err != nil {
		return 0, err
	}
	mode, err := a.Store.Mode()
	if err != nil {
		return 0, err
	}
	n := 0
	var last error
	for _, item := range items {
		verdict, err := a.Model.Decide(ctx, llm.Brief{
			DeviceName:  item.DeviceName,
			Kind:        item.Kind,
			Room:        item.Room,
			Destination: item.Destination,
			Category:    item.Category,
			Evidence:    item.Evidence,
			Lookups:     item.Lookups,
			Bytes:       item.Bytes,
			Cadence:     item.Cadence,
			SendTime:    item.SendTime,
			Similar:     item.Similar,
			Protocol:    item.Protocol,
		})
		if err != nil {
			last = err
			continue
		}
		if err := a.Store.RecordVerdict(item.ID, verdict.Action, suggestionText(verdict, item), mode == "active"); err != nil {
			last = err
			continue
		}
		n++
	}
	if n > 0 && mode == "active" && a.OnEnforce != nil {
		if err := a.OnEnforce(); err != nil {
			last = err
		}
	}
	a.mu.Lock()
	a.reviewed += n
	if last != nil {
		a.detail = last.Error()
	} else if n > 0 {
		a.detail = "AI Inference chose drop or allow per device from the past 24 hours, including protocol."
	}
	a.mu.Unlock()
	return n, last
}

// Snapshot is safe to put on the dashboard.
func (a *Agent) Snapshot() Status {
	mode, err := a.Store.Mode()
	if err != nil || mode == "" {
		mode = "passive"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	detail := a.modelErr
	if detail == "" {
		detail = a.detail
	}
	if detail == "" && a.ready {
		detail = "AI Inference is ready. Passive mode stores its choice and does not change Envoy."
		if mode == "active" {
			detail = "AI Inference is ready. Active mode enforces drop and allow."
		}
	}
	return Status{
		Name:       "Ano",
		Mode:       mode,
		Collecting: true,
		Reviewed:   a.reviewed,
		Detail:     detail,
		Model: Model{
			Kind:   a.Model.Settings.Kind,
			Name:   a.Model.Settings.Model,
			URL:    a.Model.Settings.URL,
			Ready:  a.ready,
			Detail: a.modelErr,
		},
	}
}

// SetMode changes passive or active and reloads enforcement when needed.
func (a *Agent) SetMode(mode string) error {
	if err := a.Store.SetMode(mode); err != nil {
		return err
	}
	if a.OnEnforce != nil {
		return a.OnEnforce()
	}
	return nil
}

// Loop collects on a short tick and reviews on a slower one.
func (a *Agent) Loop(ctx context.Context) {
	collect := time.NewTicker(2 * time.Second)
	review := time.NewTicker(20 * time.Second)
	defer collect.Stop()
	defer review.Stop()
	_ = a.Store.Collect()
	a.observe()
	_, _ = a.review(ctx, 1)
	for {
		select {
		case <-ctx.Done():
			return
		case <-collect.C:
			_ = a.Store.Collect()
			a.observe()
		case <-review.C:
			_, _ = a.Review(ctx)
		}
	}
}

// observe records bytes this computer is exchanging with devices Ano already knows.
func (a *Agent) observe() {
	flows, err := flow.Read()
	if err != nil {
		return
	}
	var notes []store.FlowNote
	for _, reading := range a.meter.Readings(flows) {
		if reading.Delta < 0 {
			continue
		}
		notes = append(notes, store.FlowNote{Addr: reading.Addr, Service: reading.Service, Protocol: reading.Protocol, Delta: reading.Delta})
	}
	_ = a.Store.NoteFlows(notes)
}
