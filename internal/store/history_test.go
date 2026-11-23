package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryBucketsBytesThatLeft(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConnectRouter("192.168.86.1", "Wi-Fi", []LANNeighbor{{
		Name: "Maharajans-Mini", Addr: "192.168.11.133", MAC: "18:4a:53:04:e7:8d", Kind: "computer",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.11.133", Remote: "1.0.0.1", Port: 443, Protocol: "HTTPS", Bytes: 2000}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.11.133", Remote: "1.0.0.1", Port: 443, Protocol: "HTTPS", Bytes: 500}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.History("1.0.0.1:443", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].LeftBytes != 2500 || rows[0].Lookups != 2 || rows[0].KeptBytes != 0 {
		t.Fatalf("history %+v", rows)
	}
	if _, err := s.db.Exec(`DELETE FROM history`); err != nil {
		t.Fatal(err)
	}
	if err := s.backfillHistory(); err != nil {
		t.Fatal(err)
	}
	rows, err = s.History("1.0.0.1:443", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].LeftBytes != 2500 || rows[0].Lookups != 2 {
		t.Fatalf("backfill %+v", rows)
	}
	if err := s.backfillHistory(); err != nil {
		t.Fatal(err)
	}
	rows, err = s.History("1.0.0.1:443", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].LeftBytes != 2500 {
		t.Fatalf("second backfill doubled the hour: %+v", rows)
	}
}

func TestHistoryNamesFiltersByTime(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConnectRouter("192.168.86.1", "Wi-Fi", []LANNeighbor{{
		Name: "Maharajans-Mini", Addr: "192.168.11.133", MAC: "18:4a:53:04:e7:8d", Kind: "computer",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.11.133", Remote: "1.0.0.1", Port: 443, Protocol: "HTTPS", Bytes: 2500}); err != nil {
		t.Fatal(err)
	}
	var patternID, dest, name string
	if err := s.db.QueryRow(`SELECT pattern_id, destination, device_name FROM history`).Scan(&patternID, &dest, &name); err != nil {
		t.Fatal(err)
	}
	older := time.Now().UTC().Add(-5 * time.Hour).Truncate(time.Hour)
	if _, err := s.db.Exec(`INSERT INTO history (pattern_id, bucket, destination, device_name, bytes, kept, sends, refused) VALUES (?, ?, ?, ?, 80, 0, 1, 0)`, patternID, older.Format(time.RFC3339), dest, name); err != nil {
		t.Fatal(err)
	}
	all, err := s.HistoryNames("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].LeftBytes != 2580 || all[0].PatternID != patternID {
		t.Fatalf("all names %+v", all)
	}
	rows, err := s.HistoryNames("", older.Add(time.Hour).Format(time.RFC3339), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].LeftBytes != 2500 {
		t.Fatalf("from filter %+v", rows)
	}
	rows, err = s.HistoryNames("", "", older.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].LeftBytes != 80 || rows[0].At != older.Format(time.RFC3339) {
		t.Fatalf("to filter %+v", rows)
	}
}

func TestHistoryBackfillSkipsTheBaselineOfAPrunedWindow(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConnectRouter("192.168.86.1", "Wi-Fi", []LANNeighbor{{
		Name: "Maharajans-Mini", Addr: "192.168.11.133", MAC: "18:4a:53:04:e7:8d", Kind: "computer",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.11.133", Remote: "1.0.0.1", Port: 443, Protocol: "HTTPS", Bytes: 100}); err != nil {
		t.Fatal(err)
	}
	var deviceID, patternID string
	if err := s.db.QueryRow(`SELECT device_id, id FROM patterns WHERE destination = ?`, "1.0.0.1:443").Scan(&deviceID, &patternID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM history`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM observations`); err != nil {
		t.Fatal(err)
	}
	older := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	newer := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`INSERT INTO observations (at, device_id, destination, protocol, lookups, bytes) VALUES (?, ?, '1.0.0.1:443', 'HTTPS', 8, 9000)`, older, deviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO observations (at, device_id, destination, protocol, lookups, bytes) VALUES (?, ?, '1.0.0.1:443', 'HTTPS', 9, 9500)`, newer, deviceID); err != nil {
		t.Fatal(err)
	}
	if err := s.backfillHistory(); err != nil {
		t.Fatal(err)
	}
	rows, err := s.History("", patternID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].LeftBytes != 500 || rows[0].Lookups != 1 {
		t.Fatalf("pruned window %+v", rows)
	}
}
