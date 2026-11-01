package via

import (
	"bufio"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbar/hbar/internal/store"
)

func TestProxyForwardsStreamingAndRefusesALeak(t *testing.T) {
	st := testStore(t)
	id := enroll(t, st, "127.0.0.1")
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		for {
			conn, err := backend.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 4)
				_, _ = io.ReadFull(conn, buf)
				_, _ = conn.Write([]byte("pong"))
			}(conn)
		}
	}()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		_ = (&Proxy{
			Store:       st,
			DialPrivate: true,
			Dial: func(network, address string) (net.Conn, error) {
				return net.Dial(network, backend.Addr().String())
			},
		}).Serve(ln)
	}()

	if err := exchange(ln.Addr().String(), "video.googlevideo.com:443"); err != nil {
		t.Fatal(err)
	}
	if err := exchange(ln.Addr().String(), "device-metrics-us.amazon.com:443"); err != nil {
		t.Fatal(err)
	}
	leak := waitCard(t, st, "device-metrics-us.amazon.com")
	stream := waitCard(t, st, "video.googlevideo.com")
	if leak.Category != "outbound" || stream.Category != "stream" {
		t.Fatalf("leak %s stream %s", leak.Category, stream.Category)
	}
	if _, err := st.Decide(leak.ID, "drop"); err != nil {
		t.Fatal(err)
	}
	status, err := connectStatus(ln.Addr().String(), "device-metrics-us.amazon.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, " 403 ") {
		t.Fatalf("status %s", status)
	}
	if _, _, enrolled, err := st.ViaDevice("127.0.0.1"); err != nil || !enrolled || id == "" {
		t.Fatal(enrolled, err, id)
	}
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func enroll(t *testing.T, st *store.Store, addr string) string {
	t.Helper()
	if err := st.ConnectRouter("192.168.86.1", "Wi-Fi", []store.LANNeighbor{{
		Name: "Maharajan's Fire TV", Addr: addr, MAC: "90:39:5f:ad:ac:4b", Kind: "tv",
	}}); err != nil {
		t.Fatal(err)
	}
	devices, err := st.Devices()
	if err != nil {
		t.Fatal(err)
	}
	for _, device := range devices {
		if device.Name == "Maharajan's Fire TV" {
			if err := st.SetViaAno(device.ID, true); err != nil {
				t.Fatal(err)
			}
			return device.ID
		}
	}
	t.Fatal("missing fire tv")
	return ""
}

func exchange(proxy, target string) error {
	conn, err := net.Dial("tcp", proxy)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.Contains(line, " 200 ") {
		return errStatus(line)
	}
	for {
		header, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		if header == "\r\n" || header == "\n" {
			break
		}
	}
	if _, err := io.WriteString(conn, "ping"); err != nil {
		return err
	}
	buf := make([]byte, 4)
	_, err = io.ReadFull(reader, buf)
	return err
}

func connectStatus(proxy, target string) (string, error) {
	conn, err := net.Dial("tcp", proxy)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil {
		return "", err
	}
	reader := bufio.NewReader(conn)
	return reader.ReadString('\n')
}

type statusError string

func (e statusError) Error() string { return string(e) }

func errStatus(line string) error { return statusError(line) }

func waitCard(t *testing.T, st *store.Store, host string) store.Card {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cards, err := st.Cards(false)
		if err != nil {
			t.Fatal(err)
		}
		for _, card := range cards {
			if card.Destination == host && card.Kind == "tv" && card.Observations > 0 {
				return card
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no card for %s", host)
	return store.Card{}
}
