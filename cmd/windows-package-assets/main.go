// Generate packaging icons from the finished public app icon. No authoring input.
package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/appicon"
)

func main() {
	if len(os.Args) != 3 {
		panic("specify finished icon and package Assets directory")
	}
	f, err := os.Open(os.Args[1])
	must(err)
	src, err := png.Decode(f)
	must(err)
	must(f.Close())
	must(os.MkdirAll(os.Args[2], 0755))
	must(os.WriteFile(filepath.Join(os.Args[2], "app.ico"), appicon.ICO(src), 0644))
	for name, size := range map[string]image.Point{"StoreLogo.png": {50, 50}, "Square44x44Logo.png": {44, 44}, "Square150x150Logo.png": {150, 150}, "Wide310x150Logo.png": {310, 150}, "SplashScreen.png": {620, 300}} {
		out := image.NewNRGBA(image.Rect(0, 0, size.X, size.Y))
		side := min(size.X, size.Y)
		bounds := src.Bounds()
		left, top := (size.X-side)/2, (size.Y-side)/2
		for y := 0; y < side; y++ {
			for x := 0; x < side; x++ {
				out.Set(left+x, top+y, src.At(bounds.Min.X+x*bounds.Dx()/side, bounds.Min.Y+y*bounds.Dy()/side))
			}
		}
		f, err := os.Create(filepath.Join(os.Args[2], name))
		must(err)
		must(png.Encode(f, out))
		must(f.Close())
	}
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
