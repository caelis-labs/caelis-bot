package desktopcontrol

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"math"
	"testing"
	"time"
)

func testFrame(t *testing.T) Frame {
	t.Helper()
	var imageBytes bytes.Buffer
	if err := jpeg.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 960, 540)), nil); err != nil {
		t.Fatal(err)
	}
	return Frame{World: World{Display: "screen", Frame: Rect{-1920, 100, 1920, 1080}, WorkArea: Rect{-1920, 100, 1920, 1040}, Actor: Rect{-400, 100, 180, 240}}, Image: imageBytes.Bytes(), MIME: "image/jpeg", CapturedAt: time.Now()}
}
func TestObservedPixelsAndGeometryStayTogether(t *testing.T) {
	f := testFrame(t)
	r, err := Result(f)
	if err != nil {
		t.Fatal(err)
	}
	var o Observation
	if err = json.Unmarshal([]byte(r.Content[0]["text"]), &o); err != nil {
		t.Fatal(err)
	}
	if o.ID == "" || o.Image.Width != 960 || o.Image.Height != 540 || r.StructuredContent["observation"] != o.ID {
		t.Fatal("missing observation identity/dimensions")
	}
	x, y, err := o.DesktopPoint(480, 270)
	if err != nil || x != -960 || y != 640 {
		t.Fatal("DPI/origin conversion failed", x, y, err)
	}
	data, err := base64.StdEncoding.DecodeString(r.Content[1]["data"])
	if err != nil || !bytes.Equal(data, f.Image) {
		t.Fatal("image bytes changed")
	}
	for _, point := range [][2]float64{{960, 0}, {0, 540}, {-1, 0}, {0, math.NaN()}, {math.Inf(1), 0}} {
		if _, _, err = o.DesktopPoint(point[0], point[1]); err == nil {
			t.Fatal("invalid pixel accepted")
		}
	}
}
func TestObservationRejectsMalformedOrOversizedImages(t *testing.T) {
	for _, mutate := range []func(*Frame){func(f *Frame) { f.Image = make([]byte, MaxImageBytes+1) }, func(f *Frame) { f.MIME = "image/png" }, func(f *Frame) { f.Image = []byte("not an image") }, func(f *Frame) { f.World.Frame.Width = math.NaN() }, func(f *Frame) { f.CapturedAt = time.Time{} }} {
		f := testFrame(t)
		mutate(&f)
		if _, err := Result(f); err == nil {
			t.Fatal("invalid observation accepted")
		}
	}
}
