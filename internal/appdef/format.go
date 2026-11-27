package appdef

import (
	"fmt"
	"html/template"
	"regexp"
	"strconv"
	"strings"
)

// Funcs are the functions every app template has besides Go's own. printf
// is Go's, except that a number is given to a number verb whatever its
// kind: rows from the database hold whole numbers as int64, so
// {{printf "%.2f" .price}} printed %!f(int64=34) for a price of 34. A
// float meets %d rounded, and a string holding a number meets either.
var Funcs = template.FuncMap{"printf": numberPrintf}

var verbRE = regexp.MustCompile(`%[-+# 0]*(?:\d+|\*)?(?:\.(?:\d+|\*)?)?([a-zA-Z%])`)

func numberPrintf(format string, args ...any) string {
	verbs := verbRE.FindAllStringSubmatch(format, -1)
	i := 0
	for _, v := range verbs {
		if v[1] == "%" {
			continue
		}
		if i >= len(args) {
			break
		}
		args[i] = forVerb(v[1], args[i])
		i++
	}
	return fmt.Sprintf(format, args...)
}

// forVerb converts one printf argument to what verb prints.
func forVerb(verb string, a any) any {
	switch verb {
	case "f", "F", "e", "E", "g", "G":
		switch x := a.(type) {
		case int:
			return float64(x)
		case int64:
			return float64(x)
		case int32:
			return float64(x)
		case uint, uint64, uint32:
			f, _ := strconv.ParseFloat(fmt.Sprint(x), 64)
			return f
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
				return f
			}
		}
	case "d":
		switch x := a.(type) {
		case float64:
			return int64(x + copySign(0.5, x))
		case float32:
			return int64(float64(x) + copySign(0.5, float64(x)))
		case string:
			if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil {
				return n
			}
			if f, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
				return int64(f + copySign(0.5, f))
			}
		}
	}
	return a
}

func copySign(v, sign float64) float64 {
	if sign < 0 {
		return -v
	}
	return v
}

// BadFormatRE finds Go's mark for a value printed with the wrong verb, such
// as %!f(int64=34) or %!d(string=abc), in a rendered page.
var BadFormatRE = regexp.MustCompile(`%!(?:[a-zA-Z]\([^)]{0,60}\)|\(EXTRA[^)]{0,60}\)|[a-zA-Z]\(MISSING\))`)
