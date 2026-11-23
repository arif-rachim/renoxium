package web_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/platform"
)

// The builder MCP server is frozen for the ChatGPT app directory review
// (internal/mcpserver/freeze_test.go): agents do not see the clinic-queue-uae
// template, which came after the reviewed version, and the app-limit and
// storage-full errors keep the reviewed wording for agents while the website
// says it its newer way.
func TestAgentsSeeTheReviewedMCPServer(t *testing.T) {
	e := newEnvPlans(t, true)
	c := e.browser()
	e.signup(c, "owner@clinic.ae")
	sess := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)

	list, msg := call(t, sess, "get_example", map[string]any{})
	if msg != "" {
		t.Fatal(msg)
	}
	names := []string{}
	for _, x := range list["examples"].([]any) {
		names = append(names, x.(map[string]any)["name"].(string))
	}
	if got := strings.Join(names, ", "); strings.Contains(got, "clinic-queue-uae") || !strings.Contains(got, "booking-uae") {
		t.Fatalf("get_example lists %s", got)
	}
	_, hidden := call(t, sess, "get_example", map[string]any{"name": "clinic-queue-uae"})
	_, unknown := call(t, sess, "get_example", map[string]any{"name": "no-such-example"})
	if hidden == "" || strings.ReplaceAll(hidden, "clinic-queue-uae", "no-such-example") != unknown {
		t.Fatalf("get_example of the hidden template: %q, of an unknown one: %q", hidden, unknown)
	}
	_, hidden = call(t, sess, "create_app", map[string]any{"app": "shifa", "from_example": "clinic-queue-uae"})
	_, unknown = call(t, sess, "create_app", map[string]any{"app": "shifa", "from_example": "no-such-example"})
	if hidden == "" || strings.ReplaceAll(hidden, "clinic-queue-uae", "no-such-example") != unknown ||
		!strings.Contains(unknown, "choose one of: booking-uae, catalog, delivery-tracking, home-service-uae, listings-uae, quote-portal-uae, reservation-uae, store-uae, tasks") {
		t.Fatalf("create_app from the hidden template: %q, from an unknown one: %q", hidden, unknown)
	}
	if _, err := e.p.Info("shifa"); err == nil {
		t.Fatal("an app was created from the hidden template")
	}
	// The website still offers the template.
	if r, err := c.Get(e.base + "/templates/clinic-queue-uae/use"); err != nil || r.StatusCode != 200 {
		t.Fatalf("use this template: %v %v", err, r)
	}

	// Storage full: the reviewed wording for agents, the new one on the website.
	call(t, sess, "create_app", map[string]any{"app": "app-1"})
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	saved := platform.StorageMBPerPlan["free"]
	platform.StorageMBPerPlan["free"] = 0
	_, full := call(t, sess, "upload_file", map[string]any{"app": "app-1", "data_base64": base64.StdEncoding.EncodeToString(buf.Bytes())})
	_, siteErr := e.p.AddFile("app-1", bytes.NewReader(buf.Bytes()))
	platform.StorageMBPerPlan["free"] = saved
	if !strings.HasPrefix(full, "storage is full: 0 MB of 0 MB used, and this file is 0.0 MB; delete unused images or upgrade at ") || !strings.HasSuffix(full, "/billing") {
		t.Errorf("upload_file when storage is full: %q", full)
	}
	if siteErr == nil || !strings.Contains(siteErr.Error(), "delete unused images, or for more, ") {
		t.Errorf("the website's storage error: %v", siteErr)
	}

	// The app limit: the same, for create_app.
	limit := platform.AppsPerPlan["free"]
	for i := 2; i <= limit; i++ {
		if _, msg := call(t, sess, "create_app", map[string]any{"app": fmt.Sprintf("app-%d", i)}); msg != "" {
			t.Fatal(msg)
		}
	}
	_, over := call(t, sess, "create_app", map[string]any{"app": "one-more"})
	want := fmt.Sprintf("the free plan allows %d app(s) and this account has %d; upgrade the plan at ", limit, limit)
	if !strings.HasPrefix(over, want) || !strings.HasSuffix(over, "/billing to create more") {
		t.Errorf("create_app over the limit: %q", over)
	}
	_, _, err := e.p.CreateFromTemplate(t.Context(), e.p.OwnerOf("app-1"), "one-more", "clinic-queue-uae", "")
	if err == nil || !strings.Contains(err.Error(), "for more, contact Renoxium at") {
		t.Errorf("the website's app-limit error: %v", err)
	}
}
