// Package chat turns a typed or spoken request into one Ano action.
// The local model chooses the action. Ano applies only the actions it already knows.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ano/ano/internal/agent"
	"github.com/ano/ano/internal/store"
)

// Gateway is the home forward path. Chat can ask it to start or stop.
type Gateway interface {
	Enable() error
	Disable()
}

// Model is the local model. It answers and picks an action. Ano applies the action.
type Model interface {
	Ask(ctx context.Context, system, prompt string) (string, error)
}

// Service reads a request, asks the model, and applies the chosen action.
type Service struct {
	Store *store.Store
	Agent *agent.Agent
	Model Model
	Apply func() error
	Gate  Gateway
}

// Reply is what the screen says back, and whether Ano changed.
type Reply struct {
	Say     string `json:"say"`
	Changed bool   `json:"changed"`
}

const instruction = `You are Ano, talking with the person at home. Return one JSON object and nothing else.
Speak in one or two short sentences.
If they greet you or thank you, set action to none and answer the greeting in say. Do not bring up the schedule unless they mentioned it.
If they ask a general question, set action to none and answer it in say. Use the current facts. Do not change anything.
If they ask what Ano is, what this device does, how it works, or anything else about this product, set action to none and answer from the product description. Do not offer a change unless they asked for one.
If they seem to want a change but left out a detail, set action to none. In say, ask one question and suggest the choices that fit what they said.
Use a change action only when the request already names the whole change.
action is one of: none, mode, schedule, review, drop, undrop, decide, speakers, gateway.
mode sets mode to passive or active.
schedule sets on, every, unit, hour, minute, weekday, monthDay. unit is minute, hour, day, week, or month. weekday is 0 for Sunday through 6 for Saturday. monthDay is 1 to 28.
review runs the review now.
drop sets name to a DNS name and port to 0 for all ports, or one port.
undrop removes that DNS name.
decide sets name to a listed destination and choice to refuse, send, answer, or aside.
speakers sets on to true or false.
gateway sets on to true or false.
Do not invent a name that is not in the request or the current list.
Example: {"action":"none","say":"Hi. What would you like to change?"}`

// Ask sends the request to the model and applies the action it returns.
func (s *Service) Ask(ctx context.Context, text string) (Reply, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 500 {
		return Reply{}, errors.New("enter a short request")
	}
	facts, err := s.facts()
	if err != nil {
		return Reply{}, err
	}
	if s.Model == nil {
		return Reply{}, errors.New("Ano has no model")
	}
	raw, err := s.Model.Ask(ctx, instruction, facts+"\nRequest: "+text)
	if err != nil {
		return Reply{}, err
	}
	cmd, err := parse(raw)
	if err != nil {
		cmd = command{Action: "none", Say: clip(strings.TrimSpace(raw))}
	}
	if clear, ok := clearCommand(text); ok {
		clear = mergeFields(clear, cmd)
		clear.Say = ""
		clear = withUserCadence(clear, text)
		return s.apply(ctx, clear)
	}
	if inferredMode(cmd, text) {
		cmd.Action = "mode"
		cmd.Say = ""
		return s.apply(ctx, cmd)
	}
	return s.converse(cmd, text)
}

func (s *Service) converse(cmd command, user string) (Reply, error) {
	say := cmd.Say
	if cmd.Action != "" && cmd.Action != "none" && claimsDone(say) && !strings.Contains(say, "?") {
		say = ""
	}
	if productAsk(user) {
		if spoken := usable(say); spoken != "" && answersProduct(spoken) {
			return Reply{Say: spoken}, nil
		}
		return Reply{Say: aboutSay(user)}, nil
	}
	if spoken := usable(say); spoken != "" {
		return Reply{Say: spoken}, nil
	}
	fact, err := s.factSay(user)
	if err != nil {
		return Reply{}, err
	}
	if fact != "" {
		return Reply{Say: fact}, nil
	}
	return Reply{Say: suggest(user)}, nil
}

type command struct {
	Action   string
	Say      string
	Mode     string
	On       *bool
	Every    *int
	Unit     string
	Hour     *int
	Minute   *int
	Weekday  *int
	MonthDay *int
	Name     string
	Port     *int
	Choice   string
}

