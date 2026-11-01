package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hbar/hbar/internal/model"

	_ "modernc.org/sqlite"
)

// Store is the on-disk policy, cards, and decision log.
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// Device is one household gadget.
type Device struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Room   string `json:"room"`
	ViaAno bool   `json:"viaAno"`
}

// Card is one pattern waiting for a person, or already decided.
type Card struct {
	ID           string  `json:"id"`
	DeviceID     string  `json:"deviceId"`
	DeviceName   string  `json:"deviceName"`
	Room         string  `json:"room"`
	Kind         string  `json:"kind"`
	Destination  string  `json:"destination"`
	Category     string  `json:"category"`
	Protocol     string  `json:"protocol"`
	Activity     string  `json:"activity"`
	Sentence     string  `json:"sentence"`
	Consequence  string  `json:"consequence"`
	Suggestion   string  `json:"suggestion"`
	Status       string  `json:"status"`
	Action       string  `json:"action"`
	Evidence     string  `json:"evidence"`
	Source       string  `json:"source"`
	Reroute      string  `json:"reroute"`
	LlmAction    string  `json:"llmAction"`
	LlmReason    string  `json:"llmReason"`
	Observations int     `json:"observations"`
	Metrics      Metrics `json:"metrics"`
}

// ReviewItem is collected metadata waiting for the local model.
type ReviewItem struct {
	ID          string
	DeviceID    string
	DeviceName  string
	Kind        string
	Room        string
	Destination string
	Category    string
	Protocol    string
	Evidence    string
	Lookups     int
	Bytes       int
	Cadence     string
	SendTime    string
	Similar     string
}

// Metrics is the usage picture before a drop and the refusals after it.
type Metrics struct {
	BeforeLookups int  `json:"beforeLookups"`
	BeforeBytes   int  `json:"beforeBytes"`
	EverySeconds  int  `json:"everySeconds"`
	AfterLookups  int  `json:"afterLookups"`
	AfterBytes    int  `json:"afterBytes"`
	Dropped       bool `json:"dropped"`
}

// Decision is one line in the log the household can read later.
type Decision struct {
	ID          string `json:"id"`
	At          string `json:"at"`
	DeviceName  string `json:"deviceName"`
	Destination string `json:"destination"`
	Outcome     string `json:"outcome"`
	Detail      string `json:"detail"`
}

// Sample is one live point for the charts.
type Sample struct {
	At          string `json:"at"`
	PatternID   string `json:"patternId"`
	Destination string `json:"destination"`
	DeviceName  string `json:"deviceName"`
	LeftBytes   int    `json:"leftBytes"`
	KeptBytes   int    `json:"keptBytes"`
	Lookups     int    `json:"lookups"`
	Refused     int    `json:"refused"`
}

// EnvoyRoute is an approved rule Envoy should carry.
type EnvoyRoute struct {
	Destination string
	Action      string
	Reroute     string
}

// Open creates or opens the SQLite file. Devices come from a Wi-Fi and router scan, not from a saved list.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.dropUnseen(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.backfillSends(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.stampProtocols(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.backfillSeries(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  speaker_only INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS devices (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  room TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS patterns (
  id TEXT PRIMARY KEY,
  device_id TEXT NOT NULL,
  destination TEXT NOT NULL,
  category TEXT NOT NULL,
  evidence TEXT NOT NULL,
  observations INTEGER NOT NULL,
  UNIQUE(device_id, destination)
);
CREATE TABLE IF NOT EXISTS rules (
  id TEXT PRIMARY KEY,
  pattern_id TEXT NOT NULL UNIQUE,
  action TEXT NOT NULL,
  status TEXT NOT NULL,
  source TEXT NOT NULL,
  sentence TEXT NOT NULL,
  consequence TEXT NOT NULL,
  suggestion TEXT NOT NULL,
  activity TEXT NOT NULL,
  reviewed_at TEXT
);
CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY,
  at TEXT NOT NULL,
  device_name TEXT NOT NULL,
  destination TEXT NOT NULL,
  outcome TEXT NOT NULL,
  detail TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS samples (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL,
  pattern_id TEXT NOT NULL,
  destination TEXT NOT NULL,
  device_name TEXT NOT NULL,
  left_bytes INTEGER NOT NULL,
  kept_bytes INTEGER NOT NULL,
  lookups INTEGER NOT NULL,
  refused INTEGER NOT NULL
);
`)
	if err != nil {
		return err
	}
	for _, col := range []struct{ name, decl string }{
		{"bytes_out", "INTEGER NOT NULL DEFAULT 0"},
		{"every_seconds", "INTEGER NOT NULL DEFAULT 0"},
		{"per_lookup_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"before_lookups", "INTEGER NOT NULL DEFAULT 0"},
		{"before_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"after_lookups", "INTEGER NOT NULL DEFAULT 0"},
		{"after_bytes", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.ensureColumn("patterns", col.name, col.decl); err != nil {
			return err
		}
	}
	if err := s.ensureColumn("rules", "reroute_host", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, col := range []struct{ name, decl string }{
		{"llm_action", "TEXT NOT NULL DEFAULT ''"},
		{"llm_reason", "TEXT NOT NULL DEFAULT ''"},
		{"llm_at", "TEXT NOT NULL DEFAULT ''"},
		{"llm_lookups", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.ensureColumn("rules", col.name, col.decl); err != nil {
			return err
		}
	}
	if err := s.ensureColumn("settings", "mode", "TEXT NOT NULL DEFAULT 'passive'"); err != nil {
		return err
	}
	if err := s.ensureColumn("patterns", "protocol", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("devices", "addr", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("devices", "mac", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("devices", "via_ano", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("settings", "router_addr", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("settings", "router_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("settings", "router_iface", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err = s.db.Exec(`
CREATE TABLE IF NOT EXISTS observations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL,
  device_id TEXT NOT NULL,
  destination TEXT NOT NULL,
  lookups INTEGER NOT NULL,
  bytes INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sends (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL,
  device_id TEXT NOT NULL,
  destination TEXT NOT NULL,
  category TEXT NOT NULL,
  bytes INTEGER NOT NULL
)`); err != nil {
		return err
	}
	if err := s.ensureColumn("observations", "protocol", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("sends", "protocol", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO settings (id, speaker_only) VALUES (1, 1) ON CONFLICT(id) DO NOTHING`)
	return err
}

func (s *Store) ensureColumn(table, column, decl string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + decl)
	return err
}

