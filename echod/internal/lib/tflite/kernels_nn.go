package tflite

import (
	"fmt"
	"math"

	"github.com/HuskerMinion/techo5/echod/internal/lib/vec"
)

func conv2D(o *OpDesc, in, out []*Tensor) error {
	p := o.conv
	x, w, y := in[0], in[1], out[0]
	if len(x.Shape) != 4 || len(w.Shape) != 4 {
		return fmt.Errorf("expected 4-D input and filter, got %v and %v", x.Shape, w.Shape)
	}

	batch, ih, iw, ic := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	oc, kh, kw := w.Shape[0], w.Shape[1], w.Shape[2]
	if kh < 1 || kw < 1 {
		return fmt.Errorf("a %dx%d filter", kh, kw)
	}
	if w.Shape[3] != ic {
		return fmt.Errorf("filter has %d input channels, input has %d", w.Shape[3], ic)
	}

	var bias []float32
	if len(in) > 2 && in[2] != nil {
		bias = in[2].F32
	}

	oh := outputSize(ih, kh, p.strideH, p.dilationH, p.samePad)
	ow := outputSize(iw, kw, p.strideW, p.dilationW, p.samePad)
	if tooMuch(batch, oh, ow, oc, kh, kw, ic) {
		return fmt.Errorf("an output of %dx%dx%dx%d from %dx%dx%d windows is more work than a model of this kind asks", batch, oh, ow, oc, kh, kw, ic)
	}
	y.resize([]int{batch, oh, ow, oc})

	padH, padW := 0, 0
	if p.samePad {
		padH = padFor(p.strideH, p.dilationH, ih, kh, oh)
		padW = padFor(p.strideW, p.dilationW, iw, kw, ow)
	}

	xd, wd, yd := x.F32, w.F32, y.F32
	yi := 0
	for b := 0; b < batch; b++ {
		for oy := 0; oy < oh; oy++ {
			for ox := 0; ox < ow; ox++ {
				for f := 0; f < oc; f++ {
					var acc float32
					if bias != nil {
						acc = bias[f]
					}
					for ky := 0; ky < kh; ky++ {
						iy := oy*p.strideH - padH + ky*p.dilationH
						if iy < 0 || iy >= ih {
							continue
						}

						// With a single input channel the channel axis is a dead loop and the
						// contiguous run is along the width instead, which is where the mel
						// model's 512-tap filters live.
						if ic == 1 && p.dilationW == 1 {
							x0 := ox*p.strideW - padW
							lo, hi := max(0, -x0), min(kw, iw-x0)
							if hi > lo {
								xo := (b*ih+iy)*iw + x0 + lo
								wo := (f*kh+ky)*kw + lo
								acc += vec.Dot(wd[wo:wo+hi-lo], xd[xo:xo+hi-lo])
							}
							continue
						}

						for kx := 0; kx < kw; kx++ {
							ix := ox*p.strideW - padW + kx*p.dilationW
							if ix < 0 || ix >= iw {
								continue
							}
							xo := ((b*ih+iy)*iw + ix) * ic
							wo := ((f*kh+ky)*kw + kx) * ic
							acc += vec.Dot(xd[xo:xo+ic], wd[wo:wo+ic])
						}
					}
					yd[yi] = activate(acc, p.act)
					yi++
				}
			}
		}
	}
	return nil
}

