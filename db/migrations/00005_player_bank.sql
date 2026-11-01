-- +goose Up
-- One row per item a player has banked. Every item stacks in the bank, so an
-- item id appears at most once per player; an item with nothing banked has no
-- row (skills spec, Bank). The slot limit is game data, not a constraint.
CREATE TABLE player_bank (
    player_id bigint  NOT NULL REFERENCES players (id) ON DELETE CASCADE,
    item_id   integer NOT NULL CONSTRAINT player_bank_item_positive CHECK (item_id > 0),
    quantity  bigint  NOT NULL CONSTRAINT player_bank_quantity_positive CHECK (quantity > 0),
    PRIMARY KEY (player_id, item_id)
);

-- +goose Down
DROP TABLE player_bank;
