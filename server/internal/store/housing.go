package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Area is a housing area (housing spec, Data model).
type Area struct {
	ID   int32
	Name string
	X, Z float32
}

// Plot is one house plot. OwnerID is 0 while the plot is empty; ClaimedAt and
// RentPaidUntil are then the zero time.
type Plot struct {
	ID      int32
	AreaID  int32
	X, Z    float32
	OwnerID int64

	ClaimedAt     time.Time
	RentPaidUntil time.Time
}

// Home is the room built on a claimed plot. A zero WallpaperItem or
// FlooringItem means none is set.
type Home struct {
	ID            int64
	PlotID        int32
	OwnerID       int64
	WallpaperItem int32
	FlooringItem  int32
	GridSize      int
	// Locked keeps visitors out; an open home admits them.
	Locked bool
}

// Layers of a placed item, with the bit values the protocol and the room
// model use.
const (
	LayerFloor   uint8 = 1
	LayerRug     uint8 = 2
	LayerSurface uint8 = 4
	LayerWall    uint8 = 8
)

// PlacedItem is one item placed in a home. ID is chosen by the caller (the
// room's item id), is non-zero and is unique across all homes, so saving the
// same edit twice places nothing twice. Rotation and Side are 0..3. ParentID
// is 0 unless the item sits on a surface item, in Slot; a wall item's Side is
// the wall and X its position along it.
type PlacedItem struct {
	ID       int64
	Item     int32
	X, Y     int16
	Rotation uint8
	Layer    uint8
	ParentID int64
	Slot     uint8
	Side     uint8
}

// HomeRoom is a home with the items placed in it, by placed id.
type HomeRoom struct {
	Home
	Items []PlacedItem
}

// HomeEdit replaces the room of one home.
type HomeEdit struct {
	HomeID  int64
	OwnerID int64
	// Holdings, when set, becomes the owner's holdings: the other half of
	// placing or removing an item. Nil leaves them alone (a move or rotate).
	Holdings *Holdings
	// Items is the whole room. An item already placed is updated, a new one
	// is placed and one not listed is removed. An ID placed in another home is
	// invalid. A ParentID must name another listed item.
	Items         []PlacedItem
	WallpaperItem int32
	FlooringItem  int32
}

// DefaultGridSize is the side of a new home's room, in tiles.
const DefaultGridSize = 8

// ClaimPlot claims one plot for one player.
type ClaimPlot struct {
	PlayerID int64
	PlotID   int32
	// Cost is the gold taken, and Rent the time it pays for: the plot is
	// paid until Now+Rent (game data: two months' rent).
	Cost int64
	Rent time.Duration
	Now  time.Time
}

// PayRent pays rent for the plot the player owns.
type PayRent struct {
	PlayerID int64
	// Cost is the gold taken, and Extend how far RentPaidUntil moves.
	Cost   int64
	Extend time.Duration
}

// Audit kinds written by the housing methods. The payload has the plot id,
// the gold taken and the new rent_paid_until.
const (
	HousingClaimKind = "housing_claim"
	HousingRentKind  = "housing_rent"
)

