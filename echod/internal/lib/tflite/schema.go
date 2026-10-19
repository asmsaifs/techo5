package tflite

import (
	"errors"
	"fmt"
)

// Field slots of the schema tables we read. Slot numbers are part of the file format: they are
// assigned by field order in schema.fbs and never reused, so appended fields do not disturb them.
const (
	modelOperatorCodes = 1
	modelSubgraphs     = 2
	modelDescription   = 3
	modelBuffers       = 4

	opcodeDeprecatedBuiltin = 0
	opcodeCustom            = 1
	opcodeBuiltin           = 3

	subgraphTensors   = 0
	subgraphInputs    = 1
	subgraphOutputs   = 2
	subgraphOperators = 3
	subgraphName      = 4

	tensorShape          = 0
	tensorType           = 1
	tensorBuffer         = 2
	tensorName           = 3
	tensorQuantization   = 4
	tensorIsVariable     = 5
	tensorShapeSignature = 7

	operatorOpcodeIndex = 0
	operatorInputs      = 1
	operatorOutputs     = 2
	operatorOptions     = 4
	operatorCustom      = 5

	quantMin       = 0
	quantMax       = 1
	quantScale     = 2
	quantZeroPoint = 3
	quantDim       = 6

	bufferData   = 0
	bufferOffset = 1
)

// TensorType is the schema's TensorType enum.
type TensorType uint8

const (
	Float32 TensorType = 0
	Int32   TensorType = 2
	Int64   TensorType = 4
	UInt8   TensorType = 3
	Bool    TensorType = 6
	Int8    TensorType = 9
)

func (t TensorType) String() string {
	switch t {
	case Float32:
		return "float32"
	case Int32:
		return "int32"
	case Int64:
		return "int64"
	case Bool:
		return "bool"
	case Int8:
		return "int8"
	case UInt8:
		return "uint8"
	}
	return fmt.Sprintf("type%d", uint8(t))
}

// Op is the schema's BuiltinOperator enum, limited to the codes this package names.
type Op int32

const (
	OpAdd            Op = 0
	OpAveragePool2D  Op = 1
	OpConv2D         Op = 3
	OpDepthwiseConv  Op = 4
	OpFullyConnected Op = 9
	OpLogistic       Op = 14
	OpMaxPool2D      Op = 17
	OpMul            Op = 18
	OpRelu           Op = 19
	OpRelu6          Op = 21
	OpReshape        Op = 22
	OpSoftmax        Op = 25
	OpConcatenation  Op = 2
	OpPad            Op = 34
	OpTranspose      Op = 39
	OpMean           Op = 40
	OpSub            Op = 41
	OpDiv            Op = 42
	OpSqueeze        Op = 43
	OpStridedSlice   Op = 45
	OpExp            Op = 47
	OpMaximum        Op = 55
	OpMinimum        Op = 57
	OpExpandDims     Op = 70
	OpLog            Op = 73
	OpSum            Op = 74
	OpSqrt           Op = 75
	OpRsqrt          Op = 76
	OpShape          Op = 77
	OpReduceProd     Op = 81
	OpReduceMax      Op = 82
	OpPack           Op = 83
	OpSquare         Op = 92
	OpFill           Op = 94
	OpLeakyRelu      Op = 98
	OpSquaredDiff    Op = 99
	OpSplitV         Op = 102
	OpQuantize       Op = 114
	OpBatchMatMul    Op = 126
	OpCallOnce       Op = 129
	OpVarHandle      Op = 142
	OpReadVariable   Op = 143
	OpAssignVariable Op = 144
)

