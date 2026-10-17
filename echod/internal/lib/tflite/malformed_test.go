package tflite

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

// A vector's length word is a number the file states, and the one place a file says how much to
// allocate. One that claims more than is left of the file is refused before anything is made of it:
// billions of anything is out of memory, which is fatal rather than a panic Parse can turn into an
// error.
func TestParseRefusesAVectorLongerThanTheFile(t *testing.T) {
	good, err := os.ReadFile("../../feature/detect/assets/stop.tflite")
	if err != nil {
		t.Fatal(err)
	}
	r := root(buf(good))
	for _, field := range []int{modelOperatorCodes, modelSubgraphs, modelBuffers} {
		b := bytes.Clone(good)
		p := r.pos + r.slot(field)
		p += buf(b).u32(p)
		binary.LittleEndian.PutUint32(b[p:], 0xfffffff0)
		if m, err := Parse(b); err == nil || !strings.Contains(err.Error(), "past the end") {
			t.Errorf("field %d: Parse = %v, %v; want a vector past the end", field, m, err)
		}
	}

	// A string's length is read the same way, and wide enough not to wrap around to a small one.
	b := buf{4, 0, 0, 0, 0xfe, 0xff, 0xff, 0xff, 'a', 0, 0, 0}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("a string longer than the buffer was read")
			}
		}()
		b.fits(4, b.u32(4), 1)
	}()
}

// f32in is a float32 input tensor of the given shape, the sort every model starts with.
func f32in(name string, shape ...int) *TensorDesc {
	return &TensorDesc{Name: name, Type: Float32, Shape: shape}
}

// A model file is not a trustworthy document. Parse only proves the flatbuffer can be read, so a file
// that says something no converter would say gets all the way to New, where it used to be an index out
// of range on whichever goroutine was loading wake words. Each of these has to come back as an error.
func TestNewRefusesAModelThatParsesButMakesNoSense(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model *Model
	}{
		{"no subgraphs", &Model{}},
		{"an input that is not a tensor", &Model{Subgraphs: []*Subgraph{{
			Tensors: []*TensorDesc{f32in("x", 1, 4)},
			Inputs:  []int{7},
			Outputs: []int{0},
		}}}},
		{"an output that is not a tensor", &Model{Subgraphs: []*Subgraph{{
			Tensors: []*TensorDesc{f32in("x", 1, 4)},
			Inputs:  []int{0},
			Outputs: []int{-3},
		}}}},
		{"an operator reading past the table", &Model{Subgraphs: []*Subgraph{{
			Tensors: []*TensorDesc{f32in("x", 1, 4), f32in("y", 1, 4)},
			Inputs:  []int{0},
			Outputs: []int{1},
			Ops:     []*OpDesc{{Op: OpAdd, Inputs: []int{0, 9}, Outputs: []int{1}}},
		}}}},
		{"an operator writing past the table", &Model{Subgraphs: []*Subgraph{{
			Tensors: []*TensorDesc{f32in("x", 1, 4), f32in("y", 1, 4)},
			Inputs:  []int{0},
			Outputs: []int{1},
			Ops:     []*OpDesc{{Op: OpAdd, Inputs: []int{0, 0}, Outputs: []int{4}}},
		}}}},
		{"a negative dimension", &Model{Subgraphs: []*Subgraph{{
			Tensors: []*TensorDesc{f32in("x", 1, -4)},
			Inputs:  []int{0},
			Outputs: []int{0},
		}}}},
		{"a shape nothing could hold", &Model{Subgraphs: []*Subgraph{{
			Tensors: []*TensorDesc{f32in("x", 1<<30, 1<<30)},
			Inputs:  []int{0},
			Outputs: []int{0},
		}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := New(tc.model)
			if err == nil {
				t.Fatalf("built an interpreter from %s: %+v", tc.name, in)
			}
			if !strings.HasPrefix(err.Error(), "tflite: ") {
				t.Errorf("error is %q, which does not name the package", err)
			}
		})
	}
}

