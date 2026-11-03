package session

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/gather"
	"github.com/cleiderg/lush/server/internal/world"
)

// SkillActionCommand is a client SkillAction. Handle turns a Tree or
// FishingSpot target into a gather.StartCommand for the map id the gathering
// system numbers ID with, or answers ResourceGone if there is none. The Grill
// target (cooking) is not handled here.
type SkillActionCommand struct {
	Player world.PlayerID
	Kind   protocol.SkillTarget
	ID     uint16
}

func (c SkillActionCommand) Target() world.PlayerID { return c.Player }

// skills is the session's side of gathering: the layout it needs to name
// places by kind, and the SkillCatalog sent on join.
type skills struct {
	sys     *gather.System
	kinds   map[string]string // map id -> tree or spot kind
	catalog []byte
	hatchet gamedata.ItemID // lowest-level hatchet
	// startAt is the map id of the StartCommand being handled, for StartResult.
	startAt string
}

// SetGather connects the gathering system and its layout: SkillAction starts
// gathering, its results go out as SkillResult, and joining players get the
// SkillCatalog and ResourceState. The system must have been created with the
// session as its Sink. Call it before the world starts ticking.
func (s *Server) SetGather(sys *gather.System, l gather.Layout) {
	k := &skills{sys: sys, kinds: map[string]string{}}
	for _, p := range l.Trees {
		k.kinds[p.ID] = p.Kind
	}
	for _, p := range l.Spots {
		k.kinds[p.ID] = p.Kind
	}
	k.hatchet = lowestTool(s.data, gamedata.SkillWoodcutting)
	k.catalog = encodeSkillCatalog(s.data, l)
	s.gather = k
}

// lowestTool is the item of the lowest tool level for skill sk (the lowest
// item id among equals), or 0.
func lowestTool(d *gamedata.Data, sk gamedata.Skill) gamedata.ItemID {
	var best gamedata.ItemID
	lvl := 0
	for _, it := range d.Items {
		if it.ToolSkill == sk && (best == 0 || it.ToolLevel < lvl) {
			best, lvl = it.ID, it.ToolLevel
		}
	}
	return best
}

// handleSkillAction starts gathering for p, or tells them the target is gone.
func (s *Server) handleSkillAction(a SkillActionCommand, p *world.Player) {
	k := s.gather
	if k == nil {
		return
	}
	var id string
	var ok bool
	var sk world.Skill
	switch a.Kind {
	case protocol.SkillTargetTree:
		id, ok = k.sys.TreePlace(int(a.ID))
		sk = world.Woodcutting
	case protocol.SkillTargetFishingSpot:
		id, ok = k.sys.SpotPlace(int(a.ID))
		sk = world.Fishing
	default:
		return
	}
	if !ok {
		s.sendSkillResult(p.ID, sk, protocol.SkillResultCodeResourceGone, 0, 0)
		return
	}
	k.startAt = id
	k.sys.Handle(gather.StartCommand{Player: p.ID, Place: id}, p)
	k.startAt = ""
}

// requirement is the tool and the level a player at skill level lvl lacks for
// the place with the map id.
func (s *Server) requirement(p *world.Player, id string) (sk world.Skill, tool gamedata.ItemID, level int) {
	kind := s.gather.kinds[id]
	for _, t := range s.data.Skills.Trees {
		if t.Key == kind {
			return world.Woodcutting, s.gather.hatchet, s.toolLevelNeeded(p, world.Woodcutting, t.Level)
		}
	}
	for _, f := range s.data.Skills.FishingSpots {
		if f.Key == kind+"_spot" {
			return world.Fishing, f.Tool, s.toolLevelNeeded(p, world.Fishing, f.Level)
		}
	}
	return world.Fishing, 0, 0
}

// toolLevelNeeded is the level that opens the target: its own level, or when
// the player has that but only carries a tool above their level, that tool's.
func (s *Server) toolLevelNeeded(p *world.Player, sk world.Skill, base int) int {
	if p == nil || p.SkillLevel(sk) < base {
		return base
	}
	gsk := gamedata.SkillWoodcutting
	if sk == world.Fishing {
		gsk = gamedata.SkillFishing
	}
	need := 0
	for _, st := range p.Inv.Slots {
		if it, ok := s.data.Item(st.Item); ok && st.Item != 0 && it.ToolSkill == gsk && (need == 0 || it.ToolLevel < need) {
			need = it.ToolLevel
		}
	}
	return max(base, need)
}

