package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// GroupRank is a member's rank (groups spec, Groups). Higher is more senior.
type GroupRank int16

// The ranks, as stored.
const (
	RankRecruit GroupRank = iota + 1
	RankMember
	RankOfficer
	RankLeader
)

// Group sizes and prices (groups spec, Groups); placeholders to tune.
const (
	// GroupFoundCost is the gold taken from the founder.
	GroupFoundCost int64 = 500
	// GroupMaxMembers is the most members a group can have.
	GroupMaxMembers = 30
)

// Audit kinds written by the group methods. The found payload has the group
// id, name and gold; the disband payload has the group id, name and the vault
// gold paid to the leader.
const (
	GroupFoundKind   = "group_found"
	GroupDisbandKind = "group_disband"
)

// GroupMember is one member of a group.
type GroupMember struct {
	Player    int64
	Rank      GroupRank
	JoinedAt  time.Time
	RankSince time.Time
	// LastLogin is the player's last login, for leader inactivity.
	LastLogin time.Time
}

// GroupPermissions are the leader's switches for Members and Officers.
type GroupPermissions struct {
	MemberKeys      bool
	MemberVaultRoom bool
	OfficerWithdraw bool
}

// Group is a saved group. Members are ordered by Player id.
type Group struct {
	ID          int64
	Name        string
	Banner      int16
	VaultGold   int64
	Permissions GroupPermissions
	Members     []GroupMember
}

// Leader returns the player id of the leader, or 0 if there is none.
func (g Group) Leader() int64 {
	for _, m := range g.Members {
		if m.Rank == RankLeader {
			return m.Player
		}
	}
	return 0
}

// FoundGroup founds a group; Founder pays Cost gold and becomes its Leader.
type FoundGroup struct {
	Founder int64
	Name    string
	Banner  int16
	Cost    int64
	Now     time.Time
}

// Groups stores groups, their members, cooldowns and last logins. Every method
// that moves gold does so in one transaction with its audit entry.
type Groups interface {
	// FoundGroup, in one transaction, takes f.Cost gold from the founder,
	// creates the group with the founder as Leader and appends an audit
	// entry. An unknown founder fails with ErrNotFound; a founder already in
	// a group or a name taken (ignoring case) with ErrAlreadyExists; too
	// little gold or a bad name or banner with ErrInvalid. On any error
	// nothing changes.
	FoundGroup(ctx context.Context, f FoundGroup) (Group, error)
	// DisbandGroup, in one transaction, adds the vault gold to the leader's
	// gold, appends an audit entry and deletes the group and its members. An
	// unknown group (for example one already disbanded) fails with
	// ErrNotFound and moves no gold.
	DisbandGroup(ctx context.Context, group int64) error
	// JoinGroup adds the player as a Recruit. An unknown group or player
	// fails with ErrNotFound; a player already in a group with
	// ErrAlreadyExists; a full group or a leave cooldown still running at
	// now with ErrInvalid.
	JoinGroup(ctx context.Context, group, player int64, now time.Time) error
	// SetGroupRank sets a member's rank and rank-since time. Leader can't be
	// set or changed here (see HandoverGroup); that fails with ErrInvalid. A
	// player not in the group fails with ErrNotFound.
	SetGroupRank(ctx context.Context, group, player int64, rank GroupRank, now time.Time) error
	// RemoveGroupMember removes a member who left or was kicked and starts
	// their cooldown until cooldownUntil. The leader can't be removed
	// (ErrInvalid); a player not in the group fails with ErrNotFound.
	RemoveGroupMember(ctx context.Context, group, player int64, cooldownUntil time.Time) error
	// HandoverGroup makes an Officer the Leader and the old leader an Officer,
	// both with rank-since now. from must be the leader and to an Officer, or
	// it fails with ErrInvalid.
	HandoverGroup(ctx context.Context, group, from, to int64, now time.Time) error
	// SetGroupPermissions saves the leader's switches.
	SetGroupPermissions(ctx context.Context, group int64, p GroupPermissions) error
	// SetLastLogin saves a player's last login time.
	SetLastLogin(ctx context.Context, player int64, at time.Time) error
	// LoadGroups returns every group with its members, by group id.
	LoadGroups(ctx context.Context) ([]Group, error)
	// LoadLeaveCooldowns returns the end of each saved leave cooldown by
	// player id, including ones already over.
	LoadLeaveCooldowns(ctx context.Context) (map[int64]time.Time, error)
}