func maxPool2D(o *OpDesc, in, out []*Tensor) error {
	p := o.pool
	x, y := in[0], out[0]
	if len(x.Shape) != 4 {
		return fmt.Errorf("expected a 4-D input, got %v", x.Shape)
	}

	batch, ih, iw, c := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	if p.strideH < 1 || p.strideW < 1 || p.filterH < 1 || p.filterW < 1 {
		return fmt.Errorf("stride %dx%d, filter %dx%d", p.strideH, p.strideW, p.filterH, p.filterW)
	}
	oh := outputSize(ih, p.filterH, p.strideH, 1, p.samePad)
	ow := outputSize(iw, p.filterW, p.strideW, 1, p.samePad)
	if tooMuch(batch, oh, ow, c, p.filterH, p.filterW) {
		return fmt.Errorf("an output of %dx%dx%dx%d from %dx%d windows is more work than a model of this kind asks", batch, oh, ow, c, p.filterH, p.filterW)
	}
	y.resize([]int{batch, oh, ow, c})

	padH, padW := 0, 0
	if p.samePad {
		padH = padFor(p.strideH, 1, ih, p.filterH, oh)
		padW = padFor(p.strideW, 1, iw, p.filterW, ow)
	}

	xd, yd := x.F32, y.F32
	yi := 0
	for b := 0; b < batch; b++ {
		for oy := 0; oy < oh; oy++ {
			y0 := oy*p.strideH - padH
			for ox := 0; ox < ow; ox++ {
				x0 := ox*p.strideW - padW
				for ch := 0; ch < c; ch++ {
					best := float32(math.Inf(-1))
					for ky := 0; ky < p.filterH; ky++ {
						iy := y0 + ky
						if iy < 0 || iy >= ih {
							continue
						}
						for kx := 0; kx < p.filterW; kx++ {
							ix := x0 + kx
							if ix < 0 || ix >= iw {
								continue
							}
							if v := xd[((b*ih+iy)*iw+ix)*c+ch]; v > best {
								best = v
							}
						}
					}
					yd[yi] = activate(best, p.act)
					yi++
				}
			}
		}
	}
	return nil
}

func fullyConnected(o *OpDesc, in, out []*Tensor) error {
	x, w, y := in[0], in[1], out[0]
	if len(w.Shape) < 2 {
		return fmt.Errorf("expected 2-D weights, got %v", w.Shape)
	}

	inC := w.Shape[len(w.Shape)-1]
	oc := count(w.Shape[:len(w.Shape)-1])
	total := x.Count()
	if inC == 0 || total%inC != 0 {
		return fmt.Errorf("input of %d values does not divide into rows of %d", total, inC)
	}
	batch := total / inC

	var bias []float32
	if len(in) > 2 && in[2] != nil {
		bias = in[2].F32
	}

	y.resize([]int{batch, oc})
	act := o.act
	for b := 0; b < batch; b++ {
		row := x.F32[b*inC : (b+1)*inC]
		for f := 0; f < oc; f++ {
			acc := vec.Dot(row, w.F32[f*inC:(f+1)*inC])
			if bias != nil {
				acc += bias[f]
			}
			y.F32[b*oc+f] = activate(acc, act)
		}
	}
	return nil
}

func batchMatMul(o *OpDesc, in, out []*Tensor) error {
	a, b, y := in[0], in[1], out[0]
	adjX, adjY := o.adjX, o.adjY
	if len(a.Shape) < 2 || len(b.Shape) < 2 {
		return fmt.Errorf("expected at least 2-D inputs, got %v and %v", a.Shape, b.Shape)
	}

	m, k := a.Dim(-2), a.Dim(-1)
	if adjX {
		m, k = k, m
	}
	kb, n := b.Dim(-2), b.Dim(-1)
	if adjY {
		kb, n = n, kb
	}
	if k != kb {
		return fmt.Errorf("inner dimensions %d and %d do not match", k, kb)
	}

	batchShape, err := broadcastShape(a.Shape[:len(a.Shape)-2], b.Shape[:len(b.Shape)-2])
	if err != nil {
		return err
	}
	y.resize(append(append([]int{}, batchShape...), m, n))

	batches := count(batchShape)
	as := strides(a.Shape[:len(a.Shape)-2], batchShape)
	bs := strides(b.Shape[:len(b.Shape)-2], batchShape)
	idx := make([]int, len(batchShape))

	ao, bo := 0, 0
	for batch := 0; batch < batches; batch++ {
		am := a.F32[ao*m*k:]
		bm := b.F32[bo*kb*n:]
		ym := y.F32[batch*m*n:]

		for i := 0; i < m; i++ {
			for j := 0; j < n; j++ {
				var acc float32
				for p := 0; p < k; p++ {
					var av, bv float32
					if adjX {
						av = am[p*m+i]
					} else {
						av = am[i*k+p]
					}
					if adjY {
						bv = bm[j*kb+p]
					} else {
						bv = bm[p*n+j]
					}
					acc += av * bv
				}
				ym[i*n+j] = acc
			}
		}
		ao, bo = step(idx, batchShape, as, bs, ao, bo)
	}
	return nil
}

