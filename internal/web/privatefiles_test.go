package web_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// visitorUpload posts a file to /_rx/upload as a signed-in visitor.
func (e *env) visitorUpload(host string, session *http.Cookie, name string, data []byte) map[string]any {
	e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", name)
	part.Write(data)
	mw.Close()
	r := httptest.NewRequest("POST", "http://"+host+platform.UploadPath, &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.AddCookie(session)
	w := httptest.NewRecorder()
	e.p.ServeApp(w, r)
	var out map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		e.t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	return out
}

func TestOwnerOpensVisitorFilesInThePanel(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "rana@cars.ae")
	e.publishDef(owner, "rana-cars", &appdef.App{
		Tables: []appdef.Table{{Name: "bookings", Columns: []appdef.Column{{Name: "user_id", Type: "integer"}, {Name: "licence", Type: "file", Label: "Driving licence"}}}},
		Scripts: map[string]string{"book": `function handle(req) {
			const f = files.get(req.form.licence);
			if (!f || f.user_id !== req.user.id) return {status: 400, text: "upload again"};
			db.exec("INSERT INTO bookings (user_id, licence) VALUES (?, ?)", req.user.id, f.id);
			return {redirect: "/"};
		}`},
		Templates: map[string]string{"home": "ok"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "home"}, {Method: "POST", Path: "/book", Script: "book", Auth: "user"}},
	})
	host := "rana-cars.renoxium.test"
	sami := e.signUpVisitor(host, "sami@mail.ae", "Sami")
	img := tinyPNG(t, 90)
	up := e.visitorUpload(host, sami, "licence.png", img)
	id := up["id"].(string)
	r := httptest.NewRequest("POST", "http://"+host+"/book", strings.NewReader(url.Values{"licence": {id}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(sami)
	w := httptest.NewRecorder()
	e.p.ServeApp(w, r)
	if w.Code != 303 {
		t.Fatalf("book: %d %s", w.Code, w.Body.String())
	}

	get := func(c *http.Client, path string) (*http.Response, string) {
		resp, err := c.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		return resp, read(t, resp)
	}
	if _, page := get(owner, "/apps/rana-cars/t/bookings"); !strings.Contains(page, "licence.png") {
		t.Fatalf("list shows no file name: %s", page)
	}
	if _, page := get(owner, "/apps/rana-cars/t/bookings/1"); !strings.Contains(page, `href="/apps/rana-cars/files/`+id+`"`) || !strings.Contains(page, "licence__delete") {
		t.Fatalf("form has no file link: %s", page)
	}
	if _, page := get(owner, "/apps/rana-cars/visitors"); !strings.Contains(page, "<td>1</td>") {
		t.Fatalf("visitors page has no file count: %s", page)
	}
	resp, body := get(owner, "/apps/rana-cars/files/"+id)
	if resp.StatusCode != 200 || body != string(img) || resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") || !strings.HasPrefix(resp.Header.Get("Cache-Control"), "private") {
		t.Fatalf("owner opens file: %d %v", resp.StatusCode, resp.Header)
	}
	// Another Renoxium account, and nobody signed in, cannot.
	other := e.browser()
	e.signup(other, "mallory@else.ae")
	if resp, body := get(other, "/apps/rana-cars/files/"+id); resp.StatusCode != 404 || strings.Contains(body, string(img)) {
		t.Fatalf("other account: %d", resp.StatusCode)
	}
	if resp, _ := get(e.browser(), "/apps/rana-cars/files/"+id); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signed out: %d", resp.StatusCode)
	}

	// The owner deletes the file from the row.
	resp, err := owner.PostForm(e.base+"/apps/rana-cars/t/bookings/1", url.Values{"csrf": {e.csrf(owner, "/")}, "user_id": {"1"}, "licence": {id}, "licence__delete": {"1"}})
	if err != nil || resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: %v %d", err, resp.StatusCode)
	}
	if _, err := e.p.PrivateFileInfo("rana-cars", platform.Live, id); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("file after delete: %v", err)
	}
	row, _ := e.p.AdminGet(t.Context(), "rana-cars", "bookings", 1)
	if row["licence"] != nil {
		t.Fatalf("column after delete: %v", row["licence"])
	}
	if resp, _ := get(owner, "/apps/rana-cars/files/"+id); resp.StatusCode != 404 {
		t.Fatalf("deleted file: %d", resp.StatusCode)
	}
}