// dropUnseen removes devices that were never found on Wi-Fi or the router.
func (s *Store) dropUnseen() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, name FROM devices WHERE TRIM(COALESCE(mac, '')) = ''`)
	if err != nil {
		return err
	}
	type row struct{ id, name string }
	var gone []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.name); err != nil {
			rows.Close()
			return err
		}
		gone = append(gone, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range gone {
		if _, err = s.db.Exec(`DELETE FROM rules WHERE pattern_id IN (SELECT id FROM patterns WHERE device_id = ?)`, item.id); err != nil {
			return err
		}
		if _, err = s.db.Exec(`DELETE FROM samples WHERE pattern_id IN (SELECT id FROM patterns WHERE device_id = ?)`, item.id); err != nil {
			return err
		}
		if _, err = s.db.Exec(`DELETE FROM patterns WHERE device_id = ?`, item.id); err != nil {
			return err
		}
		if _, err = s.db.Exec(`DELETE FROM observations WHERE device_id = ?`, item.id); err != nil {
			return err
		}
		if _, err = s.db.Exec(`DELETE FROM sends WHERE device_id = ?`, item.id); err != nil {
			return err
		}
		if _, err = s.db.Exec(`DELETE FROM decisions WHERE device_name = ?`, item.name); err != nil {
			return err
		}
		if _, err = s.db.Exec(`DELETE FROM devices WHERE id = ?`, item.id); err != nil {
			return err
		}
	}
	return nil
}

// LoadExample inserts the speaker and phone used by tests. The running app does not call this.
func (s *Store) LoadExample() error {
	if err := s.seed(); err != nil {
		return err
	}
	if err := s.backfillSends(); err != nil {
		return err
	}
	if err := s.stampProtocols(); err != nil {
		return err
	}
	return s.backfillSeries()
}

func (s *Store) seed() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	devices := []Device{
		{ID: "dev-kitchen-speaker", Name: "Kitchen speaker", Kind: "speaker", Room: "Kitchen"},
		{ID: "dev-hall-phone", Name: "Hall phone", Kind: "phone", Room: "Hall"},
	}
	for _, d := range devices {
		if _, err := s.db.Exec(`INSERT INTO devices (id, name, kind, room) VALUES (?, ?, ?, ?)`, d.ID, d.Name, d.Kind, d.Room); err != nil {
			return err
		}
	}
	type seedPat struct {
		id, device, dest, category, protocol, evidence string
		observations, bytesOut, every, perLookup       int
	}
	pats := []seedPat{
		{"pat-metrics-us", "dev-kitchen-speaker", "device-metrics-us.amazon.com", "telemetry", "HTTPS", "84 small uploads today, about every 5 minutes", 84, 1008000, 300, 12000},
		{"pat-metrics-us-2", "dev-kitchen-speaker", "device-metrics-us-2.amazon.com", "telemetry", "HTTPS", "a steady trickle, a few kilobytes each time", 40, 160000, 300, 4000},
		{"pat-voice", "dev-kitchen-speaker", "avs-alexa-na.amazon.com", "voice-assistant cloud", "HTTPS", "used when someone talks to the speaker", 12, 576000, 0, 48000},
		{"pat-phone-ads", "dev-hall-phone", "app-measurement.com", "ads", "HTTPS", "the hall phone calling an ad measurement name", 20, 160000, 600, 8000},
	}
	for _, p := range pats {
		devName := "Kitchen speaker"
		if p.device == "dev-hall-phone" {
			devName = "Hall phone"
		}
		advice := model.Advise(model.Pattern{
			Destination: p.dest,
			DeviceName:  devName,
			Category:    p.category,
			Evidence:    p.evidence,
		}, nil)
		if _, err := s.db.Exec(`INSERT INTO patterns (id, device_id, destination, category, protocol, evidence, observations, bytes_out, every_seconds, per_lookup_bytes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.id, p.device, p.dest, advice.Category, p.protocol, p.evidence, p.observations, p.bytesOut, p.every, p.perLookup); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT INTO rules (id, pattern_id, action, status, source, sentence, consequence, suggestion, activity) VALUES (?, ?, ?, 'suggested', ?, ?, ?, ?, ?)`,
			"rule-"+p.id, p.id, advice.Suggestion, advice.Source, advice.Sentence, advice.Consequence, advice.Suggestion, advice.Activity); err != nil {
			return err
		}
	}
	return s.logLocked("Kitchen speaker", "device-metrics-us.amazon.com", "suggested", "The advisor opened a usage-report card. Nothing is dropped until you choose.")
}

// SpeakerOnly reports the review filter.
func (s *Store) SpeakerOnly() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v int
	err := s.db.QueryRow(`SELECT speaker_only FROM settings WHERE id = 1`).Scan(&v)
	return v == 1, err
}

// SetSpeakerOnly stores the speaker-first option.
func (s *Store) SetSpeakerOnly(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := 0
	if on {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE settings SET speaker_only = ? WHERE id = 1`, v)
	return err
}

// Mode is passive or active. Passive is the default and only collects.
func (s *Store) Mode() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var mode string
	err := s.db.QueryRow(`SELECT mode FROM settings WHERE id = 1`).Scan(&mode)
	if err != nil {
		return "", err
	}
	if mode != "active" {
		return "passive", nil
	}
	return "active", nil
}

// SetMode stores passive or active. Active applies stored Qwen verdicts.
// Passive withdraws verdicts the agent applied and leaves a person's choice alone.
func (s *Store) SetMode(mode string) error {
	if mode != "passive" && mode != "active" {
		return errors.New("mode must be passive or active")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`UPDATE settings SET mode = ? WHERE id = 1`, mode); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if mode == "passive" {
		if _, err := s.db.Exec(`UPDATE rules SET status = 'suggested', action = suggestion, source = 'model' WHERE source = 'qwen' AND status = 'approved'`); err != nil {
			return err
		}
		return s.logLocked("Ano", "", "mode", "Passive mode. Ano keeps collecting and does not enforce AI Inference.")
	}
	rows, err := s.db.Query(`SELECT pattern_id, llm_action FROM rules WHERE source != 'customer' AND status = 'suggested' AND llm_action IN ('drop', 'allow_direct')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type apply struct{ id, action string }
	var items []apply
	for rows.Next() {
		var item apply
		if err := rows.Scan(&item.id, &item.action); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		if err := s.enforceVerdictLocked(item.id, item.action, now); err != nil {
			return err
		}
	}
	return s.logLocked("Ano", "", "mode", "Active mode. AI Inference drop and allow choices are enforced.")
}

// Pending returns names whose collected counts the model has not judged yet.
func (s *Store) Pending(minNew int) ([]ReviewItem, error) {
	if minNew < 1 {
		minNew = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
SELECT p.id, p.device_id, d.name, d.kind, d.room, p.destination, p.category, p.evidence, p.observations, p.bytes_out
FROM patterns p
JOIN devices d ON d.id = p.device_id
JOIN rules r ON r.pattern_id = p.id
WHERE r.source != 'customer'
AND p.category NOT IN ('seen', 'stream')
AND (r.llm_at = '' OR p.observations >= r.llm_lookups + ?)
ORDER BY p.id`, minNew)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewItem
	for rows.Next() {
		var item ReviewItem
		if err := rows.Scan(&item.ID, &item.DeviceID, &item.DeviceName, &item.Kind, &item.Room, &item.Destination, &item.Category, &item.Evidence, &item.Lookups, &item.Bytes); err != nil {
			return nil, err
		}
		facts, err := s.dayFactsLocked(item.DeviceID, item.Destination, item.Category)
		if err != nil {
			return nil, err
		}
		item.Cadence = facts.cadence
		item.SendTime = facts.sendTime
		item.Similar = facts.similar
		item.Protocol = facts.protocol
		out = append(out, item)
	}
	return out, rows.Err()
}