// step advances an odometer over shape and returns the two source offsets that go with the new
// position. Strides of zero are how a broadcast axis stays put.
func step(idx, shape, as, bs []int, ao, bo int) (int, int) {
	for d := len(shape) - 1; d >= 0; d-- {
		idx[d]++
		ao += as[d]
		bo += bs[d]
		if idx[d] < shape[d] {
			return ao, bo
		}
		idx[d] = 0
		ao -= as[d] * shape[d]
		bo -= bs[d] * shape[d]
	}
	return ao, bo
}

// maxWork is the most multiply-adds one operator may ask for: a damaged file can describe an output inside
// maxElements whose windows would take minutes, and the models these kernels run need a few million.
const maxWork = 1 << 28

// tooMuch is whether an operator whose outputs and windows have these sizes is past maxWork. The product
// is taken in floating point: in an int it can wrap round on the device, whose ints are 32 bits, to a
// small number or none, and a minutes-long operator would pass.
func tooMuch(dims ...int) bool {
	w := 1.0
	for _, d := range dims {
		if d < 0 {
			return true
		}
		w *= float64(d)
	}
	return w > maxWork
}

// depthwiseConv2D convolves each input channel with its own filters: depth multiplier of them, output
// channel c coming from input channel c / multiplier. The filter is [1, kh, kw, channels out].
func depthwiseConv2D(o *OpDesc, in, out []*Tensor) error {
	p := o.conv
	x, w, y := in[0], in[1], out[0]
	if len(x.Shape) != 4 || len(w.Shape) != 4 || w.Shape[0] != 1 {
		return fmt.Errorf("expected 4-D input and [1,h,w,c] filter, got %v and %v", x.Shape, w.Shape)
	}
	batch, ih, iw, ic := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	kh, kw, oc := w.Shape[1], w.Shape[2], w.Shape[3]
	if kh < 1 || kw < 1 {
		return fmt.Errorf("a %dx%d filter", kh, kw)
	}
	if ic == 0 || oc%ic != 0 || (p.depthMul != 0 && oc != ic*p.depthMul) {
		return fmt.Errorf("filter has %d channels for %d input channels, multiplier %d", oc, ic, p.depthMul)
	}
	mult := oc / ic
	if p.strideH < 1 || p.strideW < 1 || p.dilationH < 1 || p.dilationW < 1 {
		return fmt.Errorf("stride %dx%d, dilation %dx%d", p.strideH, p.strideW, p.dilationH, p.dilationW)
	}

	var bias []float32
	if len(in) > 2 && in[2] != nil {
		bias = in[2].F32
		if len(bias) != oc {
			return fmt.Errorf("bias has %d values for %d channels", len(bias), oc)
		}
	}

	oh := outputSize(ih, kh, p.strideH, p.dilationH, p.samePad)
	ow := outputSize(iw, kw, p.strideW, p.dilationW, p.samePad)
	if tooMuch(batch, oh, ow, oc, kh, kw) {
		return fmt.Errorf("an output of %dx%dx%dx%d from %dx%d windows is more work than a model of this kind asks", batch, oh, ow, oc, kh, kw)
	}
	y.resize([]int{batch, oh, ow, oc})

	padH, padW := 0, 0
	if p.samePad {
		padH = padFor(p.strideH, p.dilationH, ih, kh, oh)
		padW = padFor(p.strideW, p.dilationW, iw, kw, ow)
	}

	xd, wd, yd := x.F32, w.F32, y.F32
	acc := make([]float32, oc)
	yi := 0
	for b := 0; b < batch; b++ {
		for oy := 0; oy < oh; oy++ {
			for ox := 0; ox < ow; ox++ {
				if bias != nil {
					copy(acc, bias)
				} else {
					clear(acc)
				}
				for ky := 0; ky < kh; ky++ {
					iy := oy*p.strideH - padH + ky*p.dilationH
					if iy < 0 || iy >= ih {
						continue
					}
					for kx := 0; kx < kw; kx++ {
						ix := ox*p.strideW - padW + kx*p.dilationW
						if ix < 0 || ix >= iw {
							continue
						}
						xs := xd[((b*ih+iy)*iw+ix)*ic:][:ic]
						ws := wd[(ky*kw+kx)*oc:][:oc]
						if mult == 1 {
							for c, v := range xs {
								acc[c] += v * ws[c]
							}
							continue
						}
						for c := range oc {
							acc[c] += xs[c/mult] * ws[c]
						}
					}
				}
				for c := range oc {
					yd[yi] = activate(acc[c], p.act)
					yi++
				}
			}
		}
	}
	return nil
}

