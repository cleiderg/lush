package groups

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func newSys(t *testing.T) (*System, *world.FakeClock) {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	clk := world.NewFakeClock(t0)
	return New(&d.Groups, clk), clk
}

// found makes a group led by player 1.
func found(t *testing.T, s *System) GroupID {
	t.Helper()
	r := s.Found(1, 500, "Anteaters", 1)
	if r.Code != OK {
		t.Fatalf("found: %v", r.Code)
	}
	return r.GroupID
}

// join adds players lo..hi as Recruits.
func join(t *testing.T, s *System, id GroupID, lo, hi world.PlayerID) {
	t.Helper()
	for p := lo; p <= hi; p++ {
		if c := s.Invite(1, p); c != OK {
			t.Fatalf("invite %d: %v", p, c)
		}
		if c := s.Accept(p, id); c != OK {
			t.Fatalf("accept %d: %v", p, c)
		}
	}
}

func rankOf(s *System, id GroupID, p world.PlayerID) gamedata.Rank {
	g, _ := s.Group(id)
	for _, m := range g.Members {
		if m.Player == p {
			return m.Rank
		}
	}
	return ""
}

func TestFound(t *testing.T) {
	tests := []struct {
		name   string
		gold   int
		gname  string
		banner int
		pre    func(s *System)
		want   Code
	}{
		{"ok", 500, "Anteaters", 1, nil, OK},
		{"exact gold and spaces", 500, "Zot Zot 99", 12, nil, OK},
		{"not enough gold", 499, "Anteaters", 1, nil, NotEnoughGold},
		{"too short", 500, "ab", 1, nil, BadName},
		{"too long", 500, strings.Repeat("a", 21), 1, nil, BadName},
		{"bad characters", 500, "Ant-eaters", 1, nil, BadName},
		{"blocked word", 500, "Fuck Club", 1, nil, BadName},
		{"banner zero", 500, "Anteaters", 0, nil, BadBanner},
		{"banner 13", 500, "Anteaters", 13, nil, BadBanner},
		{"taken", 500, "Anteaters", 1, func(s *System) { s.Found(2, 500, "Anteaters", 2) }, NameTaken},
		{"taken other case", 500, "ANTEATERS", 1, func(s *System) { s.Found(2, 500, "anteaters", 2) }, NameTaken},
		{"already in group", 500, "Anteaters", 1, func(s *System) { s.Found(1, 500, "Other Group", 2) }, AlreadyInGroup},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newSys(t)
			if tt.pre != nil {
				tt.pre(s)
			}
			r := s.Found(1, tt.gold, tt.gname, tt.banner)
			if r.Code != tt.want {
				t.Fatalf("code %v, want %v", r.Code, tt.want)
			}
			if r.Code != OK {
				return
			}
			if r.Cost != 500 {
				t.Errorf("cost %d, want 500", r.Cost)
			}
			if rankOf(s, r.GroupID, 1) != gamedata.RankLeader {
				t.Errorf("founder is not Leader")
			}
		})
	}
}

