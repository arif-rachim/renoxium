package web

import (
	"context"
	"fmt"
	"net/url"

	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// appCard is what the dashboard shows about one app: whether it is live,
// whether changes wait for publishing, and a few figures from live.
type appCard struct {
	platform.AppInfo
	Status, StatusText string // "live", "wait" or "draft"
	Host               string // the live address without the scheme
	Sales              string // card sales of the last 30 days, "" without payments
	SalesCount         int
	Visitors           int
	Errors             int // recent live errors
}

func appStatus(info platform.AppInfo) (status, text string) {
	switch {
	case info.LiveVersion == 0:
		return "draft", "Not published yet"
	case info.PreviewVersion > info.LiveVersion:
		return "wait", fmt.Sprintf("Live · version %d waiting", info.PreviewVersion)
	default:
		return "live", "Live"
	}
}

func (s *Site) appCard(ctx context.Context, info platform.AppInfo) appCard {
	c := appCard{AppInfo: info}
	c.Status, c.StatusText = appStatus(info)
	if u, err := url.Parse(info.LiveURL); err == nil {
		c.Host = u.Host
	}
	if info.LiveVersion == 0 {
		return c
	}
	if cur, periods, err := s.platform.Sales(ctx, info.App); err == nil && len(periods) > 0 {
		last := periods[len(periods)-1]
		c.Sales, c.SalesCount = fmt.Sprintf("%s %.2f", cur, last.Amount), last.Count
	}
	_, c.Visitors, _ = s.platform.Visitors(ctx, info.App, "", 1)
	if errs, err := s.platform.Logs(info.App, applog.Filter{Stage: platform.Live, Level: "error", Limit: 20}); err == nil {
		c.Errors = len(errs)
	}
	return c
}
