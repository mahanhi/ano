package via

import "strings"

// ParseSNI reads the server name from a TLS ClientHello record.
func ParseSNI(b []byte) (string, bool) {
	if len(b) < 5 || b[0] != 22 {
		return "", false
	}
	hs := b[5:]
	if len(hs) < 42 || hs[0] != 1 {
		return "", false
	}
	body := hs[4:]
	if len(body) < 35 {
		return "", false
	}
	i := 34
	if i >= len(body) {
		return "", false
	}
	sid := int(body[i])
	i += 1 + sid
	if i+2 > len(body) {
		return "", false
	}
	cs := int(body[i])<<8 | int(body[i+1])
	i += 2 + cs
	if i+1 > len(body) {
		return "", false
	}
	comp := int(body[i])
	i += 1 + comp
	if i+2 > len(body) {
		return "", false
	}
	extLen := int(body[i])<<8 | int(body[i+1])
	i += 2
	end := i + extLen
	if end > len(body) {
		end = len(body)
	}
	for i+4 <= end {
		typ := int(body[i])<<8 | int(body[i+1])
		n := int(body[i+2])<<8 | int(body[i+3])
		i += 4
		if n < 0 || i+n > end {
			return "", false
		}
		if typ == 0 && n >= 5 {
			nameLen := int(body[i+3])<<8 | int(body[i+4])
			if nameLen > 0 && i+5+nameLen <= i+n {
				return string(body[i+5 : i+5+nameLen]), true
			}
		}
		i += n
	}
	return "", false
}

// HTTPHost reads a Host header from the start of an HTTP request.
func HTTPHost(b []byte) (string, bool) {
	text := string(b)
	lower := strings.ToLower(text)
	i := strings.Index(lower, "\nhost:")
	if i < 0 {
		if strings.HasPrefix(lower, "host:") {
			i = 0
		} else {
			return "", false
		}
	} else {
		i++
	}
	line := text[i:]
	if nl := strings.IndexAny(line, "\r\n"); nl >= 0 {
		line = line[:nl]
	}
	host := strings.TrimSpace(line[len("host:"):])
	if h, ok := Normalize(host); ok {
		return h, true
	}
	return "", false
}
