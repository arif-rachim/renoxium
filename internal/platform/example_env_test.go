package platform_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// exampleEnv is one finished example on preview, as an owner (or their AI
// assistant) set it up: change lets a test edit the seed before it is
// applied, as the assistant does when it fills in the owner's settings.
type exampleEnv struct {
	t   *testing.T
	p   *platform.Platform
	app string
	dir string
}

func newExampleEnv(t *testing.T, example string, change func(def *appdef.App)) *exampleEnv {
	t.Helper()
	dir := t.TempDir()
	p, err := platform.New(platform.Config{DataDir: dir, AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	ex, ok := examples.Get(example)
	if !ok {
		t.Fatalf("no example %s", example)
	}
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if change != nil {
		change(def)
	}
	const app = "shop"
	if _, err := p.CreateApp("usr_test", app); err != nil {
		t.Fatal(err)
	}
	if res, err := p.ApplyChange(context.Background(), app, def); err != nil || !res.OK {
		t.Fatalf("apply %s: %v %+v", example, err, res)
	}
	return &exampleEnv{t: t, p: p, app: app, dir: dir}
}

// do sends a test request to the preview, as email when it is set. It fails
// the test on a server error, and on pages that show a script's mistakes.
func (e *exampleEnv) do(method, path, email string, form map[string]string) platform.TestResponse {
	e.t.Helper()
	r, err := e.p.TestRequest(context.Background(), e.app, platform.TestRequestInput{Method: method, Path: path, Form: form, UserEmail: email})
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	if r.Status >= 500 {
		e.t.Fatalf("%s %s: %d\n%s", method, path, r.Status, logText(r))
	}
	if r.Status == 200 && strings.Contains(r.ContentType, "html") {
		for _, bad := range []string{"NaN", "undefined", "[object Object]", "Infinity"} {
			if i := strings.Index(visibleText(r.Body), bad); i >= 0 {
				e.t.Errorf("%s %s shows %q: …%s…", method, path, bad, around(visibleText(r.Body), i))
			}
		}
	}
	return r
}

func (e *exampleEnv) get(path, email string) platform.TestResponse {
	return e.do("GET", path, email, nil)
}

func (e *exampleEnv) post(path, email string, form map[string]string) platform.TestResponse {
	return e.do("POST", path, email, form)
}

// signup creates a visitor account on the preview.
func (e *exampleEnv) signup(email string) {
	e.t.Helper()
	if r := e.post("/_rx/auth/signup", "", map[string]string{"name": "Fatima Al Marri", "email": email, "password": "fatima password"}); r.Status != 303 {
		e.t.Fatalf("signup %s: %+v", email, r)
	}
}

// row reads one row of the preview database.
func (e *exampleEnv) row(query string, args ...any) map[string]any {
	e.t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(e.dir, "apps", e.app, "preview.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		e.t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(query, args...)
	if err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	if !rows.Next() {
		e.t.Fatalf("no row for %s %v", query, args)
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		e.t.Fatal(err)
	}
	out := map[string]any{}
	for i, c := range cols {
		out[c] = vals[i]
	}
	return out
}

// exec changes the preview database directly, as the owner's admin panel would.
func (e *exampleEnv) exec(query string, args ...any) {
	e.t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(e.dir, "apps", e.app, "preview.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		e.t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
}

// num is a database value as a number.
func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case []byte:
		f, _ := strconv.ParseFloat(string(x), 64)
		return f
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func aed(n float64) string { return fmt.Sprintf("AED %.2f", n) }

// payAmount is the amount on the simulated card checkout of the preview.
var payAmountRE = regexp.MustCompile(`Pay ([0-9.]+) AED`)

func (e *exampleEnv) payAmount(location, email string) float64 {
	e.t.Helper()
	m := payAmountRE.FindStringSubmatch(visibleText(e.get(location, email).Body))
	if m == nil {
		e.t.Fatalf("no amount on the checkout at %s", location)
	}
	f, _ := strconv.ParseFloat(m[1], 64)
	return f
}

var tagRE = regexp.MustCompile(`(?s)<style.*?(?:</style>|$)|<script.*?(?:</script>|$)|<[^>]+>`)
var spaceRE = regexp.MustCompile(`\s+`)

// visibleText is a page's text without tags, styles and scripts.
func visibleText(html string) string {
	return spaceRE.ReplaceAllString(tagRE.ReplaceAllString(html, " "), " ")
}

func around(s string, i int) string {
	a, b := max(0, i-60), min(len(s), i+60)
	return s[a:b]
}
