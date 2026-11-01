package via

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDNSRecordsALeakAndForwardsStreaming(t *testing.T) {
	st := testStore(t)
	enroll(t, st, "127.0.0.1")
	up, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := up.ReadFrom(buf)
			if err != nil {
				return
			}
			reply := append([]byte(nil), buf[:n]...)
			binary.BigEndian.PutUint16(reply[2:4], 0x8180)
			_, _ = up.WriteTo(reply, addr)
		}
	}()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	_, port, _ := net.SplitHostPort(up.LocalAddr().String())
	go func() {
		_ = ServeDNS(pc, st, "127.0.0.1:"+port, false)
	}()

	reply := ask(t, pc.LocalAddr().String(), "device-metrics-us.amazon.com")
	if binary.BigEndian.Uint16(reply[2:4])&0x000F != 0 {
		t.Fatalf("flags %x", reply[2:4])
	}
	ask(t, pc.LocalAddr().String(), "video.googlevideo.com")
	leak := waitCard(t, st, "device-metrics-us.amazon.com")
	stream := waitCard(t, st, "video.googlevideo.com")
	if leak.Category != "outbound" || leak.Protocol != "DNS" || stream.Category != "stream" {
		t.Fatalf("leak %+v stream %+v", leak.Category, stream.Category)
	}
	if _, err := st.Decide(leak.ID, "drop"); err != nil {
		t.Fatal(err)
	}
	refused := ask(t, pc.LocalAddr().String(), "device-metrics-us.amazon.com")
	if binary.BigEndian.Uint16(refused[2:4])&0x000F != 3 {
		t.Fatalf("rcode %x", refused[2:4])
	}
}

func ask(t *testing.T, addr, host string) []byte {
	t.Helper()
	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(dnsQuery(host)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n < 12 || !strings.Contains(string(buf[:n]), strings.Split(host, ".")[0]) {
		t.Fatalf("short reply %d", n)
	}
	return append([]byte(nil), buf[:n]...)
}

func dnsQuery(host string) []byte {
	msg := []byte{0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	for _, label := range strings.Split(host, ".") {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	return append(msg, 0, 0, 1, 0, 1)
}
