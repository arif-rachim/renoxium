package platform_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/files"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

func rentalApp() *appdef.App {
	return &appdef.App{
		Tables: []appdef.Table{{Name: "bookings", Columns: []appdef.Column{{Name: "user_id", Type: "integer"}, {Name: "licence", Type: "file"}}}},
		Templates: map[string]string{
			"home": `<!doctype html><html><head><script src="/_rx/upload.js"></script></head><body><form method="post" action="/book">` +
				`<input type="file" name="licence" data-rx-upload accept="image/*,application/pdf"><button>Book</button></form></body></html>`,
		},
		Scripts: map[string]string{
			"book": `function handle(req) {
				const f = files.get(req.form.licence);
				if (!f || f.user_id !== req.user.id) return {status: 400, text: "upload your licence again"};
				db.exec("INSERT INTO bookings (user_id, licence) VALUES (?, ?)", req.user.id, f.id);
				return {json: f};
			}`,
			"info":   `function handle(req) { return {json: {file: files.get(req.query.id), url: files.url(req.query.id)}} }`,
			"remove": `function handle(req) { return {json: files.delete(req.form.id)} }`,
		},
		Routes: []appdef.Route{
			{Method: "GET", Path: "/", Template: "home"},
			{Method: "POST", Path: "/book", Script: "book", Auth: "user"},
			{Method: "GET", Path: "/info", Script: "info"},
			{Method: "POST", Path: "/remove", Script: "remove"},
		},
	}
}

func publishedRental(t *testing.T) (*platform.Platform, string) {
	t.Helper()
	p, _ := newAuthPlatform(t)
	if _, err := p.CreateApp("usr_test", "rental"); err != nil {
		t.Fatal(err)
	}
	if res, err := p.ApplyChange(context.Background(), "rental", rentalApp()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if _, err := p.Publish("rental", 0); err != nil {
		t.Fatal(err)
	}
	return p, "rental.renoxium.test"
}

func signedInVisitor(t *testing.T, p *platform.Platform, host, email string) *visitorBrowser {
	t.Helper()
	v := newVisitor(t, p, host)
	if w := v.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"name": {"V"}, "email": {email}, "password": {"a long password"}}); w.Code != 303 {
		t.Fatalf("signup %s: %d %s", email, w.Code, w.Body.String())
	}
	return v
}

// upload posts one file the way a browser's FormData does.
func (b *visitorBrowser) upload(name, ctype string, data []byte, header http.Header) *httptest.ResponseRecorder {
	b.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("note", "ignored")
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
	h.Set("Content-Type", ctype)
	part, _ := mw.CreatePart(h)
	part.Write(data)
	mw.Close()
	r := httptest.NewRequest("POST", "http://"+b.host+platform.UploadPath, &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	for k, v := range header {
		r.Header[k] = v
	}
	for _, c := range b.cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	b.p.ServeApp(w, r)
	return w
}

type uploaded struct {
	ID, Name, Type, Error string
	Size                  int64
}

func decodeUpload(t *testing.T, w *httptest.ResponseRecorder) uploaded {
	t.Helper()
	var u uploaded
	if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil {
		t.Fatalf("upload answer %d: %q", w.Code, w.Body.String())
	}
	return u
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 4, 4)), nil)
	return buf.Bytes()
}

var (
	pdfBytes  = []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n")
	heicBytes = append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c', 0, 0, 0, 0, 'm', 'i', 'f', '1', 'h', 'e', 'i', 'c'}, make([]byte, 64)...)
	avifBytes = append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f', 0, 0, 0, 0, 'm', 'i', 'f', '1', 'a', 'v', 'i', 'f'}, make([]byte, 64)...)
)

