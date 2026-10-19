// Package migrations embeds the SQL schema migrations so the binaries carry
// them and no files need to be copied into the runtime image.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
