package storetest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/store"
)

// CardPotStore is a store with card pots.
type CardPotStore interface {
	store.Players
	store.CardPots
}

// RunCardPots runs the CardPots contract against fresh stores from factory,
// which also returns a function listing the store's audit entries in append
// order (Kind, Actor and Counterparty only).
func RunCardPots(t *testing.T, factory func(t *testing.T) (CardPotStore, func() []store.Entry)) {
	ctx := context.Background()

	// rig creates players with 100 gold each, named by token hash.
	type rig struct {
		s     CardPotStore
		audit func() []store.Entry
		ids   []int64
		names []string
	}
	newRig := func(t *testing.T, names ...string) *rig {
		t.Helper()
		s, audit := factory(t)
		r := &rig{s: s, audit: audit, names: names}
		for _, n := range names {
			p, err := s.CreateGuest(ctx, n, store.Holdings{Gold: 100}, nil)
			if err != nil {
				t.Fatal(err)
			}
			r.ids = append(r.ids, p.ID)
		}
		return r
	}
	gold := func(g int64) store.Holdings { return store.Holdings{Gold: g} }
	// state renders every player's holdings, the audit log and the open pots.
	state := func(t *testing.T, r *rig) string {
		t.Helper()
		var hs []store.Holdings
		for _, n := range r.names {
			p, err := r.s.LoadByTokenHash(ctx, n)
			if err != nil {
				t.Fatal(err)
			}
			hs = append(hs, p.Holdings)
		}
		pots, err := r.s.OpenCardPots(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("holdings %v\naudit %v\npots %v", hs, r.audit(), pots)
	}
	stakeEntry := func(id int64) store.Entry {
		return store.Entry{Kind: "gold", Actor: id, Payload: map[string]any{"gold": -25}}
	}
	// open stakes 25 from every player in r, at table 2.
	open := func(t *testing.T, r *rig) int64 {
		t.Helper()
		var stakes []store.CardStake
		var changes []store.EconomyChange
		var entries []store.Entry
		for _, id := range r.ids {
			stakes = append(stakes, store.CardStake{PlayerID: id, Gold: 25})
			changes = append(changes, store.EconomyChange{PlayerID: id, Holdings: gold(75)})
			entries = append(entries, stakeEntry(id))
		}
		pot, err := r.s.OpenCardPot(ctx, 2, stakes, changes, entries)
		if err != nil {
			t.Fatal(err)
		}
		return pot
	}
	wantGold := func(t *testing.T, r *rig, want ...int64) {
		t.Helper()
		for i, n := range r.names {
			p, err := r.s.LoadByTokenHash(ctx, n)
			if err != nil {
				t.Fatal(err)
			}
			if p.Gold != want[i] {
				t.Errorf("%s gold = %d, want %d", n, p.Gold, want[i])
			}
		}
	}
	wantPots := func(t *testing.T, r *rig, want []store.CardPot) {
		t.Helper()
		got, err := r.s.OpenCardPots(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 && len(want) == 0 {
			return
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("open pots = %+v, want %+v", got, want)
		}
	}

	t.Run("stake commit writes holdings, audit and pot", func(t *testing.T) {
		r := newRig(t, "a", "b", "c")
		pot := open(t, r)
		if pot <= 0 {
			t.Fatalf("pot id = %d", pot)
		}
		wantGold(t, r, 75, 75, 75)
		audit := r.audit()
		if len(audit) != 3 {
			t.Fatalf("audit = %+v, want one entry per player", audit)
		}
		for i, e := range audit {
			if e.Kind != "gold" || e.Actor != r.ids[i] {
				t.Errorf("audit[%d] = %+v, want gold by %d", i, e, r.ids[i])
			}
		}
		wantPots(t, r, []store.CardPot{{ID: pot, Table: 2, Stakes: []store.CardStake{
			{PlayerID: r.ids[0], Gold: 25}, {PlayerID: r.ids[1], Gold: 25}, {PlayerID: r.ids[2], Gold: 25}}}})
	})

	t.Run("stake failure partway writes nothing", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			stakes  func(ids []int64) []store.CardStake
			changes func(ids []int64) []store.EconomyChange
			entries func(ids []int64) []store.Entry
			wantErr error
		}{
			// The last player in id order fails after the others are written.
			{"invalid holdings of last player",
				nil,
				func(ids []int64) []store.EconomyChange {
					return []store.EconomyChange{{PlayerID: ids[0], Holdings: gold(75)}, {PlayerID: ids[1], Holdings: gold(75)}, {PlayerID: ids[2], Holdings: gold(-1)}}
				},
				nil, store.ErrInvalid},
			{"unknown audit actor after the others",
				nil, nil,
				func(ids []int64) []store.Entry {
					return []store.Entry{stakeEntry(ids[0]), stakeEntry(ids[1]), stakeEntry(ids[2] + 1000)}
				},
				store.ErrNotFound},
			{"stake of unknown player",
				func(ids []int64) []store.CardStake {
					return []store.CardStake{{PlayerID: ids[0], Gold: 25}, {PlayerID: ids[2] + 1000, Gold: 25}}
				},
				nil, nil, store.ErrNotFound},
			{"player staked twice",
				func(ids []int64) []store.CardStake {
					return []store.CardStake{{PlayerID: ids[0], Gold: 25}, {PlayerID: ids[0], Gold: 25}}
				},
				nil, nil, store.ErrInvalid},
			{"zero stake",
				func(ids []int64) []store.CardStake { return []store.CardStake{{PlayerID: ids[0], Gold: 0}} },
				nil, nil, store.ErrInvalid},
			{"no stakes",
				func([]int64) []store.CardStake { return nil },
				nil, nil, store.ErrInvalid},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r := newRig(t, "a", "b", "c")
				stakes := []store.CardStake{{PlayerID: r.ids[0], Gold: 25}, {PlayerID: r.ids[1], Gold: 25}, {PlayerID: r.ids[2], Gold: 25}}
				changes := []store.EconomyChange{{PlayerID: r.ids[0], Holdings: gold(75)}, {PlayerID: r.ids[1], Holdings: gold(75)}, {PlayerID: r.ids[2], Holdings: gold(75)}}
				entries := []store.Entry{stakeEntry(r.ids[0]), stakeEntry(r.ids[1]), stakeEntry(r.ids[2])}
				if tc.stakes != nil {
					stakes = tc.stakes(r.ids)
				}
				if tc.changes != nil {
					changes = tc.changes(r.ids)
				}
				if tc.entries != nil {
					entries = tc.entries(r.ids)
				}
				before := state(t, r)
				if _, err := r.s.OpenCardPot(ctx, 2, stakes, changes, entries); !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if after := state(t, r); after != before {
					t.Fatalf("failed stake commit wrote:\nbefore %s\nafter  %s", before, after)
				}
				wantPots(t, r, nil)
			})
		}
	})

	t.Run("payout writes winner, audit and closes the pot", func(t *testing.T) {
		r := newRig(t, "a", "b")
		pot := open(t, r)
		if err := r.s.PayCardPot(ctx, pot, []store.EconomyChange{{PlayerID: r.ids[1], Holdings: gold(125)}},
			[]store.Entry{{Kind: "gold", Actor: r.ids[1], Payload: map[string]any{"gold": 50}}}); err != nil {
			t.Fatal(err)
		}
		wantGold(t, r, 75, 125)
		if audit := r.audit(); len(audit) != 3 || audit[2].Actor != r.ids[1] {
			t.Fatalf("audit = %+v, want the payout entry last", audit)
		}
		wantPots(t, r, nil)

		before := state(t, r)
		if err := r.s.PayCardPot(ctx, pot, []store.EconomyChange{{PlayerID: r.ids[1], Holdings: gold(175)}},
			[]store.Entry{{Kind: "gold", Actor: r.ids[1]}}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("second payout: err = %v, want ErrNotFound", err)
		}
		if after := state(t, r); after != before {
			t.Fatalf("second payout wrote:\nbefore %s\nafter  %s", before, after)
		}
	})

	t.Run("payout failure leaves the pot open", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			pot     func(pot int64) int64
			changes func(ids []int64) []store.EconomyChange
			entries func(ids []int64) []store.Entry
			wantErr error
		}{
			{"invalid holdings", nil,
				func(ids []int64) []store.EconomyChange {
					return []store.EconomyChange{{PlayerID: ids[1], Holdings: gold(-1)}}
				}, nil, store.ErrInvalid},
			{"unknown audit actor after the holdings", nil, nil,
				func(ids []int64) []store.Entry {
					return []store.Entry{{Kind: "gold", Actor: ids[1]}, {Kind: "gold", Actor: ids[1] + 1000}}
				}, store.ErrNotFound},
			{"unknown winner", nil,
				func(ids []int64) []store.EconomyChange {
					return []store.EconomyChange{{PlayerID: ids[1] + 1000, Holdings: gold(50)}}
				}, nil, store.ErrNotFound},
			{"unknown pot", func(pot int64) int64 { return pot + 1000 }, nil, nil, store.ErrNotFound},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r := newRig(t, "a", "b")
				pot := open(t, r)
				id := pot
				if tc.pot != nil {
					id = tc.pot(pot)
				}
				changes := []store.EconomyChange{{PlayerID: r.ids[1], Holdings: gold(125)}}
				entries := []store.Entry{{Kind: "gold", Actor: r.ids[1]}}
				if tc.changes != nil {
					changes = tc.changes(r.ids)
				}
				if tc.entries != nil {
					entries = tc.entries(r.ids)
				}
				before := state(t, r)
				if err := r.s.PayCardPot(ctx, id, changes, entries); !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if after := state(t, r); after != before {
					t.Fatalf("failed payout wrote:\nbefore %s\nafter  %s", before, after)
				}
				wantPots(t, r, []store.CardPot{{ID: pot, Table: 2, Stakes: []store.CardStake{
					{PlayerID: r.ids[0], Gold: 25}, {PlayerID: r.ids[1], Gold: 25}}}})
			})
		}
	})

	t.Run("refund stake closes the pot after the last one", func(t *testing.T) {
		r := newRig(t, "a", "b")
		pot := open(t, r)
		refund := func(i int, g int64) error {
			return r.s.RefundCardStake(ctx, pot, r.ids[i], []store.EconomyChange{{PlayerID: r.ids[i], Holdings: gold(g)}},
				[]store.Entry{{Kind: "gold", Actor: r.ids[i]}})
		}
		before := state(t, r)
		if err := r.s.RefundCardStake(ctx, pot, r.ids[0], []store.EconomyChange{{PlayerID: r.ids[0], Holdings: gold(-1)}},
			[]store.Entry{{Kind: "gold", Actor: r.ids[0]}}); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("invalid refund: err = %v, want ErrInvalid", err)
		}
		if after := state(t, r); after != before {
			t.Fatalf("failed refund wrote:\nbefore %s\nafter  %s", before, after)
		}
		if err := refund(0, 100); err != nil {
			t.Fatal(err)
		}
		wantPots(t, r, []store.CardPot{{ID: pot, Table: 2, Stakes: []store.CardStake{{PlayerID: r.ids[1], Gold: 25}}}})
		before = state(t, r)
		if err := refund(0, 125); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("second refund: err = %v, want ErrNotFound", err)
		}
		if after := state(t, r); after != before {
			t.Fatalf("second refund wrote:\nbefore %s\nafter  %s", before, after)
		}
		if err := refund(1, 100); err != nil {
			t.Fatal(err)
		}
		wantGold(t, r, 100, 100)
		wantPots(t, r, nil)
		if n := len(r.audit()); n != 4 {
			t.Fatalf("%d audit entries, want 2 stakes and 2 refunds", n)
		}
	})

	t.Run("refund open pots at startup", func(t *testing.T) {
		r := newRig(t, "a", "b", "c")
		first := open(t, &rig{s: r.s, ids: r.ids[:2]})
		second := open(t, &rig{s: r.s, ids: r.ids[2:]})
		// a's stake in the first pot is already refunded; a later pot is paid.
		if err := r.s.RefundCardStake(ctx, first, r.ids[0], []store.EconomyChange{{PlayerID: r.ids[0], Holdings: gold(100)}},
			[]store.Entry{{Kind: "gold", Actor: r.ids[0]}}); err != nil {
			t.Fatal(err)
		}
		paid := open(t, &rig{s: r.s, ids: r.ids[:1]})
		if err := r.s.PayCardPot(ctx, paid, []store.EconomyChange{{PlayerID: r.ids[0], Holdings: gold(100)}},
			[]store.Entry{{Kind: "gold", Actor: r.ids[0]}}); err != nil {
			t.Fatal(err)
		}
		wantGold(t, r, 100, 75, 75)
		auditBefore := len(r.audit())

		var asked []store.CardStake
		got, err := r.s.RefundOpenCardPots(ctx, func(p store.CardPot, st store.CardStake) store.Entry {
			asked = append(asked, st)
			return store.Entry{Kind: "gold", Actor: st.PlayerID, Payload: map[string]any{"pot": p.ID, "gold": st.Gold}}
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []store.CardPot{
			{ID: first, Table: 2, Stakes: []store.CardStake{{PlayerID: r.ids[1], Gold: 25}}},
			{ID: second, Table: 2, Stakes: []store.CardStake{{PlayerID: r.ids[2], Gold: 25}}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("refunded %+v, want %+v", got, want)
		}
		wantGold(t, r, 100, 100, 100)
		wantPots(t, r, nil)
		audit := r.audit()[auditBefore:]
		if len(audit) != 2 || audit[0].Actor != r.ids[1] || audit[1].Actor != r.ids[2] {
			t.Fatalf("refund audit = %+v, want one entry each for b and c", audit)
		}
		if len(asked) != 2 {
			t.Fatalf("entry called for %+v", asked)
		}
		if got, err := r.s.RefundOpenCardPots(ctx, func(store.CardPot, store.CardStake) store.Entry { return store.Entry{} }); err != nil || len(got) != 0 {
			t.Fatalf("second refund = %+v, %v; want nothing", got, err)
		}
	})

	t.Run("failed startup refund leaves the pot open", func(t *testing.T) {
		r := newRig(t, "a", "b")
		pot := open(t, r)
		before := state(t, r)
		// b's entry fails after a's gold and entry are written.
		_, err := r.s.RefundOpenCardPots(ctx, func(_ store.CardPot, st store.CardStake) store.Entry {
			e := store.Entry{Kind: "gold", Actor: st.PlayerID}
			if st.PlayerID == r.ids[1] {
				e.Counterparty = r.ids[1] + 1000
			}
			return e
		})
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if after := state(t, r); after != before {
			t.Fatalf("failed refund wrote:\nbefore %s\nafter  %s", before, after)
		}
		wantPots(t, r, []store.CardPot{{ID: pot, Table: 2, Stakes: []store.CardStake{
			{PlayerID: r.ids[0], Gold: 25}, {PlayerID: r.ids[1], Gold: 25}}}})
	})
}
