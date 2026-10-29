package cardroom

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/cardtable"
	"github.com/cleiderg/lush/server/internal/world"
)

// Stakes in the shipped game data (PRD 13 Q2).
const (
	stake1 = 10 // table 1
	stake2 = 25 // table 2
)

func (r *room) player(t *testing.T, id world.PlayerID) *world.Player {
	t.Helper()
	p := r.world.Player(id)
	if p == nil {
		t.Fatalf("no player %d", id)
	}
	return p
}

func (r *room) setGold(t *testing.T, id world.PlayerID, gold int) {
	t.Helper()
	r.player(t, id).Inv.Gold = gold
}

func (r *room) golds(t *testing.T, ids ...world.PlayerID) []int {
	t.Helper()
	var out []int
	for _, id := range ids {
		out = append(out, r.player(t, id).Inv.Gold)
	}
	return out
}

func (r *room) locked(t *testing.T, ids ...world.PlayerID) []bool {
	t.Helper()
	var out []bool
	for _, id := range ids {
		out = append(out, r.player(t, id).Inv.Locked())
	}
	return out
}

// reasons returns the reasons of p's pending economy events.
func (r *room) reasons(t *testing.T, id world.PlayerID) []string {
	t.Helper()
	var out []string
	for _, e := range r.player(t, id).PendingEconomy() {
		out = append(out, e.Reason)
	}
	return out
}

// readyAt seats players at table id and readies them all, with the committer
// holding every request until the test resolves it. It returns after the tick
// in which the stake request is sent.
func (r *room) readyAt(t *testing.T, id TableID, players ...world.PlayerID) {
	t.Helper()
	r.commit.auto = false
	var cmds []world.Command
	for _, p := range players {
		cmds = append(cmds, SitCommand{Player: p, TableID: id})
	}
	r.run(cmds...)
	cmds = nil
	for _, p := range players {
		cmds = append(cmds, ReadyCommand{Player: p, Ready: true})
	}
	r.run(cmds...)
}

