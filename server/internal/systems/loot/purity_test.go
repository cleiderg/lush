package loot_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoIODeps checks that loot depends, directly or transitively, on no
// network or database package.
func TestNoIODeps(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, p := range []string{"net", "database", "crypto/tls"} {
			if dep == p || strings.HasPrefix(dep, p+"/") {
				t.Errorf("loot depends on %s", dep)
			}
		}
		if strings.Contains(dep, "pgx") || strings.Contains(dep, "websocket") {
			t.Errorf("loot depends on %s", dep)
		}
	}
}
