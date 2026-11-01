// Package bank is the bank booth (docs/specs/skills.md, Bank): a player
// within Rules.BankDistanceTiles of the booth, out of combat and outside the
// PvP zone, opens their bank and deposits or withdraws one, five, all or a
// given amount of an item, or deposits their whole inventory or everything
// they wear. Every item stacks in one bank slot, and the bank holds at most
// Rules.BankSlots distinct items.
//
// As for trades, an accepted request works out the resulting inventory and
// bank, locks the player's inventory and hands both sides with the audit
// changes to a Committer, which saves them as one database transaction off
// the tick and reports back through Resolve. The change is applied to the
// player only when the save succeeded, so items can't be duplicated or lost.
package bank

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// Economy audit log reasons.
const (
	ReasonDeposit  = "bank_deposit"
	ReasonWithdraw = "bank_withdraw"
)

// Result is the outcome of a request, or why the bank closed.
type Result uint8

const (
	Ok            Result = 0
	TooFar        Result = 1
	InCombat      Result = 2
	InPvpZone     Result = 3
	NotOpen       Result = 4
	Busy          Result = 5 // another request is being saved
	BankFull      Result = 6
	InventoryFull Result = 7
	NotHeld       Result = 8 // no such slot, an empty slot or a bad amount
	SaveFailed    Result = 9
	Closed        Result = 10
)

var resultNames = [...]string{"ok", "too_far", "in_combat", "in_pvp_zone", "not_open", "busy",
	"bank_full", "inventory_full", "not_held", "save_failed", "closed"}

func (r Result) String() string {
	if int(r) < len(resultNames) {
		return resultNames[r]
	}
	return fmt.Sprintf("Result(%d)", uint8(r))
}

// Amounts other than a given positive quantity.
const (
	One  = 1
	Five = 5
	// All moves every one of the item.
	All = -1
)

// OpenCommand opens the bank.
type OpenCommand struct {
	Player world.PlayerID
}

func (c OpenCommand) Target() world.PlayerID { return c.Player }

// CloseCommand closes the bank.
type CloseCommand struct {
	Player world.PlayerID
}

func (c CloseCommand) Target() world.PlayerID { return c.Player }

// DepositCommand deposits Quantity (One, Five, All or any positive amount) of
// the item in inventory slot Slot, taken from every inventory slot holding
// it. An amount larger than the player holds deposits all of it.
type DepositCommand struct {
	Player   world.PlayerID
	Slot     int
	Quantity int
}

func (c DepositCommand) Target() world.PlayerID { return c.Player }

// WithdrawCommand withdraws Quantity (One, Five, All or any positive amount)
// of the item in bank slot Slot. As many as are banked and fit in the
// inventory are withdrawn.
type WithdrawCommand struct {
	Player   world.PlayerID
	Slot     int
	Quantity int
}

func (c WithdrawCommand) Target() world.PlayerID { return c.Player }

// DepositInventoryCommand deposits every item in the inventory that fits.
type DepositInventoryCommand struct {
	Player world.PlayerID
}

func (c DepositInventoryCommand) Target() world.PlayerID { return c.Player }

// DepositEquipmentCommand deposits every worn item that fits.
type DepositEquipmentCommand struct {
	Player world.PlayerID
}

func (c DepositEquipmentCommand) Target() world.PlayerID { return c.Player }

// View is the open bank as its player sees it.
type View struct {
	Slots    []world.Stack
	Capacity int
}

// Sink receives everything the player must be told; the session implements
// it. It is called on the tick goroutine and must not block.
type Sink interface {
	// BankState is sent when the bank opens and after each saved change.
	BankState(to world.PlayerID, v View)
	// BankResult is sent for every request, and when the bank closes.
	BankResult(to world.PlayerID, r Result)
}

// CommitID identifies a bank commit in flight.
type CommitID uint64

// CommitRequest is one deposit or withdrawal to save as one database
// transaction: both sides as they will be once it is applied, and the audit
// changes it makes.
type CommitRequest struct {
	ID     CommitID
	Player world.PlayerID
	Reason string // ReasonDeposit or ReasonWithdraw
	// Inv and Bank are the player's inventory and bank once applied.
	Inv  world.Inventory
	Bank world.Bank
	// Changes are, for each item moved, the change on the side it left
	// (ChangeRemove, or ChangeRemoveWorn for a worn item, or
	// ChangeBankRemove) followed by the change on the side it entered
	// (ChangeBankAdd or ChangeAdd).
	Changes []world.Change
}

