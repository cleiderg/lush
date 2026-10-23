package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cleiderg/lush/server/internal/world"
)

// testDatabase returns LUSH_TEST_DATABASE_URL pointed at a fresh schema of
// its own, so these tests don't race the store package's tests on the shared
// database, plus a pool for inspecting that schema. It skips without the URL.
func testDatabase(t *testing.T, schema string) (string, *pgxpool.Pool) {
	t.Helper()
	base := os.Getenv("LUSH_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("LUSH_TEST_DATABASE_URL not set")
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal("LUSH_TEST_DATABASE_URL is not a URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{"DROP SCHEMA IF EXISTS " + schema + " CASCADE", "CREATE SCHEMA " + schema} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		pool.Close()
	})
	return u.String(), pool
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// TestRunWithDatabase runs the server on a real database: it migrates before
// serving, /readyz pings the pool, and the shutdown save completes with exit 0.
func TestRunWithDatabase(t *testing.T) {
	dbURL, pool := testDatabase(t, "lush_cmd_run")
	addr := freeAddr(t)
	env := map[string]string{"LUSH_DATABASE_URL": dbURL, "LUSH_LISTEN_ADDR": addr}
	clock := world.NewFakeClock(time.Unix(0, 0))
	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- run(ctx, func(k string) string { return env[k] }, clock, out) }()

	var body string
	deadline := time.Now().Add(10 * time.Second)
	for body == "" {
		if time.Now().After(deadline) {
			t.Fatalf("/readyz never answered\n%s", out)
		}
		resp, err := http.Get("http://" + addr + "/readyz")
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("/readyz = %d %q", resp.StatusCode, b)
		}
		body = string(b)
	}
	if body != "ok\n" {
		t.Fatalf("/readyz body %q, want the database-backed %q", body, "ok\n")
	}
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM goose_db_version WHERE version_id > 0").Scan(&n); err != nil || n < 1 {
		t.Fatalf("migrations applied = %d, err %v; want at least 1", n, err)
	}

	waitTickers(t, clock, 2)
	cancel()
	waitTickers(t, clock, 3)
	clock.Advance(5 * time.Second)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code %d, want 0\n%s", code, out)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("run did not return after the countdown\n%s", out)
	}
	if strings.Contains(out.String(), "in-memory store") {
		t.Fatalf("server fell back to the in-memory store:\n%s", out)
	}
}

// TestRunMigrationFails exits non-zero, logging the stage, when the schema
// conflicts with the first migration.
func TestRunMigrationFails(t *testing.T) {
	dbURL, pool := testDatabase(t, "lush_cmd_badmigrate")
	if _, err := pool.Exec(context.Background(), "CREATE TABLE players (id int)"); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"LUSH_DATABASE_URL": dbURL, "LUSH_LISTEN_ADDR": "127.0.0.1:0"}
	out := &syncBuffer{}
	code := run(context.Background(), func(k string) string { return env[k] }, world.NewFakeClock(time.Unix(0, 0)), out)
	if code != 1 {
		t.Fatalf("exit code %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out.String(), "migrate failed") {
		t.Fatalf("log should name the migrate stage:\n%s", out)
	}
}
