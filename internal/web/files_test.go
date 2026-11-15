package web_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

func tinyPNG(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 2, 2))
	img.Pix[0] = shade
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func (e *env) appGet(host, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "http://"+host+path, nil)
	w := httptest.NewRecorder()
	e.p.ServeApp(w, r)
	return w
}

func TestAgentUploadsAnImage(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "gina@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	call(t, s, "create_app", map[string]any{"app": "gina-shop"})
	img := tinyPNG(t, 200)

	out, errText := call(t, s, "upload_file", map[string]any{"app": "gina-shop", "data_base64": base64.StdEncoding.EncodeToString(img)})
	if errText != "" || out["reused"] != false {
		t.Fatalf("upload: %v %s", out, errText)
	}
	url := out["url"].(string)
	again, _ := call(t, s, "upload_file", map[string]any{"app": "gina-shop", "data_base64": base64.RawStdEncoding.EncodeToString(img)})
	if again["url"] != url || again["reused"] != true {
		t.Fatalf("second upload: %v", again)
	}
	if w := e.appGet("gina-shop.renoxium.test", url); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), img) {
		t.Fatalf("serve: %d", w.Code)
	}
	if _, errText := call(t, s, "upload_file", map[string]any{"app": "gina-shop", "data_base64": base64.StdEncoding.EncodeToString([]byte("<html>"))}); !strings.Contains(errText, "only JPEG") {
		t.Fatalf("html upload: %q", errText)
	}
	if _, errText := call(t, s, "upload_file", map[string]any{"app": "gina-shop", "data_base64": "%%%"}); !strings.Contains(errText, "base64") {
		t.Fatalf("bad base64: %q", errText)
	}
}

func TestAdminImageUpload(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "hana@shop.ae")
	def := &appdef.App{
		Tables:    []appdef.Table{{Name: "products", Columns: []appdef.Column{{Name: "name", Type: "text"}, {Name: "photo", Type: "image"}}}},
		Seed:      map[string][]map[string]any{"products": {{"name": "Oud"}}},
		Templates: map[string]string{"t": "x"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
	}
	publish(e, c, "hana-shop", def)

	submit := func(fields map[string]string, file []byte) (*http.Response, string) {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fields["csrf"] = e.csrf(c, "/")
		for k, v := range fields {
			mw.WriteField(k, v)
		}
		if file != nil {
			fw, _ := mw.CreateFormFile("photo__upload", "photo.png")
			fw.Write(file)
		}
		mw.Close()
		req, _ := http.NewRequest("POST", e.base+"/apps/hana-shop/t/products/1", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		r, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r, read(t, r)
	}

	img := tinyPNG(t, 77)
	r, _ := submit(map[string]string{"name": "Oud", "photo": ""}, img)
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("upload: %d", r.StatusCode)
	}
	row, _ := e.p.AdminGet(t.Context(), "hana-shop", "products", 1)
	url, _ := row["photo"].(string)
	if !strings.HasPrefix(url, "/_rx/files/") {
		t.Fatalf("photo = %v", row["photo"])
	}
	if w := e.appGet("hana-shop.renoxium.test", url); w.Code != 200 {
		t.Fatalf("served: %d", w.Code)
	}
	// The edit form shows the image from the live app.
	resp, _ := c.Get(e.base + "/apps/hana-shop/t/products/1")
	if page := read(t, resp); !strings.Contains(page, `src="https://hana-shop.renoxium.test`+url+`"`) || !strings.Contains(page, `enctype="multipart/form-data"`) {
		t.Fatalf("form: %s", page)
	}
	// Saving without a new file keeps the image.
	submit(map[string]string{"name": "Royal Oud", "photo": url}, nil)
	if row, _ := e.p.AdminGet(t.Context(), "hana-shop", "products", 1); row["photo"] != url || row["name"] != "Royal Oud" {
		t.Fatalf("kept: %v", row)
	}
	// A file that is not an image is refused with a message on the form.
	r, page := submit(map[string]string{"name": "Royal Oud", "photo": url}, []byte("<script>alert(1)</script>"))
	if r.StatusCode != 422 || !strings.Contains(page, "only JPEG") {
		t.Fatalf("bad file: %d %s", r.StatusCode, page)
	}
	// Remove.
	submit(map[string]string{"name": "Royal Oud", "photo": url, "photo__remove": "1"}, nil)
	if row, _ := e.p.AdminGet(t.Context(), "hana-shop", "products", 1); row["photo"] != nil {
		t.Fatalf("removed: %v", row["photo"])
	}
}

// publish builds and publishes an app through MCP as the browser's user.
func publish(e *env, c *http.Client, app string, def *appdef.App) {
	e.t.Helper()
	e.publishDef(c, app, def)
}