func repeatBool(b bool, n int) []bool {
	out := make([]bool, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// TestStakeCommittedBeforeStart checks that once every seated player is
// ready, one stake request holds every seated player's resulting inventory
// and gold change, their inventories stay locked and the game waits while it
// is in flight, and the game starts with every gold balance down by the stake
// only after Resolve reports success.
func TestStakeCommittedBeforeStart(t *testing.T) {
	r := newRoom(t)
	players := []world.PlayerID{1, 2, 3}
	r.setGold(t, 1, 100)
	r.setGold(t, 2, stake1) // exactly the stake is enough
	r.setGold(t, 3, 57)
	r.readyAt(t, 1, players...)

	if len(r.commit.reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(r.commit.reqs))
	}
	req := r.commit.reqs[0]
	if req.Kind != KindStake || req.Table != 1 || req.Pot != 3*stake1 || len(req.Sides) != 3 {
		t.Fatalf("request %+v, want one stake of %d from 3 players at table 1", req, 3*stake1)
	}
	for i, side := range req.Sides {
		before := []int{100, stake1, 57}[i]
		if side.Player != players[i] || side.Inv.Gold != before-stake1 {
			t.Errorf("side %d: player %d with %d gold, want player %d with %d", i, side.Player, side.Inv.Gold, players[i], before-stake1)
		}
		if want := []world.Change{{Kind: world.ChangeGold, Gold: -stake1}}; !reflect.DeepEqual(side.Changes, want) {
			t.Errorf("side %d changes %+v, want %+v", i, side.Changes, want)
		}
	}
	if !r.sys.Staking(1) || r.table(t, 1).InProgress() {
		t.Fatal("want table 1 staking and no game yet")
	}
	if got := r.locked(t, players...); !reflect.DeepEqual(got, repeatBool(true, 3)) {
		t.Errorf("locked %v, want all locked while in flight", got)
	}

	// While in flight: no new request, no game, no new seat or Ready change,
	// and gold still untouched.
	r.sink.reset()
	r.run(SitCommand{Player: 4, TableID: 1}, ReadyCommand{Player: 1, Ready: false})
	r.run()
	if len(r.commit.reqs) != 1 || r.table(t, 1).InProgress() {
		t.Fatalf("%d requests, in progress %v; want 1 and no game", len(r.commit.reqs), r.table(t, 1).InProgress())
	}
	if want := []sitResult{{4, 1, 0, InProgress}}; !reflect.DeepEqual(r.sink.sits, want) {
		t.Errorf("sits %v, want %v", r.sink.sits, want)
	}
	if want := []leaveResult{{1, 1, InProgress}}; !reflect.DeepEqual(r.sink.readies, want) {
		t.Errorf("readies %v, want %v", r.sink.readies, want)
	}
	if got := r.golds(t, players...); !reflect.DeepEqual(got, []int{100, stake1, 57}) {
		t.Errorf("gold %v before Resolve, want unchanged", got)
	}

	r.sys.Resolve(req.ID, true)
	r.run()
	if !r.table(t, 1).InProgress() || r.sys.Staking(1) || r.sys.Pot(1) != 3*stake1 {
		t.Fatalf("in progress %v, staking %v, pot %d; want a game with pot %d",
			r.table(t, 1).InProgress(), r.sys.Staking(1), r.sys.Pot(1), 3*stake1)
	}
	if got, want := r.golds(t, players...), []int{100 - stake1, 0, 57 - stake1}; !reflect.DeepEqual(got, want) {
		t.Errorf("gold %v, want %v", got, want)
	}
	if got := r.locked(t, players...); !reflect.DeepEqual(got, repeatBool(false, 3)) {
		t.Errorf("locked %v, want all unlocked", got)
	}
	for _, p := range players {
		if got := r.reasons(t, p); !reflect.DeepEqual(got, []string{ReasonStake}) {
			t.Errorf("player %d economy events %v, want [%s]", p, got, ReasonStake)
		}
	}
	want := []leaveResult{{1, 1, OK}, {2, 1, OK}, {3, 1, OK}}
	if !reflect.DeepEqual(r.sink.stakes, want) {
		t.Errorf("stake results %v, want %v", r.sink.stakes, want)
	}
	if !r.lastChange(t).snap.InProgress {
		t.Error("last published snapshot shows no game")
	}
}

// TestStakeFailed checks that when saving the stakes fails, the game does not
// start, nobody's gold changes, the inventories are unlocked and the Ready
// flags clear, so no request is sent again until everyone readies again.
func TestStakeFailed(t *testing.T) {
	r := newRoom(t)
	players := []world.PlayerID{1, 2}
	r.readyAt(t, 1, players...)
	if len(r.commit.reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(r.commit.reqs))
	}
	r.sink.reset()
	r.sys.Resolve(r.commit.reqs[0].ID, false)
	r.run()
	r.run()
	if r.table(t, 1).InProgress() || r.sys.Staking(1) || r.sys.Pot(1) != 0 {
		t.Fatal("game started or still staking after a failed save")
	}
	if got := r.golds(t, players...); !reflect.DeepEqual(got, []int{roomGold, roomGold}) {
		t.Errorf("gold %v, want unchanged", got)
	}
	if got := r.locked(t, players...); !reflect.DeepEqual(got, repeatBool(false, 2)) {
		t.Errorf("locked %v, want all unlocked", got)
	}
	for _, p := range players {
		if got := r.reasons(t, p); len(got) != 0 {
			t.Errorf("player %d economy events %v, want none", p, got)
		}
	}
	if want := []leaveResult{{1, 1, StakeFailed}, {2, 1, StakeFailed}}; !reflect.DeepEqual(r.sink.stakes, want) {
		t.Errorf("stake results %v, want %v", r.sink.stakes, want)
	}
	for _, seat := range r.lastChange(t).snap.Seats {
		if seat.Ready {
			t.Errorf("seat %+v still ready", seat)
		}
	}
	if len(r.commit.reqs) != 1 {
		t.Errorf("%d requests, want no retry without Ready", len(r.commit.reqs))
	}

	// Ready again: a new request, which can succeed.
	r.commit.auto = true
	r.run(ReadyCommand{Player: 1, Ready: true}, ReadyCommand{Player: 2, Ready: true})
	r.run()
	if !r.table(t, 1).InProgress() || len(r.commit.reqs) != 2 {
		t.Fatalf("in progress %v after %d requests, want a game after 2", r.table(t, 1).InProgress(), len(r.commit.reqs))
	}
}

