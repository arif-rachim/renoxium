package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/email"
)

func (s *Site) registerEmailFlows() {
	s.mux.HandleFunc("GET /forgot", s.forgotForm)
	s.mux.HandleFunc("POST /forgot", s.forgot)
	s.mux.HandleFunc("GET /reset", s.resetForm)
	s.mux.HandleFunc("POST /reset", s.reset)
	s.mux.HandleFunc("GET /verify", s.verifyEmail)
	s.mux.HandleFunc("POST /verify/resend", s.resendVerification)
}

func (s *Site) send(ctx context.Context, m email.Message) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := s.Mailer.Send(ctx, m); err != nil {
		slog.Error("email failed", "to", m.To, "kind", m.Tag, "err", err)
	}
}

func (s *Site) sendVerification(ctx context.Context, u accounts.User) {
	token, err := s.accounts.CreateEmailToken(u.ID, accounts.PurposeVerify)
	if err != nil {
		slog.Error("verification token", "err", err)
		return
	}
	link := s.base + "/verify?token=" + url.QueryEscape(token)
	s.send(ctx, email.Message{To: u.Email, Tag: "verify_email", Subject: "Confirm your Renoxium email",
		Text: "Welcome to Renoxium.\n\nConfirm your email address by opening this link:\n\n" + link +
			"\n\nThe link works for 7 days. If you did not create a Renoxium account, ignore this email.\n",
		HTML: "<p>Welcome to Renoxium.</p><p><a href=\"" + link + "\">Confirm your email address</a></p>" +
			"<p>The link works for 7 days. If you did not create a Renoxium account, ignore this email.</p>"})
}

func noReferrer(w http.ResponseWriter) { w.Header().Set("Referrer-Policy", "no-referrer") }

func (s *Site) forgotForm(w http.ResponseWriter, r *http.Request) {
	render(w, http.StatusOK, "forgot", map[string]any{"CSRF": s.csrfToken(w, r)})
}

// forgot always gives the same answer, so it does not reveal which emails
// have an account.
func (s *Site) forgot(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	addr, _ := accounts.NormalizeEmail(r.PostFormValue("email"))
	if !s.limiter.allow("forgot:"+s.clientIP(r)) || !s.mailLimiter.allow("forgot:"+addr) {
		http.Error(w, "too many attempts; try again later", http.StatusTooManyRequests)
		return
	}
	// Sent after the answer, so how long the answer takes does not show
	// whether the address has an account.
	ctx := context.WithoutCancel(r.Context())
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		u, err := s.accounts.UserByEmail(addr)
		if err != nil {
			return
		}
		if token, err := s.accounts.CreateEmailToken(u.ID, accounts.PurposeReset); err == nil {
			link := s.base + "/reset?token=" + url.QueryEscape(token)
			s.send(ctx, email.Message{To: u.Email, Tag: "password_reset", Subject: "Reset your Renoxium password",
				Text: "Someone asked to reset the password of your Renoxium account.\n\nChoose a new password here:\n\n" + link +
					"\n\nThe link works for 1 hour and only once. If it was not you, ignore this email; your password stays the same.\n",
				HTML: "<p>Someone asked to reset the password of your Renoxium account.</p><p><a href=\"" + link + "\">Choose a new password</a></p>" +
					"<p>The link works for 1 hour and only once. If it was not you, ignore this email; your password stays the same.</p>"})
		}
	}()
	render(w, http.StatusOK, "notice", map[string]any{"Title": "Check your email",
		"Message": "If an account exists for " + addr + ", we sent it a link to choose a new password. The link works for 1 hour."})
}

func (s *Site) resetForm(w http.ResponseWriter, r *http.Request) {
	noReferrer(w)
	token := r.URL.Query().Get("token")
	u, err := s.accounts.CheckEmailToken(token, accounts.PurposeReset)
	if err != nil {
		render(w, http.StatusBadRequest, "error", map[string]any{"Message": "This link has expired or was already used. Ask for a new one on the sign-in page."})
		return
	}
	render(w, http.StatusOK, "reset", map[string]any{"CSRF": s.csrfToken(w, r), "Token": token, "Email": u.Email})
}

func (s *Site) reset(w http.ResponseWriter, r *http.Request) {
	noReferrer(w)
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; open the link from your email again", http.StatusForbidden)
		return
	}
	token := r.PostFormValue("token")
	u, err := s.accounts.ResetPassword(token, r.PostFormValue("password"))
	switch {
	case errors.Is(err, accounts.ErrNotFound):
		render(w, http.StatusBadRequest, "error", map[string]any{"Message": "This link has expired or was already used. Ask for a new one on the sign-in page."})
	case err != nil:
		email := ""
		if cu, cerr := s.accounts.CheckEmailToken(token, accounts.PurposeReset); cerr == nil {
			email = cu.Email
		}
		render(w, http.StatusBadRequest, "reset", map[string]any{"CSRF": s.csrfToken(w, r), "Token": token, "Email": email, "Error": err.Error()})
	default:
		s.startSession(w, r, u, "/")
	}
}

func (s *Site) verifyEmail(w http.ResponseWriter, r *http.Request) {
	noReferrer(w)
	if _, err := s.accounts.VerifyEmail(r.URL.Query().Get("token")); err != nil {
		render(w, http.StatusBadRequest, "error", map[string]any{"Message": "This confirmation link has expired or was already used. Sign in to get a new one."})
		return
	}
	render(w, http.StatusOK, "notice", map[string]any{"Title": "Email confirmed", "Message": "Thank you. Your email address is confirmed.", "Link": "/"})
}

func (s *Site) resendVerification(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if !s.mailLimiter.allow("verify:" + u.ID) {
		http.Error(w, "too many emails; try again later", http.StatusTooManyRequests)
		return
	}
	if !u.Verified {
		s.sendVerification(r.Context(), u)
	}
	render(w, http.StatusOK, "notice", map[string]any{"Title": "Check your email", "Message": "We sent a confirmation link to " + u.Email + ".", "Link": "/"})
}
