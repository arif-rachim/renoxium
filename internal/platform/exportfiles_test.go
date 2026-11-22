package platform_test

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// An app's export holds its uploaded files themselves, under the name their
// address ends with, so it is everything the app needs to run elsewhere.
func TestExportHoldsTheFiles(t *testing.T) {
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	if _, err := p.CreateApp("usr_test", "shop"); err != nil {
		t.Fatal(err)
	}
	var img bytes.Buffer
	png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 40, 30)))
	sent := img.Bytes()
	fi, err := p.AddFile("shop", bytes.NewReader(sent))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := p.ExportApp(context.Background(), "shop", zw); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	want := "shop/files/" + strings.TrimPrefix(fi.URL, platform.FilesPrefix)
	for _, f := range zr.File {
		if f.Name != want {
			continue
		}
		rc, _ := f.Open()
		got, _ := io.ReadAll(rc)
		rc.Close()
		if !bytes.Equal(got, sent) {
			t.Fatalf("%s holds %d bytes, want the %d uploaded", want, len(got), len(sent))
		}
		return
	}
	t.Fatalf("the export lacks %s", want)
}
