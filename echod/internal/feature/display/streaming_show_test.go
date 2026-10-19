//go:build !dot && !spot

package display

import "testing"

func TestStreamingDot(t *testing.T) {
	r := testRenderer()
	x, y := r.w-r.s(20), r.s(20)
	r.streamingDot(scene{})
	if r.dst.RGBAAt(x, y) == colStreaming {
		t.Fatal("a dot with nothing streamed")
	}
	r.streamingDot(scene{streaming: true})
	if got := r.dst.RGBAAt(x, y); got != colStreaming {
		t.Fatalf("no dot while streamed: %v", got)
	}
}
