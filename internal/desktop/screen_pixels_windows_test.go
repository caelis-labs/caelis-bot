package desktop

import (
	"encoding/binary"
	"image"
	"image/color"
	"runtime"
	"testing"
	"unsafe"
)

// Exercise the actual GDI copy with synthetic pixels, without observing the
// user's desktop. Offset source bounds, row order, BGR conversion and opaque
// alpha must all survive the DIB-section path used by F1.
func TestWindowsCaptureDIBSectionCopiesOffsetPixels(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dc, _, _ := winGDI.NewProc("CreateCompatibleDC").Call(0)
	if dc == 0 {
		t.Fatal("cannot create fixture DC")
	}
	defer winGDI.NewProc("DeleteDC").Call(dc)
	var header [40]byte
	binary.LittleEndian.PutUint32(header[:], 40)
	binary.LittleEndian.PutUint32(header[4:], 7)
	binary.LittleEndian.PutUint32(header[8:], 5)
	binary.LittleEndian.PutUint16(header[12:], 1)
	binary.LittleEndian.PutUint16(header[14:], 32)
	var bits uintptr
	bitmap, _, _ := winGDI.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&header[0])), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 {
		t.Fatal("cannot create fixture bitmap")
	}
	defer winGDI.NewProc("DeleteObject").Call(bitmap)
	previous, _, _ := winGDI.NewProc("SelectObject").Call(dc, bitmap)
	defer winGDI.NewProc("SelectObject").Call(dc, previous)
	for y := 0; y < 5; y++ {
		for x := 0; x < 7; x++ {
			value := uintptr(x*30 | y*40<<8 | (x+y)*15<<16)
			if ok, _, _ := winGDI.NewProc("SetPixelV").Call(dc, uintptr(x), uintptr(y), value); ok == 0 {
				t.Fatal("cannot paint fixture pixel")
			}
		}
	}
	im, err := windowsPixelsFromDC(dc, image.Rect(2, 1, 7, 5))
	if err != nil {
		t.Fatal(err)
	}
	if im.Bounds() != image.Rect(0, 0, 5, 4) {
		t.Fatal("capture dimensions changed")
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 5; x++ {
			want := color.NRGBA{uint8((x + 2) * 30), uint8((y + 1) * 40), uint8((x + y + 3) * 15), 255}
			if got := im.NRGBAAt(x, y); got != want {
				t.Fatalf("pixel %d,%d: %v, want %v", x, y, got, want)
			}
		}
	}
	if _, err := windowsPixelsFromDC(dc, image.Rectangle{}); err == nil {
		t.Fatal("empty capture admitted")
	}
}
