package files

import (
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // decode WebP uploads
)

// Widths are the sizes an image can be resized to. A requested width is
// rounded up to the next one, so each image has at most this many variants.
var Widths = []int{160, 320, 480, 640, 960, 1280, 1920}

// MaxPixels is the largest image resized; bigger ones are served as uploaded.
const MaxPixels = 16_000_000

// ResizeWait is how long a request waits for the resizer before it gets
// the original image instead.
var ResizeWait = 3 * time.Second

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
// become JPEG; PNG stays PNG. A size not made yet is only made when
// mayCreate (if set) allows it and the resizer is free within a few
// seconds; otherwise this request gets the original, and a later one makes
// the smaller copy. Resizing is slow and happens one image at a time, so
// one app's images must not keep every other app's waiting.
func (s *Store) Variant(b Blob, width int, mayCreate func() bool) (*os.File, string, error) {
	if !Resizable(b.ContentType) {
		f, err := s.Open(b.Hash)
		return f, b.ContentType, err
	}
	ctype, ext := "image/jpeg", "jpg"
	if b.ContentType == "image/png" {
		ctype, ext = "image/png", "png"
	}
	src, err := s.Open(b.Hash)
	if err != nil {
		return nil, "", err
	}
	// A JPEG stored with an orientation note (uploads from before photos were
	// turned upright on upload) is turned here; its copies have their own
	// names, so copies made before this was done are not served again.
	turn, suffix := 1, ""
	if b.ContentType == "image/jpeg" {
		if turn = jpegOrientation(src); turn > 1 {
			suffix = "-u"
		}
		src.Seek(0, 0)
	}
	path := filepath.Join(s.dir, "variants", b.Hash[0:2], b.Hash+"-"+strconv.Itoa(width)+suffix+"."+ext)
	if f, err := os.Open(path); err == nil {
		src.Close()
		return f, ctype, nil
	}
	cfg, _, err := image.DecodeConfig(src)
	shown := cfg.Width // the width as the image is seen
	if turn >= 5 {
		shown = cfg.Height
	}
	if err != nil || shown <= width || cfg.Width*cfg.Height > MaxPixels {
		src.Seek(0, 0)
		return src, b.ContentType, nil
	}
	if mayCreate != nil && !mayCreate() {
		src.Seek(0, 0)
		return src, b.ContentType, nil
	}
	select {
	case resizing <- struct{}{}:
	case <-time.After(ResizeWait):
		src.Seek(0, 0)
		return src, b.ContentType, nil
	}
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
	w, h := width, max(1, cfg.Height*width/cfg.Width)
	if turn >= 5 { // stored on its side: the stored height becomes the width seen
		w, h = max(1, cfg.Width*width/cfg.Height), width
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
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
		err = jpeg.Encode(tmp, turned(dst, turn), &jpeg.Options{Quality: 82})
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

// Deferred reports whether f, as Variant returned it for width, is the
// original standing in for a smaller copy that was not made this time
// (the resizer was busy), rather than the final answer.
func Deferred(f *os.File, b Blob, width int) bool {
	if !Resizable(b.ContentType) || filepath.Base(f.Name()) != b.Hash {
		return false // a stored variant, or a type that is never resized
	}
	pos, _ := f.Seek(0, 1)
	defer f.Seek(pos, 0)
	f.Seek(0, 0)
	turn := 1
	if b.ContentType == "image/jpeg" {
		turn = jpegOrientation(f)
		f.Seek(0, 0)
	}
	cfg, _, err := image.DecodeConfig(f)
	shown := cfg.Width
	if turn >= 5 {
		shown = cfg.Height
	}
	return err == nil && shown > width && cfg.Width*cfg.Height <= MaxPixels
}
