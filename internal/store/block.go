package store

import (
	"net"
	"sort"
	"strings"
)

// DropName is one DNS name Ano drops, on one port or on every port.
type DropName struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Port   int    `json:"port"`
	Source string `json:"source"`
}

// Blocks returns names added from the admin screen.
func (s *Store) Blocks() ([]DropName, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blocksLocked()
}

// DropNames returns every DNS name Ano is set to drop.
func (s *Store) DropNames() ([]DropName, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropNamesLocked()
}

// AddBlock lists a DNS name for drop. Port 0 means every port.
func (s *Store) AddBlock(name string, port int) ([]DropName, error) {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	if !blockName(name) {
		return nil, errBlockName
	}
	if port != 0 && (port < 1 || port > 65535) {
		return nil, errBlockPort
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO blocks (id, name, port) VALUES (?, ?, ?)`, newID(), name, port)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errBlockExists
		}
		return nil, err
	}
	return s.dropNamesLocked()
}

// DeleteBlock removes a name added from the admin screen.
func (s *Store) DeleteBlock(id string) ([]DropName, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM blocks WHERE id = ?`, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, errBlockMissing
	}
	return s.dropNamesLocked()
}

func (s *Store) blocksLocked() ([]DropName, error) {
	rows, err := s.db.Query(`SELECT id, name, port FROM blocks ORDER BY name, port`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DropName{}
	for rows.Next() {
		var item DropName
		if err := rows.Scan(&item.ID, &item.Name, &item.Port); err != nil {
			return nil, err
		}
		item.Source = "admin"
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) dropNamesLocked() ([]DropName, error) {
	out, err := s.blocksLocked()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT p.destination FROM rules r JOIN patterns p ON p.id = r.pattern_id WHERE r.status = 'approved' AND r.action = 'drop' AND p.category NOT IN ('seen', 'stream')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var dest string
		if err := rows.Scan(&dest); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(dest)
		if host, _, err := net.SplitHostPort(name); err == nil {
			name = host
		}
		name = strings.TrimSuffix(strings.ToLower(name), ".")
		if net.ParseIP(name) != nil || !blockName(name) || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, DropName{ID: "refuse:" + name, Name: name, Port: 0, Source: "refuse"})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Port < out[j].Port
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func blockName(name string) bool {
	if name == "" || len(name) > 253 || strings.Contains(name, "..") || !strings.Contains(name, ".") {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '-' {
			return false
		}
	}
	return true
}

var (
	errBlockName    = errString("enter a DNS name such as ads.example.com")
	errBlockPort    = errString("choose all ports, or one port from 1 to 65535")
	errBlockExists  = errString("that name is already listed for that port")
	errBlockMissing = errString("that name is not on the drop list")
)

type errString string

func (e errString) Error() string { return string(e) }
