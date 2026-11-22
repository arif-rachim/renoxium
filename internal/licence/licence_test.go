package licence

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	raw, err := Sign(priv, Licence{App: "shop", Account: "usr_1", Email: "a@shop.ae", Plan: "pro"})
	if err != nil {
		t.Fatal(err)
	}
	l, err := Verify(raw, pub)
	if err != nil || l.App != "shop" || l.Plan != "pro" || l.Issued == "" {
		t.Fatalf("%+v %v", l, err)
	}

	// Another key, a changed licence, a free plan and nothing at all are refused.
	other, _, _ := ed25519.GenerateKey(nil)
	if _, err := Verify(raw, other); !errors.Is(err, ErrBadSig) {
		t.Errorf("another key: %v", err)
	}
	if _, err := Verify(bytes.Replace(raw, []byte(`pro`), []byte(`business`), 1), pub); !errors.Is(err, ErrBadSig) {
		t.Errorf("a changed plan: %v", err)
	}
	free, _ := Sign(priv, Licence{App: "shop", Plan: "free"})
	if _, err := Verify(free, pub); !errors.Is(err, ErrPlan) {
		t.Errorf("free plan: %v", err)
	}
	if _, err := Verify(nil, pub); !errors.Is(err, ErrUnsigned) {
		t.Errorf("no licence: %v", err)
	}
	// Without keys given, Renoxium's own keys are used: a licence signed elsewhere fails.
	if _, err := Verify(raw); !errors.Is(err, ErrBadSig) {
		t.Errorf("not Renoxium's key: %v", err)
	}

	seed, public, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	key, err := KeyFromSeed(seed)
	if err != nil || key.Public().(ed25519.PublicKey) == nil {
		t.Fatal(err)
	}
	_ = public
	if _, err := KeyFromSeed("short"); err == nil {
		t.Error("a bad seed was accepted")
	}
}