func TestInviteAndJoin(t *testing.T) {
	s, _ := newSys(t)
	id := found(t, s)
	join(t, s, id, 2, 4)
	s.Promote(1, 2) // recruit -> member
	s.Promote(1, 3)
	s.Promote(1, 3) // member -> officer
	if rankOf(s, id, 3) != gamedata.RankOfficer || rankOf(s, id, 4) != gamedata.RankRecruit {
		t.Fatal("setup ranks wrong")
	}
	if rankOf(s, id, 2) != gamedata.RankMember {
		t.Fatal("setup member wrong")
	}

	if c := s.Invite(2, 5); c != NotAllowed {
		t.Errorf("member invite = %v", c)
	}
	if c := s.Invite(4, 5); c != NotAllowed {
		t.Errorf("recruit invite = %v", c)
	}
	if c := s.Invite(3, 5); c != OK {
		t.Errorf("officer invite = %v", c)
	}
	if c := s.Invite(99, 5); c != NotInGroup {
		t.Errorf("outsider invite = %v", c)
	}
	if c := s.Invite(1, 2); c != AlreadyInGroup {
		t.Errorf("invite member = %v", c)
	}
	if c := s.Accept(6, id); c != NotInvited {
		t.Errorf("uninvited accept = %v", c)
	}
	if c := s.Accept(5, id); c != OK {
		t.Fatalf("accept = %v", c)
	}
	if rankOf(s, id, 5) != gamedata.RankRecruit {
		t.Error("new member is not a Recruit")
	}
	// one group at a time
	other := s.Found(10, 500, "Other", 2).GroupID
	s.Invite(10, 5)
	if c := s.Accept(5, other); c != NotInvited && c != AlreadyInGroup {
		t.Errorf("second group = %v", c)
	}
	if c := s.Invite(10, 5); c != AlreadyInGroup {
		t.Errorf("invite player in a group = %v", c)
	}
	if g, _ := s.GroupOf(5); g != id {
		t.Error("player moved groups")
	}
}

func TestMaxMembers(t *testing.T) {
	s, _ := newSys(t)
	id := found(t, s)
	join(t, s, id, 2, 30)
	if g, _ := s.Group(id); len(g.Members) != 30 {
		t.Fatalf("members %d", len(g.Members))
	}
	if c := s.Invite(1, 31); c != GroupFull {
		t.Errorf("invite at 30 = %v", c)
	}
	// An invite sent while there was room can't be accepted once full.
	s.Leave(30)
	s.Invite(1, 31)
	s.Invite(1, 32)
	s.Accept(31, id)
	if c := s.Accept(32, id); c != GroupFull {
		t.Errorf("accept at 30 = %v", c)
	}
}

func TestRankActions(t *testing.T) {
	// players: 1 leader, 2 officer, 3 officer, 4 member, 5 recruit
	setup := func(t *testing.T) (*System, GroupID) {
		s, _ := newSys(t)
		id := found(t, s)
		join(t, s, id, 2, 5)
		for _, p := range []world.PlayerID{2, 3} {
			s.Promote(1, p)
			s.Promote(1, p)
		}
		s.Promote(1, 4)
		return s, id
	}
	tests := []struct {
		name   string
		act    func(s *System) Code
		want   Code
		target world.PlayerID
		after  gamedata.Rank // "" means removed
	}{
		{"leader promotes recruit", func(s *System) Code { return s.Promote(1, 5) }, OK, 5, gamedata.RankMember},
		{"leader promotes member to officer", func(s *System) Code { return s.Promote(1, 4) }, OK, 4, gamedata.RankOfficer},
		{"officer promotes recruit", func(s *System) Code { return s.Promote(2, 5) }, OK, 5, gamedata.RankMember},
		{"officer can't promote member to officer", func(s *System) Code { return s.Promote(2, 4) }, RankTooLow, 4, gamedata.RankMember},
		{"officer can't promote officer", func(s *System) Code { return s.Promote(2, 3) }, RankTooLow, 3, gamedata.RankOfficer},
		{"officer can't promote leader", func(s *System) Code { return s.Promote(2, 1) }, RankTooLow, 1, gamedata.RankLeader},
		{"member can't promote", func(s *System) Code { return s.Promote(4, 5) }, NotAllowed, 5, gamedata.RankRecruit},
		{"leader demotes officer", func(s *System) Code { return s.Demote(1, 2) }, OK, 2, gamedata.RankMember},
		{"officer demotes member", func(s *System) Code { return s.Demote(2, 4) }, OK, 4, gamedata.RankRecruit},
		{"officer can't demote officer", func(s *System) Code { return s.Demote(2, 3) }, RankTooLow, 3, gamedata.RankOfficer},
		{"can't demote recruit", func(s *System) Code { return s.Demote(1, 5) }, BadTarget, 5, gamedata.RankRecruit},
		{"member can't demote", func(s *System) Code { return s.Demote(4, 5) }, NotAllowed, 5, gamedata.RankRecruit},
		{"can't promote self", func(s *System) Code { return s.Promote(2, 2) }, SelfNotAllowed, 2, gamedata.RankOfficer},
		{"leader kicks officer", func(s *System) Code { return s.Kick(1, 2) }, OK, 2, ""},
		{"officer kicks recruit", func(s *System) Code { return s.Kick(2, 5) }, OK, 5, ""},
		{"officer can't kick officer", func(s *System) Code { return s.Kick(2, 3) }, RankTooLow, 3, gamedata.RankOfficer},
		{"officer can't kick leader", func(s *System) Code { return s.Kick(2, 1) }, RankTooLow, 1, gamedata.RankLeader},
		{"member can't kick", func(s *System) Code { return s.Kick(4, 5) }, NotAllowed, 5, gamedata.RankRecruit},
		{"kick outsider", func(s *System) Code { return s.Kick(1, 99) }, NoSuchMember, 99, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, id := setup(t)
			if got := tt.act(s); got != tt.want {
				t.Fatalf("code %v, want %v", got, tt.want)
			}
			if got := rankOf(s, id, tt.target); got != tt.after {
				t.Errorf("target rank %q, want %q", got, tt.after)
			}
		})
	}
}

