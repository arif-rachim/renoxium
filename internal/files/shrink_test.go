package files

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
)

// photo is a w x h JPEG whose top-left quarter is red and the rest blue,
// stored with EXIF orientation o (0: no EXIF at all).
func photo(w, h, o int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{0, 0, 200, 255}), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, w/2, h/2), image.NewUniform(color.RGBA{220, 0, 0, 255}), image.Point{}, draw.Src)
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	if o == 0 {
		return buf.Bytes()
	}
	// APP1: "Exif\0\0", a big-endian TIFF header, one IFD entry: Orientation (0x0112), SHORT, count 1.
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(o), 0, 0, 0, 0, 0, 0}
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(seg)+2))
	out := append([]byte{0xFF, 0xD8}, app1...)
	out = append(out, seg...)
	return append(out, buf.Bytes()[2:]...)
}

// stored decodes what the store kept for b.
func stored(t *testing.T, s *Store, b Blob) (image.Image, int) {
	t.Helper()
	f, err := s.Open(b.Hash)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	raw, _ := io.ReadAll(f)
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return img, jpegOrientation(bytes.NewReader(raw))
}

// corner names the quarter of img that is red: the photo's top-left quarter
// as it is seen.
func corner(img image.Image) string {
	b := img.Bounds()
	for name, p := range map[string]image.Point{"top-left": {b.Dx() / 4, b.Dy() / 4}, "top-right": {b.Dx() * 3 / 4, b.Dy() / 4},
		"bottom-left": {b.Dx() / 4, b.Dy() * 3 / 4}, "bottom-right": {b.Dx() * 3 / 4, b.Dy() * 3 / 4}} {
		if r, _, bl, _ := img.At(b.Min.X+p.X, b.Min.Y+p.Y).RGBA(); r > bl {
			return name
		}
	}
	return "none"
}

// A photo wider than the largest size ever shown is stored at that size,
// upright; smaller ones and other files are stored as sent.
func TestPutImageShrinksPhotos(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name         string
		w, h, o      int
		wantW, wantH int
		wantCorner   string
	}{
		{"landscape camera photo", 4000, 3000, 0, 1920, 1440, "top-left"},
		{"portrait camera photo", 3000, 4000, 0, 1440, 1920, "top-left"},
		{"phone held upright (stored on its side)", 4000, 3000, 6, 1440, 1920, "top-right"},
		{"phone held the other way", 4000, 3000, 8, 1440, 1920, "bottom-left"},
		{"upside down", 4000, 3000, 3, 1920, 1440, "bottom-right"},
		{"small photo with an orientation note", 800, 600, 6, 600, 800, "top-right"},
	} {
		raw := photo(c.w, c.h, c.o)
		b, _, err := s.PutImage(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		img, turn := stored(t, s, b)
		if got := img.Bounds(); b.ContentType != "image/jpeg" || got.Dx() != c.wantW || got.Dy() != c.wantH || turn != 1 || corner(img) != c.wantCorner {
			t.Errorf("%s: stored %s %dx%d, orientation %d, red %s; want %dx%d upright, red %s",
				c.name, b.ContentType, got.Dx(), got.Dy(), turn, corner(img), c.wantW, c.wantH, c.wantCorner)
		}
		if c.w > MaxSide && b.Size >= int64(len(raw)) {
			t.Errorf("%s: %d bytes stored for %d sent", c.name, b.Size, len(raw))
		}
	}

	// The same photo twice is one file.
	raw := photo(4000, 3000, 0)
	first, _, _ := s.PutImage(bytes.NewReader(raw))
	again, existed, _ := s.PutImage(bytes.NewReader(raw))
	if !existed || again.Hash != first.Hash {
		t.Error("the same photo was stored twice")
	}

	// A photo that already fits is stored byte for byte.
	small := photo(1920, 1080, 0)
	b, _, _ := s.PutImage(bytes.NewReader(small))
	plain, _, _ := s.Put(bytes.NewReader(small))
	if b.Hash != plain.Hash || b.Size != int64(len(small)) {
		t.Error("a photo within the limit was changed")
	}

	// A PNG stays a PNG and keeps its transparency.
	logo := image.NewNRGBA(image.Rect(0, 0, 3000, 2000))
	draw.Draw(logo, image.Rect(1000, 500, 2000, 1500), image.NewUniform(color.NRGBA{10, 120, 60, 255}), image.Point{}, draw.Src)
	var buf bytes.Buffer
	png.Encode(&buf, logo)
	b, _, err = s.PutImage(&buf)
	img, _ := stored(t, s, b)
	_, _, _, edge := img.At(5, 5).RGBA()
	_, _, _, middle := img.At(960, 640).RGBA()
	if err != nil || b.ContentType != "image/png" || img.Bounds().Dx() != 1920 || img.Bounds().Dy() != 1280 || edge != 0 || middle != 0xffff {
		t.Errorf("png: %v %s %v, alpha at the edge %d, in the middle %d", err, b.ContentType, img.Bounds(), edge, middle)
	}

	// Types that are never resized are stored as sent, and the size limit still holds.
	buf.Reset()
	gif.Encode(&buf, image.NewPaletted(image.Rect(0, 0, 2500, 10), color.Palette{color.Black, color.White}), nil)
	sent := buf.Len()
	if b, _, err = s.PutImage(&buf); err != nil || b.ContentType != "image/gif" || b.Size != int64(sent) {
		t.Errorf("gif: %v %+v", err, b)
	}
	if _, _, err = s.PutImage(io.LimitReader(zeros{}, MaxSize+1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over the size limit: %v", err)
	}
	if _, _, err = s.PutImage(bytes.NewReader([]byte("not an image"))); !errors.Is(err, ErrType) {
		t.Errorf("not an image: %v", err)
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// A photo stored with an orientation note, from before uploads were turned
// upright, gets smaller copies that are upright too.
func TestVariantOfATurnedPhoto(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.Put(bytes.NewReader(photo(1000, 500, 6))) // seen as 500 wide, 1000 high
	if err != nil {
		t.Fatal(err)
	}
	f, ctype, err := s.Variant(b, 320, nil)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil || ctype != "image/jpeg" || img.Bounds().Dx() != 320 || img.Bounds().Dy() != 640 || corner(img) != "top-right" {
		t.Fatalf("copy: %v %s %v, red %s", err, ctype, img.Bounds(), corner(img))
	}
	// It is 500 wide as seen, so a request for 640 gets the original.
	f, _, _ = s.Variant(b, 640, nil)
	defer f.Close()
	if cfg, _, _ := image.DecodeConfig(f); cfg.Width != 1000 {
		t.Errorf("a request wider than the photo gave %d wide", cfg.Width)
	}
}
