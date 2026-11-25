package branding

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
)

//go:embed logo.svg
var svgBytes []byte

// DefaultICOSizes lists the multi-DPI icon dimensions bundled into ServerManager.ico.
var DefaultICOSizes = []int{16, 20, 24, 32, 48, 256}

// SVG returns the raw embedded vector icon bytes.
func SVG() []byte {
	out := make([]byte, len(svgBytes))
	copy(out, svgBytes)
	return out
}

// RenderNRGBA renders the anti-aliased Server Manager badge icon matching logo.svg.
func RenderNRGBA(size int) *image.NRGBA {
	if size < 8 {
		size = 8
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const samples = 4
	invSamples := 1.0 / float64(samples*samples)

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var rSum, gSum, bSum, aSum float64
			for sy := 0; sy < samples; sy++ {
				ny := (float64(y) + (float64(sy)+0.5)/float64(samples)) / float64(size)
				for sx := 0; sx < samples; sx++ {
					nx := (float64(x) + (float64(sx)+0.5)/float64(samples)) / float64(size)
					sr, sg, sb, sa := sampleLogo(nx, ny)
					rSum += sr * sa
					gSum += sg * sa
					bSum += sb * sa
					aSum += sa
				}
			}
			aAvg := aSum * invSamples
			if aAvg <= 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: clampByte((rSum * invSamples) / aAvg),
				G: clampByte((gSum * invSamples) / aAvg),
				B: clampByte((bSum * invSamples) / aAvg),
				A: clampByte(aAvg * 255.0),
			})
		}
	}
	return img
}

// PNG encodes the Server Manager badge logo as a square transparent PNG.
func PNG(size int) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, RenderNRGBA(size)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ICO packages PNG-compressed icon frames into a valid Windows .ico file.
func ICO(sizes ...int) ([]byte, error) {
	if len(sizes) == 0 {
		sizes = DefaultICOSizes
	}
	pngs := make([][]byte, 0, len(sizes))
	for _, s := range sizes {
		if s < 16 || s > 256 {
			return nil, fmt.Errorf("invalid ICO size %d (must be 16..256)", s)
		}
		p, err := PNG(s)
		if err != nil {
			return nil, err
		}
		pngs = append(pngs, p)
	}

	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0)) // reserved
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // icon type
	_ = binary.Write(&buf, binary.LittleEndian, uint16(len(sizes)))

	offset := uint32(6 + 16*len(sizes))
	for i, s := range sizes {
		dim := byte(s)
		if s == 256 {
			dim = 0
		}
		buf.WriteByte(dim)
		buf.WriteByte(dim)
		buf.WriteByte(0)
		buf.WriteByte(0)
		_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(32))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(pngs[i])))
		_ = binary.Write(&buf, binary.LittleEndian, offset)
		offset += uint32(len(pngs[i]))
	}
	for _, p := range pngs {
		buf.Write(p)
	}
	return buf.Bytes(), nil
}

func sampleLogo(x, y float64) (r, g, b, a float64) {
	dBox := sdRoundRect(x, y, 0.5, 0.5, 0.445, 0.445, 0.21)
	if dBox > 0 {
		return 0, 0, 0, 0
	}

	// Slate-navy (#0F172A) -> Cobalt (#1D4ED8) -> Sky (#0284C7) gradient.
	t := clamp01(0.42*x + 0.58*y)
	if t < 0.55 {
		u := t / 0.55
		r = lerp(15, 29, u)
		g = lerp(23, 78, u)
		b = lerp(42, 216, u)
	} else {
		u := (t - 0.55) / 0.45
		r = lerp(29, 2, u)
		g = lerp(78, 132, u)
		b = lerp(216, 199, u)
	}

	// Crisp blue-cyan rim border.
	if dBox > -0.022 {
		rim := clamp01((dBox+0.022)/0.022) * 0.45
		r = lerp(r, 96, rim)
		g = lerp(g, 195, rim)
		b = lerp(b, 250, rim)
	}

	// Three server rack blades matching logo.svg proportions.
	blades := []struct {
		cx, cy, bx, by, rad float64
		ledR, ledG, ledB    float64
		barW                float64
	}{
		{0.50, 0.281, 0.3125, 0.070, 0.043, 16, 185, 129, 0.273},
		{0.50, 0.469, 0.3125, 0.070, 0.043, 14, 165, 233, 0.344},
		{0.418, 0.656, 0.2305, 0.070, 0.043, 34, 211, 238, 0.211},
	}

	for _, bl := range blades {
		if sdRoundRect(x, y, bl.cx, bl.cy, bl.bx, bl.by, bl.rad) <= 0 {
			r, g, b = 246, 250, 255
			if sdCircle(x, y, 0.277, bl.cy, 0.025) <= 0 {
				r, g, b = bl.ledR, bl.ledG, bl.ledB
			} else if sdRoundRect(x, y, 0.352+bl.barW*0.5, bl.cy, bl.barW*0.5, 0.0175, 0.0175) <= 0 {
				r, g, b = 44, 82, 166
			} else if bl.cx > 0.45 && sdCircle(x, y, 0.730, bl.cy, 0.0175) <= 0 {
				r, g, b = 16, 185, 129
			}
		}
	}

	// Bottom-right Launch / Control Orb.
	dOrbOuter := sdCircle(x, y, 0.727, 0.727, 0.148)
	if dOrbOuter <= 0 {
		dOrbRing := sdCircle(x, y, 0.727, 0.727, 0.129)
		if dOrbRing > 0 {
			return 15, 23, 42, 1
		}
		if dOrbRing > -0.012 {
			return 167, 243, 208, 1
		}
		u := clamp01((x - 0.60) / 0.25)
		r = lerp(16, 5, u)
		g = lerp(185, 150, u)
		b = lerp(129, 105, u)
		if pointInTriangle(x, y, 0.691, 0.666, 0.789, 0.727, 0.691, 0.788) {
			return 255, 255, 255, 1
		}
		return r, g, b, 1
	}

	return r, g, b, 1
}

func pointInTriangle(px, py, ax, ay, bx, by, cx, cy float64) bool {
	d1 := (px-bx)*(ay-by) - (ax-bx)*(py-by)
	d2 := (px-cx)*(by-cy) - (bx-cx)*(py-cy)
	d3 := (px-ax)*(cy-ay) - (cx-ax)*(py-ay)
	hasNeg := (d1 < 0) || (d2 < 0) || (d3 < 0)
	hasPos := (d1 > 0) || (d2 > 0) || (d3 > 0)
	return !(hasNeg && hasPos)
}

func sdRoundRect(px, py, cx, cy, bx, by, r float64) float64 {
	qx := math.Abs(px-cx) - (bx - r)
	qy := math.Abs(py-cy) - (by - r)
	ox := math.Max(qx, 0)
	oy := math.Max(qy, 0)
	return math.Hypot(ox, oy) + math.Min(math.Max(qx, qy), 0) - r
}

func sdCircle(px, py, cx, cy, r float64) float64 {
	return math.Hypot(px-cx, py-cy) - r
}

func lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}