// RecordVerdict stores Qwen's drop or allow. Enforcement happens only in active mode,
// and never over a choice a person already made.
func (s *Store) RecordVerdict(patternID, action, reason string, enforce bool) error {
	if action == "allow" {
		action = "allow_direct"
	}
	if action != "drop" && action != "allow_direct" {
		return fmt.Errorf("verdict must be drop or allow")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "No reason given."
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var source, sentence, deviceName, dest string
	var lookups int
	err := s.db.QueryRow(`
SELECT r.source, r.sentence, d.name, p.destination, p.observations
FROM rules r
JOIN patterns p ON p.id = r.pattern_id
JOIN devices d ON d.id = p.device_id
WHERE r.pattern_id = ?`, patternID).Scan(&source, &sentence, &deviceName, &dest, &lookups)
	if err == sql.ErrNoRows {
		return errors.New("card not found")
	}
	if err != nil {
		return err
	}
	if source == "customer" {
		return nil
	}
	for _, marker := range []string{" Qwen:", " AI Inference:"} {
		if i := strings.Index(sentence, marker); i >= 0 {
			sentence = sentence[:i]
		}
	}
	sentence = sentence + " AI Inference: " + reason
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE rules SET suggestion = ?, llm_action = ?, llm_reason = ?, llm_at = ?, llm_lookups = ?, sentence = ? WHERE pattern_id = ?`,
		action, action, reason, now, lookups, sentence, patternID); err != nil {
		return err
	}
	if !enforce {
		return s.logLocked(deviceName, dest, "qwen", "AI Inference suggested "+verb(action)+" "+dest+" while Ano is passive. Nothing is enforced.")
	}
	return s.enforceVerdictLocked(patternID, action, now)
}

func (s *Store) enforceVerdictLocked(patternID, action, now string) error {
	res, err := s.db.Exec(`UPDATE rules SET action = ?, status = 'approved', source = 'qwen', reviewed_at = ? WHERE pattern_id = ? AND source != 'customer'`, action, now, patternID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil
	}
	if action == "drop" {
		if _, err := s.db.Exec(`UPDATE patterns SET
			before_lookups = CASE WHEN before_lookups = 0 THEN observations ELSE before_lookups END,
			before_bytes = CASE WHEN before_bytes = 0 THEN bytes_out ELSE before_bytes END
			WHERE id = ?`, patternID); err != nil {
			return err
		}
	}
	card, err := s.cardByIDLocked(patternID)
	if err != nil {
		return err
	}
	return s.logLocked(card.DeviceName, card.Destination, action, "AI Inference "+verb(action)+" "+card.Destination+" and active mode enforced it.")
}

// ObservationCount is how many collection rows are stored.
func (s *Store) ObservationCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM observations`).Scan(&n)
	return n, err
}

// Collect records one more lookup for every device and stores the sample.
func (s *Store) Collect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
SELECT p.id, p.device_id, p.destination, p.protocol, r.status, r.action, p.observations, p.bytes_out, p.per_lookup_bytes
FROM patterns p
JOIN rules r ON r.pattern_id = p.id
WHERE p.category NOT IN ('seen', 'stream', 'outbound')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type tick struct {
		id, deviceID, dest, protocol, status, action string
		lookups, bytes, per                          int
	}
	var items []tick
	for rows.Next() {
		var item tick
		if err := rows.Scan(&item.id, &item.deviceID, &item.dest, &item.protocol, &item.status, &item.action, &item.lookups, &item.bytes, &item.per); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range items {
		if item.status == "approved" && item.action == "drop" {
			if _, err := s.db.Exec(`UPDATE patterns SET after_lookups = after_lookups + 1, after_bytes = after_bytes + per_lookup_bytes WHERE id = ?`, item.id); err != nil {
				return err
			}
		} else if _, err := s.db.Exec(`UPDATE patterns SET observations = observations + 1, bytes_out = bytes_out + per_lookup_bytes WHERE id = ?`, item.id); err != nil {
			return err
		}
		lookups := item.lookups + 1
		bytes := item.bytes + item.per
		if _, err := s.db.Exec(`INSERT INTO observations (at, device_id, destination, protocol, lookups, bytes) VALUES (?, ?, ?, ?, ?, ?)`, now, item.deviceID, item.dest, item.protocol, lookups, bytes); err != nil {
			return err
		}
	}
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	if _, err := s.db.Exec(`DELETE FROM observations WHERE at < ?`, cutoff); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM sends WHERE at < ?`, cutoff); err != nil {
		return err
	}
	return s.snapshotLocked(time.Now().UTC().Format(time.RFC3339))
}

type dayFacts struct {
	cadence  string
	sendTime string
	similar  string
	protocol string
}

// backfillSends writes one send per cadence across the past 24 hours so Qwen
// can see how often a name goes out and which hours it uses. A name with no
// fixed interval keeps a few clustered talk times instead of a timer.
func (s *Store) backfillSends() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT device_id, destination, category, protocol, every_seconds, per_lookup_bytes FROM patterns`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type pat struct {
		device, dest, category, protocol string
		every, per                       int
	}
	var items []pat
	for rows.Next() {
		var item pat
		if err := rows.Scan(&item.device, &item.dest, &item.category, &item.protocol, &item.every, &item.per); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-24 * time.Hour)
	for _, item := range items {
		if item.category == "seen" || item.category == "stream" || item.category == "outbound" {
			continue
		}
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sends WHERE device_id = ? AND destination = ?`, item.device, item.dest).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		protocol := item.protocol
		if protocol == "" {
			protocol = "HTTPS"
		}
		times := sendTimes(start, end, item.every)
		for _, at := range times {
			if _, err := s.db.Exec(`INSERT INTO sends (at, device_id, destination, category, protocol, bytes) VALUES (?, ?, ?, ?, ?, ?)`,
				at.UTC().Format(time.RFC3339), item.device, item.dest, item.category, protocol, item.per); err != nil {
				return err
			}
		}
	}
	return nil
}

// stampProtocols copies a pattern's protocol onto sends that were stored before protocol was collected.
func (s *Store) stampProtocols() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE sends SET protocol = (
		SELECT p.protocol FROM patterns p
		WHERE p.device_id = sends.device_id AND p.destination = sends.destination
	) WHERE protocol = ''`)
	return err
}

func sendTimes(start, end time.Time, everySeconds int) []time.Time {
	if everySeconds > 0 {
		step := time.Duration(everySeconds) * time.Second
		var out []time.Time
		for t := start; !t.After(end); t = t.Add(step) {
			out = append(out, t)
		}
		return out
	}
	// No fixed timer: a handful of clustered times, the way a person talks to a speaker.
	hours := []int{7, 8, 12, 18, 19, 21}
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		loc = time.UTC
	}
	var out []time.Time
	for _, hour := range hours {
		local := end.In(loc)
		at := time.Date(local.Year(), local.Month(), local.Day(), hour, 10, 0, 0, loc)
		if at.After(end) {
			at = at.Add(-24 * time.Hour)
		}
		if !at.Before(start) {
			out = append(out, at.UTC())
		}
	}
	return out
}