// Housing stores housing areas, plots and homes.
type Housing interface {
	// UpsertPlots inserts the areas and plots that don't exist and updates
	// the name and position of those that do. It never changes ownership, so
	// calling it again with the same lists changes nothing. A plot naming an
	// unknown area fails with ErrInvalid; on any error nothing changes.
	UpsertPlots(ctx context.Context, areas []Area, plots []Plot) error
	// LoadPlots returns every plot with its owner and paid-until, by plot id.
	LoadPlots(ctx context.Context) ([]Plot, error)
	// LoadHome returns the home of the player's plot, or an error wrapping
	// ErrNotFound.
	LoadHome(ctx context.Context, owner int64) (Home, error)
	// LoadHomes returns every home with its items, by home id.
	LoadHomes(ctx context.Context) ([]HomeRoom, error)
	// ApplyHomeEdit, in one transaction, sets the owner's holdings (if
	// e.Holdings is set), replaces the home's placed items, wallpaper and
	// flooring, and appends entries to the economy audit log. An unknown home
	// fails with ErrNotFound; an owner who doesn't own it, invalid holdings,
	// an invalid item or an item id from another home with ErrInvalid. On any
	// error nothing changes.
	ApplyHomeEdit(ctx context.Context, e HomeEdit, entries []Entry) error
	// SetHomeLocked sets whether the home is locked. An unknown home fails
	// with ErrNotFound.
	SetHomeLocked(ctx context.Context, homeID int64, locked bool) error
	// ClaimPlot, in one transaction, checks that the plot is empty and that
	// the player owns no plot and holds c.Cost gold, takes the gold, sets
	// owner, claimed_at and rent_paid_until, creates the home and appends a
	// HousingClaimKind audit entry. An unknown plot or player fails with
	// ErrNotFound, a taken plot or a player who already has one with
	// ErrAlreadyExists, and too little gold or a negative cost with ErrInvalid.
	// On any error nothing changes. It returns the claimed plot.
	ClaimPlot(ctx context.Context, c ClaimPlot) (Plot, error)
	// PayRent, in one transaction, takes the gold, moves the rent_paid_until
	// of the player's plot forward by r.Extend and appends a HousingRentKind
	// audit entry. A player with no plot fails with ErrNotFound, too little
	// gold with ErrInvalid. On any error nothing changes. It returns the plot.
	PayRent(ctx context.Context, r PayRent) (Plot, error)
}

var (
	_ Housing = (*Memory)(nil)
	_ Housing = (*PGPlayers)(nil)
)

func (c ClaimPlot) validate() error {
	if c.Cost < 0 || c.Rent <= 0 {
		return fmt.Errorf("cost %d rent %s: %w", c.Cost, c.Rent, ErrInvalid)
	}
	return nil
}

func (r PayRent) validate() error {
	if r.Cost < 0 || r.Extend <= 0 {
		return fmt.Errorf("cost %d extend %s: %w", r.Cost, r.Extend, ErrInvalid)
	}
	return nil
}

func claimEntry(c ClaimPlot, until time.Time) Entry {
	return Entry{Kind: HousingClaimKind, Actor: c.PlayerID,
		Payload: map[string]any{"plot": c.PlotID, "gold": c.Cost, "rent_paid_until": until.UTC()}}
}

func rentEntry(r PayRent, plot int32, until time.Time) Entry {
	return Entry{Kind: HousingRentKind, Actor: r.PlayerID,
		Payload: map[string]any{"plot": plot, "gold": r.Cost, "rent_paid_until": until.UTC()}}
}

var layerNames = map[uint8]string{LayerFloor: "floor", LayerRug: "rug", LayerSurface: "surface", LayerWall: "wall"}

func layerByName(n string) uint8 {
	for l, name := range layerNames {
		if name == n {
			return l
		}
	}
	return 0
}

// validate checks the edit's shape, which both stores require.
func (e HomeEdit) validate() error {
	if e.WallpaperItem < 0 || e.FlooringItem < 0 {
		return fmt.Errorf("wallpaper %d flooring %d: %w", e.WallpaperItem, e.FlooringItem, ErrInvalid)
	}
	if e.Holdings != nil {
		if err := e.Holdings.validate(); err != nil {
			return err
		}
	}
	ids := map[int64]bool{}
	for i, it := range e.Items {
		if _, ok := layerNames[it.Layer]; !ok || it.Item <= 0 || it.Rotation > 3 || it.Side > 3 || it.ID <= 0 || it.ParentID < 0 {
			return fmt.Errorf("item %d: %+v: %w", i, it, ErrInvalid)
		}
		if ids[it.ID] {
			return fmt.Errorf("item %d listed twice: %w", it.ID, ErrInvalid)
		}
		ids[it.ID] = true
	}
	for i, it := range e.Items {
		if it.ParentID != 0 && !ids[it.ParentID] {
			return fmt.Errorf("item %d: parent %d not in the room: %w", i, it.ParentID, ErrInvalid)
		}
	}
	if len(parentsFirst(e.Items)) != len(e.Items) {
		return fmt.Errorf("items stacked in a loop: %w", ErrInvalid)
	}
	return nil
}

