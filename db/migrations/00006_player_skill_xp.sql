-- +goose Up
-- Skill XP is kept in tenths of a point, as the world keeps it (skills spec).
ALTER TABLE players
    ADD COLUMN woodcutting_xp bigint NOT NULL DEFAULT 0 CONSTRAINT players_woodcutting_xp_nonnegative CHECK (woodcutting_xp >= 0),
    ADD COLUMN fishing_xp     bigint NOT NULL DEFAULT 0 CONSTRAINT players_fishing_xp_nonnegative CHECK (fishing_xp >= 0),
    ADD COLUMN cooking_xp     bigint NOT NULL DEFAULT 0 CONSTRAINT players_cooking_xp_nonnegative CHECK (cooking_xp >= 0);

-- +goose Down
ALTER TABLE players DROP COLUMN cooking_xp, DROP COLUMN fishing_xp, DROP COLUMN woodcutting_xp;
