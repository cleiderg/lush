package session_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

func testData(t *testing.T) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// kitHoldings is the starting kit as the store should hold it.
func kitHoldings(t *testing.T, d *gamedata.Data) (store.Holdings, []world.Change) {
	t.Helper()
	inv, changes, err := world.NewStartingInventory(d)
	if err != nil {
		t.Fatal(err)
	}
	var h store.Holdings
	h.Gold = int64(inv.Gold)
	for i, s := range inv.Slots {
		if !s.Empty() {
			h.Inventory[i] = store.Stack{Item: int32(s.Item), Qty: int64(s.Qty)}
		}
	}
	h.Equipment[store.EquipWeapon] = int32(inv.Equipment.Weapon)
	h.Equipment[store.EquipHead] = int32(inv.Equipment.Head)
	h.Equipment[store.EquipBody] = int32(inv.Equipment.Body)
	h.Equipment[store.EquipShield] = int32(inv.Equipment.Shield)
	return h, changes
}

func decodeStats(t *testing.T, r *rig, c *client) protocol.PlayerStats {
	t.Helper()
	typ, tbl := r.next(c)
	if typ != protocol.MessagePlayerStats {
		t.Fatalf("got %v, want PlayerStats", typ)
	}
	var st protocol.PlayerStats
	st.Init(tbl.Bytes, tbl.Pos)
	return st
}

func decodeInventory(t *testing.T, r *rig, c *client) protocol.InventoryState {
	t.Helper()
	typ, tbl := r.next(c)
	if typ != protocol.MessageInventoryState {
		t.Fatalf("got %v, want InventoryState", typ)
	}
	var inv protocol.InventoryState
	inv.Init(tbl.Bytes, tbl.Pos)
	return inv
}

// joinFully reads Welcome, ItemCatalog, NpcCatalog, PlayerStats and InventoryState, in
// that order, and returns the player id, token and the last two.
func joinFully(t *testing.T, r *rig, c *client) (world.PlayerID, string, protocol.ItemCatalog, protocol.PlayerStats, protocol.InventoryState) {
	t.Helper()
	id, tok := r.expectWelcome(c)
	typ, tbl := r.next(c)
	if typ != protocol.MessageItemCatalog {
		t.Fatalf("after Welcome got %v, want ItemCatalog", typ)
	}
	var cat protocol.ItemCatalog
	cat.Init(tbl.Bytes, tbl.Pos)
	if typ, _ := r.next(c); typ != protocol.MessageNpcCatalog {
		t.Fatalf("after ItemCatalog got %v, want NpcCatalog", typ)
	}
	st := decodeStats(t, r, c)
	inv := decodeInventory(t, r, c)
	return world.PlayerID(id), tok, cat, st, inv
}

func checkInventoryState(t *testing.T, got protocol.InventoryState, want store.Holdings, slots int) {
	t.Helper()
	if got.SlotsLength() != slots {
		t.Fatalf("InventoryState has %d slots, want %d", got.SlotsLength(), slots)
	}
	var s protocol.InventorySlot
	for i := 0; i < slots; i++ {
		got.Slots(&s, i)
		if w := want.Inventory[i]; int32(s.ItemId()) != w.Item || int64(s.Quantity()) != w.Qty {
			t.Fatalf("slot %d = %d×%d, want %d×%d", i, s.Quantity(), s.ItemId(), w.Qty, w.Item)
		}
	}
	eq := [store.EquipSlots]int32{int32(got.Weapon()), int32(got.Head()), int32(got.Body()), int32(got.Shield())}
	if eq != want.Equipment {
		t.Fatalf("equipment = %v, want %v", eq, want.Equipment)
	}
}

