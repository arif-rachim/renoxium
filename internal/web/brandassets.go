package web

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/arif-rachim/renoxium/brand"
)

// brandDefines are templates every site page can use: the inline mark and
// wordmark for headers, and the icon links for <head>.
var brandDefines = `{{define "brandlogo"}}<svg class="mark" viewBox="` + brand.MarkViewBox + `" aria-hidden="true" focusable="false"><path fill="currentColor" fill-rule="evenodd" d="` +
	brand.MarkPath + `"/></svg><span>renoxium</span>{{end}}` +
	`{{define "icons"}}<link rel="icon" href="/favicon.ico" sizes="32x32"><link rel="icon" href="/favicon.svg" type="image/svg+xml">` +
	`<link rel="apple-touch-icon" href="/apple-touch-icon.png"><link rel="manifest" href="/site.webmanifest"><meta name="theme-color" content="` + brand.Navy + `">{{end}}`

// brandFiles are served at the site root as well as under /brand/.
var brandFiles = map[string]string{
	"/favicon.ico": "favicon.ico", "/favicon.svg": "favicon.svg", "/apple-touch-icon.png": "apple-touch-icon.png",
	"/icon-192.png": "icon-192.png", "/icon-512.png": "icon-512.png", "/icon-maskable-512.png": "icon-maskable-512.png",
	"/og-image.png": "og-image.png",
}

func (s *Site) registerBrand() {
	for route, file := range brandFiles {
		s.mux.HandleFunc("GET "+route, serveBrandFile(file))
	}
	s.mux.HandleFunc("GET /brand/{file}", func(w http.ResponseWriter, r *http.Request) {
		serveBrandFile(r.PathValue("file"))(w, r)
	})
	s.mux.HandleFunc("GET /site.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write([]byte(`{"name":"Renoxium","short_name":"Renoxium","start_url":"/","display":"standalone","background_color":"#ffffff","theme_color":"` + brand.Navy + `",` +
			`"icons":[{"src":"/icon-192.png","sizes":"192x192","type":"image/png"},{"src":"/icon-512.png","sizes":"512x512","type":"image/png"},` +
			`{"src":"/icon-maskable-512.png","sizes":"512x512","type":"image/png","purpose":"maskable"}]}`))
	})
}

func serveBrandFile(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if name == "" || strings.ContainsAny(name, "/\\") {
			http.NotFound(w, r)
			return
		}
		b, err := fs.ReadFile(brand.Files, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ct := mime.TypeByExtension(path.Ext(name))
		if path.Ext(name) == ".ico" {
			ct = "image/x-icon"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(b)
	}
}
