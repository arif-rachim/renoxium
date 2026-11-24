package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// Owners sign up and sign in with a code sent to their WhatsApp number, as
// well as with email and password. The first sign-in with a number creates
// an account without email; the owner can add one later on the Account
// page. Accounts without a password confirm deleting and renaming with a
// WhatsApp code instead.

// DefaultWhatsAppDaily caps the sign-in codes the site sends in a day.
const DefaultWhatsAppDaily = 500

func (s *Site) registerWhatsApp() {
	s.mux.HandleFunc("GET /login/whatsapp", s.waForm)
	s.mux.HandleFunc("POST /login/whatsapp", s.waSend)
	s.mux.HandleFunc("POST /login/whatsapp/verify", s.waVerify)
	s.mux.HandleFunc("POST /account/whatsapp", s.waAccount)
	s.mux.HandleFunc("POST /account/email", s.addEmail)
}

// sendOwnerCode creates a code and sends it. Every code is a paid message,
// so they are limited per visitor address, per number and per day.
func (s *Site) sendOwnerCode(ctx context.Context, r *http.Request, phone, purpose, userID string) error {
	if s.WhatsApp == nil {
		return errors.New("WhatsApp codes are not available on this server")
	}
	if !s.waLimiter.allow("ip:"+s.clientIP(r)) || !s.waNumberLimiter.allow(phone) {
		return errors.New("too many codes requested; try again in 15 minutes")
	}
	if !s.waDailyLimiter.allow("all") {
		slog.Error("daily limit of WhatsApp sign-in codes reached")
		return errors.New("WhatsApp codes are not available right now; try again later")
	}
	code, err := s.accounts.NewPhoneCode(phone, purpose, userID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := s.WhatsApp.SendCode(ctx, phone, code); err != nil {
		// Meta's answer describes the platform's own WhatsApp account: it
		// goes to the server log only.
		slog.Error("whatsapp owner code", "purpose", purpose, "err", err)
		return errors.New("we could not send a WhatsApp code to this number; check it and try again")
	}
	return nil
}

// checkOwnerCode checks a code, limiting guesses per number and address
// across every code sent, so asking for new codes buys no more guesses.
func (s *Site) checkOwnerCode(r *http.Request, phone, purpose, code string) (string, error) {
	if !s.limiter.allow("wa-verify:"+phone) || !s.limiter.allow("wa-verify-ip:"+s.clientIP(r)) {
		return "", errors.New("too many attempts; try again in 15 minutes")
	}
	id, err := s.accounts.UsePhoneCode(phone, purpose, code)
	switch {
	case errors.Is(err, accounts.ErrCodeWrong):
		return "", errors.New("That code is not right. Check the WhatsApp message and try again.")
	case err != nil:
		return "", errors.New("This code has expired. Ask for a new one.")
	}
	return id, nil
}

const phoneHint = "Enter your WhatsApp number with the country code, for example +971 50 123 4567."

func (s *Site) waForm(w http.ResponseWriter, r *http.Request) {
	if s.WhatsApp == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	render(w, http.StatusOK, "wa_phone", map[string]any{"CSRF": s.csrfToken(w, r), "Next": safeNext(r.URL.Query().Get("next"))})
}

func (s *Site) waSend(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.PostFormValue("next"))
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	data := map[string]any{"CSRF": s.csrfToken(w, r), "Next": next, "Phone": r.PostFormValue("phone")}
	phone, ok := whatsapp.Normalize(r.PostFormValue("phone"))
	if !ok {
		data["Error"] = phoneHint
		render(w, http.StatusBadRequest, "wa_phone", data)
		return
	}
	if err := s.sendOwnerCode(r.Context(), r, phone, accounts.PhoneSignIn, ""); err != nil {
		data["Error"] = capitalize(err.Error()) + "."
		render(w, http.StatusTooManyRequests, "wa_phone", data)
		return
	}
	data["Phone"] = phone
	render(w, http.StatusOK, "wa_code", data)
}

func (s *Site) waVerify(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.PostFormValue("next"))
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	phone, ok := whatsapp.Normalize(r.PostFormValue("phone"))
	if !ok {
		http.Redirect(w, r, "/login/whatsapp", http.StatusSeeOther)
		return
	}
	if _, err := s.checkOwnerCode(r, phone, accounts.PhoneSignIn, r.PostFormValue("code")); err != nil {
		render(w, http.StatusUnauthorized, "wa_code", map[string]any{"CSRF": s.csrfToken(w, r), "Next": next, "Phone": phone, "Error": err.Error()})
		return
	}
	u, created, err := s.accounts.PhoneUser(phone)
	if err != nil {
		serverError(w, err)
		return
	}
	if created {
		slog.Info("account created with WhatsApp", "user", u.ID)
	}
	s.startSession(w, r, u, next)
}

