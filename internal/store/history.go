package store

import (
	"strings"
	"time"
)

const historyKeep = 14 * 24 * time.Hour

// recordHistoryLocked adds bytes that left, or bytes a drop kept, into the hour bucket for one name.
// The caller holds s.mu.
func (s *Store) recordHistoryLocked(patternID, destination, deviceName string, at time.Time, left, kept, sends, refused int) error {
	if patternID == "" || (left == 0 && kept == 0 && sends == 0 && refused == 0) {
		return nil
	}
	if left < 0 {
		left = 0
	}
	if kept < 0 {
		kept = 0
	}
	bucket := at.UTC().Truncate(time.Hour).Format(time.RFC3339)
	if _, err := s.db.Exec(`
INSERT INTO history (pattern_id, destination, device_name, bucket, bytes, kept, sends, refused)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(pattern_id, bucket) DO UPDATE SET
  bytes = history.bytes + excluded.bytes,
  kept = history.kept + excluded.kept,
  sends = history.sends + excluded.sends,
  refused = history.refused + excluded.refused,
  destination = excluded.destination,
  device_name = excluded.device_name`,
		patternID, destination, deviceName, bucket, left, kept, sends, refused); err != nil {
		return err
	}
	cutoff := time.Now().UTC().Add(-historyKeep).Format(time.RFC3339)
	_, err := s.db.Exec(`DELETE FROM history WHERE bucket < ?`, cutoff)
	return err
}

// History returns hourly bytes that left for one destination, one pattern, or one device.
// LeftBytes is what went out during that hour. KeptBytes is what a drop held back.
func (s *Store) History(destination, patternID, deviceID string) ([]Sample, error) {
	if destination == "" && patternID == "" && deviceID == "" {
		return []Sample{}, nil
	}
	since := time.Now().UTC().Add(-historyKeep).Format(time.RFC3339)
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
SELECT h.bucket, h.pattern_id, h.destination, h.device_name, h.bytes, h.kept, h.sends, h.refused
FROM history h
JOIN patterns p ON p.id = h.pattern_id
WHERE h.bucket >= ?
  AND (? = '' OR h.destination = ?)
  AND (? = '' OR h.pattern_id = ?)
  AND (? = '' OR p.device_id = ?)
ORDER BY h.bucket ASC, h.destination`,
		since, destination, destination, patternID, patternID, deviceID, deviceID)
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

// HistoryNames returns one row per name. LeftBytes is what left between from and to.
// At is the latest hour in that window. Empty bounds mean every stored hour.
func (s *Store) HistoryNames(deviceID, from, to string) ([]Sample, error) {
	since := time.Now().UTC().Add(-historyKeep).Format(time.RFC3339)
	start, startOK := historyBound(from)
	if startOK && start > since {
		since = start
	}
	end, endOK := historyBound(to)
	until := ""
	if endOK {
		until = end
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
SELECT MAX(h.bucket), h.pattern_id, h.destination, MAX(h.device_name), SUM(h.bytes), SUM(h.kept), SUM(h.sends), SUM(h.refused)
FROM history h
JOIN patterns p ON p.id = h.pattern_id
WHERE h.bucket >= ?
  AND (? = '' OR p.device_id = ?)
  AND (? = '' OR h.bucket <= ?)
GROUP BY h.pattern_id, h.destination
ORDER BY MAX(h.bucket) DESC, SUM(h.bytes) DESC`,
		since, deviceID, deviceID, until, until)
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

func historyBound(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return "", false
		}
	}
	return parsed.UTC().Format(time.RFC3339), true
}

// backfillHistory turns the stored observations into hour buckets once.
// The first observation of a name counts only when it is the first send. Later rows count the increase, so a pruned window does not dump the whole total into one hour.
func (s *Store) backfillHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM history`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	rows, err := s.db.Query(`
SELECT o.at, o.device_id, o.destination, o.lookups, o.bytes, p.id, d.name
FROM observations o
JOIN patterns p ON p.device_id = o.device_id AND p.destination = o.destination
JOIN devices d ON d.id = o.device_id
ORDER BY o.device_id, o.destination, o.at, o.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type key struct{ device, dest string }
	type bucketKey struct{ pattern, bucket string }
	type total struct {
		dest, name         string
		bytes, kept, sends int
	}
	prev := map[key]int{}
	seen := map[key]bool{}
	totals := map[bucketKey]*total{}
	var order []bucketKey
	for rows.Next() {
		var at, device, dest, pattern, name string
		var lookups, bytes int
		if err := rows.Scan(&at, &device, &dest, &lookups, &bytes, &pattern, &name); err != nil {
			return err
		}
		when, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			when, err = time.Parse(time.RFC3339, at)
			if err != nil {
				continue
			}
		}
		k := key{device, dest}
		delta := 0
		if !seen[k] {
			seen[k] = true
			if lookups <= 1 {
				delta = bytes
			}
		} else {
			delta = bytes - prev[k]
			if delta < 0 {
				delta = bytes
			}
		}
		prev[k] = bytes
		if delta <= 0 {
			continue
		}
		bk := bucketKey{pattern, when.UTC().Truncate(time.Hour).Format(time.RFC3339)}
		item := totals[bk]
		if item == nil {
			item = &total{dest: dest, name: name}
			totals[bk] = item
			order = append(order, bk)
		}
		item.bytes += delta
		item.sends++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, bk := range order {
		item := totals[bk]
		when, err := time.Parse(time.RFC3339, bk.bucket)
		if err != nil {
			return err
		}
		if err := s.recordHistoryLocked(bk.pattern, item.dest, item.name, when, item.bytes, item.kept, item.sends, 0); err != nil {
			return err
		}
	}
	return nil
}
