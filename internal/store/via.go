package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// ViaNote is one name a device sent through Ano.
type ViaNote struct {
	Addr     string
	Host     string
	Protocol string
	Category string
	Bytes    int
	Refused  bool
}

// SetViaAno adds or removes a device from the path through Ano.
func (s *Store) SetViaAno(id string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var name string
	err := s.db.QueryRow(`SELECT name FROM devices WHERE id = ?`, id).Scan(&name)
	if err == sql.ErrNoRows {
		return errors.New("device not found")
	}
	if err != nil {
		return err
	}
	v := 0
	if on {
		v = 1
	}
	if _, err := s.db.Exec(`UPDATE devices SET via_ano = ? WHERE id = ?`, v, id); err != nil {
		return err
	}
	if on {
		return s.logLocked(name, "", "via", name+" was added to Ano. Streaming stays on the direct path. Other names are monitored.")
	}
	return s.logLocked(name, "", "via", name+" was removed from Ano.")
}

// ViaDevice returns the device at addr when it was added to Ano.
func (s *Store) ViaDevice(addr string) (id, name string, enrolled bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var via int
	err = s.db.QueryRow(`SELECT id, name, via_ano FROM devices WHERE addr = ?`, strings.TrimSpace(addr)).Scan(&id, &name, &via)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return id, name, via == 1, nil
}

// ViaRule returns the live rule for one device and name, when a card exists.
func (s *Store) ViaRule(deviceID, host string) (status, action string, found bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.db.QueryRow(`SELECT r.status, r.action FROM rules r JOIN patterns p ON p.id = r.pattern_id WHERE p.device_id = ? AND p.destination = ?`, deviceID, strings.TrimSpace(host)).Scan(&status, &action)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return status, action, true, nil
}

// NoteVia records bytes for a name that arrived through Ano.
// Streaming is stored and left enforceable-off. Other names can take Refuse and Send.
func (s *Store) NoteVia(note ViaNote) error {
	note.Addr = strings.TrimSpace(note.Addr)
	note.Host = strings.ToLower(strings.TrimSpace(note.Host))
	note.Host = strings.TrimSuffix(note.Host, ".")
	if note.Addr == "" || note.Host == "" || note.Bytes < 0 {
		return nil
	}
	if note.Category != "stream" && note.Category != "outbound" {
		return nil
	}
	if note.Protocol == "" {
		note.Protocol = "HTTPS"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var deviceID, deviceName string
	var via int
	err := s.db.QueryRow(`SELECT id, name, via_ano FROM devices WHERE addr = ?`, note.Addr).Scan(&deviceID, &deviceName, &via)
	if err == sql.ErrNoRows || via != 1 {
		return nil
	}
	if err != nil {
		return err
	}
	evidence := "This name went through Ano and it is not streaming. Ano records it. Refuse holds it when that rule is on. Send lets it through."
	consequence := "Passive mode watches this name. Active mode applies Refuse and Send."
	action, source := "ignore", "model"
	if note.Category == "stream" {
		evidence = "Streaming through Ano. Ano forwards it on the direct path."
		consequence = "Video and music stay on the direct path."
		action, source = "allow_direct", "stream"
	}
	sum := sha256.Sum256([]byte(deviceID + "\n" + note.Host))
	patternID := "out-" + hex.EncodeToString(sum[:12])
	var observations, bytesOut, afterBytes, afterLookups int
	err = s.db.QueryRow(`SELECT observations, bytes_out, after_bytes, after_lookups FROM patterns WHERE device_id = ? AND destination = ?`, deviceID, note.Host).Scan(&observations, &bytesOut, &afterBytes, &afterLookups)
	if err == sql.ErrNoRows {
		if _, err = s.db.Exec(`INSERT INTO patterns (id, device_id, destination, category, protocol, evidence, observations, bytes_out, every_seconds, per_lookup_bytes) VALUES (?, ?, ?, ?, ?, ?, 0, 0, 0, 0)`,
			patternID, deviceID, note.Host, note.Category, note.Protocol, evidence); err != nil {
			return err
		}
		if _, err = s.db.Exec(`INSERT INTO rules (id, pattern_id, action, status, source, sentence, consequence, suggestion, activity) VALUES (?, ?, ?, 'suggested', ?, ?, ?, '', ?)`,
			newID(), patternID, action, source, evidence, consequence, note.Host); err != nil {
			return err
		}
		observations, bytesOut, afterBytes, afterLookups = 0, 0, 0, 0
	} else if err != nil {
		return err
	} else if err = s.db.QueryRow(`SELECT id FROM patterns WHERE device_id = ? AND destination = ?`, deviceID, note.Host).Scan(&patternID); err != nil {
		return err
	}
	observations++
	if note.Refused {
		afterBytes += note.Bytes
		afterLookups++
	} else {
		bytesOut += note.Bytes
	}
	if _, err = s.db.Exec(`UPDATE patterns SET observations = ?, bytes_out = ?, after_bytes = ?, after_lookups = ?, protocol = ? WHERE id = ?`,
		observations, bytesOut, afterBytes, afterLookups, note.Protocol, patternID); err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err = s.db.Exec(`INSERT INTO observations (at, device_id, destination, protocol, lookups, bytes) VALUES (?, ?, ?, ?, ?, ?)`,
		now.Format(time.RFC3339Nano), deviceID, note.Host, note.Protocol, observations, bytesOut); err != nil {
		return err
	}
	if _, err = s.db.Exec(`INSERT INTO sends (at, device_id, destination, category, protocol, bytes) VALUES (?, ?, ?, ?, ?, ?)`,
		now.Format(time.RFC3339), deviceID, note.Host, note.Category, note.Protocol, note.Bytes); err != nil {
		return err
	}
	if observations == 1 {
		if _, err = s.db.Exec(`INSERT INTO samples (at, pattern_id, destination, device_name, left_bytes, kept_bytes, lookups, refused) VALUES (?, ?, ?, ?, 0, 0, 0, 0)`,
			now.Add(-2*time.Second).Format(time.RFC3339), patternID, note.Host, deviceName); err != nil {
			return err
		}
	}
	refused := 0
	if note.Refused {
		refused = 1
	}
	_, err = s.db.Exec(`INSERT INTO samples (at, pattern_id, destination, device_name, left_bytes, kept_bytes, lookups, refused) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		now.Format(time.RFC3339), patternID, note.Host, deviceName, bytesOut, afterBytes, observations, refused)
	return err
}

// ViaTarget is one device a person added so its traffic can go through Ano.
type ViaTarget struct {
	ID   string
	Name string
	Kind string
	Addr string
	MAC  string
}

// ViaTargets lists devices added to Ano.
func (s *Store) ViaTargets() ([]ViaTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, name, kind, addr, mac FROM devices WHERE via_ano = 1 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ViaTarget
	for rows.Next() {
		var t ViaTarget
		if err := rows.Scan(&t.ID, &t.Name, &t.Kind, &t.Addr, &t.MAC); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