// Committer saves accepted requests. The player's inventory is locked when
// it is called. It saves the request off the tick and then calls
// System.Resolve with req.ID. It is called on the tick goroutine and must not
// block.
type Committer interface {
	CommitBank(req CommitRequest)
}

type inflight struct {
	req    CommitRequest
	player *world.Player // as it was at commit time
}

type resolution struct {
	id CommitID
	ok bool
}

// System runs the bank booth.
type System struct {
	data      *gamedata.Data
	areas     *areas.Lookup
	booth     world.Vec2
	reach     float64 // metres
	clock     world.Clock
	sink      Sink
	committer Committer

	open   []world.PlayerID // in opening order
	isOpen map[world.PlayerID]bool

	nextID     CommitID
	commits    map[CommitID]*inflight
	committing map[world.PlayerID]CommitID

	rmu      sync.Mutex
	resolved []resolution // from Resolve, applied by Tick
}

// New creates the bank system for the booth at booth. Rules.BankSlots and
// Rules.BankDistanceTiles come from d, the PvP zone from l; tileSize is the
// length of a tile in metres (the collision grid's cell size). A player is in
// combat for Rules.LogoutCombatLock after they last dealt or took damage, by
// clock.
func New(d *gamedata.Data, l *areas.Lookup, booth world.Vec2, tileSize float64, clock world.Clock, sink Sink, c Committer) *System {
	return &System{
		data:       d,
		areas:      l,
		booth:      booth,
		reach:      float64(d.Rules.BankDistanceTiles) * tileSize,
		clock:      clock,
		sink:       sink,
		committer:  c,
		isOpen:     make(map[world.PlayerID]bool),
		commits:    make(map[CommitID]*inflight),
		committing: make(map[world.PlayerID]CommitID),
	}
}

func (s *System) Name() string { return "bank" }

// IsOpen reports whether p has the bank open.
func (s *System) IsOpen(p world.PlayerID) bool { return s.isOpen[p] }

// Committing reports whether a request of p's is being saved.
func (s *System) Committing(p world.PlayerID) bool {
	_, ok := s.committing[p]
	return ok
}

// Resolve reports the outcome of commit id: ok if the database transaction
// committed. The next Tick applies it. Safe for concurrent use; unknown or
// repeated ids are ignored.
func (s *System) Resolve(id CommitID, ok bool) {
	s.rmu.Lock()
	s.resolved = append(s.resolved, resolution{id, ok})
	s.rmu.Unlock()
}

// InReach reports whether pos is within the bank distance of the booth (the
// boundary counts as inside).
func (s *System) InReach(pos world.Vec2) bool {
	dx := float64(pos.X) - float64(s.booth.X)
	dy := float64(pos.Y) - float64(s.booth.Y)
	return dx*dx+dy*dy <= s.reach*s.reach
}

// check reports why p may not use the bank, if they may not.
func (s *System) check(p *world.Player) (Result, bool) {
	if s.areas.InPvP(p.Pos) {
		return InPvpZone, true
	}
	if !s.InReach(p.Pos) {
		return TooFar, true
	}
	if !p.LastCombat.IsZero() && s.clock.Now().Before(p.LastCombat.Add(s.data.Rules.LogoutCombatLock())) {
		return InCombat, true
	}
	return 0, false
}

// Handle applies a bank command for p and reports its result.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	switch c := cmd.(type) {
	case OpenCommand:
		s.sink.BankResult(p.ID, s.openBank(p))
	case CloseCommand:
		if s.isOpen[p.ID] {
			s.close(p.ID, Closed)
		}
	case DepositCommand:
		s.request(p, func(inv *world.Inventory, b *world.Bank) ([]world.Change, Result) {
			return s.deposit(inv, b, c.Slot, c.Quantity)
		}, ReasonDeposit)
	case WithdrawCommand:
		s.request(p, func(inv *world.Inventory, b *world.Bank) ([]world.Change, Result) {
			return s.withdraw(inv, b, c.Slot, c.Quantity)
		}, ReasonWithdraw)
	case DepositInventoryCommand:
		s.request(p, s.depositInventory, ReasonDeposit)
	case DepositEquipmentCommand:
		s.request(p, s.depositEquipment, ReasonDeposit)
	}
}

