package web_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/files"
)

// An image close to the 10 MB limit fits through MCP as base64.
func TestLargeUploadThroughMCP(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "ivy@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	call(t, s, "create_app", map[string]any{"app": "ivy-shop"})

	img := image.NewRGBA(image.Rect(0, 0, 1550, 1550))
	rand.New(rand.NewSource(1)).Read(img.Pix)
	var buf bytes.Buffer
	(&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&buf, img)
	if buf.Len() > files.MaxSize || buf.Len() < 9<<20 {
		t.Fatalf("test image is %d bytes", buf.Len())
	}
	out, errText := call(t, s, "upload_file", map[string]any{"app": "ivy-shop", "data_base64": base64.StdEncoding.EncodeToString(buf.Bytes())})
	if errText != "" || out["size"] != float64(buf.Len()) {
		t.Fatalf("upload of %d bytes: %v %s", buf.Len(), out, errText)
	}
}

// A camera photo sent by an assistant through upload_file is stored at most
// 1920 px on its longest side, and the size reported is the size stored.
func TestCameraPhotoThroughMCPIsStoredSmaller(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "kai@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	call(t, s, "create_app", map[string]any{"app": "kai-shop"})

	img := image.NewRGBA(image.Rect(0, 0, 4000, 3000))
	rand.New(rand.NewSource(2)).Read(img.Pix)
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 60})
	sent := buf.Len()
	out, errText := call(t, s, "upload_file", map[string]any{"app": "kai-shop", "data_base64": base64.StdEncoding.EncodeToString(buf.Bytes())})
	if errText != "" || out["content_type"] != "image/jpeg" || out["size"].(float64) >= float64(sent) {
		t.Fatalf("upload of %d bytes: %v %s", sent, out, errText)
	}
	hash := strings.TrimSuffix(path.Base(out["url"].(string)), ".jpg")
	f, err := os.Open(filepath.Join(e.dir, "files", hash[0:2], hash[2:4], hash))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width != 1920 || cfg.Height != 1440 {
		t.Fatalf("stored photo: %v %dx%d", err, cfg.Width, cfg.Height)
	}
}