// TestPotPaidToWinner checks that at game end one payout request credits the
// winner with the whole pot, forfeited stakes included, and that the
// winner's gold changes in memory only once Resolve reports success. A failed
// payout is owed again and retried.
func TestPotPaidToWinner(t *testing.T) {
	r := newRoom(t)
	r.readyAt(t, 2, 1, 2, 3)
	r.sys.Resolve(r.commit.reqs[0].ID, true)
	r.run()
	if !r.table(t, 2).InProgress() {
		t.Fatal("game did not start")
	}
	afterStake := roomGold - stake2

	// Players 3 and 2 leave mid-game: both forfeit and player 1 wins.
	r.run(LeaveCommand{Player: 3})
	r.run(LeaveCommand{Player: 2})
	if r.table(t, 2).InProgress() {
		t.Fatal("game still in progress")
	}
	if seat, ok := r.table(t, 2).Winner(); !ok || seat != 0 {
		t.Fatalf("winner seat %d (%v), want seat 0", seat, ok)
	}
	if len(r.commit.reqs) != 2 {
		t.Fatalf("%d requests, want the stake and one payout", len(r.commit.reqs))
	}
	pay := r.commit.reqs[1]
	pot := 3 * stake2
	if pay.Kind != KindPayout || pay.Table != 2 || pay.Pot != pot || len(pay.Sides) != 1 {
		t.Fatalf("payout %+v, want the pot of %d at table 2 to one player", pay, pot)
	}
	side := pay.Sides[0]
	if side.Player != 1 || side.Inv.Gold != afterStake+pot {
		t.Errorf("payout to player %d with %d gold, want player 1 with %d", side.Player, side.Inv.Gold, afterStake+pot)
	}
	if want := []world.Change{{Kind: world.ChangeGold, Gold: pot}}; !reflect.DeepEqual(side.Changes, want) {
		t.Errorf("payout changes %+v, want %+v", side.Changes, want)
	}
	if got := r.golds(t, 1); got[0] != afterStake || !r.player(t, 1).Inv.Locked() {
		t.Errorf("winner gold %d, locked %v before Resolve; want %d and locked", got[0], r.player(t, 1).Inv.Locked(), afterStake)
	}

	// A failed save changes nothing and is retried on the next Tick.
	r.sys.Resolve(pay.ID, false)
	r.run()
	if got := r.golds(t, 1); got[0] != afterStake {
		t.Errorf("winner gold %d after a failed payout, want %d", got[0], afterStake)
	}
	if len(r.commit.reqs) != 3 || r.commit.reqs[2].Kind != KindPayout || r.commit.reqs[2].Pot != pot {
		t.Fatalf("requests %+v, want the payout retried", r.commit.reqs)
	}

	r.sys.Resolve(r.commit.reqs[2].ID, true)
	r.run()
	if got, want := r.golds(t, 1, 2, 3), []int{afterStake + pot, afterStake, afterStake}; !reflect.DeepEqual(got, want) {
		t.Errorf("gold %v, want %v", got, want)
	}
	if r.player(t, 1).Inv.Locked() {
		t.Error("winner still locked")
	}
	if got, want := r.reasons(t, 1), []string{ReasonStake, ReasonPot}; !reflect.DeepEqual(got, want) {
		t.Errorf("winner economy events %v, want %v", got, want)
	}
	r.run()
	if len(r.commit.reqs) != 3 {
		t.Errorf("%d requests, want no more once paid", len(r.commit.reqs))
	}
}

// stubRoom is a System hosting one 2-seat table 1 with stake stake1 that
// plays stubRules, in a world whose players 1 and 2 carry roomGold, with a
// committer that saves every request straight away.
type stubRoom struct {
	room
	rules *stubRules
}