func (s *Service) facts() (string, error) {
	mode, err := s.Store.Mode()
	if err != nil {
		return "", err
	}
	speakers, err := s.Store.SpeakerOnly()
	if err != nil {
		return "", err
	}
	schedule, err := s.Store.ReviewSchedule()
	if err != nil {
		return "", err
	}
	cards, err := s.Store.Cards(false)
	if err != nil {
		return "", err
	}
	drops, err := s.Store.DropNames()
	if err != nil {
		return "", err
	}
	var names []string
	for _, card := range cards {
		if len(names) == 12 {
			break
		}
		names = append(names, card.Destination)
	}
	var listed []string
	for _, drop := range drops {
		if len(listed) == 12 {
			break
		}
		if drop.Port == 0 {
			listed = append(listed, drop.Name)
			continue
		}
		listed = append(listed, fmt.Sprintf("%s:%d", drop.Name, drop.Port))
	}
	on := "off"
	if schedule.Enabled {
		on = "on"
	}
	speaker := "off"
	if speakers {
		speaker = "on"
	}
	if len(names) == 0 {
		names = []string{"none"}
	}
	if len(listed) == 0 {
		listed = []string{"none"}
	}
	return fmt.Sprintf("%s\nmode %s\nspeakers %s\nschedule %s every %d %s at %02d:%02d weekday %d monthday %d\nnames %s\ndrops %s",
		productBrief, mode, speaker, on, schedule.Every, schedule.Unit, schedule.Hour, schedule.Minute, schedule.Weekday, schedule.MonthDay, strings.Join(names, ", "), strings.Join(listed, ", ")), nil
}

const productBrief = `Ano is this privacy appliance. It watches where home devices send, asks a local model what that pattern looks like, and applies Refuse or Send when a person presses the button or when Active mode is on. Passive watches and stores the suggestion. Active enforces Refuse and Send. A person's button wins over the model. The model stays off the packet path and reads the past 24 hours on a schedule. Ano stores the device, name, port, protocol, and the decision. It does not store audio, page contents, or credentials. The gateway stays off until a second Ethernet jack is connected.`

func productAsk(user string) bool {
	low := strings.ToLower(user)
	if greeting(low) {
		return false
	}
	if statusTopic(low) && !aboutDevice(low) {
		return false
	}
	if aboutDevice(low) {
		return true
	}
	if strings.Contains(low, "what does") || strings.Contains(low, "how does") || strings.Contains(low, "what is a") {
		for _, topic := range []string{"refuse", "gateway", "review", "envoy", "proxy", "button", "packet", "model"} {
			if strings.Contains(low, topic) {
				return true
			}
		}
	}
	return false
}

func statusTopic(low string) bool {
	for _, word := range []string{"schedule", "how often", "speaker", "mode", "passive", "active"} {
		if strings.Contains(low, word) {
			return true
		}
	}
	return false
}

func aboutDevice(low string) bool {
	for _, phrase := range []string{
		"ano", "this device", "the device", "this appliance", "this product", "this thing",
		"who are you", "what are you", "what do you do", "what can you do", "what is this",
		"what's this", "whats this", "what does this", "how does this", "how do you",
	} {
		if strings.Contains(low, phrase) {
			return true
		}
	}
	if (strings.Contains(low, "tell me about") || strings.Contains(low, "explain")) && (strings.Contains(low, "you") || strings.Contains(low, "ano") || strings.Contains(low, "device") || strings.Contains(low, "product")) {
		return true
	}
	return strings.Contains(low, "privacy")
}

func answersProduct(say string) bool {
	low := strings.ToLower(say)
	if strings.Contains(low, "what should i change") || strings.Contains(low, "what would you like") || strings.Contains(low, "which one") {
		return false
	}
	for _, word := range []string{"ano", "device", "watch", "privacy", "refuse", "passive", "active", "traffic", "appliance", "packet", "home", "button", "gateway"} {
		if strings.Contains(low, word) {
			return true
		}
	}
	return false
}

