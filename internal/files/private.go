package files

import (
	"encoding/binary"
	"errors"
)

// PrivateTypes maps the content types visitors may upload as private files
// to the extension used in their names.
var PrivateTypes = map[string]string{
	"image/jpeg":      "jpg",
	"image/png":       "png",
	"image/webp":      "webp",
	"image/heic":      "heic",
	"image/heif":      "heif",
	"application/pdf": "pdf",
}

// ErrPrivateType is returned for content visitors may not upload.
var ErrPrivateType = errors.New("only JPEG, PNG, WebP and HEIC photos and PDF documents are accepted")

// DetectPrivate identifies a visitor upload from its first bytes (512 are
// enough). It returns "" for anything not in PrivateTypes, so HTML, SVG,
// scripts and other files with an image or PDF name are refused.
func DetectPrivate(head []byte) string {
	if t := detectHEIF(head); t != "" {
		return t
	}
	t := Detect(head)
	if _, ok := PrivateTypes[t]; ok {
		return t
	}
	return ""
}

// detectHEIF reads an ISO-BMFF "ftyp" box: HEIC and HEIF photos (as
// iPhones take them) carry heic-family brands; AVIF, which shares the
// format, is not a private type and returns "".
func detectHEIF(head []byte) string {
	if len(head) < 16 || string(head[4:8]) != "ftyp" {
		return ""
	}
	size := int(binary.BigEndian.Uint32(head[0:4]))
	if size < 16 || size > len(head) {
		size = len(head)
	}
	brands := []string{string(head[8:12])}
	for i := 16; i+4 <= size; i += 4 {
		brands = append(brands, string(head[i:i+4]))
	}
	heic, heif := false, false
	for _, b := range brands {
		switch b {
		case "avif", "avis":
			return ""
		case "heic", "heix", "heim", "heis", "hevc", "hevx":
			heic = true
		case "mif1", "msf1":
			heif = true
		}
	}
	switch {
	case heic:
		return "image/heic"
	case heif:
		return "image/heif"
	}
	return ""
}