func newStubRoom(t *testing.T) *stubRoom {
	t.Helper()
	r := &stubRoom{room: room{sink: &fakeSink{}, commit: &fakeCommitter{auto: true}, world: world.New(world.DefaultConfig())},
		rules: &stubRules{}}
	tb, err := cardtable.New(2, 1000, r.rules)
	if err != nil {
		t.Fatal(err)
	}
	r.sys, err = New([]Entry{{ID: 1, Table: tb, Stake: stake1}}, r.sink, r.commit)
	if err != nil {
		t.Fatal(err)
	}
	r.commit.sys = r.sys
	if err := r.world.Register(r.sys); err != nil {
		t.Fatal(err)
	}
	for p := world.PlayerID(1); p <= 2; p++ {
		if err := r.world.AddPlayer(&world.Player{ID: p, Inv: world.Inventory{Gold: roomGold}}); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// TestPotPaidWhenWonByMove checks the payout when a move ends the game, the
// usual way a game is won.
func TestPotPaidWhenWonByMove(t *testing.T) {
	r := newStubRoom(t)
	r.start(t, 1, 2)
	r.rules.over, r.rules.winner = true, 1 // player 2's seat
	r.run(DrawCommand{Player: 1})
	r.run() // payout saved
	if got, want := r.golds(t, 1, 2), []int{roomGold - stake1, roomGold + stake1}; !reflect.DeepEqual(got, want) {
		t.Errorf("gold %v, want %v", got, want)
	}
	if n := len(r.commit.reqs); n != 2 || r.commit.reqs[1].Kind != KindPayout || r.commit.reqs[1].Sides[0].Player != 2 {
		t.Errorf("requests %+v, want the stake then a payout to player 2", r.commit.reqs)
	}
}

// TestRefundWhenStartFails checks that stakes saved for a game the rules
// module then refuses to start are given back, each in its own request.
func TestRefundWhenStartFails(t *testing.T) {
	r := newStubRoom(t)
	r.rules.startErr = errors.New("no deal")
	r.run(SitCommand{Player: 1, TableID: 1}, SitCommand{Player: 2, TableID: 1})
	r.run(ReadyCommand{Player: 1, Ready: true}, ReadyCommand{Player: 2, Ready: true})
	r.run() // stakes saved, start refused, refunds sent
	r.run() // refunds saved
	if r.table(t, 1).InProgress() {
		t.Fatal("game started")
	}
	if want := []leaveResult{{1, 1, StartFailed}, {2, 1, StartFailed}}; !reflect.DeepEqual(r.sink.stakes, want) {
		t.Errorf("stake results %v, want %v", r.sink.stakes, want)
	}
	var kinds []Kind
	for _, req := range r.commit.reqs {
		kinds = append(kinds, req.Kind)
	}
	if want := []Kind{KindStake, KindRefund, KindRefund}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("request kinds %v, want %v", kinds, want)
	}
	if got := r.golds(t, 1, 2); !reflect.DeepEqual(got, []int{roomGold, roomGold}) {
		t.Errorf("gold %v, want all refunded", got)
	}
	for _, p := range []world.PlayerID{1, 2} {
		if got, want := r.reasons(t, p), []string{ReasonStake, ReasonRefund}; !reflect.DeepEqual(got, want) {
			t.Errorf("player %d economy events %v, want %v", p, got, want)
		}
	}
}

// TestGoldRule checks the rule agreed in PRD 13 Q2: sitting and pressing
// Ready need at least the stake in carried gold, and at game start a seated
// player without it is unseated (NotEnoughGold) before any stake moves.
func TestGoldRule(t *testing.T) {
	tests := []struct {
		name string
		// Player 1's gold when sitting, pressing Ready and at the start.
		sitGold, readyGold, startGold int
		wantSit, wantReady            Result
		wantStake                     bool // a stake request is sent
		wantUnseated                  bool // player 1 is unseated at the start
	}{
		{"enough throughout", 50, 50, 50, OK, OK, true, false},
		{"exactly the stake", stake1, stake1, stake1, OK, OK, true, false},
		{"short when sitting", stake1 - 1, stake1, stake1, NotEnoughGold, NotSeated, false, false},
		{"short when ready", stake1, stake1 - 1, stake1, OK, NotEnoughGold, false, false},
		{"short at start", stake1, stake1, stake1 - 1, OK, OK, false, true},
		{"no gold at start", stake1, stake1, 0, OK, OK, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoom(t)
			r.commit.auto = false
			r.setGold(t, 1, tt.sitGold)
			r.run(SitCommand{Player: 1, TableID: 1}, SitCommand{Player: 2, TableID: 1})
			if got := r.sink.sits[0]; got.to != 1 || got.r != tt.wantSit {
				t.Fatalf("sit result %+v, want %v", got, tt.wantSit)
			}
			r.run(ReadyCommand{Player: 2, Ready: true})
			r.sink.reset()
			// Player 1 presses Ready, then their gold changes before the
			// card room's Tick of the same world tick.
			r.setGold(t, 1, tt.readyGold)
			r.sys.Handle(ReadyCommand{Player: 1, Ready: true}, r.player(t, 1))
			r.setGold(t, 1, tt.startGold)
			r.run()
			if len(r.sink.readies) != 1 || r.sink.readies[0].r != tt.wantReady {
				t.Fatalf("ready results %v, want %v", r.sink.readies, tt.wantReady)
			}
			if got := len(r.commit.reqs) == 1; got != tt.wantStake {
				t.Errorf("stake request sent %v, want %v", got, tt.wantStake)
			}
			if r.table(t, 1).InProgress() {
				t.Error("game started without saved stakes")
			}
			if !tt.wantUnseated {
				if len(r.sink.stakes) != 0 {
					t.Errorf("stake results %v, want none", r.sink.stakes)
				}
				return
			}
			if _, seated := r.sys.TableOf(1); seated {
				t.Error("player 1 still seated")
			}
			if want := []leaveResult{{1, 1, NotEnoughGold}}; !reflect.DeepEqual(r.sink.stakes, want) {
				t.Errorf("stake results %v, want %v", r.sink.stakes, want)
			}
			if got := r.golds(t, 1, 2); !reflect.DeepEqual(got, []int{tt.startGold, roomGold}) {
				t.Errorf("gold %v, want unchanged", got)
			}
			if r.lastChange(t).snap.Seats[1].Ready {
				t.Error("player 2 still ready after the group changed")
			}
		})
	}
}

