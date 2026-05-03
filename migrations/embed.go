package migrations

import "embed"

// Files exposes the embedded SQL migration files.
//
//go:embed *.sql
var Files embed.FS
