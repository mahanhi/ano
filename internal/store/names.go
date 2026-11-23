package store

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
)

// WebEndpoint is one recorded HTTP or HTTPS address that still has no certificate name.
type WebEndpoint struct {
	Destination string
	Port        int
}

// UnnamedWeb returns addresses whose certificate has not been classified yet.
// That is web ports plus every other TCP port, including high ports. UDP is not a certificate check.
func (s *Store) UnnamedWeb(limit int) ([]WebEndpoint, error) {
	if limit < 1 {
		limit = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
SELECT DISTINCT p.destination, p.port
FROM patterns p
JOIN rules r ON r.pattern_id = p.id
WHERE r.cert_kind = ''
  AND (p.port IN (80, 443) OR lower(p.protocol) != 'udp')
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebEndpoint
	for rows.Next() {
		var item WebEndpoint
		if err := rows.Scan(&item.Destination, &item.Port); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// TagActivity sets the certificate name and whether that certificate is self-signed.
func (s *Store) TagActivity(destination, name string, selfSigned bool) error {
	destination = strings.TrimSpace(destination)
	name = strings.TrimSpace(name)
	if destination == "" || name == destination {
		return nil
	}
	kind := "signed"
	if selfSigned {
		kind = "self-signed"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`
UPDATE rules
SET cert_kind = ?
WHERE pattern_id IN (SELECT id FROM patterns WHERE destination = ?)`, kind, destination); err != nil {
		return err
	}
	if MailDestination(destination) {
		_, err := s.db.Exec(`
UPDATE rules SET activity = 'Mail'
WHERE pattern_id IN (SELECT id FROM patterns WHERE destination = ?)`, destination)
		return err
	}
	if name == "" {
		return nil
	}
	_, err := s.db.Exec(`
UPDATE rules
SET activity = ?
WHERE id IN (
  SELECT r.id
  FROM rules r
  JOIN patterns p ON p.id = r.pattern_id
  WHERE p.destination = ? AND (r.activity = '' OR r.activity = p.destination OR r.activity IN ('invalid2.invalid', 'invalid', 'localhost'))
)`, name, destination)
	return err
}

// DecideName stores a drop or allow for an endpoint name and applies it to every current address with that name.
func (s *Store) DecideName(name, action string) (int, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !nameRuleKey(name) {
		return 0, errors.New("choose a name, not an address")
	}
	if action != "drop" && action != "allow_direct" {
		return 0, errors.New("action must be drop or allow")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`
INSERT INTO name_rules (name, action, status, source, reviewed_at) VALUES (?, ?, 'approved', 'customer', ?)
ON CONFLICT(name) DO UPDATE SET action = excluded.action, status = 'approved', source = 'customer', reviewed_at = excluded.reviewed_at`, name, action, now); err != nil {
		return 0, err
	}
	n, err := s.applyNameLocked(name, action, now)
	if err != nil {
		return n, err
	}
	verb := "allow"
	if action == "drop" {
		verb = "drop"
	}
	if err := s.logLocked("Ano", name, action, "Ano will "+verb+" every address named "+name+"."); err != nil {
		return n, err
	}
	return n, nil
}

// ApplyNameRules copies a stored name choice onto addresses that received that name later.
func (s *Store) ApplyNameRules() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT name, action FROM name_rules WHERE status = 'approved' AND action IN ('drop', 'allow_direct')`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type rule struct{ name, action string }
	var rules []rule
	for rows.Next() {
		var item rule
		if err := rows.Scan(&item.name, &item.action); err != nil {
			return 0, err
		}
		rules = append(rules, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	total := 0
	for _, item := range rules {
		n, err := s.applyNameLocked(item.name, item.action, now)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (s *Store) applyNameLocked(name, action, now string) (int, error) {
	rows, err := s.db.Query(`
SELECT p.id
FROM patterns p
JOIN rules r ON r.pattern_id = p.id
WHERE lower(r.activity) = ? AND p.category != 'stream'
  AND NOT (r.status = 'approved' AND r.action = ? AND r.source = 'customer')`, name, action)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err := s.db.Exec(`UPDATE rules SET action = ?, status = 'approved', source = 'customer', suggestion = ?, reviewed_at = ?, reroute_host = '' WHERE pattern_id = ?`, action, action, now, id); err != nil {
			return 0, err
		}
		if action == "drop" {
			if _, err := s.db.Exec(`UPDATE patterns SET
				before_lookups = CASE WHEN before_lookups = 0 THEN observations ELSE before_lookups END,
				before_bytes = CASE WHEN before_bytes = 0 THEN bytes_out ELSE before_bytes END
				WHERE id = ?`, id); err != nil {
				return 0, err
			}
		}
	}
	return len(ids), nil
}

func nameRuleKey(name string) bool {
	if name == "mail" {
		return true
	}
	if name == "" || strings.Contains(name, ":") || strings.Contains(name, " ") || net.ParseIP(name) != nil {
		return false
	}
	return strings.Contains(name, ".")
}

// NameMail gives every port 993 address the name Mail, so they group as one endpoint.
func (s *Store) NameMail() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`
UPDATE rules
SET activity = 'Mail'
WHERE activity != 'Mail'
  AND pattern_id IN (SELECT id FROM patterns WHERE port = 993)`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// TagNoCertificate records that this address completed a check and presented no certificate.
func (s *Store) TagNoCertificate(destination string) error {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
UPDATE rules
SET cert_kind = 'none'
WHERE pattern_id IN (SELECT id FROM patterns WHERE destination = ?) AND cert_kind IN ('', 'none')`, destination)
	return err
}

// BareCertificate is one self-signed or certificateless name that can be dropped.
type BareCertificate struct {
	ID         string
	Kind       string
	Suggestion string
}

// BareCertificates returns self-signed and no-certificate names that are not already dropped.
// A person's choice is left alone. Streaming stays off this list.
func (s *Store) BareCertificates() ([]BareCertificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
SELECT p.id, r.cert_kind, r.suggestion
FROM patterns p
JOIN rules r ON r.pattern_id = p.id
WHERE r.cert_kind IN ('self-signed', 'none')
  AND p.category != 'stream'
  AND p.port NOT IN (993, 995, 465, 587)
  AND r.source != 'customer'
  AND NOT (r.status = 'approved' AND r.action = 'drop')
ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BareCertificate
	for rows.Next() {
		var item BareCertificate
		if err := rows.Scan(&item.ID, &item.Kind, &item.Suggestion); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// MailDestination is an IMAP, POP, or SMTP port. A probe that omits the mail name
// sees a placeholder certificate, which is not the certificate the mail client uses.
func MailDestination(destination string) bool {
	_, port, err := net.SplitHostPort(strings.TrimSpace(destination))
	if err != nil {
		return false
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	switch n {
	case 993, 995, 465, 587:
		return true
	default:
		return false
	}
}

// ReleaseMailDrops withdraws an automatic drop on a mail port.
// A person's allow or drop is left as they set it.
func (s *Store) ReleaseMailDrops() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`
UPDATE rules
SET action = 'ignore', status = 'suggested', suggestion = '', llm_action = '',
    llm_reason = 'Mail stays direct. A probe without the mail name is not a reason to drop it.'
WHERE source != 'customer'
  AND status = 'approved'
  AND action = 'drop'
  AND pattern_id IN (
    SELECT id FROM patterns WHERE port IN (993, 995, 465, 587) AND category != 'stream'
  )`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// CertificateKind is the stored certificate class for one pattern: signed, self-signed, none, or empty.
func (s *Store) CertificateKind(patternID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var kind string
	err := s.db.QueryRow(`SELECT cert_kind FROM rules WHERE pattern_id = ?`, patternID).Scan(&kind)
	return kind, err
}

// ClearPlaceholderNames removes certificate names that are not a site name.
func (s *Store) ClearPlaceholderNames() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
UPDATE rules
SET activity = (
  SELECT p.destination FROM patterns p WHERE p.id = rules.pattern_id
)
WHERE activity IN ('invalid2.invalid', 'invalid', 'localhost')`)
	return err
}