func aboutSay(user string) string {
	low := strings.ToLower(user)
	switch {
	case strings.Contains(low, "store") || strings.Contains(low, "record") || strings.Contains(low, "audio") || strings.Contains(low, "credential") || strings.Contains(low, "privacy"):
		return "Ano stores the device, the name, the port, the protocol, and the decision. It does not store audio, page contents, or credentials."
	case strings.Contains(low, "gateway") || strings.Contains(low, "where") || strings.Contains(low, "sit") || strings.Contains(low, "jack"):
		return "Ano sits between the home Wi-Fi and the router. The gateway stays off until the second Ethernet jack is connected."
	case strings.Contains(low, "passive") || strings.Contains(low, "active") || strings.Contains(low, "mode") || strings.Contains(low, "enforce"):
		return "Passive watches and stores a suggestion. Active enforces Refuse and Send. A person's button wins over the model."
	case strings.Contains(low, "refuse") || strings.Contains(low, "send") || strings.Contains(low, "button") || strings.Contains(low, "answer"):
		return "Refuse stops a name. Send lets it go out. Answer here replies on Ano. Set aside stops enforcing that name."
	case strings.Contains(low, "model") || strings.Contains(low, "packet") || strings.Contains(low, "how"):
		return "The model stays off the packet path. It reads the past 24 hours on a schedule and suggests Refuse or Send."
	default:
		return "Ano is this privacy appliance. It watches where home devices send, and it can refuse a name when you press Refuse or when Active mode is on."
	}
}

func parse(raw string) (command, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return command{}, errors.New("no json")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw[start:end+1]), &body); err != nil {
		return command{}, err
	}
	action := strings.ToLower(asString(body["action"]))
	cmd := command{
		Action:  action,
		Say:     clip(asString(body["say"])),
		Mode:    strings.ToLower(asString(body["mode"])),
		Unit:    strings.ToLower(asString(body["unit"])),
		Name:    strings.TrimSpace(asString(body["name"])),
		Choice:  strings.ToLower(asString(body["choice"])),
		On:      asBool(body["on"]),
		Every:   asInt(body["every"]),
		Hour:    asInt(body["hour"]),
		Minute:  asInt(body["minute"]),
		Weekday: asWeekday(body["weekday"]),
		Port:    asInt(body["port"]),
	}
	if day := asInt(body["monthDay"]); day != nil {
		cmd.MonthDay = day
	} else {
		cmd.MonthDay = asInt(body["monthday"])
	}
	switch action {
	case "none", "mode", "schedule", "review", "drop", "undrop", "decide", "speakers", "gateway":
		return cmd, nil
	default:
		phrase, err := fromPhrase(cmd)
		if err != nil {
			cmd.Action = "none"
			return cmd, nil
		}
		return phrase, nil
	}
}

func fromPhrase(cmd command) (command, error) {
	clear, ok := clearCommand(cmd.Action)
	if !ok {
		return command{}, errors.New("unknown action")
	}
	clear.Say = cmd.Say
	return mergeFields(clear, cmd), nil
}

func clearCommand(text string) (command, bool) {
	low := strings.ToLower(strings.TrimSpace(text))
	if low == "" || questionOnly(low) {
		return command{}, false
	}
	if hasWord(low, "passive") && hasWord(low, "active") {
		return command{}, false
	}
	if hasWord(low, "passive") {
		return command{Action: "mode", Mode: "passive"}, true
	}
	if hasWord(low, "active") {
		return command{Action: "mode", Mode: "active"}, true
	}
	if every, unit, ok := readCadence(low); ok {
		return command{Action: "schedule", Every: &every, Unit: unit}, true
	}
	if strings.Contains(low, "review now") || strings.Contains(low, "run the review") || strings.Contains(low, "run a review") || strings.Contains(low, "start the review") || strings.Contains(low, "start a review") {
		return command{Action: "review"}, true
	}
	if strings.Contains(low, "speaker") {
		switch {
		case hasWord(low, "off") || strings.Contains(low, "disable"):
			off := false
			return command{Action: "speakers", On: &off}, true
		case hasWord(low, "on") || strings.Contains(low, "enable"):
			on := true
			return command{Action: "speakers", On: &on}, true
		default:
			return command{}, false
		}
	}
	if strings.Contains(low, "gateway") {
		switch {
		case hasWord(low, "off") || hasWord(low, "stop") || strings.Contains(low, "disable"):
			off := false
			return command{Action: "gateway", On: &off}, true
		case hasWord(low, "on") || hasWord(low, "start") || strings.Contains(low, "enable"):
			on := true
			return command{Action: "gateway", On: &on}, true
		default:
			return command{}, false
		}
	}
	name := dottedName(low)
	if name == "" {
		return command{}, false
	}
	if strings.Contains(low, "undrop") || strings.Contains(low, "remove") || strings.Contains(low, "unlist") {
		return command{Action: "undrop", Name: name}, true
	}
	if strings.Contains(low, "drop") || strings.Contains(low, "block") {
		cmd := command{Action: "drop", Name: name}
		if port, ok := portNumber(low); ok {
			cmd.Port = &port
		}
		return cmd, true
	}
	if choice := choiceWord(low); choice != "" {
		return command{Action: "decide", Name: name, Choice: choice}, true
	}
	return command{}, false
}

