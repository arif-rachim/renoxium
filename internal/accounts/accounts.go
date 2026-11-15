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

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

const (
	SessionTTL      = 30 * 24 * time.Hour
	CodeTTL         = 10 * time.Minute
	AccessTokenTTL  = time.Hour
	RefreshTokenTTL = 60 * 24 * time.Hour
	MinPassword     = 10
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
	ID    string
	Email string
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
		`CREATE TABLE IF NOT EXISTS tokens (access_hash TEXT PRIMARY KEY, refresh_hash TEXT NOT NULL UNIQUE, client_id TEXT NOT NULL,
			user_id TEXT NOT NULL, scope TEXT NOT NULL, access_expires_at INTEGER NOT NULL, refresh_expires_at INTEGER NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Store{db: db, now: time.Now}, nil
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

const (
	argonTime    = 2
	argonMemory  = 19 * 1024 // KiB
	argonThreads = 1
	argonKeyLen  = 32
)

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$%d$%d$%d$%s$%s", argonTime, argonMemory, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

func checkPassword(encoded, pw string) bool {
	var t, m uint32
	var p uint8
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "argon2id" {
		return false
	}
	if _, err := fmt.Sscanf(parts[1]+" "+parts[2]+" "+parts[3], "%d %d %d", &t, &m, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash keeps login timing the same for unknown emails.
var dummyHash = hashPassword("renoxium-dummy-password")

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
func (s *Store) CreateUser(email, password string) (User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	if utf8.RuneCountInString(password) < MinPassword || len(password) > 512 {
		return User{}, fmt.Errorf("the password must be at least %d characters", MinPassword)
	}
	u := User{ID: randomToken("usr_")[:24], Email: email}
	_, err = s.db.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		u.ID, u.Email, hashPassword(password), s.now().UTC().Format(time.RFC3339))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	return u, nil
}

// Authenticate checks an email and password.
func (s *Store) Authenticate(email, password string) (User, error) {
	email, _ = NormalizeEmail(email)
	var u User
	var encoded string
	err := s.db.QueryRow(`SELECT id, email, password_hash FROM users WHERE email = ?`, email).Scan(&u.ID, &u.Email, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		checkPassword(dummyHash, password)
		return User{}, ErrBadCredentials
	}
	if err != nil {
		return User{}, err
	}
	if !checkPassword(encoded, password) {
		return User{}, ErrBadCredentials
	}
	return u, nil
}

// User looks up an account by ID.
func (s *Store) User(id string) (User, error) {
	u := User{ID: id}
	err := s.db.QueryRow(`SELECT email FROM users WHERE id = ?`, id).Scan(&u.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
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
	if err != nil || wantClient != clientID || expires < s.now().Unix() {
		return Tokens{}, ErrInvalidGrant
	}
	if _, err := tx.Exec(`DELETE FROM tokens WHERE refresh_hash = ?`, hash(refreshToken)); err != nil {
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
