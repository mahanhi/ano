package via

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const natlookIOCTL = 0xc0544417

type natlook struct {
	saddr, daddr, rsaddr, rdaddr  [16]byte
	sx, dx, rsx, rdx              [4]byte
	af, proto, variant, direction uint8
}

// RunSteer takes the Wi-Fi path for the Fire TVs listed in the config file.
// It restores the router when the stop file appears or Ano stops answering.
func RunSteer(path string) error {
	cfg, err := readSteer(path)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	defer ln.Close()
	bpf, err := openBPF(cfg.Iface)
	if err != nil {
		return err
	}
	defer unix.Close(bpf)
	fwd, redir, err := setForwarding()
	if err != nil {
		return err
	}
	token, err := enablePF(cfg)
	if err != nil {
		restoreForwarding(fwd, redir)
		return err
	}
	if err := os.WriteFile(cfg.Ready, []byte("ready\n"), 0o644); err != nil {
		restoreForwarding(fwd, redir)
		disablePF(token)
		return err
	}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			correct(bpf, cfg)
			disablePF(token)
			restoreForwarding(fwd, redir)
			_ = os.Remove(cfg.Ready)
		})
	}
	defer cleanup()

	go acceptSteer(ln, cfg.Ano)
	missed := 0
	last := deviceKey(cfg.Devices)
	for {
		if _, err := os.Stat(cfg.Stop); err == nil {
			return nil
		}
		if !anoUp(cfg.Ano) {
			missed++
			if missed >= 3 {
				return nil
			}
		} else {
			missed = 0
		}
		next, err := readSteer(path)
		if err == nil {
			prev := append([]SteerDevice(nil), cfg.Devices...)
			cfg.Devices = next.Devices
			key := deviceKey(cfg.Devices)
			if key != last {
				gone := missingDevices(prev, cfg.Devices)
				if len(gone) > 0 {
					saved := cfg.Devices
					cfg.Devices = gone
					correct(bpf, cfg)
					cfg.Devices = saved
				}
				if err := loadRules(cfg); err == nil {
					last = key
				}
			}
		}
		announce(bpf, cfg, false)
		readARP(bpf, cfg)
		time.Sleep(300 * time.Millisecond)
	}
}

func acceptSteer(ln net.Listener, ano string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleSteer(conn, ano)
	}
}

func handleSteer(conn net.Conn, ano string) {
	defer conn.Close()
	client := CallerIP(conn.RemoteAddr().String())
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf, err := readHello(conn)
	if err != nil || len(buf) == 0 {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	sni, sniOK := ParseSNI(buf)
	httpHost, httpOK := HTTPHost(buf)
	host := sni
	if host == "" {
		host = httpHost
	}
	origIP, origPort := lookupOrig(conn)
	port := origPort
	if port == "" {
		port = "443"
		if httpOK && !sniOK {
			port = "80"
		}
	}
	forward := true
	category := "outbound"
	if host != "" && Streaming(host) {
		category = "stream"
	}
	if decision, err := askAno(ano, client, host); err == nil {
		if !decision.Enrolled {
			return
		}
		if decision.Category != "" {
			category = decision.Category
		}
		if !decision.Forward {
			forward = false
		}
	}
	protocol := "HTTPS"
	if port == "80" {
		protocol = "HTTP"
	}
	if !forward {
		_ = postNote(ano, client, host, protocol, category, len(buf), true)
		return
	}
	target := ""
	if origIP != "" && origPort != "" {
		target = net.JoinHostPort(origIP, origPort)
	} else if host != "" {
		target = net.JoinHostPort(host, port)
	}
	if target == "" {
		return
	}
	up, err := net.DialTimeout("tcp", target, 8*time.Second)
	if err != nil {
		return
	}
	defer up.Close()
	if _, err := up.Write(buf); err != nil {
		return
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
		n, _ := io.Copy(up, conn)
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
	if host != "" {
		_ = postNote(ano, client, host, protocol, category, int(moved)+len(buf), false)
	}
}

func readHello(conn net.Conn) ([]byte, error) {
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}
	if buf[0] == 22 {
		n := int(buf[3])<<8 | int(buf[4])
		if n < 0 || n > 16384 {
			return buf, nil
		}
		rest := make([]byte, n)
		if _, err := io.ReadFull(conn, rest); err != nil {
			return append(buf, rest...), err
		}
		return append(buf, rest...), nil
	}
	more := make([]byte, 2048)
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	n, _ := conn.Read(more)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	return append(buf, more[:n]...), nil
}

func lookupOrig(conn net.Conn) (string, string) {
	local, ok1 := conn.LocalAddr().(*net.TCPAddr)
	remote, ok2 := conn.RemoteAddr().(*net.TCPAddr)
	if !ok1 || !ok2 || local.IP.To4() == nil || remote.IP.To4() == nil {
		return "", ""
	}
	fd, err := unix.Open("/dev/pf", unix.O_RDWR, 0)
	if err != nil {
		return "", ""
	}
	defer unix.Close(fd)
	for _, direction := range []uint8{2, 1} {
		nl := natlook{af: unix.AF_INET, proto: unix.IPPROTO_TCP, direction: direction}
		copy(nl.saddr[:4], remote.IP.To4())
		copy(nl.daddr[:4], local.IP.To4())
		binary.BigEndian.PutUint16(nl.sx[:2], uint16(remote.Port))
		binary.BigEndian.PutUint16(nl.dx[:2], uint16(local.Port))
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(natlookIOCTL), uintptr(unsafe.Pointer(&nl)))
		if errno != 0 {
			continue
		}
		ip := net.IP(nl.rdaddr[:4])
		port := binary.BigEndian.Uint16(nl.rdx[:2])
		if port == 0 || ip.IsUnspecified() {
			continue
		}
		return ip.String(), fmt.Sprintf("%d", port)
	}
	return "", ""
}