var startCodes = map[gather.Result]protocol.SkillResultCode{
	gather.Started:       protocol.SkillResultCodeStarted,
	gather.NoSuchTarget:  protocol.SkillResultCodeResourceGone,
	gather.Felled:        protocol.SkillResultCodeResourceGone,
	gather.TooFar:        protocol.SkillResultCodeOutOfRange,
	gather.NoTool:        protocol.SkillResultCodeNeedTool,
	gather.LevelTooLow:   protocol.SkillResultCodeNeedLevel,
	gather.InventoryFull: protocol.SkillResultCodeInventoryFull,
	gather.InCombat:      protocol.SkillResultCodeStopped,
}

var stopCodes = map[gather.StopReason]protocol.SkillResultCode{
	gather.StoppedByPlayer:  protocol.SkillResultCodeStopped,
	gather.StoppedMoved:     protocol.SkillResultCodeStopped,
	gather.StoppedCombat:    protocol.SkillResultCodeStopped,
	gather.StoppedFull:      protocol.SkillResultCodeInventoryFull,
	gather.StoppedFelled:    protocol.SkillResultCodeResourceGone,
	gather.StoppedSpotMoved: protocol.SkillResultCodeResourceGone,
	gather.StoppedNoTool:    protocol.SkillResultCodeNeedTool,
	gather.StoppedLevel:     protocol.SkillResultCodeNeedLevel,
}

// sendCoded sends the SkillResult for code about the place id, with the tool
// or level it names filled in.
func (s *Server) sendCoded(to world.PlayerID, id string, code protocol.SkillResultCode) {
	sk, tool, level := s.requirement(s.w.Player(to), id)
	var item gamedata.ItemID
	var lvl int
	switch code {
	case protocol.SkillResultCodeNeedTool:
		item = tool
	case protocol.SkillResultCodeNeedLevel:
		lvl = level
	}
	s.sendSkillResult(to, sk, code, item, lvl)
}

// StartResult implements gather.Sink. Called on the tick goroutine, inside
// handleSkillAction.
func (s *Server) StartResult(to world.PlayerID, r gather.Result) {
	k := s.gather
	code, ok := startCodes[r]
	if k == nil || !ok || k.startAt == "" {
		return
	}
	if g := s.live[to]; g != nil && r == gather.Started {
		g.gatherAt = k.startAt
	}
	s.sendCoded(to, k.startAt, code)
}

// Stopped implements gather.Sink.
func (s *Server) Stopped(to world.PlayerID, why gather.StopReason) {
	g := s.live[to]
	code, ok := stopCodes[why]
	if s.gather == nil || g == nil || !ok || g.gatherAt == "" {
		return
	}
	id := g.gatherAt
	g.gatherAt = ""
	s.sendCoded(to, id, code)
}

// Gained implements gather.Sink: one SkillResult Success for the item. The
// XP reaches the client in SkillStats and a new level in LevelUp, from the
// player's own state.
func (s *Server) Gained(to world.PlayerID, sk world.Skill, item gamedata.ItemID, _ int, _ bool, _ int) {
	s.sendSkillResult(to, sk, protocol.SkillResultCodeSuccess, item, 0)
}

// ResourcesChanged implements gather.Sink: every connected player gets the
// new ResourceState.
func (s *Server) ResourcesChanged() {
	if s.gather == nil {
		return
	}
	msg := encodeResourceState(s.bld, s.gather.sys.Resources())
	for _, g := range s.live {
		if g.conn != nil {
			g.conn.Send(msg)
		}
	}
}

// sendInitialSkills sends a joining client the SkillCatalog and ResourceState.
func (s *Server) sendInitialSkills(g *guest) {
	if s.gather == nil {
		return
	}
	g.conn.Send(s.gather.catalog)
	g.conn.Send(encodeResourceState(s.bld, s.gather.sys.Resources()))
}

func (s *Server) sendSkillResult(to world.PlayerID, sk world.Skill, code protocol.SkillResultCode, item gamedata.ItemID, level int) {
	if g := s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(encodeSkillResult(s.bld, sk, code, item, level))
	}
}

