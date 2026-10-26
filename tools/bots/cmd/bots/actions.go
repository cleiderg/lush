package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/cleiderg/lush/tools/bots/internal/protocol"
)

// span is a randomised interval, set on the command line as "20s-40s" (or a
// single duration for a fixed interval).
type span struct{ min, max time.Duration }

func (s *span) String() string {
	if s.min == s.max {
		return s.min.String()
	}
	return s.min.String() + "-" + s.max.String()
}

func (s *span) Set(v string) error {
	lo, hi, found := strings.Cut(v, "-")
	if !found {
		hi = lo
	}
	a, err := time.ParseDuration(lo)
	if err != nil {
		return err
	}
	b, err := time.ParseDuration(hi)
	if err != nil {
		return err
	}
	if a <= 0 || b < a {
		return fmt.Errorf("want a positive range like 20s-40s, got %q", v)
	}
	s.min, s.max = a, b
	return nil
}

// randIn returns a duration in [lo, hi].
func randIn(rng *rand.Rand, lo, hi time.Duration) time.Duration {
	if hi <= lo {
		return lo
	}
	return lo + time.Duration(rng.Int63n(int64(hi-lo)+1))
}

// schedule says when a repeating action is next due. The first is within
// first of the start; each later one comes every.min to every.max after the
// previous one was done.
type schedule struct {
	every span
	rng   *rand.Rand
	at    time.Time
}

func newSchedule(start time.Time, every span, first time.Duration, rng *rand.Rand) *schedule {
	return &schedule{every: every, rng: rng, at: start.Add(randIn(rng, 0, first))}
}

func (s *schedule) due(now time.Time) bool { return !now.Before(s.at) }

// done records that the action happened at now and picks the next time.
func (s *schedule) done(now time.Time) { s.at = now.Add(randIn(s.rng, s.every.min, s.every.max)) }

// inventory is what the bot knows from the latest InventoryState.
type inventory struct {
	known  bool
	weapon uint16   // item id worn in the weapon slot, 0 if none
	slots  []uint16 // item id per inventory slot, 0 if empty
}

// itemAction is an ItemAction to send.
type itemAction struct {
	kind      protocol.ItemActionKind
	slot      uint8
	equipSlot protocol.EquipSlot
}

// reequipGiveUp bounds the wait for the InventoryState that shows an unequip.
const reequipGiveUp = 10 * time.Second

// itemCycle unequips the bot's weapon when its schedule is due, then equips it
// again from the inventory as soon as an InventoryState shows it there.
type itemCycle struct {
	sched  *schedule
	held   uint16    // weapon unequipped and not yet equipped again, 0 if none
	heldAt time.Time // when it was unequipped
}

// step returns the action to send at now, if any, given the latest inventory.
func (c *itemCycle) step(now time.Time, inv inventory) (itemAction, bool) {
	if !inv.known {
		return itemAction{}, false
	}
	if c.held != 0 {
		if inv.weapon == 0 {
			for i, id := range inv.slots {
				if id == c.held {
					c.held = 0
					return itemAction{kind: protocol.ItemActionKindEquip, slot: uint8(i)}, true
				}
			}
		}
		if now.Sub(c.heldAt) >= reequipGiveUp { // the unequip never showed up
			c.held = 0
		}
		return itemAction{}, false
	}
	// Wait for a state with the weapon worn (it may still show the previous
	// unequip), so the due action isn't lost.
	if !c.sched.due(now) || inv.weapon == 0 {
		return itemAction{}, false
	}
	c.sched.done(now)
	c.held, c.heldAt = inv.weapon, now
	return itemAction{kind: protocol.ItemActionKindUnequip, equipSlot: protocol.EquipSlotWeapon}, true
}
