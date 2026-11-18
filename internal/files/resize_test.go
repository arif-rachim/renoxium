package files

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
	"time"
)

func TestVariant(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, nil)
	b, _, err := s.Put(&buf)
	if err != nil {
		t.Fatal(err)
	}
	f, ctype, err := s.Variant(b, SnapWidth(300), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(f)
	f.Close()
	if err != nil || ctype != "image/jpeg" || cfg.Width != 320 || cfg.Height != 160 {
		t.Fatalf("variant %s %+v %v", ctype, cfg, err)
	}
	// Cached on disk the second time; a wider request than the image gives the original.
	if f, _, err = s.Variant(b, 320, nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	f, _, _ = s.Variant(b, 1280, nil)
	cfg, _, _ = image.DecodeConfig(f)
	f.Close()
	if cfg.Width != 1000 {
		t.Fatalf("upscaled to %d", cfg.Width)
	}

	small := image.NewNRGBA(image.Rect(0, 0, 400, 400))
	small.Set(0, 0, color.NRGBA{0, 0, 0, 0})
	buf.Reset()
	png.Encode(&buf, small)
	pb, _, _ := s.Put(&buf)
	f, ctype, err = s.Variant(pb, 160, nil)
	if err != nil || ctype != "image/png" {
		t.Fatalf("png variant %s %v", ctype, err)
	}
	f.Close()
	if SnapWidth(5000) != 1920 || SnapWidth(1) != 160 {
		t.Error("SnapWidth")
	}
}

// A size that may not be made right now (the app's allowance is used up,
// or the resizer stays busy) answers with the original, marked as not the
// final answer; a later request makes the smaller copy.
func TestVariantCanBeDeferred(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, nil)
	b, _, _ := s.Put(&buf)
	width := func(f *os.File) int {
		cfg, _, _ := image.DecodeConfig(f)
		f.Seek(0, 0)
		return cfg.Width
	}

	f, _, err := s.Variant(b, 320, func() bool { return false })
	if err != nil || width(f) != 1000 || !Deferred(f, b, 320) {
		t.Fatalf("refused resize: %v", err)
	}
	f.Close()

	// The resizer is busy: the request waits ResizeWait, then gets the original.
	defer func(d time.Duration) { ResizeWait = d }(ResizeWait)
	ResizeWait = 50 * time.Millisecond
	resizing <- struct{}{}
	start := time.Now()
	f, _, err = s.Variant(b, 320, nil)
	<-resizing
	if err != nil || width(f) != 1000 || !Deferred(f, b, 320) || time.Since(start) > 2*time.Second {
		t.Fatalf("busy resizer: %v after %s", err, time.Since(start))
	}
	f.Close()

	// Free again: the copy is made, and it is the final answer.
	f, _, err = s.Variant(b, 320, func() bool { return true })
	if err != nil || width(f) != 320 || Deferred(f, b, 320) {
		t.Fatalf("resize: %v", err)
	}
	f.Close()
	// An image narrower than asked is final as it is.
	f, _, _ = s.Variant(b, 1280, nil)
	if Deferred(f, b, 1280) {
		t.Fatal("an image that is never resized was marked deferred")
	}
	f.Close()
}
