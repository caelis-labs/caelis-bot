package desktop

import (
	"bytes"
	"encoding/binary"
	"golang.org/x/sys/windows"
	"image"
	"image/color"
	"image/png"
	"unsafe"
)

func readWindowsClipboard() ([]string, []byte, error) {
	if ok, _, _ := winUser.NewProc("OpenClipboard").Call(0); ok == 0 {
		return nil, nil, errClipboardInvalid
	}
	defer winUser.NewProc("CloseClipboard").Call()
	if h, _, _ := winUser.NewProc("GetClipboardData").Call(15); h != 0 { // CF_HDROP
		query := windows.NewLazySystemDLL("shell32.dll").NewProc("DragQueryFileW")
		n, _, _ := query.Call(h, 0xffffffff, 0, 0)
		if n > 8 {
			return nil, nil, errClipboardTooLarge
		}
		var paths []string
		for i := uintptr(0); i < n; i++ {
			size, _, _ := query.Call(h, i, 0, 0)
			if size > 32767 {
				return nil, nil, errClipboardInvalid
			}
			name := make([]uint16, size+1)
			query.Call(h, i, uintptr(unsafe.Pointer(&name[0])), size+1)
			paths = append(paths, windows.UTF16ToString(name))
		}
		return paths, nil, nil
	}
	name, _ := windows.UTF16PtrFromString("PNG")
	format, _, _ := winUser.NewProc("RegisterClipboardFormatW").Call(uintptr(unsafe.Pointer(name)))
	if data, err := clipboardBytes(format); err != nil {
		return nil, nil, err
	} else if data != nil {
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 16<<20 {
			return nil, nil, errClipboardInvalid
		}
		return nil, data, nil
	}
	data, err := clipboardBytes(8)
	if err != nil || data == nil {
		return nil, nil, err
	} // CF_DIB; uncommon/compressed formats fail explicitly.
	data, err = dibPNG(data)
	return nil, data, err
}
func clipboardBytes(format uintptr) ([]byte, error) {
	h, _, _ := winUser.NewProc("GetClipboardData").Call(format)
	if h == 0 {
		return nil, nil
	}
	k := windows.NewLazySystemDLL("kernel32.dll")
	n, _, _ := k.NewProc("GlobalSize").Call(h)
	if n == 0 || n > maxDraftFile {
		return nil, errClipboardTooLarge
	}
	p, _, _ := k.NewProc("GlobalLock").Call(h)
	if p == 0 {
		return nil, errClipboardInvalid
	}
	defer k.NewProc("GlobalUnlock").Call(h)
	data := make([]byte, int(n))
	copyMemory := windows.NewLazySystemDLL("ntdll.dll").NewProc("RtlMoveMemory")
	if err := copyMemory.Find(); err != nil {
		return nil, errClipboardInvalid
	}
	copyMemory.Call(uintptr(unsafe.Pointer(&data[0])), p, n)
	return data, nil
}
func dibPNG(data []byte) ([]byte, error) {
	if len(data) < 40 {
		return nil, errClipboardInvalid
	}
	header := int(binary.LittleEndian.Uint32(data[:4]))
	if header < 40 || header > len(data) {
		return nil, errClipboardInvalid
	}
	w, h := int(int32(binary.LittleEndian.Uint32(data[4:8]))), int(int32(binary.LittleEndian.Uint32(data[8:12])))
	top := h < 0
	if top {
		h = -h
	}
	bits := int(binary.LittleEndian.Uint16(data[14:16]))
	compression := binary.LittleEndian.Uint32(data[16:20])
	if w <= 0 || h <= 0 || int64(w)*int64(h) > 16<<20 || binary.LittleEndian.Uint16(data[12:14]) != 1 || (bits != 24 && bits != 32) || compression != 0 {
		return nil, errClipboardInvalid
	}
	stride := ((int64(w)*int64(bits) + 31) / 32) * 4
	if stride*int64(h) > int64(len(data)-header) {
		return nil, errClipboardInvalid
	}
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := y
		if !top {
			row = h - 1 - y
		}
		for x := 0; x < w; x++ {
			offset := header + row*int(stride) + x*(bits/8)
			im.SetNRGBA(x, y, color.NRGBA{data[offset+2], data[offset+1], data[offset], 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		return nil, err
	}
	if out.Len() > maxDraftFile {
		return nil, errClipboardTooLarge
	}
	return out.Bytes(), nil
}
