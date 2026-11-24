package contentpack

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"
)

// A single indexed surface, split across multiple accessory meshes, can reuse
// its position data. Density and reuse are both legitimate authoring choices.
func TestGLBAllowsDenseSharedGeometry(t *testing.T) {
	const vertices = 250002
	const indices = 3 * vertices
	const posBytes = 12 * vertices
	const binBytes = posBytes + indices*4
	data := make([]byte, binBytes)
	for i := range vertices {
		for axis := range 3 {
			if i%3 == axis {
				binary.LittleEndian.PutUint32(data[12*i+4*axis:], math.Float32bits(1))
			}
		}
	}
	for i := range indices {
		binary.LittleEndian.PutUint32(data[posBytes+4*i:], uint32(i%vertices))
	}
	views := []map[string]int{{"buffer": 0, "byteOffset": posBytes, "byteLength": indices * 4}}
	accessors := []map[string]any{{"bufferView": 0, "componentType": 5125, "count": indices, "type": "SCALAR"}}
	for range 2050 {
		views = append(views, map[string]int{"buffer": 0, "byteLength": posBytes})
		accessors = append(accessors, map[string]any{"bufferView": len(views) - 1, "componentType": 5126, "count": vertices, "type": "VEC3", "min": []int{0, 0, 0}, "max": []int{1, 1, 1}})
	}
	var meshes []map[string]any
	var nodes []map[string]int
	var roots []int
	for i := range 129 {
		var primitives []map[string]any
		num := 1
		if i == 0 {
			num = 33
		}
		for j := range num {
			primitives = append(primitives, map[string]any{"attributes": map[string]int{"POSITION": 1 + i + j}, "indices": 0})
		}
		meshes = append(meshes, map[string]any{"primitives": primitives})
		nodes = append(nodes, map[string]int{"mesh": i})
		roots = append(roots, i)
	}
	doc := map[string]any{"asset": map[string]string{"version": "2.0"}, "buffers": []map[string]int{{"byteLength": binBytes}}, "bufferViews": views, "accessors": accessors, "meshes": meshes, "nodes": nodes, "scenes": []map[string][]int{{"nodes": roots}}, "scene": 0}
	header, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	header = append(header, bytes.Repeat([]byte{' '}, (4-len(header)%4)%4)...)
	glb := []byte("glTF")
	for _, word := range []int{2, 28 + len(header) + len(data), len(header), 0x4e4f534a} {
		glb = binary.LittleEndian.AppendUint32(glb, uint32(word))
	}
	glb = append(glb, header...)
	glb = binary.LittleEndian.AppendUint32(glb, uint32(len(data)))
	glb = binary.LittleEndian.AppendUint32(glb, 0x004e4942)
	glb = append(glb, data...)
	if err := validateGLB(glb); err != nil {
		t.Fatal(err)
	}
}

func TestFloatSpansCheckOnlyReferencedLanes(t *testing.T) {
	spans := make(floatSpans)
	spans.add(4, 28, 12)
	spans.add(16, 40, 12)
	spans.add(4, 16, 12)
	b := make([]byte, 44)
	binary.LittleEndian.PutUint32(b[24:], math.Float32bits(float32(math.NaN())))
	if err := spans.validate(b); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(b[40:], math.Float32bits(float32(math.NaN())))
	if err := spans.validate(b); err == nil {
		t.Fatal("accepted invalid referenced float")
	}
}