func (s *Store) dayFactsLocked(deviceID, destination, category string) (dayFacts, error) {
	mine, err := s.sendStatLocked(deviceID, destination)
	if err != nil {
		return dayFacts{}, err
	}
	rows, err := s.db.Query(`SELECT DISTINCT destination FROM sends WHERE device_id = ? AND category = ? AND destination != ? ORDER BY destination`, deviceID, category, destination)
	if err != nil {
		return dayFacts{}, err
	}
	defer rows.Close()
	var peers []string
	for rows.Next() {
		var dest string
		if err := rows.Scan(&dest); err != nil {
			return dayFacts{}, err
		}
		stat, err := s.sendStatLocked(deviceID, dest)
		if err != nil {
			return dayFacts{}, err
		}
		peers = append(peers, fmt.Sprintf("%s over %s, %s", dest, stat.protocol, stat.line))
	}
	similar := "No other name of this kind from this device in the past 24 hours."
	if len(peers) > 0 {
		similar = strings.Join(peers, "; ")
	}
	return dayFacts{
		cadence:  mine.cadence,
		sendTime: mine.when,
		similar:  similar,
		protocol: mine.protocol,
	}, nil
}

type sendStat struct {
	cadence  string
	when     string
	line     string
	protocol string
}

func (s *Store) sendStatLocked(deviceID, destination string) (sendStat, error) {
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	rows, err := s.db.Query(`SELECT at, protocol FROM sends WHERE device_id = ? AND destination = ? AND at >= ? ORDER BY at`, deviceID, destination, cutoff)
	if err != nil {
		return sendStat{}, err
	}
	defer rows.Close()
	var times []time.Time
	var protocols []string
	seenProtocol := map[string]bool{}
	for rows.Next() {
		var at, protocol string
		if err := rows.Scan(&at, &protocol); err != nil {
			return sendStat{}, err
		}
		parsed, err := time.Parse(time.RFC3339, at)
		if err != nil {
			continue
		}
		times = append(times, parsed)
		protocol = strings.TrimSpace(protocol)
		if protocol != "" && !seenProtocol[protocol] {
			seenProtocol[protocol] = true
			protocols = append(protocols, protocol)
		}
	}
	if err := rows.Err(); err != nil {
		return sendStat{}, err
	}
	protocol := strings.Join(protocols, ", ")
	if protocol == "" {
		protocol = "unknown"
	}
	if len(times) == 0 {
		return sendStat{
			cadence:  "No sends recorded in the past 24 hours.",
			when:     "No send time in the past 24 hours.",
			line:     "nothing in the past 24 hours",
			protocol: protocol,
		}, nil
	}
	gap := medianGap(times)
	when := sendHours(times)
	cadence := fmt.Sprintf("%d sends in the past 24 hours, about every %s", len(times), gap)
	return sendStat{
		cadence:  cadence,
		when:     when,
		protocol: protocol,
		line:     fmt.Sprintf("%d times, about every %s, %s", len(times), gap, when),
	}, nil
}

func medianGap(times []time.Time) string {
	if len(times) < 2 {
		return "a single send"
	}
	gaps := make([]time.Duration, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		gaps = append(gaps, times[i].Sub(times[i-1]))
	}
	for i := 0; i < len(gaps); i++ {
		for j := i + 1; j < len(gaps); j++ {
			if gaps[j] < gaps[i] {
				gaps[i], gaps[j] = gaps[j], gaps[i]
			}
		}
	}
	gap := gaps[len(gaps)/2]
	if gap < time.Second {
		return "under a second"
	}
	if gap%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(gap/time.Minute))
	}
	if gap > time.Minute {
		return fmt.Sprintf("%dm%ds", int(gap/time.Minute), int((gap%time.Minute)/time.Second))
	}
	return fmt.Sprintf("%ds", int(gap/time.Second))
}

func sendHours(times []time.Time) string {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		loc = time.UTC
	}
	seen := map[int]bool{}
	var hours []int
	for _, at := range times {
		h := at.In(loc).Hour()
		if !seen[h] {
			seen[h] = true
			hours = append(hours, h)
		}
	}
	for i := 0; i < len(hours); i++ {
		for j := i + 1; j < len(hours); j++ {
			if hours[j] < hours[i] {
				hours[i], hours[j] = hours[j], hours[i]
			}
		}
	}
	if len(hours) >= 20 {
		return fmt.Sprintf("sends in %d of 24 hours, throughout the day", len(hours))
	}
	labels := make([]string, len(hours))
	for i, h := range hours {
		labels[i] = fmt.Sprintf("%02d:00", h)
	}
	return "sends around " + strings.Join(labels, ", ") + " Pacific"
}

// LANNeighbor is a device seen on the home router.
type LANNeighbor struct {
	Name string
	Addr string
	MAC  string
	Kind string
	Room string
}

// AdminDevice is one device in the regulator, with the traffic Ano has collected.
type AdminDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Room      string `json:"room"`
	Addr      string `json:"addr"`
	MAC       string `json:"mac"`
	Names     int    `json:"names"`
	BytesLeft int    `json:"bytesLeft"`
	BytesKept int    `json:"bytesKept"`
	Protocols string `json:"protocols"`
	OnNetwork bool   `json:"onNetwork"`
	ViaAno    bool   `json:"viaAno"`
}

// AdminGroup is one kind of device on the home network.
type AdminGroup struct {
	Kind    string        `json:"kind"`
	Title   string        `json:"title"`
	Devices []AdminDevice `json:"devices"`
}

// AdminView is the traffic regulator: the router, grouped devices, and collected traffic.
type AdminView struct {
	RouterAddr  string       `json:"routerAddr"`
	RouterIface string       `json:"routerIface"`
	RouterAt    string       `json:"routerAt"`
	Connected   bool         `json:"connected"`
	Detail      string       `json:"detail"`
	Mode        string       `json:"mode"`
	Groups      []AdminGroup `json:"groups"`
	ViaAddr     string       `json:"viaAddr"`
	ViaPort     int          `json:"viaPort"`
	ProxyReady  bool         `json:"proxyReady"`
	DNSReady    bool         `json:"dnsReady"`
	DNSDetail   string       `json:"dnsDetail"`
	SteerOn     bool         `json:"steerOn"`
	SteerDetail string       `json:"steerDetail"`
}

// ConnectRouter records the home router and the devices seen on its link.
// Devices found on an earlier connect that are no longer on that link are removed.
func (s *Store) ConnectRouter(router, iface string, neighbors []LANNeighbor) error {
	return s.connectRouter(router, iface, neighbors, true)
}