// Tick applies the commit results reported since the last Tick, then closes
// the bank of every player who left the world, entered the PvP zone or
// combat, or walked out of reach.
func (s *System) Tick(w *world.World, _ time.Duration) {
	s.rmu.Lock()
	resolved := s.resolved
	s.resolved = nil
	s.rmu.Unlock()
	for _, r := range resolved {
		s.finish(w, r.id, r.ok)
	}

	for i := 0; i < len(s.open); {
		id := s.open[i]
		p := w.Player(id)
		if p == nil {
			s.forget(id) // removes s.open[i]
			continue
		}
		if r, bad := s.check(p); bad {
			s.close(id, r) // removes s.open[i]
			continue
		}
		i++
	}
}

func (s *System) openBank(p *world.Player) Result {
	if r, bad := s.check(p); bad {
		return r
	}
	if !s.isOpen[p.ID] {
		s.isOpen[p.ID] = true
		s.open = append(s.open, p.ID)
	}
	s.sendState(p.ID, &p.Bank)
	return Ok
}

func (s *System) close(id world.PlayerID, r Result) {
	s.forget(id)
	s.sink.BankResult(id, r)
}

func (s *System) forget(id world.PlayerID) {
	delete(s.isOpen, id)
	for i, o := range s.open {
		if o == id {
			s.open = append(s.open[:i], s.open[i+1:]...)
			break
		}
	}
}

func (s *System) sendState(id world.PlayerID, b *world.Bank) {
	s.sink.BankState(id, View{Slots: append([]world.Stack(nil), b.Slots...), Capacity: s.data.Rules.BankSlots})
}

// move works out a request on copies of the inventory and bank. It returns
// the changes, or a Result other than Ok and leaves the copies to be dropped.
type move func(inv *world.Inventory, b *world.Bank) ([]world.Change, Result)

// request checks p may use the bank, works out the request and, if it moves
// anything, locks p's inventory and hands it to the Committer. A request that
// is refused changes nothing.
func (s *System) request(p *world.Player, m move, reason string) {
	if s.Committing(p.ID) {
		s.sink.BankResult(p.ID, Busy)
		return
	}
	if !s.isOpen[p.ID] {
		s.sink.BankResult(p.ID, NotOpen)
		return
	}
	if r, bad := s.check(p); bad {
		s.close(p.ID, r)
		return
	}
	if p.Inv.Locked() {
		s.sink.BankResult(p.ID, Busy) // another system is saving this inventory
		return
	}
	inv, b := p.Inv.Clone(), p.Bank.Clone()
	changes, r := m(&inv, &b)
	if r != Ok {
		s.sink.BankResult(p.ID, r)
		return
	}
	if len(changes) == 0 {
		s.sink.BankResult(p.ID, Ok) // nothing to move
		return
	}
	s.nextID++
	req := CommitRequest{ID: s.nextID, Player: p.ID, Reason: reason, Inv: inv, Bank: b, Changes: changes}
	p.Inv.SetLocked(true)
	s.commits[req.ID] = &inflight{req: req, player: p}
	s.committing[p.ID] = req.ID
	// The Committer gets its own copies, so nothing it does reaches what is
	// applied later.
	out := req
	out.Inv, out.Bank = req.Inv.Clone(), req.Bank.Clone()
	out.Changes = append([]world.Change(nil), req.Changes...)
	s.committer.CommitBank(out)
}

// finish applies the result of commit id: on success the resulting
// inventory and bank, an economy outbox event, the new bank state and Ok; on
// failure nothing but SaveFailed. The inventory is unlocked either way. A
// player who left the world since the commit is skipped.
func (s *System) finish(w *world.World, id CommitID, ok bool) {
	c := s.commits[id]
	if c == nil {
		return
	}
	delete(s.commits, id)
	delete(s.committing, c.req.Player)
	p := c.player
	p.Inv.SetLocked(false)
	if w.Player(c.req.Player) != p {
		return // left the world (and maybe came back as a new entity)
	}
	if !ok {
		s.sink.BankResult(p.ID, SaveFailed)
		return
	}
	p.Inv = c.req.Inv
	p.Inv.SetLocked(false)
	p.Bank = c.req.Bank
	p.RecordEconomy(c.req.Reason, c.req.Changes)
	s.sendState(p.ID, &p.Bank)
	s.sink.BankResult(p.ID, Ok)
}