func inferredMode(cmd command, user string) bool {
	if cmd.Action != "mode" || (cmd.Mode != "passive" && cmd.Mode != "active") {
		return false
	}
	low := strings.ToLower(user)
	if questionOnly(low) || greeting(low) || hasWord(low, "passive") || hasWord(low, "active") {
		return false
	}
	return strings.Contains(low, "mode") || strings.Contains(low, "enforc") || strings.Contains(low, "watch")
}

func questionOnly(low string) bool {
	low = strings.TrimSpace(low)
	for _, word := range []string{"what", "why", "how", "who", "where", "which", "when"} {
		if low == word || strings.HasPrefix(low, word+" ") || strings.HasPrefix(low, word+"'") {
			return true
		}
	}
	return strings.Contains(low, "?") && !hasCommandCue(low)
}

func hasCommandCue(low string) bool {
	for _, cue := range []string{"switch", "set ", "turn ", "change", "update", "make ", "enable", "disable", "start", "stop", "drop", "block", "undrop", "remove", "review now", "every "} {
		if strings.Contains(low, cue) {
			return true
		}
	}
	return false
}

func greeting(low string) bool {
	low = strings.Trim(strings.ToLower(low), " \t.!?")
	switch low {
	case "hi", "hello", "hey", "hiya", "yo", "thanks", "thank you", "good morning", "good evening", "good afternoon", "good night", "how are you", "how's it going", "hows it going":
		return true
	}
	if strings.HasPrefix(low, "hi ") || strings.HasPrefix(low, "hello ") || strings.HasPrefix(low, "hey ") || strings.HasPrefix(low, "good morning") || strings.HasPrefix(low, "good evening") || strings.HasPrefix(low, "thanks") || strings.HasPrefix(low, "thank you") {
		return !hasCommandCue(low)
	}
	return false
}

func hasWord(text, word string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(text)
}

func choiceWord(low string) string {
	switch {
	case hasWord(low, "refuse"):
		return "refuse"
	case hasWord(low, "send") || hasWord(low, "allow"):
		return "send"
	case hasWord(low, "answer"):
		return "answer"
	case hasWord(low, "aside") || hasWord(low, "ignore"):
		return "aside"
	default:
		return ""
	}
}

var portNumberRe = regexp.MustCompile(`\bport\s+(\d{1,5})\b`)