// TestStakeWaitsForLockedInventory checks that no stake request is sent
// while a seated player's inventory is locked (for example by a trade being
// saved), and that it goes out once the lock is released.
func TestStakeWaitsForLockedInventory(t *testing.T) {
	r := newRoom(t)
	r.commit.auto = false
	r.run(SitCommand{Player: 1, TableID: 1}, SitCommand{Player: 2, TableID: 1})
	r.player(t, 2).Inv.SetLocked(true)
	r.run(ReadyCommand{Player: 1, Ready: true}, ReadyCommand{Player: 2, Ready: true})
	r.run()
	if len(r.commit.reqs) != 0 {
		t.Fatalf("%d requests while an inventory is locked, want 0", len(r.commit.reqs))
	}
	r.player(t, 2).Inv.SetLocked(false)
	r.run()
	if len(r.commit.reqs) != 1 {
		t.Fatalf("%d requests after unlock, want 1", len(r.commit.reqs))
	}
}

// TestLeaveWhileStaking checks that a leave sent while the stakes are being
// saved is applied once they are: after a successful save it forfeits, so
// the other player wins the whole pot.
func TestLeaveWhileStaking(t *testing.T) {
	r := newRoom(t)
	r.readyAt(t, 1, 1, 2)
	r.sink.reset()
	r.run(LeaveCommand{Player: 2})
	if len(r.sink.leaves) != 0 {
		t.Fatalf("leave results %v while staking, want none yet", r.sink.leaves)
	}
	r.commit.auto = true
	r.sys.Resolve(r.commit.reqs[0].ID, true)
	r.run() // stakes saved, game starts, player 2 forfeits
	if want := []leaveResult{{2, 1, OK}}; !reflect.DeepEqual(r.sink.leaves, want) {
		t.Errorf("leave results %v, want %v", r.sink.leaves, want)
	}
	r.run() // payout saved
	if got, want := r.golds(t, 1, 2), []int{roomGold + stake1, roomGold - stake1}; !reflect.DeepEqual(got, want) {
		t.Errorf("gold %v, want %v", got, want)
	}
}