func TestCan(t *testing.T) {
	// players: 1 leader, 2 officer, 3 member, 4 recruit
	s, _ := newSys(t)
	id := found(t, s)
	join(t, s, id, 2, 4)
	s.Promote(1, 2)
	s.Promote(1, 2)
	s.Promote(1, 3)

	type cell struct {
		perm string
		want [4]bool // leader, officer, member, recruit
	}
	table := func(keys, vaultRoom, withdraw [4]bool) []cell {
		return []cell{
			{gamedata.PermInvite, [4]bool{true, true, false, false}},
			{gamedata.PermPromoteDemote, [4]bool{true, true, false, false}},
			{gamedata.PermKick, [4]bool{true, true, false, false}},
			{gamedata.PermKeys, keys},
			{gamedata.PermVaultRoom, vaultRoom},
			{gamedata.PermWithdraw, withdraw},
			{gamedata.PermDoorLog, [4]bool{true, true, false, false}},
			{gamedata.PermHallAndSiege, [4]bool{true, false, false, false}},
		}
	}
	check := func(label string, cells []cell) {
		t.Helper()
		for _, c := range cells {
			for i, p := range []world.PlayerID{1, 2, 3, 4} {
				if got := s.Can(id, p, c.perm); got != c.want[i] {
					t.Errorf("%s: Can(player %d, %s) = %v, want %v", label, p, c.perm, got, c.want[i])
				}
			}
		}
	}
	check("defaults", table([4]bool{true, true, false, false}, [4]bool{true, true, false, false}, [4]bool{true, false, false, false}))

	for _, tc := range []struct {
		perm string
		rank gamedata.Rank
	}{{gamedata.PermKeys, gamedata.RankMember}, {gamedata.PermVaultRoom, gamedata.RankMember}, {gamedata.PermWithdraw, gamedata.RankOfficer}} {
		if c := s.SetToggle(1, tc.perm, tc.rank, true); c != OK {
			t.Fatalf("toggle %s/%s = %v", tc.perm, tc.rank, c)
		}
	}
	check("all on", table([4]bool{true, true, true, false}, [4]bool{true, true, true, false}, [4]bool{true, true, false, false}))

	s.SetToggle(1, gamedata.PermKeys, gamedata.RankMember, false)
	check("keys off", table([4]bool{true, true, false, false}, [4]bool{true, true, true, false}, [4]bool{true, true, false, false}))

	// Toggles outside "set by leader" cells, and non-leaders, are refused.
	for _, tc := range []struct {
		actor world.PlayerID
		perm  string
		rank  gamedata.Rank
		want  Code
	}{
		{1, gamedata.PermKeys, gamedata.RankRecruit, BadTarget},
		{1, gamedata.PermWithdraw, gamedata.RankMember, BadTarget},
		{1, gamedata.PermInvite, gamedata.RankMember, BadTarget},
		{1, gamedata.PermKeys, gamedata.RankOfficer, BadTarget},
		{2, gamedata.PermKeys, gamedata.RankMember, NotAllowed},
	} {
		if c := s.SetToggle(tc.actor, tc.perm, tc.rank, true); c != tc.want {
			t.Errorf("toggle %d %s/%s = %v, want %v", tc.actor, tc.perm, tc.rank, c, tc.want)
		}
	}
	if s.Can(id, 99, gamedata.PermInvite) || s.Can(999, 1, gamedata.PermInvite) {
		t.Error("outsider or unknown group has a permission")
	}
}