// parentsFirst orders items so each comes after its parent, leaving out any
// item whose parent chain loops or names an item not listed.
func parentsFirst(items []PlacedItem) []PlacedItem {
	out := make([]PlacedItem, 0, len(items))
	done := map[int64]bool{0: true}
	for progress := true; progress && len(out) < len(items); {
		progress = false
		for _, it := range items {
			if !done[it.ID] && done[it.ParentID] {
				out = append(out, it)
				done[it.ID] = true
				progress = true
			}
		}
	}
	return out
}

// checkSeed checks what both stores require of a seed list.
func checkSeed(areas []Area, plots []Plot) error {
	known := map[int32]bool{}
	for _, a := range areas {
		if known[a.ID] {
			return fmt.Errorf("area %d listed twice: %w", a.ID, ErrInvalid)
		}
		known[a.ID] = true
	}
	seen := map[int32]bool{}
	for _, p := range plots {
		if seen[p.ID] {
			return fmt.Errorf("plot %d listed twice: %w", p.ID, ErrInvalid)
		}
		seen[p.ID] = true
	}
	return nil
}

// ---- Memory ----

type memHousing struct {
	areas  map[int32]Area
	plots  map[int32]Plot
	homes  map[int64]Home         // by owner
	items  map[int64][]PlacedItem // by home id, ordered by placed id
	nextID int64
}

func (m *Memory) housingLocked() *memHousing {
	if m.housing == nil {
		m.housing = &memHousing{areas: map[int32]Area{}, plots: map[int32]Plot{}, homes: map[int64]Home{}, items: map[int64][]PlacedItem{}, nextID: 1}
	}
	return m.housing
}

