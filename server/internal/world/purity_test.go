package world_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoForbiddenImports checks that world, directly or transitively, imports
// no network, database or protocol packages.
func TestNoForbiddenImports(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if forbiddenImport(dep) {
			t.Errorf("world depends on forbidden package %s", dep)
		}
	}
}

func forbiddenImport(path string) bool {
	for _, p := range []string{"net", "database", "crypto/tls"} {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	for _, p := range []string{"transport", "store", "api", "protocol", "session"} {
		if strings.Contains(path, "/internal/"+p) {
			return true
		}
	}
	return strings.Contains(path, "pgx") || strings.Contains(path, "websocket") || strings.Contains(path, "flatbuffers")
}

// TestNoWallClock checks that no file in world/ calls time.Now().
func TestNoWallClock(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		timeName := ""
		for _, imp := range f.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); path == "time" {
				timeName = "time"
				if imp.Name != nil {
					timeName = imp.Name.Name
				}
			}
		}
		if timeName == "" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Now" {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == timeName {
				t.Errorf("%s: calls time.Now", fset.Position(sel.Pos()))
			}
			return true
		})
	}
}
