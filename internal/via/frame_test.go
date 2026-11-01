package via

import (
	"crypto/tls"
	"net"
	"strings"
	"testing"
)

func TestParseSNIFromClientHello(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			got <- ""
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		host, _ := ParseSNI(buf[:n])
		got <- host
	}()
	client, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true, ServerName: "device-metrics-us.amazon.com"})
	if err == nil {
		_ = client.Close()
	}
	if host := <-got; host != "device-metrics-us.amazon.com" {
		t.Fatalf("sni %q", host)
	}
}

func TestHTTPHost(t *testing.T) {
	host, ok := HTTPHost([]byte("GET / HTTP/1.1\r\nHost: Example.com:80\r\n\r\n"))
	if !ok || host != "example.com" {
		t.Fatalf("%q %v", host, ok)
	}
}

func TestARPReplyClaimsTheGateway(t *testing.T) {
	frame, err := ARPReply("90:39:5f:ad:ac:4b", "7e:2b:e1:b0:43:7e", "7e:2b:e1:b0:43:7e", "192.168.86.1", "90:39:5f:ad:ac:4b", "192.168.86.57")
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != 42 || frame[12] != 0x08 || frame[13] != 0x06 || frame[21] != 2 {
		t.Fatalf("frame %x", frame)
	}
	if frame[28] != 192 || frame[31] != 1 {
		t.Fatalf("sender %v", frame[28:32])
	}
}

func TestRenderPFKeepsTheLANAndBringsInternetTCP(t *testing.T) {
	text, err := RenderPF("en1", "192.168.86.1", "192.168.86.0/23", "192.168.86.192", []string{"192.168.86.57"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "192.168.86.57") || !strings.Contains(text, "to ! 192.168.86.0/23") || !strings.Contains(text, "port 10082") || !strings.Contains(text, "to 192.168.86.1 keep state") {
		t.Fatalf("%s", text)
	}
	if _, err := RenderPF("en1;reboot", "192.168.86.1", "192.168.86.0/23", "192.168.86.192", nil); err == nil {
		t.Fatal("accepted a bad interface")
	}
}
