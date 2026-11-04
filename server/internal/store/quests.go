package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// Quest errors, each wrapped by CompleteQuest with detail.
var (
	// ErrQuestComplete is returned when the quest is already complete.
	ErrQuestComplete = errors.New("store: quest already complete")
	// ErrNoRoom is returned when the inventory can't fit the reward items.
	ErrNoRoom = errors.New("store: no inventory room for quest rewards")
)

// QuestState is where a player is in one quest (quests spec, Quest system).
type QuestState int16

// The quest states, as stored.
const (
	QuestNotStarted QuestState = iota
	QuestInProgress
	QuestComplete
)

// QuestProgress is one player's progress in one quest.
type QuestProgress struct {
	// Quest is the id of the quest data file.
	Quest string
	State QuestState
	// Step is the current step and Count its kill or item count; both are
	// zero unless the quest is in progress.
	Step, Count int32
}

// QuestLog is everything the store keeps about a player's quests.
type QuestLog struct {
	// Points is the quest point total.
	Points int64
	// Quests has a row for each quest the player has progress saved for,
	// ordered by quest id.
	Quests []QuestProgress
}

// QuestItem is a quest reward or a quest item to remove.
type QuestItem struct {
	Item int32
	Qty  int64
	// Stackable says all of Qty goes in one inventory slot; otherwise each
	// unit takes a slot. The store does not know item data.
	Stackable bool
}

// QuestCompletion is the commit of a finished quest.
type QuestCompletion struct {
	PlayerID int64
	Quest    string
	Points   int64
	Gold     int64
	// Items are given to the inventory.
	Items []QuestItem
	// Remove are the quest items taken from the inventory. A quest item the
	// player no longer holds is skipped (a bring-items step already took it).
	Remove []QuestItem
	// XP is combat XP and Skills is skill XP, both in tenths of a point.
	XP     int64
	Skills SkillXP
}

// QuestResult is the player's state after CompleteQuest.
type QuestResult struct {
	Points int64
	XP     int64
	Skills SkillXP
	Holdings
}

// Audit kinds written by CompleteQuest, one per gold or item change.
const (
	QuestGoldKind       = "quest_gold"
	QuestItemKind       = "quest_item"
	QuestItemRemoveKind = "quest_item_remove"
)

// Quests stores quest progress.
type Quests interface {
	// LoadQuests returns the player's quest points and saved progress, or an
	// error wrapping ErrNotFound for an unknown player.
	LoadQuests(ctx context.Context, player int64) (QuestLog, error)
	// SaveQuests upserts each row of progress in one transaction. A quest
	// already complete is left as it is. A row in state QuestComplete is
	// ErrInvalid: completion goes through CompleteQuest. An unknown player
	// is ErrNotFound. On any error nothing changes.
	SaveQuests(ctx context.Context, player int64, progress []QuestProgress) error
	// CompleteQuest, in one transaction, marks the quest complete, adds the
	// points, gold, items and XP, removes the quest items and appends one
	// audit entry per gold or item change with Actor set to the player. It
	// does not change the player's Version. It returns an error wrapping
	// ErrQuestComplete if the quest is complete already, ErrNoRoom if the
	// reward items don't fit, ErrNotFound for an unknown player and ErrInvalid
	// for a bad completion; on any error nothing is written.
	CompleteQuest(ctx context.Context, c QuestCompletion) (QuestResult, error)
}

var (
	_ Quests = (*Memory)(nil)
	_ Quests = (*PGPlayers)(nil)
)

func (c QuestCompletion) validate() error {
	if c.Quest == "" {
		return fmt.Errorf("empty quest id: %w", ErrInvalid)
	}
	if c.Points < 0 || c.Gold < 0 || c.XP < 0 || c.Skills.Woodcutting < 0 || c.Skills.Fishing < 0 || c.Skills.Cooking < 0 {
		return fmt.Errorf("negative reward: %w", ErrInvalid)
	}
	for _, it := range slices.Concat(c.Items, c.Remove) {
		if it.Item <= 0 || it.Qty <= 0 {
			return fmt.Errorf("item %d qty %d: %w", it.Item, it.Qty, ErrInvalid)
		}
	}
	return nil
}

