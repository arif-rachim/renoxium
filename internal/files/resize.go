package files

import (
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // decode WebP uploads
)

// Widths are the sizes an image can be resized to. A requested width is
// rounded up to the next one, so each image has at most this many variants.
var Widths = []int{160, 320, 480, 640, 960, 1280, 1920}

// MaxPixels is the largest image resized; bigger ones are served as uploaded.
const MaxPixels = 16_000_000

// resizing allows one resize at a time: decoding needs width*height*4 bytes.
var resizing = make(chan struct{}, 1)

// Resizable reports whether a content type can be resized.
func Resizable(ctype string) bool {
	return ctype == "image/jpeg" || ctype == "image/png" || ctype == "image/webp"
}

// SnapWidth rounds a requested width up to one of Widths.
func SnapWidth(w int) int {
	for _, s := range Widths {
		if w <= s {
			return s
		}
	}
	return Widths[len(Widths)-1]
}

// Variant returns a file holding the blob scaled down to width (a value
// from Widths), creating it once. Images already that narrow, too large to
// decode, or of another type come back as the original. JPEG and WebP
// become JPEG; PNG stays PNG.
func (s *Store) Variant(b Blob, width int) (*os.File, string, error) {
	if !Resizable(b.ContentType) {
		f, err := s.Open(b.Hash)
		return f, b.ContentType, err
	}
	ctype, ext := "image/jpeg", "jpg"
	if b.ContentType == "image/png" {
		ctype, ext = "image/png", "png"
	}
	path := filepath.Join(s.dir, "variants", b.Hash[0:2], b.Hash+"-"+strconv.Itoa(width)+"."+ext)
	if f, err := os.Open(path); err == nil {
		return f, ctype, nil
	}
	src, err := s.Open(b.Hash)
	if err != nil {
		return nil, "", err
	}
	cfg, _, err := image.DecodeConfig(src)
	if err != nil || cfg.Width <= width || cfg.Width*cfg.Height > MaxPixels {
		src.Seek(0, 0)
		return src, b.ContentType, nil
	}
	resizing <- struct{}{}
	defer func() { <-resizing }()
	if f, err := os.Open(path); err == nil { // made while waiting
		src.Close()
		return f, ctype, nil
	}
	src.Seek(0, 0)
	img, _, err := image.Decode(src)
	src.Close()
	if err != nil {
		return nil, "", err
	}
	height := max(1, cfg.Height*width/cfg.Width)
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	if ctype == "image/jpeg" { // no transparency in JPEG: start from white
		draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	}
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, "", err
	}
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "variant-*")
	if err != nil {
		return nil, "", err
	}
	defer os.Remove(tmp.Name())
	if ctype == "image/png" {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(tmp, dst)
	} else {
		err = jpeg.Encode(tmp, dst, &jpeg.Options{Quality: 82})
	}
	if err == nil {
		err = tmp.Close()
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		tmp.Close()
		return nil, "", errors.Join(errors.New("resizing failed"), err)
	}
	f, err := os.Open(path)
	return f, ctype, err
}
