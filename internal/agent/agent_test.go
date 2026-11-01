package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hbar/hbar/internal/llm"
	"github.com/hbar/hbar/internal/store"
)

func TestPassiveReviewDoesNotLoadEnvoy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen2.5:0.5b"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"message":{"content":"{\"action\":\"drop\",\"reason\":\"Usage reports can stop.\"}"}}`))
	}))
	defer srv.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "hbar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.LoadExample(); err != nil {
		t.Fatal(err)
	}
	reloaded := 0
	ag := &Agent{
		Store: st,
		Model: llm.Client{Settings: llm.Settings{Kind: "ollama", URL: srv.URL, Model: "qwen2.5:0.5b"}, HTTP: srv.Client()},
		OnEnforce: func() error {
			reloaded++
			return nil
		},
	}
	n, err := ag.Review(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("reviewed %d", n)
	}
	routes, err := st.EnvoyRoutes()
	if err != nil || len(routes) != 0 || reloaded != 0 {
		t.Fatalf("passive routes %d reloads %d err %v", len(routes), reloaded, err)
	}
	if err := ag.SetMode("active"); err != nil {
		t.Fatal(err)
	}
	routes, err = st.EnvoyRoutes()
	if err != nil || len(routes) != 4 || reloaded != 1 {
		t.Fatalf("active routes %d reloads %d err %v", len(routes), reloaded, err)
	}
}
