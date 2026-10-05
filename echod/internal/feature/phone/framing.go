package phone

import (
	"bytes"
	"strconv"
	"strings"
	"sync"

	"github.com/emiago/sipgo/sip"
)

// maxFrame is the most a connection may hold of a message not yet complete before it is dropped.
const maxFrame = 64 << 10

// framer hands sipgo's stream parser only whole SIP messages. sipgo takes any read of four bytes or
// fewer that is all CRLF for a keepalive and throws it away; Linphone's server sometimes sends the
// blank line that ends a message's headers in a record of its own, and with it gone the parser reads
// the next message's first line as a header, losing that message and the one before it (an ACK, so
// an answered call never starts).
type framer struct {
	mu   sync.Mutex
	held map[string][]byte // per connection, what has arrived of a message not yet complete
}

func newFramer() *framer { return &framer{held: map[string][]byte{}} }

func (f *framer) filter(info sip.TransportReadProps, data []byte) ([]byte, error) {
	if strings.EqualFold(info.Transport, "udp") || info.LocalAddr == nil || info.RemoteAddr == nil {
		return data, nil // a datagram is a whole message already
	}
	key := info.LocalAddr.String() + ">" + info.RemoteAddr.String()

	f.mu.Lock()
	defer f.mu.Unlock()
	held := f.held[key]
	if len(held) == 0 && len(data) <= 4 && len(bytes.Trim(data, "\r\n")) == 0 {
		return data, nil // a real keepalive, between messages: sipgo answers it
	}
	out, rest := frames(append(held, data...))
	if len(rest) > maxFrame {
		rest = nil
	}
	if len(rest) == 0 {
		delete(f.held, key)
	} else {
		f.held[key] = append([]byte(nil), rest...)
	}
	return out, nil
}

// frames splits b into the whole messages at its start and what is left of the next one. Blank lines
// between messages (keepalives) are dropped.
func frames(b []byte) (whole, rest []byte) {
	for {
		b = bytes.TrimLeft(b, "\r\n")
		end := bytes.Index(b, []byte("\r\n\r\n"))
		if end < 0 {
			return whole, b
		}
		n := end + 4 + contentLength(b[:end])
		if len(b) < n {
			return whole, b
		}
		whole = append(whole, b[:n]...)
		b = b[n:]
	}
}

// contentLength reads the Content-Length (or its compact form l) of a message's headers; none is 0.
func contentLength(head []byte) int {
	for _, ln := range strings.Split(string(head), "\r\n")[1:] {
		name, val, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if strings.EqualFold(name, "content-length") || strings.EqualFold(name, "l") {
			n, err := strconv.Atoi(strings.TrimSpace(val))
			if err == nil && n >= 0 {
				return n
			}
		}
	}
	return 0
}
