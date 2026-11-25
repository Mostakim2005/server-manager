//go:build windows

package icons

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	gdi32              = windows.NewLazySystemDLL("gdi32.dll")
	procPrivateExtract = user32.NewProc("PrivateExtractIconsW")
	procGetIconInfo    = user32.NewProc("GetIconInfo")
	procDestroyIcon    = user32.NewProc("DestroyIcon")
	procGetObjectW     = gdi32.NewProc("GetObjectW")
	procCreateCompatDC = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC       = gdi32.NewProc("DeleteDC")
	procDeleteObject   = gdi32.NewProc("DeleteObject")
	procGetDIBits      = gdi32.NewProc("GetDIBits")
)

type winIconInfo struct {
	fIcon    uint32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

type winBitmap struct {
	bmType       int32
	bmWidth      int32
	bmHeight     int32
	bmWidthBytes int32
	bmPlanes     uint16
	bmBitsPixel  uint16
	bmBits       uintptr
}

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

type bitmapInfo struct {
	bmiHeader bitmapInfoHeader
	bmiColors [1]uint32
}

func extractExecutableIconPNG(exePath string) ([]byte, error) {
	pathPtr, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return nil, err
	}
	var hIcon uintptr
	var iconID uint32
	const iconSize = 48
	ret, _, callErr := procPrivateExtract.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(iconSize),
		uintptr(iconSize),
		uintptr(unsafe.Pointer(&hIcon)),
		uintptr(unsafe.Pointer(&iconID)),
		1,
		0,
	)
	if ret == 0 || hIcon == 0 {
		return nil, fmt.Errorf("PrivateExtractIconsW failed: %v", callErr)
	}
	defer procDestroyIcon.Call(hIcon)

	var ii winIconInfo
	ok, _, err := procGetIconInfo.Call(hIcon, uintptr(unsafe.Pointer(&ii)))
	if ok == 0 {
		return nil, fmt.Errorf("GetIconInfo failed: %v", err)
	}
	if ii.hbmMask != 0 {
		defer procDeleteObject.Call(ii.hbmMask)
	}
	if ii.hbmColor != 0 {
		defer procDeleteObject.Call(ii.hbmColor)
	}
	if ii.hbmColor == 0 {
		return nil, fmt.Errorf("monochrome icon unsupported")
	}

	var bm winBitmap
	if n, _, _ := procGetObjectW.Call(ii.hbmColor, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); n == 0 {
		return nil, fmt.Errorf("GetObjectW failed")
	}
	w, h := int(bm.bmWidth), int(bm.bmHeight)
	if w <= 0 || h <= 0 || w > 512 || h > 512 {
		return nil, fmt.Errorf("unexpected icon dimensions %dx%d", w, h)
	}

	hdc, _, _ := procCreateCompatDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(hdc)

	colorBits := make([]byte, w*h*4)
	bmi := bitmapInfo{
		bmiHeader: bitmapInfoHeader{
			biSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			biWidth:       int32(w),
			biHeight:      -int32(h),
			biPlanes:      1,
			biBitCount:    32,
			biCompression: 0, // BI_RGB
		},
	}
	if rows, _, _ := procGetDIBits.Call(hdc, ii.hbmColor, 0, uintptr(h), uintptr(unsafe.Pointer(&colorBits[0])), uintptr(unsafe.Pointer(&bmi)), 0); rows == 0 {
		return nil, fmt.Errorf("GetDIBits color failed")
	}

	var maskBits []byte
	if ii.hbmMask != 0 && !hasAnyAlpha(colorBits) {
		maskBits = make([]byte, w*h*4)
		_, _, _ = procGetDIBits.Call(hdc, ii.hbmMask, 0, uintptr(h), uintptr(unsafe.Pointer(&maskBits[0])), uintptr(unsafe.Pointer(&bmi)), 0)
	}

	img := bgraToNRGBA(w, h, colorBits, maskBits)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func hasAnyAlpha(bgra []byte) bool {
	for i := 3; i < len(bgra); i += 4 {
		if bgra[i] != 0 {
			return true
		}
	}
	return false
}

func bgraToNRGBA(w, h int, colorBits, maskBits []byte) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	useMask := len(maskBits) == len(colorBits)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := (y*w + x) * 4
			a := colorBits[idx+3]
			if useMask {
				if maskBits[idx] == 0 && maskBits[idx+1] == 0 && maskBits[idx+2] == 0 {
					a = 255
				} else {
					a = 0
				}
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: colorBits[idx+2],
				G: colorBits[idx+1],
				B: colorBits[idx],
				A: a,
			})
		}
	}
	return img
}
