package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ano/ano/internal/agent"
	"github.com/ano/ano/internal/store"
)

func TestParseKeepsKnownActions(t *testing.T) {
	cmd, err := parse(`sure {"action":"mode","mode":"active","say":"Active is on."}`)
	if err != nil || cmd.Action != "mode" || cmd.Mode != "active" || cmd.Say != "Active is on." {
		t.Fatalf("%+v %v", cmd, err)
	}
	cmd, err = parse(`{"action":"run","say":"I can change the mode or the schedule."}`)
	if err != nil || cmd.Action != "none" || !strings.Contains(cmd.Say, "schedule") {
		t.Fatalf("talk %+v %v", cmd, err)
	}
	cmd, err = parse(`{"action":"switch to active"}`)
	if err != nil || cmd.Action != "mode" || cmd.Mode != "active" {
		t.Fatalf("phrase %+v %v", cmd, err)
	}
	cmd, err = parse(`{"action":"update the schedule to every hour"}`)
	cmd = withUserCadence(cmd, "update the schedule to every hour")
	if err != nil || cmd.Action != "schedule" || cmd.Unit != "hour" || cmd.Every == nil || *cmd.Every != 1 {
		t.Fatalf("hourly %+v %v", cmd, err)
	}
	cmd, err = parse(`{"action":"schedule","say":"Updated."}`)
	cmd = withUserCadence(cmd, "update the schedule to every hour")
	if err != nil || cmd.Unit != "hour" || cmd.Every == nil || *cmd.Every != 1 {
		t.Fatalf("model omitted the unit %+v %v", cmd, err)
	}
	cmd, err = parse(`{"action":"schedule","unit":"hour","every":"2","weekday":"Monday","on":"yes"}`)
	if err != nil || cmd.Unit != "hour" || cmd.Every == nil || *cmd.Every != 2 || cmd.Weekday == nil || *cmd.Weekday != 1 || cmd.On == nil || !*cmd.On {
		t.Fatalf("%+v %v", cmd, err)
	}
}

