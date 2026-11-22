package web

import (
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

func (s *Site) registerWhatsApp() {
	s.mux.HandleFunc("GET /whatsapp/webhook", s.whatsappVerify)
	s.mux.HandleFunc("POST /whatsapp/webhook", s.whatsappWebhook)
	// The View button of a message to a customer: /w/<message id>.
	s.mux.HandleFunc("GET /w/{id}", s.whatsappLink)
}

// whatsappVerify answers Meta's check when the webhook is set up.
func (s *Site) whatsappVerify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if s.WhatsAppVerifyToken == "" || q.Get("hub.mode") != "subscribe" ||
		subtle.ConstantTimeCompare([]byte(q.Get("hub.verify_token")), []byte(s.WhatsAppVerifyToken)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(q.Get("hub.challenge")))
}

// whatsappWebhook applies Meta's events. Nothing is read before the
// signature is checked against the app secret.
func (s *Site) whatsappWebhook(w http.ResponseWriter, r *http.Request) {
	if s.WhatsAppAppSecret == "" {
		http.Error(w, "webhooks are not configured", http.StatusServiceUnavailable)
		return
	}
	payload, err := readLimited(w, r, 3<<20)
	if err != nil {
		return
	}
	if !whatsapp.VerifySignature(s.WhatsAppAppSecret, payload, r.Header.Get(whatsapp.SignatureHeader)) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	if err := s.platform.HandleWhatsAppWebhook(payload); err != nil {
		// A signed body Meta sent that does not parse: retrying will not help.
		slog.Error("whatsapp webhook", "err", err)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Site) whatsappLink(w http.ResponseWriter, r *http.Request) {
	to, ok := s.platform.WhatsAppLink(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, to, http.StatusFound)
}
