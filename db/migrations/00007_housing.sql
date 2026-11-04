-- +goose Up
-- Housing (housing spec, Data model). Areas and plots are seeded from game
-- data, so their ids are given, not generated. item_defs is a later migration,
-- so item ids here carry no foreign key.
CREATE TABLE housing_areas (
    area_id integer PRIMARY KEY,
    name    text NOT NULL,
    x       double precision NOT NULL,
    z       double precision NOT NULL
);

-- A plot with an owner has a claim time and a rent date; an empty one has
-- neither. A player owns at most one plot.
CREATE TABLE house_plots (
    plot_id         integer PRIMARY KEY,
    area_id         integer NOT NULL REFERENCES housing_areas (area_id),
    x               double precision NOT NULL,
    z               double precision NOT NULL,
    owner_id        bigint REFERENCES players (id),
    claimed_at      timestamptz,
    rent_paid_until timestamptz,
    CONSTRAINT house_plots_owner_unique UNIQUE (owner_id),
    CONSTRAINT house_plots_claim_complete CHECK (
        (owner_id IS NULL) = (claimed_at IS NULL) AND (owner_id IS NULL) = (rent_paid_until IS NULL))
);

CREATE INDEX house_plots_area_idx ON house_plots (area_id);

CREATE TABLE homes (
    home_id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    plot_id           integer NOT NULL UNIQUE REFERENCES house_plots (plot_id),
    owner_id          bigint NOT NULL REFERENCES players (id),
    wallpaper_item_id integer CONSTRAINT homes_wallpaper_positive CHECK (wallpaper_item_id > 0),
    flooring_item_id  integer CONSTRAINT homes_flooring_positive CHECK (flooring_item_id > 0),
    grid_size         smallint NOT NULL DEFAULT 8 CONSTRAINT homes_grid_positive CHECK (grid_size > 0)
);

CREATE TABLE placed_items (
    placed_id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    home_id          bigint NOT NULL REFERENCES homes (home_id) ON DELETE CASCADE,
    item_id          integer NOT NULL CONSTRAINT placed_items_item_positive CHECK (item_id > 0),
    x                smallint NOT NULL,
    y                smallint NOT NULL,
    rotation         smallint NOT NULL DEFAULT 0 CONSTRAINT placed_items_rotation CHECK (rotation IN (0, 90, 180, 270)),
    layer            text NOT NULL CONSTRAINT placed_items_layer CHECK (layer IN ('floor', 'surface', 'wall')),
    parent_placed_id bigint REFERENCES placed_items (placed_id) ON DELETE CASCADE
);

CREATE INDEX placed_items_home_idx ON placed_items (home_id);

-- +goose Down
DROP TABLE placed_items;
DROP TABLE homes;
DROP TABLE house_plots;
DROP TABLE housing_areas;