// AC1: a returning player's holdings and XP come from the store, HP at max.
func TestJoinLoadsStoredHoldingsAndXP(t *testing.T) {
	d := testData(t)
	m := store.NewMemory()
	ctx := context.Background()
	var h store.Holdings
	h.Gold = 1234
	h.Inventory[0] = store.Stack{Item: 12, Qty: 1}
	h.Inventory[5] = store.Stack{Item: 9, Qty: 2}
	h.Equipment[store.EquipWeapon] = 1
	h.Equipment[store.EquipHead] = 7
	sp, err := m.CreateGuest(ctx, sha("returning"), h, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Save(ctx, store.Player{ID: sp.ID, X: 3, Z: 4, XP: 5000, Version: sp.Version}); err != nil {
		t.Fatal(err)
	}

	r := newRigStore(t, 64, session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d}, m)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), "returning"))
	id, _, _, st, inv := joinFully(t, r, c)

	p := r.w.Player(id)
	if p == nil {
		t.Fatal("player not in world")
	}
	if p.XP != 5000 || p.Inv.Gold != 1234 {
		t.Fatalf("world XP %d gold %d, want 5000 and 1234", p.XP, p.Inv.Gold)
	}
	if p.Inv.Slots[0] != (world.Stack{Item: 12, Qty: 1}) || p.Inv.Slots[5] != (world.Stack{Item: 9, Qty: 2}) || p.Inv.Count(9) != 2 {
		t.Fatalf("world slots = %v", p.Inv.Slots)
	}
	if p.Inv.Equipment != (world.Equipment{Weapon: 1, Head: 7}) {
		t.Fatalf("world equipment = %+v", p.Inv.Equipment)
	}
	if max := p.MaxHP(d.Rules); p.HP != max || max <= d.Rules.BaseHP {
		t.Fatalf("HP %d, want max %d above base", p.HP, max)
	}
	if st.Xp() != 5000 || st.Gold() != 1234 || int(st.Hp()) != p.HP || int(st.MaxHp()) != p.HP ||
		int(st.CombatLevel()) != p.CombatLevel(d.Rules) {
		t.Fatalf("PlayerStats hp %d/%d level %d xp %d gold %d", st.Hp(), st.MaxHp(), st.CombatLevel(), st.Xp(), st.Gold())
	}
	checkInventoryState(t, inv, h, d.Rules.InventorySlots)
}

// AC2: a new guest gets the kit from the rules in one audited write.
func TestNewGuestGetsAuditedStartingKit(t *testing.T) {
	d := testData(t)
	m := store.NewMemory()
	r := newRigStore(t, 64, session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d}, m)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	id, tok, _, st, inv := joinFully(t, r, c)

	want, changes := kitHoldings(t, d)
	if want.Gold != int64(d.Rules.StartingGold) || want.Gold == 0 {
		t.Fatalf("kit gold %d, rules %d", want.Gold, d.Rules.StartingGold)
	}
	sp, err := m.LoadByTokenHash(context.Background(), sha(tok))
	if err != nil {
		t.Fatal(err)
	}
	if sp.Holdings != want {
		t.Fatalf("stored holdings %+v, want %+v", sp.Holdings, want)
	}
	entries := m.AuditEntries()
	wantN := len(d.Rules.StartingKit.Equipped) + len(d.Rules.StartingKit.Inventory) + 1
	if len(entries) != wantN || len(changes) != wantN {
		t.Fatalf("%d audit entries (%d changes), want %d: %+v", len(entries), len(changes), wantN, entries)
	}
	gold := 0
	for i, e := range entries {
		if e.Actor != sp.ID || e.Counterparty != 0 || e.Kind != changes[i].Kind.String() {
			t.Fatalf("entry %d = %+v, want %s by %d", i, e, changes[i].Kind, sp.ID)
		}
		if e.Kind == "gold" {
			gold++
		} else if e.Kind != "item_create" {
			t.Fatalf("entry %d kind %q", i, e.Kind)
		}
	}
	if gold != 1 {
		t.Fatalf("%d gold entries, want 1", gold)
	}
	if st.Gold() != uint32(d.Rules.StartingGold) {
		t.Fatalf("PlayerStats gold %d", st.Gold())
	}
	checkInventoryState(t, inv, want, d.Rules.InventorySlots)
	if p := r.w.Player(id); p == nil || p.HP != p.MaxHP(d.Rules) {
		t.Fatalf("new player %+v not at max HP", p)
	}
}

