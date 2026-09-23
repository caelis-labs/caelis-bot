package contentpack

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
)

func validateImage(b []byte, format string) error {
	c, f, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil {
		return errors.New("invalid image")
	}
	if f != format || c.Width < 1 || c.Height < 1 || c.Width > 2048 || c.Height > 2048 {
		return errors.New("image must be at most 2048×2048")
	}
	if format == "png" {
		for offset := 8; offset+12 <= len(b); {
			n := int(binary.BigEndian.Uint32(b[offset:]))
			if n > len(b)-offset-12 {
				return errors.New("invalid PNG chunk")
			}
			tag := string(b[offset+4 : offset+8])
			if tag == "acTL" || tag == "fcTL" || tag == "fdAT" {
				return errors.New("animated PNG is not supported by png-v1")
			}
			offset += n + 12
		}
	}
	if _, _, e := image.Decode(bytes.NewReader(b)); e != nil {
		return errors.New("invalid image data")
	}
	return nil
}

type glbView struct {
	Buffer     int
	ByteOffset int
	ByteLength int
	ByteStride int
}
type glbAccessor struct {
	BufferView    *int
	ByteOffset    int
	ComponentType int
	Count         int
	Type          string
	Sparse        json.RawMessage
}
type glbDocument struct {
	Asset       struct{ Version string }
	Buffers     []struct{ ByteLength int }
	BufferViews []glbView
	Accessors   []glbAccessor
	Nodes       []struct {
		Children []int
		Mesh     *int
		Skin     *int
	}
	Scenes []struct{ Nodes []int }
	Scene  *int
	Meshes []struct {
		Primitives []struct {
			Attributes map[string]int
			Indices    *int
			Targets    []map[string]int
			Mode       *int
		}
	}
	Skins []struct {
		Joints              []int
		InverseBindMatrices *int
	}
	Materials []json.RawMessage
	Textures  []json.RawMessage
	Images    []struct {
		BufferView *int
		MimeType   string
	}
	Animations []struct {
		Name     string
		Channels []struct {
			Sampler int
			Target  struct {
				Node *int
				Path string
			}
		}
		Samplers []struct {
			Input         int
			Output        int
			Interpolation string
		}
	}
}

