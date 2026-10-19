package announce

import (
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
)

// A device that answers "not this house" has another house word, and is named for the setup page.
func TestADeviceWithAnotherWordIsNamed(t *testing.T) {
	forget := func() {
		otherWord.Lock()
		clear(otherWord.seen)
		clear(otherWord.warned)
		otherWord.Unlock()
	}
	forget()
	t.Cleanup(forget)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not this house", http.StatusForbidden)
	}))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	n, _ := strconv.Atoi(port)
	if err := post(Peer{Name: "Garage", Address: host, Port: n}, "Our House Word", []byte("hi"), nil); err == nil {
		t.Fatal("a refusal was taken as delivered")
	}
	if got := OtherWords(); !slices.Equal(got, []string{"Garage"}) {
		t.Errorf("other words %q, want the device that refused", got)
	}
	refusedBy("Porch", http.StatusConflict)
	if got := OtherWords(); slices.Contains(got, "Porch") {
		t.Error("a device that only disagreed about the time was named")
	}
}

// However many names answer, only so many are kept, and the newest of them.
func TestOtherWordsAreKeptToAFew(t *testing.T) {
	t.Cleanup(func() {
		otherWord.Lock()
		clear(otherWord.seen)
		clear(otherWord.warned)
		otherWord.Unlock()
	})
	for i := range otherWordsKept + 10 {
		refusedBy("device "+strconv.Itoa(i), http.StatusForbidden)
	}
	got := OtherWords()
	if len(got) != otherWordsKept || !slices.Contains(got, "device "+strconv.Itoa(otherWordsKept+9)) {
		t.Errorf("kept %d, the newest among them: %t", len(got), slices.Contains(got, "device "+strconv.Itoa(otherWordsKept+9)))
	}
	otherWord.Lock()
	n := len(otherWord.warned)
	otherWord.Unlock()
	if n > otherWordsKept {
		t.Errorf("%d warnings remembered", n)
	}
}
