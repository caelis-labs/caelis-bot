package desktop

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Explicit capture only. GDI works on the supported Windows 10/Server desktop
// baseline without a newer Windows Graphics Capture API or a permission reset.
func windowsScreenPixels(bounds image.Rectangle) (*image.NRGBA, error) {
	w, h := bounds.Dx(), bounds.Dy()
	if w < 1 || h < 1 || int64(w)*int64(h) > 16<<20 {
		return nil, errors.New("screen image is too large")
	}
	dc, _, _ := winUser.NewProc("GetDC").Call(0)
	if dc == 0 {
		return nil, errors.New("interactive desktop unavailable")
	}
	defer winUser.NewProc("ReleaseDC").Call(0, dc)
	memory, _, _ := winGDI.NewProc("CreateCompatibleDC").Call(dc)
	if memory == 0 {
		return nil, errors.New("screen capture unavailable")
	}
	defer winGDI.NewProc("DeleteDC").Call(memory)
	bitmap, _, _ := winGDI.NewProc("CreateCompatibleBitmap").Call(dc, uintptr(w), uintptr(h))
	if bitmap == 0 {
		return nil, errors.New("screen bitmap unavailable")
	}
	defer winGDI.NewProc("DeleteObject").Call(bitmap)
	previous, _, _ := winGDI.NewProc("SelectObject").Call(memory, bitmap)
	ok, _, _ := winGDI.NewProc("BitBlt").Call(memory, 0, 0, uintptr(w), uintptr(h), dc, uintptr(bounds.Min.X), uintptr(bounds.Min.Y), 0x00CC0020|0x40000000)
	winGDI.NewProc("SelectObject").Call(memory, previous) // GetDIBits requires deselection.
	if ok == 0 {
		return nil, errors.New("screen capture failed")
	}
	data := make([]byte, 40+w*h*4)
	binary.LittleEndian.PutUint32(data, 40)
	binary.LittleEndian.PutUint32(data[4:], uint32(w))
	binary.LittleEndian.PutUint32(data[8:], uint32(int32(-h)))
	binary.LittleEndian.PutUint16(data[12:], 1)
	binary.LittleEndian.PutUint16(data[14:], 32)
	n, _, _ := winGDI.NewProc("GetDIBits").Call(dc, bitmap, 0, uintptr(h), uintptr(unsafe.Pointer(&data[40])), uintptr(unsafe.Pointer(&data[0])), 0)
	if n != uintptr(h) {
		return nil, errors.New("screen pixels unavailable")
	}
	result := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		s, t := 40+i*4, i*4
		result.Pix[t] = data[s+2]
		result.Pix[t+1] = data[s+1]
		result.Pix[t+2] = data[s]
		result.Pix[t+3] = 255
	}
	return result, nil
}

func windowsImageClipboard(hwnd uintptr, data []byte) bool {
	im, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return false
	}
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()
	if int64(w)*int64(h) > 16<<20 {
		return false
	}
	stride := (w*3 + 3) &^ 3
	dib := make([]byte, 40+stride*h)
	binary.LittleEndian.PutUint32(dib, 40)
	binary.LittleEndian.PutUint32(dib[4:], uint32(w))
	binary.LittleEndian.PutUint32(dib[8:], uint32(h))
	binary.LittleEndian.PutUint16(dib[12:], 1)
	binary.LittleEndian.PutUint16(dib[14:], 24)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := im.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := 40 + (h-1-y)*stride + x*3
			dib[i] = byte(bl >> 8)
			dib[i+1] = byte(g >> 8)
			dib[i+2] = byte(r >> 8)
		}
	}
	if ok, _, _ := winUser.NewProc("OpenClipboard").Call(hwnd); ok == 0 {
		return false
	}
	defer winUser.NewProc("CloseClipboard").Call()
	if ok, _, _ := winUser.NewProc("EmptyClipboard").Call(); ok == 0 {
		return false
	}
	set := func(format uintptr, b []byte) bool {
		k := windows.NewLazySystemDLL("kernel32.dll")
		h, _, _ := k.NewProc("GlobalAlloc").Call(2, uintptr(len(b)))
		if h == 0 {
			return false
		}
		p, _, _ := k.NewProc("GlobalLock").Call(h)
		if p == 0 {
			k.NewProc("GlobalFree").Call(h)
			return false
		}
		// Keep the OS-owned address opaque; copy via the documented memory API.
		windows.NewLazySystemDLL("ntdll.dll").NewProc("RtlMoveMemory").Call(p, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
		k.NewProc("GlobalUnlock").Call(h)
		ok, _, _ := winUser.NewProc("SetClipboardData").Call(format, h)
		if ok == 0 {
			k.NewProc("GlobalFree").Call(h)
			return false
		}
		return true
	}
	name, _ := windows.UTF16PtrFromString("PNG")
	format, _, _ := winUser.NewProc("RegisterClipboardFormatW").Call(uintptr(unsafe.Pointer(name)))
	standard := set(8, dib)
	custom := format != 0 && set(format, data)
	return standard || custom
}
