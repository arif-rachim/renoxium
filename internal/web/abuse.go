package web

import (
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/email"
)

func (s *Site) registerAbuse() {
	s.mux.HandleFunc("GET /abuse", s.abuseForm)
	s.mux.HandleFunc("POST /abuse", s.abuseReport)
}

func (s *Site) abuseForm(w http.ResponseWriter, r *http.Request) {
	render(w, http.StatusOK, "abuse", map[string]any{"CSRF": s.csrfToken(w, r), "Address": r.URL.Query().Get("app")})
}

// abuseReport sends a report about an app to the operator. Reports are
// logged as well, so none is lost when email is not set up.
func (s *Site) abuseReport(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if !s.mailLimiter.allow("abuse:" + s.clientIP(r)) {
		http.Error(w, "too many reports; try again later", http.StatusTooManyRequests)
		return
	}
	clip := func(v string, n int) string {
		v = strings.TrimSpace(v)
		if utf8.RuneCountInString(v) > n {
			v = string([]rune(v)[:n])
		}
		return v
	}
	address, details, contact := clip(r.PostFormValue("address"), 300), clip(r.PostFormValue("details"), 4000), clip(r.PostFormValue("contact"), 254)
	if address == "" || details == "" {
		render(w, http.StatusBadRequest, "abuse", map[string]any{"CSRF": s.csrfToken(w, r), "Address": address, "Details": details,
			"Contact": contact, "Error": "Tell us which app and what is wrong."})
		return
	}
	slog.Warn("abuse report", "address", address, "contact", contact, "details", details)
	if s.AbuseEmail != "" {
		m := email.Message{To: s.AbuseEmail, Tag: "abuse_report", Subject: "Abuse report: " + clip(address, 80),
			Text: "App: " + address + "\nReporter: " + contact + "\n\n" + details +
				"\n\nTo take the app offline: renoxium -data <dir> suspend <app> \"reason\"\n"}
		if addr, err := emailAddress(contact); err == nil {
			m.ReplyTo = addr
		}
		s.send(r.Context(), m)
	}
	render(w, http.StatusOK, "notice", map[string]any{"Title": "Thank you", "Message": "We received your report and will look at it."})
}

func emailAddress(s string) (string, error) { return accounts.NormalizeEmail(s) }