func validateGLB(b []byte) error {
	fail := func() error { return errors.New("unsupported or invalid basic-3d-v1 GLB") }
	if len(b) < 28 || string(b[:4]) != "glTF" || binary.LittleEndian.Uint32(b[4:]) != 2 || int(binary.LittleEndian.Uint32(b[8:])) != len(b) {
		return fail()
	}
	n := int(binary.LittleEndian.Uint32(b[12:]))
	if n < 2 || n > 2<<20 || n%4 != 0 || 20+n+8 > len(b) || binary.LittleEndian.Uint32(b[16:]) != 0x4e4f534a {
		return fail()
	}
	at := 20 + n
	binSize := int(binary.LittleEndian.Uint32(b[at:]))
	if binSize%4 != 0 || at+8+binSize != len(b) || binary.LittleEndian.Uint32(b[at+4:]) != 0x004e4942 {
		return fail()
	}
	data := b[at+8:]
	var raw map[string]any
	if e := StrictJSON(b[20:20+n], &raw); e != nil {
		return e
	}
	var safe func(any) bool
	safe = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if k == "uri" {
					return false
				}
				if k == "extensions" {
					m, ok := v.(map[string]any)
					if !ok {
						return false
					}
					for name := range m {
						if name != "KHR_materials_unlit" {
							return false
						}
					}
				}
				if k == "extensionsRequired" || k == "extensionsUsed" {
					list, ok := v.([]any)
					if !ok {
						return false
					}
					for _, name := range list {
						if name != "KHR_materials_unlit" {
							return false
						}
					}
				}
				if !safe(v) {
					return false
				}
			}
		case []any:
			for _, v := range x {
				if !safe(v) {
					return false
				}
			}
		}
		return true
	}
	if !safe(raw) {
		return errors.New("external resources and unsupported GLB extensions are forbidden")
	}
	var d glbDocument
	if e := json.Unmarshal(b[20:20+n], &d); e != nil {
		return e
	}
	if d.Asset.Version != "2.0" || len(d.Buffers) != 1 || d.Buffers[0].ByteLength < 1 || d.Buffers[0].ByteLength > len(data) || len(d.Nodes) > 512 || len(d.Meshes) == 0 || len(d.Meshes) > 128 || len(d.Accessors) > 2048 || len(d.Materials) > 64 || len(d.Textures) > 32 || len(d.Images) > 16 || len(d.Animations) > 32 || len(d.BufferViews) > 2048 || len(d.Skins) > 16 {
		return fail()
	}
	valid := func(i, n int) bool { return i >= 0 && i < n }
	for _, v := range d.BufferViews {
		if v.Buffer != 0 || v.ByteOffset < 0 || v.ByteLength < 1 || v.ByteOffset > d.Buffers[0].ByteLength || v.ByteLength > d.Buffers[0].ByteLength-v.ByteOffset || v.ByteStride < 0 || v.ByteStride > 252 || (v.ByteStride != 0 && (v.ByteStride < 4 || v.ByteStride%4 != 0)) {
			return fail()
		}
	}
	sizes := map[int]int{5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4}
	widths := map[string]int{"SCALAR": 1, "VEC2": 2, "VEC3": 3, "VEC4": 4, "MAT4": 16}
	components := int64(0)
	for _, a := range d.Accessors {
		if a.BufferView == nil || !valid(*a.BufferView, len(d.BufferViews)) || a.ByteOffset < 0 || a.Count < 1 || a.Count > 250000 || sizes[a.ComponentType] == 0 || widths[a.Type] == 0 || len(a.Sparse) > 0 {
			return errors.New("invalid accessor; sparse accessors are not supported")
		}
		components += int64(a.Count) * int64(widths[a.Type])
		if components > 4_000_000 {
			return errors.New("accessor budget exceeded")
		}
		v := d.BufferViews[*a.BufferView]
		element := sizes[a.ComponentType] * widths[a.Type]
		stride := v.ByteStride
		if stride == 0 {
			stride = element
		}
		if stride < element || a.ByteOffset > v.ByteLength || int64(a.ByteOffset)+int64(a.Count-1)*int64(stride)+int64(element) > int64(v.ByteLength) {
			return fail()
		}
		if a.ComponentType == 5126 {
			for i := 0; i < a.Count; i++ {
				for c := 0; c < widths[a.Type]; c++ {
					offset := v.ByteOffset + a.ByteOffset + i*stride + c*4
					f := math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
					if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) || math.Abs(float64(f)) > 1e6 {
						return errors.New("non-finite or excessive model coordinates")
					}
				}
			}
		}
	}
	count := 0
	for _, mesh := range d.Meshes {
		if len(mesh.Primitives) == 0 || len(mesh.Primitives) > 32 {
			return fail()
		}
		for _, p := range mesh.Primitives {
			if p.Mode != nil && *p.Mode != 4 {
				return errors.New("only triangle primitives are supported")
			}
			position, ok := p.Attributes["POSITION"]
			if !ok || !valid(position, len(d.Accessors)) || d.Accessors[position].Type != "VEC3" || d.Accessors[position].ComponentType != 5126 || len(p.Targets) > 8 {
				return fail()
			}
			for _, a := range p.Attributes {
				if !valid(a, len(d.Accessors)) {
					return fail()
				}
			}
			for _, target := range p.Targets {
				for _, a := range target {
					if !valid(a, len(d.Accessors)) {
						return fail()
					}
				}
			}
			vertices := d.Accessors[position].Count
			count += vertices
			if p.Indices != nil {
				if !valid(*p.Indices, len(d.Accessors)) {
					return fail()
				}
				a := d.Accessors[*p.Indices]
				if a.Type != "SCALAR" || (a.ComponentType != 5121 && a.ComponentType != 5123 && a.ComponentType != 5125) || a.Count > 750000 {
					return fail()
				}
			}
		}
	}
	if count > 250000 {
		return errors.New("model vertex budget exceeded")
	}
	parents := make([]bool, len(d.Nodes))
	colors := make([]int, len(d.Nodes))
	var visit func(int) bool
	visit = func(i int) bool {
		if !valid(i, len(d.Nodes)) || colors[i] == 1 {
			return false
		}
		if colors[i] == 2 {
			return true
		}
		colors[i] = 1
		for _, child := range d.Nodes[i].Children {
			if !valid(child, len(d.Nodes)) || parents[child] {
				return false
			}
			parents[child] = true
			if !visit(child) {
				return false
			}
		}
		colors[i] = 2
		return true
	}
	for i, node := range d.Nodes {
		if (node.Mesh != nil && !valid(*node.Mesh, len(d.Meshes))) || (node.Skin != nil && !valid(*node.Skin, len(d.Skins))) || !visit(i) {
			return errors.New("invalid or cyclic node graph")
		}
	}
	if len(d.Scenes) == 0 || len(d.Scenes) > 16 || (d.Scene != nil && !valid(*d.Scene, len(d.Scenes))) {
		return fail()
	}
	for _, s := range d.Scenes {
		seen := map[int]bool{}
		for _, i := range s.Nodes {
			if !valid(i, len(d.Nodes)) || parents[i] || seen[i] {
				return fail()
			}
			seen[i] = true
		}
	}
	for _, s := range d.Skins {
		if len(s.Joints) > 128 || len(s.Joints) == 0 {
			return fail()
		}
		for _, i := range s.Joints {
			if !valid(i, len(d.Nodes)) {
				return fail()
			}
		}
		if s.InverseBindMatrices != nil && !valid(*s.InverseBindMatrices, len(d.Accessors)) {
			return fail()
		}
	}
	instances := 0
	for _, node := range d.Nodes {
		if node.Mesh != nil {
			for _, primitive := range d.Meshes[*node.Mesh].Primitives {
				instances += d.Accessors[primitive.Attributes["POSITION"]].Count
			}
		}
	}
	if instances > 250000 {
		return errors.New("instanced vertex budget exceeded")
	}
	pixels := 0
	for _, im := range d.Images {
		if im.BufferView == nil || !valid(*im.BufferView, len(d.BufferViews)) {
			return fail()
		}
		v := d.BufferViews[*im.BufferView]
		encoded := data[v.ByteOffset : v.ByteOffset+v.ByteLength]
		format := "png"
		if im.MimeType == "image/jpeg" {
			format = "jpeg"
		} else if im.MimeType != "image/png" {
			return fail()
		}
		if e := validateImage(encoded, format); e != nil {
			return e
		}
		cfg, _, _ := image.DecodeConfig(bytes.NewReader(encoded))
		pixels += cfg.Width * cfg.Height
	}
	if pixels > 16<<20 {
		return errors.New("texture pixel budget exceeded")
	}
	keys := 0
	names := map[string]bool{}
	for _, a := range d.Animations {
		if !textOK(a.Name, 64) || names[a.Name] || len(a.Channels) > 256 || len(a.Samplers) > 256 {
			return fail()
		}
		names[a.Name] = true
		for _, s := range a.Samplers {
			if !valid(s.Input, len(d.Accessors)) || !valid(s.Output, len(d.Accessors)) || (s.Interpolation != "" && s.Interpolation != "LINEAR" && s.Interpolation != "STEP" && s.Interpolation != "CUBICSPLINE") {
				return fail()
			}
			input := d.Accessors[s.Input]
			keys += input.Count
			if keys > 100000 {
				return fmt.Errorf("animation key budget exceeded: %d", keys)
			}
			if input.Type != "SCALAR" || input.ComponentType != 5126 {
				return fail()
			}
			v := d.BufferViews[*input.BufferView]
			stride := v.ByteStride
			if stride == 0 {
				stride = 4
			}
			prev := float32(-1)
			for i := 0; i < input.Count; i++ {
				t := math.Float32frombits(binary.LittleEndian.Uint32(data[v.ByteOffset+input.ByteOffset+i*stride:]))
				if t < 0 || t > 60 || t <= prev {
					return errors.New("animation times must increase and stay within 60s")
				}
				prev = t
			}
			if prev <= 0 {
				return errors.New("animation duration must be positive")
			}
		}
		for _, c := range a.Channels {
			if !valid(c.Sampler, len(a.Samplers)) || c.Target.Node == nil || !valid(*c.Target.Node, len(d.Nodes)) {
				return fail()
			}
			switch c.Target.Path {
			case "translation", "rotation", "scale", "weights":
			default:
				return fail()
			}
		}
	}
	return nil
}