var opNames = map[Op]string{
	OpAdd: "ADD", OpAveragePool2D: "AVERAGE_POOL_2D", OpConcatenation: "CONCATENATION", OpConv2D: "CONV_2D",
	OpDepthwiseConv: "DEPTHWISE_CONV_2D", OpFullyConnected: "FULLY_CONNECTED",
	OpLogistic: "LOGISTIC", OpMaxPool2D: "MAX_POOL_2D", OpMul: "MUL", OpRelu: "RELU",
	OpRelu6: "RELU6", OpReshape: "RESHAPE", OpSoftmax: "SOFTMAX", OpPad: "PAD",
	OpTranspose: "TRANSPOSE", OpMean: "MEAN", OpSub: "SUB", OpDiv: "DIV",
	OpSqueeze: "SQUEEZE", OpStridedSlice: "STRIDED_SLICE", OpExp: "EXP",
	OpMaximum: "MAXIMUM", OpMinimum: "MINIMUM", OpExpandDims: "EXPAND_DIMS",
	OpLog: "LOG", OpSum: "SUM", OpSqrt: "SQRT", OpRsqrt: "RSQRT",
	OpReduceMax: "REDUCE_MAX", OpSquare: "SQUARE", OpLeakyRelu: "LEAKY_RELU",
	OpShape: "SHAPE", OpReduceProd: "REDUCE_PROD", OpPack: "PACK", OpFill: "FILL",
	OpSquaredDiff: "SQUARED_DIFFERENCE", OpBatchMatMul: "BATCH_MATMUL",
	OpSplitV: "SPLIT_V", OpQuantize: "QUANTIZE", OpCallOnce: "CALL_ONCE",
	OpVarHandle: "VAR_HANDLE", OpReadVariable: "READ_VARIABLE", OpAssignVariable: "ASSIGN_VARIABLE",
}

func (o Op) String() string {
	if n, ok := opNames[o]; ok {
		return n
	}
	return fmt.Sprintf("OP_%d", int32(o))
}

// Activation is the schema's ActivationFunctionType enum.
type Activation uint8

const (
	ActNone Activation = iota
	ActRelu
	ActRelu1
	ActRelu6
	ActTanh
	ActSignBit
)

// Model is a parsed .tflite file. Weight data is referenced in place, so the byte slice handed
// to Parse must outlive the model.
type Model struct {
	Description string
	Subgraphs   []*Subgraph

	buffers []vector
}

// BufferSizes is the length of each constant buffer's data, in bytes, in the file's order.
func (m *Model) BufferSizes() []int {
	out := make([]int, len(m.buffers))
	for i, v := range m.buffers {
		out[i] = v.len()
	}
	return out
}

type Subgraph struct {
	Name    string
	Tensors []*TensorDesc
	Ops     []*OpDesc
	Inputs  []int
	Outputs []int
}

type TensorDesc struct {
	Name string
	Type TensorType
	// Shape is the shape recorded at conversion time. Dimensions the signature marks dynamic
	// hold whatever value the converter traced, so they are a starting point, not a promise.
	Shape    []int
	Dynamic  []bool
	Buffer   int
	Variable bool

	// Scale, ZeroPoint and QuantDim are a quantized tensor's parameters: one scale and zero point for
	// the whole tensor, or one of each per channel along QuantDim. Empty for a float tensor.
	Scale     []float32
	ZeroPoint []int64
	QuantDim  int
}

// OpDesc is one operator with its builtin options already decoded. Options are read at load so
// that a kernel invoked every audio frame is not walking flatbuffer vtables to find its stride.
type OpDesc struct {
	Op      Op
	Custom  string
	Inputs  []int
	Outputs []int

	conv     convParams
	pool     poolParams
	act      Activation
	keepDims bool
	alpha    float32
	beta     float32 // softmax
	dims     []int
	count    int
	axis     int

	beginMask    int
	endMask      int
	ellipsisMask int
	newAxisMask  int
	shrinkMask   int

	adjX bool
	adjY bool

	init int
}

// InitSubgraph is the subgraph a CALL_ONCE runs, and zero for any other operator.
func (o *OpDesc) InitSubgraph() int { return o.init }

var errShort = errors.New("tflite: buffer too short")

