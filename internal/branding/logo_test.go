package branding

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
)

func TestSVGEmbedded(t *testing.T) {
	svg := string(SVG())
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "playOrb") {
		t.Fatalf("unexpected embedded SVG content")
	}
}

func TestPNGAndICOGen(t *testing.T) {
	pngBytes, err := PNG(64)
	if err != nil {
		t.Fatalf("PNG(64) failed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("decode PNG failed: %v", err)
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
		t.Fatalf("unexpected bounds: %v", img.Bounds())
	}

	icoBytes, err := ICO()
	if err != nil {
		t.Fatalf("ICO() failed: %v", err)
	}
	if len(icoBytes) < 6+16*len(DefaultICOSizes) {
		t.Fatalf("ICO unexpectedly short: %d", len(icoBytes))
	}
	if icoBytes[2] != 1 || icoBytes[4] != byte(len(DefaultICOSizes)) {
		t.Fatalf("invalid ICO header: %v", icoBytes[:6])
	}
}