func portNumber(low string) (int, bool) {
	m := portNumberRe.FindStringSubmatch(low)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

func mergeFields(dst, src command) command {
	if dst.Say == "" {
		dst.Say = src.Say
	}
	if dst.Port == nil {
		dst.Port = src.Port
	}
	if dst.Hour == nil {
		dst.Hour = src.Hour
	}
	if dst.Minute == nil {
		dst.Minute = src.Minute
	}
	if dst.On == nil {
		dst.On = src.On
	}
	if dst.Weekday == nil {
		dst.Weekday = src.Weekday
	}
	if dst.MonthDay == nil {
		dst.MonthDay = src.MonthDay
	}
	if dst.Every == nil {
		dst.Every = src.Every
	}
	if dst.Unit == "" {
		dst.Unit = src.Unit
	}
	if dst.Mode == "" {
		dst.Mode = src.Mode
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if dst.Choice == "" {
		dst.Choice = src.Choice
	}
	return dst
}

func usable(say string) string {
	say = clip(say)
	if say == "" || claimsDone(say) {
		return ""
	}
	return say
}

func claimsDone(say string) bool {
	low := strings.ToLower(say)
	for _, phrase := range []string{"nothing changed", "updated", "i set", "i changed", "i turned", "mode is", "the schedule is", "schedule is now", "listed for drop", "no longer listed", "review is done", "gateway is", "speakers only is"} {
		if strings.Contains(low, phrase) {
			return true
		}
	}
	return false
}

func statusAsk(user string) bool {
	low := strings.ToLower(user)
	return strings.Contains(low, "?") || strings.Contains(low, "what") || strings.Contains(low, "status") || strings.Contains(low, "how often") || strings.HasPrefix(strings.TrimSpace(low), "when")
}

func (s *Service) factSay(user string) (string, error) {
	if !statusAsk(user) {
		return "", nil
	}
	low := strings.ToLower(user)
	switch {
	case strings.Contains(low, "schedule") || strings.Contains(low, "review") || strings.Contains(low, "often"):
		schedule, err := s.Store.ReviewSchedule()
		if err != nil {
			return "", err
		}
		return scheduleSay(schedule.Enabled, schedule.Every, schedule.Unit, schedule.Hour, schedule.Minute), nil
	case strings.Contains(low, "mode") || strings.Contains(low, "passive") || strings.Contains(low, "active"):
		mode, err := s.Store.Mode()
		if err != nil {
			return "", err
		}
		if mode == "active" {
			return "Mode is active. Active enforces Refuse and Send.", nil
		}
		return "Mode is passive. Passive watches.", nil
	case strings.Contains(low, "speaker"):
		on, err := s.Store.SpeakerOnly()
		if err != nil {
			return "", err
		}
		if on {
			return "Speakers only is on.", nil
		}
		return "Speakers only is off.", nil
	default:
		return "", nil
	}
}

func suggest(user string) string {
	low := strings.ToLower(user)
	switch {
	case greeting(low):
		return "Hi. I can change the mode, the schedule, a drop, or a device choice. What should I change?"
	case strings.Contains(low, "schedule") || strings.Contains(low, "review") || strings.Contains(low, "hour") || strings.Contains(low, "daily") || strings.Contains(low, "weekly"):
		return "The review can run every minute, hour, day, week, or month. Which one?"
	case strings.Contains(low, "drop") || strings.Contains(low, "block") || strings.Contains(low, "dns"):
		return "Which DNS name should I drop, and is that all ports or one port?"
	case strings.Contains(low, "mode") || strings.Contains(low, "passive") || strings.Contains(low, "active") || strings.Contains(low, "enforce"):
		return "Passive watches. Active enforces. Which one?"
	case strings.Contains(low, "gateway"):
		return "Gateway on or off? It stays off until the second Ethernet jack is connected."
	case strings.Contains(low, "speaker"):
		return "Speakers only on or off?"
	default:
		return "I can change the mode, the schedule, a drop, or a device choice. What should I change?"
	}
}

var everyCount = regexp.MustCompile(`every\s+(\d+)\s*(minutes|minute|mins|min|hours|hour|hrs|hr|days|day|weeks|week|months|month)`)
var everyOne = regexp.MustCompile(`every\s+(minutes|minute|hours|hour|days|day|weeks|week|months|month)`)

func withUserCadence(cmd command, user string) command {
	every, unit, ok := readCadence(user)
	if !ok {
		every, unit, ok = readCadence(cmd.Action + " " + cmd.Say)
	}
	if !ok {
		return cmd
	}
	low := strings.ToLower(user)
	if strings.Contains(low, "what") || strings.Contains(low, "?") {
		return cmd
	}
	cmd.Action = "schedule"
	cmd.Every = &every
	cmd.Unit = unit
	return cmd
}

func readCadence(text string) (int, string, bool) {
	text = strings.ToLower(text)
	if m := everyCount.FindStringSubmatch(text); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return 0, "", false
		}
		return n, unitWord(m[2]), true
	}
	switch {
	case strings.Contains(text, "hourly"):
		return 1, "hour", true
	case strings.Contains(text, "daily"):
		return 1, "day", true
	case strings.Contains(text, "weekly"):
		return 1, "week", true
	case strings.Contains(text, "monthly"):
		return 1, "month", true
	}
	if m := everyOne.FindStringSubmatch(text); m != nil {
		return 1, unitWord(m[1]), true
	}
	return 0, "", false
}

func unitWord(word string) string {
	switch {
	case strings.HasPrefix(word, "min"):
		return "minute"
	case strings.HasPrefix(word, "h"):
		return "hour"
	case strings.HasPrefix(word, "d"):
		return "day"
	case strings.HasPrefix(word, "w"):
		return "week"
	default:
		return "month"
	}
}

