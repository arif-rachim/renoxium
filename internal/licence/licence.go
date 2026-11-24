// Package licence is the note that lets an app exported from Renoxium run on
// its owner's own server: who it was exported for, on which plan, signed by
// Renoxium. It is checked offline, so a self-hosted site never depends on
// Renoxium being reachable. It states the terms; it is not a lock, since the
// source is available.
package licence

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Licence says which app was exported for whom, on which plan.
type Licence struct {
	App     string `json:"app"`
	Account string `json:"account"`
	Email   string `json:"email"`
	Plan    string `json:"plan"`
	Issued  string `json:"issued"` // RFC 3339, UTC
}

// file is licence.json: the licence exactly as signed, and its signature.
type file struct {
	Licence   json.RawMessage `json:"licence"`
	Signature string          `json:"signature"`
}

// PublicKeys are the keys Renoxium signs licences with (base64, standard
// encoding). A new key is added here before the server signs with it; an old
// one stays as long as licences signed with it should keep working.
var PublicKeys = []string{
	"8m47ioamGxp2ajwSK2uVDxd1tL2/CY1ur/ViGy9DQRQ=",
}

// Errors.
var (
	ErrUnsigned = errors.New("this export has no licence for running it on your own server; export it again from a paid work app or an account by agreement with Renoxium")
	ErrBadSig   = errors.New("the licence in this export is not signed by Renoxium")
	ErrPlan     = errors.New("running an app on your own server needs a paid work app or an agreement with Renoxium")
)

// SelfHostPlans are the plans whose exports may run on the owner's server.
// Licences keep the former plan names: "business" for an account by
// agreement with Renoxium, "pro" for a paid work app.
var SelfHostPlans = map[string]bool{"pro": true, "business": true}

// Sign returns licence.json for l, signed with key.
func Sign(key ed25519.PrivateKey, l Licence) ([]byte, error) {
	if l.Issued == "" {
		l.Issued = time.Now().UTC().Format(time.RFC3339)
	}
	body, err := json.Marshal(l)
	if err != nil {
		return nil, err
	}
	// Not indented: the licence must stay byte for byte as signed.
	return json.Marshal(file{Licence: body, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, body))})
}

// Verify checks licence.json against PublicKeys (or keys, when given) and
// that its plan may run on the owner's server.
func Verify(raw []byte, keys ...ed25519.PublicKey) (Licence, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Licence{}, ErrUnsigned
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil || len(f.Licence) == 0 {
		return Licence{}, fmt.Errorf("the licence is not readable: %w", errors.Join(err, ErrBadSig))
	}
	sig, err := base64.StdEncoding.DecodeString(f.Signature)
	if err != nil {
		return Licence{}, ErrBadSig
	}
	if len(keys) == 0 {
		for _, k := range PublicKeys {
			if b, err := base64.StdEncoding.DecodeString(k); err == nil && len(b) == ed25519.PublicKeySize {
				keys = append(keys, ed25519.PublicKey(b))
			}
		}
	}
	signed := false
	for _, k := range keys {
		if ed25519.Verify(k, f.Licence, sig) {
			signed = true
			break
		}
	}
	if !signed {
		return Licence{}, ErrBadSig
	}
	var l Licence
	if err := json.Unmarshal(f.Licence, &l); err != nil {
		return Licence{}, ErrBadSig
	}
	if !SelfHostPlans[l.Plan] {
		return l, ErrPlan
	}
	return l, nil
}

// KeyFromSeed reads a signing key from its base64 seed, as kept in the
// server's environment (RENOXIUM_LICENCE_KEY).
func KeyFromSeed(seed string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(seed))
	if err != nil || len(b) != ed25519.SeedSize {
		return nil, errors.New("the licence key is not a base64 ed25519 seed of 32 bytes")
	}
	return ed25519.NewKeyFromSeed(b), nil
}

// NewKey makes a key pair: the seed for the server's environment and the
// public key for PublicKeys.
func NewKey() (seed, public string, err error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(priv.Seed()), base64.StdEncoding.EncodeToString(pub), nil
}