// AC4: after Welcome come ItemCatalog with every item, PlayerStats and
// InventoryState.
func TestJoinSendsCatalogStatsInventoryInOrder(t *testing.T) {
	d := testData(t)
	r := newRigCfg(t, 64, session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d})
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	_, _, cat, _, _ := joinFully(t, r, c)
	if cat.ItemsLength() != len(d.Items) || len(d.Items) == 0 {
		t.Fatalf("catalog has %d items, want %d", cat.ItemsLength(), len(d.Items))
	}
	var def protocol.ItemDef
	for i, it := range d.Items {
		cat.Items(&def, i)
		price, sells := d.ShopSellPrice(it.ID)
		if gamedata.ItemID(def.Id()) != it.ID || string(def.Name()) != it.Name ||
			int(def.LevelToEquip()) != it.LevelToEquip || int(def.Accuracy()) != it.Accuracy ||
			int(def.Strength()) != it.Strength || int(def.Defence()) != it.Defence ||
			int(def.AttackSpeed()) != it.AttackSpeed || int(def.Heal()) != it.Heals ||
			int(def.ShopBuysFor()) != it.ShopBuysFor || def.ShopSells() != sells || int(def.ShopSellsFor()) != price {
			t.Fatalf("item %d: wire %+v differs from %+v", i, def, it)
		}
		if it.Kind.IsEquipment() == (def.EquipSlot() == protocol.EquipSlotNone) {
			t.Fatalf("item %s: equip slot %v for kind %s", it.Key, def.EquipSlot(), it.Kind)
		}
	}
}

// countUpdates reads n messages and counts PlayerStats and InventoryState.
func countUpdates(t *testing.T, r *rig, c *client, n int) (stats, inv int, last protocol.PlayerStats) {
	t.Helper()
	for i := 0; i < n; i++ {
		typ, tbl := r.next(c)
		switch typ {
		case protocol.MessagePlayerStats:
			stats++
			last.Init(tbl.Bytes, tbl.Pos)
		case protocol.MessageInventoryState:
			inv++
		}
	}
	return
}

// AC5: stats and inventory are resent once after a change, never otherwise.
func TestStatsAndInventoryResentOnlyOnChange(t *testing.T) {
	d := testData(t)
	r := newRigCfg(t, 256, session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d})
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	id, _, _, _, _ := joinFully(t, r, c)

	if s, i, _ := countUpdates(t, r, c, 10); s != 0 || i != 0 {
		t.Fatalf("unchanged player got %d PlayerStats and %d InventoryState", s, i)
	}

	// The test goroutine runs the ticks (in next), so it may change the player between them.
	p := r.w.Player(id)
	p.Inv.Gold += 25
	s, i, last := countUpdates(t, r, c, 10)
	if s != 1 || i != 0 {
		t.Fatalf("after gold change: %d PlayerStats and %d InventoryState, want 1 and 0", s, i)
	}
	if int(last.Gold()) != p.Inv.Gold {
		t.Fatalf("PlayerStats gold %d, want %d", last.Gold(), p.Inv.Gold)
	}

	if _, err := p.Inv.Unequip(gamedata.SlotWeapon); err != nil {
		t.Fatal(err)
	}
	if s, i, _ := countUpdates(t, r, c, 10); s != 0 || i != 1 {
		t.Fatalf("after unequip: %d PlayerStats and %d InventoryState, want 0 and 1", s, i)
	}
}

// failGrant fails the first guest creation partway through its write: the
// kit it passes on has an invalid slot after the valid ones, so a database
// store has already inserted the player row when the write fails.
type failGrant struct {
	store.Players
	failed atomic.Bool
}

