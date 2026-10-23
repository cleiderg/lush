// Package migrations embeds the SQL migration files so the server binary
// carries them and needs no repo checkout at runtime.
package migrations

import "embed"

// FS holds the goose migration files at its root.
//
//go:embed *.sql
var FS embed.FS
