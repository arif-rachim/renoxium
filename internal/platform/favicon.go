package platform

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// defaultFavicon is served at /favicon.ico for apps without their own: a
// 32×32 rounded square in a neutral slate colour.
var defaultFavicon = func() []byte {
	const n, r = 32, 7
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	fill := color.NRGBA{R: 0x47, G: 0x55, B: 0x69, A: 0xff}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			// Distance into the nearest corner's rounding, if any.
			dx, dy := 0, 0
			if x < r {
				dx = r - x
			} else if x >= n-r {
				dx = x - (n - r - 1)
			}
			if y < r {
				dy = r - y
			} else if y >= n-r {
				dy = y - (n - r - 1)
			}
			if dx*dx+dy*dy <= r*r {
				img.SetNRGBA(x, y, fill)
			}
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}()
