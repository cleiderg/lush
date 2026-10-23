package store_test

import (
	"testing"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/store/storetest"
)

func TestMemoryPlayers(t *testing.T) {
	storetest.RunPlayers(t, func(*testing.T) store.Players { return store.NewMemory() })
}
