package exec

import "strings"

// oversizeArguments is an argument list over the platform's limit.
func oversizeArguments() []string { return []string{strings.Repeat("a", 40000)} }
