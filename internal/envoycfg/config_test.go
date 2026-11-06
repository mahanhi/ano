package envoycfg

import "strings"
import "testing"

func TestRenderOmitsDirectAllowsAndSuggested(t *testing.T) {
	cfg := Render(Options{ListenPort: 10080, AdminPort: 19001, AccessLog: "/tmp/ano-access.log"}, []Route{
		{Destination: "device-metrics-us.amazon.com", Action: "drop"},
	})
	if !strings.Contains(cfg, "device-metrics-us.amazon.com") {
		t.Fatal("missing dropped host")
	}
	if !strings.Contains(cfg, "status: 403") {
		t.Fatal("missing drop status")
	}
	if strings.Contains(cfg, "avs-alexa-na.amazon.com") {
		t.Fatal("direct allow leaked into envoy")
	}
}

func TestRenderProxyRoute(t *testing.T) {
	cfg := Render(Options{AccessLog: "/tmp/ano-access.log"}, []Route{
		{Destination: "example-speaker.test", Action: "allow_envoy"},
	})
	if !strings.Contains(cfg, "status: 200") || !strings.Contains(cfg, "allow_envoy") {
		t.Fatal(cfg)
	}
	if strings.Contains(cfg, "host_rewrite_literal") {
		t.Fatal("an unmapped proxy route should answer locally")
	}
}

func TestRenderEgressDoesNotCallAno(t *testing.T) {
	cfg := Render(Options{AccessLog: "/tmp/ano-access.log", Egress: true, DropIPs: []string{"3.3.3.3"}}, nil)
	if !strings.Contains(cfg, "ano_egress") || !strings.Contains(cfg, "ano_udp") || !strings.Contains(cfg, "ORIGINAL_DST") || !strings.Contains(cfg, "port_value: 10082") || !strings.Contains(cfg, "port_value: 10083") || !strings.Contains(cfg, "3.3.3.3") {
		t.Fatal(cfg)
	}
	if strings.Contains(cfg, "ext_authz") || strings.Contains(cfg, "ext_proc") {
		t.Fatal(cfg)
	}
}

func TestRenderProxyReroute(t *testing.T) {
	cfg := Render(Options{AccessLog: "/tmp/ano-access.log"}, []Route{
		{Destination: "device-metrics-us.amazon.com", Action: "allow_envoy", Reroute: "logs.example.net"},
	})
	if !strings.Contains(cfg, "host_rewrite_literal: \"logs.example.net\"") {
		t.Fatal(cfg)
	}
	if !strings.Contains(cfg, "cluster: reroute_0") || !strings.Contains(cfg, "sni: \"logs.example.net\"") {
		t.Fatal(cfg)
	}
	if strings.Contains(cfg, "status: 200") {
		t.Fatal("a mapped route should not use the local 200")
	}
}
