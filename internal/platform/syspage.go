package platform

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"
)

// System pages: what the platform shows a person in a browser when there
// is no app page to show (a private preview, a suspended or unpublished
// app, a page that does not exist, an app error). They are plain and
// neutral, since they appear on the business's own address.

type sysPageData struct {
	Title, Heading string
	Body           template.HTML
	Link, LinkText string
	Code           int
}

var sysPages = template.Must(template.New("sys").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex"><title>{{.Title}}</title><style>
:root{color-scheme:light dark;--fg:#111827;--muted:#5b6474;--bg:#f4f5f7;--card:#fff;--line:#dcdfe5;--accent:#13203a;--on:#fff}
@media (prefers-color-scheme:dark){:root{--fg:#eef0f4;--muted:#a3abb9;--bg:#0f1218;--card:#181c24;--line:#2c3240;--accent:#e7ecf4;--on:#0f1218}}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px 16px;background:var(--bg);color:var(--fg);
font:16px/1.6 system-ui,-apple-system,"Segoe UI",Roboto,"Noto Sans Arabic",Tahoma,sans-serif}
main{max-width:460px;width:100%;background:var(--card);border:1px solid var(--line);border-radius:18px;padding:32px 28px;text-align:center}
.code{display:inline-block;font-size:13px;font-weight:600;color:var(--muted);border:1px solid var(--line);border-radius:999px;padding:2px 10px;margin-bottom:12px}
h1{font-size:24px;line-height:1.25;margin:0 0 10px;letter-spacing:-.01em}p{margin:0 0 10px;color:var(--muted)}
a.btn{display:inline-flex;align-items:center;justify-content:center;min-height:46px;margin-top:12px;padding:10px 20px;border-radius:10px;background:var(--accent);color:var(--on);font-weight:600;text-decoration:none}
a{color:var(--fg)}:focus-visible{outline:3px solid var(--accent);outline-offset:2px}
</style></head><body><main>{{if .Code}}<span class="code">{{.Code}}</span>{{end}}<h1>{{.Heading}}</h1>{{.Body}}{{with .Link}}<a class="btn" href="{{.}}">{{$.LinkText}}</a>{{end}}</main></body></html>`))

func sysPage(d sysPageData) []byte {
	var buf bytes.Buffer
	if sysPages.Execute(&buf, d) != nil {
		return []byte(d.Heading)
	}
	return buf.Bytes()
}

func writeSysPage(w http.ResponseWriter, status int, d sysPageData) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex")
	w.WriteHeader(status)
	w.Write(sysPage(d))
}

// wantsHTML reports whether a request comes from a browser loading a page,
// rather than a script or another program that wants the plain answer.
func wantsHTML(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html")
}

// browserPage turns the platform's plain "not found" and "something went
// wrong" answers into a page for people; programs keep the plain text.
func browserPage(r *http.Request, out result, home string) result {
	if !wantsHTML(r) || !strings.HasPrefix(out.contentType, "text/plain") {
		return out
	}
	switch out.status {
	case http.StatusNotFound:
		out.body = sysPage(sysPageData{Title: "Page not found", Code: 404, Heading: "This page does not exist",
			Body: "<p>The address may be mistyped, or the page was moved.</p>", Link: home, LinkText: "Go to the home page"})
	case http.StatusInternalServerError:
		out.body = sysPage(sysPageData{Title: "Something went wrong", Code: 500, Heading: "Something went wrong",
			Body: "<p>The page could not be shown. The error has been logged; please try again in a moment.</p>", Link: home, LinkText: "Go to the home page"})
	default:
		return out
	}
	out.contentType = "text/html; charset=utf-8"
	return out
}
