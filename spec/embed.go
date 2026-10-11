// Package spec embeds the schema files so that executables validate against
// the files of this directory and never against a copy.
package spec

import "embed"

// Schemas holds every file under schemas/, rooted at the name "schemas".
//
//go:embed schemas
var Schemas embed.FS
