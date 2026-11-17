package platform

import (
	"context"
	"sync"
)

// ErrorCount is how many server errors an app's live stage logged since the
// last TakeErrors, with the newest message.
type ErrorCount struct {
	N    int
	Last string
}

type errorCounter struct {
	mu sync.Mutex
	m  map[string]ErrorCount
}

func (c *errorCounter) note(app, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]ErrorCount{}
	}
	e := c.m[app]
	e.N++
	e.Last = msg
	c.m[app] = e
}

// TakeErrors returns the live server errors counted per app and starts
// counting again. Browser errors are not counted: visitors' browsers and
// extensions cause many that the app cannot fix.
func (p *Platform) TakeErrors() map[string]ErrorCount {
	p.errors.mu.Lock()
	defer p.errors.mu.Unlock()
	out := p.errors.m
	p.errors.m = nil
	return out
}

// MailOwner emails an app's owner from Renoxium about the live app.
func (p *Platform) MailOwner(app, subject, text string) {
	p.mailOwner(context.Background(), &stage{app: app, name: Live}, subject, text)
}

// OwnerOf returns the account that owns an app, or "".
func (p *Platform) OwnerOf(app string) string { return p.ownerOf(app) }
