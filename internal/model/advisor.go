// Package model is the on-machine speaker advisor. It scores a destination
// from the name, the rhythm, and past Drop or Allow choices, then writes the
// sentence a person reviews. It does not read payloads and it does not change
// live routing by itself.
package model

import "strings"

// Pattern is one speaker (or other device) behavior, already folded so a
// burst of the same lookup is a single review.
type Pattern struct {
	Destination string
	DeviceName  string
	Kind        string
	Category    string
	Rhythm      string
	Evidence    string
}

// Label is a decision the household already made.
type Label struct {
	DeviceName  string
	Destination string
	Category    string
	Action      string
}

// Advice is what the review card shows before anyone enforces it.
type Advice struct {
	Category    string
	Activity    string
	Sentence    string
	Consequence string
	Suggestion  string
	Source      string
}

// Classify maps a hostname onto a household category. Unknown names stay
// unknown and are suggested as a drop until a person allows them.
func Classify(host string) (category, activity string) {
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, "metrics") || strings.Contains(h, "telemetry") || strings.Contains(h, "analytics") || strings.Contains(h, "log."):
		return "telemetry", "usage reports"
	case strings.Contains(h, "adsystem") || strings.Contains(h, "doubleclick") || strings.Contains(h, "app-measurement") || strings.Contains(h, "scorecard"):
		return "ads", "ad calls"
	case strings.Contains(h, "firmware") || strings.Contains(h, "softwareupdate") || strings.Contains(h, "ota"):
		return "firmware-check", "firmware checks"
	default:
		return "unknown", "an unrecognized name"
	}
}

// Advise writes the card. A matching past decision wins over the default
// suggestion, and the sentence names that earlier choice.
func Advise(p Pattern, labels []Label) Advice {
	category := p.Category
	activity := ""
	if category == "" {
		category, activity = Classify(p.Destination)
	} else {
		_, activity = activityFor(category)
	}
	if activity == "" {
		_, activity = activityFor(category)
	}

	suggestion := defaultSuggestion(category)
	because := ""
	for _, label := range labels {
		if label.Category != category || label.Action == "" {
			continue
		}
		if label.Destination == p.Destination && label.DeviceName == p.DeviceName {
			continue
		}
		suggestion = label.Action
		if label.Action == "allow_envoy" {
			suggestion = "allow_direct"
		}
		because = " You already chose " + verb(suggestion) + " for " + activity + " on " + label.DeviceName + "."
		break
	}

	device := p.DeviceName
	if device == "" {
		device = "This device"
	}
	evidence := p.Evidence
	if evidence == "" {
		evidence = "a new lookup"
	}

	sentence := device + " is sending " + activity + " (" + evidence + ")." + because + " Suggested action: " + verb(suggestion) + "."
	return Advice{
		Category:    category,
		Activity:    activity,
		Sentence:    sentence,
		Consequence: consequence(category, suggestion),
		Suggestion:  suggestion,
		Source:      "model",
	}
}

func activityFor(category string) (string, string) {
	switch category {
	case "telemetry":
		return category, "usage reports"
	case "ads":
		return category, "ad calls"
	case "firmware-check":
		return category, "firmware checks"
	case "voice-assistant cloud":
		return category, "voice assistant traffic"
	default:
		return "unknown", "an unrecognized name"
	}
}

func defaultSuggestion(category string) string {
	switch category {
	case "telemetry", "ads", "unknown":
		return "drop"
	default:
		return "allow_direct"
	}
}

func verb(action string) string {
	switch action {
	case "drop":
		return "Drop"
	case "allow_envoy":
		return "Allow through the proxy"
	default:
		return "Allow directly"
	}
}

func consequence(category, suggestion string) string {
	switch category {
	case "telemetry":
		return "Dropping this stops those usage reports. Spoken answers are a separate card."
	case "ads":
		return "Dropping this stops the ad calls. It does not turn the speaker off."
	case "firmware-check":
		return "Allowing this keeps firmware checks working. Usage reports stay a separate card."
	case "voice-assistant cloud":
		if suggestion == "drop" {
			return "Dropping this stops the speaker from answering. Usage reports are a separate card."
		}
		return "Allowing this keeps spoken answers working. It does not allow the usage-report names."
	default:
		if suggestion == "drop" {
			return "Dropping this refuses the name until you allow it. Nothing is blocked until you choose."
		}
		return "Allowing this lets the name through. You can change it later."
	}
}
