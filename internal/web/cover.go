package web

import (
	"bytes"
	"context"
	"image/jpeg"
	"image/png"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/arif-rachim/renoxium/internal/platform"
)

// An app's cover is a picture of its preview's home page at desktop size,
// shown on its card on Your apps. It is rendered by the same headless
// browser as the agents' screenshots, once per preview version, and kept
// in memory; without that browser the cards show the app's initial.

type appCover struct {
	version int
	jpeg    []byte
	failed  time.Time // when rendering last failed for this version
}

type coverCache struct {
	mu   sync.Mutex
	m    map[string]*appCover
	busy map[string]chan struct{} // closed when the app's cover is rendered
}

const (
	coverRetryAfter = 5 * time.Minute
	coverMaxApps    = 500
)

// get returns the cover of an app's preview version, rendering it first if
// it is not kept yet. Requests for the same app wait for one rendering.
func (c *coverCache) get(ctx context.Context, p *platform.Platform, app string, version int) ([]byte, bool) {
	for {
		c.mu.Lock()
		if c.m == nil {
			c.m, c.busy = map[string]*appCover{}, map[string]chan struct{}{}
		}
		if e := c.m[app]; e != nil && e.version == version {
			if e.jpeg != nil {
				c.mu.Unlock()
				return e.jpeg, true
			}
			if time.Since(e.failed) < coverRetryAfter {
				c.mu.Unlock()
				return nil, false
			}
		}
		wait, running := c.busy[app]
		if !running {
			break
		}
		c.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, false
		}
	}
	done := make(chan struct{})
	c.busy[app] = done
	c.mu.Unlock()

	// The picture is kept even if this visitor leaves before it is ready.
	shotCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	e := &appCover{version: version}
	res, err := p.ScreenshotPreview(shotCtx, app, platform.ScreenshotInput{Path: "/", Width: 1280, Height: 800})
	if err == nil {
		var img, out = bytes.NewReader(res.PNG), bytes.Buffer{}
		if pic, derr := png.Decode(img); derr != nil {
			err = derr
		} else if err = jpeg.Encode(&out, pic, &jpeg.Options{Quality: 80}); err == nil {
			e.jpeg = out.Bytes()
		}
	}
	if err != nil {
		e.failed = time.Now()
		slog.Info("app cover not rendered", "app", app, "err", err)
	}
	c.mu.Lock()
	if len(c.m) >= coverMaxApps {
		c.m = map[string]*appCover{}
	}
	c.m[app] = e
	delete(c.busy, app)
	c.mu.Unlock()
	close(done)
	return e.jpeg, e.jpeg != nil
}

// appCoverImage serves the cover of an app to its owner and staff.
func (s *Site) appCoverImage(w http.ResponseWriter, r *http.Request) {
	_, app, ok := s.member(w, r)
	if !ok {
		return
	}
	info, err := s.platform.Info(app)
	if err != nil || info.PreviewVersion == 0 || !s.platform.ScreenshotsOn() {
		http.NotFound(w, r)
		return
	}
	pic, ok := s.covers.get(r.Context(), s.platform, app, info.PreviewVersion)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400") // the address carries the version
	w.Write(pic)
}
