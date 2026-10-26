// Package pvp holds the pure player-versus-player rules from
// docs/specs/gameplay.md (Combat; Death in the PvP zone): who may attack whom,
// and how a player who dies in the PvP zone splits their holdings into kept
// and dropped items.
//
// Both rules take plain values and read their numbers from gamedata and
// never see a World or Player. ApplyDeath applies a death split to one
// inventory. Combat (combat.go) is the system that applies them on the
// combat tick: player attacks, hits, XP, and death with respawn.
package pvp

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// AreaLookup says which gameplay area a position is in. *areas.Lookup
// implements it; its safe circle takes precedence over every polygon.
type AreaLookup interface {
	AreaAt(p world.Vec2) areas.Area
}

// Fighter is what the attack rule needs to know about one player.
type Fighter struct {
	Level int        // combat level
	Pos   world.Vec2 // world metres
}

// Verdict is the outcome of CanAttack. Allowed is the only one that permits
// the attack; the others say why it was refused.
type Verdict uint8

const (
	Allowed           Verdict = iota
	AttackerNotInZone         // the attacker is outside the PvP zone (or in the safe circle)
	TargetNotInZone           // the target is outside the PvP zone (or in the safe circle)
	OutsideLevelGap           // combat levels differ by more than Rules.PvPLevelGap
)

var verdictNames = [...]string{"allowed", "attacker_not_in_zone", "target_not_in_zone", "outside_level_gap"}

func (v Verdict) String() string {
	if int(v) < len(verdictNames) {
		return verdictNames[v]
	}
	return fmt.Sprintf("Verdict(%d)", uint8(v))
}

// CanAttack reports whether attacker may attack target: both must stand in
// the PvP zone, outside the safe circle, and their combat levels must differ
// by at most Rules.PvPLevelGap. The first failing condition is returned.
func CanAttack(d *gamedata.Data, a AreaLookup, attacker, target Fighter) Verdict {
	if !a.AreaAt(attacker.Pos).PvP() {
		return AttackerNotInZone
	}
	if !a.AreaAt(target.Pos).PvP() {
		return TargetNotInZone
	}
	gap := attacker.Level - target.Level
	if gap < 0 {
		gap = -gap
	}
	if gap > d.Rules.PvPLevelGap {
		return OutsideLevelGap
	}
	return Allowed
}

// Piece is a quantity of one item taken from one place in a player's
// holdings: an inventory slot, or an equipment slot when Worn is set.
type Piece struct {
	Item gamedata.ItemID
	Qty  int
	Worn gamedata.Slot // equipment slot it came from, or SlotNone
	Slot int           // inventory slot index it came from; -1 when worn
}

// DeathSplit is how a PvP death divides a player's holdings. Kept and
// Dropped together are exactly the original items; Gold is the whole gold
// balance, which is always kept (gold is never dropped in Phase 1).
type DeathSplit struct {
	Kept    []Piece
	Dropped []Piece
	Gold    int
}

// wornOrder is the order equipment is listed in, and the tie-break between
// worn items of equal value.
var wornOrder = [...]gamedata.Slot{gamedata.SlotWeapon, gamedata.SlotHead, gamedata.SlotBody, gamedata.SlotShield}

// SplitOnDeath keeps the Rules.PvPItemsKept most valuable single items in inv,
// valued by what the shop pays for one (Item.ShopBuysFor; unknown items are
// worth 0). Worn items count, and every unit of a stack counts separately.
// Everything else is dropped. Gold is never dropped.
//
// Ties are broken by item id, then worn before carried (in weapon, head,
// body, shield order), then by inventory slot, so the same holdings always
// give the same split. Kept and Dropped list pieces in that order. inv is not
// changed.
func SplitOnDeath(d *gamedata.Data, inv world.Inventory) DeathSplit {
	type source struct {
		piece Piece
		value int
		rank  int // worn slots first, then inventory slots, in order
	}
	srcs := make([]source, 0, len(wornOrder)+len(inv.Slots))
	for i, s := range wornOrder {
		if id := inv.Equipment.Get(s); id != 0 {
			srcs = append(srcs, source{Piece{Item: id, Qty: 1, Worn: s, Slot: -1}, value(d, id), i})
		}
	}
	for i, st := range inv.Slots {
		if !st.Empty() && st.Qty > 0 {
			srcs = append(srcs, source{Piece{Item: st.Item, Qty: st.Qty, Slot: i}, value(d, st.Item), len(wornOrder) + i})
		}
	}
	slices.SortFunc(srcs, func(x, y source) int {
		return cmp.Or(
			cmp.Compare(y.value, x.value),
			cmp.Compare(x.piece.Item, y.piece.Item),
			cmp.Compare(x.rank, y.rank),
		)
	})

	split := DeathSplit{Gold: inv.Gold}
	left := max(d.Rules.PvPItemsKept, 0)
	for _, s := range srcs {
		keep := min(left, s.piece.Qty)
		left -= keep
		if keep > 0 {
			p := s.piece
			p.Qty = keep
			split.Kept = append(split.Kept, p)
		}
		if drop := s.piece.Qty - keep; drop > 0 {
			p := s.piece
			p.Qty = drop
			split.Dropped = append(split.Dropped, p)
		}
	}
	return split
}

// ReasonPvPDeath is the economy audit reason for the changes ApplyDeath makes.
const ReasonPvPDeath = "pvp_death"

// ApplyDeath applies SplitOnDeath to inv: every dropped piece is removed, worn
// ones straight from their equipment slot (ChangeRemoveWorn) and carried ones
// from their inventory slot (ChangeRemove), one change per piece. The kept
// items stay where they are and the gold balance is untouched, so it never
// needs a free slot.
//
// It returns the dropped stacks, one per dropped piece in Dropped order, for
// the caller to spawn as ground items on the same tick, and the changes to
// record under ReasonPvPDeath. On error, including a locked inventory
// (world.ErrInventoryLocked), inv is unchanged and nothing is returned.
func ApplyDeath(d *gamedata.Data, inv *world.Inventory) ([]world.Stack, []world.Change, error) {
	if inv.Locked() {
		return nil, nil, fmt.Errorf("pvp death: %w", world.ErrInventoryLocked)
	}
	split := SplitOnDeath(d, *inv)
	work := inv.Clone()
	dropped := make([]world.Stack, 0, len(split.Dropped))
	changes := make([]world.Change, 0, len(split.Dropped))
	for _, p := range split.Dropped {
		var ch []world.Change
		var err error
		if p.Worn != gamedata.SlotNone {
			ch, err = work.RemoveWorn(p.Worn)
		} else {
			ch, err = work.RemoveFromSlot(p.Slot, p.Qty)
		}
		if err == nil && (len(ch) != 1 || ch[0].Item != p.Item || ch[0].Qty != p.Qty) {
			err = errors.New("removal does not match the dropped piece")
		}
		if err != nil {
			return nil, nil, fmt.Errorf("pvp death: drop %d × item %d: %w", p.Qty, p.Item, err)
		}
		dropped = append(dropped, world.Stack{Item: p.Item, Qty: p.Qty})
		changes = append(changes, ch...)
	}
	*inv = work
	return dropped, changes, nil
}

func value(d *gamedata.Data, id gamedata.ItemID) int {
	it, ok := d.Item(id)
	if !ok {
		return 0
	}
	return it.ShopBuysFor
}