// Parse reads a model. Malformed input reaches the accessors as out-of-range indexing, so the
// panic is converted rather than checked at every offset.
func Parse(b []byte) (m *Model, err error) {
	if len(b) < 8 {
		return nil, errShort
	}
	defer func() {
		if r := recover(); r != nil {
			m, err = nil, fmt.Errorf("tflite: malformed model: %v", r)
		}
	}()

	data := buf(b)
	r := root(data)

	codes := r.vec(modelOperatorCodes, 4)
	m = &Model{Description: r.str(modelDescription)}

	for i, bufs := 0, r.vec(modelBuffers, 4); i < bufs.len(); i++ {
		t := bufs.table(i)
		if t.u64f(bufferOffset, 0) != 0 {
			return nil, fmt.Errorf("tflite: buffer %d is stored outside the file", i)
		}
		m.buffers = append(m.buffers, t.vec(bufferData, 1))
	}

	for i, subs := 0, r.vec(modelSubgraphs, 4); i < subs.len(); i++ {
		st := subs.table(i)
		g := &Subgraph{
			Name:    st.str(subgraphName),
			Inputs:  st.vec(subgraphInputs, 4).ints(),
			Outputs: st.vec(subgraphOutputs, 4).ints(),
		}

		for j, ts := 0, st.vec(subgraphTensors, 4); j < ts.len(); j++ {
			tt := ts.table(j)
			d := &TensorDesc{
				Name:     tt.str(tensorName),
				Type:     TensorType(tt.u8f(tensorType, 0)),
				Shape:    tt.vec(tensorShape, 4).ints(),
				Buffer:   int(tt.u32f(tensorBuffer, 0)),
				Variable: tt.boolf(tensorIsVariable, false),
			}
			if q, ok := tt.table(tensorQuantization); ok {
				// The range is not used here, but it is a vector other readers of the same file
				// allocate from, so it is held to the same bounds.
				q.vec(quantMin, 4)
				q.vec(quantMax, 4)
				d.Scale = q.vec(quantScale, 4).floats()
				d.ZeroPoint = q.vec(quantZeroPoint, 8).int64s()
				d.QuantDim = int(q.i32f(quantDim, 0))
			}
			d.Dynamic = make([]bool, len(d.Shape))
			if sig := tt.vec(tensorShapeSignature, 4); sig.len() == len(d.Shape) {
				for k := range d.Shape {
					d.Dynamic[k] = sig.i32(k) < 0
				}
			}
			g.Tensors = append(g.Tensors, d)
		}

		for j, os := 0, st.vec(subgraphOperators, 4); j < os.len(); j++ {
			ot := os.table(j)
			idx := int(ot.u32f(operatorOpcodeIndex, 0))
			if idx >= codes.len() {
				return nil, fmt.Errorf("tflite: operator %d has opcode index %d of %d", j, idx, codes.len())
			}
			ct := codes.table(idx)
			o := &OpDesc{
				Op:      opcode(ct),
				Custom:  ct.str(opcodeCustom),
				Inputs:  ot.vec(operatorInputs, 4).ints(),
				Outputs: ot.vec(operatorOutputs, 4).ints(),
			}
			o.decodeOptions(ot.table(operatorOptions))
			ot.vec(operatorCustom, 1) // the same: read elsewhere, bounded here
			g.Ops = append(g.Ops, o)
		}

		m.Subgraphs = append(m.Subgraphs, g)
	}

	if len(m.Subgraphs) == 0 {
		return nil, errors.New("tflite: model has no subgraphs")
	}
	return m, nil
}

// opcode is the builtin an OperatorCode names. Models written before builtin_code widened to int32
// carry the code in the deprecated byte field only, so that one counts when builtin_code is absent from
// the table, and not when it is there and zero: ADD is genuinely zero, and the engine that runs the
// model (microwakeword's interpreter) reads it the same way. Taking a zero for absent made a model
// that is ADD to the engine some other op here.
func opcode(ct table) Op {
	if ct.slot(opcodeBuiltin) != 0 {
		return Op(ct.i32f(opcodeBuiltin, 0))
	}
	return Op(ct.u8f(opcodeDeprecatedBuiltin, 0))
}