func TestLeaveCooldown(t *testing.T) {
	for _, how := range []string{"leave", "kick"} {
		t.Run(how, func(t *testing.T) {
			s, clk := newSys(t)
			id := found(t, s)
			join(t, s, id, 2, 2)
			if how == "leave" {
				s.Leave(2)
			} else {
				s.Kick(1, 2)
			}
			if _, in := s.GroupOf(2); in {
				t.Fatal("still in group")
			}
			if c := s.Invite(1, 2); c != OnCooldown {
				t.Errorf("invite during cooldown = %v", c)
			}
			if r := s.Found(2, 500, "New Group", 3); r.Code != OnCooldown {
				t.Errorf("found during cooldown = %v", r.Code)
			}
			clk.Advance(24*time.Hour - time.Second)
			if c := s.Invite(1, 2); c != OnCooldown {
				t.Errorf("invite at 23h59m59s = %v", c)
			}
			clk.Advance(time.Second)
			if c := s.Invite(1, 2); c != OK {
				t.Errorf("invite at 24h = %v", c)
			}
			if c := s.Accept(2, id); c != OK {
				t.Errorf("accept at 24h = %v", c)
			}
		})
	}
	t.Run("invite sent before leaving can't be used in cooldown", func(t *testing.T) {
		s, _ := newSys(t)
		id := found(t, s)
		join(t, s, id, 2, 2)
		s.Invite(1, 3)
		s.Accept(3, id)
		s.Leave(3)
		s.Invite(1, 3) // refused
		if c := s.Accept(3, id); c != NotInvited {
			t.Errorf("accept = %v", c)
		}
	})
}

func TestLeaderCannotLeave(t *testing.T) {
	s, _ := newSys(t)
	found(t, s)
	if c := s.Leave(1); c != CannotLeave {
		t.Errorf("leader leave = %v", c)
	}
}

func TestHandover(t *testing.T) {
	s, _ := newSys(t)
	id := found(t, s)
	join(t, s, id, 2, 3)
	s.Promote(1, 2)
	s.Promote(1, 2)
	if r := s.Handover(1, 3); r.Code != NotOfficer {
		t.Errorf("handover to recruit = %v", r.Code)
	}
	if r := s.Handover(2, 1); r.Code != NotAllowed {
		t.Errorf("officer handover = %v", r.Code)
	}
	r := s.Handover(1, 2)
	if r.Code != OK || r.Old != 1 || r.New != 2 {
		t.Fatalf("handover = %+v", r)
	}
	if rankOf(s, id, 2) != gamedata.RankLeader || rankOf(s, id, 1) != gamedata.RankOfficer {
		t.Error("ranks not swapped")
	}
}