func TestApplyChangesModeScheduleAndAName(t *testing.T) {
	s := openStore(t)
	svc := &Service{Store: s, Agent: &agent.Agent{Store: s}}
	reply, err := svc.apply(context.Background(), command{Action: "mode", Mode: "active"})
	if err != nil || !reply.Changed {
		t.Fatal(reply, err)
	}
	mode, err := s.Mode()
	if err != nil || mode != "active" {
		t.Fatalf("mode %s %v", mode, err)
	}
	hour := 4
	reply, err = svc.apply(context.Background(), command{Action: "schedule", Hour: &hour, Unit: "day"})
	if err != nil || !reply.Changed {
		t.Fatal(reply, err)
	}
	schedule, err := s.ReviewSchedule()
	if err != nil || schedule.Hour != 4 || schedule.Unit != "day" || !schedule.Enabled {
		t.Fatalf("%+v %v", schedule, err)
	}
	hourly := withUserCadence(command{Action: "schedule"}, "update the schedule to every hour")
	reply, err = svc.apply(context.Background(), hourly)
	if err != nil || !reply.Changed || !strings.Contains(reply.Say, "every hour") {
		t.Fatal(reply, err)
	}
	schedule, err = s.ReviewSchedule()
	if err != nil || schedule.Every != 1 || schedule.Unit != "hour" {
		t.Fatalf("hourly save %+v %v", schedule, err)
	}
	reply, err = svc.apply(context.Background(), command{Action: "drop", Name: "ads.example.com"})
	if err != nil || !reply.Changed {
		t.Fatal(reply, err)
	}
	reply, err = svc.apply(context.Background(), command{Action: "undrop", Name: "ads.example.com"})
	if err != nil || !reply.Changed {
		t.Fatal(reply, err)
	}
	reply, err = svc.apply(context.Background(), command{Action: "decide", Name: "device-metrics-us.amazon.com", Choice: "refuse"})
	if err != nil || !reply.Changed {
		t.Fatal(reply, err)
	}
	cards, err := s.Cards(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range cards {
		if card.Destination == "device-metrics-us.amazon.com" && card.Action != "drop" {
			t.Fatalf("decision %+v", card)
		}
	}
	reply, err = svc.apply(context.Background(), command{Action: "none", Say: "Ano is passive."})
	if err != nil || reply.Changed || reply.Say != "Ano is passive." {
		t.Fatal(reply, err)
	}
	on := true
	gate := &gateStub{}
	svc.Gate = gate
	reply, err = svc.apply(context.Background(), command{Action: "gateway", On: &on})
	if err != nil || !reply.Changed || !gate.on {
		t.Fatal(reply, err, gate.on)
	}
}

func TestAskTalksUntilTheRequestIsComplete(t *testing.T) {
	s := openStore(t)
	svc := &Service{Store: s, Agent: &agent.Agent{Store: s}}

	svc.Model = scripted{raw: `{"action":"none","say":"Hi. What should I change?"}`}
	reply, err := svc.Ask(context.Background(), "hello")
	if err != nil || reply.Changed || reply.Say != "Hi. What should I change?" {
		t.Fatal(reply, err)
	}

	svc.Model = scripted{raw: `{"action":"schedule","say":"Updated."}`}
	reply, err = svc.Ask(context.Background(), "update the schedule")
	if err != nil || reply.Changed || !strings.Contains(reply.Say, "Which one?") {
		t.Fatal(reply, err)
	}
	schedule, err := s.ReviewSchedule()
	if err != nil || schedule.Unit != "day" {
		t.Fatalf("schedule changed %+v %v", schedule, err)
	}

	svc.Model = scripted{raw: `{"action":"none","say":"Every hour, or once a day?"}`}
	reply, err = svc.Ask(context.Background(), "I want reviews more often")
	if err != nil || reply.Changed || reply.Say != "Every hour, or once a day?" {
		t.Fatal(reply, err)
	}

	svc.Model = scripted{raw: `{"action":"mode","mode":"active","say":"Mode is active."}`}
	reply, err = svc.Ask(context.Background(), "what is the mode?")
	if err != nil || reply.Changed || !strings.Contains(reply.Say, "passive") {
		t.Fatal(reply, err)
	}

	svc.Model = scripted{raw: `{"action":"none","say":"Hello, I am Ano."}`}
	reply, err = svc.Ask(context.Background(), "update the schedule to every hour")
	if err != nil || !reply.Changed || !strings.Contains(reply.Say, "every hour") {
		t.Fatal(reply, err)
	}
	schedule, err = s.ReviewSchedule()
	if err != nil || schedule.Every != 1 || schedule.Unit != "hour" {
		t.Fatalf("hourly %+v %v", schedule, err)
	}

	svc.Model = scripted{raw: `{"action":"none","say":"What should I change?"}`}
	reply, err = svc.Ask(context.Background(), "what does this device do?")
	if err != nil || reply.Changed || !strings.Contains(reply.Say, "privacy appliance") {
		t.Fatal(reply, err)
	}

	svc.Model = scripted{raw: `{"action":"none","say":"Ano watches where home devices send."}`}
	reply, err = svc.Ask(context.Background(), "what is Ano?")
	if err != nil || reply.Changed || reply.Say != "Ano watches where home devices send." {
		t.Fatal(reply, err)
	}

	svc.Model = scripted{raw: `not json`}
	reply, err = svc.Ask(context.Background(), "how does Ano work?")
	if err != nil || reply.Changed || !strings.Contains(reply.Say, "packet path") {
		t.Fatal(reply, err)
	}

	svc.Model = scripted{raw: `{"action":"mode","mode":"active","say":"Sure."}`}
	reply, err = svc.Ask(context.Background(), "start enforcing")
	if err != nil || !reply.Changed || !strings.Contains(reply.Say, "active") {
		t.Fatal(reply, err)
	}
}

type scripted struct{ raw string }

func (s scripted) Ask(context.Context, string, string) (string, error) { return s.raw, nil }

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.LoadExample(); err != nil {
		t.Fatal(err)
	}
	return s
}

type gateStub struct{ on bool }

func (g *gateStub) Enable() error { g.on = true; return nil }
func (g *gateStub) Disable()      { g.on = false }