func askAno(ano, addr, host string) (Decision, error) {
	body, _ := json.Marshal(map[string]string{"addr": addr, "host": host})
	req, err := http.NewRequest(http.MethodPost, ano+"/api/via/check", bytes.NewReader(body))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Decision{}, err
	}
	defer resp.Body.Close()
	var decision Decision
	if err := json.NewDecoder(resp.Body).Decode(&decision); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

func postNote(ano, addr, host, protocol, category string, n int, refused bool) error {
	body, _ := json.Marshal(map[string]any{
		"addr": addr, "host": host, "protocol": protocol, "category": category, "bytes": n, "refused": refused,
	})
	req, err := http.NewRequest(http.MethodPost, ano+"/api/via/note", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func readSteer(path string) (SteerConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SteerConfig{}, err
	}
	var cfg SteerConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return SteerConfig{}, err
	}
	return cfg, nil
}

func deviceKey(devices []SteerDevice) string {
	var b strings.Builder
	for _, device := range devices {
		b.WriteString(device.Addr)
		b.WriteByte(' ')
	}
	return b.String()
}

func missingDevices(prev, next []SteerDevice) []SteerDevice {
	have := map[string]bool{}
	for _, device := range next {
		have[device.Addr] = true
	}
	var gone []SteerDevice
	for _, device := range prev {
		if !have[device.Addr] {
			gone = append(gone, device)
		}
	}
	return gone
}

func anoUp(ano string) bool {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(ano + "/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func enablePF(cfg SteerConfig) (string, error) {
	out, err := exec.Command("pfctl", "-E").CombinedOutput()
	token := tokenOf(string(out))
	if err != nil && token == "" {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	if err := loadRules(cfg); err != nil {
		if token != "" {
			_ = exec.Command("pfctl", "-X", token).Run()
		}
		return "", err
	}
	return token, nil
}

func loadRules(cfg SteerConfig) error {
	var addrs []string
	for _, device := range cfg.Devices {
		if net.ParseIP(device.Addr) != nil {
			addrs = append(addrs, device.Addr)
		}
	}
	text, err := RenderPF(cfg.Iface, cfg.Gateway, cfg.LAN, cfg.OurIP, addrs)
	if err != nil {
		return err
	}
	rules := cfg.Ready + ".pf"
	if err := os.WriteFile(rules, []byte(text), 0o644); err != nil {
		return err
	}
	load, err := exec.Command("pfctl", "-a", "com.apple/ano", "-f", rules).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(load)))
	}
	return nil
}

func tokenOf(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "token") {
			if i := strings.LastIndex(line, " "); i >= 0 {
				return strings.TrimSpace(line[i:])
			}
		}
	}
	return ""
}

func disablePF(token string) {
	_ = exec.Command("pfctl", "-a", "com.apple/ano", "-F", "all").Run()
	if token != "" {
		_ = exec.Command("pfctl", "-X", token).Run()
	}
}

func setForwarding() (fwd, redir string, err error) {
	fwd, err = sysctlGet("net.inet.ip.forwarding")
	if err != nil {
		return "", "", err
	}
	redir, err = sysctlGet("net.inet.ip.redirect")
	if err != nil {
		return "", "", err
	}
	if err := sysctlSet("net.inet.ip.forwarding", "1"); err != nil {
		return "", "", err
	}
	if err := sysctlSet("net.inet.ip.redirect", "0"); err != nil {
		_ = sysctlSet("net.inet.ip.forwarding", fwd)
		return "", "", err
	}
	return fwd, redir, nil
}