// ObserveRouter adds devices found on the Wi-Fi and the router and leaves earlier devices in place.
func (s *Store) ObserveRouter(router, iface string, neighbors []LANNeighbor) error {
	return s.connectRouter(router, iface, neighbors, false)
}

func (s *Store) connectRouter(router, iface string, neighbors []LANNeighbor, prune bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE settings SET router_addr = ?, router_iface = ?, router_at = ? WHERE id = 1`, strings.TrimSpace(router), strings.TrimSpace(iface), now); err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT id, name, kind, room, mac FROM devices`)
	if err != nil {
		return err
	}
	type known struct{ id, name, kind, room string }
	byMAC := map[string]known{}
	var lanIDs []struct{ id, mac string }
	for rows.Next() {
		var id, name, kind, room, mac string
		if err := rows.Scan(&id, &name, &kind, &room, &mac); err != nil {
			rows.Close()
			return err
		}
		if canon, ok := canonicalMAC(mac); ok {
			byMAC[canon] = known{id, name, kind, room}
		}
		if strings.HasPrefix(id, "lan-") {
			lanIDs = append(lanIDs, struct{ id, mac string }{id, mac})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, n := range neighbors {
		mac, ok := canonicalMAC(n.MAC)
		n.Addr = strings.TrimSpace(n.Addr)
		if !ok || n.Addr == "" || n.Addr == router {
			continue
		}
		keep[mac] = true
		name := strings.TrimSpace(n.Name)
		if name == "" || name == "?" {
			name = "Device " + n.Addr
		}
		kind := n.Kind
		if kind == "" {
			kind = "unknown"
		}
		room := strings.TrimSpace(n.Room)
		if room == "" {
			room = "Home"
		}
		if cur, found := byMAC[mac]; found {
			if !strings.HasPrefix(name, "Device ") {
				cur.name = name
			}
			if (cur.kind == "unknown" || cur.kind == "computer") && kind != "unknown" && kind != "computer" {
				cur.kind = kind
			}
			if cur.room == "" || cur.room == "Home" || room != "Home" {
				cur.room = room
			}
			if _, err = s.db.Exec(`UPDATE devices SET name = ?, kind = ?, room = ?, addr = ?, mac = ? WHERE id = ?`, cur.name, cur.kind, cur.room, n.Addr, mac, cur.id); err != nil {
				return err
			}
			continue
		}
		id := "lan-" + strings.ReplaceAll(mac, ":", "")
		if _, err = s.db.Exec(`INSERT INTO devices (id, name, kind, room, addr, mac) VALUES (?, ?, ?, ?, ?, ?)`, id, name, kind, room, n.Addr, mac); err != nil {
			return err
		}
	}
	if prune {
		for _, row := range lanIDs {
			canon, ok := canonicalMAC(row.mac)
			if ok && keep[canon] {
				continue
			}
			if _, err = s.db.Exec(`DELETE FROM devices WHERE id = ?`, row.id); err != nil {
				return err
			}
		}
	}
	return nil
}

func canonicalMAC(mac string) (string, bool) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(mac)), ":")
	if len(parts) != 6 {
		return "", false
	}
	for i, part := range parts {
		if part == "" || len(part) > 2 {
			return "", false
		}
		if len(part) == 1 {
			parts[i] = "0" + part
		}
	}
	return strings.Join(parts, ":"), true
}

// AdminView groups every known device and the traffic collected for it.
func (s *Store) AdminView() (AdminView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var view AdminView
	var mode string
	if err := s.db.QueryRow(`SELECT router_addr, router_iface, router_at, mode FROM settings WHERE id = 1`).Scan(&view.RouterAddr, &view.RouterIface, &view.RouterAt, &mode); err != nil {
		return AdminView{}, err
	}
	if mode != "active" {
		mode = "passive"
	}
	view.Mode = mode
	view.Connected = view.RouterAddr != ""
	if view.Connected {
		if view.RouterIface == "Wi-Fi" {
			view.Detail = "Connected to the Wi-Fi router at " + view.RouterAddr + ". Devices on that Wi-Fi are grouped below, including any that also sit on the wired link. Traffic is what Ano has collected."
		} else {
			where := view.RouterAddr
			if view.RouterIface != "" {
				where += " on " + view.RouterIface
			}
			view.Detail = "Connected to the home router at " + where + ". Devices below are the ones on that link. Traffic is what Ano has collected for each device."
		}
	} else {
		view.Detail = "Ano is reading the Wi-Fi and the router."
	}
	traffic := map[string]AdminDevice{}
	rows, err := s.db.Query(`SELECT device_id, COUNT(*), COALESCE(SUM(bytes_out), 0), COALESCE(SUM(after_bytes), 0),
		COALESCE(GROUP_CONCAT(DISTINCT NULLIF(protocol, '')), '')
		FROM patterns GROUP BY device_id`)
	if err != nil {
		return AdminView{}, err
	}
	for rows.Next() {
		var id, protocols string
		var names, left, kept int
		if err := rows.Scan(&id, &names, &left, &kept, &protocols); err != nil {
			rows.Close()
			return AdminView{}, err
		}
		traffic[id] = AdminDevice{Names: names, BytesLeft: left, BytesKept: kept, Protocols: protocols}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return AdminView{}, err
	}
	devRows, err := s.db.Query(`SELECT id, name, kind, room, addr, mac, via_ano FROM devices ORDER BY name`)
	if err != nil {
		return AdminView{}, err
	}
	defer devRows.Close()
	grouped := map[string][]AdminDevice{}
	for devRows.Next() {
		var d AdminDevice
		var via int
		if err := devRows.Scan(&d.ID, &d.Name, &d.Kind, &d.Room, &d.Addr, &d.MAC, &via); err != nil {
			return AdminView{}, err
		}
		d.ViaAno = via == 1
		if t, ok := traffic[d.ID]; ok {
			d.Names = t.Names
			d.BytesLeft = t.BytesLeft
			d.BytesKept = t.BytesKept
			d.Protocols = t.Protocols
		}
		d.OnNetwork = d.Addr != ""
		grouped[d.Kind] = append(grouped[d.Kind], d)
	}
	if err := devRows.Err(); err != nil {
		return AdminView{}, err
	}
	order := []struct{ kind, title string }{
		{"speaker", "Speakers"},
		{"tv", "TVs"},
		{"camera", "Cameras"},
		{"garage", "Garage"},
		{"light", "Lights"},
		{"phone", "Phones"},
		{"printer", "Printers"},
		{"thermostat", "Thermostats"},
		{"computer", "Computers"},
		{"unknown", "Other devices"},
	}
	seen := map[string]bool{}
	view.Groups = []AdminGroup{}
	for _, item := range order {
		devices := grouped[item.kind]
		if len(devices) == 0 {
			continue
		}
		seen[item.kind] = true
		view.Groups = append(view.Groups, AdminGroup{Kind: item.kind, Title: item.title, Devices: devices})
	}
	for kind, devices := range grouped {
		if seen[kind] || len(devices) == 0 {
			continue
		}
		view.Groups = append(view.Groups, AdminGroup{Kind: kind, Title: kind, Devices: devices})
	}
	return view, nil
}

