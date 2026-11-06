package store

import (
	"path/filepath"
	"testing"
)

func TestAdminDropListKeepsNamesAndPorts(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.AddBlock("Ads.Example.com", 0); err != nil {
		t.Fatal(err)
	}
	got, err := s.AddBlock("metrics.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "ads.example.com" || got[0].Port != 0 || got[1].Port != 443 {
		t.Fatalf("%+v", got)
	}
	if _, err := s.AddBlock("ads.example.com", 0); err == nil {
		t.Fatal("a repeated name was accepted")
	}
	if _, err := s.AddBlock("not a name", 0); err == nil {
		t.Fatal("a bad name was accepted")
	}
	left, err := s.DeleteBlock(got[1].ID)
	if err != nil || len(left) != 1 || left[0].Name != "ads.example.com" {
		t.Fatalf("%+v %v", left, err)
	}
}