func restoreForwarding(fwd, redir string) {
	if fwd != "" {
		_ = sysctlSet("net.inet.ip.forwarding", fwd)
	}
	if redir != "" {
		_ = sysctlSet("net.inet.ip.redirect", redir)
	}
}

func sysctlGet(name string) (string, error) {
	out, err := exec.Command("sysctl", "-n", name).CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func sysctlSet(name, value string) error {
	out, err := exec.Command("sysctl", "-w", name+"="+value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func announce(fd int, cfg SteerConfig, restore bool) {
	src := cfg.OurMAC
	sender := cfg.OurMAC
	if restore {
		src = cfg.GatewayMAC
		sender = cfg.GatewayMAC
	}
	for _, device := range cfg.Devices {
		frame, err := ARPReply(device.MAC, src, sender, cfg.Gateway, device.MAC, device.Addr)
		if err != nil {
			continue
		}
		_, _ = unix.Write(fd, frame)
	}
}

func correct(fd int, cfg SteerConfig) {
	for i := 0; i < 5; i++ {
		announce(fd, cfg, true)
		time.Sleep(100 * time.Millisecond)
	}
}

func openBPF(iface string) (int, error) {
	var fd int
	var err error
	for i := 0; i < 255; i++ {
		fd, err = unix.Open(fmt.Sprintf("/dev/bpf%d", i), unix.O_RDWR, 0)
		if err == nil {
			break
		}
	}
	if err != nil {
		return 0, fmt.Errorf("could not open a packet device")
	}
	if err := ioctlName(fd, unix.BIOCSETIF, iface); err != nil {
		unix.Close(fd)
		return 0, err
	}
	if err := unix.IoctlSetInt(fd, unix.BIOCSHDRCMPLT, 1); err != nil {
		unix.Close(fd)
		return 0, err
	}
	_ = unix.IoctlSetInt(fd, unix.BIOCIMMEDIATE, 1)
	_ = setARPFilter(fd)
	_ = unix.SetNonblock(fd, true)
	return fd, nil
}

func ioctlName(fd int, req uint, name string) error {
	var ifr [32]byte
	copy(ifr[:], name)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&ifr[0])))
	if errno != 0 {
		return errno
	}
	return nil
}

func setARPFilter(fd int) error {
	insns := []unix.BpfInsn{
		{Code: 0x28, K: 12},
		{Code: 0x15, Jf: 1, K: 0x0806},
		{Code: 0x06, K: 0xffff},
		{Code: 0x06},
	}
	prog := unix.BpfProgram{Len: uint32(len(insns)), Insns: &insns[0]}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.BIOCSETF), uintptr(unsafe.Pointer(&prog)))
	if errno != 0 {
		return errno
	}
	return nil
}

func readARP(fd int, cfg SteerConfig) {
	buf := make([]byte, 4096)
	n, err := unix.Read(fd, buf)
	if err != nil || n < 26 {
		return
	}
	off := 0
	for off+26 <= n {
		hdrlen := int(binary.LittleEndian.Uint16(buf[off+24 : off+26]))
		caplen := int(binary.LittleEndian.Uint32(buf[off+16 : off+20]))
		if hdrlen < 26 || caplen < 0 || off+hdrlen+caplen > n {
			return
		}
		frame := buf[off+hdrlen : off+hdrlen+caplen]
		answerARP(fd, cfg, frame)
		off += (hdrlen + caplen + 3) &^ 3
	}
}

func answerARP(fd int, cfg SteerConfig, frame []byte) {
	if len(frame) < 42 || frame[12] != 0x08 || frame[13] != 0x06 {
		return
	}
	if binary.BigEndian.Uint16(frame[20:22]) != 1 {
		return
	}
	tip := net.IP(frame[38:42]).String()
	if tip != cfg.Gateway {
		return
	}
	sender := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", frame[22], frame[23], frame[24], frame[25], frame[26], frame[27])
	sip := net.IP(frame[28:32]).String()
	for _, device := range cfg.Devices {
		if strings.EqualFold(device.MAC, sender) && device.Addr == sip {
			reply, err := ARPReply(device.MAC, cfg.OurMAC, cfg.OurMAC, cfg.Gateway, device.MAC, device.Addr)
			if err == nil {
				_, _ = unix.Write(fd, reply)
			}
		}
	}
}
