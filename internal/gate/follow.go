package gate

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ano/ano/internal/store"
)

// AccessNote is one finished Envoy connection.
type AccessNote struct {
	Src      string
	Dst      string
	Port     int
	Bytes    int
	Protocol string
}

// ParseAccessLine reads one Envoy access-log line.
// Lines that are not an internet connection are skipped.
func ParseAccessLine(line string) (AccessNote, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if (len(fields) != 4 && len(fields) != 5) || fields[1] == "-" {
		return AccessNote{}, false
	}
	rawProtocol := "TCP"
	if len(fields) == 5 {
		rawProtocol = fields[4]
	}
	srcHost, _, err := net.SplitHostPort(fields[0])
	if err != nil {
		return AccessNote{}, false
	}
	dstHost, dstPort, err := net.SplitHostPort(fields[1])
	if err != nil {
		return AccessNote{}, false
	}
	src := net.ParseIP(srcHost)
	dst := net.ParseIP(dstHost)
	port, err2 := strconv.Atoi(dstPort)
	in, err3 := strconv.Atoi(fields[2])
	out, err4 := strconv.Atoi(fields[3])
	if err2 != nil || err3 != nil || err4 != nil || src == nil || dst == nil || src.To4() == nil || dst.To4() == nil || port < 1 || port > 65535 {
		return AccessNote{}, false
	}
	if dst.IsPrivate() || dst.IsLoopback() || in+out <= 0 {
		return AccessNote{}, false
	}
	return AccessNote{Src: src.String(), Dst: dst.String(), Port: port, Protocol: endpointProtocol(rawProtocol, port), Bytes: in + out}, true
}

func endpointProtocol(raw string, port int) string {
	protocol := strings.ToUpper(strings.TrimSpace(raw))
	if protocol == "" || strings.HasPrefix(protocol, "HTTP") {
		protocol = "TCP"
	}
	switch {
	case port == 443 && protocol == "TCP":
		return "HTTPS"
	case port == 443 && protocol == "UDP":
		return "QUIC"
	case (port == 53 || port == 853) && (protocol == "TCP" || protocol == "UDP"):
		return "DNS"
	default:
		return protocol
	}
}

// Follow reads Envoy's access log and writes the database off the traffic path.
func Follow(st *store.Store, path string) {
	notes := make(chan store.ForwardNote, 2048)
	go writeNotes(st, notes)
	readLog(path, notes)
}

func writeNotes(st *store.Store, notes <-chan store.ForwardNote) {
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	batch := make([]store.ForwardNote, 0, 64)
	flush := func() {
		for _, note := range batch {
			_ = st.NoteForward(note)
		}
		batch = batch[:0]
	}
	for {
		select {
		case note, ok := <-notes:
			if !ok {
				flush()
				return
			}
			batch = append(batch, note)
			if len(batch) >= 64 {
				flush()
			}
		case <-tick.C:
			if len(batch) > 0 {
				flush()
			}
		}
	}
}

func readLog(path string, notes chan<- store.ForwardNote) {
	offset := fileSize(path)
	for {
		offset = readNew(path, offset, notes)
		time.Sleep(time.Second)
	}
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func readNew(path string, offset int64, notes chan<- store.ForwardNote) int64 {
	file, err := os.Open(path)
	if err != nil {
		return offset
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return offset
	}
	if info.Size() < offset {
		offset = 0
	}
	if _, err = file.Seek(offset, 0); err != nil {
		return offset
	}
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return offset
		}
		offset += int64(len(line))
		note, ok := ParseAccessLine(line)
		if !ok {
			continue
		}
		notes <- store.ForwardNote{Addr: note.Src, Remote: note.Dst, Port: note.Port, Protocol: note.Protocol, Bytes: note.Bytes}
	}
}
