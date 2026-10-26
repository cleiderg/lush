package world

// EconomyEvent is one change to a player's holdings waiting to be saved: why
// it happened, the changes it made for the economy audit log, and the
// player's inventory right after it, so the session can save the holdings
// event by event.
type EconomyEvent struct {
	Reason  string
	Changes []Change
	Inv     Inventory // a copy; later edits to the player's inventory don't reach it
}

// Clone returns a copy of inv that shares no memory with it.
func (inv *Inventory) Clone() Inventory {
	c := *inv
	c.Slots = append([]Stack(nil), inv.Slots...)
	return c
}

// RecordEconomy appends an event to the player's economy outbox with a copy
// of their inventory as it is now. Systems call it on the tick right after
// changing the inventory. Tick-goroutine only.
func (p *Player) RecordEconomy(reason string, changes []Change) {
	p.outbox = append(p.outbox, EconomyEvent{
		Reason:  reason,
		Changes: append([]Change(nil), changes...),
		Inv:     p.Inv.Clone(),
	})
}

// PendingEconomy returns the events in the outbox, oldest first, without
// removing them. The slice must not be modified.
func (p *Player) PendingEconomy() []EconomyEvent { return p.outbox }

// DrainEconomy removes and returns every event in the outbox, oldest first.
// The session writes them to the database off the tick. Tick-goroutine only.
func (p *Player) DrainEconomy() []EconomyEvent {
	out := p.outbox
	p.outbox = nil
	return out
}
