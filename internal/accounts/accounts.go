// Package accounts stores Renoxium users, browser sessions and the OAuth
// clients, codes and tokens agents use to act for a user. Secrets are only
// ever stored as SHA-256 hashes; passwords as argon2id hashes.
package accounts

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"

	"github.com/arif-rachim/renoxium/internal/password"
)

const (
	SessionTTL      = 30 * 24 * time.Hour
	CodeTTL         = 10 * time.Minute
	AccessTokenTTL  = time.Hour
	RefreshTokenTTL = 60 * 24 * time.Hour
	MinPassword     = password.MinLength
	maxRedirectURIs = 10
)

var (
	ErrNotFound       = errors.New("not found")
	ErrBadCredentials = errors.New("wrong email or password")
	ErrEmailTaken     = errors.New("an account with this email already exists")
	ErrInvalidGrant   = errors.New("invalid_grant")
)

// User is a Renoxium account.
type User struct {
	ID       string
	Email    string
	Verified bool
}

// Client is an OAuth client registered dynamically by an agent platform.
type Client struct {
	ID           string
	Name         string
	RedirectURIs []string
}

// Tokens is a token endpoint response.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope,omitempty"`
}

// Store is safe for concurrent use.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens or creates the accounts database.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS sessions (hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), expires_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS clients (id TEXT PRIMARY KEY, name TEXT NOT NULL, redirect_uris TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS codes (hash TEXT PRIMARY KEY, client_id TEXT NOT NULL, user_id TEXT NOT NULL, redirect_uri TEXT NOT NULL,
			challenge TEXT NOT NULL, scope TEXT NOT NULL, expires_at INTEGER NOT NULL, used INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS email_tokens (hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), purpose TEXT NOT NULL,
			expires_at INTEGER NOT NULL, used INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS tokens (access_hash TEXT PRIMARY KEY, refresh_hash TEXT NOT NULL UNIQUE, client_id TEXT NOT NULL,
			user_id TEXT NOT NULL, scope TEXT NOT NULL, access_expires_at INTEGER NOT NULL, refresh_expires_at INTEGER NOT NULL)`,
		// Refresh tokens already rotated away. Seeing one again means it was
		// copied: the whole grant is revoked.
		`CREATE TABLE IF NOT EXISTS used_refresh (hash TEXT PRIMARY KEY, client_id TEXT NOT NULL, user_id TEXT NOT NULL, expires_at INTEGER NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN verified_at TEXT`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, now: time.Now}
	s.Cleanup()
	if err := s.ensureBilling(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Cleanup deletes expired sessions, codes, links and tokens. Call it
// periodically.
func (s *Store) Cleanup() {
	now := s.now().Unix()
	for _, stmt := range []string{
		`DELETE FROM sessions WHERE expires_at < ?`,
		`DELETE FROM codes WHERE expires_at < ? - 86400`, // kept a day so a replay is still recognised
		`DELETE FROM email_tokens WHERE expires_at < ? OR used = 1`,
		`DELETE FROM tokens WHERE refresh_expires_at < ?`,
		`DELETE FROM used_refresh WHERE expires_at < ?`,
	} {
		s.db.Exec(stmt, now)
	}
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func randomToken(prefix string) string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b[:])
}

func hash(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

// --- users and passwords ---

// NormalizeEmail validates and lower-cases an email address.
func NormalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 {
		return "", errors.New("enter a valid email address")
	}
	return email, nil
}

// CreateUser registers a new account.
func (s *Store) CreateUser(email, pw string) (User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	if err := password.Validate(pw); err != nil {
		return User{}, err
	}
	u := User{ID: randomToken("usr_")[:24], Email: email}
	_, err = s.db.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		u.ID, u.Email, password.Hash(pw), s.now().UTC().Format(time.RFC3339))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	return u, nil
}

// Authenticate checks an email and password.
func (s *Store) Authenticate(email, pw string) (User, error) {
	email, _ = NormalizeEmail(email)
	var u User
	var encoded string
	err := s.db.QueryRow(`SELECT id, email, password_hash FROM users WHERE email = ?`, email).Scan(&u.ID, &u.Email, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		password.CheckMissing(pw)
		return User{}, ErrBadCredentials
	}
	if err != nil {
		return User{}, err
	}
	if !password.Check(encoded, pw) {
		return User{}, ErrBadCredentials
	}
	return u, nil
}

// User looks up an account by ID.
func (s *Store) User(id string) (User, error) {
	u := User{ID: id}
	var verified sql.NullString
	err := s.db.QueryRow(`SELECT email, verified_at FROM users WHERE id = ?`, id).Scan(&u.Email, &verified)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.Verified = verified.Valid
	return u, err
}

// Token purposes for emailed links.
const (
	PurposeVerify = "verify"
	PurposeReset  = "reset"
)

// Lifetimes of emailed links.
const (
	VerifyTTL = 7 * 24 * time.Hour
	ResetTTL  = time.Hour
)

// UserByEmail looks up an account by email.
func (s *Store) UserByEmail(email string) (User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return User{}, ErrNotFound
	}
	var id string
	if err := s.db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id); err != nil {
		return User{}, ErrNotFound
	}
	return s.User(id)
}

// CreateEmailToken issues a single-use token for an emailed link. Earlier
// unused tokens of the same purpose stop working.
func (s *Store) CreateEmailToken(userID, purpose string) (string, error) {
	ttl := VerifyTTL
	if purpose == PurposeReset {
		ttl = ResetTTL
	}
	token := randomToken("eml_")
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM email_tokens WHERE user_id = ? AND purpose = ?`, userID, purpose); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO email_tokens (hash, user_id, purpose, expires_at) VALUES (?, ?, ?, ?)`,
		hash(token), userID, purpose, s.now().Add(ttl).Unix()); err != nil {
		return "", err
	}
	return token, tx.Commit()
}

// CheckEmailToken returns the user of a live token without using it.
func (s *Store) CheckEmailToken(token, purpose string) (User, error) {
	var id string
	err := s.db.QueryRow(`SELECT user_id FROM email_tokens WHERE hash = ? AND purpose = ? AND used = 0 AND expires_at > ?`,
		hash(token), purpose, s.now().Unix()).Scan(&id)
	if err != nil {
		return User{}, ErrNotFound
	}
	return s.User(id)
}

func (s *Store) useEmailToken(tx *sql.Tx, token, purpose string) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT user_id FROM email_tokens WHERE hash = ? AND purpose = ? AND used = 0 AND expires_at > ?`,
		hash(token), purpose, s.now().Unix()).Scan(&id)
	if err != nil {
		return "", ErrNotFound
	}
	if _, err := tx.Exec(`UPDATE email_tokens SET used = 1 WHERE hash = ?`, hash(token)); err != nil {
		return "", err
	}
	return id, nil
}

