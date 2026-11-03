package store

import (
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
	homes  map[int64]Home // by owner
	nextID int64
}

func (m *Memory) housingLocked() *memHousing {
	if m.housing == nil {
		m.housing = &memHousing{areas: map[int32]Area{}, plots: map[int32]Plot{}, homes: map[int64]Home{}, nextID: 1}
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
		`SELECT home_id, plot_id, owner_id, wallpaper_item_id, flooring_item_id, grid_size
		 FROM homes WHERE owner_id = $1`, owner).Scan(&h.ID, &h.PlotID, &h.OwnerID, &wall, &floor, &h.GridSize)
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