// amount turns a requested quantity into how many to move out of have, or
// false if the quantity is not One, Five, All or positive.
func amount(qty, have int) (int, bool) {
	switch {
	case qty == All:
		return have, true
	case qty > 0:
		return min(qty, have), true
	}
	return 0, false
}

func (s *System) deposit(inv *world.Inventory, b *world.Bank, index, qty int) ([]world.Change, Result) {
	if index < 0 || index >= len(inv.Slots) || inv.Slots[index].Empty() {
		return nil, NotHeld
	}
	item := inv.Slots[index].Item
	n, ok := amount(qty, inv.Count(item))
	if !ok {
		return nil, NotHeld
	}
	return s.depositItem(inv, b, item, n)
}

// depositItem moves n of item from the inventory into the bank.
func (s *System) depositItem(inv *world.Inventory, b *world.Bank, item gamedata.ItemID, n int) ([]world.Change, Result) {
	added, err := b.Add(item, n, s.data.Rules.BankSlots)
	if err != nil {
		return nil, BankFull
	}
	removed, err := inv.Remove(item, n)
	if err != nil {
		return nil, NotHeld
	}
	return append(removed, added...), Ok
}

func (s *System) withdraw(inv *world.Inventory, b *world.Bank, index, qty int) ([]world.Change, Result) {
	if index < 0 || index >= len(b.Slots) {
		return nil, NotHeld
	}
	item := b.Slots[index].Item
	n, ok := amount(qty, b.Slots[index].Qty)
	if !ok {
		return nil, NotHeld
	}
	n = min(n, s.room(inv, item))
	if n == 0 {
		return nil, InventoryFull
	}
	removed, err := b.Remove(item, n)
	if err != nil {
		return nil, NotHeld
	}
	added, err := inv.Add(s.data, item, n)
	if err != nil {
		return nil, InventoryFull
	}
	return append(removed, added...), Ok
}

// room returns how many of item the inventory has room for.
func (s *System) room(inv *world.Inventory, item gamedata.ItemID) int {
	it, ok := s.data.Item(item)
	if !ok {
		return 0
	}
	if !it.Stackable() {
		return inv.FreeSlots()
	}
	if inv.Count(item) > 0 {
		for _, st := range inv.Slots {
			if st.Item == item {
				return math.MaxInt - st.Qty
			}
		}
	}
	if inv.FreeSlots() > 0 {
		return math.MaxInt
	}
	return 0
}

// depositInventory deposits every item in the inventory, in slot order, that
// fits in the bank. It is refused with BankFull only if items are held and
// none fit.
func (s *System) depositInventory(inv *world.Inventory, b *world.Bank) ([]world.Change, Result) {
	var changes []world.Change
	held := false
	for _, st := range inv.Slots {
		if st.Empty() {
			continue // includes slots emptied by an earlier deposit
		}
		held = true
		if ch, r := s.depositItem(inv, b, st.Item, inv.Count(st.Item)); r == Ok {
			changes = append(changes, ch...)
		}
	}
	if held && len(changes) == 0 {
		return nil, BankFull
	}
	return changes, Ok
}

// equipmentSlots is the order worn items are deposited in.
var equipmentSlots = [...]gamedata.Slot{gamedata.SlotWeapon, gamedata.SlotHead, gamedata.SlotBody, gamedata.SlotShield}

// depositEquipment deposits every worn item that fits in the bank. It is
// refused with BankFull only if items are worn and none fit.
func (s *System) depositEquipment(inv *world.Inventory, b *world.Bank) ([]world.Change, Result) {
	var changes []world.Change
	worn := false
	for _, slot := range equipmentSlots {
		item := inv.Equipment.Get(slot)
		if item == 0 {
			continue
		}
		worn = true
		added, err := b.Add(item, 1, s.data.Rules.BankSlots)
		if err != nil {
			continue
		}
		removed, err := inv.RemoveWorn(slot)
		if err != nil {
			return nil, NotHeld
		}
		changes = append(changes, removed...)
		changes = append(changes, added...)
	}
	if worn && len(changes) == 0 {
		return nil, BankFull
	}
	return changes, Ok
}
