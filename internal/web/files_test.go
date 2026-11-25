package web_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/arif-rachim/renoxium/internal/files"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
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
	if page := read(t, resp); !strings.Contains(page, `src="https://hana-shop.renoxium.test`+url+`?w=320"`) || !strings.Contains(page, `enctype="multipart/form-data"`) {
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
	// A camera photo over 10 MB is accepted and stored smaller, at the largest size ever shown.
	big := image.NewRGBA(image.Rect(0, 0, 4000, 3000))
	rand.New(rand.NewSource(5)).Read(big.Pix)
	var camera bytes.Buffer
	jpeg.Encode(&camera, big, &jpeg.Options{Quality: 97})
	if camera.Len() <= files.MaxSize || camera.Len() > files.MaxUpload {
		t.Fatalf("test photo is %d bytes", camera.Len())
	}
	if r, page := submit(map[string]string{"name": "Royal Oud", "photo": url}, camera.Bytes()); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("camera photo of %d bytes: %d %s", camera.Len(), r.StatusCode, page)
	}
	row, _ = e.p.AdminGet(t.Context(), "hana-shop", "products", 1)
	photo, _ := row["photo"].(string)
	w := e.appGet("hana-shop.renoxium.test", photo)
	cfg, _, err := image.DecodeConfig(w.Body)
	if photo == url || w.Code != 200 || err != nil || cfg.Width != 1920 || cfg.Height != 1440 {
		t.Fatalf("camera photo stored as %s: %d %v %dx%d", photo, w.Code, err, cfg.Width, cfg.Height)
	}
	url = photo
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

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A photo the user attached in ChatGPT arrives as a download address
// (openai/fileParams), and any public https image can be copied by url;
// neither needs the assistant to type the file out as base64.
func TestAgentUploadsFromAnAddress(t *testing.T) {
	img := tinyPNG(t, 90)
	var asked []string
	e := newEnvPlans(t, false, func(c *platform.Config) {
		c.FetchTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
			asked = append(asked, r.URL.String())
			switch r.URL.Path {
			case "/attachment":
				h := http.Header{"Location": {"https://cdn.example.com/photo.png"}}
				return &http.Response{StatusCode: 302, Header: h, Body: http.NoBody, Request: r}, nil
			case "/photo.png":
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(img)), Request: r}, nil
			}
			return &http.Response{StatusCode: 403, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
		})
	})
	c := e.browser()
	e.signup(c, "ivo@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	call(t, s, "create_app", map[string]any{"app": "ivo-shop"})

	out, errText := call(t, s, "upload_file", map[string]any{"app": "ivo-shop",
		"file": map[string]any{"download_url": "https://files.example.com/attachment", "file_id": "file_123"}})
	if errText != "" {
		t.Fatalf("chat file: %s (asked %v)", errText, asked)
	}
	if w := e.appGet("ivo-shop.renoxium.test", out["url"].(string)); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), img) {
		t.Fatalf("serve: %d", w.Code)
	}
	again, errText := call(t, s, "upload_file", map[string]any{"app": "ivo-shop", "url": "https://cdn.example.com/photo.png"})
	if errText != "" || again["url"] != out["url"] {
		t.Fatalf("by url: %v %s", again, errText)
	}
	if _, errText := call(t, s, "upload_file", map[string]any{"app": "ivo-shop", "url": "https://files.example.com/expired"}); !strings.Contains(errText, "403") {
		t.Fatalf("expired address: %q", errText)
	}
	if _, errText := call(t, s, "upload_file", map[string]any{"app": "ivo-shop", "url": "http://cdn.example.com/photo.png"}); !strings.Contains(errText, "https") {
		t.Fatalf("plain http: %q", errText)
	}
	if _, errText := call(t, s, "upload_file", map[string]any{"app": "ivo-shop", "url": "https://cdn.example.com/photo.png",
		"data_base64": base64.StdEncoding.EncodeToString(img)}); !strings.Contains(errText, "exactly one") {
		t.Fatalf("two sources: %q", errText)
	}
}

// What assistants commonly get wrong in base64 is accepted where it is
// harmless and named where it is not.
func TestAgentUploadBase64Errors(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "juno@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	call(t, s, "create_app", map[string]any{"app": "juno-shop"})
	img := tinyPNG(t, 30)
	b64 := base64.StdEncoding.EncodeToString(img)
	if _, errText := call(t, s, "upload_file", map[string]any{"app": "juno-shop", "data_base64": "data:image/png;base64," + b64}); errText != "" {
		t.Fatalf("data URL: %s", errText)
	}
	if _, errText := call(t, s, "upload_file", map[string]any{"app": "juno-shop", "data_base64": base64.RawURLEncoding.EncodeToString(img)}); errText != "" {
		t.Fatalf("URL-safe: %s", errText)
	}
	if _, errText := call(t, s, "upload_file", map[string]any{"app": "juno-shop", "data_base64": b64[:10] + "*" + b64[11:]}); !strings.Contains(errText, "character 11") {
		t.Fatalf("bad character: %q", errText)
	}
	var photo bytes.Buffer
	jpeg.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 600, 400)), nil)
	cut := base64.StdEncoding.EncodeToString(photo.Bytes()[:photo.Len()/2])
	if _, errText := call(t, s, "upload_file", map[string]any{"app": "juno-shop", "data_base64": cut}); !strings.Contains(errText, "incomplete or damaged") {
		t.Fatalf("cut photo: %q", errText)
	}
}

