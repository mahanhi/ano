// Package agent is Ano. It collects device metadata and asks the local model
// for a drop or allow. Passive mode stores the verdict. Active mode enforces it.
package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ano/ano/internal/flow"
	"github.com/ano/ano/internal/llm"
	"github.com/ano/ano/internal/store"
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

// Model describes the local model endpoint.
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
	running  bool
	nextTry  time.Time
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

// Review asks AI Inference about names whose counts have moved, then enforces only in active mode.
func (a *Agent) Review(ctx context.Context) (int, error) {
	items, err := a.Store.Pending(40)
	if err != nil {
		return 0, err
	}
	return a.judge(ctx, items, false)
}

// Daily sends the past day of observed traffic to AI Inference and stores drop or allow.
func (a *Agent) Daily(ctx context.Context) (int, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	items, err := a.Store.Observed(now.Add(-24*time.Hour), start)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		detail := "No new traffic to review. AI Inference already read today's sends."
		_ = a.Store.MarkReview(now, detail)
		a.mu.Lock()
		a.detail = detail
		a.mu.Unlock()
		return 0, nil
	}
	n, err := a.judge(ctx, items, true)
	var detail string
	if err != nil && n == 0 {
		detail = err.Error()
	} else if n == 0 {
		detail = "No new traffic to review. AI Inference already read today's sends."
	} else {
		detail = fmt.Sprintf("AI Inference read %d names from the past 24 hours and suggested drop or allow.", n)
	}
	if err == nil || n > 0 {
		_ = a.Store.MarkReview(now, detail)
	}
	a.mu.Lock()
	a.detail = detail
	a.mu.Unlock()
	return n, err
}

func (a *Agent) judge(ctx context.Context, items []store.ReviewItem, daily bool) (int, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return 0, fmt.Errorf("AI Inference is already reading today's traffic")
	}
	a.running = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()
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
	} else if n > 0 && !daily {
		a.detail = "AI Inference chose drop or allow per device from the past 24 hours, including protocol."
	}
	a.mu.Unlock()
	return n, last
}

// Cadence is one schedule for the traffic review.
type Cadence struct {
	Every    int
	Unit     string
	Hour     int
	Minute   int
	Weekday  int
	MonthDay int
}

// Due reports whether the review should run at now. last is the previous completed review.
func Due(now, last time.Time, c Cadence) bool {
	if c.Hour < 0 || c.Hour > 23 || c.Minute < 0 || c.Minute > 59 || c.Every < 1 {
		return false
	}
	switch c.Unit {
	case "minute":
		if c.Every > 59 || now.Minute()%c.Every != 0 {
			return false
		}
		return !sameClock(now, last, true, true)
	case "hour":
		if c.Every > 24 || now.Hour()%c.Every != 0 || now.Minute() != c.Minute {
			return false
		}
		return !sameClock(now, last, true, false)
	case "day":
		if c.Every > 31 || !clockReached(now, c.Hour, c.Minute) || civilDays(now)%c.Every != 0 {
			return false
		}
		return !sameDate(now, last)
	case "week":
		if c.Every > 52 || c.Weekday < 0 || c.Weekday > 6 || int(now.Weekday()) != c.Weekday || !clockReached(now, c.Hour, c.Minute) {
			return false
		}
		if sameDate(now, last) {
			return false
		}
		return (civilDays(now)/7)%c.Every == 0
	case "month":
		if c.Every > 12 || c.MonthDay < 1 || c.MonthDay > 28 || now.Day() != c.MonthDay || !clockReached(now, c.Hour, c.Minute) {
			return false
		}
		if lastInMonth(now, last) {
			return false
		}
		months := (now.Year()-2020)*12 + int(now.Month()) - 1
		return months%c.Every == 0
	default:
		return false
	}
}

func clockReached(now time.Time, hour, minute int) bool {
	slot := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	return !now.Before(slot)
}

func sameDate(now, last time.Time) bool {
	if last.IsZero() {
		return false
	}
	ly, lm, ld := last.In(now.Location()).Date()
	ny, nm, nd := now.Date()
	return ly == ny && lm == nm && ld == nd
}

func lastInMonth(now, last time.Time) bool {
	if last.IsZero() {
		return false
	}
	ly, lm, _ := last.In(now.Location()).Date()
	ny, nm, _ := now.Date()
	return ly == ny && lm == nm
}

func sameClock(now, last time.Time, hour, minute bool) bool {
	if last.IsZero() || !sameDate(now, last) {
		return false
	}
	local := last.In(now.Location())
	if hour && local.Hour() != now.Hour() {
		return false
	}
	return !minute || local.Minute() == now.Minute()
}

func civilDays(now time.Time) int {
	epoch := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return int(start.Sub(epoch).Hours() / 24)
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

// Loop collects on a short tick and asks AI Inference once each day.
func (a *Agent) Loop(ctx context.Context) {
	collect := time.NewTicker(2 * time.Second)
	day := time.NewTicker(time.Minute)
	defer collect.Stop()
	defer day.Stop()
	_ = a.Store.Collect()
	a.observe()
	a.maybeDaily(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-collect.C:
			_ = a.Store.Collect()
			a.observe()
		case <-day.C:
			a.maybeDaily(ctx)
		}
	}
}

func (a *Agent) maybeDaily(ctx context.Context) {
	schedule, err := a.Store.ReviewSchedule()
	if err != nil || !schedule.Enabled {
		return
	}
	a.mu.Lock()
	wait := time.Now().Before(a.nextTry)
	a.mu.Unlock()
	if wait {
		return
	}
	var last time.Time
	if schedule.Last != "" {
		last, err = time.Parse(time.RFC3339, schedule.Last)
		if err != nil {
			last = time.Time{}
		}
	}
	if !Due(time.Now(), last, Cadence{Every: schedule.Every, Unit: schedule.Unit, Hour: schedule.Hour, Minute: schedule.Minute, Weekday: schedule.Weekday, MonthDay: schedule.MonthDay}) {
		return
	}
	_, err = a.Daily(ctx)
	if err != nil {
		a.mu.Lock()
		a.nextTry = time.Now().Add(15 * time.Minute)
		a.mu.Unlock()
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
		notes = append(notes, store.FlowNote{Addr: reading.Addr, Service: reading.Service, Protocol: reading.Protocol, Port: reading.Port, Delta: reading.Delta})
	}
	_ = a.Store.NoteFlows(notes)
}
