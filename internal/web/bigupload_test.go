package web_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"math/rand"
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
