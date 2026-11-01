package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecideParsesADrop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"action\":\"drop\",\"reason\":\"Usage reports are not needed.\"}"}}]}`))
	}))
	defer srv.Close()
	client := Client{Settings: Settings{URL: srv.URL, Model: "qwen2.5:0.5b"}, HTTP: srv.Client()}
	got, err := client.Decide(context.Background(), Brief{DeviceName: "Kitchen speaker", Destination: "device-metrics-us.amazon.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != "drop" || got.Reason != "Usage reports are not needed." {
		t.Fatalf("%+v", got)
	}
}

func TestParseVerdictAcceptsAllowAlias(t *testing.T) {
	got, err := parseVerdict("```json\n{\"action\":\"allow\",\"reason\":\"Spoken answers need this name.\",\"cadence\":\"every 5m\",\"sendTime\":\"evening\",\"similar\":\"unlike the metrics names\",\"protocol\":\"HTTPS\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != "allow_direct" || got.Cadence != "every 5m" || got.Protocol != "HTTPS" {
		t.Fatalf("%+v", got)
	}
}
