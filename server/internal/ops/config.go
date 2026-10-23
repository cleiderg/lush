// Package ops holds the operational basics: configuration, metrics, logging
// and graceful shutdown.
package ops

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Config is the server configuration, read from environment variables.
//
//	LUSH_LISTEN_ADDR      listen address            (default ":8080")
//	LUSH_TICK_RATE        world ticks per second    (default 20)
//	LUSH_DATABASE_URL     Postgres URL placeholder  (default "", unused until the store lands)
//	LUSH_ENV              "development" or "production" (default "development")
//	LUSH_MOVE_SPEED       walk speed, world units/s (default 4, PRD decision D-044)
//	LUSH_SPAWN_X, LUSH_SPAWN_Z  guest spawn position (default 0, 0: map origin, centre of central campus)
//	LUSH_SHUTDOWN_COUNTDOWN  restart countdown, Go duration (default 60s in production, 5s otherwise)
//	LUSH_WS_ORIGINS       comma-separated page origins allowed to open a WebSocket
//	                      besides the server's own host, e.g. "play.example.com,localhost:*"
//	                      (default none: same-origin only)
type Config struct {
	ListenAddr        string
	TickRate          int
	DatabaseURL       string
	Env               string
	ShutdownCountdown time.Duration
	MoveSpeed         float32
	SpawnX, SpawnZ    float32
	WSOrigins         []string
}

// LoadConfig builds a Config from getenv (os.Getenv in production).
func LoadConfig(getenv func(string) string) (Config, error) {
	c := Config{ListenAddr: ":8080", TickRate: 20, Env: "development"}
	if v := getenv("LUSH_LISTEN_ADDR"); v != "" {
		c.ListenAddr = v
	}
	if v := getenv("LUSH_TICK_RATE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return c, fmt.Errorf("LUSH_TICK_RATE %q: must be a positive integer", v)
		}
		c.TickRate = n
	}
	c.MoveSpeed = 4
	for _, f := range []struct {
		key string
		dst *float32
		pos bool
	}{{"LUSH_MOVE_SPEED", &c.MoveSpeed, true}, {"LUSH_SPAWN_X", &c.SpawnX, false}, {"LUSH_SPAWN_Z", &c.SpawnZ, false}} {
		if v := getenv(f.key); v != "" {
			n, err := strconv.ParseFloat(v, 32)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || (f.pos && n <= 0) {
				return c, fmt.Errorf("%s %q: must be a finite number%s", f.key, v, map[bool]string{true: " > 0"}[f.pos])
			}
			*f.dst = float32(n)
		}
	}
	for _, o := range strings.Split(getenv("LUSH_WS_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.WSOrigins = append(c.WSOrigins, o)
		}
	}
	c.DatabaseURL = getenv("LUSH_DATABASE_URL")
	if v := getenv("LUSH_ENV"); v != "" {
		c.Env = v
	}
	c.ShutdownCountdown = 5 * time.Second
	if c.Env == "production" {
		c.ShutdownCountdown = 60 * time.Second
	}
	if v := getenv("LUSH_SHUTDOWN_COUNTDOWN"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return c, fmt.Errorf("LUSH_SHUTDOWN_COUNTDOWN %q: must be a non-negative duration", v)
		}
		c.ShutdownCountdown = d
	}
	return c, nil
}
