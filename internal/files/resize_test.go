package files

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
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
	f, ctype, err := s.Variant(b, SnapWidth(300))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(f)
	f.Close()
	if err != nil || ctype != "image/jpeg" || cfg.Width != 320 || cfg.Height != 160 {
		t.Fatalf("variant %s %+v %v", ctype, cfg, err)
	}
	// Cached on disk the second time; a wider request than the image gives the original.
	if f, _, err = s.Variant(b, 320); err != nil {
		t.Fatal(err)
	}
	f.Close()
	f, _, _ = s.Variant(b, 1280)
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
	f, ctype, err = s.Variant(pb, 160)
	if err != nil || ctype != "image/png" {
		t.Fatalf("png variant %s %v", ctype, err)
	}
	f.Close()
	if SnapWidth(5000) != 1920 || SnapWidth(1) != 160 {
		t.Error("SnapWidth")
	}
}
