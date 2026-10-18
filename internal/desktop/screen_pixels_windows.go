package desktop

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"runtime"
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
	// GetDC/ReleaseDC and GDI's drawing batch belong to one OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dc, _, _ := winUser.NewProc("GetDC").Call(0)
	if dc == 0 {
		return nil, errors.New("interactive desktop unavailable")
	}
	defer winUser.NewProc("ReleaseDC").Call(0, dc)
	return windowsPixelsFromDC(dc, bounds)
}

// Copy directly into a CPU-readable DIB section. A device-dependent bitmap
// followed by GetDIBits forces an extra driver conversion/readback on F1.
// The caller owns the source DC and stays on its OS thread until return.
func windowsPixelsFromDC(dc uintptr, bounds image.Rectangle) (*image.NRGBA, error) {
	w, h := bounds.Dx(), bounds.Dy()
	if dc == 0 || w < 1 || h < 1 || int64(w)*int64(h) > 16<<20 {
		return nil, errors.New("screen image is too large")
	}
	memory, _, _ := winGDI.NewProc("CreateCompatibleDC").Call(dc)
	if memory == 0 {
		return nil, errors.New("screen capture unavailable")
	}
	defer winGDI.NewProc("DeleteDC").Call(memory)
	var header [40]byte
	binary.LittleEndian.PutUint32(header[:], 40)
	binary.LittleEndian.PutUint32(header[4:], uint32(w))
	binary.LittleEndian.PutUint32(header[8:], uint32(int32(-h)))
	binary.LittleEndian.PutUint16(header[12:], 1)
	binary.LittleEndian.PutUint16(header[14:], 32)
	var pixels uintptr
	bitmap, _, _ := winGDI.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&header[0])), 0, uintptr(unsafe.Pointer(&pixels)), 0, 0)
	if bitmap == 0 || pixels == 0 {
		return nil, errors.New("screen bitmap unavailable")
	}
	defer winGDI.NewProc("DeleteObject").Call(bitmap)
	previous, _, _ := winGDI.NewProc("SelectObject").Call(memory, bitmap)
	if previous == 0 || previous == ^uintptr(0) {
		return nil, errors.New("screen bitmap unavailable")
	}
	defer winGDI.NewProc("SelectObject").Call(memory, previous)
	ok, _, _ := winGDI.NewProc("BitBlt").Call(memory, 0, 0, uintptr(w), uintptr(h), dc, uintptr(bounds.Min.X), uintptr(bounds.Min.Y), 0x00CC0020|0x40000000)
	if ok == 0 {
		return nil, errors.New("screen capture failed")
	}
	// Microsoft requires the drawing batch to complete before reading ppvBits.
	if ok, _, _ := winGDI.NewProc("GdiFlush").Call(); ok == 0 {
		return nil, errors.New("screen pixels unavailable")
	}
	result := image.NewNRGBA(image.Rect(0, 0, w, h))
	windows.NewLazySystemDLL("ntdll.dll").NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&result.Pix[0])), pixels, uintptr(len(result.Pix)))
	for i := 0; i < len(result.Pix); i += 4 {
		result.Pix[i], result.Pix[i+2], result.Pix[i+3] = result.Pix[i+2], result.Pix[i], 255
	}
	return result, nil
}

// Decode and convert pixels on the capture worker, before entering the UI
// thread for the short native clipboard ownership transaction.
func windowsClipboardDIB(data []byte) ([]byte, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 16<<20 {
		return nil, errors.New("invalid clipboard image")
	}
	im, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()
	if int64(w)*int64(h) > 16<<20 {
		return nil, errors.New("clipboard image too large")
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
	return dib, nil
}

func windowsImageClipboard(hwnd uintptr, data, dib []byte) bool {
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
