package files

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/image/draw"
)

// MaxSide is the longest side a stored photo keeps: no page shows an image
// wider than the largest of Widths, so anything beyond it only fills the disk.
const MaxSide = 1920

// MaxUpload is the largest photo accepted for shrinking: camera photos are
// often over MaxSize as sent and well under it once they fit. What is
// stored is never over MaxSize.
const MaxUpload = 25 << 20

// ErrPhotoTooLarge is returned for photos over MaxUpload.
var ErrPhotoTooLarge = fmt.Errorf("the photo is larger than %d MB", MaxUpload>>20)

// ShrinkPixels is the largest photo shrunk on upload. Decoding one needs
// about 1.5 bytes a pixel, one photo at a time; bigger ones are stored as
// sent.
const ShrinkPixels = 50_000_000

// ShrinkWait is how long an upload waits for the resizer before the file
// is stored as sent.
var ShrinkWait = 30 * time.Second

// masterQuality is the JPEG quality of a shrunk photo. It is higher than the
// quality of the sizes made from it, which are encoded once more.
const masterQuality = 88

// PutImage stores an upload like Put, but first makes a photo fit: a JPEG,
// PNG or WebP whose longest side is over MaxSide is scaled down to it, and a
// JPEG taken with the camera turned is turned upright, since the copy that
// is stored carries no orientation note. JPEG and WebP become JPEG; PNG
// stays PNG and keeps its transparency. Everything else, and a photo that
// cannot be read, is stored as sent.
func (s *Store) PutImage(r io.Reader) (Blob, bool, error) {
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "incoming-*")
	if err != nil {
		return Blob{}, false, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, io.LimitReader(r, MaxUpload+1))
	if err != nil {
		return Blob{}, false, err
	}
	if n > MaxUpload {
		return Blob{}, false, ErrPhotoTooLarge
	}
	if small := s.shrink(tmp, true); small != nil {
		return s.Put(small)
	}
	if n > MaxSize { // not a photo that could be made smaller
		return Blob{}, false, ErrTooLarge
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return Blob{}, false, err
	}
	return s.Put(tmp)
}

// shrink returns the smaller copy of the photo in f, or nil when f should be
// stored as it is. With upright, a JPEG that already fits but carries an
// orientation note is turned too.
func (s *Store) shrink(f *os.File, upright bool) *bytes.Reader {
	head := make([]byte, 512)
	f.Seek(0, io.SeekStart)
	k, _ := io.ReadFull(f, head)
	ctype := Detect(head[:k])
	if !Resizable(ctype) {
		return nil
	}
	f.Seek(0, io.SeekStart)
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil
	}
	turn := 1
	if ctype == "image/jpeg" {
		f.Seek(0, io.SeekStart)
		turn = jpegOrientation(f)
	}
	long := max(cfg.Width, cfg.Height)
	if (long <= MaxSide && (turn == 1 || !upright)) || cfg.Width*cfg.Height > ShrinkPixels {
		return nil
	}
	select {
	case resizing <- struct{}{}:
	case <-time.After(ShrinkWait):
		return nil
	}
	defer func() { <-resizing }()
	f.Seek(0, io.SeekStart)
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	w, h := cfg.Width, cfg.Height
	if long > MaxSide {
		w, h = max(1, cfg.Width*MaxSide/long), max(1, cfg.Height*MaxSide/long)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if ctype != "image/png" { // no transparency in JPEG: start from white
		draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	}
	if long > MaxSide {
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	} else {
		draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Over)
	}
	var out bytes.Buffer
	if ctype == "image/png" {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, dst)
	} else {
		err = jpeg.Encode(&out, turned(dst, turn), &jpeg.Options{Quality: masterQuality})
	}
	if err != nil {
		return nil
	}
	return bytes.NewReader(out.Bytes())
}

// jpegOrientation reads the EXIF orientation of a JPEG (1 to 8); 1, upright,
// when there is none.
func jpegOrientation(r io.Reader) int {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:2]); err != nil || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	for {
		if _, err := io.ReadFull(r, b[:4]); err != nil || b[0] != 0xFF {
			return 1
		}
		marker, size := b[1], int(binary.BigEndian.Uint16(b[2:4]))-2
		if marker == 0xDA || size < 0 { // image data starts: no EXIF before it
			return 1
		}
		seg := make([]byte, size)
		if _, err := io.ReadFull(r, seg); err != nil {
			return 1
		}
		if marker != 0xE1 || len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
			continue
		}
		t := seg[6:]
		var order binary.ByteOrder = binary.BigEndian
		if string(t[:2]) == "II" {
			order = binary.LittleEndian
		} else if string(t[:2]) != "MM" {
			return 1
		}
		off := int(order.Uint32(t[4:8]))
		if off+2 > len(t) {
			return 1
		}
		count := int(order.Uint16(t[off : off+2]))
		for i := 0; i < count; i++ {
			e := off + 2 + i*12
			if e+12 > len(t) {
				return 1
			}
			if order.Uint16(t[e:e+2]) == 0x0112 {
				if v := int(order.Uint16(t[e+8 : e+10])); v >= 1 && v <= 8 {
					return v
				}
				return 1
			}
		}
		return 1
	}
}

// turned turns an image stored with EXIF orientation o the way it is meant
// to be seen.
func turned(src *image.RGBA, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			si, di := src.PixOffset(x, y), dst.PixOffset(dx, dy)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}

// FitStored makes a photo stored before uploads were shrunk fit MaxSide, in
// place: its address is its hash, which rows of the apps hold, so the file
// keeps its name and type. Only JPEG and PNG can be written back as the
// type they are; other types and photos that fit are left alone. The
// photo's smaller copies are removed, to be made again from the new file.
// It returns the size now stored and whether the file changed.
func (s *Store) FitStored(b Blob) (int64, bool, error) {
	if b.ContentType != "image/jpeg" && b.ContentType != "image/png" {
		return b.Size, false, nil
	}
	f, err := s.Open(b.Hash)
	if err != nil {
		return b.Size, false, err
	}
	small := s.shrink(f, false)
	f.Close()
	if small == nil || int64(small.Len()) >= b.Size {
		return b.Size, false, nil
	}
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "fit-*")
	if err != nil {
		return b.Size, false, err
	}
	defer os.Remove(tmp.Name())
	size, err := io.Copy(tmp, small)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.path(b.Hash))
	}
	if err != nil {
		return b.Size, false, err
	}
	old, _ := filepath.Glob(filepath.Join(s.dir, "variants", b.Hash[0:2], b.Hash+"-*"))
	for _, v := range old {
		os.Remove(v)
	}
	return size, true, nil
}
