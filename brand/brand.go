// Package brand holds the Renoxium logo and the icons made from it. The
// site serves them (favicon, app icons, link previews) and its pages draw
// the mark inline from MarkPath. See README.md for which file to use where.
package brand

import (
	"embed"
	"regexp"
)

//go:embed *.svg *.png favicon.ico
var Files embed.FS

// MarkViewBox and MarkPath draw the Renoxium mark (the R whose counter is
// a speech bubble) with fill-rule evenodd, taken from renoxium-mark.svg.
var MarkViewBox, MarkPath = func() (string, string) {
	b, err := Files.ReadFile("renoxium-mark.svg")
	if err != nil {
		panic(err)
	}
	vb := regexp.MustCompile(`viewBox="([^"]+)"`).FindSubmatch(b)
	d := regexp.MustCompile(` d="([^"]+)"`).FindSubmatch(b)
	if vb == nil || d == nil {
		panic("brand: renoxium-mark.svg has no viewBox or path")
	}
	return string(vb[1]), string(d[1])
}()

// Navy is the colour of the mark.
const Navy = "#1B2F5B"
