//go:build !dot

package video

import (
	"errors"
	"os"
	"testing"
	"time"
)

// A pipe given its size still honors a read deadline: the camera's timeouts (the first frame, a stall)
// rely on it, and setting the size through Fd put the pipe in blocking mode, where deadlines do nothing.
func TestAGrownPipeKeepsItsDeadline(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	growPipe(r)
	_ = r.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	done := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 1))
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("the read ended with %v, not its deadline", err)
		}
	case <-time.After(5 * time.Second):
		w.Close() // lets the read end
		t.Error("the read's deadline did nothing")
	}
}
