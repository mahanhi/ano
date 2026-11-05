package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// ForwardNote is one internet conversation Ano forwarded for a home device.
type ForwardNote struct {
	Addr     string
	Remote   string
	Port     int
	Protocol string
	Bytes    int
}

// NoteForward records bytes Ano forwarded from a known device to an internet address.
func (s *Store) NoteForward(note ForwardNote) error {
	note.Addr = strings.TrimSpace(note.Addr)
	note.Remote = strings.TrimSpace(note.Remote)
	if note.Addr == "" || net.ParseIP(note.Remote) == nil || note.Bytes <= 0 {
		return nil
	}
	if note.Port < 1 || note.Port > 65535 {
		return nil
	}
	protocol := note.Protocol
	if protocol == "" {
		protocol = "TCP"
	}
	dest := net.JoinHostPort(note.Remote, strconv.Itoa(note.Port))
	s.mu.Lock()
	defer s.mu.Unlock()
	var deviceID, deviceName string
	err := s.db.QueryRow(`SELECT id, name FROM devices WHERE addr = ?`, note.Addr).Scan(&deviceID, &deviceName)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	evidence := fmt.Sprintf("Ano forwarded this from %s to %s.", deviceName, dest)
	sum := sha256.Sum256([]byte(deviceID + "\n" + dest))
	patternID := "fwd-" + hex.EncodeToString(sum[:12])
	var observations, bytesOut int
	err = s.db.QueryRow(`SELECT observations, bytes_out FROM patterns WHERE device_id = ? AND destination = ?`, deviceID, dest).Scan(&observations, &bytesOut)
	if err == sql.ErrNoRows {
		if _, err = s.db.Exec(`INSERT INTO patterns (id, device_id, destination, category, protocol, port, evidence, observations, bytes_out, every_seconds, per_lookup_bytes) VALUES (?, ?, ?, 'seen', ?, ?, ?, 0, 0, 0, 0)`,
			patternID, deviceID, dest, protocol, note.Port, evidence); err != nil {
			return err
		}
		if _, err = s.db.Exec(`INSERT INTO rules (id, pattern_id, action, status, source, sentence, consequence, suggestion, activity) VALUES (?, ?, 'ignore', 'suggested', 'gate', ?, ?, '', ?)`,
			newID(), patternID, evidence, "Ano recorded the forward. The packet already went to the modem.", dest); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if err = s.db.QueryRow(`SELECT id FROM patterns WHERE device_id = ? AND destination = ?`, deviceID, dest).Scan(&patternID); err != nil {
		return err
	}
	observations++
	bytesOut += note.Bytes
	if _, err = s.db.Exec(`UPDATE patterns SET observations = ?, bytes_out = ?, protocol = ?, port = ? WHERE id = ?`,
		observations, bytesOut, protocol, note.Port, patternID); err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err = s.db.Exec(`INSERT INTO observations (at, device_id, destination, protocol, lookups, bytes) VALUES (?, ?, ?, ?, ?, ?)`,
		now.Format(time.RFC3339Nano), deviceID, dest, protocol, observations, bytesOut); err != nil {
		return err
	}
	if observations == 1 {
		if _, err = s.db.Exec(`INSERT INTO samples (at, pattern_id, destination, device_name, left_bytes, kept_bytes, lookups, refused) VALUES (?, ?, ?, ?, 0, 0, 0, 0)`,
			now.Add(-2*time.Second).Format(time.RFC3339), patternID, dest, deviceName); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(`INSERT INTO samples (at, pattern_id, destination, device_name, left_bytes, kept_bytes, lookups, refused) VALUES (?, ?, ?, ?, ?, 0, ?, 0)`,
		now.Format(time.RFC3339), patternID, dest, deviceName, bytesOut, observations)
	return err
}
