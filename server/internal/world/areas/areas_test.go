package areas_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

const sharedAreas = "../../../../shared/map/areas.json"

// TestEmbeddedAreasMatchShared fails when the embedded copy drifts from the
// shared map's areas file.
func TestEmbeddedAreasMatchShared(t *testing.T) {
	shared, err := os.ReadFile(sharedAreas)
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := os.ReadFile("areas.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(embedded, shared) {
		t.Fatal("server/internal/world/areas/areas.json differs from shared/map/areas.json; " +
			"refresh it from the repo root with: cp shared/map/areas.json server/internal/world/areas/areas.json")
	}
}

func load(t testing.TB) *areas.Lookup {
	t.Helper()
	l, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestAreaAt(t *testing.T) {
	l := load(t)
	sp := l.Spawn()
	r := float32(l.SafeRadius())
	tests := []struct {
		name string
		pos  world.Vec2
		want areas.Area
	}{
		{"spawn", sp, areas.SafeCircle},
		{"shop", l.Shop(), areas.SafeCircle},
		{"park outside circle", world.Vec2{X: -21.5, Y: -180}, areas.AldrichPark},
		{"just inside safe radius", world.Vec2{X: sp.X + r - 0.1, Y: sp.Y}, areas.SafeCircle},
		{"just outside safe radius", world.Vec2{X: sp.X + r + 0.1, Y: sp.Y}, areas.AldrichPark},
		{"just inside safe radius south", world.Vec2{X: sp.X, Y: sp.Y + r - 0.1}, areas.SafeCircle},
		{"just outside safe radius south", world.Vec2{X: sp.X, Y: sp.Y + r + 0.1}, areas.AldrichPark},
		{"south fields", world.Vec2{X: -280, Y: 280}, areas.SouthFields},
		{"south fields east edge inside", world.Vec2{X: -200.1, Y: 280}, areas.SouthFields},
		{"south fields east edge outside", world.Vec2{X: -199.9, Y: 280}, areas.None},
		{"pvp zone", world.Vec2{X: 300, Y: -340}, areas.PvPZone},
		{"pvp south edge inside", world.Vec2{X: 300, Y: -280.1}, areas.PvPZone},
		{"pvp south edge outside", world.Vec2{X: 300, Y: -279.9}, areas.None},
		{"pvp diagonal edge inside", world.Vec2{X: 210.1, Y: -290}, areas.PvPZone},
		{"pvp diagonal edge outside", world.Vec2{X: 209.9, Y: -290}, areas.None},
		{"park west edge inside", world.Vec2{X: -159.8, Y: -71.3}, areas.AldrichPark},
		{"park west edge outside", world.Vec2{X: -160.0, Y: -71.3}, areas.None},
		{"outside all areas", world.Vec2{X: 0, Y: 1000}, areas.None},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.AreaAt(tt.pos); got != tt.want {
				t.Errorf("AreaAt(%v) = %v, want %v", tt.pos, got, tt.want)
			}
		})
	}
}

func TestHelpers(t *testing.T) {
	l := load(t)
	tests := []struct {
		name         string
		pos          world.Vec2
		safe, pvp    bool
		pve, areaPvP bool
	}{
		{"spawn", l.Spawn(), true, false, false, false},
		{"park", world.Vec2{X: -21.5, Y: -180}, false, false, true, false},
		{"south fields", world.Vec2{X: -280, Y: 280}, false, false, true, false},
		{"pvp", world.Vec2{X: 300, Y: -340}, false, true, false, true},
		{"nowhere", world.Vec2{X: 0, Y: 1000}, false, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.InSafeCircle(tt.pos); got != tt.safe {
				t.Errorf("InSafeCircle = %v, want %v", got, tt.safe)
			}
			if got := l.InPvP(tt.pos); got != tt.pvp {
				t.Errorf("InPvP = %v, want %v", got, tt.pvp)
			}
			a := l.AreaAt(tt.pos)
			if a.PvE() != tt.pve || a.PvP() != tt.areaPvP {
				t.Errorf("%v: PvE=%v PvP=%v, want %v %v", a, a.PvE(), a.PvP(), tt.pve, tt.areaPvP)
			}
		})
	}
}

