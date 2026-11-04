// Package groups holds the rules for player groups (docs/specs/groups.md,
// D-054): founding, invitations, ranks, leaving, handover and disbanding.
//
// It is pure game logic. It does no database work: every method changes the
// in-memory state and returns a typed result that the session layer commits
// (gold charged, vault gold returned, hall released). It imports no store,
// transport or api package and never reads the wall clock.
package groups

import (
	"sort"
	"strings"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// GroupID identifies a group.
type GroupID int64

// HallID identifies a group hall. Zero means the group holds no hall.
type HallID int

// Code is the outcome of a group action.
type Code int

const (
	OK Code = iota
	NotEnoughGold
	BadName
	NameTaken
	BadBanner
	AlreadyInGroup
	NotInGroup
	NoSuchGroup
	NoSuchMember
	NotAllowed  // the actor's rank lacks the permission
	RankTooLow  // the target is not below the actor's rank
	GroupFull   // 30 members
	OnCooldown  // left or was kicked less than 24 h ago
	NotInvited  // no pending invitation
	CannotLeave // the leader must hand over or disband first
	BadTarget   // for example promoting past the actor's rank, or an unknown toggle
	NotOfficer  // handover target is not an Officer
	NotInactive // the leader logged in recently
	NoSuccessor // no Officer to take over
	SelfNotAllowed
)

// Member is one member of a group.
type Member struct {
	Player    world.PlayerID
	Rank      gamedata.Rank
	Joined    time.Time
	RankSince time.Time // when the member took their current rank
}

// Group is one player group.
type Group struct {
	ID      GroupID
	Name    string
	Banner  int
	Members []Member // in join order
	// Vault is the vault gold. The session layer keeps it up to date.
	Vault int
	// Hall is the hall the group holds, or 0.
	Hall HallID
	// Toggles are the leader's switches, keyed "permission/rank". Absent means off.
	Toggles map[string]bool
}

func (g *Group) member(p world.PlayerID) *Member {
	for i := range g.Members {
		if g.Members[i].Player == p {
			return &g.Members[i]
		}
	}
	return nil
}

func (g *Group) leader() *Member {
	for i := range g.Members {
		if g.Members[i].Rank == gamedata.RankLeader {
			return &g.Members[i]
		}
	}
	return nil
}

// Can reports whether a member holds a permission, following the rank table
// and the leader's toggles.
func (s *System) Can(id GroupID, p world.PlayerID, perm string) bool {
	g := s.groups[id]
	if g == nil {
		return false
	}
	m := g.member(p)
	return m != nil && s.can(g, m.Rank, perm)
}

func (s *System) can(g *Group, r gamedata.Rank, perm string) bool {
	for _, row := range s.rules.Permissions {
		if row.Key != perm {
			continue
		}
		switch c := row.Cell(r); c.Mode {
		case gamedata.CellYes:
			return true
		case gamedata.CellSetByLeader:
			if v, ok := g.Toggles[toggleKey(perm, r)]; ok {
				return v
			}
			return c.On
		}
		return false
	}
	return false
}

func toggleKey(perm string, r gamedata.Rank) string { return perm + "/" + string(r) }

func rankLevel(r gamedata.Rank) int {
	for i, x := range gamedata.Ranks {
		if x == r {
			return len(gamedata.Ranks) - i
		}
	}
	return 0
}

// System holds every group. Only the tick (or the session layer on the tick)
// calls it.
type System struct {
	rules    *gamedata.Groups
	clock    world.Clock
	groups   map[GroupID]*Group
	byPlayer map[world.PlayerID]GroupID
	names    map[string]GroupID // lower-case name -> group
	cooldown map[world.PlayerID]time.Time
	invites  map[GroupID]map[world.PlayerID]bool
	nextID   GroupID
}

// New returns an empty System.
func New(rules *gamedata.Groups, clock world.Clock) *System {
	return &System{
		rules:    rules,
		clock:    clock,
		groups:   map[GroupID]*Group{},
		byPlayer: map[world.PlayerID]GroupID{},
		names:    map[string]GroupID{},
		cooldown: map[world.PlayerID]time.Time{},
		invites:  map[GroupID]map[world.PlayerID]bool{},
		nextID:   1,
	}
}

// Group returns a copy of a group.
func (s *System) Group(id GroupID) (Group, bool) {
	g := s.groups[id]
	if g == nil {
		return Group{}, false
	}
	c := *g
	c.Members = append([]Member(nil), g.Members...)
	c.Toggles = make(map[string]bool, len(g.Toggles))
	for k, v := range g.Toggles {
		c.Toggles[k] = v
	}
	return c, true
}

// GroupOf returns the group a player is in.
func (s *System) GroupOf(p world.PlayerID) (GroupID, bool) {
	id, ok := s.byPlayer[p]
	return id, ok
}

// CooldownUntil returns when a player may join a group again, if they can't now.
func (s *System) CooldownUntil(p world.PlayerID) (time.Time, bool) {
	t, ok := s.cooldown[p]
	if !ok || !s.clock.Now().Before(t) {
		return time.Time{}, false
	}
	return t, true
}

func (s *System) onCooldown(p world.PlayerID) bool {
	_, ok := s.CooldownUntil(p)
	return ok
}

// SetVault records the vault balance after the session layer commits a deposit or withdrawal.
func (s *System) SetVault(id GroupID, gold int) {
	if g := s.groups[id]; g != nil {
		g.Vault = gold
	}
}

// SetHall records the hall a group holds (0 for none).
func (s *System) SetHall(id GroupID, hall HallID) {
	if g := s.groups[id]; g != nil {
		g.Hall = hall
	}
}

// FoundResult is the outcome of Found. On OK the session layer takes Cost gold
// from the founder and saves the group.
type FoundResult struct {
	Code    Code
	GroupID GroupID
	Cost    int
}

// Found creates a group with the player as Leader. gold is the player's gold.
func (s *System) Found(p world.PlayerID, gold int, name string, banner int) FoundResult {
	if _, in := s.byPlayer[p]; in {
		return FoundResult{Code: AlreadyInGroup}
	}
	if s.onCooldown(p) {
		return FoundResult{Code: OnCooldown}
	}
	if !s.rules.ValidGroupName(name) {
		return FoundResult{Code: BadName}
	}
	if !s.validBanner(banner) {
		return FoundResult{Code: BadBanner}
	}
	key := strings.ToLower(name)
	if _, taken := s.names[key]; taken {
		return FoundResult{Code: NameTaken}
	}
	cost := s.rules.Group.FoundCost
	if gold < cost {
		return FoundResult{Code: NotEnoughGold, Cost: cost}
	}
	now := s.clock.Now()
	id := s.nextID
	s.nextID++
	s.groups[id] = &Group{
		ID: id, Name: name, Banner: banner,
		Members: []Member{{Player: p, Rank: gamedata.RankLeader, Joined: now, RankSince: now}},
		Toggles: map[string]bool{},
	}
	s.byPlayer[p] = id
	s.names[key] = id
	return FoundResult{Code: OK, GroupID: id, Cost: cost}
}

func (s *System) validBanner(id int) bool {
	for _, b := range s.rules.Group.Banners {
		if b.ID == id {
			return true
		}
	}
	return false
}

// actor looks up the group and the acting member.
func (s *System) actor(p world.PlayerID) (*Group, *Member, Code) {
	id, ok := s.byPlayer[p]
	if !ok {
		return nil, nil, NotInGroup
	}
	g := s.groups[id]
	return g, g.member(p), OK
}

// Invite records an invitation from an Officer or Leader to a player.
func (s *System) Invite(actor, target world.PlayerID) Code {
	g, m, c := s.actor(actor)
	if c != OK {
		return c
	}
	if !s.can(g, m.Rank, gamedata.PermInvite) {
		return NotAllowed
	}
	if _, in := s.byPlayer[target]; in {
		return AlreadyInGroup
	}
	if s.onCooldown(target) {
		return OnCooldown
	}
	if len(g.Members) >= s.rules.Group.MaxMembers {
		return GroupFull
	}
	if s.invites[g.ID] == nil {
		s.invites[g.ID] = map[world.PlayerID]bool{}
	}
	s.invites[g.ID][target] = true
	return OK
}

// Accept joins an invited player to the group as a Recruit.
func (s *System) Accept(p world.PlayerID, id GroupID) Code {
	g := s.groups[id]
	if g == nil {
		return NoSuchGroup
	}
	if !s.invites[id][p] {
		return NotInvited
	}
	if _, in := s.byPlayer[p]; in {
		return AlreadyInGroup
	}
	if s.onCooldown(p) {
		return OnCooldown
	}
	if len(g.Members) >= s.rules.Group.MaxMembers {
		return GroupFull
	}
	now := s.clock.Now()
	g.Members = append(g.Members, Member{Player: p, Rank: gamedata.RankRecruit, Joined: now, RankSince: now})
	s.byPlayer[p] = id
	delete(s.invites[id], p)
	return OK
}

// Decline drops an invitation.
func (s *System) Decline(p world.PlayerID, id GroupID) {
	delete(s.invites[id], p)
}

func (s *System) target(g *Group, actor *Member, t world.PlayerID) (*Member, Code) {
	if actor.Player == t {
		return nil, SelfNotAllowed
	}
	tm := g.member(t)
	if tm == nil {
		return nil, NoSuchMember
	}
	if rankLevel(tm.Rank) >= rankLevel(actor.Rank) {
		return nil, RankTooLow
	}
	return tm, OK
}

// Promote raises a member one rank. The new rank must be below the actor's.
func (s *System) Promote(actor, t world.PlayerID) Code {
	g, am, c := s.actor(actor)
	if c != OK {
		return c
	}
	if !s.can(g, am.Rank, gamedata.PermPromoteDemote) {
		return NotAllowed
	}
	tm, c := s.target(g, am, t)
	if c != OK {
		return c
	}
	next := rankLevel(tm.Rank) + 1
	if next >= rankLevel(am.Rank) {
		return RankTooLow
	}
	tm.Rank = gamedata.Ranks[len(gamedata.Ranks)-next]
	tm.RankSince = s.clock.Now()
	return OK
}

// Demote lowers a member one rank.
func (s *System) Demote(actor, t world.PlayerID) Code {
	g, am, c := s.actor(actor)
	if c != OK {
		return c
	}
	if !s.can(g, am.Rank, gamedata.PermPromoteDemote) {
		return NotAllowed
	}
	tm, c := s.target(g, am, t)
	if c != OK {
		return c
	}
	if tm.Rank == gamedata.RankRecruit {
		return BadTarget
	}
	tm.Rank = gamedata.Ranks[len(gamedata.Ranks)-rankLevel(tm.Rank)+1]
	tm.RankSince = s.clock.Now()
	return OK
}

// Kick removes a member below the actor's rank and starts their cooldown.
func (s *System) Kick(actor, t world.PlayerID) Code {
	g, am, c := s.actor(actor)
	if c != OK {
		return c
	}
	if !s.can(g, am.Rank, gamedata.PermKick) {
		return NotAllowed
	}
	if _, c := s.target(g, am, t); c != OK {
		return c
	}
	s.remove(g, t)
	return OK
}

// Leave removes the player from their group and starts their cooldown. The
// leader must hand over or disband first.
func (s *System) Leave(p world.PlayerID) Code {
	g, m, c := s.actor(p)
	if c != OK {
		return c
	}
	if m.Rank == gamedata.RankLeader {
		return CannotLeave
	}
	s.remove(g, p)
	return OK
}

func (s *System) remove(g *Group, p world.PlayerID) {
	for i := range g.Members {
		if g.Members[i].Player == p {
			g.Members = append(g.Members[:i], g.Members[i+1:]...)
			break
		}
	}
	delete(s.byPlayer, p)
	s.cooldown[p] = s.clock.Now().Add(time.Duration(s.rules.Group.LeaveCooldownH) * time.Hour)
}

// SetToggle lets the leader switch a "set by leader" cell on or off.
func (s *System) SetToggle(actor world.PlayerID, perm string, rank gamedata.Rank, on bool) Code {
	g, am, c := s.actor(actor)
	if c != OK {
		return c
	}
	if am.Rank != gamedata.RankLeader {
		return NotAllowed
	}
	for _, row := range s.rules.Permissions {
		if row.Key == perm && row.Cell(rank).Mode == gamedata.CellSetByLeader {
			g.Toggles[toggleKey(perm, rank)] = on
			return OK
		}
	}
	return BadTarget
}

// LeadershipResult is the outcome of a handover or succession. On OK, Old
// becomes an Officer and New the Leader.
type LeadershipResult struct {
	Code     Code
	GroupID  GroupID
	Old, New world.PlayerID
}

// Handover passes leadership to an Officer.
func (s *System) Handover(actor, t world.PlayerID) LeadershipResult {
	g, am, c := s.actor(actor)
	if c != OK {
		return LeadershipResult{Code: c}
	}
	if am.Rank != gamedata.RankLeader {
		return LeadershipResult{Code: NotAllowed}
	}
	tm := g.member(t)
	if tm == nil || t == actor {
		return LeadershipResult{Code: NoSuchMember}
	}
	if tm.Rank != gamedata.RankOfficer {
		return LeadershipResult{Code: NotOfficer}
	}
	return s.swap(g, am, tm)
}

func (s *System) swap(g *Group, old, next *Member) LeadershipResult {
	now := s.clock.Now()
	old.Rank, old.RankSince = gamedata.RankOfficer, now
	next.Rank, next.RankSince = gamedata.RankLeader, now
	return LeadershipResult{Code: OK, GroupID: g.ID, Old: old.Player, New: next.Player}
}

// CheckInactiveLeader makes the longest-serving Officer the leader when the
// leader's last login is more than the configured days ago. Ties go to the
// earliest to join. The session layer supplies the login time from storage.
func (s *System) CheckInactiveLeader(id GroupID, leaderLastLogin time.Time) LeadershipResult {
	g := s.groups[id]
	if g == nil {
		return LeadershipResult{Code: NoSuchGroup}
	}
	limit := time.Duration(s.rules.Group.LeaderInactiveDays) * 24 * time.Hour
	if s.clock.Now().Sub(leaderLastLogin) <= limit {
		return LeadershipResult{Code: NotInactive}
	}
	var officers []*Member
	for i := range g.Members {
		if g.Members[i].Rank == gamedata.RankOfficer {
			officers = append(officers, &g.Members[i])
		}
	}
	if len(officers) == 0 {
		return LeadershipResult{Code: NoSuccessor}
	}
	sort.SliceStable(officers, func(i, j int) bool {
		a, b := officers[i], officers[j]
		if !a.RankSince.Equal(b.RankSince) {
			return a.RankSince.Before(b.RankSince)
		}
		return a.Joined.Before(b.Joined)
	})
	return s.swap(g, g.leader(), officers[0])
}

// DisbandResult is the outcome of Disband. On OK the session layer pays
// VaultGold to Leader, releases Hall (if non-zero) and deletes the group.
type DisbandResult struct {
	Code      Code
	GroupID   GroupID
	Leader    world.PlayerID
	VaultGold int
	Hall      HallID
	Members   []world.PlayerID // everyone removed, the leader included
}

// Disband removes the group and all its members. Disbanding is not leaving, so
// members get no cooldown.
func (s *System) Disband(actor world.PlayerID) DisbandResult {
	g, am, c := s.actor(actor)
	if c != OK {
		return DisbandResult{Code: c}
	}
	if !s.can(g, am.Rank, gamedata.PermHallAndSiege) || am.Rank != gamedata.RankLeader {
		return DisbandResult{Code: NotAllowed}
	}
	res := DisbandResult{Code: OK, GroupID: g.ID, Leader: actor, VaultGold: g.Vault, Hall: g.Hall}
	for _, m := range g.Members {
		res.Members = append(res.Members, m.Player)
		delete(s.byPlayer, m.Player)
	}
	delete(s.names, strings.ToLower(g.Name))
	delete(s.invites, g.ID)
	delete(s.groups, g.ID)
	return res
}
