// Package password hashes and checks passwords with argon2id.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// MinLength is the shortest accepted password, in characters.
const MinLength = 10

const (
	argonTime    = 2
	argonMemory  = 19 * 1024 // KiB
	argonThreads = 1
	argonKeyLen  = 32
)

// Validate checks a new password's length.
func Validate(pw string) error {
	if utf8.RuneCountInString(pw) < MinLength || len(pw) > 512 {
		return fmt.Errorf("the password must be at least %d characters", MinLength)
	}
	return nil
}

// slots caps how many hashes run at once. Each takes about 19 MiB, so
// without a cap a burst of logins could exhaust memory on a small server.
var slots = make(chan struct{}, 4)

func acquire() func() {
	slots <- struct{}{}
	return func() { <-slots }
}

// Hash returns an encoded argon2id hash with a random salt.
func Hash(pw string) string {
	defer acquire()()
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$%d$%d$%d$%s$%s", argonTime, argonMemory, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

// Check reports whether pw matches an encoded hash.
func Check(encoded, pw string) bool {
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
	// Only hashes this package wrote are accepted, so a stored value cannot
	// ask for an expensive computation.
	if t != argonTime || m != argonMemory || p != argonThreads || len(want) != argonKeyLen {
		return false
	}
	defer acquire()()
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

var dummy = Hash("renoxium-dummy-password")

// CheckMissing spends the same time as Check, for logins with an unknown
// email, so timing does not reveal which emails have accounts.
func CheckMissing(pw string) { Check(dummy, pw) }