// TestSpawnAndShopMatchFile checks the lookup's spawn, shop and safe radius
// against shared/map/areas.json.
func TestSpawnAndShopMatchFile(t *testing.T) {
	data, err := os.ReadFile(sharedAreas)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Spawn, Shop struct{ X, Z int }
		SafeRadius  int
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	l := load(t)
	want := func(x, z int) world.Vec2 { return world.Vec2{X: float32(x) / 10, Y: float32(z) / 10} }
	if got, w := l.Spawn(), want(f.Spawn.X, f.Spawn.Z); got != w {
		t.Errorf("Spawn() = %v, want %v", got, w)
	}
	if got, w := l.Shop(), want(f.Shop.X, f.Shop.Z); got != w {
		t.Errorf("Shop() = %v, want %v", got, w)
	}
	if got, w := l.SafeRadius(), float64(f.SafeRadius)/10; got != w {
		t.Errorf("SafeRadius() = %v, want %v", got, w)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, data string
	}{
		{"not json", `{`},
		{"bad version", `{"version":2,"units":"decimetres","spawn":{"x":0,"z":0},"safeRadius":1,"shop":{"x":0,"z":0},"areas":[]}`},
		{"bad units", `{"version":1,"units":"metres","spawn":{"x":0,"z":0},"safeRadius":1,"shop":{"x":0,"z":0},"areas":[]}`},
		{"no radius", `{"version":1,"units":"decimetres","spawn":{"x":0,"z":0},"shop":{"x":0,"z":0},"areas":[]}`},
		{"no spawn", `{"version":1,"units":"decimetres","safeRadius":1,"shop":{"x":0,"z":0},"areas":[]}`},
		{"no shop", `{"version":1,"units":"decimetres","spawn":{"x":0,"z":0},"safeRadius":1,"areas":[]}`},
		{"missing areas", `{"version":1,"units":"decimetres","spawn":{"x":0,"z":0},"safeRadius":1,"shop":{"x":0,"z":0},"areas":[]}`},
		{"unknown area", `{"version":1,"units":"decimetres","spawn":{"x":0,"z":0},"safeRadius":1,"shop":{"x":0,"z":0},"areas":[{"id":"moon","kind":"pve","p":[0,0,1,0,1,1]}]}`},
		{"wrong kind", `{"version":1,"units":"decimetres","spawn":{"x":0,"z":0},"safeRadius":1,"shop":{"x":0,"z":0},"areas":[{"id":"pvp_zone","kind":"pve","p":[0,0,1,0,1,1]}]}`},
		{"short polygon", `{"version":1,"units":"decimetres","spawn":{"x":0,"z":0},"safeRadius":1,"shop":{"x":0,"z":0},"areas":[{"id":"pvp_zone","kind":"pvp","p":[0,0,1,0]}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := areas.Parse([]byte(tt.data)); err == nil {
				t.Error("Parse succeeded, want error")
			}
		})
	}
}

func TestAreaAtNoAllocs(t *testing.T) {
	l := load(t)
	pts := []world.Vec2{l.Spawn(), {X: -21.5, Y: -180}, {X: -280, Y: 280}, {X: 300, Y: -340}, {X: 0, Y: 1000}}
	allocs := testing.AllocsPerRun(100, func() {
		for _, p := range pts {
			_ = l.AreaAt(p)
			_ = l.InPvP(p)
			_ = l.InSafeCircle(p)
		}
	})
	if allocs != 0 {
		t.Fatalf("AreaAt allocates %v times per run, want 0", allocs)
	}
}

func BenchmarkAreaAt(b *testing.B) {
	l := load(b)
	pts := []world.Vec2{l.Spawn(), {X: -21.5, Y: -180}, {X: -280, Y: 280}, {X: 300, Y: -340}, {X: 0, Y: 1000}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = l.AreaAt(pts[i%len(pts)])
	}
}

// TestNoForbiddenImports checks that areas, like world, depends on no
// network, database or protocol packages.
func TestNoForbiddenImports(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, p := range []string{"net", "database", "crypto/tls"} {
			if dep == p || strings.HasPrefix(dep, p+"/") {
				t.Errorf("areas depends on forbidden package %s", dep)
			}
		}
		for _, p := range []string{"transport", "store", "api", "protocol", "session"} {
			if strings.Contains(dep, "/internal/"+p) {
				t.Errorf("areas depends on forbidden package %s", dep)
			}
		}
	}
}
