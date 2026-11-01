package via

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/hbar/hbar/internal/store"
)

// ServeDNS answers queries from devices added in the UI.
// Streaming names are resolved and returned. Other names are recorded.
// A Refuse rule answers that the name does not exist. prefixed packets start
// with 4 address bytes and 2 port bytes, which the privileged relay adds so
// Ano still knows which device asked.
func ServeDNS(pc net.PacketConn, st *store.Store, upstream string, prefixed bool) error {
	buf := make([]byte, 4096)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return err
		}
		packet := append([]byte(nil), buf[:n]...)
		go func(packet []byte, addr net.Addr) {
			client := clientIP(addr)
			payload := packet
			var prefix []byte
			if prefixed {
				if len(packet) < 7 {
					return
				}
				prefix = append([]byte(nil), packet[:6]...)
				ip := net.IP(packet[:4])
				client = ip.To4().String()
				payload = packet[6:]
			}
			reply := answerDNS(st, client, payload, upstream)
			if len(reply) == 0 {
				return
			}
			if prefixed {
				reply = append(prefix, reply...)
			}
			_, _ = pc.WriteTo(reply, addr)
		}(packet, addr)
	}
}

func answerDNS(st *store.Store, client string, query []byte, upstream string) []byte {
	host, ok := questionName(query)
	if !ok {
		return nil
	}
	name, ok := Normalize(host)
	if !ok {
		return forwardDNS(query, upstream)
	}
	decision, err := Decide(st, client, name)
	if err != nil || !decision.Enrolled {
		return forwardDNS(query, upstream)
	}
	if !decision.Forward {
		reply := nxDomain(query)
		_ = st.NoteVia(store.ViaNote{Addr: client, Host: name, Protocol: "DNS", Category: decision.Category, Bytes: len(query), Refused: true})
		return reply
	}
	reply := forwardDNS(query, upstream)
	if len(reply) == 0 {
		reply = servfail(query)
	}
	_ = st.NoteVia(store.ViaNote{Addr: client, Host: name, Protocol: "DNS", Category: decision.Category, Bytes: len(query) + len(reply)})
	return reply
}

func questionName(msg []byte) (string, bool) {
	if len(msg) < 12 {
		return "", false
	}
	if binary.BigEndian.Uint16(msg[4:6]) < 1 {
		return "", false
	}
	off := 12
	var labels []string
	for {
		if off >= len(msg) {
			return "", false
		}
		n := int(msg[off])
		if n == 0 {
			break
		}
		if n&0xC0 != 0 {
			return "", false
		}
		off++
		if off+n > len(msg) {
			return "", false
		}
		labels = append(labels, string(msg[off:off+n]))
		off += n
	}
	if len(labels) == 0 {
		return "", false
	}
	name := ""
	for i, label := range labels {
		if i > 0 {
			name += "."
		}
		name += label
	}
	return name, true
}

func nxDomain(query []byte) []byte {
	return dnsReply(query, 0x8183)
}

func servfail(query []byte) []byte {
	return dnsReply(query, 0x8182)
}

func dnsReply(query []byte, flags uint16) []byte {
	end, ok := questionEnd(query)
	if !ok {
		return nil
	}
	out := append([]byte(nil), query[:end]...)
	binary.BigEndian.PutUint16(out[2:4], flags)
	binary.BigEndian.PutUint16(out[6:8], 0)
	binary.BigEndian.PutUint16(out[8:10], 0)
	binary.BigEndian.PutUint16(out[10:12], 0)
	return out
}

func questionEnd(msg []byte) (int, bool) {
	if len(msg) < 12 {
		return 0, false
	}
	off := 12
	for {
		if off >= len(msg) {
			return 0, false
		}
		n := int(msg[off])
		if n == 0 {
			off++
			break
		}
		if n&0xC0 != 0 {
			off += 2
			break
		}
		off += 1 + n
	}
	if off+4 > len(msg) {
		return 0, false
	}
	return off + 4, true
}

func forwardDNS(query []byte, upstream string) []byte {
	if upstream == "" {
		return nil
	}
	target := upstream
	if _, _, err := net.SplitHostPort(upstream); err != nil {
		target = net.JoinHostPort(upstream, "53")
	}
	conn, err := net.DialTimeout("udp", target, 2*time.Second)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(query); err != nil {
		return nil
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil || n < 12 {
		return nil
	}
	return append([]byte(nil), buf[:n]...)
}
