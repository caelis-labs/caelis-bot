package taskterminal

import (
	"bytes"
	"context"
	"encoding/binary"
	"golang.org/x/sys/windows"
	"image"
	"image/jpeg"
	"unsafe"
)

// Snapshots share the cancellable observation lease. User window operations
// take precedence; screenshots never target a window found by its title.
func (m *WindowManager) Thumbnail(ctx context.Context, id string) ([]byte, error) {
	m.mu.Lock()
	e := m.entries[id]
	m.mu.Unlock()
	if e == nil {
		return nil, ErrWindowIdentity
	}
	e.controller.mu.Lock()
	ready := !e.controller.running && !e.controller.closed && e.controller.observed.State == WindowForeground
	e.controller.mu.Unlock()
	if !ready {
		return nil, context.Canceled
	}
	l := e.launcher
	if !l.observationMu.TryLock() {
		return nil, context.Canceled
	}
	if !l.mu.TryLock() {
		l.observationMu.Unlock()
		return nil, context.Canceled
	}
	ctx, cancel := context.WithCancel(ctx)
	l.observationCancel = cancel
	l.observationMu.Unlock()
	defer l.mu.Unlock()
	defer cancel()
	if w, ok := l.windows[id].(interface {
		Thumbnail(context.Context) ([]byte, error)
	}); ok {
		return w.Thumbnail(ctx)
	}
	return nil, ErrWindowIdentity
}

func (w *consoleWindow) Thumbnail(ctx context.Context) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if w.process == 0 || !w.validWindow() {
		return nil, ErrWindowIdentity
	}
	if n, err := windows.WaitForSingleObject(w.process, 0); err != nil || n != uint32(windows.WAIT_TIMEOUT) {
		return nil, ErrWindowIdentity
	}
	if n, _, _ := terminalUser.NewProc("IsIconic").Call(w.hwnd); n != 0 {
		return nil, ErrWindowUnsupported
	}
	pixels, err := printConsoleWindow(ctx, w.hwnd)
	if err != nil {
		return nil, err
	}
	if !w.validWindow() {
		return nil, ErrWindowIdentity
	}
	var out bytes.Buffer
	if err = jpeg.Encode(&out, pixels, &jpeg.Options{Quality: 75}); err != nil {
		return nil, err
	}
	if out.Len() > 256<<10 {
		return nil, ErrWindowUnsupported
	}
	return out.Bytes(), nil
}

func printConsoleWindow(ctx context.Context, hwnd uintptr) (image.Image, error) {
	var rect struct{ Left, Top, Right, Bottom int32 }
	if ok, _, _ := terminalUser.NewProc("GetClientRect").Call(hwnd, uintptr(unsafe.Pointer(&rect))); ok == 0 {
		return nil, ErrWindowUnsupported
	}
	width, height := int(rect.Right-rect.Left), int(rect.Bottom-rect.Top)
	if width < 1 || height < 1 || width > 16384 || height > 16384 || int64(width)*int64(height) > 8<<20 {
		return nil, ErrWindowUnsupported
	}
	gdi := windows.NewLazySystemDLL("gdi32.dll")
	dc, _, _ := terminalUser.NewProc("GetDC").Call(hwnd)
	if dc == 0 {
		return nil, ErrWindowUnsupported
	}
	defer terminalUser.NewProc("ReleaseDC").Call(hwnd, dc)
	memory, _, _ := gdi.NewProc("CreateCompatibleDC").Call(dc)
	if memory == 0 {
		return nil, ErrWindowUnsupported
	}
	defer gdi.NewProc("DeleteDC").Call(memory)
	bitmap, _, _ := gdi.NewProc("CreateCompatibleBitmap").Call(dc, uintptr(width), uintptr(height))
	if bitmap == 0 {
		return nil, ErrWindowUnsupported
	}
	defer gdi.NewProc("DeleteObject").Call(bitmap)
	previous, _, _ := gdi.NewProc("SelectObject").Call(memory, bitmap)
	gdi.NewProc("PatBlt").Call(memory, 0, 0, uintptr(width), uintptr(height), 0x00000042)
	// WM_PRINTCLIENT + SMTO_ABORTIFHUNG bounds the foreign window call to 500 ms.
	// No screen BitBlt fallback: an occluding unrelated window is never sampled.
	var result uintptr
	ok, _, _ := terminalUser.NewProc("SendMessageTimeoutW").Call(hwnd, 0x318, memory, 0x04|0x10, 0x01|0x02, 500, uintptr(unsafe.Pointer(&result)))
	gdi.NewProc("SelectObject").Call(memory, previous)
	if ok == 0 {
		return nil, ErrWindowUnsupported
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	data := make([]byte, 40+width*height*4)
	binary.LittleEndian.PutUint32(data, 40)
	binary.LittleEndian.PutUint32(data[4:], uint32(width))
	binary.LittleEndian.PutUint32(data[8:], uint32(int32(-height)))
	binary.LittleEndian.PutUint16(data[12:], 1)
	binary.LittleEndian.PutUint16(data[14:], 32)
	lines, _, _ := gdi.NewProc("GetDIBits").Call(dc, bitmap, 0, uintptr(height), uintptr(unsafe.Pointer(&data[40])), uintptr(unsafe.Pointer(&data[0])), 0)
	if lines != uintptr(height) {
		return nil, ErrWindowUnsupported
	}
	targetWidth := min(width, 480)
	targetHeight := max(1, height*targetWidth/width)
	if targetHeight > 320 {
		targetHeight = 320
		targetWidth = max(1, width*targetHeight/height)
	}
	preview := image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	different := false
	first := data[40:43]
	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			src := 40 + ((y*height/targetHeight)*width+x*width/targetWidth)*4
			dst := y*preview.Stride + x*4
			preview.Pix[dst] = data[src+2]
			preview.Pix[dst+1] = data[src+1]
			preview.Pix[dst+2] = data[src]
			preview.Pix[dst+3] = 255
			different = different || !bytes.Equal(data[src:src+3], first)
		}
	}
	if !different {
		return nil, ErrWindowUnsupported
	}
	return preview, nil
}
