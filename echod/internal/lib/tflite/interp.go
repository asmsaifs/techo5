package tflite

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Tensor is a buffer plus its current shape. Data is NHWC row-major and flat; Shape describes
// how to read it. Integer tensors decode into I32 whatever their declared width, because in
// these models they only ever carry shapes, axes and permutations.
type Tensor struct {
	Name  string
	Type  TensorType
	Shape []int
	F32   []float32
	I32   []int32
	Const bool

	// Scale and ZeroPoint are what a quantized model's tensor was quantized with (its first, for one
	// quantized per channel): a real value is (q - ZeroPoint) * Scale. The tensor itself holds real
	// values, in F32: a quantized model is run in float. Zero for a float model's tensors.
	Scale     float32
	ZeroPoint int64
}

// maxElements caps what one tensor may be asked to hold. The models this runs are a few hundred
// thousand elements at their largest, so anything past this is a shape a file made up rather than one
// a converter wrote — either directly, or through a RESHAPE whose target comes out of a constant. The
// cap keeps a made-up shape a refusal instead of an allocation nobody can take back.
const maxElements = 1 << 26

// count is how many elements a shape describes. A shape that is not one — a negative dimension, or a
// product past maxElements — panics with what it was, which New and Invoke turn into an error. Checking
// it here rather than at every resize is the same trade Parse makes: the model is about to be refused,
// so the cost of finding out is not worth spreading over every kernel.
func count(shape []int) int {
	n := 1
	for _, d := range shape {
		if d < 0 || (d != 0 && n > maxElements/d) {
			panic(fmt.Sprintf("shape %v is not a size this runs", shape))
		}
		n *= d
	}
	return n
}

// Count is the number of elements the current shape describes.
func (t *Tensor) Count() int { return count(t.Shape) }

// Dim is the size of an axis, counting from the end for negative i.
func (t *Tensor) Dim(i int) int {
	if i < 0 {
		i += len(t.Shape)
	}
	if i < 0 || i >= len(t.Shape) {
		return 1
	}
	return t.Shape[i]
}

func (t *Tensor) resize(shape []int) {
	n := count(shape)
	t.Shape = append(t.Shape[:0], shape...)
	switch t.Type {
	case Float32:
		if cap(t.F32) < n {
			t.F32 = make([]float32, n)
		}
		t.F32 = t.F32[:n]
	default:
		if cap(t.I32) < n {
			t.I32 = make([]int32, n)
		}
		t.I32 = t.I32[:n]
	}
}

// Interpreter runs one subgraph. It is not safe for concurrent use: tensors are reused between
// invocations so that steady-state inference does not allocate.
type Interpreter struct {
	model   *Model
	graph   *Subgraph
	tensors []*Tensor
}

// New prepares the model's first subgraph for execution.
//
// A model file is not a trustworthy document: it arrives over the network, and Parse only proves that
// its flatbuffer can be read, not that what it says is coherent. A model with no subgraph, an operator
// reading a tensor one past the end of the table, or a shape with a negative dimension all parse
// cleanly and then panic in here or in a kernel, on whatever goroutine was loading wake words. The
// checks below name the structural ones; the recover catches the rest, the same trade Parse makes.
func New(m *Model) (in *Interpreter, err error) {
	defer func() {
		if r := recover(); r != nil {
			in, err = nil, fmt.Errorf("tflite: malformed model: %v", r)
		}
	}()

	if len(m.Subgraphs) == 0 {
		return nil, errors.New("tflite: model has no subgraphs")
	}
	g := m.Subgraphs[0]
	if err := checkIndices(g); err != nil {
		return nil, err
	}
	in = &Interpreter{model: m, graph: g, tensors: make([]*Tensor, len(g.Tensors))}

	for i, d := range g.Tensors {
		t := &Tensor{Name: d.Name, Type: d.Type, Shape: append([]int(nil), d.Shape...)}
		if len(d.Scale) > 0 {
			t.Scale = d.Scale[0]
			if len(d.ZeroPoint) > 0 {
				t.ZeroPoint = d.ZeroPoint[0]
			}
		}
		raw := m.buffer(d.Buffer)
		if raw != nil {
			if err := decode(t, raw); err != nil {
				return nil, fmt.Errorf("tensor %d (%s): %w", i, d.Name, err)
			}
			if err := dequantize(t, d); err != nil {
				return nil, fmt.Errorf("tensor %d (%s): %w", i, d.Name, err)
			}
			t.Const = true
		} else {
			switch d.Type {
			case Float32, Int32, Int64, Bool:
			case Int8, UInt8:
				// A quantized model's activations are carried as real values: the model is run in
				// float, the weights dequantized as they are read.
				if len(d.Scale) == 0 {
					return nil, fmt.Errorf("tflite: tensor %d (%s) is %s with no scale", i, d.Name, d.Type)
				}
				t.Type = Float32
			default:
				return nil, fmt.Errorf("tflite: tensor %d (%s) has unsupported type %s", i, d.Name, d.Type)
			}
			t.resize(t.Shape)
		}
		in.tensors[i] = t
	}

	for i, o := range g.Ops {
		if _, ok := kernels[o.Op]; !ok {
			return nil, fmt.Errorf("tflite: operator %d is %s, which is not implemented", i, o.Op)
		}
	}
	return in, nil
}

