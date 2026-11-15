package platform

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

const (
	phoneCodeTTL      = 10 * time.Minute
	phoneCodeAttempts = 5
	// phoneGuessesPerHour caps wrong and right guesses for one number.
	phoneGuessesPerHour = 10
)

func rowsAffected(res sql.Result, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func sixDigits() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06d", n.Int64())
}

// whatsappAvailable reports whether phone sign-in can deliver codes on this
// stage. Preview always can: codes go to the app log.
func (p *Platform) whatsappAvailable(st *stage) bool {
	return st.name == Preview || p.cfg.WhatsApp != nil
}

// servePhone handles /_rx/auth/phone (ask for a number, send a code) and
// /_rx/auth/phone/verify (check the code, sign in, creating the account on
// first use).
func (p *Platform) servePhone(w http.ResponseWriter, r *http.Request, st *stage, page string) {
	if !p.whatsappAvailable(st) {
		p.authPage(w, r, st, http.StatusNotFound, "message", authData{Title: "Not available", Message: "Sign-in with WhatsApp is not available here."})
		return
	}
	switch {
	case page == "phone" && r.Method == http.MethodGet:
		p.authPage(w, r, st, http.StatusOK, "phone", authData{Next: localPath(r.URL.Query().Get("next"))})
	case page == "phone" && r.Method == http.MethodPost:
		p.sendPhoneCode(w, r, st)
	case page == "phone/verify" && r.Method == http.MethodPost:
		p.verifyPhoneCode(w, r, st)
	default:
		http.NotFound(w, r)
	}
}

func (p *Platform) sendPhoneCode(w http.ResponseWriter, r *http.Request, st *stage) {
	next := localPath(r.PostFormValue("next"))
	phone, ok := whatsapp.Normalize(r.PostFormValue("phone"))
	data := authData{Next: next, Phone: r.PostFormValue("phone")}
	if !ok {
		data.Error = "Enter your WhatsApp number with the country code, for example +971 50 123 4567."
		p.authPage(w, r, st, http.StatusBadRequest, "phone", data)
		return
	}
	// Every code costs a WhatsApp message: limit per visitor address and per number.
	if !p.allow("phone-ip:"+st.app+":"+p.clientIP(r), 10, time.Hour) || !p.allow("phone:"+st.app+":"+phone, 3, 15*time.Minute) {
		data.Error = "Too many codes requested. Try again in 15 minutes."
		p.authPage(w, r, st, http.StatusTooManyRequests, "phone", data)
		return
	}
	code := sixDigits()
	_, err := st.admin.ExecContext(r.Context(), `INSERT INTO rx_phone_codes (phone, code_hash, expires_at, attempts) VALUES (?, ?, ?, 0)
		ON CONFLICT (phone) DO UPDATE SET code_hash = excluded.code_hash, expires_at = excluded.expires_at, attempts = 0`,
		phone, sha(phone+":"+code), time.Now().Add(phoneCodeTTL).Unix())
	if err != nil {
		http.Error(w, "could not create a code", http.StatusInternalServerError)
		return
	}
	if err := p.sendCode(r.Context(), st, phone, code); err != nil {
		data.Error = "We could not send a WhatsApp message to this number. Check it and try again."
		p.authPage(w, r, st, http.StatusBadGateway, "phone", data)
		return
	}
	data.Phone = phone
	p.authPage(w, r, st, http.StatusOK, "phone_code", data)
}

// sendCode delivers from live; on preview the code goes to the app log.
func (p *Platform) sendCode(ctx context.Context, st *stage, phone, code string) error {
	if st.name == Preview {
		p.event(st, "whatsapp", "info", "", fmt.Sprintf("not sent from preview: sign-in code %s for %s", code, phone))
		return nil
	}
	if !p.allow("wa-day:"+st.app, whatsappPerAppPerDay, 24*time.Hour) || !p.allow("wa-day", whatsappPerDay, 24*time.Hour) {
		p.event(st, "whatsapp", "error", "", "sign-in code not sent: the daily WhatsApp limit was reached")
		return errors.New("daily WhatsApp limit reached")
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := p.cfg.WhatsApp.SendCode(ctx, phone, code); err != nil {
		p.event(st, "whatsapp", "error", "", "sign-in code not delivered: "+err.Error())
		return err
	}
	return nil
}

func (p *Platform) verifyPhoneCode(w http.ResponseWriter, r *http.Request, st *stage) {
	next := localPath(r.PostFormValue("next"))
	phone, _ := whatsapp.Normalize(r.PostFormValue("phone"))
	code := strings.TrimSpace(r.PostFormValue("code"))
	data := authData{Next: next, Phone: phone}
	ctx := r.Context()

	// Guesses are limited per number across every code sent to it, and per
	// visitor address, so asking for new codes does not buy more guesses.
	if !p.allow("phone-verify:"+st.app+":"+phone, phoneGuessesPerHour, time.Hour) || !p.allow("phone-verify-ip:"+st.app+":"+p.clientIP(r), 30, time.Hour) {
		data.Error = "Too many attempts. Try again in an hour."
		p.authPage(w, r, st, http.StatusTooManyRequests, "phone", data)
		return
	}
	// Count the attempt before comparing, in one statement, so parallel
	// requests cannot test more codes than the limit allows.
	res, err := st.admin.ExecContext(ctx, `UPDATE rx_phone_codes SET attempts = attempts + 1 WHERE phone = ? AND attempts < ? AND expires_at >= ?`,
		phone, phoneCodeAttempts, time.Now().Unix())
	var want string
	if n, _ := rowsAffected(res, err); n == 1 {
		err = st.admin.QueryRowContext(ctx, `SELECT code_hash FROM rx_phone_codes WHERE phone = ?`, phone).Scan(&want)
	} else {
		err = errors.New("no live code")
	}
	if err != nil {
		data.Error = "This code has expired. Ask for a new one."
		p.authPage(w, r, st, http.StatusBadRequest, "phone", data)
		return
	}
	if subtle.ConstantTimeCompare([]byte(sha(phone+":"+code)), []byte(want)) != 1 {
		data.Error = "That code is not right. Check the WhatsApp message and try again."
		p.authPage(w, r, st, http.StatusUnauthorized, "phone_code", data)
		return
	}
	// A code works once.
	res, err = st.admin.ExecContext(ctx, `DELETE FROM rx_phone_codes WHERE phone = ? AND code_hash = ?`, phone, want)
	if n, err := rowsAffected(res, err); err != nil || n != 1 {
		data.Error = "This code has expired. Ask for a new one."
		p.authPage(w, r, st, http.StatusBadRequest, "phone", data)
		return
	}

	var id int64
	if err := st.admin.QueryRowContext(ctx, `SELECT id FROM rx_users WHERE phone = ?`, phone).Scan(&id); err != nil {
		name := strings.TrimSpace(r.PostFormValue("name"))
		if utf8.RuneCountInString(name) > 80 {
			name = string([]rune(name)[:80])
		}
		res, err := st.admin.ExecContext(ctx, `INSERT INTO rx_users (phone, name, verified_at) VALUES (?, ?, datetime('now'))`, phone, name)
		if err != nil {
			http.Error(w, "could not create the account", http.StatusInternalServerError)
			return
		}
		id, _ = res.LastInsertId()
		p.event(st, "auth", "info", "", fmt.Sprintf("visitor #%d signed up with WhatsApp", id))
	}
	if err := p.startVisitorSession(ctx, w, st, id); err != nil {
		http.Error(w, "could not sign you in", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}