// averagePool2D is the mean over each window, of the cells inside the input only, as TFLite counts it.
func averagePool2D(o *OpDesc, in, out []*Tensor) error {
	p := o.pool
	x, y := in[0], out[0]
	if len(x.Shape) != 4 {
		return fmt.Errorf("expected a 4-D input, got %v", x.Shape)
	}

	batch, ih, iw, c := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	if p.strideH < 1 || p.strideW < 1 || p.filterH < 1 || p.filterW < 1 {
		return fmt.Errorf("stride %dx%d, filter %dx%d", p.strideH, p.strideW, p.filterH, p.filterW)
	}
	oh := outputSize(ih, p.filterH, p.strideH, 1, p.samePad)
	ow := outputSize(iw, p.filterW, p.strideW, 1, p.samePad)
	if tooMuch(batch, oh, ow, c, p.filterH, p.filterW) {
		return fmt.Errorf("an output of %dx%dx%dx%d from %dx%d windows is more work than a model of this kind asks", batch, oh, ow, c, p.filterH, p.filterW)
	}
	y.resize([]int{batch, oh, ow, c})

	padH, padW := 0, 0
	if p.samePad {
		padH = padFor(p.strideH, 1, ih, p.filterH, oh)
		padW = padFor(p.strideW, 1, iw, p.filterW, ow)
	}

	xd, yd := x.F32, y.F32
	yi := 0
	for b := 0; b < batch; b++ {
		for oy := 0; oy < oh; oy++ {
			y0 := oy*p.strideH - padH
			for ox := 0; ox < ow; ox++ {
				x0 := ox*p.strideW - padW
				for ch := 0; ch < c; ch++ {
					var sum float32
					n := 0
					for ky := 0; ky < p.filterH; ky++ {
						iy := y0 + ky
						if iy < 0 || iy >= ih {
							continue
						}
						for kx := 0; kx < p.filterW; kx++ {
							ix := x0 + kx
							if ix < 0 || ix >= iw {
								continue
							}
							sum += xd[((b*ih+iy)*iw+ix)*c+ch]
							n++
						}
					}
					if n > 0 {
						sum /= float32(n)
					}
					yd[yi] = activate(sum, p.act)
					yi++
				}
			}
		}
	}
	return nil
}

// softmax is exp(beta * x) normalized over the last axis.
func softmax(o *OpDesc, in, out []*Tensor) error {
	x, y := in[0], out[0]
	if len(x.Shape) == 0 {
		return fmt.Errorf("softmax of a scalar")
	}
	n := x.Shape[len(x.Shape)-1]
	y.resize(x.Shape)
	if n == 0 {
		return nil
	}
	xd, yd := x.F32, y.F32
	for start := 0; start+n <= len(xd); start += n {
		row, dst := xd[start:start+n], yd[start:start+n]
		top := row[0]
		for _, v := range row {
			top = max(top, v)
		}
		var sum float64
		for i, v := range row {
			e := math.Exp(float64(o.beta * (v - top)))
			dst[i] = float32(e)
			sum += e
		}
		for i := range dst {
			dst[i] = float32(float64(dst[i]) / sum)
		}
	}
	return nil
}