// The same for the streaming front end, which reads a chain of operators out of the file and so has
// its own set of things a model can fail to be.
func TestNewStreamRefusesAModelThatMakesNoSense(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model *Model
	}{
		{"no subgraphs", &Model{}},
		{"no input", &Model{Subgraphs: []*Subgraph{{
			Tensors: []*TensorDesc{f32in("x", 1, 4, 4, 1)},
			Outputs: []int{0},
		}}}},
		{"no output", &Model{Subgraphs: []*Subgraph{{
			Tensors: []*TensorDesc{f32in("x", 1, 4, 4, 1)},
			Inputs:  []int{0},
		}}}},
		{"an operator that writes nothing", &Model{Subgraphs: []*Subgraph{{
			Tensors: []*TensorDesc{f32in("x", 1, 4, 4, 1), f32in("y", 1, 4, 4, 1)},
			Inputs:  []int{0},
			Outputs: []int{1},
			Ops:     []*OpDesc{{Op: OpRelu, Inputs: []int{0}}},
		}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if s, err := NewStream(tc.model, []int{1, 4, 4, 1}); err == nil {
				t.Fatalf("built a stream from %s: %+v", tc.name, s)
			}
		})
	}
}

func TestStreamWriteTurnsAKernelPanicIntoAnError(t *testing.T) {
	// A stage NewStream would not have built: its second operand is missing, which a kernel reads
	// without asking. Write is on the wake word's goroutine, so this has to come back as an error.
	s := &Stream{stages: []stage{{
		op:       &OpDesc{Op: OpAdd, Inputs: []int{0, -1}, Outputs: []int{1}},
		consts:   []*Tensor{nil},
		out:      &Tensor{Type: Float32},
		need:     1,
		consume:  1,
		rowSize:  2,
		width:    1,
		channels: 2,
	}}}
	out, err := s.Write([]float32{1, 2})
	if err == nil || !strings.Contains(err.Error(), "malformed model") {
		t.Fatalf("Write = %v, %v; want a malformed-model error", out, err)
	}
}

// opcodeTable is an OperatorCode table with deprecated_builtin_code dep, and builtin_code builtin when
// has is set, absent from the table otherwise.
func opcodeTable(dep uint8, builtin int32, has bool) table {
	// The vtable first (its size, the table's, then one offset a slot up to builtin_code), the table
	// after it: its offset back to the vtable, the byte field, and the int32 one.
	b := make(buf, 24)
	binary.LittleEndian.PutUint16(b[0:], 12)
	binary.LittleEndian.PutUint16(b[2:], 12)
	binary.LittleEndian.PutUint16(b[4+2*opcodeDeprecatedBuiltin:], 4)
	if has {
		binary.LittleEndian.PutUint16(b[4+2*opcodeBuiltin:], 8)
	}
	binary.LittleEndian.PutUint32(b[12:], 12)
	b[16] = dep
	binary.LittleEndian.PutUint32(b[20:], uint32(builtin))
	return table{b, 12}
}

// The deprecated byte names the op only when builtin_code is absent, not when it is there and zero,
// which is ADD; the engine that runs the model reads it so, and a zero taken for absent made it some
// other op here.
func TestOpcodeFallsBackOnlyWhenBuiltinCodeIsAbsent(t *testing.T) {
	for _, c := range []struct {
		name    string
		dep     uint8
		builtin int32
		has     bool
		want    Op
	}{
		{"builtin_code there and zero", 3, 0, true, 0},
		{"builtin_code absent", 3, 0, false, 3},
		{"builtin_code past the byte", 127, 150, true, 150},
		{"neither", 0, 0, false, 0},
	} {
		if got := opcode(opcodeTable(c.dep, c.builtin, c.has)); got != c.want {
			t.Errorf("%s: op %d, want %d", c.name, got, c.want)
		}
	}
}
