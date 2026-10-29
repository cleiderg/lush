-- +goose Up
-- A card pot holds a staked card game's stakes. It is opened in the
-- transaction that takes every seated player's stake and closed in the one
-- that pays the winner, or that refunds the last stake still in it. A pot
-- still open at startup is refunded before players join (card-tables spec).
CREATE TABLE card_pots (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    table_id  integer NOT NULL,
    opened_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    closed_by text CONSTRAINT card_pots_closed_by_kind CHECK (closed_by IN ('payout', 'refund')),
    CONSTRAINT card_pots_closed_by_set CHECK ((closed_at IS NULL) = (closed_by IS NULL))
);

CREATE INDEX card_pots_open_idx ON card_pots (id) WHERE closed_at IS NULL;

-- One row per player staked in a pot; refunded_at is set when that stake
-- goes back to the player.
CREATE TABLE card_pot_stakes (
    pot_id      bigint NOT NULL REFERENCES card_pots (id) ON DELETE CASCADE,
    player_id   bigint NOT NULL REFERENCES players (id),
    stake       bigint NOT NULL CONSTRAINT card_pot_stakes_stake_positive CHECK (stake > 0),
    refunded_at timestamptz,
    PRIMARY KEY (pot_id, player_id)
);

-- +goose Down
DROP TABLE card_pot_stakes;
DROP TABLE card_pots;
