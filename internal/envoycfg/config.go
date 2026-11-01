// Package envoycfg writes the Envoy config Ano actually loads.
// Approved drops and proxy allows become routes. Direct allows stay out of
// Envoy. Suggested cards are not written here, so a model suggestion cannot
// change routing on its own.
package envoycfg

import (
	"fmt"
	"strings"
)

// Route is one approved decision Envoy should enforce.
type Route struct {
	Destination string
	Action      string // drop or allow_envoy
	Reroute     string // upstream host when Action is allow_envoy
}

// Options controls ports and the access log path.
type Options struct {
	ListenPort int
	AdminPort  int
	AccessLog  string
}

// Render returns an Envoy static config. Destinations must already be
// validated hostnames.
func Render(opts Options, routes []Route) string {
	if opts.ListenPort == 0 {
		opts.ListenPort = 10080
	}
	if opts.AdminPort == 0 {
		opts.AdminPort = 19001
	}
	var b strings.Builder
	fmt.Fprintf(&b, `static_resources:
  listeners:
  - name: hbar_speaker
    address:
      socket_address: { address: 0.0.0.0, port_value: %d }
    filter_chains:
    - filters:
      - name: envoy.filters.network.http_connection_manager
        typed_config:
          "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          stat_prefix: hbar
          codec_type: AUTO
          strip_matching_host_port: true
          access_log:
          - name: envoy.access_loggers.file
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.access_loggers.file.v3.FileAccessLog
              path: %s
          route_config:
            name: speaker_routes
            virtual_hosts:
`, opts.ListenPort, yamlString(opts.AccessLog))

	var clusters []Route
	for i, route := range routes {
		fmt.Fprintf(&b, `            - name: route_%d
              domains: [%s, %s]
              routes:
              - match: { prefix: "/" }
`, i, yamlString(route.Destination), yamlString(route.Destination+":*"))
		if route.Action == "allow_envoy" && route.Reroute != "" {
			clusters = append(clusters, route)
			fmt.Fprintf(&b, `                route:
                  cluster: reroute_%d
                  host_rewrite_literal: %s
                  timeout: 4s
                response_headers_to_add:
                - header: { key: x-hbar-action, value: allow_envoy }
                  append_action: OVERWRITE_IF_EXISTS_OR_ADD
                - header: { key: x-hbar-reroute, value: %s }
                  append_action: OVERWRITE_IF_EXISTS_OR_ADD
`, i, yamlString(route.Reroute), yamlString(route.Reroute))
			continue
		}
		status := 403
		body := "Ano dropped this speaker destination\n"
		header := "drop"
		if route.Action == "allow_envoy" {
			status = 200
			body = "Ano is the proxy for this speaker destination\n"
			header = "allow_envoy"
		}
		fmt.Fprintf(&b, `                direct_response:
                  status: %d
                  body:
                    inline_string: %s
                response_headers_to_add:
                - header: { key: x-hbar-action, value: %s }
                  append_action: OVERWRITE_IF_EXISTS_OR_ADD
`, status, yamlString(body), header)
	}

	b.WriteString(`            - name: unmatched
              domains: ["*"]
              routes:
              - match: { prefix: "/" }
                direct_response:
                  status: 404
                  body:
                    inline_string: "Ano is not proxying this host. Direct allows never enter Envoy.\n"
                response_headers_to_add:
                - header: { key: x-hbar-action, value: unmatched }
                  append_action: OVERWRITE_IF_EXISTS_OR_ADD
          http_filters:
          - name: envoy.filters.http.router
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
  clusters:
`)
	if len(clusters) == 0 {
		b.WriteString(`  - name: hbar_unused
    connect_timeout: 1s
    type: STATIC
    load_assignment:
      cluster_name: hbar_unused
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address: { address: 127.0.0.1, port_value: 9 }
`)
	}
	for i, route := range routes {
		if route.Action != "allow_envoy" || route.Reroute == "" {
			continue
		}
		fmt.Fprintf(&b, `  - name: reroute_%d
    connect_timeout: 2s
    type: LOGICAL_DNS
    dns_lookup_family: V4_ONLY
    load_assignment:
      cluster_name: reroute_%d
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address: { address: %s, port_value: 443 }
    transport_socket:
      name: envoy.transport_sockets.tls
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext
        sni: %s
`, i, i, yamlString(route.Reroute), yamlString(route.Reroute))
	}
	b.WriteString(`
admin:
  address:
    socket_address: { address: 127.0.0.1, port_value: `)
	fmt.Fprintf(&b, "%d }\n", opts.AdminPort)
	return b.String()
}

func yamlString(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, "") + `"`
}
