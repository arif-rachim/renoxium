package accounts

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/password"
)

// Owners can sign up and sign in with a one-time code sent to their WhatsApp
// number. Such an account has a phone and may have no email or password
// until the owner adds them on the Account page.

const (
	PhoneCodeTTL      = 10 * time.Minute
	phoneCodeAttempts = 5
)

// Purposes of WhatsApp codes.
const (
	PhoneSignIn  = "signin"  // sign in, creating the account on first use
	PhoneLink    = "link"    // add a number to a signed-in account
	PhoneConfirm = "confirm" // confirm a sensitive action instead of a password
)

var (
	ErrPhoneTaken = errors.New("this WhatsApp number already belongs to another Renoxium account")
	ErrCodeWrong  = errors.New("that code is not right")
	ErrCodeGone   = errors.New("this code has expired; ask for a new one")
	ErrHasEmail   = errors.New("this account already has an email address")
	ErrLastSignIn = errors.New("add an email and password first, so you can still sign in")
)

// Label is how the account is shown: its email, or its WhatsApp number
// when it has no email.
func (u User) Label() string {
	if u.Email != "" {
		return u.Email
	}
	return u.Phone
}

// ensurePhone lets email be empty and adds the phone columns and codes.
func (s *Store) ensurePhone() error {
	var ddl string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'users'`).Scan(&ddl); err != nil {
		return err
	}
	if strings.Contains(ddl, "email TEXT NOT NULL UNIQUE") {
		if err := s.relaxEmail(ddl); err != nil {
			return fmt.Errorf("make email optional: %w", err)
		}
	}
	for _, col := range []string{"phone TEXT", "phone_verified_at TEXT"} {
		if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN ` + col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS users_phone ON users(phone)`,
		`CREATE TABLE IF NOT EXISTS phone_codes (phone TEXT NOT NULL, purpose TEXT NOT NULL, user_id TEXT NOT NULL DEFAULT '',
			code_hash TEXT NOT NULL, expires_at INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (phone, purpose))`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// relaxEmail rebuilds the users table of an older database, whose email
// column is NOT NULL, the way SQLite documents for changing a column.
func (s *Store) relaxEmail(ddl string) error {
	open := strings.Index(ddl, "(")
	if open < 0 {
		return errors.New("unexpected users table definition")
	}
	newDDL := "CREATE TABLE users_new " + strings.Replace(ddl[open:], "email TEXT NOT NULL UNIQUE", "email TEXT UNIQUE", 1)
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{newDDL, `INSERT INTO users_new SELECT * FROM users`, `DROP TABLE users`, `ALTER TABLE users_new RENAME TO users`} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	broken := rows.Next()
	rows.Close()
	if broken {
		return errors.New("foreign keys broken by the rebuild")
	}
	return tx.Commit()
}

func sixDigits() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func phoneCodeHash(phone, purpose, code string) string {
	return hash(phone + ":" + purpose + ":" + code)
}

// NewPhoneCode stores a new six-digit code for phone (E.164) and returns
// it. An earlier code of the same purpose stops working. userID is the
// signed-in account the code is for, if any.
func (s *Store) NewPhoneCode(phone, purpose, userID string) (string, error) {
	code := sixDigits()
	_, err := s.db.Exec(`INSERT INTO phone_codes (phone, purpose, user_id, code_hash, expires_at, attempts) VALUES (?, ?, ?, ?, ?, 0)
		ON CONFLICT (phone, purpose) DO UPDATE SET user_id = excluded.user_id, code_hash = excluded.code_hash, expires_at = excluded.expires_at, attempts = 0`,
		phone, purpose, userID, phoneCodeHash(phone, purpose, code), s.now().Add(PhoneCodeTTL).Unix())
	return code, err
}

// UsePhoneCode checks a code. A right code works once; a code allows five
// guesses. It returns the account the code was issued for.
func (s *Store) UsePhoneCode(phone, purpose, code string) (userID string, err error) {
	code = strings.TrimSpace(code)
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Count the guess first, in one statement, so parallel requests cannot
	// test more codes than the limit allows.
	res, err := tx.Exec(`UPDATE phone_codes SET attempts = attempts + 1 WHERE phone = ? AND purpose = ? AND attempts < ? AND expires_at >= ?`,
		phone, purpose, phoneCodeAttempts, s.now().Unix())
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", ErrCodeGone
	}
	var want string
	if err := tx.QueryRow(`SELECT code_hash, user_id FROM phone_codes WHERE phone = ? AND purpose = ?`, phone, purpose).Scan(&want, &userID); err != nil {
		return "", err
	}
	if subtle.ConstantTimeCompare([]byte(phoneCodeHash(phone, purpose, code)), []byte(want)) != 1 {
		// The counted guess must stay counted.
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return "", ErrCodeWrong
	}
	if _, err := tx.Exec(`DELETE FROM phone_codes WHERE phone = ? AND purpose = ?`, phone, purpose); err != nil {
		return "", err
	}
	return userID, tx.Commit()
}

// UserByPhone looks up an account by its WhatsApp number (E.164).
func (s *Store) UserByPhone(phone string) (User, error) {
	var id string
	if err := s.db.QueryRow(`SELECT id FROM users WHERE phone = ?`, phone).Scan(&id); err != nil {
		return User{}, ErrNotFound
	}
	return s.User(id)
}

// PhoneUser returns the account of a WhatsApp number whose code was just
// checked, creating one without email or password on first use.
func (s *Store) PhoneUser(phone string) (u User, created bool, err error) {
	if u, err := s.UserByPhone(phone); err == nil {
		return u, false, nil
	}
	id := randomToken("usr_")[:24]
	now := s.now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(`INSERT INTO users (id, email, password_hash, created_at, phone, phone_verified_at) VALUES (?, NULL, '', ?, ?, ?)`, id, now, phone, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") { // signed up a moment ago in another request
			u, err := s.UserByPhone(phone)
			return u, false, err
		}
		return User{}, false, err
	}
	u, err = s.User(id)
	return u, true, err
}

// LinkPhone adds a confirmed WhatsApp number to an account, replacing its
// earlier one.
func (s *Store) LinkPhone(userID, phone string) error {
	_, err := s.db.Exec(`UPDATE users SET phone = ?, phone_verified_at = ? WHERE id = ?`, phone, s.now().UTC().Format(time.RFC3339), userID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrPhoneTaken
	}
	return err
}

// UnlinkPhone removes the WhatsApp number of an account that can still sign
// in with its email and password.
func (s *Store) UnlinkPhone(userID string) error {
	u, err := s.User(userID)
	if err != nil {
		return err
	}
	if u.Email == "" || !u.HasPassword {
		return ErrLastSignIn
	}
	_, err = s.db.Exec(`UPDATE users SET phone = NULL, phone_verified_at = NULL WHERE id = ?`, userID)
	return err
}

// AddEmail gives an account that has none an email and password, so it
// can also sign in with them. The email still needs confirming.
func (s *Store) AddEmail(userID, email, pw string) (User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	if err := password.Validate(pw); err != nil {
		return User{}, err
	}
	res, err := s.db.Exec(`UPDATE users SET email = ?, password_hash = ?, verified_at = NULL WHERE id = ? AND email IS NULL`, email, password.Hash(pw), userID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return User{}, ErrHasEmail
	}
	return s.User(userID)
}

// cleanupPhoneCodes is part of Cleanup.
func cleanupPhoneCodes(db *sql.DB, now int64) {
	db.Exec(`DELETE FROM phone_codes WHERE expires_at < ?`, now)
}
