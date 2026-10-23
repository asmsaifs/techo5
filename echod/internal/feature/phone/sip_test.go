package phone

import (
	"context"
	"errors"
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

// A call is rung for only when it came from the provider: the listener is on the home network, and on
// a panel nothing in front of it keeps anyone else's INVITE out.
func TestOnlyTheProviderRings(t *testing.T) {
	for _, c := range []struct {
		server, source string
		want           bool
	}{
		{"192.0.2.10", "192.0.2.10:5060", true},
		{"192.0.2.10", "192.0.2.10:40123", true},
		{"192.0.2.10", "[::ffff:192.0.2.10]:5061", true},
		{"192.0.2.10", "198.51.100.7:5060", false},
		{"192.0.2.10", "203.0.113.20:5060", false},
		{"2001:db8::5", "[2001:db8::5]:5061", true},
		{"2001:db8::5", "[2001:db8::6]:5061", false},
		{"localhost", "127.0.0.1:5061", true},
		{"localhost", "192.0.2.10:5061", false},
		{"192.0.2.10", "", false},
		{"192.0.2.10", "192.0.2.10", false},
		{"192.0.2.10", "provider.example:5060", false},
	} {
		if got := fromProvider(c.server, c.source); got != c.want {
			t.Errorf("a call from %q with the provider at %q: rung %v, want %v", c.source, c.server, got, c.want)
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

func TestUnanswered(t *testing.T) {
	if !unanswered(errors.New(`fail to create transaction req="REGISTER sip:x SIP/2.0": Timer_B timed out. transaction timeout`)) {
		t.Error("a sign-in nobody answered")
	}
	if unanswered(errors.New("dial tcp: i/o timeout")) || unanswered(nil) {
		t.Error("not reaching the provider is not the same thing")
	}
}

// The provider's addresses are remembered from each lookup, and a call is checked against them when
// the lookup fails: a name server that is down when the call arrives does not refuse the provider's
// call.
func TestTheProviderIsRememberedWhenTheLookupFails(t *testing.T) {
	p := newProvider("sip.provider.example")
	answer := []net.IPAddr{{IP: net.ParseIP("192.0.2.10")}}
	var down error
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) { return answer, down }

	if !p.from("192.0.2.10:5061") {
		t.Fatal("a call from the address the lookup gave was refused")
	}
	answer = []net.IPAddr{{IP: net.ParseIP("192.0.2.11")}}
	if !p.from("192.0.2.10:5061") {
		t.Error("the provider moved, and a call from where it was looked up before was refused")
	}

	answer, down = nil, errors.New("no name server")
	for _, c := range []struct {
		source string
		want   bool
	}{
		{"192.0.2.10:5061", true},
		{"192.0.2.11:5060", true},
		{"192.0.2.99:5061", false},
	} {
		if got := p.from(c.source); got != c.want {
			t.Errorf("with the lookup down, a call from %s: rung %v, want %v", c.source, got, c.want)
		}
	}
}