// waAccount links, confirms or removes the account's WhatsApp number.
func (s *Site) waAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok || !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	fail := func(status int, msg string) {
		s.renderAccount(w, r, u, status, map[string]any{"PhoneError": msg, "PendingPhone": r.PostFormValue("phone")})
	}
	switch r.PostFormValue("action") {
	case "send":
		phone, ok := whatsapp.Normalize(r.PostFormValue("phone"))
		if !ok {
			fail(http.StatusBadRequest, phoneHint)
			return
		}
		// Whether the number has another account is only said once the code
		// shows that whoever asks holds the number.
		if err := s.sendOwnerCode(r.Context(), r, phone, accounts.PhoneLink, u.ID); err != nil {
			fail(http.StatusTooManyRequests, capitalize(err.Error())+".")
			return
		}
		s.renderAccount(w, r, u, http.StatusOK, map[string]any{"PendingPhone": phone})
	case "confirm":
		phone, _ := whatsapp.Normalize(r.PostFormValue("phone"))
		id, err := s.checkOwnerCode(r, phone, accounts.PhoneLink, r.PostFormValue("code"))
		if err == nil && id != u.ID {
			err = errors.New("This code was sent for another account.")
		}
		if err != nil {
			fail(http.StatusUnauthorized, err.Error())
			return
		}
		if err := s.accounts.LinkPhone(u.ID, phone); err != nil {
			fail(http.StatusConflict, capitalize(err.Error())+".")
			return
		}
		slog.Info("WhatsApp number linked", "user", u.ID)
		http.Redirect(w, r, "/account?phone=1", http.StatusSeeOther)
	case "remove":
		if err := s.accounts.UnlinkPhone(u.ID); err != nil {
			fail(http.StatusBadRequest, capitalize(err.Error())+".")
			return
		}
		http.Redirect(w, r, "/account", http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/account", http.StatusSeeOther)
	}
}

// addEmail gives an account made with WhatsApp an email and password.
func (s *Site) addEmail(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok || !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if !s.limiter.allow("add-email:" + u.ID) {
		http.Error(w, "too many attempts; try again later", http.StatusTooManyRequests)
		return
	}
	u2, err := s.accounts.AddEmail(u.ID, r.PostFormValue("email"), r.PostFormValue("password"))
	if err != nil {
		s.renderAccount(w, r, u, http.StatusBadRequest, map[string]any{"EmailError": capitalize(err.Error()) + ".", "NewEmail": r.PostFormValue("email")})
		return
	}
	if s.mailLimiter.allow("verify:" + u.ID) {
		s.sendVerification(r.Context(), u2)
	}
	http.Redirect(w, r, "/account?email=1", http.StatusSeeOther)
}

// reauthView is what the "confirm it is you" field of a sensitive form
// needs: a password, or for an account without one a WhatsApp code.
type reauthView struct {
	ID          string // the field's id
	HasPassword bool
	Phone       string
	Sent        bool // a code was just sent
}

func (s *Site) reauthView(r *http.Request, u accounts.User, id string) reauthView {
	return reauthView{ID: id, HasPassword: u.HasPassword, Phone: u.Phone,
		Sent: r.Method == http.MethodPost && r.PostFormValue("send_code") != "" && !u.HasPassword}
}

// wantsReauthCode reports whether the form asked for a confirmation code.
func wantsReauthCode(r *http.Request, u accounts.User) bool {
	return !u.HasPassword && r.PostFormValue("send_code") != ""
}

// sendReauthCode sends the confirmation code of an account without password.
func (s *Site) sendReauthCode(r *http.Request, u accounts.User) error {
	if u.Phone == "" {
		return errors.New("this account has no way to confirm it is you; add an email and password first")
	}
	return s.sendOwnerCode(r.Context(), r, u.Phone, accounts.PhoneConfirm, u.ID)
}

// checkReauth checks the password, or the WhatsApp code of an account
// without one. The error is ready to show.
func (s *Site) checkReauth(r *http.Request, u accounts.User) error {
	if u.HasPassword {
		if _, err := s.accounts.Authenticate(u.Email, r.PostFormValue("password")); err != nil {
			return errors.New("Your password is not right.")
		}
		return nil
	}
	if u.Phone == "" {
		return errors.New("This account has no way to confirm it is you; add an email and password first.")
	}
	id, err := s.checkOwnerCode(r, u.Phone, accounts.PhoneConfirm, r.PostFormValue("code"))
	if err == nil && id != u.ID {
		err = errors.New("This code was sent for another account.")
	}
	return err
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