// A link to a page of the preview through the owner's account opens that
// page, as assistants link to the pages they changed.
func TestPreviewDeepLink(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "lena@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	call(t, s, "create_app", map[string]any{"app": "lena-shop"})
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, err := c.Get(e.base + "/apps/lena-shop/preview/queue?lang=ar")
	if err != nil {
		t.Fatal(err)
	}
	if loc := r.Header.Get("Location"); r.StatusCode != 303 || !strings.Contains(loc, "lena-shop--preview.") || !strings.HasSuffix(loc, "/queue?lang=ar") && !strings.Contains(loc, "next=%2Fqueue%3Flang%3Dar") {
		t.Fatalf("deep link: %d %s", r.StatusCode, loc)
	}
}

// Before the first publish the owner can switch the data pages to the
// preview's test data and add a photo there; the assistant then sees the
// photo and the row that uses it. Live data is not touched.
func TestAdminEditsPreviewData(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "mira@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	call(t, s, "create_app", map[string]any{"app": "mira-shop"})
	def := map[string]any{
		"tables":    []any{map[string]any{"name": "products", "columns": []any{map[string]any{"name": "name", "type": "text"}, map[string]any{"name": "photo", "type": "image"}}}},
		"seed":      map[string]any{"products": []any{map[string]any{"name": "Oud"}}},
		"templates": map[string]any{"t": "x"},
		"routes":    []any{map[string]any{"method": "GET", "path": "/", "template": "t"}},
	}
	if out, errText := call(t, s, "apply_change", map[string]any{"app": "mira-shop", "definition": def}); errText != "" || out["ok"] != true {
		t.Fatalf("apply: %v %s", out, errText)
	}
	// Not published: the live data pages have no tables, the switch is there.
	resp, _ := c.Get(e.base + "/apps/mira-shop")
	if page := read(t, resp); !strings.Contains(page, "Edit preview data") || strings.Contains(page, "/apps/mira-shop/t/products") {
		t.Fatalf("overview before switching: %s", page)
	}
	if resp, _ = c.Get(e.base + "/apps/mira-shop/data-stage?stage=preview&back=/apps/mira-shop/t/products"); resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/apps/mira-shop/t/products" {
		t.Fatalf("switch: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, _ = c.Get(e.base + "/apps/mira-shop/t/products")
	page := read(t, resp)
	if !strings.Contains(page, "Preview data.") || !strings.Contains(page, "Oud") || !strings.Contains(page, "Back to live data") {
		t.Fatalf("preview data page: %d %s", resp.StatusCode, page)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("csrf", e.csrf(c, "/"))
	mw.WriteField("name", "Oud")
	mw.WriteField("photo", "")
	fw, _ := mw.CreateFormFile("photo__upload", "photo.png")
	fw.Write(tinyPNG(t, 120))
	mw.Close()
	req, _ := http.NewRequest("POST", e.base+"/apps/mira-shop/t/products/1", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if r, err := c.Do(req); err != nil || r.StatusCode != http.StatusSeeOther {
		t.Fatalf("save preview row: %v %v", err, r)
	}
	out, _ := call(t, s, "get_app_outline", map[string]any{"app": "mira-shop"})
	files, _ := out["files"].([]any)
	if len(files) != 1 || !strings.Contains(fmt.Sprint(files[0]), "products#1.photo") {
		t.Fatalf("outline files: %v", out["files"])
	}
	// Back to live: no tables again, and nothing was published.
	c.Get(e.base + "/apps/mira-shop/data-stage?back=/apps/mira-shop")
	resp, _ = c.Get(e.base + "/apps/mira-shop")
	if page := read(t, resp); strings.Contains(page, "Preview data.") || !strings.Contains(page, "Edit preview data") {
		t.Fatalf("back to live: %s", page)
	}
}

// create_app takes the business's data in the same call, and says when
// the home page could not be checked in a browser.
func TestCreateAppWithBusinessData(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "nora@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	out, errText := call(t, s, "create_app", map[string]any{"app": "nora-oud", "from_example": "store-uae", "business": "Nora Oud",
		"settings": map[string]any{"whatsapp": "+971501234567"}, "rows": map[string]any{"products": []any{map[string]any{"name": "Rose Oud", "price": 90, "active": 1, "stock": 3}}},
		"check": true})
	if errText != "" {
		t.Fatal(errText)
	}
	if apply, _ := out["apply"].(map[string]any); apply["ok"] != true {
		t.Fatalf("apply: %v", out["apply"])
	}
	if home, _ := out["home"].(map[string]any); home == nil || home["note"] == nil {
		t.Fatalf("home check: %v", out["home"])
	}
	page, _ := call(t, s, "view_page", map[string]any{"app": "nora-oud", "path": "/"})
	if body, _ := page["body"].(string); !strings.Contains(body, "Rose Oud") || !strings.Contains(body, "Nora Oud") {
		t.Fatalf("home page: %.300s", body)
	}
}
