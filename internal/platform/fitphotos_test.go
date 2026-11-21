package platform_test

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"math/rand"
	"testing"

	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// Photos stored before uploads were shrunk are made to fit once, in the
// background: the address stays, the page still shows the photo, and the
// owner's storage usage goes down.
func TestFitStoredPhotos(t *testing.T) {
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	if _, err := p.CreateApp("usr_test", "shop"); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 3000, 2000))
	rand.New(rand.NewSource(3)).Read(img.Pix)
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70})
	url := platform.AddFileAsBefore(t, p, "shop", buf.Bytes())
	// A new upload is already as small as it gets and is not looked at again.
	if _, err := p.AddFile("shop", bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	before, _ := p.StorageUsage("usr_test")

	if n := p.FitStoredPhotos(context.Background(), 0); n != 1 {
		t.Fatalf("%d files made to fit, want 1", n)
	}
	after, _ := p.StorageUsage("usr_test")
	if after >= before {
		t.Errorf("storage usage %d, was %d", after, before)
	}
	r, err := p.TestRequest(context.Background(), "shop", platform.TestRequestInput{Path: url})
	if err != nil || r.Status != 200 || r.ContentType != "image/jpeg" {
		t.Errorf("the photo at its old address: %v %d %s", err, r.Status, r.ContentType)
	}
	if n := p.FitStoredPhotos(context.Background(), 0); n != 0 {
		t.Errorf("a second pass changed %d files", n)
	}
}
