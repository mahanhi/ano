package certname

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Identity is the certificate name and whether that certificate is self-signed.
// Present is false when the server completed the check and offered no certificate.
type Identity struct {
	Name          string
	SelfSigned    bool
	Present       bool
	NoCertificate bool
}

// Identify reads the certificate for one recorded address.
// Port 80 is read as HTTP. Every other port is a TLS handshake, including high ports such as 5223.
// A placeholder certificate is not a name. On port 443, Ano then reads the certificate for the host the server redirects to.
func Identify(ctx context.Context, ip string, port int) (Identity, error) {
	if net.ParseIP(ip) == nil || port < 1 || port > 65535 {
		return Identity{}, nil
	}
	if port != 80 && port != 443 {
		return tlsName(ctx, ip, strconv.Itoa(port), "")
	}
	if port == 443 {
		id, err := tlsName(ctx, ip, "443", "")
		if err == nil && (id.Name != "" || id.SelfSigned) {
			return id, nil
		}
		if err != nil || id.Present {
			page, pageErr := nameFromPage(ctx, "https://"+ip+"/", ip)
			if pageErr == nil && (page.Name != "" || page.SelfSigned || page.NoCertificate) {
				return page, nil
			}
			if err != nil {
				return Identity{}, err
			}
			return id, nil
		}
		return id, nil
	}
	return nameFromPage(ctx, "http://"+ip+"/", ip)
}

func tlsName(ctx context.Context, host, port, serverName string) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	raw, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return Identity{}, err
	}
	conn := tls.Client(raw, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         serverName,
	})
	defer conn.Close()
	if err := conn.HandshakeContext(ctx); err != nil {
		if handshakeGaveUp(err) {
			return Identity{}, err
		}
		return Identity{NoCertificate: true}, nil
	}
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return Identity{NoCertificate: true}, nil
	}
	return identityFromLeaf(state.PeerCertificates[0]), nil
}

func nameFromPage(ctx context.Context, rawURL, dialIP string) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("User-Agent", "Ano")
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return Identity{}, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		id := identityFromLeaf(resp.TLS.PeerCertificates[0])
		if id.Name != "" || id.SelfSigned {
			return id, nil
		}
		return Identity{Present: true}, nil
	}
	loc, err := resp.Location()
	if err != nil || loc == nil {
		return Identity{NoCertificate: true}, nil
	}
	return nameFromLocation(ctx, dialIP, loc)
}

func nameFromLocation(ctx context.Context, dialIP string, loc *url.URL) (Identity, error) {
	host := loc.Hostname()
	if host == "" {
		return Identity{NoCertificate: true}, nil
	}
	port := loc.Port()
	if port == "" {
		port = "443"
	}
	if oneName(host) != "" && dialIP != "" {
		if id, err := tlsName(ctx, dialIP, "443", host); err == nil && (id.Name != "" || id.SelfSigned) {
			return id, nil
		}
	}
	if loc.Scheme == "https" || port == "443" {
		return tlsName(ctx, host, port, host)
	}
	if oneName(host) == "" {
		return Identity{NoCertificate: true}, nil
	}
	return tlsName(ctx, host, "443", host)
}

// handshakeGaveUp is a timeout or a cancelled check. The server was not shown to lack a certificate.
func handshakeGaveUp(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func identityFromLeaf(leaf *x509.Certificate) Identity {
	return Identity{
		Name:       clean(leaf.Subject.CommonName, leaf.DNSNames),
		SelfSigned: selfSigned(leaf),
		Present:    true,
	}
}

// selfSigned is true when the certificate is signed by its own key.
// Some of these certificates are not marked as a certificate authority, so the stricter check refuses them.
func selfSigned(leaf *x509.Certificate) bool {
	if leaf.CheckSignatureFrom(leaf) == nil {
		return true
	}
	return bytes.Equal(leaf.RawSubject, leaf.RawIssuer) &&
		leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) == nil
}

func clean(common string, dnsNames []string) string {
	if name := oneName(common); name != "" {
		return name
	}
	for _, dns := range dnsNames {
		if name := oneName(dns); name != "" {
			return name
		}
	}
	return ""
}

func oneName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "*.")
	value = strings.ToLower(value)
	if value == "" || len(value) > 253 || strings.Contains(value, "..") || !strings.Contains(value, ".") {
		return ""
	}
	if placeholder(value) || net.ParseIP(value) != nil {
		return ""
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '-' {
			return ""
		}
	}
	return value
}

func placeholder(name string) bool {
	switch name {
	case "invalid", "invalid2.invalid", "localhost", "localhost.localdomain":
		return true
	default:
		return strings.HasSuffix(name, ".invalid") || strings.Contains(name, "no sni")
	}
}