func scheduleSay(on bool, every int, unit string, hour, minute int) string {
	if !on {
		return "The schedule is off."
	}
	name := unit
	if every != 1 {
		name += "s"
	}
	count := ""
	if every != 1 {
		count = fmt.Sprintf("%d ", every)
	}
	switch unit {
	case "minute":
		return "The schedule is every " + count + name + "."
	case "hour":
		return fmt.Sprintf("The schedule is every %s%s at minute %d.", count, name, minute)
	default:
		return fmt.Sprintf("The schedule is every %s%s at %02d:%02d.", count, name, hour, minute)
	}
}

func dottedName(text string) string {
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, ".,")
		if strings.Count(field, ".") >= 1 && !strings.Contains(field, " ") {
			return field
		}
	}
	return ""
}

func (s *Service) apply(ctx context.Context, cmd command) (Reply, error) {
	switch cmd.Action {
	case "none":
		if cmd.Say == "" {
			cmd.Say = "Nothing changed."
		}
		return Reply{Say: cmd.Say}, nil
	case "mode":
		if cmd.Mode != "passive" && cmd.Mode != "active" {
			return Reply{Say: "Passive watches. Active enforces. Which one?"}, nil
		}
		if s.Agent == nil {
			return Reply{Say: "Ano could not change the mode."}, nil
		}
		if err := s.Agent.SetMode(cmd.Mode); err != nil {
			return Reply{Say: err.Error()}, nil
		}
		if cmd.Mode == "active" {
			return Reply{Say: "Mode is active. Active enforces.", Changed: true}, nil
		}
		return Reply{Say: "Mode is passive. Passive watches.", Changed: true}, nil
	case "schedule":
		current, err := s.Store.ReviewSchedule()
		if err != nil {
			return Reply{}, err
		}
		on := current.Enabled
		if cmd.On != nil {
			on = *cmd.On
		}
		every, hour, minute, weekday, monthDay := current.Every, current.Hour, current.Minute, current.Weekday, current.MonthDay
		unit := current.Unit
		if cmd.Every != nil {
			every = *cmd.Every
		}
		if cmd.Hour != nil {
			hour = *cmd.Hour
		}
		if cmd.Minute != nil {
			minute = *cmd.Minute
		}
		if cmd.Weekday != nil {
			weekday = *cmd.Weekday
		}
		if cmd.MonthDay != nil {
			monthDay = *cmd.MonthDay
		}
		if cmd.Unit != "" {
			unit = cmd.Unit
		}
		if err := s.Store.SetReviewSchedule(on, every, unit, hour, minute, weekday, monthDay); err != nil {
			return Reply{Say: err.Error()}, nil
		}
		return Reply{Say: scheduleSay(on, every, unit, hour, minute), Changed: true}, nil
	case "review":
		if s.Agent == nil {
			return Reply{Say: "Ano could not start the review."}, nil
		}
		n, err := s.Agent.Daily(ctx)
		if err != nil && n == 0 {
			return Reply{Say: err.Error()}, nil
		}
		say := cmd.Say
		if say == "" {
			say = "The review is done."
			if n == 0 {
				say = "No new traffic to review."
			}
		}
		return Reply{Say: say, Changed: true}, nil
	case "drop":
		port := 0
		if cmd.Port != nil {
			port = *cmd.Port
		}
		if _, err := s.Store.AddBlock(cmd.Name, port); err != nil {
			return Reply{Say: err.Error()}, nil
		}
		if err := s.reload(); err != nil {
			return Reply{}, err
		}
		return Reply{Say: orSay(cmd.Say, "That name is listed for drop."), Changed: true}, nil
	case "undrop":
		drops, err := s.Store.DropNames()
		if err != nil {
			return Reply{}, err
		}
		id, explain := matchDrop(drops, cmd.Name)
		if id == "" {
			return Reply{Say: explain}, nil
		}
		if _, err := s.Store.DeleteBlock(id); err != nil {
			return Reply{Say: err.Error()}, nil
		}
		if err := s.reload(); err != nil {
			return Reply{}, err
		}
		return Reply{Say: orSay(cmd.Say, "That name is no longer listed."), Changed: true}, nil
	case "decide":
		cards, err := s.Store.Cards(false)
		if err != nil {
			return Reply{}, err
		}
		card, err := matchCard(cards, cmd.Name)
		if err != nil {
			return Reply{Say: err.Error()}, nil
		}
		choice := decision(cmd.Choice)
		if choice == "" {
			return Reply{Say: "Say refuse, send, answer, or aside."}, nil
		}
		if _, err := s.Store.Decide(card.ID, choice); err != nil {
			return Reply{Say: err.Error()}, nil
		}
		if err := s.reload(); err != nil {
			return Reply{}, err
		}
		return Reply{Say: orSay(cmd.Say, "That choice is saved."), Changed: true}, nil
	case "speakers":
		if cmd.On == nil {
			return Reply{Say: "Say speakers on or off."}, nil
		}
		if err := s.Store.SetSpeakerOnly(*cmd.On); err != nil {
			return Reply{}, err
		}
		state := "off"
		if *cmd.On {
			state = "on"
		}
		return Reply{Say: orSay(cmd.Say, "Speakers only is "+state+"."), Changed: true}, nil
	case "gateway":
		if cmd.On == nil || s.Gate == nil {
			return Reply{Say: "Say gateway on or off."}, nil
		}
		if *cmd.On {
			if err := s.Gate.Enable(); err != nil {
				return Reply{Say: err.Error()}, nil
			}
			return Reply{Say: orSay(cmd.Say, "The gateway is on."), Changed: true}, nil
		}
		s.Gate.Disable()
		return Reply{Say: orSay(cmd.Say, "The gateway is off."), Changed: true}, nil
	default:
		return Reply{Say: suggest("")}, nil
	}
}