func TestDetectPrivateRefusesDisguisedContent(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	webp := []byte("RIFF\x10\x00\x00\x00WEBPVP8 ")
	for _, c := range []struct {
		name string
		data []byte
		want string
	}{
		{"jpeg", jpegBytes(t), "image/jpeg"},
		{"png", png, "image/png"},
		{"webp", webp, "image/webp"},
		{"heic", heicBytes, "image/heic"},
		{"pdf", pdfBytes, "application/pdf"},
		{"avif is not accepted", avifBytes, ""},
		{"gif is not accepted", []byte("GIF89a\x01\x00\x01\x00"), ""},
		{"html", []byte("<!doctype html><html><script>alert(1)</script>"), ""},
		{"html after spaces", []byte("   \n<html><body onload=alert(1)>"), ""},
		{"svg", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), ""},
		{"bare svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`), ""},
		{"javascript", []byte("alert(document.cookie)"), ""},
		{"empty", nil, ""},
	} {
		if got := files.DetectPrivate(c.data); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSanitizeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd.jpg":          "passwd.jpg",
		`C:\Users\a\licence front.JPG`:  "licence front.jpg",
		"x\"; filename=evil.html.pdf":   "x__ filename_evil.html.pdf",
		"<script>.png":                  "script.png",
		"":                              "file.png",
		"рахим паспорт.heic":            "рахим паспорт.png",
		strings.Repeat("a", 300) + ".a": strings.Repeat("a", 90) + ".png",
	} {
		ctype := "image/png"
		if strings.HasSuffix(want, ".jpg") {
			ctype = "image/jpeg"
		} else if strings.HasSuffix(want, ".pdf") {
			ctype = "application/pdf"
		}
		if got := platform.SanitizeFileName(in, ctype); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestVisitorUploads(t *testing.T) {
	p, host := publishedRental(t)
	ctx := context.Background()

	// Signed out: refused.
	guest := newVisitor(t, p, host)
	if w := guest.upload("licence.jpg", "image/jpeg", jpegBytes(t), nil); w.Code != 401 {
		t.Fatalf("signed out: %d %s", w.Code, w.Body.String())
	}
	if w := guest.do("GET", platform.UploadPath, nil); w.Code != 405 {
		t.Fatalf("GET upload: %d", w.Code)
	}

	ali := signedInVisitor(t, p, host, "ali@mail.ae")
	bea := signedInVisitor(t, p, host, "bea@mail.ae")

	// From another site: refused before anything is read.
	if w := ali.upload("licence.jpg", "image/jpeg", jpegBytes(t), http.Header{"Origin": {"https://evil.example"}}); w.Code != 403 {
		t.Fatalf("cross-site: %d", w.Code)
	}
	// Disguised content is refused whatever its name and type say.
	for name, data := range map[string][]byte{
		"licence.jpg": []byte("<html><script>alert(1)</script></html>"),
		"scan.pdf":    []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"photo.png":   []byte("alert(document.cookie)"),
	} {
		w := ali.upload(name, "image/jpeg", data, nil)
		if u := decodeUpload(t, w); w.Code != 415 || !strings.Contains(u.Error, "only JPEG") {
			t.Fatalf("%s: %d %+v", name, w.Code, u)
		}
	}
	// Over 10 MB.
	big := make([]byte, files.MaxSize+1)
	copy(big, jpegBytes(t))
	if w := ali.upload("big.jpg", "image/jpeg", big, nil); w.Code != 413 {
		t.Fatalf("too large: %d %s", w.Code, w.Body.String())
	}

	before, _ := p.StorageUsage("usr_test")
	img := jpegBytes(t)
	w := ali.upload("../Licence Front.JPEG", "application/octet-stream", img, nil)
	lic := decodeUpload(t, w)
	if w.Code != 200 || !platform.ValidPrivateID(lic.ID) || lic.Name != "Licence Front.jpg" || lic.Type != "image/jpeg" || lic.Size != int64(len(img)) {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("upload answer may be cached")
	}
	if after, _ := p.StorageUsage("usr_test"); after != before+int64(len(img)) {
		t.Fatalf("storage %d -> %d", before, after)
	}
	// The same bytes uploaded again get a new, unrelated id: nothing is deduplicated.
	again := decodeUpload(t, bea.upload("mine.jpg", "image/jpeg", img, nil))
	if again.ID == "" || again.ID == lic.ID {
		t.Fatalf("second upload: %+v", again)
	}
	pdf := decodeUpload(t, ali.upload("passport.pdf", "application/pdf", pdfBytes, nil))
	heic := decodeUpload(t, ali.upload("IMG_0001.HEIC", "image/heic", heicBytes, nil))
	if pdf.Type != "application/pdf" || heic.Type != "image/heic" || heic.Name != "IMG_0001.heic" {
		t.Fatalf("pdf %+v heic %+v", pdf, heic)
	}

	// The uploader opens it, with headers that keep it private and inert.
	w = ali.do("GET", platform.PrivatePrefix+lic.ID, nil)
	h := w.Header()
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), img) || h.Get("Content-Type") != "image/jpeg" || h.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || !strings.HasPrefix(h.Get("Cache-Control"), "private") ||
		h.Get("Content-Disposition") != `inline; filename="Licence Front.jpg"` {
		t.Fatalf("own file: %d %v", w.Code, h)
	}
	w = ali.do("GET", platform.PrivatePrefix+pdf.ID, nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/pdf" || w.Header().Get("Content-Disposition") != `attachment; filename=passport.pdf` ||
		w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("pdf: %d %v", w.Code, w.Header())
	}
	// Anyone else gets 404, as for a file that does not exist.
	for name, b := range map[string]*visitorBrowser{"other visitor": bea, "signed out": guest} {
		if w := b.do("GET", platform.PrivatePrefix+lic.ID, nil); w.Code != 404 || bytes.Contains(w.Body.Bytes(), img[:8]) {
			t.Fatalf("%s: %d", name, w.Code)
		}
	}
	if w := ali.do("GET", platform.PrivatePrefix+"pf_00000000000000000000000000000000", nil); w.Code != 404 {
		t.Fatalf("unknown id: %d", w.Code)
	}
	if w := ali.do("GET", platform.FilesPrefix+lic.ID+".jpg", nil); w.Code != 404 {
		t.Fatalf("private file through the public path: %d", w.Code)
	}

	// Scripts check ownership before saving an id from a form.
	if w := ali.do("POST", "/book", url.Values{"licence": {again.ID}}); w.Code != 400 {
		t.Fatalf("booking with someone else's file: %d %s", w.Code, w.Body.String())
	}
	if w := ali.do("POST", "/book", url.Values{"licence": {"pf_nonsense"}}); w.Code != 400 {
		t.Fatalf("booking with a made-up id: %d", w.Code)
	}
	w = ali.do("POST", "/book", url.Values{"licence": {lic.ID}})
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got["id"] != lic.ID || got["name"] != "Licence Front.jpg" || got["type"] != "image/jpeg" || got["size"] != float64(len(img)) ||
		got["user_id"] == nil || got["created_at"] == nil || got["url"] != platform.PrivatePrefix+lic.ID {
		t.Fatalf("book: %d %s", w.Code, w.Body.String())
	}
	if w := guest.do("GET", "/info?id="+lic.ID, nil); !strings.Contains(w.Body.String(), `"url":"/_rx/private/`+lic.ID) {
		t.Fatalf("info: %s", w.Body.String())
	}
	if w := guest.do("GET", "/info?id=nope", nil); w.Body.String() != `{"file":null,"url":null}` {
		t.Fatalf("info for unknown: %s", w.Body.String())
	}
	if w := guest.do("POST", "/remove", url.Values{"id": {heic.ID}}); w.Body.String() != "true" {
		t.Fatalf("delete: %s", w.Body.String())
	}
	if w := guest.do("POST", "/remove", url.Values{"id": {heic.ID}}); w.Body.String() != "false" {
		t.Fatalf("delete again: %s", w.Body.String())
	}
	if w := ali.do("GET", platform.PrivatePrefix+heic.ID, nil); w.Code != 404 {
		t.Fatalf("deleted file: %d", w.Code)
	}

	// Deleting the visitor deletes their files, not other people's.
	f, _ := p.PrivateFileInfo("rental", platform.Live, lic.ID)
	if err := p.DeleteVisitor(ctx, "rental", f.UserID, "owner@club.ae"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{lic.ID, pdf.ID} {
		if _, _, err := p.OpenPrivateFile("rental", platform.Live, id); !errors.Is(err, platform.ErrNotFound) {
			t.Fatalf("%s after visitor delete: %v", id, err)
		}
	}
	if _, fh, err := p.OpenPrivateFile("rental", platform.Live, again.ID); err != nil {
		t.Fatalf("other visitor's file: %v", err)
	} else {
		fh.Close()
	}

	// Exports include the live files.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := p.ExportApp(ctx, "rental", zw); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	var listed, content bool
	for _, zf := range zr.File {
		switch zf.Name {
		case "rental/private-files.json":
			listed = true
		case "rental/private-files/" + again.ID + "/mine.jpg":
			rc, _ := zf.Open()
			data, _ := io.ReadAll(rc)
			rc.Close()
			content = bytes.Equal(data, img)
		}
	}
	if !listed || !content {
		t.Fatalf("export: listed %v content %v", listed, content)
	}

	// Deleting the app deletes them all and frees the storage.
	if err := p.DeleteApp("rental"); err != nil {
		t.Fatal(err)
	}
	if used, _ := p.StorageUsage("usr_test"); used != 0 {
		t.Fatalf("storage after app delete: %d", used)
	}
}

func TestPreviewUploadsStayOnPreview(t *testing.T) {
	p, host := publishedRental(t)
	ctx := context.Background()
	live := signedInVisitor(t, p, host, "cai@mail.ae")
	liveFile := decodeUpload(t, live.upload("licence.jpg", "image/jpeg", jpegBytes(t), nil))

	// Preview is rebuilt from live, accounts included, but not its files.
	if res, err := p.ApplyChange(ctx, "rental", rentalApp()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	res, err := p.TestRequest(ctx, "rental", platform.TestRequestInput{Path: "/info?id=" + liveFile.ID})
	if err != nil || res.Body != `{"file":null,"url":null}` {
		t.Fatalf("live file on preview: %v %s", err, res.Body)
	}
	res, err = p.TestRequest(ctx, "rental", platform.TestRequestInput{Method: "POST", Path: platform.UploadPath, UserEmail: "cai@mail.ae",
		Files: map[string]platform.TestFile{"file": {Name: "scan.pdf", Type: "application/pdf", Base64: base64.StdEncoding.EncodeToString(pdfBytes)}}})
	var prev uploaded
	if err != nil || res.Status != 200 || json.Unmarshal([]byte(res.Body), &prev) != nil || prev.Type != "application/pdf" {
		t.Fatalf("test_request upload: %v %+v", err, res)
	}
	if len(res.Logs) == 0 || !strings.Contains(res.Logs[0].Message, "uploaded "+prev.ID) {
		t.Fatalf("upload log: %+v", res.Logs)
	}
	res, _ = p.TestRequest(ctx, "rental", platform.TestRequestInput{Method: "POST", Path: "/book", UserEmail: "cai@mail.ae", Form: map[string]string{"licence": prev.ID}})
	if res.Status != 200 {
		t.Fatalf("book on preview: %+v", res)
	}
	res, _ = p.TestRequest(ctx, "rental", platform.TestRequestInput{Method: "POST", Path: platform.UploadPath, UserEmail: "cai@mail.ae",
		Files: map[string]platform.TestFile{"file": {Name: "x.jpg", Type: "image/jpeg", Base64: base64.StdEncoding.EncodeToString([]byte("<html>"))}}})
	if res.Status != 415 {
		t.Fatalf("test_request disguised upload: %+v", res)
	}
	// Live never sees the preview file.
	if w := live.do("GET", "/info?id="+prev.ID, nil); w.Body.String() != `{"file":null,"url":null}` {
		t.Fatalf("preview file on live: %s", w.Body.String())
	}
	if w := live.do("GET", platform.PrivatePrefix+prev.ID, nil); w.Code != 404 {
		t.Fatalf("preview file on live host: %d", w.Code)
	}
	// The next change drops preview files with the rest of preview data.
	if res, err := p.ApplyChange(ctx, "rental", rentalApp()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if _, err := p.PrivateFileInfo("rental", platform.Preview, prev.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("preview file after rebuild: %v", err)
	}
	if _, fh, err := p.OpenPrivateFile("rental", platform.Live, liveFile.ID); err != nil {
		t.Fatalf("live file after preview rebuild: %v", err)
	} else {
		fh.Close()
	}
}

func TestUploadsAreRateLimited(t *testing.T) {
	p, host := publishedRental(t)
	v := signedInVisitor(t, p, host, "dan@mail.ae")
	img := jpegBytes(t)
	for i := 0; i < platform.UploadsPerVisitorPerDay; i++ {
		if w := v.upload("a.jpg", "image/jpeg", img, nil); w.Code != 200 {
			t.Fatalf("upload %d: %d", i, w.Code)
		}
	}
	if w := v.upload("a.jpg", "image/jpeg", img, nil); w.Code != 429 {
		t.Fatalf("over the daily limit: %d", w.Code)
	}
	// Another visitor still can.
	if w := signedInVisitor(t, p, host, "eve@mail.ae").upload("a.jpg", "image/jpeg", img, nil); w.Code != 200 {
		t.Fatalf("other visitor: %d", w.Code)
	}
}

func TestUploadScriptIsServed(t *testing.T) {
	p, host := publishedRental(t)
	w := newVisitor(t, p, host).do("GET", platform.UploadJSPath, nil)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") || !strings.Contains(w.Body.String(), "data-rx-upload") {
		t.Fatalf("upload.js: %d %s", w.Code, w.Header())
	}
}

func TestFileColumnType(t *testing.T) {
	def := rentalApp()
	if problems := def.Validate(); len(problems) > 0 {
		t.Fatal(problems)
	}
	def.Tables[0].Columns = append(def.Tables[0].Columns, appdef.Column{Name: "scan", Type: "document"})
	if problems := def.Validate(); len(problems) == 0 || !strings.Contains(strings.Join(problems, " "), "image or file") {
		t.Fatalf("unknown type: %v", problems)
	}
}

// Scripts in worker processes (no files there) reach the records through
// the host, and the ownership check works across the process boundary.
func TestFilesAPIInWorkers(t *testing.T) {
	pool, err := sandbox.NewPool(sandbox.PoolConfig{Workers: 1, Timeout: 3 * time.Second, HeapLimit: 32 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	p := newPlatformWith(t, pool)
	p.CreateApp("usr_test", "rental")
	if res, err := p.ApplyChange(context.Background(), "rental", rentalApp()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if _, err := p.Publish("rental", 0); err != nil {
		t.Fatal(err)
	}
	host := "rental.renoxium.test"
	v := signedInVisitor(t, p, host, "fay@mail.ae")
	other := signedInVisitor(t, p, host, "gus@mail.ae")
	mine := decodeUpload(t, v.upload("l.jpg", "image/jpeg", jpegBytes(t), nil))
	theirs := decodeUpload(t, other.upload("l.jpg", "image/jpeg", jpegBytes(t), nil))
	if w := v.do("POST", "/book", url.Values{"licence": {mine.ID}}); w.Code != 200 {
		t.Fatalf("own file: %d %s", w.Code, w.Body.String())
	}
	if w := v.do("POST", "/book", url.Values{"licence": {theirs.ID}}); w.Code != 400 {
		t.Fatalf("other's file: %d %s", w.Code, w.Body.String())
	}
}

// Anyone can sign up, so visitors' uploads must not be able to fill the
// owner's storage, which the owner's own product photos share.
func TestVisitorUploadBytesAreCapped(t *testing.T) {
	p, host := publishedRental(t)
	big := func(n int) []byte {
		b := make([]byte, n)
		copy(b, "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
		return b
	}
	v := signedInVisitor(t, p, host, "filler@mail.ae")
	file := big(9 << 20)
	stored := 0
	for i := 0; i < 6; i++ {
		w := v.upload("scan.png", "image/png", file, nil)
		if w.Code == 200 {
			stored++
			continue
		}
		if w.Code != http.StatusInsufficientStorage || !strings.Contains(w.Body.String(), "the most one person may") {
			t.Fatalf("upload %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if want := platform.MaxPrivateBytesPerVisitor / (9 << 20); stored != int(want) {
		t.Fatalf("one visitor stored %d files of 9 MB, want %d", stored, want)
	}
	// Another visitor still can.
	if w := signedInVisitor(t, p, host, "other@mail.ae").upload("licence.png", "image/png", big(1<<20), nil); w.Code != 200 {
		t.Fatalf("another visitor: %d %s", w.Code, w.Body.String())
	}
}