// FlowNote is bytes this computer exchanged with one device on one service.
type FlowNote struct {
	Addr     string
	Service  string
	Protocol string
	Delta    int
}

// NoteFlows records observed bytes against the device that owns the address.
func (s *Store) NoteFlows(notes []FlowNote) error {
	if len(notes) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, note := range notes {
		note.Addr = strings.TrimSpace(note.Addr)
		note.Service = strings.TrimSpace(note.Service)
		if note.Addr == "" || note.Service == "" || note.Delta < 0 {
			continue
		}
		var deviceID, deviceName string
		err := s.db.QueryRow(`SELECT id, name FROM devices WHERE addr = ?`, note.Addr).Scan(&deviceID, &deviceName)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		protocol := note.Protocol
		if protocol == "" {
			protocol = "TCP"
		}
		var patternID string
		var bytesOut, observations int
		err = s.db.QueryRow(`SELECT id, bytes_out, observations FROM patterns WHERE device_id = ? AND destination = ?`, deviceID, note.Service).Scan(&patternID, &bytesOut, &observations)
		if err == sql.ErrNoRows {
			patternID = "seen-" + strings.ReplaceAll(deviceID, ":", "") + "-" + strings.ToLower(strings.ReplaceAll(note.Service, " ", ""))
			evidence := "Bytes this computer exchanged with this device on " + note.Service + ". Ano is not in front of the device's other traffic."
			if _, err = s.db.Exec(`INSERT INTO patterns (id, device_id, destination, category, protocol, evidence, observations, bytes_out, every_seconds, per_lookup_bytes) VALUES (?, ?, ?, 'seen', ?, ?, 0, 0, 0, 0)`,
				patternID, deviceID, note.Service, protocol, evidence); err != nil {
				return err
			}
			sentence := evidence
			if _, err = s.db.Exec(`INSERT INTO rules (id, pattern_id, action, status, source, sentence, consequence, suggestion, activity) VALUES (?, ?, 'ignore', 'suggested', 'seen', ?, ?, '', ?)`,
				newID(), patternID, sentence, "Nothing is enforced. This is traffic this computer could see.", note.Service); err != nil {
				return err
			}
			bytesOut = 0
			observations = 0
		} else if err != nil {
			return err
		}
		if note.Delta == 0 && observations == 0 {
			continue
		}
		now := time.Now().UTC()
		if note.Delta > 0 {
			bytesOut += note.Delta
			observations++
			if _, err = s.db.Exec(`UPDATE patterns SET bytes_out = ?, observations = ?, protocol = ? WHERE id = ?`, bytesOut, observations, protocol, patternID); err != nil {
				return err
			}
			if _, err = s.db.Exec(`INSERT INTO observations (at, device_id, destination, protocol, lookups, bytes) VALUES (?, ?, ?, ?, ?, ?)`,
				now.Format(time.RFC3339Nano), deviceID, note.Service, protocol, observations, bytesOut); err != nil {
				return err
			}
			if observations == 1 {
				if _, err = s.db.Exec(`INSERT INTO samples (at, pattern_id, destination, device_name, left_bytes, kept_bytes, lookups, refused) VALUES (?, ?, ?, ?, 0, 0, 0, 0)`,
					now.Add(-2*time.Second).Format(time.RFC3339), patternID, note.Service, deviceName); err != nil {
					return err
				}
			}
		}
		if _, err = s.db.Exec(`INSERT INTO samples (at, pattern_id, destination, device_name, left_bytes, kept_bytes, lookups, refused) VALUES (?, ?, ?, ?, ?, 0, ?, 0)`,
			now.Format(time.RFC3339), patternID, note.Service, deviceName, bytesOut, observations); err != nil {
			return err
		}
	}
	return nil
}

// Devices lists household devices.
func (s *Store) Devices() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, name, kind, room, via_ano FROM devices ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var via int
		if err := rows.Scan(&d.ID, &d.Name, &d.Kind, &d.Room, &via); err != nil {
			return nil, err
		}
		d.ViaAno = via == 1
		out = append(out, d)
	}
	return out, rows.Err()
}

