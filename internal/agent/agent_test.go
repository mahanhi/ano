package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ano/ano/internal/llm"
	"github.com/ano/ano/internal/store"
)

func TestPassiveReviewDoesNotLoadEnvoy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen2.5:0.5b"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"message":{"content":"{\"action\":\"drop\",\"reason\":\"Usage reports can stop.\"}"}}`))
	}))
	defer srv.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.LoadExample(); err != nil {
		t.Fatal(err)
	}
	reloaded := 0
	ag := &Agent{
		Store: st,
		Model: llm.Client{Settings: llm.Settings{Kind: "ollama", URL: srv.URL, Model: "qwen2.5:0.5b"}, HTTP: srv.Client()},
		OnEnforce: func() error {
			reloaded++
			return nil
		},
	}
	n, err := ag.Review(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("reviewed %d", n)
	}
	routes, err := st.EnvoyRoutes()
	if err != nil || len(routes) != 0 || reloaded != 0 {
		t.Fatalf("passive routes %d reloads %d err %v", len(routes), reloaded, err)
	}
	if err := ag.SetMode("active"); err != nil {
		t.Fatal(err)
	}
	routes, err = st.EnvoyRoutes()
	if err != nil || len(routes) != 4 || reloaded != 1 {
		t.Fatalf("active routes %d reloads %d err %v", len(routes), reloaded, err)
	}
}

func TestDueRunsOnceAfterTheConfiguredTime(t *testing.T) {
	loc := time.Local
	day := time.Date(2026, 11, 6, 0, 0, 0, 0, loc)
	before := day.Add(2*time.Hour + 59*time.Minute)
	at := day.Add(3 * time.Hour)
	later := day.Add(10 * time.Hour)
	yesterday := at.Add(-24 * time.Hour)
	dayCadence := Cadence{Every: 1, Unit: "day", Hour: 3}
	if Due(before, yesterday, dayCadence) {
		t.Fatal("ran before the configured time")
	}
	if !Due(at, yesterday, dayCadence) || !Due(later, time.Time{}, dayCadence) {
		t.Fatal("missed the day's review")
	}
	if Due(later, at, dayCadence) {
		t.Fatal("ran twice in one day")
	}
	if Due(later, yesterday, Cadence{Every: 1, Unit: "day", Hour: 25}) {
		t.Fatal("accepted an invalid hour")
	}
	minute := time.Date(2026, 11, 6, 10, 5, 0, 0, loc)
	if !Due(minute, minute.Add(-5*time.Minute), Cadence{Every: 5, Unit: "minute"}) || Due(minute, minute, Cadence{Every: 5, Unit: "minute"}) {
		t.Fatal("minute schedule")
	}
	hour := time.Date(2026, 11, 6, 14, 15, 0, 0, loc)
	if !Due(hour, hour.Add(-2*time.Hour), Cadence{Every: 2, Unit: "hour", Minute: 15}) || Due(hour.Add(time.Minute), hour, Cadence{Every: 2, Unit: "hour", Minute: 15}) {
		t.Fatal("hour schedule")
	}
	monday := time.Date(2026, 11, 2, 3, 0, 0, 0, loc)
	if monday.Weekday() != time.Monday || !Due(monday, time.Time{}, Cadence{Every: 1, Unit: "week", Hour: 3, Weekday: int(time.Monday)}) {
		t.Fatal("week schedule")
	}
	first := time.Date(2026, 11, 1, 3, 0, 0, 0, loc)
	if !Due(first, time.Time{}, Cadence{Every: 1, Unit: "month", Hour: 3, MonthDay: 1}) || Due(first, first, Cadence{Every: 1, Unit: "month", Hour: 3, MonthDay: 1}) {
		t.Fatal("month schedule")
	}
}
