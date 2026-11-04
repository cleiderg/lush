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

// AuctionBid is one player's bid on an auction. Held is the gold held for it:
// all of Amount while it is the top bid, 0 once the bidder is outbid.
type AuctionBid struct {
	Player int64
	Amount int64
	Held   int64
}

// Auction is an open house auction. Bids are ordered from the highest amount.
// PreviousOwner is 0 when there is none.
type Auction struct {
	PlotID        int32
	PreviousOwner int64
	EndsAt        time.Time
	Bids          []AuctionBid
}

// PlaceAuctionBid is a bid of Amount by Player on the auction of PlotID.
type PlaceAuctionBid struct {
	PlotID int32
	Player int64
	Amount int64
}

// FinishAuction settles the auction of PlotID. The winner pays their bid and
// RentCost gold, and the plot is paid until Now+Rent.
type FinishAuction struct {
	PlotID   int32
	RentCost int64
	Rent     time.Duration
	Now      time.Time
}

// AuctionRefund is held gold returned to a bidder.
type AuctionRefund struct {
	Player int64
	Gold   int64
}

// AuctionResult is the outcome of FinishAuction. Winner is 0 when nobody
// could take the house, which is then empty and claimable.
type AuctionResult struct {
	Plot    Plot
	Winner  int64
	Bid     int64
	Rent    int64
	Refunds []AuctionRefund
}

// Audit kinds written by the auction methods. Hold and refund payloads have
// the plot id and the gold; the win has the plot, the bid and the rent.
const (
	AuctionHoldKind   = "house_auction_hold"
	AuctionRefundKind = "house_auction_refund"
	AuctionWinKind    = "house_auction_win"
)

// Auctions stores open house auctions and the gold held for their bids.
type Auctions interface {
	// OpenAuction creates an auction on an empty plot. An unknown plot or
	// previous owner fails with ErrNotFound; a plot with an owner or an open
	// auction with ErrAlreadyExists. previousOwner 0 means none.
	OpenAuction(ctx context.Context, plot int32, previousOwner int64, endsAt time.Time) error
	// PlaceAuctionBid, in one transaction, takes the held gold from the bidder
	// (only the difference when raising their own bid), returns the gold held
	// for every other bid and appends a hold entry and a refund entry for
	// each. An unknown auction or bidder fails with ErrNotFound; an amount not
	// above the top bid, or too little gold, with ErrInvalid. On any error
	// nothing changes. It returns the auction.
	PlaceAuctionBid(ctx context.Context, b PlaceAuctionBid) (Auction, error)
	// FinishAuction, in one transaction, goes down the bids from the highest
	// and gives the plot to the first bidder who owns no plot and can pay the
	// bid plus f.RentCost from their spendable gold and held bid: it sets the
	// owner, claimed_at and rent_paid_until, creates the home, refunds every
	// other held bid, removes the auction and appends audit entries. With no
	// taker the plot is left empty and all held gold is refunded. An unknown
	// auction fails with ErrNotFound. On any error nothing changes.
	FinishAuction(ctx context.Context, f FinishAuction) (AuctionResult, error)
	// LoadAuctions returns every open auction with its bids, by plot id.
	LoadAuctions(ctx context.Context) ([]Auction, error)
}

var (
	_ Auctions = (*Memory)(nil)
	_ Auctions = (*PGPlayers)(nil)
)

func (f FinishAuction) validate() error {
	if f.RentCost < 0 || f.Rent <= 0 {
		return fmt.Errorf("rent cost %d rent %s: %w", f.RentCost, f.Rent, ErrInvalid)
	}
	return nil
}

func (b PlaceAuctionBid) validate() error {
	if b.Amount <= 0 {
		return fmt.Errorf("amount %d: %w", b.Amount, ErrInvalid)
	}
	return nil
}

func auctionEntry(kind string, player int64, plot int32, gold int64) Entry {
	return Entry{Kind: kind, Actor: player, Payload: map[string]any{"plot": plot, "gold": gold}}
}

func winEntry(winner int64, plot int32, bid, rent int64, until time.Time) Entry {
	return Entry{Kind: AuctionWinKind, Actor: winner,
		Payload: map[string]any{"plot": plot, "bid": bid, "rent": rent, "rent_paid_until": until.UTC()}}
}

// sortBids orders bids from the highest amount, earlier bids first on a tie.
// The input is in the order the bids were first placed.
func sortBids(bids []AuctionBid) []AuctionBid {
	out := slices.Clone(bids)
	slices.SortStableFunc(out, func(a, b AuctionBid) int { return cmp.Compare(b.Amount, a.Amount) })
	return out
}

