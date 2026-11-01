package via

import (
	"net"
	"strings"
)

// Normalize returns a lowercase host without a port. Names and IPv4 addresses are accepted.
func Normalize(host string) (string, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if h, p, err := net.SplitHostPort(host); err == nil && p != "" {
		host = strings.Trim(h, "[]")
	}
	host = strings.Trim(host, "[]")
	if host == "" || len(host) > 253 || strings.ContainsAny(host, " \r\n\t/\\") {
		return "", false
	}
	if net.ParseIP(host) != nil {
		return host, true
	}
	if strings.Contains(host, "..") {
		return "", false
	}
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return "", false
	}
	if !strings.Contains(host, ".") {
		return "", false
	}
	return host, true
}

// Streaming reports video and music delivery names. Other names stay available for regulation.
func Streaming(host string) bool {
	host, ok := Normalize(host)
	if !ok || net.ParseIP(host) != nil {
		return false
	}
	for _, suffix := range streamSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// streamSuffixes are delivery networks for picture and sound.
// Measurement and advertising names on the same vendors are left out on purpose.
var streamSuffixes = []string{
	"aiv-cdn.net",
	"aiv-delivery.net",
	"amazonvideo.com",
	"media-amazon.com",
	"primevideo.com",
	"nflxvideo.net",
	"nflximg.net",
	"nflxso.net",
	"netflix.com",
	"netflix.net",
	"googlevideo.com",
	"youtube.com",
	"ytimg.com",
	"ggpht.com",
	"hulu.com",
	"hulustream.com",
	"disneyplus.com",
	"bamgrid.com",
	"dssott.com",
	"akamaihd.net",
	"akamaized.net",
	"spotify.com",
	"scdn.co",
	"ttvnw.net",
	"twitch.tv",
}
