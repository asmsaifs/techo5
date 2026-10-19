package tflite_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/lib/tflite"
)

// The float models this package runs give the same bits, output for output, as they did before it learned
// to run quantized ones: testdata/golden_float.txt holds a hash of every output's exact float32 bits, as
// written by the interpreter of that time. TFLITE_GOLDEN_WRITE=<file> writes it again.

const goldenFile = "testdata/golden_float.txt"

// goldenRuns are the models and inputs. Inputs are fixed pseudo-random values, the same on every run.
func goldenRuns(t *testing.T) map[string][]float32 {
	t.Helper()
	out := map[string][]float32{}
	read := func(p string) *tflite.Model {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Skip(err)
		}
		m, err := tflite.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	rng := rand.New(rand.NewPCG(5, 8))
	fill := func(x []float32, scale float32) {
		for i := range x {
			x[i] = (rng.Float32()*2 - 1) * scale
		}
	}

	// The mel front end, on a window of audio at a few lengths.
	mel := read("../oww/assets/melspectrogram.tflite")
	for _, n := range []int{1280, 1760, 3200} {
		in, err := tflite.New(mel)
		if err != nil {
			t.Fatal(err)
		}
		in.ResizeInput(0, []int{1, n})
		fill(in.Input(0).F32, 8000)
		if err := in.Invoke(); err != nil {
			t.Fatal(err)
		}
		out[fmt.Sprintf("mel/%d", n)] = append([]float32(nil), in.Output(0).F32...)
	}

	// The embedding, whole and streamed.
	emb := read("../oww/assets/embedding_model.tflite")
	in, err := tflite.New(emb)
	if err != nil {
		t.Fatal(err)
	}
	in.ResizeInput(0, []int{1, 76, 32, 1})
	rows := make([]float32, 100*32)
	fill(rows, 2)
	copy(in.Input(0).F32, rows)
	if err := in.Invoke(); err != nil {
		t.Fatal(err)
	}
	out["embedding/whole"] = append([]float32(nil), in.Output(0).F32...)

	s, err := tflite.NewStream(emb, []int{1, 76, 32, 1})
	if err != nil {
		t.Fatal(err)
	}
	var streamed []float32
	for i := 0; i < 100; i += 4 {
		y, err := s.Write(rows[i*32 : (i+4)*32])
		if err != nil {
			t.Fatal(err)
		}
		streamed = append(streamed, y...)
	}
	out["embedding/stream"] = streamed

	// A wake word classifier, when one is named (CLASSIFIER), on embeddings.
	if p := os.Getenv("CLASSIFIER"); p != "" {
		c := read(p)
		in, err := tflite.New(c)
		if err != nil {
			t.Fatal(err)
		}
		x := in.Input(0)
		in.ResizeInput(0, x.Shape)
		fill(in.Input(0).F32, 1)
		if err := in.Invoke(); err != nil {
			t.Fatal(err)
		}
		out["classifier"] = append([]float32(nil), in.Output(0).F32...)
	}
	return out
}

func digest(x []float32) string {
	h := sha256.New()
	var b [4]byte
	for _, v := range x {
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		h.Write(b[:])
	}
	return fmt.Sprintf("%d %s", len(x), hex.EncodeToString(h.Sum(nil)))
}

func TestFloatModelsUnchanged(t *testing.T) {
	runs := goldenRuns(t)
	if p := os.Getenv("TFLITE_GOLDEN_WRITE"); p != "" {
		var b strings.Builder
		for _, k := range []string{"mel/1280", "mel/1760", "mel/3200", "embedding/whole", "embedding/stream", "classifier"} {
			if x, ok := runs[k]; ok {
				fmt.Fprintf(&b, "%s %s\n", k, digest(x))
			}
		}
		if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(goldenFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	seen := 0
	for sc.Scan() {
		name, want, ok := strings.Cut(sc.Text(), " ")
		if !ok {
			continue
		}
		x, ok := runs[name]
		if !ok {
			continue // a classifier, when none is named this run
		}
		seen++
		if got := digest(x); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	if seen < 5 {
		t.Fatalf("only %d of the golden outputs were checked", seen)
	}
}
