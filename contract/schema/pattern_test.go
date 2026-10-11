package schema

import "testing"

func TestPatternCommonSyntax(t *testing.T) {
	for _, p := range []string{
		`^[a-z]{1,3}$`, `(?:ab)+`, `\x41`, `[\w-]+`, `a{2,}`, `a{3}`, `\.`, `^$`,
		`(^|/)\.\.?(/|$)`, `[^/\x00]+`, `a|b`, `a*?`, `\d\D\w\W\s\S`, `\bword\b`,
		`[a-f0-9]`, `\/`, `\-`, `[\]]`, `[\[]`, `a{0,1000}`, `(a)(b)`,
		`^https://[^\s]+$`,
	} {
		if _, err := compilePattern(p); err != nil {
			t.Errorf("pattern %q: %v", p, err)
		}
	}
}

func TestPatternRefusals(t *testing.T) {
	for p, why := range map[string]string{
		`(?i)a`:       "flag group",
		`(?P<n>a)`:    "named group",
		`(?<n>a)`:     "named group",
		`(?=a)`:       "lookahead",
		`(?!a)`:       "negative lookahead",
		`(?<=a)`:      "lookbehind",
		`\p{L}`:       "property class",
		`\pL`:         "property class",
		`\z`:          "Go-only end anchor",
		`\A`:          "Go-only start anchor",
		`\Z`:          "ECMAScript-only",
		`\Q.\E`:       "Go-only quoting",
		`[[:alpha:]]`: "POSIX class",
		`\x{41}`:      "Go-only escape",
		`\x4`:         "short hex escape",
		`\1`:          "back-reference",
		`\k<n>`:       "named back-reference",
		`\e`:          "unknown escape",
		`\_`:          "unknown escape",
		`\c`:          "control escape",
		`\0`:          "NUL escape",
		`a{`:          "brace opening no quantifier",
		`a{,2}`:       "quantifier without a lower bound",
		`a{x}`:        "brace opening no quantifier",
		`a{2,1}`:      "quantifier counting down",
		`a{1001}`:     "quantifier beyond RE2",
		`a{1,1001}`:   "quantifier beyond RE2",
		`a{-1}`:       "negative quantifier",
		`a}`:          "bare closing brace",
		`a]`:          "bare closing bracket",
		`[]a]`:        "class starting with ]",
		`[^]a]`:       "class starting with ]",
		`[a`:          "unterminated class",
		`[a[b]`:       "bracket in a class",
		`(a`:          "unterminated group",
		`a)`:          "unbalanced group",
		`a\`:          "lone backslash",
		`[\b]`:        "backspace class",
		`*a`:          "nothing to repeat",
		`a**`:         "double repeat",
		`a*+`:         "possessive quantifier",
		`(?:a`:        "unterminated group",
		`(?#comment)`: "comment group",
		`(?s:.)`:      "flag group",
		`[z-a]`:       "backwards range",
		`\8`:          "back-reference",
		`(?:a){1001}`: "quantifier beyond RE2",
		`(?P=n)`:      "named back-reference",
		`\<`:          "Go-only word boundary",
		`\C`:          "Go-only byte class",
		`[\p{L}]`:     "property class in a class",
		`[a-\d]`:      "range ending in a class",
	} {
		if _, err := compilePattern(p); err == nil {
			t.Errorf("pattern %q (%s) was accepted", p, why)
		}
	}
	if _, err := compilePattern(j(`@u0041`)); err == nil {
		t.Error(`the ECMAScript-only escape \u0041 was accepted`)
	}
}