// Cards returns review cards. speakerOnly hides everything that is not a speaker.
func (s *Store) Cards(speakerOnly bool) ([]Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := `
SELECT ` + cardColumns + `
FROM patterns p
JOIN devices d ON d.id = p.device_id
JOIN rules r ON r.pattern_id = p.id`
	if speakerOnly {
		q += ` WHERE d.kind = 'speaker'`
	}
	q += ` ORDER BY CASE r.status WHEN 'suggested' THEN 0 ELSE 1 END, d.name, p.destination`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Decisions returns the newest log lines.
func (s *Store) Decisions(limit int) ([]Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT id, at, device_name, destination, outcome, detail FROM decisions ORDER BY at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Decision{}
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.ID, &d.At, &d.DeviceName, &d.Destination, &d.Outcome, &d.Detail); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// EnvoyRoutes returns approved drop and allow_envoy rules only.
func (s *Store) EnvoyRoutes() ([]EnvoyRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
SELECT p.destination, r.action, r.reroute_host
FROM rules r
JOIN patterns p ON p.id = r.pattern_id
WHERE r.status = 'approved' AND r.action IN ('drop', 'allow_envoy') AND p.category NOT IN ('seen', 'stream')
ORDER BY p.destination`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EnvoyRoute
	for rows.Next() {
		var r EnvoyRoute
		if err := rows.Scan(&r.Destination, &r.Action, &r.Reroute); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Decide records the household choice and makes it the live rule.
func (s *Store) Decide(patternID, action string) (Card, error) {
	var category string
	if err := s.db.QueryRow(`SELECT category FROM patterns WHERE id = ?`, patternID).Scan(&category); err == nil && category == "seen" {
		return Card{}, errors.New("this is traffic this computer could see, and it is not a name Ano can refuse")
	}
	if category == "stream" {
		return Card{}, errors.New("streaming stays on the direct path")
	}
	if action == "ignore" {
		s.mu.Lock()
		defer s.mu.Unlock()
		res, err := s.db.Exec(`UPDATE rules SET status = 'rejected', source = 'customer', reviewed_at = ?, reroute_host = '' WHERE pattern_id = ?`, time.Now().UTC().Format(time.RFC3339), patternID)
		if err != nil {
			return Card{}, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return Card{}, errors.New("card not found")
		}
		card, err := s.cardByIDLocked(patternID)
		if err != nil {
			return Card{}, err
		}
		if err := s.logLocked(card.DeviceName, card.Destination, "ignore", card.DeviceName+": set aside "+card.Destination+". Nothing is enforced."); err != nil {
			return Card{}, err
		}
		return card, nil
	}
	if action != "drop" && action != "allow_direct" && action != "allow_envoy" {
		return Card{}, fmt.Errorf("action must be drop, allow_direct, or allow_envoy")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE rules SET action = ?, status = 'approved', source = 'customer', reviewed_at = ?, reroute_host = CASE WHEN ? = 'allow_envoy' THEN reroute_host ELSE '' END WHERE pattern_id = ?`, action, time.Now().UTC().Format(time.RFC3339), action, patternID)
	if err != nil {
		return Card{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Card{}, errors.New("card not found")
	}
	if action == "drop" {
		if _, err := s.db.Exec(`UPDATE patterns SET
			before_lookups = CASE WHEN before_lookups = 0 THEN observations ELSE before_lookups END,
			before_bytes = CASE WHEN before_bytes = 0 THEN bytes_out ELSE before_bytes END
			WHERE id = ?`, patternID); err != nil {
			return Card{}, err
		}
	}
	card, err := s.cardByIDLocked(patternID)
	if err != nil {
		return Card{}, err
	}
	detail := card.DeviceName + ": " + verb(action) + " " + card.Destination + "."
	if err := s.logLocked(card.DeviceName, card.Destination, action, detail); err != nil {
		return Card{}, err
	}
	return card, nil
}

// SetReroute maps where Envoy sends a name that goes through the proxy.
// A hostname puts the name on Answer here and rewrites the upstream host.
// An empty hostname clears the map and leaves the current action in place.
func (s *Store) SetReroute(patternID, host string) (Card, error) {
	host = normalizeHost(host)
	if host != "" && !validHost(host) {
		return Card{}, errors.New("enter a hostname such as logs.example.net")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	var err error
	var res sql.Result
	if host == "" {
		res, err = s.db.Exec(`UPDATE rules SET reroute_host = '' WHERE pattern_id = ?`, patternID)
	} else {
		res, err = s.db.Exec(`UPDATE rules SET action = 'allow_envoy', status = 'approved', source = 'customer', reviewed_at = ?, reroute_host = ? WHERE pattern_id = ?`, now, host, patternID)
	}
	if err != nil {
		return Card{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Card{}, errors.New("card not found")
	}
	card, err := s.cardByIDLocked(patternID)
	if err != nil {
		return Card{}, err
	}
	detail := card.DeviceName + ": cleared the proxy re-route for " + card.Destination + "."
	outcome := "reroute_clear"
	if host != "" {
		outcome = "reroute"
		detail = card.DeviceName + ": re-routes " + card.Destination + " through Envoy to " + host + "."
	}
	if err := s.logLocked(card.DeviceName, card.Destination, outcome, detail); err != nil {
		return Card{}, err
	}
	return card, nil
}

// Simulate records a lookup. A name with no card gets a suggested card from the advisor and is not enforced.
func (s *Store) Simulate(deviceID, host string) (Card, string, error) {
	host = normalizeHost(host)
	if host == "" || !validHost(host) {
		return Card{}, "", errors.New("enter a hostname such as device-metrics-us.amazon.com")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var deviceName, kind string
	err := s.db.QueryRow(`SELECT name, kind FROM devices WHERE id = ?`, deviceID).Scan(&deviceName, &kind)
	if err == sql.ErrNoRows {
		return Card{}, "", errors.New("unknown device")
	}
	if err != nil {
		return Card{}, "", err
	}

	var patternID, status, action string
	err = s.db.QueryRow(`SELECT id FROM patterns WHERE device_id = ? AND destination = ?`, deviceID, host).Scan(&patternID)
	if err == sql.ErrNoRows {
		card, err := s.openCardLocked(deviceID, deviceName, host)
		if err != nil {
			return Card{}, "", err
		}
		if err := s.logLocked(deviceName, host, "suggested", "New name. The advisor wrote a card and did not enforce it."); err != nil {
			return Card{}, "", err
		}
		return card, "suggested", nil
	}
	if err != nil {
		return Card{}, "", err
	}
	if err := s.db.QueryRow(`SELECT status, action FROM rules WHERE pattern_id = ?`, patternID).Scan(&status, &action); err != nil {
		return Card{}, "", err
	}
	if status == "approved" && action == "drop" {
		_, _ = s.db.Exec(`UPDATE patterns SET after_lookups = after_lookups + 1, after_bytes = after_bytes + per_lookup_bytes WHERE id = ?`, patternID)
	} else {
		_, _ = s.db.Exec(`UPDATE patterns SET observations = observations + 1, bytes_out = bytes_out + per_lookup_bytes WHERE id = ?`, patternID)
	}
	outcome := status
	detail := "Still waiting for your review."
	if status == "approved" {
		outcome = action
		detail = "Live rule: " + verb(action) + "."
	}
	if status == "rejected" {
		detail = "You set this aside earlier. It is still not enforced."
	}
	card, err := s.cardByIDLocked(patternID)
	if err != nil {
		return Card{}, "", err
	}
	if err := s.logLocked(deviceName, host, outcome, detail); err != nil {
		return Card{}, "", err
	}
	return card, outcome, nil
}

func (s *Store) openCardLocked(deviceID, deviceName, host string) (Card, error) {
	category, _ := model.Classify(host)
	if isVoiceHost(host) {
		category = "voice-assistant cloud"
	}
	labels, err := s.labelsLocked()
	if err != nil {
		return Card{}, err
	}
	evidence := "just looked up, once"
	advice := model.Advise(model.Pattern{
		Destination: host,
		DeviceName:  deviceName,
		Category:    category,
		Evidence:    evidence,
	}, labels)
	pid := newID()
	if _, err := s.db.Exec(`INSERT INTO patterns (id, device_id, destination, category, protocol, evidence, observations, bytes_out, every_seconds, per_lookup_bytes) VALUES (?, ?, ?, ?, ?, ?, 1, 8000, 0, 8000)`,
		pid, deviceID, host, advice.Category, "HTTPS", evidence); err != nil {
		return Card{}, err
	}
	if _, err := s.db.Exec(`INSERT INTO rules (id, pattern_id, action, status, source, sentence, consequence, suggestion, activity) VALUES (?, ?, ?, 'suggested', ?, ?, ?, ?, ?)`,
		newID(), pid, advice.Suggestion, advice.Source, advice.Sentence, advice.Consequence, advice.Suggestion, advice.Activity); err != nil {
		return Card{}, err
	}
	return s.cardByIDLocked(pid)
}

func (s *Store) labelsLocked() ([]model.Label, error) {
	rows, err := s.db.Query(`
SELECT d.name, p.destination, p.category, r.action
FROM rules r
JOIN patterns p ON p.id = r.pattern_id
JOIN devices d ON d.id = p.device_id
WHERE r.status = 'approved'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Label
	for rows.Next() {
		var l model.Label
		if err := rows.Scan(&l.DeviceName, &l.Destination, &l.Category, &l.Action); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

const cardColumns = `p.id, p.device_id, d.name, d.room, d.kind, p.destination, p.category, p.protocol,
       r.activity, r.sentence, r.consequence, r.suggestion, r.status, r.action,
       p.evidence, r.source, r.reroute_host, r.llm_action, r.llm_reason, p.observations, p.bytes_out, p.every_seconds, p.per_lookup_bytes,
       p.before_lookups, p.before_bytes, p.after_lookups, p.after_bytes`

func (s *Store) cardByIDLocked(id string) (Card, error) {
	row := s.db.QueryRow(`
SELECT `+cardColumns+`
FROM patterns p
JOIN devices d ON d.id = p.device_id
JOIN rules r ON r.pattern_id = p.id
WHERE p.id = ?`, id)
	return scanCard(row)
}

// TickSpeaker records one more lookup for every speaker. A dropped name
// counts as refused. Anything still allowed counts as bytes that left.
func (s *Store) TickSpeaker() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
SELECT p.id, r.status, r.action
FROM patterns p
JOIN devices d ON d.id = p.device_id
JOIN rules r ON r.pattern_id = p.id
WHERE d.kind = 'speaker'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type tick struct{ id, status, action string }
	var items []tick
	for rows.Next() {
		var item tick
		if err := rows.Scan(&item.id, &item.status, &item.action); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		if item.status == "approved" && item.action == "drop" {
			if _, err := s.db.Exec(`UPDATE patterns SET after_lookups = after_lookups + 1, after_bytes = after_bytes + per_lookup_bytes WHERE id = ?`, item.id); err != nil {
				return err
			}
			continue
		}
		if _, err := s.db.Exec(`UPDATE patterns SET observations = observations + 1, bytes_out = bytes_out + per_lookup_bytes WHERE id = ?`, item.id); err != nil {
			return err
		}
	}
	return s.snapshotLocked(time.Now().UTC().Format(time.RFC3339))
}

// Series returns recent live samples, oldest first.
func (s *Store) Series() ([]Sample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT at, pattern_id, destination, device_name, left_bytes, kept_bytes, lookups, refused FROM samples ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sample{}
	for rows.Next() {
		var sample Sample
		if err := rows.Scan(&sample.At, &sample.PatternID, &sample.Destination, &sample.DeviceName, &sample.LeftBytes, &sample.KeptBytes, &sample.Lookups, &sample.Refused); err != nil {
			return nil, err
		}
		out = append(out, sample)
	}
	return out, rows.Err()
}

func (s *Store) backfillSeries() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM samples`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	now := time.Now().UTC()
	for i := 24; i >= 1; i-- {
		at := now.Add(-time.Duration(i) * 2 * time.Second).Format(time.RFC3339)
		if err := s.snapshotLockedAt(at, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) snapshotLocked(at string) error {
	return s.snapshotLockedAt(at, 0)
}

func (s *Store) snapshotLockedAt(at string, stepsAgo int) error {
	rows, err := s.db.Query(`
SELECT p.id, p.destination, d.name, p.observations, p.bytes_out, p.before_lookups, p.before_bytes,
       p.after_lookups, p.after_bytes, p.per_lookup_bytes, r.status, r.action
FROM patterns p
JOIN devices d ON d.id = p.device_id
JOIN rules r ON r.pattern_id = p.id
WHERE d.kind = 'speaker'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, dest, name, status, action string
		var observations, bytesOut, beforeLookups, beforeBytes, afterLookups, afterBytes, per int
		if err := rows.Scan(&id, &dest, &name, &observations, &bytesOut, &beforeLookups, &beforeBytes, &afterLookups, &afterBytes, &per, &status, &action); err != nil {
			return err
		}
		dropped := status == "approved" && action == "drop"
		left := bytesOut
		lookups := observations
		kept := 0
		refused := 0
		if dropped {
			left = beforeBytes
			lookups = beforeLookups
			kept = afterBytes - stepsAgo*per
			refused = afterLookups - stepsAgo
			if kept < 0 {
				kept = 0
			}
			if refused < 0 {
				refused = 0
			}
		} else {
			left = bytesOut - stepsAgo*per
			lookups = observations - stepsAgo
			if left < 0 {
				left = 0
			}
			if lookups < 0 {
				lookups = 0
			}
		}
		if _, err := s.db.Exec(`INSERT INTO samples (at, pattern_id, destination, device_name, left_bytes, kept_bytes, lookups, refused) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			at, id, dest, name, left, kept, lookups, refused); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM samples WHERE id NOT IN (SELECT id FROM samples ORDER BY id DESC LIMIT 2000)`)
	return err
}

func (s *Store) NoteBlocked(host string) error {
	host = normalizeHost(host)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
UPDATE patterns
SET after_lookups = after_lookups + 1,
    after_bytes = after_bytes + per_lookup_bytes
WHERE destination = ?
AND EXISTS (
  SELECT 1 FROM rules
  WHERE rules.pattern_id = patterns.id AND rules.status = 'approved' AND rules.action = 'drop'
)`, host)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCard(row scanner) (Card, error) {
	var c Card
	var bytesOut, every, per, beforeLookups, beforeBytes, afterLookups, afterBytes int
	err := row.Scan(&c.ID, &c.DeviceID, &c.DeviceName, &c.Room, &c.Kind, &c.Destination, &c.Category, &c.Protocol, &c.Activity, &c.Sentence, &c.Consequence, &c.Suggestion, &c.Status, &c.Action, &c.Evidence, &c.Source, &c.Reroute, &c.LlmAction, &c.LlmReason, &c.Observations, &bytesOut, &every, &per, &beforeLookups, &beforeBytes, &afterLookups, &afterBytes)
	if err != nil {
		return Card{}, err
	}
	dropped := c.Status == "approved" && c.Action == "drop"
	c.Metrics.EverySeconds = every
	c.Metrics.Dropped = dropped
	if dropped || beforeLookups > 0 {
		if beforeLookups == 0 {
			c.Metrics.BeforeLookups = c.Observations
			c.Metrics.BeforeBytes = bytesOut
		} else {
			c.Metrics.BeforeLookups = beforeLookups
			c.Metrics.BeforeBytes = beforeBytes
		}
		c.Metrics.AfterLookups = afterLookups
		c.Metrics.AfterBytes = afterBytes
	} else {
		c.Metrics.BeforeLookups = c.Observations
		c.Metrics.BeforeBytes = bytesOut
	}
	return c, nil
}

func (s *Store) logLocked(device, dest, outcome, detail string) error {
	_, err := s.db.Exec(`INSERT INTO decisions (id, at, device_name, destination, outcome, detail) VALUES (?, ?, ?, ?, ?, ?)`,
		newID(), time.Now().UTC().Format(time.RFC3339Nano), device, dest, outcome, detail)
	return err
}

func verb(action string) string {
	switch action {
	case "drop":
		return "Drop"
	case "allow_envoy":
		return "Allow through the proxy"
	case "allow_direct":
		return "Allow directly"
	default:
		return action
	}
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	return host
}

func validHost(host string) bool {
	if len(host) < 3 || len(host) > 253 || strings.Contains(host, "..") {
		return false
	}
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return strings.Contains(host, ".")
}

func isVoiceHost(host string) bool {
	return strings.Contains(host, "avs-alexa") || strings.Contains(host, "alexa.amazon")
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
