-- +goose Up
-- Door log (groups spec, Doors): every lock, unlock, open, close and break of
-- a hall door, kept 7 days. hall and door are the ids from the map data.
-- Deleting a player or group deletes their rows; the log is suspicion, not
-- the economy audit.
CREATE TABLE group_door_log (
    log_id    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id  bigint      NOT NULL REFERENCES groups (group_id) ON DELETE CASCADE,
    hall      text        NOT NULL CONSTRAINT group_door_log_hall_nonempty CHECK (hall <> ''),
    door      text        NOT NULL CONSTRAINT group_door_log_door_nonempty CHECK (door <> ''),
    action    text        NOT NULL CONSTRAINT group_door_log_action CHECK (action IN ('open', 'close', 'lock', 'unlock', 'break')),
    player_id bigint      REFERENCES players (id) ON DELETE SET NULL,
    at        timestamptz NOT NULL
);
CREATE INDEX group_door_log_group_at ON group_door_log (group_id, at, log_id);
CREATE INDEX group_door_log_at ON group_door_log (at);

-- +goose Down
DROP TABLE group_door_log;