// VerifyEmail marks the email of the token's user as verified.
func (s *Store) VerifyEmail(token string) (User, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	id, err := s.useEmailToken(tx, token, PurposeVerify)
	if err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(`UPDATE users SET verified_at = coalesce(verified_at, ?) WHERE id = ?`, s.now().UTC().Format(time.RFC3339), id); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return s.User(id)
}

// ResetPassword sets a new password with a reset token. It signs the user
// out everywhere: browser sessions and agent tokens are revoked. Receiving the
// reset email also proves the address, so it is marked verified.
func (s *Store) ResetPassword(token, pw string) (User, error) {
	if err := password.Validate(pw); err != nil {
		return User{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	id, err := s.useEmailToken(tx, token, PurposeReset)
	if err != nil {
		return User{}, err
	}
	for _, stmt := range []string{
		`UPDATE users SET password_hash = ?, verified_at = coalesce(verified_at, ?) WHERE id = ?`,
		`DELETE FROM sessions WHERE user_id = ?`,
		`DELETE FROM tokens WHERE user_id = ?`,
	} {
		args := []any{id}
		if strings.HasPrefix(stmt, "UPDATE") {
			args = []any{password.Hash(pw), s.now().UTC().Format(time.RFC3339), id}
		}
		if _, err := tx.Exec(stmt, args...); err != nil {
			return User{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return s.User(id)
}

// --- browser sessions ---

// CreateSession starts a browser session and returns its secret.
func (s *Store) CreateSession(userID string) (string, error) {
	secret := randomToken("ses_")
	_, err := s.db.Exec(`INSERT INTO sessions (hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hash(secret), userID, s.now().Add(SessionTTL).Unix())
	return secret, err
}

// SessionUser returns the user of a live session.
func (s *Store) SessionUser(secret string) (User, error) {
	var id string
	err := s.db.QueryRow(`SELECT user_id FROM sessions WHERE hash = ? AND expires_at > ?`, hash(secret), s.now().Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return s.User(id)
}

// DeleteSession ends a browser session.
func (s *Store) DeleteSession(secret string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE hash = ?`, hash(secret))
	return err
}

// --- OAuth clients ---

// ValidRedirectURI accepts https URLs, and http only for loopback hosts
// (native and local clients). Fragments are not allowed.
func ValidRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.Host == "" || u.User != nil || len(raw) > 2000 {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		h := u.Hostname()
		return h == "localhost" || h == "127.0.0.1" || h == "::1"
	}
	return false
}

// RegisterClient stores a public client (RFC 7591). Only PKCE public clients
// are supported, so no client secret is issued.
func (s *Store) RegisterClient(name string, redirectURIs []string) (Client, error) {
	if len(redirectURIs) == 0 || len(redirectURIs) > maxRedirectURIs {
		return Client{}, fmt.Errorf("register between 1 and %d redirect_uris", maxRedirectURIs)
	}
	for _, r := range redirectURIs {
		if !ValidRedirectURI(r) {
			return Client{}, fmt.Errorf("redirect_uri %q must be https, or http on localhost", r)
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Unnamed agent"
	}
	if utf8.RuneCountInString(name) > 80 {
		name = string([]rune(name)[:80])
	}
	c := Client{ID: randomToken("cli_")[:32], Name: name, RedirectURIs: redirectURIs}
	uris, _ := json.Marshal(redirectURIs)
	_, err := s.db.Exec(`INSERT INTO clients (id, name, redirect_uris, created_at) VALUES (?, ?, ?, ?)`,
		c.ID, c.Name, string(uris), s.now().UTC().Format(time.RFC3339))
	return c, err
}

// Client looks up a registered client.
func (s *Store) Client(id string) (Client, error) {
	c := Client{ID: id}
	var uris string
	err := s.db.QueryRow(`SELECT name, redirect_uris FROM clients WHERE id = ?`, id).Scan(&c.Name, &uris)
	if errors.Is(err, sql.ErrNoRows) {
		return Client{}, ErrNotFound
	}
	if err != nil {
		return Client{}, err
	}
	return c, json.Unmarshal([]byte(uris), &c.RedirectURIs)
}

// AllowsRedirect reports whether uri exactly matches a registered redirect URI.
func (c Client) AllowsRedirect(uri string) bool {
	for _, r := range c.RedirectURIs {
		if r == uri {
			return true
		}
	}
	return false
}

// --- authorization codes and tokens ---

// CreateCode issues a single-use authorization code bound to a PKCE S256 challenge.
func (s *Store) CreateCode(clientID, userID, redirectURI, challenge, scope string) (string, error) {
	code := randomToken("cod_")
	_, err := s.db.Exec(`INSERT INTO codes (hash, client_id, user_id, redirect_uri, challenge, scope, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		hash(code), clientID, userID, redirectURI, challenge, scope, s.now().Add(CodeTTL).Unix())
	return code, err
}

// ExchangeCode redeems an authorization code. All checks fail with ErrInvalidGrant.
func (s *Store) ExchangeCode(code, clientID, redirectURI, verifier string) (Tokens, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Tokens{}, err
	}
	defer tx.Rollback()
	var userID, wantClient, wantRedirect, challenge, scope string
	var expires int64
	var used int
	err = tx.QueryRow(`SELECT client_id, user_id, redirect_uri, challenge, scope, expires_at, used FROM codes WHERE hash = ?`, hash(code)).
		Scan(&wantClient, &userID, &wantRedirect, &challenge, &scope, &expires, &used)
	if err != nil {
		return Tokens{}, ErrInvalidGrant
	}
	if _, err := tx.Exec(`UPDATE codes SET used = 1 WHERE hash = ?`, hash(code)); err != nil {
		return Tokens{}, err
	}
	if used != 0 {
		// A replayed code may be stolen: revoke what it issued.
		tx.Exec(`DELETE FROM tokens WHERE client_id = ? AND user_id = ?`, wantClient, userID)
		tx.Commit()
		return Tokens{}, ErrInvalidGrant
	}
	sum := sha256.Sum256([]byte(verifier))
	if expires < s.now().Unix() || wantClient != clientID || wantRedirect != redirectURI || len(verifier) < 43 || len(verifier) > 128 ||
		subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) != 1 {
		tx.Commit()
		return Tokens{}, ErrInvalidGrant
	}
	t, err := s.issue(tx, clientID, userID, scope)
	if err != nil {
		return Tokens{}, err
	}
	return t, tx.Commit()
}

// Refresh rotates a refresh token.
func (s *Store) Refresh(refreshToken, clientID string) (Tokens, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Tokens{}, err
	}
	defer tx.Rollback()
	var userID, wantClient, scope string
	var expires int64
	err = tx.QueryRow(`SELECT user_id, client_id, scope, refresh_expires_at FROM tokens WHERE refresh_hash = ?`, hash(refreshToken)).
		Scan(&userID, &wantClient, &scope, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		var usedClient, usedUser string
		if tx.QueryRow(`SELECT client_id, user_id FROM used_refresh WHERE hash = ?`, hash(refreshToken)).Scan(&usedClient, &usedUser) == nil {
			tx.Exec(`DELETE FROM tokens WHERE client_id = ? AND user_id = ?`, usedClient, usedUser)
			tx.Commit()
		}
		return Tokens{}, ErrInvalidGrant
	}
	if err != nil || wantClient != clientID || expires < s.now().Unix() {
		return Tokens{}, ErrInvalidGrant
	}
	if _, err := tx.Exec(`DELETE FROM tokens WHERE refresh_hash = ?`, hash(refreshToken)); err != nil {
		return Tokens{}, err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO used_refresh (hash, client_id, user_id, expires_at) VALUES (?, ?, ?, ?)`,
		hash(refreshToken), clientID, userID, expires); err != nil {
		return Tokens{}, err
	}
	t, err := s.issue(tx, clientID, userID, scope)
	if err != nil {
		return Tokens{}, err
	}
	return t, tx.Commit()
}

func (s *Store) issue(tx *sql.Tx, clientID, userID, scope string) (Tokens, error) {
	t := Tokens{AccessToken: randomToken("rxa_"), RefreshToken: randomToken("rxr_"), TokenType: "Bearer",
		ExpiresIn: int(AccessTokenTTL.Seconds()), Scope: scope}
	now := s.now()
	_, err := tx.Exec(`INSERT INTO tokens (access_hash, refresh_hash, client_id, user_id, scope, access_expires_at, refresh_expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		hash(t.AccessToken), hash(t.RefreshToken), clientID, userID, scope, now.Add(AccessTokenTTL).Unix(), now.Add(RefreshTokenTTL).Unix())
	return t, err
}

// AccessToken returns the user and expiry of a live access token.
func (s *Store) AccessToken(token string) (userID string, expires time.Time, err error) {
	var exp int64
	err = s.db.QueryRow(`SELECT user_id, access_expires_at FROM tokens WHERE access_hash = ?`, hash(token)).Scan(&userID, &exp)
	if err != nil || exp < s.now().Unix() {
		return "", time.Time{}, ErrNotFound
	}
	return userID, time.Unix(exp, 0), nil
}