// ---- Memory ----

type memAuction struct {
	previous int64
	endsAt   time.Time
	bids     []AuctionBid // in the order first placed
}

func (a *memAuction) view(plot int32) Auction {
	return Auction{PlotID: plot, PreviousOwner: a.previous, EndsAt: a.endsAt, Bids: sortBids(a.bids)}
}

func (m *Memory) OpenAuction(ctx context.Context, plot int32, previousOwner int64, endsAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("open auction %d: %w", plot, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.housingLocked()
	p, ok := h.plots[plot]
	if !ok {
		return fmt.Errorf("open auction %d: %w", plot, ErrNotFound)
	}
	if previousOwner != 0 {
		if _, ok := m.byID[previousOwner]; !ok {
			return fmt.Errorf("open auction %d: player %d: %w", plot, previousOwner, ErrNotFound)
		}
	}
	if p.OwnerID != 0 {
		return fmt.Errorf("open auction %d: owned by %d: %w", plot, p.OwnerID, ErrAlreadyExists)
	}
	if _, open := h.auctions[plot]; open {
		return fmt.Errorf("open auction %d: already open: %w", plot, ErrAlreadyExists)
	}
	h.auctions[plot] = &memAuction{previous: previousOwner, endsAt: endsAt}
	return nil
}

func (m *Memory) PlaceAuctionBid(ctx context.Context, b PlaceAuctionBid) (Auction, error) {
	if err := ctx.Err(); err != nil {
		return Auction{}, fmt.Errorf("bid on auction %d: %w", b.PlotID, err)
	}
	if err := b.validate(); err != nil {
		return Auction{}, fmt.Errorf("bid on auction %d: %w", b.PlotID, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.housingLocked().auctions[b.PlotID]
	if !ok {
		return Auction{}, fmt.Errorf("bid on auction %d: %w", b.PlotID, ErrNotFound)
	}
	pl, ok := m.byID[b.Player]
	if !ok {
		return Auction{}, fmt.Errorf("bid on auction %d: player %d: %w", b.PlotID, b.Player, ErrNotFound)
	}
	if bids := sortBids(a.bids); len(bids) > 0 && b.Amount <= bids[0].Amount {
		return Auction{}, fmt.Errorf("bid on auction %d: amount %d, top %d: %w", b.PlotID, b.Amount, bids[0].Amount, ErrInvalid)
	}
	var own int64
	for _, x := range a.bids {
		if x.Player == b.Player {
			own = x.Held
		}
	}
	hold := b.Amount - own
	if pl.Gold < hold {
		return Auction{}, fmt.Errorf("bid on auction %d: gold %d, hold %d: %w", b.PlotID, pl.Gold, hold, ErrInvalid)
	}
	entries := []Entry{auctionEntry(AuctionHoldKind, b.Player, b.PlotID, hold)}
	for _, x := range a.bids {
		if x.Player != b.Player && x.Held > 0 {
			entries = append(entries, auctionEntry(AuctionRefundKind, x.Player, b.PlotID, x.Held))
		}
	}
	for _, e := range entries {
		if err := checkPayload(e); err != nil {
			return Auction{}, fmt.Errorf("bid on auction %d: %w", b.PlotID, err)
		}
	}
	pl.Gold -= hold
	m.byID[pl.ID] = pl
	found := false
	for i, x := range a.bids {
		switch {
		case x.Player == b.Player:
			a.bids[i].Amount, a.bids[i].Held = b.Amount, b.Amount
			found = true
		case x.Held > 0:
			other := m.byID[x.Player]
			other.Gold += x.Held
			m.byID[x.Player] = other
			a.bids[i].Held = 0
		}
	}
	if !found {
		a.bids = append(a.bids, AuctionBid{Player: b.Player, Amount: b.Amount, Held: b.Amount})
	}
	m.audit = append(m.audit, entries...)
	return a.view(b.PlotID), nil
}

func (m *Memory) FinishAuction(ctx context.Context, f FinishAuction) (AuctionResult, error) {
	if err := ctx.Err(); err != nil {
		return AuctionResult{}, fmt.Errorf("finish auction %d: %w", f.PlotID, err)
	}
	if err := f.validate(); err != nil {
		return AuctionResult{}, fmt.Errorf("finish auction %d: %w", f.PlotID, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.housingLocked()
	a, ok := h.auctions[f.PlotID]
	if !ok {
		return AuctionResult{}, fmt.Errorf("finish auction %d: %w", f.PlotID, ErrNotFound)
	}
	res := AuctionResult{Plot: h.plots[f.PlotID]}
	var winner *AuctionBid
	for _, b := range sortBids(a.bids) {
		_, owns := h.homes[b.Player]
		if !owns && m.byID[b.Player].Gold+b.Held >= b.Amount+f.RentCost {
			winner = &b
			break
		}
	}
	var entries []Entry
	for _, b := range a.bids {
		if b.Held > 0 && (winner == nil || b.Player != winner.Player) {
			res.Refunds = append(res.Refunds, AuctionRefund{b.Player, b.Held})
			entries = append(entries, auctionEntry(AuctionRefundKind, b.Player, f.PlotID, b.Held))
		}
	}
	until := f.Now.Add(f.Rent)
	if winner != nil {
		entries = append(entries, winEntry(winner.Player, f.PlotID, winner.Amount, f.RentCost, until))
	}
	for _, e := range entries {
		if err := checkPayload(e); err != nil {
			return AuctionResult{}, fmt.Errorf("finish auction %d: %w", f.PlotID, err)
		}
	}
	for _, r := range res.Refunds {
		pl := m.byID[r.Player]
		pl.Gold += r.Gold
		m.byID[r.Player] = pl
	}
	if winner != nil {
		pl := m.byID[winner.Player]
		pl.Gold -= winner.Amount + f.RentCost - winner.Held
		m.byID[winner.Player] = pl
		p := res.Plot
		p.OwnerID, p.ClaimedAt, p.RentPaidUntil = winner.Player, f.Now, until
		h.plots[p.ID] = p
		h.homes[winner.Player] = Home{ID: h.nextID, PlotID: p.ID, OwnerID: winner.Player, GridSize: DefaultGridSize}
		h.nextID++
		res.Plot, res.Winner, res.Bid, res.Rent = p, winner.Player, winner.Amount, f.RentCost
	}
	delete(h.auctions, f.PlotID)
	m.audit = append(m.audit, entries...)
	return res, nil
}

func (m *Memory) LoadAuctions(ctx context.Context) ([]Auction, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load auctions: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.housingLocked()
	var out []Auction
	for _, plot := range slices.Sorted(maps.Keys(h.auctions)) {
		out = append(out, h.auctions[plot].view(plot))
	}
	return out, nil
}

// ---- Postgres ----

// lockPlayers locks the player rows in id order, so transactions that touch
// the same players never wait on each other in a cycle. It returns their gold.
func lockPlayers(ctx context.Context, tx pgx.Tx, ids []int64) (map[int64]int64, error) {
	rows, err := tx.Query(ctx, `SELECT id, gold FROM players WHERE id = ANY($1) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return nil, fmt.Errorf("lock players: %w", err)
	}
	gold := map[int64]int64{}
	var id, g int64
	if _, err := pgx.ForEachRow(rows, []any{&id, &g}, func() error {
		gold[id] = g
		return nil
	}); err != nil {
		return nil, fmt.Errorf("lock players: %w", err)
	}
	return gold, nil
}

func (s *PGPlayers) OpenAuction(ctx context.Context, plot int32, previousOwner int64, endsAt time.Time) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var owner *int64
		err := tx.QueryRow(ctx, `SELECT owner_id FROM house_plots WHERE plot_id = $1 FOR UPDATE`, plot).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock plot: %w", err)
		}
		if owner != nil {
			return fmt.Errorf("owned by %d: %w", *owner, ErrAlreadyExists)
		}
		var open bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM house_auctions WHERE plot_id = $1)`, plot).Scan(&open); err != nil {
			return fmt.Errorf("check auction: %w", err)
		}
		if open {
			return fmt.Errorf("already open: %w", ErrAlreadyExists)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO house_auctions (plot_id, previous_owner_id, ends_at) VALUES ($1, NULLIF($2, 0), $3)`,
			plot, previousOwner, endsAt); err != nil {
			return fmt.Errorf("auction: %w", classify(err))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("open auction %d: %w", plot, err)
	}
	return nil
}

// lockAuction locks the auction row of the plot and returns its id.
func lockAuction(ctx context.Context, tx pgx.Tx, plot int32) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `SELECT auction_id FROM house_auctions WHERE plot_id = $1 FOR UPDATE`, plot).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("lock auction: %w", err)
	}
	return id, nil
}

// loadBids returns the auction's bids, highest first.
func loadBids(ctx context.Context, tx pgx.Tx, auction int64) ([]AuctionBid, error) {
	rows, err := tx.Query(ctx,
		`SELECT player_id, amount, held FROM house_bids WHERE auction_id = $1 ORDER BY amount DESC, bid_id`, auction)
	if err != nil {
		return nil, fmt.Errorf("load bids: %w", err)
	}
	var out []AuctionBid
	var b AuctionBid
	if _, err := pgx.ForEachRow(rows, []any{&b.Player, &b.Amount, &b.Held}, func() error {
		out = append(out, b)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("load bids: %w", err)
	}
	return out, nil
}

func (s *PGPlayers) PlaceAuctionBid(ctx context.Context, b PlaceAuctionBid) (Auction, error) {
	if err := b.validate(); err != nil {
		return Auction{}, fmt.Errorf("bid on auction %d: %w", b.PlotID, err)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		id, err := lockAuction(ctx, tx, b.PlotID)
		if err != nil {
			return err
		}
		bids, err := loadBids(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(bids) > 0 && b.Amount <= bids[0].Amount {
			return fmt.Errorf("amount %d, top %d: %w", b.Amount, bids[0].Amount, ErrInvalid)
		}
		ids := []int64{b.Player}
		var own int64
		var outbid []AuctionBid
		for _, x := range bids {
			switch {
			case x.Player == b.Player:
				own = x.Held
			case x.Held > 0:
				outbid = append(outbid, x)
				ids = append(ids, x.Player)
			}
		}
		gold, err := lockPlayers(ctx, tx, ids)
		if err != nil {
			return err
		}
		hold := b.Amount - own
		have, ok := gold[b.Player]
		if !ok {
			return fmt.Errorf("player %d: %w", b.Player, ErrNotFound)
		}
		if have < hold {
			return fmt.Errorf("gold %d, hold %d: %w", have, hold, ErrInvalid)
		}
		if _, err := tx.Exec(ctx, `UPDATE players SET gold = gold - $2, updated_at = now() WHERE id = $1`, b.Player, hold); err != nil {
			return fmt.Errorf("hold: %w", classify(err))
		}
		audit := []Entry{auctionEntry(AuctionHoldKind, b.Player, b.PlotID, hold)}
		for _, x := range outbid {
			if _, err := tx.Exec(ctx, `UPDATE players SET gold = gold + $2, updated_at = now() WHERE id = $1`, x.Player, x.Held); err != nil {
				return fmt.Errorf("refund: %w", classify(err))
			}
			if _, err := tx.Exec(ctx, `UPDATE house_bids SET held = 0 WHERE auction_id = $1 AND player_id = $2`, id, x.Player); err != nil {
				return fmt.Errorf("refund bid: %w", classify(err))
			}
			audit = append(audit, auctionEntry(AuctionRefundKind, x.Player, b.PlotID, x.Held))
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO house_bids (auction_id, player_id, amount, held) VALUES ($1, $2, $3, $3)
			 ON CONFLICT (auction_id, player_id) DO UPDATE SET amount = EXCLUDED.amount, held = EXCLUDED.held`,
			id, b.Player, b.Amount); err != nil {
			return fmt.Errorf("bid: %w", classify(err))
		}
		for _, e := range audit {
			if err := (PGAudit{}).Append(ctx, tx, e); err != nil {
				return classify(err)
			}
		}
		return nil
	})
	if err != nil {
		return Auction{}, fmt.Errorf("bid on auction %d: %w", b.PlotID, err)
	}
	all, err := s.LoadAuctions(ctx)
	if err != nil {
		return Auction{}, fmt.Errorf("bid on auction %d: %w", b.PlotID, err)
	}
	for _, a := range all {
		if a.PlotID == b.PlotID {
			return a, nil
		}
	}
	return Auction{PlotID: b.PlotID}, nil
}

func (s *PGPlayers) FinishAuction(ctx context.Context, f FinishAuction) (AuctionResult, error) {
	if err := f.validate(); err != nil {
		return AuctionResult{}, fmt.Errorf("finish auction %d: %w", f.PlotID, err)
	}
	var res AuctionResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		res = AuctionResult{}
		id, err := lockAuction(ctx, tx, f.PlotID)
		if err != nil {
			return err
		}
		bids, err := loadBids(ctx, tx, id)
		if err != nil {
			return err
		}
		ids := make([]int64, 0, len(bids))
		for _, b := range bids {
			ids = append(ids, b.Player)
		}
		gold, err := lockPlayers(ctx, tx, ids)
		if err != nil {
			return err
		}
		// The players are locked, so nobody here can claim a plot meanwhile.
		owns := map[int64]bool{}
		rows, err := tx.Query(ctx, `SELECT owner_id FROM house_plots WHERE owner_id = ANY($1)`, ids)
		if err != nil {
			return fmt.Errorf("check owners: %w", err)
		}
		var owner int64
		if _, err := pgx.ForEachRow(rows, []any{&owner}, func() error {
			owns[owner] = true
			return nil
		}); err != nil {
			return fmt.Errorf("check owners: %w", err)
		}
		var winner *AuctionBid
		for _, b := range bids {
			if !owns[b.Player] && gold[b.Player]+b.Held >= b.Amount+f.RentCost {
				winner = &b
				break
			}
		}
		var x, z float64
		if err := tx.QueryRow(ctx, `SELECT plot_id, area_id, x, z FROM house_plots WHERE plot_id = $1 FOR UPDATE`, f.PlotID).
			Scan(&res.Plot.ID, &res.Plot.AreaID, &x, &z); err != nil {
			return fmt.Errorf("lock plot: %w", err)
		}
		res.Plot.X, res.Plot.Z = float32(x), float32(z)
		var audit []Entry
		for _, b := range bids {
			if b.Held == 0 || (winner != nil && b.Player == winner.Player) {
				continue
			}
			if _, err := tx.Exec(ctx, `UPDATE players SET gold = gold + $2, updated_at = now() WHERE id = $1`, b.Player, b.Held); err != nil {
				return fmt.Errorf("refund: %w", classify(err))
			}
			res.Refunds = append(res.Refunds, AuctionRefund{b.Player, b.Held})
			audit = append(audit, auctionEntry(AuctionRefundKind, b.Player, f.PlotID, b.Held))
		}
		if winner != nil {
			until := f.Now.Add(f.Rent)
			if _, err := tx.Exec(ctx, `UPDATE players SET gold = gold - $2, updated_at = now() WHERE id = $1`,
				winner.Player, winner.Amount+f.RentCost-winner.Held); err != nil {
				return fmt.Errorf("payment: %w", classify(err))
			}
			if _, err := tx.Exec(ctx,
				`UPDATE house_plots SET owner_id = $2, claimed_at = $3, rent_paid_until = $4 WHERE plot_id = $1`,
				f.PlotID, winner.Player, f.Now, until); err != nil {
				return fmt.Errorf("plot: %w", classify(err))
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO homes (plot_id, owner_id, grid_size) VALUES ($1, $2, $3)`,
				f.PlotID, winner.Player, DefaultGridSize); err != nil {
				return fmt.Errorf("home: %w", classify(err))
			}
			audit = append(audit, winEntry(winner.Player, f.PlotID, winner.Amount, f.RentCost, until))
			res.Plot.OwnerID, res.Plot.ClaimedAt, res.Plot.RentPaidUntil = winner.Player, f.Now, until
			res.Winner, res.Bid, res.Rent = winner.Player, winner.Amount, f.RentCost
		}
		if _, err := tx.Exec(ctx, `DELETE FROM house_auctions WHERE auction_id = $1`, id); err != nil {
			return fmt.Errorf("remove auction: %w", err)
		}
		for _, e := range audit {
			if err := (PGAudit{}).Append(ctx, tx, e); err != nil {
				return classify(err)
			}
		}
		return nil
	})
	if err != nil {
		return AuctionResult{}, fmt.Errorf("finish auction %d: %w", f.PlotID, err)
	}
	return res, nil
}

func (s *PGPlayers) LoadAuctions(ctx context.Context) ([]Auction, error) {
	var out []Auction
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		out = nil
		rows, err := tx.Query(ctx,
			`SELECT auction_id, plot_id, coalesce(previous_owner_id, 0), ends_at FROM house_auctions ORDER BY plot_id`)
		if err != nil {
			return err
		}
		var ids []int64
		var id int64
		var a Auction
		if _, err := pgx.ForEachRow(rows, []any{&id, &a.PlotID, &a.PreviousOwner, &a.EndsAt}, func() error {
			ids = append(ids, id)
			out = append(out, a)
			return nil
		}); err != nil {
			return err
		}
		for i, id := range ids {
			if out[i].Bids, err = loadBids(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load auctions: %w", err)
	}
	return out, nil
}