func validateProgress(progress []QuestProgress) error {
	for _, p := range progress {
		if p.Quest == "" || p.State < QuestNotStarted || p.State >= QuestComplete || p.Step < 0 || p.Count < 0 {
			return fmt.Errorf("progress %+v: %w", p, ErrInvalid)
		}
	}
	return nil
}

// applyQuestRewards removes c.Remove from h, then adds c.Gold and c.Items,
// and returns the audit entries. Both stores use it. On error h may be partly
// changed; the caller discards it.
func applyQuestRewards(h *Holdings, c QuestCompletion) ([]Entry, error) {
	var entries []Entry
	entry := func(kind string, payload map[string]any) {
		payload["quest"] = c.Quest
		entries = append(entries, Entry{Kind: kind, Actor: c.PlayerID, Payload: payload})
	}
	for _, it := range c.Remove {
		left := it.Qty
		for i := range h.Inventory {
			st := &h.Inventory[i]
			if st.Item != it.Item || left == 0 {
				continue
			}
			n := min(left, st.Qty)
			st.Qty -= n
			left -= n
			if st.Qty == 0 {
				*st = Stack{}
			}
		}
		if took := it.Qty - left; took > 0 {
			entry(QuestItemRemoveKind, map[string]any{"item": it.Item, "qty": took})
		}
	}
	if c.Gold > 0 {
		if h.Gold > 1<<62 {
			return nil, fmt.Errorf("gold %d + %d: %w", h.Gold, c.Gold, ErrInvalid)
		}
		h.Gold += c.Gold
		entry(QuestGoldKind, map[string]any{"gold": c.Gold})
	}
	for _, it := range c.Items {
		if !addQuestItem(h, it) {
			return nil, fmt.Errorf("item %d qty %d: %w", it.Item, it.Qty, ErrNoRoom)
		}
		entry(QuestItemKind, map[string]any{"item": it.Item, "qty": it.Qty})
	}
	return entries, nil
}

// addQuestItem puts it in h's inventory and reports whether it fit.
func addQuestItem(h *Holdings, it QuestItem) bool {
	if it.Stackable {
		for i := range h.Inventory {
			if st := &h.Inventory[i]; st.Item == it.Item {
				st.Qty += it.Qty
				return true
			}
		}
	}
	for i := range h.Inventory {
		if h.Inventory[i] != (Stack{}) {
			continue
		}
		if it.Stackable {
			h.Inventory[i] = Stack{Item: it.Item, Qty: it.Qty}
			return true
		}
		h.Inventory[i] = Stack{Item: it.Item, Qty: 1}
		it.Qty--
		if it.Qty == 0 {
			return true
		}
	}
	return false
}

// ---- Memory ----

type memQuests struct {
	points map[int64]int64
	rows   map[int64]map[string]QuestProgress
}

func (m *Memory) questsLocked() *memQuests {
	if m.quests == nil {
		m.quests = &memQuests{points: map[int64]int64{}, rows: map[int64]map[string]QuestProgress{}}
	}
	return m.quests
}

