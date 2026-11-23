package certname

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestTLSNameReadsTheCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tlsName(context.Background(), host, port, host)
	if err != nil {
		t.Fatal(err)
	}
	want := clean(srv.Certificate().Subject.CommonName, srv.Certificate().DNSNames)
	if got.Name == "" || got.Name != want || !got.SelfSigned || !got.Present {
		t.Fatalf("got %+v want %q", got, want)
	}
}

func TestHighPortReadsTheCertificateName(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	if port == 80 || port == 443 {
		t.Fatalf("test server used a web port %d", port)
	}
	got, err := Identify(context.Background(), host, port)
	if err != nil {
		t.Fatal(err)
	}
	want := clean(srv.Certificate().Subject.CommonName, srv.Certificate().DNSNames)
	if got.Name == "" || got.Name != want || !got.SelfSigned || !got.Present {
		t.Fatalf("got %+v want %q", got, want)
	}
}

func TestPlaceholderCertificateStaysSelfSigned(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "invalid2.invalid"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		DNSNames:              []string{"invalid2.invalid"},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	got := identityFromLeaf(leaf)
	if got.Name != "" || !got.SelfSigned || !got.Present {
		t.Fatalf("got %+v", got)
	}
}

func TestHandshakeWithNoCertificateIsRecorded(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	got, err := tlsName(context.Background(), "127.0.0.1", port, "")
	if err != nil || !got.NoCertificate || got.Present || got.Name != "" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestCleanSkipsAPlaceholderCertificate(t *testing.T) {
	if got := clean("invalid2.invalid", []string{"*.googleapis.com"}); got != "googleapis.com" {
		t.Fatalf("san name %q", got)
	}
	if got := clean("invalid2.invalid", nil); got != "" {
		t.Fatalf("placeholder stayed %q", got)
	}
}

func TestHTTPRedirectUsesTheCertificateName(t *testing.T) {
	secure := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer secure.Close()
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, secure.URL, http.StatusFound)
	}))
	defer plain.Close()
	got, err := nameFromPage(context.Background(), plain.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	want := clean(secure.Certificate().Subject.CommonName, secure.Certificate().DNSNames)
	if got.Name == "" || got.Name != want || !got.SelfSigned {
		t.Fatalf("got %+v want %q", got, want)
	}
}
