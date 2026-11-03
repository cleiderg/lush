-- +goose Up
-- House auctions (housing spec, Neighborhoods and ownership). An auction is
-- open on an empty plot until it is finished; each player has one bid on it.
-- held is the gold taken from the bidder for the bid: all of amount while it
-- is the top bid, 0 once the bidder is outbid and refunded.
CREATE TABLE house_auctions (
    auction_id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    plot_id           integer NOT NULL UNIQUE REFERENCES house_plots (plot_id),
    previous_owner_id bigint REFERENCES players (id),
    ends_at           timestamptz NOT NULL
);

CREATE TABLE house_bids (
    bid_id     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    auction_id bigint NOT NULL REFERENCES house_auctions (auction_id) ON DELETE CASCADE,
    player_id  bigint NOT NULL REFERENCES players (id),
    amount     bigint NOT NULL CONSTRAINT house_bids_amount_positive CHECK (amount > 0),
    held       bigint NOT NULL CONSTRAINT house_bids_held_range CHECK (held >= 0 AND held <= amount),
    CONSTRAINT house_bids_one_per_player UNIQUE (auction_id, player_id)
);

-- +goose Down
DROP TABLE house_bids;
DROP TABLE house_auctions;
