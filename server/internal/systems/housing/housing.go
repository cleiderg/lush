// Package housing holds the pure rules for owning a house
// (docs/specs/housing.md, Neighborhoods and ownership): claiming an empty
// house, prepaying rent, the warning before a lapse, and the lapse that sends
// a house to auction. Every number comes from gamedata.Rules. The caller
// moves the gold (as one store transaction with an audit entry) and persists
// the returned House; nothing here touches the clock, the store or the network.
package housing

import (
	"errors"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// Economy audit log reasons.
const (
	ReasonClaim = "house_claim"
	ReasonRent  = "house_rent"
)

// State is where a house stands against its prepaid rent.
type State int

const (
	Empty   State = iota // nobody owns it; claimable
	Owned                // at least one month plus the warning period prepaid
	Warning              // fewer than the warning days left before dropping below one month
	Lapsed               // less than one month prepaid; goes to auction
)

func (s State) String() string {
	return [...]string{"Empty", "Owned", "Warning", "Lapsed"}[s]
}

var (
	ErrOwned            = errors.New("housing: house is already owned")
	ErrNotOwner         = errors.New("housing: not the owner of this house")
	ErrAlreadyOwnsHouse = errors.New("housing: player already owns a house")
	ErrNotEnoughGold    = errors.New("housing: not enough gold")
	ErrTooManyMonths    = errors.New("housing: rent would be prepaid too far ahead")
	ErrBadMonths        = errors.New("housing: months must be at least 1")
	ErrLapsed           = errors.New("housing: house has lapsed")
)

// House is the ownership part of a house plot.
type House struct {
	Owner     world.PlayerID // 0 when empty
	PaidUntil time.Time
}

// Rules applies the rent settings.
type Rules struct{ r gamedata.Rules }

// New returns the rent rules for r.
func New(r gamedata.Rules) Rules { return Rules{r} }

func (r Rules) month() time.Duration {
	return time.Duration(r.r.HouseDaysPerMonth) * 24 * time.Hour
}

// AuctionDuration is how long a lapsed house stays up for auction.
func (r Rules) AuctionDuration() time.Duration {
	return time.Duration(r.r.HouseAuctionDays) * 24 * time.Hour
}

// ClaimCost is the gold to claim an empty house.
func (r Rules) ClaimCost() int { return r.r.HouseClaimMonths * r.r.HouseRentPerMonth }

// State reports the house's state at now. The house lapses once less than one
// month is prepaid, and warns while fewer than the warning days remain before
// that point.
func (r Rules) State(h House, now time.Time) State {
	if h.Owner == 0 {
		return Empty
	}
	left := h.PaidUntil.Sub(now)
	switch {
	case left < r.month():
		return Lapsed
	case left-r.month() < time.Duration(r.r.HouseWarningDays)*24*time.Hour:
		return Warning
	}
	return Owned
}

// Claim has player (owning ownedHouse houses already, with gold) claim the
// empty house h. It returns the new house and the gold to take.
func (r Rules) Claim(h House, player world.PlayerID, ownsHouse bool, gold int, now time.Time) (House, int, error) {
	switch {
	case h.Owner != 0:
		return h, 0, ErrOwned
	case ownsHouse:
		return h, 0, ErrAlreadyOwnsHouse
	case gold < r.ClaimCost():
		return h, 0, ErrNotEnoughGold
	}
	return House{Owner: player, PaidUntil: now.Add(time.Duration(r.r.HouseClaimMonths) * r.month())}, r.ClaimCost(), nil
}

// PayRent buys months whole months of rent for the owner. It is rejected when
// the house has lapsed or when paid-until would pass now plus the maximum
// prepaid months. It returns the new house and the gold to take.
func (r Rules) PayRent(h House, player world.PlayerID, months, gold int, now time.Time) (House, int, error) {
	switch {
	case h.Owner == 0 || h.Owner != player:
		return h, 0, ErrNotOwner
	case months < 1:
		return h, 0, ErrBadMonths
	case r.State(h, now) == Lapsed:
		return h, 0, ErrLapsed
	}
	paid := h.PaidUntil.Add(time.Duration(months) * r.month())
	if paid.Sub(now) > time.Duration(r.r.HouseMaxPrepaidMonths)*r.month() {
		return h, 0, ErrTooManyMonths
	}
	cost := months * r.r.HouseRentPerMonth
	if gold < cost {
		return h, 0, ErrNotEnoughGold
	}
	h.PaidUntil = paid
	return h, cost, nil
}
