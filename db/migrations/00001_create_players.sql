-- +goose Up
CREATE TABLE players (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_hash  bytea NOT NULL,
    x           double precision NOT NULL DEFAULT 0,
    z           double precision NOT NULL DEFAULT 0,
    version     bigint NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX players_token_hash_key ON players (token_hash);

-- +goose Down
DROP TABLE players;
