package gamedata_test

import (
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoForbiddenDeps checks that gamedata, directly or transitively, depends
// on no network or database package and no other server/internal package
// (so world and systems can both import it without a cycle).
func TestNoForbiddenDeps(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if forbiddenDep(dep) {
			t.Errorf("gamedata depends on forbidden package %s", dep)
		}
	}
}

func forbiddenDep(path string) bool {
	for _, p := range []string{"net", "database", "crypto/tls"} {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	if i := strings.Index(path, "/internal/"); i >= 0 && strings.HasSuffix(path[:i], "/server") {
		return !strings.HasSuffix(path, "/internal/systems/gamedata")
	}
	return strings.Contains(path, "pgx") || strings.Contains(path, "websocket") || strings.Contains(path, "flatbuffers")
}

// TestNoOSImport checks that no non-test file imports os: data comes from the
// embedded files or an fs.FS passed to Load. (os can't be checked
// transitively, because fmt and encoding/json depend on it.)
func TestNoOSImport(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "os" || strings.HasPrefix(p, "os/") {
				t.Errorf("%s imports %s", name, p)
			}
		}
	}
}

func TestForbiddenDep(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"net", true},
		{"net/http", true},
		{"database/sql", true},
		{"github.com/cleiderg/lush/server/internal/store", true},
		{"github.com/cleiderg/lush/server/internal/transport", true},
		{"github.com/cleiderg/lush/server/internal/api", true},
		{"github.com/cleiderg/lush/server/internal/session", true},
		{"github.com/cleiderg/lush/server/internal/protocol", true},
		{"github.com/cleiderg/lush/server/internal/world", true},
		{"github.com/cleiderg/lush/server/internal/systems/gamedata", false},
		{"encoding/json", false},
		{"internal/abi", false},
		{"vendor/golang.org/x/net/dns/dnsmessage", false},
	}
	for _, tt := range tests {
		if got := forbiddenDep(tt.path); got != tt.want {
			t.Errorf("forbiddenDep(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
