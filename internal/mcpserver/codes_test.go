package mcpserver

import (
	"testing"

	"github.com/arif-rachim/renoxium/internal/applog"
)

// get_logs {kind: "codes"} finds the preview's sign-in codes among the
// requests, newest first, so the assistant gives the tester their code in
// one call.
func TestPreviewCodes(t *testing.T) {
	events := []applog.Event{
		{Message: "GET /my-route 200"},
		{Message: "not sent from preview: sign-in code 482913 for +971501234567"},
		{Message: "POST /_rx/auth/phone 200"},
		{Message: "not sent from preview: to sara@example.com, \"Your sign-in link\""},
		{Message: "not sent from preview: sign-in code 111111 for +971501234567"},
	}
	got := previewCodes(events, 0)
	if len(got) != 3 || got[0].Message != events[1].Message {
		t.Fatalf("codes: %+v", got)
	}
	if got := previewCodes(events, 1); len(got) != 1 {
		t.Fatalf("limit: %+v", got)
	}
}
