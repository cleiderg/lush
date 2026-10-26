package world

import (
	"fmt"
	"time"
)

// NPCID identifies an NPC entity. NPC ids come from Config's NPC id range.
type NPCID uint64

// NPCTypeID is a row of the NPC table (systems/gamedata).
type NPCTypeID uint16

// NPC is a non-player entity. Only the tick may mutate it.
type NPC struct {
	ID     NPCID // assigned by AddNPC
	Type   NPCTypeID
	Pos    Vec2
	Home   Vec2 // spawn spot; the NPC walks back here when leashed
	HP     int  // current hit points
	Facing float32
	Anim   Anim

	// Goal is where the NPC is walking; set by systems and consumed by the
	// NPC system's movement step.
	Goal    Vec2
	HasGoal bool
	// Returning is set while the NPC walks home after going past its leash.
	Returning bool
	// LastCombat is when the NPC last dealt or took damage; zero if never.
	LastCombat time.Time
}

// MarkCombat records that the NPC dealt or took damage at now.
func (n *NPC) MarkCombat(now time.Time) { n.LastCombat = now }

func (w *World) isNPCID(id uint64) bool {
	return id >= uint64(w.cfg.NPCIDMin) && id <= uint64(w.cfg.NPCIDMax)
}

// AddNPC assigns n a free id from the NPC id range and inserts it.
// Ids are handed out in a rotating order so a removed NPC's id is not reused
// straight away. It fails when every id in the range is in use.
// Tick-goroutine only (or before the loop starts).
func (w *World) AddNPC(n *NPC) (NPCID, error) {
	min, max := w.cfg.NPCIDMin, w.cfg.NPCIDMax
	if min > max {
		return 0, fmt.Errorf("add npc: empty NPC id range %d..%d", min, max)
	}
	size := uint64(max-min) + 1
	if uint64(len(w.npcs)) >= size {
		return 0, fmt.Errorf("add npc: NPC id range %d..%d is full", min, max)
	}
	id := w.nextNPC
	for {
		if id < min || id > max {
			id = min
		}
		if _, used := w.npcs[id]; !used {
			break
		}
		id++
	}
	if id == max {
		w.nextNPC = min
	} else {
		w.nextNPC = id + 1
	}
	n.ID = id
	w.npcs[id] = n
	w.npcOrder = append(w.npcOrder, n)
	return id, nil
}

// RemoveNPC deletes an NPC. Tick-goroutine only.
func (w *World) RemoveNPC(id NPCID) {
	n, ok := w.npcs[id]
	if !ok {
		return
	}
	delete(w.npcs, id)
	for i, q := range w.npcOrder {
		if q == n {
			w.npcOrder = append(w.npcOrder[:i], w.npcOrder[i+1:]...)
			break
		}
	}
}

// NPC returns the NPC with id, or nil.
func (w *World) NPC(id NPCID) *NPC { return w.npcs[id] }

// NPCs returns NPCs in insertion order. The slice must not be modified.
func (w *World) NPCs() []*NPC { return w.npcOrder }