func TestInactiveLeader(t *testing.T) {
	day := 24 * time.Hour
	// players: 1 leader, then officers promoted at different times.
	build := func(t *testing.T) (*System, *world.FakeClock, GroupID) {
		s, clk := newSys(t)
		id := found(t, s)
		join(t, s, id, 2, 4)
		clk.Advance(day)
		// 3 becomes officer first, then 2 later.
		s.Promote(1, 3)
		s.Promote(1, 3)
		clk.Advance(day)
		s.Promote(1, 2)
		s.Promote(1, 2)
		return s, clk, id
	}

	t.Run("longest-serving officer", func(t *testing.T) {
		s, clk, id := build(t)
		clk.Advance(20 * day)
		r := s.CheckInactiveLeader(id, clk.Now().Add(-15*day))
		if r.Code != OK || r.New != 3 || r.Old != 1 {
			t.Fatalf("result %+v", r)
		}
		if rankOf(s, id, 3) != gamedata.RankLeader || rankOf(s, id, 1) != gamedata.RankOfficer {
			t.Error("ranks wrong")
		}
	})
	t.Run("exactly 14 days is not inactive", func(t *testing.T) {
		s, clk, id := build(t)
		if r := s.CheckInactiveLeader(id, clk.Now().Add(-14*day)); r.Code != NotInactive {
			t.Errorf("code %v", r.Code)
		}
		if rankOf(s, id, 1) != gamedata.RankLeader {
			t.Error("leader changed")
		}
	})
	t.Run("tie goes to earliest joined", func(t *testing.T) {
		s, clk := newSys(t)
		id := found(t, s)
		join(t, s, id, 2, 3) // 2 joins before 3
		// Promote both to officer within the same instant.
		for _, p := range []world.PlayerID{3, 2} {
			s.Promote(1, p)
			s.Promote(1, p)
		}
		r := s.CheckInactiveLeader(id, clk.Now().Add(-15*day))
		if r.Code != OK || r.New != 2 {
			t.Fatalf("result %+v", r)
		}
	})
	t.Run("no officer", func(t *testing.T) {
		s, clk := newSys(t)
		id := found(t, s)
		join(t, s, id, 2, 2)
		if r := s.CheckInactiveLeader(id, clk.Now().Add(-30*day)); r.Code != NoSuccessor {
			t.Errorf("code %v", r.Code)
		}
	})
	t.Run("unknown group", func(t *testing.T) {
		s, clk := newSys(t)
		if r := s.CheckInactiveLeader(7, clk.Now()); r.Code != NoSuchGroup {
			t.Errorf("code %v", r.Code)
		}
	})
}

func TestDisband(t *testing.T) {
	s, clk := newSys(t)
	id := found(t, s)
	join(t, s, id, 2, 3)
	s.Promote(1, 2)
	s.Promote(1, 2)
	s.SetVault(id, 1234)
	s.SetHall(id, 4)
	s.Invite(1, 9)

	if r := s.Disband(2); r.Code != NotAllowed {
		t.Errorf("officer disband = %v", r.Code)
	}
	if r := s.Disband(99); r.Code != NotInGroup {
		t.Errorf("outsider disband = %v", r.Code)
	}
	r := s.Disband(1)
	if r.Code != OK || r.Leader != 1 || r.VaultGold != 1234 || r.Hall != 4 || r.GroupID != id {
		t.Fatalf("result %+v", r)
	}
	if len(r.Members) != 3 {
		t.Errorf("members %v", r.Members)
	}
	if _, ok := s.Group(id); ok {
		t.Error("group still exists")
	}
	for p := world.PlayerID(1); p <= 3; p++ {
		if _, in := s.GroupOf(p); in {
			t.Errorf("player %d still in a group", p)
		}
	}
	if c := s.Accept(9, id); c != NoSuchGroup {
		t.Errorf("accept after disband = %v", c)
	}
	// The name is free again, and members can found straight away.
	if r := s.Found(2, 500, "anteaters", 1); r.Code != OK {
		t.Errorf("refound = %v", r.Code)
	}
	_ = clk
}

// The rules package stays pure: no store, transport or api imports.
func TestImportsStayPure(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p := imp.Path.Value
			for _, bad := range []string{"/store", "/transport", "/api", "/session", "database/sql", "pgx", "net/http"} {
				if strings.Contains(p, bad) {
					t.Errorf("%s imports %s", e.Name(), p)
				}
			}
		}
	}
}
