package main

import (
	"slices"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

func TestGameSystemsRegisterRegenBeforeSession(t *testing.T) {
	data, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	mover, _, err := newMover(5)
	if err != nil {
		t.Fatal(err)
	}
	clock := world.NewFakeClock(time.Unix(0, 0))
	w := world.New(world.DefaultConfig())
	sess := session.New(session.Config{Data: data}, w, clock, store.NewMemory(), nil)
	defer sess.Close()
	systems, err := gameSystems(mover, data.Rules, clock, sess)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range systems {
		if err := w.Register(s); err != nil {
			t.Fatal(err)
		}
		names = append(names, s.Name())
	}
	want := []string{"movement", "emote", "chat", "regen", "session"}
	if !slices.Equal(names, want) {
		t.Fatalf("systems = %v, want %v", names, want)
	}
}