// checkIndices makes sure every tensor a subgraph names exists. Invoke looks these up without bounds
// checks of its own, once per operator per audio frame, so they are settled here instead.
func checkIndices(g *Subgraph) error {
	n := len(g.Tensors)
	for _, i := range g.Inputs {
		if i < 0 || i >= n {
			return fmt.Errorf("tflite: subgraph input is tensor %d, which does not exist", i)
		}
	}
	for _, i := range g.Outputs {
		if i < 0 || i >= n {
			return fmt.Errorf("tflite: subgraph output is tensor %d, which does not exist", i)
		}
	}
	for j, o := range g.Ops {
		for _, i := range o.Inputs {
			// An optional input, such as a missing bias, is encoded as index -1.
			if i < -1 || i >= n {
				return fmt.Errorf("tflite: op %d (%s) reads tensor %d, which does not exist", j, o.Op, i)
			}
		}
		for _, i := range o.Outputs {
			if i < 0 || i >= n {
				return fmt.Errorf("tflite: op %d (%s) writes tensor %d, which does not exist", j, o.Op, i)
			}
		}
	}
	return nil
}

func decode(t *Tensor, raw []byte) error {
	switch t.Type {
	case Float32:
		n := len(raw) / 4
		t.F32 = make([]float32, n)
		for i := range t.F32 {
			t.F32[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
		}
	case Int32:
		n := len(raw) / 4
		t.I32 = make([]int32, n)
		for i := range t.I32 {
			t.I32[i] = int32(binary.LittleEndian.Uint32(raw[4*i:]))
		}
	case Int64:
		n := len(raw) / 8
		t.I32 = make([]int32, n)
		for i := range t.I32 {
			t.I32[i] = int32(int64(binary.LittleEndian.Uint64(raw[8*i:])))
		}
	case Bool:
		t.I32 = make([]int32, len(raw))
		for i, b := range raw {
			if b != 0 {
				t.I32[i] = 1
			}
		}
	case Int8:
		t.I32 = make([]int32, len(raw))
		for i, b := range raw {
			t.I32[i] = int32(int8(b))
		}
	case UInt8:
		t.I32 = make([]int32, len(raw))
		for i, b := range raw {
			t.I32[i] = int32(b)
		}
	default:
		return fmt.Errorf("unsupported constant type %s", t.Type)
	}
	return nil
}

// dequantize turns a quantized constant (int8 or uint8 weights, int32 biases) into the real values it
// stands for, so the float kernels run it. A constant with no scale is left as it is, except an 8-bit
// one, which means nothing without one.
func dequantize(t *Tensor, d *TensorDesc) error {
	if len(d.Scale) == 0 {
		if t.Type == Int8 || t.Type == UInt8 {
			return fmt.Errorf("%s constant with no scale", t.Type)
		}
		return nil
	}
	switch t.Type {
	case Int8, UInt8, Int32:
	default:
		return nil
	}
	channels, inner := 1, 1
	if len(d.Scale) > 1 {
		axis := d.QuantDim
		if len(t.Shape) == 1 {
			// A bias quantized per channel is one value a channel, and carries the weights' axis.
			axis = 0
		}
		if axis < 0 || axis >= len(t.Shape) || t.Shape[axis] != len(d.Scale) {
			return fmt.Errorf("%d scales for axis %d of shape %v", len(d.Scale), axis, t.Shape)
		}
		channels = len(d.Scale)
		for _, n := range t.Shape[axis+1:] {
			if n < 1 || inner > maxElements/n {
				return fmt.Errorf("a shape %v no channel can be found in", t.Shape)
			}
			inner *= n
		}
	}
	for _, sc := range d.Scale {
		// A scale of nothing, or one that is not a number, gives weights that are not numbers either: a
		// model that runs, and says nothing true, is worse than one refused.
		if !(sc > 0) || math.IsInf(float64(sc), 0) {
			return fmt.Errorf("a scale of %v", sc)
		}
	}
	if len(d.ZeroPoint) > 1 && len(d.ZeroPoint) != len(d.Scale) {
		return fmt.Errorf("%d zero points for %d scales", len(d.ZeroPoint), len(d.Scale))
	}
	t.F32 = make([]float32, len(t.I32))
	for i, q := range t.I32 {
		c := 0
		if channels > 1 {
			c = (i / inner) % channels
		}
		var zp int64
		switch len(d.ZeroPoint) {
		case 0:
		case 1:
			zp = d.ZeroPoint[0]
		default:
			zp = d.ZeroPoint[c]
		}
		t.F32[i] = float32(int64(q)-zp) * d.Scale[c]
	}
	t.I32, t.Type = nil, Float32
	return nil
}

// Tensor returns a tensor by graph index.
func (in *Interpreter) Tensor(i int) *Tensor { return in.tensors[i] }

// Input returns the i'th subgraph input.
func (in *Interpreter) Input(i int) *Tensor { return in.tensors[in.graph.Inputs[i]] }

// Output returns the i'th subgraph output.
func (in *Interpreter) Output(i int) *Tensor { return in.tensors[in.graph.Outputs[i]] }

// ResizeInput sets an input's shape. Every other shape follows from it during Invoke, so this is
// the only place a caller has to think about dynamic dimensions.
func (in *Interpreter) ResizeInput(i int, shape []int) {
	in.Input(i).resize(shape)
}

// Invoke runs every operator in order, recomputing shapes as it goes.
//
// Shapes are recomputed from what the model says, including constants a RESHAPE or a FILL reads its
// target out of, so a file can still name a size or an operand count no kernel expects after New has
// checked everything that is checkable up front. That lands as an index or an allocation rather than an
// error, and this is the last frame that can turn it back into one.
func (in *Interpreter) Invoke() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("tflite: malformed model: %v", r)
		}
	}()

	for i, o := range in.graph.Ops {
		ins := make([]*Tensor, len(o.Inputs))
		for j, idx := range o.Inputs {
			// An optional input, such as a missing bias, is encoded as index -1.
			if idx >= 0 {
				ins[j] = in.tensors[idx]
			}
		}
		outs := make([]*Tensor, len(o.Outputs))
		for j, idx := range o.Outputs {
			outs[j] = in.tensors[idx]
		}
		if err := kernels[o.Op](o, ins, outs); err != nil {
			return fmt.Errorf("tflite: op %d (%s): %w", i, o.Op, err)
		}
	}
	return nil
}

type kernel func(o *OpDesc, in, out []*Tensor) error

var kernels map[Op]kernel

func activate(v float32, a Activation) float32 {
	switch a {
	case ActRelu:
		if v < 0 {
			return 0
		}
	case ActRelu1:
		if v < -1 {
			return -1
		}
		if v > 1 {
			return 1
		}
	case ActRelu6:
		if v < 0 {
			return 0
		}
		if v > 6 {
			return 6
		}
	case ActTanh:
		return float32(math.Tanh(float64(v)))
	}
	return v
}

// padFor is TFLite's padding split for SAME: the total padding is distributed with the smaller
// half before the data.
func padFor(stride, dilation, in, filter, out int) int {
	effective := (filter-1)*dilation + 1
	p := ((out-1)*stride + effective - in) / 2
	if p < 0 {
		return 0
	}
	return p
}

func outputSize(in, filter, stride, dilation int, samePad bool) int {
	if samePad {
		return (in + stride - 1) / stride
	}
	effective := (filter-1)*dilation + 1
	if in < effective {
		return 0
	}
	return (in-effective)/stride + 1
}
