package housing

import (
	"errors"
	"sort"
	"time"

	"github.com/cleiderg/lush/server/internal/world"
)

// Economy audit log reasons for auctions.
const (
	ReasonAuctionHold   = "house_auction_hold"
	ReasonAuctionRefund = "house_auction_refund"
	ReasonAuctionWin    = "house_auction_win"
)

var (
	ErrBidTooLow     = errors.New("housing: bid is below one month's rent")
	ErrNotAboveTop   = errors.New("housing: bid is not above the top bid")
	ErrPreviousOwner = errors.New("housing: previous owner cannot bid")
	ErrAuctionOver   = errors.New("housing: auction has ended")
)

// Bid is one player's highest bid. Held is the gold currently held for it:
// the full amount while the bid is the top bid, 0 once the player is outbid.
type Bid struct {
	Player world.PlayerID
	Amount int
	Held   int
}

// Auction is a house up for auction. Each player has at most one entry in Bids.
type Auction struct {
	PreviousOwner world.PlayerID
	EndsAt        time.Time
	Bids          []Bid
}

// Refund is held gold to return to a player.
type Refund struct {
	Player world.PlayerID
	Gold   int
}

// Wallet is what the caller knows about a bidder at resolution time. Gold is
// the spendable gold, not counting gold held for the auction.
type Wallet struct {
	Gold      int
	OwnsHouse bool
}

// StartAuction opens the auction for a house whose rent lapsed.
func (r Rules) StartAuction(previous world.PlayerID, now time.Time) Auction {
	return Auction{PreviousOwner: previous, EndsAt: now.Add(r.AuctionDuration())}
}

func (a Auction) top() (Bid, bool) {
	var best Bid
	found := false
	for _, b := range a.Bids {
		if !found || b.Amount > best.Amount {
			best, found = b, true
		}
	}
	return best, found
}

// PlaceBid has player bid amount. gold is their spendable gold (not counting
// gold they already hold here). It returns the new auction, the gold to newly
// hold (only the difference when raising their own bid), and the refund for
// the bidder it outbids. The input auction is not modified.
func (r Rules) PlaceBid(a Auction, player world.PlayerID, amount int, ownsHouse bool, gold int, now time.Time) (Auction, int, []Refund, error) {
	top, hasTop := a.top()
	var mine *Bid
	for i := range a.Bids {
		if a.Bids[i].Player == player {
			mine = &a.Bids[i]
		}
	}
	hold := amount
	if mine != nil {
		hold -= mine.Held
	}
	switch {
	case !now.Before(a.EndsAt):
		return a, 0, nil, ErrAuctionOver
	case player == a.PreviousOwner:
		return a, 0, nil, ErrPreviousOwner
	case ownsHouse:
		return a, 0, nil, ErrAlreadyOwnsHouse
	case amount < r.r.HouseRentPerMonth:
		return a, 0, nil, ErrBidTooLow
	case hasTop && amount <= top.Amount:
		return a, 0, nil, ErrNotAboveTop
	case gold < hold:
		return a, 0, nil, ErrNotEnoughGold
	}
	out := Auction{PreviousOwner: a.PreviousOwner, EndsAt: a.EndsAt}
	var refunds []Refund
	for _, b := range a.Bids {
		if b.Player == player {
			continue
		}
		if b.Held > 0 {
			refunds = append(refunds, Refund{b.Player, b.Held})
			b.Held = 0
		}
		out.Bids = append(out.Bids, b)
	}
	out.Bids = append(out.Bids, Bid{Player: player, Amount: amount, Held: amount})
	return out, hold, refunds, nil
}

// Result is the outcome of resolving an auction.
type Result struct {
	Done    bool  // the auction had ended
	House   House // the house afterwards; Owner 0 means empty and claimable
	Winner  world.PlayerID
	Bid     int // winning bid, a gold sink
	Rent    int // two months' rent, a gold sink
	Take    int // gold to take from the winner's wallet (Bid+Rent minus what was held)
	Refunds []Refund
}

// Resolve settles the auction at now. Before EndsAt nothing changes. Bidders
// are offered the house from the highest bid down; one who can't pay the bid
// plus two months' rent from their wallet and held gold, or who owns another
// house by then, is skipped. A skipped bidder's held gold is refunded. With no
// taker the house is left empty and claimable.
func (r Rules) Resolve(a Auction, wallet func(world.PlayerID) Wallet, now time.Time) Result {
	if now.Before(a.EndsAt) {
		return Result{}
	}
	res := Result{Done: true}
	bids := append([]Bid(nil), a.Bids...)
	sort.SliceStable(bids, func(i, j int) bool { return bids[i].Amount > bids[j].Amount })
	rent := r.ClaimCost()
	for i, b := range bids {
		w := wallet(b.Player)
		total := b.Amount + rent
		if w.OwnsHouse || w.Gold+b.Held < total {
			if b.Held > 0 {
				res.Refunds = append(res.Refunds, Refund{b.Player, b.Held})
			}
			continue
		}
		res.Winner, res.Bid, res.Rent, res.Take = b.Player, b.Amount, rent, total-b.Held
		res.House = House{Owner: b.Player, PaidUntil: now.Add(time.Duration(r.r.HouseClaimMonths) * r.month())}
		for _, rest := range bids[i+1:] {
			if rest.Held > 0 {
				res.Refunds = append(res.Refunds, Refund{rest.Player, rest.Held})
			}
		}
		return res
	}
	return res
}
