// Package loot rolls what an NPC kill drops, from the NPC table in gamedata
// (docs/specs/gameplay.md, NPCs): every kill drops GoldMin..GoldMax gold, and
// each item drop rolls on its own at 1 in Chance.
//
// Roll takes plain values and an injected random source. It never sees or
// changes a World or Player; the combat system (#144) and the drop spawner
// apply the result.
package loot

import (
	"fmt"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// Rand is the random source for loot rolls. formula.Rand and
// *math/rand/v2.Rand satisfy it.
type Rand interface {
	IntN(n int) int // uniform in [0, n)
}

// Loot is what one NPC kill drops.
type Loot struct {
	Gold  int
	Items []world.Stack // one stack per drop that landed, in table order
}

// Roll rolls the loot for one kill of NPC type id. It draws the gold first,
// then one roll per drop with Chance > 1, in table order, so identically
// seeded sources give identical loot. An unknown id returns an error and no
// loot.
func Roll(d *gamedata.Data, id gamedata.NPCTypeID, r Rand) (Loot, error) {
	npc, ok := d.NPC(id)
	if !ok {
		return Loot{}, fmt.Errorf("roll loot: unknown npc type %d", id)
	}
	l := Loot{Gold: npc.GoldMin}
	if span := npc.GoldMax - npc.GoldMin; span > 0 {
		l.Gold += r.IntN(span + 1)
	}
	for _, drop := range npc.Drops {
		if drop.Chance > 1 && r.IntN(drop.Chance) != 0 {
			continue
		}
		l.Items = append(l.Items, world.Stack{Item: drop.Item, Qty: drop.Quantity})
	}
	return l, nil
}
