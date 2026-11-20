// Package migrations embeds the goose SQL migrations so binaries and
// integration tests can apply them without a separate goose CLI.
package migrations

import "embed"

// FS holds every *.sql migration in this directory.
//
//go:embed *.sql
var FS embed.FS
