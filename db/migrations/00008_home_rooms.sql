-- +goose Up
-- Home rooms (housing spec, Room and grid model, Social). A surface item sits
-- in a slot of its parent, a wall item on a side of the room, a rug on its
-- own layer under the floor items, and a home is open or locked.
ALTER TABLE placed_items
    ADD COLUMN slot smallint NOT NULL DEFAULT 0 CONSTRAINT placed_items_slot CHECK (slot >= 0),
    ADD COLUMN side smallint NOT NULL DEFAULT 0 CONSTRAINT placed_items_side CHECK (side BETWEEN 0 AND 3);

ALTER TABLE placed_items DROP CONSTRAINT placed_items_layer;
ALTER TABLE placed_items ADD CONSTRAINT placed_items_layer CHECK (layer IN ('floor', 'rug', 'surface', 'wall'));

ALTER TABLE homes ADD COLUMN locked boolean NOT NULL DEFAULT false;

-- +goose Down
DELETE FROM placed_items WHERE layer = 'rug';
ALTER TABLE homes DROP COLUMN locked;
ALTER TABLE placed_items DROP CONSTRAINT placed_items_layer;
ALTER TABLE placed_items ADD CONSTRAINT placed_items_layer CHECK (layer IN ('floor', 'surface', 'wall'));
ALTER TABLE placed_items DROP COLUMN side, DROP COLUMN slot;
