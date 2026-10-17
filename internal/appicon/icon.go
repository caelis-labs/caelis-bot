// Package appicon derives Windows icon sizes from the finished public PNG.
package appicon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
)

// ICO uses uncompressed 32-bit DIBs so both the shell and NSIS can select an
// exact small size without downsampling the full application illustration.
func ICO(src image.Image) []byte {
	sizes := []int{16, 20, 24, 32, 40, 48, 64, 256}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	var images [][]byte
	for _, size := range sizes {
		data := dib(src, size)
		entry := make([]byte, 16)
		entry[0], entry[1] = byte(size), byte(size)
		binary.LittleEndian.PutUint16(entry[4:], 1)
		binary.LittleEndian.PutUint16(entry[6:], 32)
		binary.LittleEndian.PutUint32(entry[8:], uint32(len(data)))
		binary.LittleEndian.PutUint32(entry[12:], uint32(offset))
		out.Write(entry)
		images = append(images, data)
		offset += len(data)
	}
	for _, data := range images {
		out.Write(data)
	}
	return out.Bytes()
}

func dib(src image.Image, size int) []byte {
	maskStride := ((size + 31) / 32) * 4
	data := make([]byte, 40+size*size*4+maskStride*size)
	binary.LittleEndian.PutUint32(data, 40)
	binary.LittleEndian.PutUint32(data[4:], uint32(size))
	binary.LittleEndian.PutUint32(data[8:], uint32(size*2))
	binary.LittleEndian.PutUint16(data[12:], 1)
	binary.LittleEndian.PutUint16(data[14:], 32)
	bounds := src.Bounds()
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Average in premultiplied space; transparent edges retain their color.
			var r, g, b, a, n uint64
			x0, x1 := x*bounds.Dx()/size, max((x+1)*bounds.Dx()/size, x*bounds.Dx()/size+1)
			y0, y1 := y*bounds.Dy()/size, max((y+1)*bounds.Dy()/size, y*bounds.Dy()/size+1)
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := src.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
					r += uint64(cr)
					g += uint64(cg)
					b += uint64(cb)
					a += uint64(ca)
					n++
				}
			}
			c := color.NRGBAModel.Convert(color.RGBA64{uint16(r / n), uint16(g / n), uint16(b / n), uint16(a / n)}).(color.NRGBA)
			i := 40 + ((size-1-y)*size+x)*4
			data[i], data[i+1], data[i+2], data[i+3] = c.B, c.G, c.R, c.A
			if c.A == 0 {
				data[40+size*size*4+(size-1-y)*maskStride+x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	return data
}
