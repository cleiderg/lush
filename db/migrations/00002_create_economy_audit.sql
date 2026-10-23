-- +goose Up
CREATE TABLE economy_audit (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at                    timestamptz NOT NULL DEFAULT now(),
    kind                  text NOT NULL,
    actor_player_id       bigint NOT NULL REFERENCES players (id),
    counterparty_player_id bigint REFERENCES players (id),
    payload               jsonb NOT NULL DEFAULT '{}'
);

CREATE INDEX economy_audit_actor_idx ON economy_audit (actor_player_id, at);

-- +goose Down
DROP TABLE economy_audit;