func (f *failGrant) CreateGuest(ctx context.Context, h string, kit store.Holdings, entries []store.Entry) (store.Player, error) {
	if f.failed.CompareAndSwap(false, true) {
		kit.Inventory[store.InventorySlots-1] = store.Stack{Item: 1, Qty: -1}
	}
	return f.Players.CreateGuest(ctx, h, kit, entries)
}

// pgStore returns a Postgres store in a schema of its own, so it doesn't race
// other packages' tests on the shared database, and an audit row counter.
func pgStore(t *testing.T) (store.Players, func() int) {
	t.Helper()
	base := os.Getenv("LUSH_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("LUSH_TEST_DATABASE_URL not set")
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal("LUSH_TEST_DATABASE_URL is not a URL")
	}
	const schema = "session_test"
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		pool.Close()
	})
	for _, stmt := range []string{"DROP SCHEMA IF EXISTS " + schema + " CASCADE", "CREATE SCHEMA " + schema} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return store.NewPGPlayers(pool), func() int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM economy_audit").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
}

// AC3: a failure at the kit grant leaves no kit-less player behind, and the
// next join with that token ends with exactly one kit.
func TestFailedKitGrantNeverHalfCreates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store func(t *testing.T) (store.Players, func() int)
	}{
		{"memory", func(*testing.T) (store.Players, func() int) {
			m := store.NewMemory()
			return m, func() int { return len(m.AuditEntries()) }
		}},
		{"pgx", pgStore},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testData(t)
			players, auditRows := tc.store(t)
			ctx := context.Background()
			r := newRigStore(t, 64, session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d},
				&failGrant{Players: players})
			const tok = "kit-token"
			session.SetTokenSource(r.sess, func() (string, error) { return tok, nil })
			v := uint16(protocol.ProtocolVersionCurrent)

			c := r.dial(hello(v, ""))
			rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, _, err := c.c.Read(rctx)
			cancel()
			if err == nil {
				t.Fatal("join with a failed kit grant got a message")
			}
			if _, err := players.LoadByTokenHash(ctx, sha(tok)); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("after failed grant: load err = %v, want ErrNotFound", err)
			}
			if n := auditRows(); n != 0 {
				t.Fatalf("after failed grant: %d audit rows, want 0", n)
			}

			want, changes := kitHoldings(t, d)
			for i := 0; i < 2; i++ { // the join that creates, then a resume
				c := r.dial(hello(v, tok))
				_, got, _, _, _ := joinFully(t, r, c)
				if got != tok {
					t.Fatalf("join %d token %q, want %q", i, got, tok)
				}
				sp, err := players.LoadByTokenHash(ctx, sha(tok))
				if err != nil {
					t.Fatal(err)
				}
				if sp.Holdings != want {
					t.Fatalf("join %d holdings %+v, want one kit %+v", i, sp.Holdings, want)
				}
				if n := auditRows(); n != len(changes) {
					t.Fatalf("join %d: %d audit rows, want %d (one kit)", i, n, len(changes))
				}
			}
		})
	}
}

// AC6: periodic and logout saves carry the player's XP.
func TestSavesIncludeXP(t *testing.T) {
	r := newSaveRig(t)
	id, sid, c := r.join()
	r.w.Tick() // arms the interval
	r.w.Player(id).XP = 777
	r.intervalPasses()
	savedXP := func(xp int64) func() bool {
		return func() bool {
			for _, s := range r.st.snapshot() {
				if s.p.ID == sid && s.err == nil && s.p.XP == xp {
					return true
				}
			}
			return false
		}
	}
	r.st.waitFor(t, "periodic save with XP", savedXP(777))

	r.w.Player(id).XP = 900
	c.c.CloseNow()
	r.tickUntilGone(id)
	r.st.waitFor(t, "logout save with XP", savedXP(900))
}
