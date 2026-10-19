package web

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A page that is not there answers a slow POST at once and lets its connection go.
func TestNotFoundDoesNotWaitOnABody(t *testing.T) {
	closed := make(chan struct{}, 4)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { notFound(w) }))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateClosed {
			closed <- struct{}{}
		}
	}
	srv.Start()
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprint(conn, "POST /nothing HTTP/1.1\r\nHost: d\r\nContent-Length: 1000\r\n\r\nab")
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "HTTP/1.1 404") {
		t.Fatalf("%q, %v", line, err)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Error("the server still held the connection after answering")
	}
}

// The index answers anything but a read without waiting on a body.
func TestTheIndexRefusesAPostAtOnce(t *testing.T) {
	f := &Feature{}
	srv := httptest.NewServer(f.mux())
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: d\r\nContent-Length: 1000\r\n\r\nab")
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "HTTP/1.1 405") {
		t.Errorf("%q, %v", line, err)
	}
}
