-- +goose Up
-- Quest progress (quests spec, Quest system). One row per player and quest the
-- store has been told about; a quest with no row is not started. state is 0 not
-- started, 1 in progress, 2 complete. step and count are the current step and
-- its kill or item count. Quest ids are the ids of the quest data files.
CREATE TABLE player_quests (
    player_id  bigint      NOT NULL REFERENCES players (id) ON DELETE CASCADE,
    quest_id   text        NOT NULL CONSTRAINT player_quests_id_nonempty CHECK (quest_id <> ''),
    state      smallint    NOT NULL CONSTRAINT player_quests_state_range CHECK (state BETWEEN 0 AND 2),
    step       integer     NOT NULL DEFAULT 0 CONSTRAINT player_quests_step_nonnegative CHECK (step >= 0),
    count      integer     NOT NULL DEFAULT 0 CONSTRAINT player_quests_count_nonnegative CHECK (count >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (player_id, quest_id)
);

ALTER TABLE players
    ADD COLUMN quest_points bigint NOT NULL DEFAULT 0 CONSTRAINT players_quest_points_nonnegative CHECK (quest_points >= 0);

-- +goose Down
ALTER TABLE players DROP COLUMN quest_points;
DROP TABLE player_quests;
