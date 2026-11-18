package sandbox

import (
	"fmt"

	"github.com/dop251/goja"
)

// Lib is shared library code of an app: helper functions and constants
// evaluated before every script, so scripts need not repeat them. Each
// library is compiled as its own program named "lib/<name>", so errors and
// stack traces point at the library and line where they happen.
type Lib struct {
	Name string `json:"name"`
	Src  string `json:"src"`
}

// LibHost is a Host whose scripts get shared libraries. Run evaluates them,
// in the order given, before the script.
type LibHost interface {
	Lib() []Lib
}

func libOf(h Host) []Lib {
	if lh, ok := h.(LibHost); ok {
		return lh.Lib()
	}
	return nil
}

// LibProgramName is the program name of a library in errors and stack
// traces, e.g. "lib/helpers:12:3".
func LibProgramName(name string) string { return "lib/" + name }

// runLib evaluates libraries in vm. Libraries must not define handle; with
// check set, that is verified after each one so the error names it.
func (g *Goja) runLib(vm *goja.Runtime, lib []Lib, check bool) error {
	for _, l := range lib {
		p, err := g.compile(LibProgramName(l.Name), l.Src)
		if err != nil {
			return &ScriptError{Message: fmt.Sprintf("lib %q: %v", l.Name, err)}
		}
		if _, err := vm.RunProgram(p); err != nil {
			return &ScriptError{Message: fmt.Sprintf("lib %q: %s", l.Name, describe(err))}
		}
		if check && vm.Get("handle") != nil {
			return &ScriptError{Message: fmt.Sprintf("lib %q defines handle; only scripts define handle(req)", l.Name)}
		}
	}
	return nil
}