var _ Groups = (*PGPlayers)(nil)

func (f FoundGroup) validate() error {
	if f.Cost < 0 {
		return fmt.Errorf("cost %d: %w", f.Cost, ErrInvalid)
	}
	return nil
}

// groupViolation maps a unique violation to ErrAlreadyExists, else classify.
func groupViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return fmt.Errorf("%w: %w", ErrAlreadyExists, err)
	}
	return classify(err)
}

func (s *PGPlayers) FoundGroup(ctx context.Context, f FoundGroup) (Group, error) {
	if err := f.validate(); err != nil {
		return Group{}, fmt.Errorf("found group %q for %d: %w", f.Name, f.Founder, err)
	}
	var g Group
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		g = Group{Name: f.Name, Banner: f.Banner}
		var gold int64
		err := tx.QueryRow(ctx, `SELECT gold FROM players WHERE id = $1 FOR UPDATE`, f.Founder).Scan(&gold)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock founder: %w", err)
		}
		var in bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM group_members WHERE player_id = $1)`, f.Founder).Scan(&in); err != nil {
			return fmt.Errorf("founder membership: %w", err)
		}
		if in {
			return fmt.Errorf("founder already in a group: %w", ErrAlreadyExists)
		}
		if gold < f.Cost {
			return fmt.Errorf("gold %d, cost %d: %w", gold, f.Cost, ErrInvalid)
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO groups (name, banner) VALUES ($1, $2) RETURNING group_id`,
			f.Name, f.Banner).Scan(&g.ID); err != nil {
			return groupViolation(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO group_permissions (group_id) VALUES ($1)`, g.ID); err != nil {
			return groupViolation(err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO group_members (player_id, group_id, rank, joined_at, rank_since)
			 VALUES ($1, $2, $3, $4, $4)`, f.Founder, g.ID, int16(RankLeader), f.Now); err != nil {
			return groupViolation(err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE players SET gold = gold - $2, updated_at = now() WHERE id = $1`, f.Founder, f.Cost); err != nil {
			return classify(err)
		}
		return PGAudit{}.Append(ctx, tx, Entry{Kind: GroupFoundKind, Actor: f.Founder,
			Payload: map[string]any{"group": g.ID, "name": f.Name, "gold": f.Cost}})
	})
	if err != nil {
		return Group{}, fmt.Errorf("found group %q for %d: %w", f.Name, f.Founder, err)
	}
	return s.loadGroup(ctx, g.ID)
}

func (s *PGPlayers) DisbandGroup(ctx context.Context, group int64) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var name string
		var vault int64
		err := tx.QueryRow(ctx,
			`SELECT name, vault_gold FROM groups WHERE group_id = $1 FOR UPDATE`, group).Scan(&name, &vault)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock group: %w", err)
		}
		var leader int64
		err = tx.QueryRow(ctx,
			`SELECT player_id FROM group_members WHERE group_id = $1 AND rank = $2`,
			group, int16(RankLeader)).Scan(&leader)
		if err != nil {
			return fmt.Errorf("find leader: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE players SET gold = gold + $2, updated_at = now() WHERE id = $1`, leader, vault); err != nil {
			return classify(err)
		}
		if err := (PGAudit{}).Append(ctx, tx, Entry{Kind: GroupDisbandKind, Actor: leader,
			Payload: map[string]any{"group": group, "name": name, "gold": vault}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM groups WHERE group_id = $1`, group); err != nil {
			return fmt.Errorf("delete group: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("disband group %d: %w", group, err)
	}
	return nil
}

func (s *PGPlayers) JoinGroup(ctx context.Context, group, player int64, now time.Time) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var n int
		err := tx.QueryRow(ctx,
			`SELECT (SELECT count(*) FROM group_members WHERE group_id = g.group_id)
			 FROM groups g WHERE g.group_id = $1 FOR UPDATE`, group).Scan(&n)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock group: %w", err)
		}
		if n >= GroupMaxMembers {
			return fmt.Errorf("group has %d members: %w", n, ErrInvalid)
		}
		var until *time.Time
		err = tx.QueryRow(ctx,
			`SELECT until_at FROM group_leave_cooldowns WHERE player_id = $1`, player).Scan(&until)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("cooldown: %w", err)
		}
		if err == nil && until.After(now) {
			return fmt.Errorf("cooldown until %s: %w", until.UTC().Format(time.RFC3339), ErrInvalid)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO group_members (player_id, group_id, rank, joined_at, rank_since)
			 VALUES ($1, $2, $3, $4, $4)`, player, group, int16(RankRecruit), now); err != nil {
			return groupViolation(err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("join group %d player %d: %w", group, player, err)
	}
	return nil
}

func (s *PGPlayers) SetGroupRank(ctx context.Context, group, player int64, rank GroupRank, now time.Time) error {
	if rank < RankRecruit || rank >= RankLeader {
		return fmt.Errorf("set rank %d of %d in group %d: %w", rank, player, group, ErrInvalid)
	}
	// A leader's rank changes only by handover.
	tag, err := s.pool.Exec(ctx,
		`UPDATE group_members SET rank = $3, rank_since = $4
		 WHERE group_id = $1 AND player_id = $2 AND rank <> $5`,
		group, player, int16(rank), now, int16(RankLeader))
	if err != nil {
		return fmt.Errorf("set rank of %d in group %d: %w", player, group, classify(err))
	}
	if tag.RowsAffected() == 0 {
		return s.memberMissing(ctx, "set rank", group, player)
	}
	return nil
}

// memberMissing says why a member update matched no row: ErrInvalid for a
// leader, else ErrNotFound.
func (s *PGPlayers) memberMissing(ctx context.Context, op string, group, player int64) error {
	var rank int16
	err := s.pool.QueryRow(ctx,
		`SELECT rank FROM group_members WHERE group_id = $1 AND player_id = $2`, group, player).Scan(&rank)
	if err == nil && GroupRank(rank) == RankLeader {
		return fmt.Errorf("%s of %d in group %d: leader: %w", op, player, group, ErrInvalid)
	}
	return fmt.Errorf("%s of %d in group %d: %w", op, player, group, ErrNotFound)
}

func (s *PGPlayers) RemoveGroupMember(ctx context.Context, group, player int64, cooldownUntil time.Time) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var rank int16
		err := tx.QueryRow(ctx,
			`SELECT rank FROM group_members WHERE group_id = $1 AND player_id = $2 FOR UPDATE`,
			group, player).Scan(&rank)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock member: %w", err)
		}
		if GroupRank(rank) == RankLeader {
			return fmt.Errorf("leader: %w", ErrInvalid)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM group_members WHERE player_id = $1`, player); err != nil {
			return fmt.Errorf("delete member: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO group_leave_cooldowns (player_id, until_at) VALUES ($1, $2)
			 ON CONFLICT (player_id) DO UPDATE SET until_at = EXCLUDED.until_at`,
			player, cooldownUntil); err != nil {
			return classify(err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("remove %d from group %d: %w", player, group, err)
	}
	return nil
}

func (s *PGPlayers) HandoverGroup(ctx context.Context, group, from, to int64, now time.Time) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var fromRank, toRank int16
		if err := tx.QueryRow(ctx,
			`SELECT rank FROM group_members WHERE group_id = $1 AND player_id = $2 FOR UPDATE`,
			group, from).Scan(&fromRank); err != nil {
			return notFoundOr(err, "lock leader")
		}
		if err := tx.QueryRow(ctx,
			`SELECT rank FROM group_members WHERE group_id = $1 AND player_id = $2 FOR UPDATE`,
			group, to).Scan(&toRank); err != nil {
			return notFoundOr(err, "lock officer")
		}
		if GroupRank(fromRank) != RankLeader || GroupRank(toRank) != RankOfficer {
			return fmt.Errorf("ranks %d and %d: %w", fromRank, toRank, ErrInvalid)
		}
		// Demote first so there is never two leaders.
		if _, err := tx.Exec(ctx,
			`UPDATE group_members SET rank = $2, rank_since = $3 WHERE player_id = $1`,
			from, int16(RankOfficer), now); err != nil {
			return classify(err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE group_members SET rank = $2, rank_since = $3 WHERE player_id = $1`,
			to, int16(RankLeader), now); err != nil {
			return classify(err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("hand group %d from %d to %d: %w", group, from, to, err)
	}
	return nil
}

func notFoundOr(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

func (s *PGPlayers) SetGroupPermissions(ctx context.Context, group int64, p GroupPermissions) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE group_permissions SET member_keys = $2, member_vault_room = $3, officer_withdraw = $4
		 WHERE group_id = $1`, group, p.MemberKeys, p.MemberVaultRoom, p.OfficerWithdraw)
	if err != nil {
		return fmt.Errorf("set permissions of group %d: %w", group, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("set permissions of group %d: %w", group, ErrNotFound)
	}
	return nil
}

func (s *PGPlayers) SetLastLogin(ctx context.Context, player int64, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE players SET last_login_at = $2 WHERE id = $1`, player, at)
	if err != nil {
		return fmt.Errorf("set last login of %d: %w", player, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("set last login of %d: %w", player, ErrNotFound)
	}
	return nil
}

func (s *PGPlayers) LoadGroups(ctx context.Context) ([]Group, error) {
	return s.queryGroups(ctx, "")
}

func (s *PGPlayers) loadGroup(ctx context.Context, id int64) (Group, error) {
	gs, err := s.queryGroups(ctx, "WHERE g.group_id = $1", id)
	if err != nil {
		return Group{}, err
	}
	if len(gs) == 0 {
		return Group{}, fmt.Errorf("load group %d: %w", id, ErrNotFound)
	}
	return gs[0], nil
}

// queryGroups loads the groups matching where (which may use args), with
// permissions and members.
func (s *PGPlayers) queryGroups(ctx context.Context, where string, args ...any) ([]Group, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT g.group_id, g.name, g.banner, g.vault_gold,
		        p.member_keys, p.member_vault_room, p.officer_withdraw
		 FROM groups g JOIN group_permissions p USING (group_id) `+where+` ORDER BY g.group_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("load groups: %w", err)
	}
	var out []Group
	var g Group
	if _, err := pgx.ForEachRow(rows, []any{&g.ID, &g.Name, &g.Banner, &g.VaultGold,
		&g.Permissions.MemberKeys, &g.Permissions.MemberVaultRoom, &g.Permissions.OfficerWithdraw}, func() error {
		out = append(out, g)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("load groups: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	index := make(map[int64]int, len(out))
	for i, x := range out {
		index[x.ID] = i
	}
	rows, err = s.pool.Query(ctx,
		`SELECT m.group_id, m.player_id, m.rank, m.joined_at, m.rank_since, pl.last_login_at
		 FROM group_members m JOIN players pl ON pl.id = m.player_id
		 WHERE m.group_id = ANY($1) ORDER BY m.player_id`, groupIDs(out))
	if err != nil {
		return nil, fmt.Errorf("load group members: %w", err)
	}
	var gid int64
	var m GroupMember
	if _, err := pgx.ForEachRow(rows, []any{&gid, &m.Player, &m.Rank, &m.JoinedAt, &m.RankSince, &m.LastLogin}, func() error {
		i := index[gid]
		out[i].Members = append(out[i].Members, m)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("load group members: %w", err)
	}
	return out, nil
}

func groupIDs(gs []Group) []int64 {
	ids := make([]int64, len(gs))
	for i, g := range gs {
		ids[i] = g.ID
	}
	return ids
}

func (s *PGPlayers) LoadLeaveCooldowns(ctx context.Context) (map[int64]time.Time, error) {
	rows, err := s.pool.Query(ctx, `SELECT player_id, until_at FROM group_leave_cooldowns`)
	if err != nil {
		return nil, fmt.Errorf("load leave cooldowns: %w", err)
	}
	out := map[int64]time.Time{}
	var id int64
	var at time.Time
	if _, err := pgx.ForEachRow(rows, []any{&id, &at}, func() error {
		out[id] = at
		return nil
	}); err != nil {
		return nil, fmt.Errorf("load leave cooldowns: %w", err)
	}
	return out, nil
}
