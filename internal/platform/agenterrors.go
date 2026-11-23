package platform

import "fmt"

// Errors whose wording differs for AI agents. The builder MCP server is
// frozen while the ChatGPT app directory reviews it (see
// internal/mcpserver/freeze_test.go), so its tools must keep answering with
// the wording the reviewers saw (commit d10b7c5), while the website's own
// pages use the newer wording. Error() is the website's text; ForAgent() is
// what create_app and upload_file return. After the review, ForAgent can
// return Error() (and the snapshot be regenerated).

// AppLimitError is returned by CreateApp when the account has as many apps
// as its plan allows.
type AppLimitError struct {
	Plan         string
	Limit, Count int
	site         string
}

func (e *AppLimitError) Error() string {
	return fmt.Sprintf("this account may have %d apps and has %d; for more, contact Renoxium at %s/support", e.Limit, e.Count, e.site)
}

// ForAgent is the wording of the reviewed MCP server.
func (e *AppLimitError) ForAgent() string {
	return fmt.Sprintf("the %s plan allows %d app(s) and this account has %d; upgrade the plan at %s/billing to create more",
		e.Plan, e.Limit, e.Count, e.site)
}

// StorageFullError is returned by AddFile when a new file would pass the
// account's storage quota.
type StorageFullError struct {
	Used, Quota, Size int64
	more, site        string
}

func (e *StorageFullError) Error() string {
	return fmt.Sprintf("storage is full: %d MB of %d MB used, and this file is %.1f MB; delete unused images, or for more, %s",
		e.Used>>20, e.Quota>>20, float64(e.Size)/(1<<20), e.more)
}

// ForAgent is the wording of the reviewed MCP server.
func (e *StorageFullError) ForAgent() string {
	return fmt.Sprintf("storage is full: %d MB of %d MB used, and this file is %.1f MB; delete unused images or upgrade at %s/billing",
		e.Used>>20, e.Quota>>20, float64(e.Size)/(1<<20), e.site)
}
