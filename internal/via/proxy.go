package via

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hbar/hbar/internal/store"
)

// Proxy accepts HTTP proxy traffic from devices added in the UI.
type Proxy struct {
	Store       *store.Store
	Dial        func(network, address string) (net.Conn, error)
	DialPrivate bool
}

// Serve handles one listener until it closes.
func (p *Proxy) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go p.serve(conn)
	}
}

func (p *Proxy) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	method, target, ok := requestLine(line)
	if !ok {
		writeStatus(conn, 400, "Ano could not read that request")
		return
	}
	if _, err := readHeaderBlock(reader); err != nil {
		writeStatus(conn, 400, "Ano could not read that request")
		return
	}
	host, port, ok := hostPort(method, target)
	if !ok {
		writeStatus(conn, 400, "Ano could not read that name")
		return
	}
	name, ok := Normalize(host)
	if !ok {
		writeStatus(conn, 400, "Ano could not read that name")
		return
	}
	decision, err := Decide(p.Store, clientIP(conn.RemoteAddr()), name)
	if err != nil || !decision.Enrolled {
		writeStatus(conn, 403, "This device is not added to Ano")
		return
	}
	protocol := "HTTP"
	if port == "443" || method == "CONNECT" {
		protocol = "HTTPS"
	}
	if !decision.Forward {
		body := "Ano refused this name. Streaming stays on the direct path."
		if decision.Answer {
			body = "Ano answered this name here."
		}
		writeStatus(conn, 403, body)
		_ = p.Store.NoteVia(store.ViaNote{Addr: clientIP(conn.RemoteAddr()), Host: name, Protocol: protocol, Category: decision.Category, Bytes: len(body), Refused: true})
		return
	}
	if blockedTarget(name, p.DialPrivate) {
		writeStatus(conn, 403, "Ano refused this name. Streaming stays on the direct path.")
		return
	}
	dial := p.Dial
	if dial == nil {
		dial = func(network, address string) (net.Conn, error) {
			return net.DialTimeout(network, address, 8*time.Second)
		}
	}
	up, err := dial("tcp", net.JoinHostPort(name, port))
	if err != nil {
		writeStatus(conn, 502, "Ano could not reach this name")
		return
	}
	defer up.Close()
	_ = conn.SetDeadline(time.Time{})
	if method == "CONNECT" {
		if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
	} else {
		if _, err := fmt.Fprintf(up, "%s %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", method, originPath(target), name); err != nil {
			return
		}
	}
	var moved int64
	var mu sync.Mutex
	add := func(n int64) {
		if n > 0 {
			mu.Lock()
			moved += n
			mu.Unlock()
		}
	}
	done := make(chan struct{}, 2)
	go func() {
		n, _ := io.Copy(up, reader)
		add(n)
		_ = up.Close()
		done <- struct{}{}
	}()
	go func() {
		n, _ := io.Copy(conn, up)
		add(n)
		_ = conn.Close()
		done <- struct{}{}
	}()
	<-done
	<-done
	_ = p.Store.NoteVia(store.ViaNote{Addr: clientIP(conn.RemoteAddr()), Host: name, Protocol: protocol, Category: decision.Category, Bytes: int(moved)})
}

func requestLine(line string) (method, target string, ok bool) {
	line = strings.TrimRight(line, "\r\n")
	fields := strings.Split(line, " ")
	if len(fields) < 2 {
		return "", "", false
	}
	method = strings.ToUpper(fields[0])
	switch method {
	case "CONNECT", "GET", "POST", "HEAD", "PUT", "DELETE", "OPTIONS":
		return method, fields[1], true
	default:
		return "", "", false
	}
}

func readHeaderBlock(r *bufio.Reader) ([]string, error) {
	var lines []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trim := strings.TrimRight(line, "\r\n")
		if trim == "" {
			return lines, nil
		}
		lines = append(lines, trim)
		if len(lines) > 64 {
			return nil, io.ErrUnexpectedEOF
		}
	}
}

func hostPort(method, target string) (string, string, bool) {
	if method == "CONNECT" {
		host, port, err := net.SplitHostPort(target)
		if err != nil {
			return "", "", false
		}
		if _, err := strconv.Atoi(port); err != nil {
			return "", "", false
		}
		return host, port, true
	}
	if strings.HasPrefix(strings.ToLower(target), "http://") {
		rest := target[len("http://"):]
		slash := strings.IndexByte(rest, '/')
		authority := rest
		if slash >= 0 {
			authority = rest[:slash]
		}
		if h, p, err := net.SplitHostPort(authority); err == nil {
			return h, p, true
		}
		if authority == "" {
			return "", "", false
		}
		return authority, "80", true
	}
	return "", "", false
}

func originPath(target string) string {
	if strings.HasPrefix(strings.ToLower(target), "http://") {
		rest := target[len("http://"):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			return rest[i:]
		}
	}
	return "/"
}

func writeStatus(conn net.Conn, status int, body string) {
	reason := "Ano"
	if status == 200 {
		reason = "OK"
	}
	fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nContent-Type: text/plain\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, reason, len(body), body)
}
