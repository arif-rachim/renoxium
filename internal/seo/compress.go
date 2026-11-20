package seo

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// minCompress is the smallest body worth compressing, when its size is known.
const minCompress = 1024

var gzipPool = sync.Pool{New: func() any {
	zw, _ := gzip.NewWriterLevel(io.Discard, 5)
	return zw
}}

// compressible lists the content types that shrink: text, not images.
var compressible = map[string]bool{
	"text/html": true, "text/css": true, "text/plain": true, "text/xml": true, "text/markdown": true,
	"text/javascript": true, "application/javascript": true, "application/json": true,
	"application/xml": true, "application/manifest+json": true, "application/ld+json": true,
	"image/svg+xml": true,
}

// Compress gzips text responses for clients that accept it. Streams
// (text/event-stream), partial content and small bodies pass through.
func Compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || !acceptsGzip(r) || r.Header.Get("Range") != "" {
			next.ServeHTTP(w, r)
			return
		}
		cw := &compressWriter{ResponseWriter: w}
		defer cw.close()
		next.ServeHTTP(cw, r)
	})
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, q, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			q = strings.TrimSpace(q)
			return q != "q=0" && q != "q=0.0"
		}
	}
	return false
}

// compressWriter holds a response back until it has minCompress bytes (or
// the handler flushes or finishes), so short answers are sent as they are.
type compressWriter struct {
	http.ResponseWriter
	zw      *gzip.Writer
	status  int
	held    []byte
	started bool
}

func (c *compressWriter) WriteHeader(status int) {
	if !c.started && c.status == 0 {
		c.status = status
	}
}

// start sends the header and what was held back, compressed when worth it.
func (c *compressWriter) start() {
	if c.started {
		return
	}
	c.started = true
	if c.status == 0 {
		if len(c.held) == 0 {
			return // nothing was written: net/http answers 200 by itself
		}
		c.status = http.StatusOK
	}
	h := c.Header()
	h.Add("Vary", "Accept-Encoding")
	if h.Get("Content-Type") == "" && len(c.held) > 0 {
		h.Set("Content-Type", http.DetectContentType(c.held))
	}
	size := len(c.held)
	if n, err := strconv.Atoi(h.Get("Content-Length")); err == nil {
		size = n
	}
	ct, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
	if c.status >= 200 && c.status != http.StatusNoContent && c.status != http.StatusPartialContent && c.status != http.StatusNotModified &&
		h.Get("Content-Encoding") == "" && h.Get("Content-Range") == "" && compressible[ct] && size >= minCompress {
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		c.zw = gzipPool.Get().(*gzip.Writer)
		c.zw.Reset(c.ResponseWriter)
	}
	c.ResponseWriter.WriteHeader(c.status)
	if len(c.held) > 0 {
		c.write(c.held)
		c.held = nil
	}
}

func (c *compressWriter) write(p []byte) (int, error) {
	if c.zw != nil {
		return c.zw.Write(p)
	}
	return c.ResponseWriter.Write(p)
}

func (c *compressWriter) Write(p []byte) (int, error) {
	if c.started {
		return c.write(p)
	}
	c.held = append(c.held, p...)
	if len(c.held) >= minCompress || c.Header().Get("Content-Length") != "" {
		c.start()
	}
	return len(p), nil
}

// Flush sends what has been written so far, for handlers that stream.
func (c *compressWriter) Flush() {
	c.start()
	if c.zw != nil {
		c.zw.Flush()
	}
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the connection.
func (c *compressWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (c *compressWriter) close() {
	c.start()
	if c.zw != nil {
		c.zw.Close()
		c.zw.Reset(io.Discard)
		gzipPool.Put(c.zw)
		c.zw = nil
	}
}
