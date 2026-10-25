-- +goose Up
ALTER TABLE players
    ADD COLUMN gold      bigint NOT NULL DEFAULT 0 CONSTRAINT players_gold_nonnegative CHECK (gold >= 0),
    ADD COLUMN combat_xp bigint NOT NULL DEFAULT 0 CONSTRAINT players_combat_xp_nonnegative CHECK (combat_xp >= 0);

-- One row per occupied inventory slot; an empty slot has no row.
CREATE TABLE player_inventory (
    player_id bigint   NOT NULL REFERENCES players (id) ON DELETE CASCADE,
    slot      smallint NOT NULL CONSTRAINT player_inventory_slot_range CHECK (slot BETWEEN 0 AND 27),
    item_id   integer  NOT NULL CONSTRAINT player_inventory_item_positive CHECK (item_id > 0),
    quantity  bigint   NOT NULL CONSTRAINT player_inventory_quantity_positive CHECK (quantity > 0),
    PRIMARY KEY (player_id, slot)
);

-- One row per occupied equipment slot; an empty slot has no row.
CREATE TABLE player_equipment (
    player_id bigint  NOT NULL REFERENCES players (id) ON DELETE CASCADE,
    slot      text    NOT NULL CONSTRAINT player_equipment_slot_name CHECK (slot IN ('weapon', 'head', 'body', 'shield')),
    item_id   integer NOT NULL CONSTRAINT player_equipment_item_positive CHECK (item_id > 0),
    PRIMARY KEY (player_id, slot)
);

-- +goose Down
DROP TABLE player_equipment;
DROP TABLE player_inventory;
ALTER TABLE players DROP COLUMN combat_xp, DROP COLUMN gold;
