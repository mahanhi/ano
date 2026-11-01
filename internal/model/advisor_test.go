package model

import "testing"

func TestClassifyMetrics(t *testing.T) {
	category, activity := Classify("device-metrics-us.amazon.com")
	if category != "telemetry" || activity != "usage reports" {
		t.Fatalf("got %s %s", category, activity)
	}
}

func TestAdviseSuggestsDropForUsageReports(t *testing.T) {
	advice := Advise(Pattern{
		Destination: "device-metrics-us.amazon.com",
		DeviceName:  "Kitchen speaker",
		Kind:        "speaker",
		Category:    "telemetry",
		Evidence:    "about every 5 minutes, small uploads",
	}, nil)
	if advice.Suggestion != "drop" {
		t.Fatalf("suggestion %s", advice.Suggestion)
	}
	if advice.Source != "model" {
		t.Fatalf("source %s", advice.Source)
	}
	if !contains(advice.Sentence, "Kitchen speaker") || !contains(advice.Sentence, "usage reports") {
		t.Fatalf("sentence %s", advice.Sentence)
	}
}

func TestAdviseReusesAPastDrop(t *testing.T) {
	advice := Advise(Pattern{
		Destination: "device-metrics-us-2.amazon.com",
		DeviceName:  "Bedroom speaker",
		Category:    "telemetry",
		Evidence:    "a steady trickle",
	}, []Label{{
		DeviceName:  "Kitchen speaker",
		Destination: "device-metrics-us.amazon.com",
		Category:    "telemetry",
		Action:      "drop",
	}})
	if advice.Suggestion != "drop" {
		t.Fatalf("suggestion %s", advice.Suggestion)
	}
	if !contains(advice.Sentence, "Kitchen speaker") {
		t.Fatalf("sentence %s", advice.Sentence)
	}
}

func TestAdviseKeepsVoiceCloudAsAnAllow(t *testing.T) {
	advice := Advise(Pattern{
		Destination: "avs-alexa-na.amazon.com",
		DeviceName:  "Kitchen speaker",
		Category:    "voice-assistant cloud",
		Evidence:    "used when someone talks to the speaker",
	}, nil)
	if advice.Suggestion != "allow_direct" {
		t.Fatalf("suggestion %s", advice.Suggestion)
	}
	if !contains(advice.Consequence, "spoken answers") {
		t.Fatalf("consequence %s", advice.Consequence)
	}
}

func contains(s, part string) bool {
	return len(s) >= len(part) && (s == part || len(part) == 0 || (len(s) > 0 && (func() bool {
		return stringIndex(s, part) >= 0
	})()))
}

func stringIndex(s, part string) int {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}
