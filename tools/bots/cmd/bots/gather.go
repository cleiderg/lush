package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/cleiderg/lush/tools/bots/internal/protocol"
)

// Item ids from server/internal/systems/gamedata/data/items.json.
const (
	bronzeHatchetID = 15
	smallNetID      = 17
)

const (
	// shopReach is how close to the stall a bot stands to buy; the server's
	// shop distance is 2 tiles, so this leaves a margin.
	shopReach = 1.5
	// retryEvery spaces repeated ShopBuys and SkillActions that got no answer.
	retryEvery = 5 * time.Second
)

// place is a tree or a candidate fishing spot, in world metres.
type place struct {
	kind string
	x, z float64
}

// gatherMap is what gatherers need from shared/map: skills.json and the shop
// stall from areas.json. Both are read at runtime so map changes need no copy.
type gatherMap struct {
	trees []place // index is the tree id on the wire (file order)
	spots []place // index is FishingSpotState.candidate (file order)
	shop  [2]float64
}

type xz struct {
	X float64 `json:"x"`
	Z float64 `json:"z"`
}

type placeFile struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	xz
}

func loadGatherMap(skillsPath, areasPath string) (*gatherMap, error) {
	var sk struct {
		Trees []placeFile `json:"trees"`
		Pond  struct {
			Spots []placeFile `json:"spots"`
		} `json:"pond"`
	}
	var ar struct {
		Shop xz `json:"shop"`
	}
	for path, v := range map[string]any{skillsPath: &sk, areasPath: &ar} {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, v); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	m := &gatherMap{shop: [2]float64{ar.Shop.X / 10, ar.Shop.Z / 10}}
	conv := func(in []placeFile) (out []place) {
		for _, p := range in { // decimetres to metres
			out = append(out, place{p.Kind, p.X / 10, p.Z / 10})
		}
		return out
	}
	m.trees, m.spots = conv(sk.Trees), conv(sk.Pond.Spots)
	if len(m.trees) == 0 || len(m.spots) == 0 {
		return nil, fmt.Errorf("%s: no trees or fishing spots", skillsPath)
	}
	return m, nil
}

// resources is what the bot knows from the latest ResourceState.
type resources struct {
	known  bool
	felled map[uint16]bool
	spots  []spotState
}

type spotState struct {
	id        uint16 // SkillAction target id
	candidate uint8  // index into gatherMap.spots
}

// skillAction is a SkillAction to send.
type skillAction struct {
	target protocol.SkillTarget
	id     uint16
}

// gatherStep is what a gatherer does this tick. Only one of buy and act is set.
type gatherStep struct {
	moveX, moveZ float64 // unit direction, or zero to stand still
	buy          uint16  // item id for a ShopBuy, 0 if none
	act          *skillAction
}

// gatherer walks to the shop for a tool, then to the nearest resource, and
// gathers there.
type gatherer struct {
	mu      sync.Mutex // noteResult runs on the read goroutine, step on the play one
	m       *gatherMap
	fishing bool // fish with a net, otherwise chop

	target    int // tree id or spot id, -1 when none
	lastSent  time.Time
	buyAt     time.Time
	full      bool // the inventory filled up; nothing more to do
	hasTarget bool
}

func newGatherer(m *gatherMap, fishing bool) *gatherer {
	return &gatherer{m: m, fishing: fishing, target: -1}
}

func (g *gatherer) tool() uint16 {
	if g.fishing {
		return smallNetID
	}
	return bronzeHatchetID
}

func hasItem(inv inventory, id uint16) bool {
	if inv.weapon == id {
		return true
	}
	for _, s := range inv.slots {
		if s == id {
			return true
		}
	}
	return false
}

// headTo returns the unit direction from (x, z) to p, and the distance.
func headTo(x, z, px, pz float64) (dx, dz, d float64) {
	d = math.Hypot(px-x, pz-z)
	if d == 0 {
		return 0, 0, 0
	}
	return (px - x) / d, (pz - z) / d, d
}

// inReach is the server's range check for gathering: the bot's tile is within
// 1 tile of the target's, diagonals included (skills range_tiles 1 and
// range_diagonal; a tile is 1 m). A distance check isn't enough: trees and
// spots needn't sit at a tile centre, so 1.4 m can be 2 tiles away.
func inReach(x, z float64, p place) bool {
	return math.Abs(math.Floor(x)-math.Floor(p.x)) <= 1 && math.Abs(math.Floor(z)-math.Floor(p.z)) <= 1
}

// pick chooses the nearest standing tree or active net spot to (x, z).
func (g *gatherer) pick(x, z float64, res resources) (id int, p place, ok bool) {
	best := math.MaxFloat64
	consider := func(i int, c place) {
		if d := math.Hypot(c.x-x, c.z-z); d < best {
			best, id, p, ok = d, i, c, true
		}
	}
	if g.fishing {
		for _, s := range res.spots {
			if int(s.candidate) < len(g.m.spots) && g.m.spots[s.candidate].kind == "net" {
				consider(int(s.id), g.m.spots[s.candidate])
			}
		}
		return
	}
	for i, t := range g.m.trees {
		if !res.felled[uint16(i)] {
			consider(i, t)
		}
	}
	return
}

// current returns the position of the chosen target if it is still there.
func (g *gatherer) current(res resources) (place, bool) {
	if !g.hasTarget {
		return place{}, false
	}
	if g.fishing {
		for _, s := range res.spots {
			if int(s.id) == g.target && int(s.candidate) < len(g.m.spots) && g.m.spots[s.candidate].kind == "net" {
				return g.m.spots[s.candidate], true
			}
		}
		return place{}, false
	}
	if g.target < len(g.m.trees) && !res.felled[uint16(g.target)] {
		return g.m.trees[g.target], true
	}
	return place{}, false
}

// noteResult records a SkillResult.
func (g *gatherer) noteResult(code protocol.SkillResultCode) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch code {
	case protocol.SkillResultCodeInventoryFull:
		g.full = true
	case protocol.SkillResultCodeResourceGone, protocol.SkillResultCodeOutOfRange:
		g.hasTarget = false
	}
}

// step decides the next move from the bot's world position (metres), its
// inventory and the resource state.
func (g *gatherer) step(now time.Time, x, z float64, inv inventory, res resources) gatherStep {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !inv.known || g.full {
		return gatherStep{}
	}
	if !hasItem(inv, g.tool()) {
		dx, dz, d := headTo(x, z, g.m.shop[0], g.m.shop[1])
		if d > shopReach {
			return gatherStep{moveX: dx, moveZ: dz}
		}
		if now.Before(g.buyAt) {
			return gatherStep{}
		}
		g.buyAt = now.Add(retryEvery)
		return gatherStep{buy: g.tool()}
	}
	if !res.known {
		return gatherStep{}
	}
	p, ok := g.current(res)
	if !ok {
		var id int
		if id, p, ok = g.pick(x, z, res); !ok {
			return gatherStep{}
		}
		if id != g.target {
			g.lastSent = time.Time{} // a new target: act at once, a repeat waits
		}
		g.target, g.hasTarget = id, true
	}
	if !inReach(x, z, p) {
		dx, dz, _ := headTo(x, z, p.x, p.z)
		return gatherStep{moveX: dx, moveZ: dz}
	}
	if now.Sub(g.lastSent) < retryEvery {
		return gatherStep{}
	}
	g.lastSent = now
	kind := protocol.SkillTargetTree
	if g.fishing {
		kind = protocol.SkillTargetFishingSpot
	}
	return gatherStep{act: &skillAction{kind, uint16(g.target)}}
}