func encodeSkillResult(b *flatbuffers.Builder, sk world.Skill, code protocol.SkillResultCode, item gamedata.ItemID, level int) []byte {
	b.Reset()
	protocol.SkillResultStart(b)
	protocol.SkillResultAddSkill(b, protocol.Skill(sk))
	protocol.SkillResultAddCode(b, code)
	protocol.SkillResultAddItemId(b, uint16(item))
	protocol.SkillResultAddLevel(b, clampU8(level))
	return finish(b, protocol.MessageSkillResult, protocol.SkillResultEnd(b))
}

// encodeResourceState builds a ResourceState.. The bytes are a copy, so they can go to many connections.
func encodeResourceState(b *flatbuffers.Builder, r gather.ResourceState) []byte {
	b.Reset()
	protocol.ResourceStateStartFelledTreesVector(b, len(r.Felled))
	for i := len(r.Felled) - 1; i >= 0; i-- {
		b.PrependUint16(uint16(r.Felled[i]))
	}
	felled := b.EndVector(len(r.Felled))
	protocol.ResourceStateStartFishingSpotsVector(b, len(r.Spots))
	for i := len(r.Spots) - 1; i >= 0; i-- {
		protocol.CreateFishingSpotState(b, uint16(r.Spots[i].Slot), uint8(r.Spots[i].Candidate))
	}
	spots := b.EndVector(len(r.Spots))
	protocol.ResourceStateStart(b)
	protocol.ResourceStateAddFelledTrees(b, felled)
	protocol.ResourceStateAddFishingSpots(b, spots)
	return finish(b, protocol.MessageResourceState, protocol.ResourceStateEnd(b))
}

// encodeSkillCatalog builds the SkillCatalog: each tree kind and fishing spot
// kind in the layout, in file order, with its level and tool.
func encodeSkillCatalog(d *gamedata.Data, l gather.Layout) []byte {
	b := flatbuffers.NewBuilder(256)
	var treeKinds, spotKinds []string
	seen := map[string]bool{}
	for _, p := range l.Trees {
		if !seen["t"+p.Kind] {
			seen["t"+p.Kind] = true
			treeKinds = append(treeKinds, p.Kind)
		}
	}
	for _, p := range l.Spots {
		if !seen["s"+p.Kind] {
			seen["s"+p.Kind] = true
			spotKinds = append(spotKinds, p.Kind)
		}
	}
	var trees, spots []flatbuffers.UOffsetT
	for _, kind := range treeKinds {
		for _, t := range d.Skills.Trees {
			if t.Key == kind {
				name := b.CreateString(kind)
				protocol.TreeKindDefStart(b)
				protocol.TreeKindDefAddKind(b, name)
				protocol.TreeKindDefAddLevel(b, clampU8(t.Level))
				trees = append(trees, protocol.TreeKindDefEnd(b))
			}
		}
	}
	for _, kind := range spotKinds {
		for _, f := range d.Skills.FishingSpots {
			if f.Key == kind+"_spot" {
				name := b.CreateString(kind)
				protocol.FishingSpotKindDefStart(b)
				protocol.FishingSpotKindDefAddKind(b, name)
				protocol.FishingSpotKindDefAddLevel(b, clampU8(f.Level))
				protocol.FishingSpotKindDefAddToolItemId(b, uint16(f.Tool))
				spots = append(spots, protocol.FishingSpotKindDefEnd(b))
			}
		}
	}
	protocol.SkillCatalogStartTreesVector(b, len(trees))
	for i := len(trees) - 1; i >= 0; i-- {
		b.PrependUOffsetT(trees[i])
	}
	tv := b.EndVector(len(trees))
	protocol.SkillCatalogStartFishingSpotsVector(b, len(spots))
	for i := len(spots) - 1; i >= 0; i-- {
		b.PrependUOffsetT(spots[i])
	}
	sv := b.EndVector(len(spots))
	protocol.SkillCatalogStart(b)
	protocol.SkillCatalogAddTrees(b, tv)
	protocol.SkillCatalogAddFishingSpots(b, sv)
	return finish(b, protocol.MessageSkillCatalog, protocol.SkillCatalogEnd(b))
}