// buffer returns the constant data for a tensor, or nil when the tensor is computed.
func (m *Model) buffer(i int) []byte {
	if i <= 0 || i >= len(m.buffers) {
		return nil
	}
	return m.buffers[i].bytes()
}

type convParams struct {
	samePad   bool
	strideW   int
	strideH   int
	dilationW int
	dilationH int
	depthMul  int // depthwise only: output channels per input channel, 0 when the file does not say
	act       Activation
}

// decodeOptions reads one operator's builtin options. Slot numbers are per options type and
// mean nothing outside the operator that uses them, hence the switch.
func (o *OpDesc) decodeOptions(t table, present bool) {
	o.conv = convParams{strideW: 1, strideH: 1, dilationW: 1, dilationH: 1}
	o.pool = poolParams{strideW: 1, strideH: 1, filterW: 1, filterH: 1}
	o.beta = 1
	if !present {
		return
	}

	switch o.Op {
	case OpDepthwiseConv:
		// Its own slots: the depth multiplier comes before the activation.
		o.conv.samePad = t.u8f(0, 0) == 0
		o.conv.strideW = int(t.i32f(1, 1))
		o.conv.strideH = int(t.i32f(2, 1))
		o.conv.depthMul = int(t.i32f(3, 0))
		o.conv.act = Activation(t.u8f(4, 0))
		o.conv.dilationW = int(t.i32f(5, 1))
		o.conv.dilationH = int(t.i32f(6, 1))

	case OpConv2D:
		o.conv.samePad = t.u8f(0, 0) == 0
		o.conv.strideW = int(t.i32f(1, 1))
		o.conv.strideH = int(t.i32f(2, 1))
		o.conv.act = Activation(t.u8f(3, 0))
		o.conv.dilationW = int(t.i32f(4, 1))
		o.conv.dilationH = int(t.i32f(5, 1))

	case OpSoftmax:
		o.beta = t.f32f(0, 1)

	case OpMaxPool2D, OpAveragePool2D:
		o.pool.samePad = t.u8f(0, 0) == 0
		o.pool.strideW = int(t.i32f(1, 1))
		o.pool.strideH = int(t.i32f(2, 1))
		o.pool.filterW = int(t.i32f(3, 1))
		o.pool.filterH = int(t.i32f(4, 1))
		o.pool.act = Activation(t.u8f(5, 0))

	case OpAdd, OpSub, OpMul, OpDiv, OpFullyConnected, OpConcatenation:
		o.act = Activation(t.u8f(0, 0))

	case OpMean, OpSum, OpReduceMax, OpReduceProd:
		o.keepDims = t.boolf(0, false)

	case OpPack:
		o.count = int(t.i32f(0, 0))
		o.axis = int(t.i32f(1, 0))

	case OpStridedSlice:
		o.beginMask = int(t.i32f(0, 0))
		o.endMask = int(t.i32f(1, 0))
		o.ellipsisMask = int(t.i32f(2, 0))
		o.newAxisMask = int(t.i32f(3, 0))
		o.shrinkMask = int(t.i32f(4, 0))

	case OpLeakyRelu:
		o.alpha = t.f32f(0, 0)

	case OpReshape, OpSqueeze:
		o.dims = t.vec(0, 4).ints()

	case OpBatchMatMul:
		o.adjX = t.boolf(0, false)
		o.adjY = t.boolf(1, false)

	case OpCallOnce:
		o.init = int(t.i32f(0, 0))
	}
}

type poolParams struct {
	samePad bool
	strideW int
	strideH int
	filterW int
	filterH int
	act     Activation
}