func (m *Memory) LoadQuests(ctx context.Context, player int64) (QuestLog, error) {
	if err := ctx.Err(); err != nil {
		return QuestLog{}, fmt.Errorf("load quests %d: %w", player, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[player]; !ok {
		return QuestLog{}, fmt.Errorf("load quests %d: %w", player, ErrNotFound)
	}
	q := m.questsLocked()
	log := QuestLog{Points: q.points[player]}
	for _, p := range q.rows[player] {
		log.Quests = append(log.Quests, p)
	}
	slices.SortFunc(log.Quests, func(a, b QuestProgress) int { return cmpString(a.Quest, b.Quest) })
	return log, nil
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (m *Memory) SaveQuests(ctx context.Context, player int64, progress []QuestProgress) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("save quests %d: %w", player, err)
	}
	if err := validateProgress(progress); err != nil {
		return fmt.Errorf("save quests %d: %w", player, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[player]; !ok {
		return fmt.Errorf("save quests %d: %w", player, ErrNotFound)
	}
	q := m.questsLocked()
	if q.rows[player] == nil {
		q.rows[player] = map[string]QuestProgress{}
	}
	for _, p := range progress {
		if q.rows[player][p.Quest].State == QuestComplete {
			continue
		}
		q.rows[player][p.Quest] = p
	}
	return nil
}

func (m *Memory) CompleteQuest(ctx context.Context, c QuestCompletion) (QuestResult, error) {
	if err := ctx.Err(); err != nil {
		return QuestResult{}, fmt.Errorf("complete quest %q for %d: %w", c.Quest, c.PlayerID, err)
	}
	if err := c.validate(); err != nil {
		return QuestResult{}, fmt.Errorf("complete quest %q for %d: %w", c.Quest, c.PlayerID, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.byID[c.PlayerID]
	if !ok {
		return QuestResult{}, fmt.Errorf("complete quest %q for %d: %w", c.Quest, c.PlayerID, ErrNotFound)
	}
	q := m.questsLocked()
	if q.rows[c.PlayerID][c.Quest].State == QuestComplete {
		return QuestResult{}, fmt.Errorf("complete quest %q for %d: %w", c.Quest, c.PlayerID, ErrQuestComplete)
	}
	h := p.Holdings
	entries, err := applyQuestRewards(&h, c)
	if err == nil {
		for _, e := range entries {
			if err = checkPayload(e); err != nil {
				break
			}
		}
	}
	if err != nil {
		return QuestResult{}, fmt.Errorf("complete quest %q for %d: %w", c.Quest, c.PlayerID, err)
	}
	p.Holdings = h
	p.XP += c.XP
	p.Skills.Woodcutting += c.Skills.Woodcutting
	p.Skills.Fishing += c.Skills.Fishing
	p.Skills.Cooking += c.Skills.Cooking
	m.byID[p.ID] = p
	if q.rows[p.ID] == nil {
		q.rows[p.ID] = map[string]QuestProgress{}
	}
	q.rows[p.ID][c.Quest] = QuestProgress{Quest: c.Quest, State: QuestComplete}
	q.points[p.ID] += c.Points
	m.audit = append(m.audit, entries...)
	return QuestResult{Points: q.points[p.ID], XP: p.XP, Skills: p.Skills, Holdings: p.Holdings}, nil
}

// ---- Postgres ----

func (s *PGPlayers) LoadQuests(ctx context.Context, player int64) (QuestLog, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return QuestLog{}, fmt.Errorf("load quests %d: %w", player, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only; nothing to undo

	var log QuestLog
	err = tx.QueryRow(ctx, `SELECT quest_points FROM players WHERE id = $1`, player).Scan(&log.Points)
	if errors.Is(err, pgx.ErrNoRows) {
		return QuestLog{}, fmt.Errorf("load quests %d: %w", player, ErrNotFound)
	}
	if err != nil {
		return QuestLog{}, fmt.Errorf("load quests %d: %w", player, err)
	}
	rows, err := tx.Query(ctx,
		`SELECT quest_id, state, step, count FROM player_quests WHERE player_id = $1 ORDER BY quest_id`, player)
	if err != nil {
		return QuestLog{}, fmt.Errorf("load quests %d: %w", player, err)
	}
	var p QuestProgress
	if _, err := pgx.ForEachRow(rows, []any{&p.Quest, &p.State, &p.Step, &p.Count}, func() error {
		log.Quests = append(log.Quests, p)
		return nil
	}); err != nil {
		return QuestLog{}, fmt.Errorf("load quests %d: %w", player, err)
	}
	return log, nil
}

func (s *PGPlayers) SaveQuests(ctx context.Context, player int64, progress []QuestProgress) error {
	if err := validateProgress(progress); err != nil {
		return fmt.Errorf("save quests %d: %w", player, err)
	}
	ids := make([]string, len(progress))
	states := make([]int16, len(progress))
	steps := make([]int32, len(progress))
	counts := make([]int32, len(progress))
	for i, p := range progress {
		ids[i], states[i], steps[i], counts[i] = p.Quest, int16(p.State), p.Step, p.Count
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM players WHERE id = $1`, player).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		// Later rows of the same quest win, as in Memory.
		last := map[string]int{}
		for i, id := range ids {
			last[id] = i
		}
		for i := range ids {
			if last[ids[i]] != i {
				continue
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO player_quests (player_id, quest_id, state, step, count)
				 VALUES ($1, $2, $3, $4, $5)
				 ON CONFLICT (player_id, quest_id) DO UPDATE
				 SET state = EXCLUDED.state, step = EXCLUDED.step, count = EXCLUDED.count, updated_at = now()
				 WHERE player_quests.state <> $6`,
				player, ids[i], states[i], steps[i], counts[i], int16(QuestComplete)); err != nil {
				return classify(err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save quests %d: %w", player, err)
	}
	return nil
}

// CompleteQuest locks the player row, so concurrent completions and economy
// writes for the player run one after another.
func (s *PGPlayers) CompleteQuest(ctx context.Context, c QuestCompletion) (QuestResult, error) {
	if err := c.validate(); err != nil {
		return QuestResult{}, fmt.Errorf("complete quest %q for %d: %w", c.Quest, c.PlayerID, err)
	}
	var res QuestResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		res = QuestResult{}
		err := tx.QueryRow(ctx,
			`SELECT gold FROM players WHERE id = $1 FOR UPDATE`, c.PlayerID).Scan(&res.Gold)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock player: %w", err)
		}
		var state QuestState
		err = tx.QueryRow(ctx,
			`SELECT state FROM player_quests WHERE player_id = $1 AND quest_id = $2`,
			c.PlayerID, c.Quest).Scan(&state)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("quest state: %w", err)
		}
		if err == nil && state == QuestComplete {
			return ErrQuestComplete
		}
		if err := loadItems(ctx, tx, c.PlayerID, &res.Holdings); err != nil {
			return err
		}
		entries, err := applyQuestRewards(&res.Holdings, c)
		if err != nil {
			return err
		}
		if err := writeEconomy(ctx, tx, []EconomyChange{{PlayerID: c.PlayerID, Holdings: res.Holdings}}, entries); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`UPDATE players SET quest_points = quest_points + $2,
			     combat_xp = combat_xp + $3, woodcutting_xp = woodcutting_xp + $4,
			     fishing_xp = fishing_xp + $5, cooking_xp = cooking_xp + $6
			 WHERE id = $1
			 RETURNING quest_points, combat_xp, woodcutting_xp, fishing_xp, cooking_xp`,
			c.PlayerID, c.Points, c.XP, c.Skills.Woodcutting, c.Skills.Fishing, c.Skills.Cooking).Scan(
			&res.Points, &res.XP, &res.Skills.Woodcutting, &res.Skills.Fishing, &res.Skills.Cooking); err != nil {
			return classify(err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_quests (player_id, quest_id, state, step, count)
			 VALUES ($1, $2, $3, 0, 0)
			 ON CONFLICT (player_id, quest_id) DO UPDATE
			 SET state = EXCLUDED.state, step = 0, count = 0, updated_at = now()`,
			c.PlayerID, c.Quest, int16(QuestComplete)); err != nil {
			return classify(err)
		}
		return nil
	})
	if err != nil {
		return QuestResult{}, fmt.Errorf("complete quest %q for %d: %w", c.Quest, c.PlayerID, err)
	}
	return res, nil
}
