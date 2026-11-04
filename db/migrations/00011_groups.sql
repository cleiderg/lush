-- +goose Up
-- Groups (groups spec, Groups and Group vault). rank is 1 Recruit, 2 Member,
-- 3 Officer, 4 Leader. The leader is the member with rank 4; a group has
-- exactly one. A player is in at most one group (player_id is the key of
-- group_members). Names are unique ignoring case. vault_gold is the group's
-- gold; it moves only with an audit row.
CREATE TABLE groups (
    group_id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text        NOT NULL CONSTRAINT groups_name_length CHECK (char_length(name) BETWEEN 3 AND 20),
    banner     smallint    NOT NULL CONSTRAINT groups_banner_range CHECK (banner BETWEEN 0 AND 11),
    vault_gold bigint      NOT NULL DEFAULT 0 CONSTRAINT groups_vault_gold_nonnegative CHECK (vault_gold >= 0),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX groups_name_lower ON groups (lower(name));

CREATE TABLE group_members (
    player_id  bigint      PRIMARY KEY REFERENCES players (id) ON DELETE CASCADE,
    group_id   bigint      NOT NULL REFERENCES groups (group_id) ON DELETE CASCADE,
    rank       smallint    NOT NULL CONSTRAINT group_members_rank_range CHECK (rank BETWEEN 1 AND 4),
    joined_at  timestamptz NOT NULL,
    rank_since timestamptz NOT NULL
);
CREATE INDEX group_members_group ON group_members (group_id);
CREATE UNIQUE INDEX group_members_one_leader ON group_members (group_id) WHERE rank = 4;

-- The leader's switches for Members and Officers.
CREATE TABLE group_permissions (
    group_id          bigint  PRIMARY KEY REFERENCES groups (group_id) ON DELETE CASCADE,
    member_keys       boolean NOT NULL DEFAULT false,
    member_vault_room boolean NOT NULL DEFAULT false,
    officer_withdraw  boolean NOT NULL DEFAULT false
);

-- A player who left or was kicked can't join a group until until_at.
CREATE TABLE group_leave_cooldowns (
    player_id bigint      PRIMARY KEY REFERENCES players (id) ON DELETE CASCADE,
    until_at  timestamptz NOT NULL
);

-- For leader inactivity (14 days without a login).
ALTER TABLE players ADD COLUMN last_login_at timestamptz NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE players DROP COLUMN last_login_at;
DROP TABLE group_leave_cooldowns;
DROP TABLE group_permissions;
DROP TABLE group_members;
DROP TABLE groups;
