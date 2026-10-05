package phone

import (
	"net"
	"testing"

	"github.com/emiago/sipgo/sip"
)

func TestHostPort(t *testing.T) {
	for _, c := range []struct {
		server string
		host   string
		port   int
	}{
		{"chicago1.voip.ms", "chicago1.voip.ms", 5061},
		{"sip.linphone.org:443", "sip.linphone.org", 443},
		{"sip.example.com:0", "sip.example.com:0", 5061},
		{"sip.example.com:x", "sip.example.com:x", 5061},
	} {
		h, p := Account{Server: c.server}.hostPort(5061)
		if h != c.host || p != c.port {
			t.Errorf("%q: got %s %d, want %s %d", c.server, h, p, c.host, c.port)
		}
	}
}

func TestWithoutFeedback(t *testing.T) {
	in := "v=0\r\nm=audio 7078 RTP/SAVPF 96 0 8\r\na=rtcp-fb:* trr-int 1000\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\n"
	want := "v=0\r\nm=audio 7078 RTP/SAVP 96 0 8\r\na=rtcp-fb:* trr-int 1000\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\n"
	if got := string(withoutFeedback([]byte(in))); got != want {
		t.Errorf("got %q", got)
	}
	if got := string(withoutFeedback([]byte("m=audio 1 RTP/AVPF 0\n"))); got != "m=audio 1 RTP/AVP 0\n" {
		t.Errorf("got %q", got)
	}
}

func TestFramer(t *testing.T) {
	f := newFramer()
	a := &net.TCPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 1}
	props := sip.TransportReadProps{Transport: "TLS", LocalAddr: a, RemoteAddr: a}
	read := func(s string) string {
		out, err := f.filter(props, []byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	ack := "ACK sip:x SIP/2.0\r\nContent-Length: 0\r\n"
	if got := read(ack); got != "" {
		t.Fatalf("partial message passed on: %q", got)
	}
	// The blank line that ends it, alone: not a keepalive.
	if got := read("\r\n"); got != ack+"\r\n" {
		t.Fatalf("got %q", got)
	}
	// A keepalive between messages goes through for sipgo to answer.
	if got := read("\r\n\r\n"); got != "\r\n\r\n" {
		t.Fatalf("keepalive: got %q", got)
	}
	// A body split across reads, and two messages in one read.
	inv := "INVITE sip:x SIP/2.0\r\nl: 4\r\n\r\nv=0\n"
	if got := read(inv[:30]); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := read(inv[30:] + ack + "\r\n"); got != inv+ack+"\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMediaAddress(t *testing.T) {
	in := "v=0\r\nc=IN IP4 192.168.1.151\r\nt=0 0\r\nm=audio 14176 RTP/AVP 0\r\nc=IN IP4 176.31.149.179\r\na=rtcp-mux\r\n"
	want := "v=0\r\nc=IN IP4 176.31.149.179\r\nt=0 0\r\nm=audio 14176 RTP/AVP 0\r\nc=IN IP4 176.31.149.179\r\na=rtcp-mux\r\n"
	if got := string(mediaAddress([]byte(in))); got != want {
		t.Errorf("got %q", got)
	}
	plain := "v=0\r\nc=IN IP4 10.0.0.1\r\nm=audio 4000 RTP/AVP 0\r\n"
	if got := string(mediaAddress([]byte(plain))); got != plain {
		t.Errorf("changed an offer with one address: %q", got)
	}
}