func (s *Service) reload() error {
	if s.Apply == nil {
		return nil
	}
	return s.Apply()
}

func matchCard(cards []store.Card, name string) (store.Card, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return store.Card{}, errors.New("say which name")
	}
	var hits []store.Card
	for _, card := range cards {
		dest := strings.ToLower(card.Destination)
		activity := strings.ToLower(card.Activity)
		device := strings.ToLower(card.DeviceName)
		if dest == name || activity == name || strings.Contains(dest, name) || strings.Contains(activity, name) || strings.Contains(device, name) {
			hits = append(hits, card)
		}
	}
	if len(hits) == 0 {
		return store.Card{}, errors.New("that name is not on a device")
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	for _, card := range hits {
		if strings.ToLower(card.Destination) == name {
			return card, nil
		}
	}
	var names []string
	for _, card := range hits {
		if len(names) == 4 {
			break
		}
		names = append(names, card.Destination)
	}
	return store.Card{}, fmt.Errorf("that name matches more than one: %s", strings.Join(names, ", "))
}

func matchDrop(drops []store.DropName, name string) (string, string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "", "say which name"
	}
	var hit *store.DropName
	for i := range drops {
		if drops[i].Name == name || strings.Contains(drops[i].Name, name) {
			if hit != nil {
				return "", "that name matches more than one drop"
			}
			hit = &drops[i]
		}
	}
	if hit == nil {
		return "", "that name is not listed"
	}
	if hit.Source != "admin" {
		return "", "that name is a Refuse choice. Change it on the device."
	}
	return hit.ID, ""
}

func decision(choice string) string {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "refuse", "drop":
		return "drop"
	case "send", "allow":
		return "allow_direct"
	case "answer", "envoy":
		return "allow_envoy"
	case "aside", "ignore":
		return "ignore"
	default:
		return ""
	}
}

func asString(v any) string {
	text, _ := v.(string)
	return strings.TrimSpace(text)
}

func asBool(v any) *bool {
	switch value := v.(type) {
	case bool:
		return &value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "on", "yes":
			on := true
			return &on
		case "false", "off", "no":
			off := false
			return &off
		}
	}
	return nil
}

func asInt(v any) *int {
	switch value := v.(type) {
	case float64:
		n := int(value)
		return &n
	case string:
		var n int
		if _, err := fmt.Sscan(strings.TrimSpace(value), &n); err == nil {
			return &n
		}
	}
	return nil
}

func asWeekday(v any) *int {
	if n := asInt(v); n != nil {
		return n
	}
	switch strings.ToLower(asString(v)) {
	case "sunday":
		n := 0
		return &n
	case "monday":
		n := 1
		return &n
	case "tuesday":
		n := 2
		return &n
	case "wednesday":
		n := 3
		return &n
	case "thursday":
		n := 4
		return &n
	case "friday":
		n := 5
		return &n
	case "saturday":
		n := 6
		return &n
	default:
		return nil
	}
}

func orSay(say, fallback string) string {
	if strings.TrimSpace(say) == "" {
		return fallback
	}
	return clip(say)
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 240 {
		return s[:240]
	}
	return s
}
