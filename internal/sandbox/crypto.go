package sandbox

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/dop251/goja"
)

// Bounds of crypto.randomId(bytes).
const (
	RandomIDDefault = 16
	RandomIDMin     = 8
	RandomIDMax     = 64
)

// RandomID returns n random bytes from crypto/rand as unpadded base64url
// text: 16 bytes give 22 characters.
func RandomID(n int) (string, error) {
	if n < RandomIDMin || n > RandomIDMax {
		return "", fmt.Errorf("crypto.randomId(bytes): bytes must be from %d to %d", RandomIDMin, RandomIDMax)
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// installCrypto gives scripts crypto.randomId(bytes?) for secret links and
// codes, and crypto.sha256(text) as lowercase hex. Both run inside the
// worker: getrandom needs no file or network access, so the kernel
// sandbox still holds.
func installCrypto(vm *goja.Runtime) {
	c := vm.NewObject()
	c.Set("randomId", func(call goja.FunctionCall) goja.Value {
		n := RandomIDDefault
		if a := call.Argument(0); !goja.IsUndefined(a) && !goja.IsNull(a) {
			f := a.ToFloat()
			if f != float64(int(f)) {
				panic(vm.NewTypeError("crypto.randomId(bytes): bytes must be a whole number"))
			}
			n = int(f)
		}
		id, err := RandomID(n)
		if err != nil {
			panic(vm.NewTypeError(err.Error()))
		}
		return vm.ToValue(id)
	})
	c.Set("sha256", func(call goja.FunctionCall) goja.Value {
		a := call.Argument(0)
		if goja.IsUndefined(a) || goja.IsNull(a) {
			panic(vm.NewTypeError("crypto.sha256(text) needs a text"))
		}
		sum := sha256.Sum256([]byte(a.String()))
		return vm.ToValue(hex.EncodeToString(sum[:]))
	})
	vm.Set("crypto", c)
}