func (m *Memory) UpsertPlots(ctx context.Context, areas []Area, plots []Plot) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("upsert plots: %w", err)
	}
	if err := checkSeed(areas, plots); err != nil {
		return fmt.Errorf("upsert plots: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.housingLocked()
	for _, p := range plots {
		if _, stored := h.areas[p.AreaID]; !stored && !slices.ContainsFunc(areas, func(a Area) bool { return a.ID == p.AreaID }) {
			return fmt.Errorf("upsert plots: plot %d area %d: %w", p.ID, p.AreaID, ErrInvalid)
		}
	}
	for _, a := range areas {
		h.areas[a.ID] = a
	}
	for _, p := range plots {
		if cur, ok := h.plots[p.ID]; ok {
			cur.AreaID, cur.X, cur.Z = p.AreaID, p.X, p.Z
			h.plots[p.ID] = cur
			continue
		}
		h.plots[p.ID] = Plot{ID: p.ID, AreaID: p.AreaID, X: p.X, Z: p.Z}
	}
	return nil
}

func (m *Memory) LoadPlots(ctx context.Context) ([]Plot, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load plots: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.housingLocked()
	out := make([]Plot, 0, len(h.plots))
	for _, id := range slices.Sorted(maps.Keys(h.plots)) {
		out = append(out, h.plots[id])
	}
	return out, nil
}

func (m *Memory) LoadHome(ctx context.Context, owner int64) (Home, error) {
	if err := ctx.Err(); err != nil {
		return Home{}, fmt.Errorf("load home %d: %w", owner, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	home, ok := m.housingLocked().homes[owner]
	if !ok {
		return Home{}, fmt.Errorf("load home %d: %w", owner, ErrNotFound)
	}
	return home, nil
}

func (m *Memory) LoadHomes(ctx context.Context) ([]HomeRoom, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load homes: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.housingLocked()
	var out []HomeRoom
	for _, home := range h.homes {
		out = append(out, HomeRoom{Home: home, Items: slices.Clone(h.items[home.ID])})
	}
	slices.SortFunc(out, func(a, b HomeRoom) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

func (m *Memory) ApplyHomeEdit(ctx context.Context, e HomeEdit, entries []Entry) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("apply home edit %d: %w", e.HomeID, err)
	}
	if err := e.validate(); err != nil {
		return fmt.Errorf("apply home edit %d: %w", e.HomeID, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.housingLocked()
	var home Home
	found := false
	for _, hm := range h.homes {
		if hm.ID == e.HomeID {
			home, found = hm, true
		}
	}
	if !found {
		return fmt.Errorf("apply home edit %d: %w", e.HomeID, ErrNotFound)
	}
	if home.OwnerID != e.OwnerID {
		return fmt.Errorf("apply home edit %d: owner %d: %w", e.HomeID, e.OwnerID, ErrInvalid)
	}
	elsewhere := map[int64]bool{}
	for id, items := range h.items {
		for _, it := range items {
			if id != home.ID {
				elsewhere[it.ID] = true
			}
		}
	}
	for _, it := range e.Items {
		if elsewhere[it.ID] {
			return fmt.Errorf("apply home edit %d: item %d is in another home: %w", e.HomeID, it.ID, ErrInvalid)
		}
	}
	var changes []EconomyChange
	if e.Holdings != nil {
		changes = []EconomyChange{{PlayerID: e.OwnerID, Holdings: *e.Holdings}}
	}
	if err := m.checkEconomyLocked(changes, entries); err != nil {
		return fmt.Errorf("apply home edit %d: %w", e.HomeID, err)
	}
	m.applyEconomyLocked(changes, entries)
	items := slices.Clone(e.Items)
	slices.SortFunc(items, func(a, b PlacedItem) int { return cmp.Compare(a.ID, b.ID) })
	h.items[home.ID] = items
	home.WallpaperItem, home.FlooringItem = e.WallpaperItem, e.FlooringItem
	h.homes[home.OwnerID] = home
	return nil
}

func (m *Memory) SetHomeLocked(ctx context.Context, homeID int64, locked bool) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("set home %d locked: %w", homeID, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.housingLocked()
	for owner, home := range h.homes {
		if home.ID == homeID {
			home.Locked = locked
			h.homes[owner] = home
			return nil
		}
	}
	return fmt.Errorf("set home %d locked: %w", homeID, ErrNotFound)
}

func (m *Memory) ClaimPlot(ctx context.Context, c ClaimPlot) (Plot, error) {
	if err := ctx.Err(); err != nil {
		return Plot{}, fmt.Errorf("claim plot %d: %w", c.PlotID, err)
	}
	if err := c.validate(); err != nil {
		return Plot{}, fmt.Errorf("claim plot %d: %w", c.PlotID, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.housingLocked()
	p, ok := h.plots[c.PlotID]
	if !ok {
		return Plot{}, fmt.Errorf("claim plot %d: %w", c.PlotID, ErrNotFound)
	}
	pl, ok := m.byID[c.PlayerID]
	if !ok {
		return Plot{}, fmt.Errorf("claim plot %d: player %d: %w", c.PlotID, c.PlayerID, ErrNotFound)
	}
	if p.OwnerID != 0 {
		return Plot{}, fmt.Errorf("claim plot %d: owned by %d: %w", c.PlotID, p.OwnerID, ErrAlreadyExists)
	}
	if _, has := h.homes[c.PlayerID]; has {
		return Plot{}, fmt.Errorf("claim plot %d: player %d has a plot: %w", c.PlotID, c.PlayerID, ErrAlreadyExists)
	}
	if pl.Gold < c.Cost {
		return Plot{}, fmt.Errorf("claim plot %d: gold %d, cost %d: %w", c.PlotID, pl.Gold, c.Cost, ErrInvalid)
	}
	until := c.Now.Add(c.Rent)
	e := claimEntry(c, until)
	if err := checkPayload(e); err != nil {
		return Plot{}, fmt.Errorf("claim plot %d: %w", c.PlotID, err)
	}
	pl.Gold -= c.Cost
	m.byID[pl.ID] = pl
	p.OwnerID, p.ClaimedAt, p.RentPaidUntil = c.PlayerID, c.Now, until
	h.plots[p.ID] = p
	h.homes[c.PlayerID] = Home{ID: h.nextID, PlotID: p.ID, OwnerID: c.PlayerID, GridSize: DefaultGridSize}
	h.nextID++
	m.audit = append(m.audit, e)
	return p, nil
}

func (m *Memory) PayRent(ctx context.Context, r PayRent) (Plot, error) {
	if err := ctx.Err(); err != nil {
		return Plot{}, fmt.Errorf("pay rent %d: %w", r.PlayerID, err)
	}
	if err := r.validate(); err != nil {
		return Plot{}, fmt.Errorf("pay rent %d: %w", r.PlayerID, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.housingLocked()
	home, ok := h.homes[r.PlayerID]
	if !ok {
		return Plot{}, fmt.Errorf("pay rent %d: no plot: %w", r.PlayerID, ErrNotFound)
	}
	pl := m.byID[r.PlayerID]
	if pl.Gold < r.Cost {
		return Plot{}, fmt.Errorf("pay rent %d: gold %d, cost %d: %w", r.PlayerID, pl.Gold, r.Cost, ErrInvalid)
	}
	p := h.plots[home.PlotID]
	until := p.RentPaidUntil.Add(r.Extend)
	e := rentEntry(r, p.ID, until)
	if err := checkPayload(e); err != nil {
		return Plot{}, fmt.Errorf("pay rent %d: %w", r.PlayerID, err)
	}
	pl.Gold -= r.Cost
	m.byID[pl.ID] = pl
	p.RentPaidUntil = until
	h.plots[p.ID] = p
	m.audit = append(m.audit, e)
	return p, nil
}

// ---- Postgres ----

func (s *PGPlayers) UpsertPlots(ctx context.Context, areas []Area, plots []Plot) error {
	if err := checkSeed(areas, plots); err != nil {
		return fmt.Errorf("upsert plots: %w", err)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, a := range areas {
			if _, err := tx.Exec(ctx,
				`INSERT INTO housing_areas (area_id, name, x, z) VALUES ($1, $2, $3, $4)
				 ON CONFLICT (area_id) DO UPDATE SET name = EXCLUDED.name, x = EXCLUDED.x, z = EXCLUDED.z`,
				a.ID, a.Name, float64(a.X), float64(a.Z)); err != nil {
				return fmt.Errorf("area %d: %w", a.ID, err)
			}
		}
		for _, p := range plots {
			if _, err := tx.Exec(ctx,
				`INSERT INTO house_plots (plot_id, area_id, x, z) VALUES ($1, $2, $3, $4)
				 ON CONFLICT (plot_id) DO UPDATE SET area_id = EXCLUDED.area_id, x = EXCLUDED.x, z = EXCLUDED.z`,
				p.ID, p.AreaID, float64(p.X), float64(p.Z)); err != nil {
				var pgErr = classify(err)
				if errors.Is(pgErr, ErrNotFound) {
					pgErr = fmt.Errorf("%w: %w", ErrInvalid, err)
				}
				return fmt.Errorf("plot %d: %w", p.ID, pgErr)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("upsert plots: %w", err)
	}
	return nil
}

func (s *PGPlayers) LoadPlots(ctx context.Context) ([]Plot, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT plot_id, area_id, x, z, coalesce(owner_id, 0), claimed_at, rent_paid_until
		 FROM house_plots ORDER BY plot_id`)
	if err != nil {
		return nil, fmt.Errorf("load plots: %w", err)
	}
	var out []Plot
	var p Plot
	var x, z float64
	var claimed, until *time.Time
	if _, err := pgx.ForEachRow(rows, []any{&p.ID, &p.AreaID, &x, &z, &p.OwnerID, &claimed, &until}, func() error {
		q := p
		q.X, q.Z = float32(x), float32(z)
		q.ClaimedAt, q.RentPaidUntil = timeOrZero(claimed), timeOrZero(until)
		out = append(out, q)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("load plots: %w", err)
	}
	return out, nil
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func (s *PGPlayers) LoadHome(ctx context.Context, owner int64) (Home, error) {
	var h Home
	var wall, floor *int32
	err := s.pool.QueryRow(ctx,
		`SELECT home_id, plot_id, owner_id, wallpaper_item_id, flooring_item_id, grid_size, locked
		 FROM homes WHERE owner_id = $1`, owner).Scan(&h.ID, &h.PlotID, &h.OwnerID, &wall, &floor, &h.GridSize, &h.Locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return Home{}, fmt.Errorf("load home %d: %w", owner, ErrNotFound)
	}
	if err != nil {
		return Home{}, fmt.Errorf("load home %d: %w", owner, err)
	}
	if wall != nil {
		h.WallpaperItem = *wall
	}
	if floor != nil {
		h.FlooringItem = *floor
	}
	return h, nil
}

// takeGold locks the player row and takes cost gold from it. A player who
// holds less fails with ErrInvalid, an unknown one with ErrNotFound.
func takeGold(ctx context.Context, tx pgx.Tx, player, cost int64) error {
	var gold int64
	err := tx.QueryRow(ctx, `SELECT gold FROM players WHERE id = $1 FOR UPDATE`, player).Scan(&gold)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("player %d: %w", player, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("lock player: %w", err)
	}
	if gold < cost {
		return fmt.Errorf("gold %d, cost %d: %w", gold, cost, ErrInvalid)
	}
	if _, err := tx.Exec(ctx, `UPDATE players SET gold = gold - $2, updated_at = now() WHERE id = $1`, player, cost); err != nil {
		return fmt.Errorf("gold: %w", classify(err))
	}
	return nil
}

// ClaimPlot locks the player row, then the plot row, so concurrent claims of
// one plot run one after another and the loser sees the owner.
func (s *PGPlayers) ClaimPlot(ctx context.Context, c ClaimPlot) (Plot, error) {
	if err := c.validate(); err != nil {
		return Plot{}, fmt.Errorf("claim plot %d: %w", c.PlotID, err)
	}
	var p Plot
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		p = Plot{}
		var owner *int64
		var x, z float64
		if err := tx.QueryRow(ctx, `SELECT 1 FROM players WHERE id = $1 FOR UPDATE`, c.PlayerID).Scan(new(int)); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("player %d: %w", c.PlayerID, ErrNotFound)
			}
			return fmt.Errorf("lock player: %w", err)
		}
		err := tx.QueryRow(ctx,
			`SELECT plot_id, area_id, x, z, owner_id FROM house_plots WHERE plot_id = $1 FOR UPDATE`,
			c.PlotID).Scan(&p.ID, &p.AreaID, &x, &z, &owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock plot: %w", err)
		}
		p.X, p.Z = float32(x), float32(z)
		if owner != nil {
			return fmt.Errorf("owned by %d: %w", *owner, ErrAlreadyExists)
		}
		var has bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM house_plots WHERE owner_id = $1)`, c.PlayerID).Scan(&has); err != nil {
			return fmt.Errorf("check plots: %w", err)
		}
		if has {
			return fmt.Errorf("player %d has a plot: %w", c.PlayerID, ErrAlreadyExists)
		}
		if err := takeGold(ctx, tx, c.PlayerID, c.Cost); err != nil {
			return err
		}
		until := c.Now.Add(c.Rent)
		if _, err := tx.Exec(ctx,
			`UPDATE house_plots SET owner_id = $2, claimed_at = $3, rent_paid_until = $4 WHERE plot_id = $1`,
			c.PlotID, c.PlayerID, c.Now, until); err != nil {
			return fmt.Errorf("plot: %w", classify(err))
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO homes (plot_id, owner_id, grid_size) VALUES ($1, $2, $3)`,
			c.PlotID, c.PlayerID, DefaultGridSize); err != nil {
			return fmt.Errorf("home: %w", classify(err))
		}
		if err := (PGAudit{}).Append(ctx, tx, claimEntry(c, until)); err != nil {
			return classify(err)
		}
		p.OwnerID, p.ClaimedAt, p.RentPaidUntil = c.PlayerID, c.Now, until
		return nil
	})
	if err != nil {
		return Plot{}, fmt.Errorf("claim plot %d: %w", c.PlotID, err)
	}
	return p, nil
}

func (s *PGPlayers) PayRent(ctx context.Context, r PayRent) (Plot, error) {
	if err := r.validate(); err != nil {
		return Plot{}, fmt.Errorf("pay rent %d: %w", r.PlayerID, err)
	}
	var p Plot
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		p = Plot{}
		var x, z float64
		if err := tx.QueryRow(ctx, `SELECT 1 FROM players WHERE id = $1 FOR UPDATE`, r.PlayerID).Scan(new(int)); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("player %d: %w", r.PlayerID, ErrNotFound)
			}
			return fmt.Errorf("lock player: %w", err)
		}
		err := tx.QueryRow(ctx,
			`SELECT plot_id, area_id, x, z, claimed_at, rent_paid_until FROM house_plots
			 WHERE owner_id = $1 FOR UPDATE`,
			r.PlayerID).Scan(&p.ID, &p.AreaID, &x, &z, &p.ClaimedAt, &p.RentPaidUntil)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no plot: %w", ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("lock plot: %w", err)
		}
		p.X, p.Z = float32(x), float32(z)
		if err := takeGold(ctx, tx, r.PlayerID, r.Cost); err != nil {
			return err
		}
		until := p.RentPaidUntil.Add(r.Extend)
		if _, err := tx.Exec(ctx, `UPDATE house_plots SET rent_paid_until = $2 WHERE plot_id = $1`, p.ID, until); err != nil {
			return fmt.Errorf("plot: %w", classify(err))
		}
		if err := (PGAudit{}).Append(ctx, tx, rentEntry(r, p.ID, until)); err != nil {
			return classify(err)
		}
		p.OwnerID, p.RentPaidUntil = r.PlayerID, until
		return nil
	})
	if err != nil {
		return Plot{}, fmt.Errorf("pay rent %d: %w", r.PlayerID, err)
	}
	return p, nil
}

func (s *PGPlayers) LoadHomes(ctx context.Context) ([]HomeRoom, error) {
	var out []HomeRoom
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT home_id, plot_id, owner_id, wallpaper_item_id, flooring_item_id, grid_size, locked
			 FROM homes ORDER BY home_id`)
		if err != nil {
			return err
		}
		byID := map[int64]int{}
		var h Home
		var wall, floor *int32
		if _, err := pgx.ForEachRow(rows, []any{&h.ID, &h.PlotID, &h.OwnerID, &wall, &floor, &h.GridSize, &h.Locked}, func() error {
			q := h
			q.WallpaperItem, q.FlooringItem = 0, 0
			if wall != nil {
				q.WallpaperItem = *wall
			}
			if floor != nil {
				q.FlooringItem = *floor
			}
			byID[q.ID] = len(out)
			out = append(out, HomeRoom{Home: q})
			return nil
		}); err != nil {
			return err
		}
		rows, err = tx.Query(ctx,
			`SELECT home_id, placed_id, item_id, x, y, rotation, layer, coalesce(parent_placed_id, 0), slot, side
			 FROM placed_items ORDER BY placed_id`)
		if err != nil {
			return err
		}
		var home int64
		var it PlacedItem
		var rot, slot, side int16
		var layer string
		_, err = pgx.ForEachRow(rows, []any{&home, &it.ID, &it.Item, &it.X, &it.Y, &rot, &layer, &it.ParentID, &slot, &side}, func() error {
			q := it
			q.Rotation, q.Layer, q.Slot, q.Side = uint8(rot/90), layerByName(layer), uint8(slot), uint8(side)
			i := byID[home]
			out[i].Items = append(out[i].Items, q)
			return nil
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("load homes: %w", err)
	}
	return out, nil
}

func (s *PGPlayers) ApplyHomeEdit(ctx context.Context, e HomeEdit, entries []Entry) error {
	if err := e.validate(); err != nil {
		return fmt.Errorf("apply home edit %d: %w", e.HomeID, err)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var owner int64
		err := tx.QueryRow(ctx, `SELECT owner_id FROM homes WHERE home_id = $1 FOR UPDATE`, e.HomeID).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock home: %w", err)
		}
		if owner != e.OwnerID {
			return fmt.Errorf("owner %d: %w", e.OwnerID, ErrInvalid)
		}
		var changes []EconomyChange
		if e.Holdings != nil {
			changes = []EconomyChange{{PlayerID: e.OwnerID, Holdings: *e.Holdings}}
		}
		if err := writeEconomy(ctx, tx, changes, entries); err != nil {
			return err
		}
		// Upsert parents before the items on them, and only then remove the
		// items not listed, so the cascade on parent_placed_id never takes a
		// listed item with it.
		keep := []int64{}
		for _, it := range parentsFirst(e.Items) {
			var parent *int64
			if it.ParentID != 0 {
				parent = &it.ParentID
			}
			tag, err := tx.Exec(ctx,
				`INSERT INTO placed_items (placed_id, home_id, item_id, x, y, rotation, layer, parent_placed_id, slot, side)
				 OVERRIDING SYSTEM VALUE VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				 ON CONFLICT (placed_id) DO UPDATE SET item_id = EXCLUDED.item_id, x = EXCLUDED.x, y = EXCLUDED.y,
				        rotation = EXCLUDED.rotation, layer = EXCLUDED.layer, parent_placed_id = EXCLUDED.parent_placed_id,
				        slot = EXCLUDED.slot, side = EXCLUDED.side
				 WHERE placed_items.home_id = EXCLUDED.home_id`,
				it.ID, e.HomeID, it.Item, it.X, it.Y, int16(it.Rotation)*90, layerNames[it.Layer], parent, int16(it.Slot), int16(it.Side))
			if err != nil {
				return fmt.Errorf("place item %d: %w", it.ID, classify(err))
			}
			if tag.RowsAffected() == 0 {
				return fmt.Errorf("item %d is in another home: %w", it.ID, ErrInvalid)
			}
			keep = append(keep, it.ID)
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM placed_items WHERE home_id = $1 AND NOT (placed_id = ANY($2))`, e.HomeID, keep); err != nil {
			return fmt.Errorf("remove items: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE homes SET wallpaper_item_id = NULLIF($2, 0), flooring_item_id = NULLIF($3, 0) WHERE home_id = $1`,
			e.HomeID, e.WallpaperItem, e.FlooringItem); err != nil {
			return fmt.Errorf("home: %w", classify(err))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("apply home edit %d: %w", e.HomeID, err)
	}
	return nil
}

func (s *PGPlayers) SetHomeLocked(ctx context.Context, homeID int64, locked bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE homes SET locked = $2 WHERE home_id = $1`, homeID, locked)
	if err != nil {
		return fmt.Errorf("set home %d locked: %w", homeID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("set home %d locked: %w", homeID, ErrNotFound)
	}
	return nil
}
