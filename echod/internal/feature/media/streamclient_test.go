package media

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A SHOUTcast v1 server's "ICY 200 OK" is read as HTTP.
func TestAnICYServerIsUnderstood(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 1024)
		c.Read(buf)
		io.WriteString(c, "ICY 200 OK\r\nicy-name:Test\r\nContent-Type: audio/mpeg\r\n\r\nhello")
	}()
	resp, err := streamClient.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("an ICY answer was refused: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "hello" || resp.Header.Get("icy-name") != "Test" {
		t.Errorf("read as %d %q %v", resp.StatusCode, body, resp.Header)
	}
}

// A station on the internet may not redirect the device into the home; one at home may redirect at home.
func TestRedirectsIntoTheHomeAreRefusedFromOutside(t *testing.T) {
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer home.Close()
	// At home to at home: allowed.
	resp, err := streamClient.Get(home.URL + "/start")
	if err != nil {
		t.Fatalf("a redirect within the home was refused: %v", err)
	}
	resp.Body.Close()

	check := streamClient.CheckRedirect
	from, _ := http.NewRequest(http.MethodGet, "http://203.0.113.7/stream", nil) // a public address
	to, _ := http.NewRequest(http.MethodGet, "http://192.168.1.1/reboot", nil)
	if err := check(to, []*http.Request{from}); err == nil || !strings.Contains(err.Error(), "home network") {
		t.Errorf("a public station's redirect into the home gave %v", err)
	}
	out, _ := http.NewRequest(http.MethodGet, "http://198.51.100.9/other", nil)
	if err := check(out, []*http.Request{from}); err != nil {
		t.Errorf("a redirect between public addresses was refused: %v", err)
	}
}
