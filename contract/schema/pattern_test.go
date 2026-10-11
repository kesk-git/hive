package schema

import (
	"encoding/json"
	"testing"
)

func TestPatternCommonSyntax(t *testing.T) {
	for _, p := range []string{
		`^[a-z]{1,3}$`, `(?:ab)+`, `\x41`, `[\w-]+`, `a{2,}`, `a{3}`, `\.`, `^$`,
		`(^|/)\.\.?(/|$)`, `[^/\x00]+`, `a|b`, `\d\D\w\W`, `\bword\b`,
		`[a-f0-9]`, `\/`, `[\-a]`, `[a-]`, `[-a]`, `[\w-]`, `[\]]`, `[\[]`, `a{0,1000}`, `(a)(b)`, `[.]`, `[^\n\t ]+`,
		`[(){}|*+?^$]`, `a*?`, `a+?`, `a??`, `a{2}?`, `a{2,}?`, `a{2,3}?`, `(?:ab)+?c`, `a{0}`, `a{0,0}`, `a{10}`, `a{0,}`, `(^)*`, `()`, `a|`, `(|a)`, `x-y,z:w`, `^\b$`, `(?:^a|b$)+`,
		`[\n\r\t\f\v\x00-\x1f]`, `\^\$\\\.\*\+\?\(\)\[\]\{\}\|`, "[\u00a0\u2000-\u200a]",
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
		`a{01}`:       "leading zero: a literal in RE2, a count in ECMAScript",
		`a{1,01}`:     "leading zero in the upper count",
		`a{00}`:       "leading zero",
		`a{0,00}`:     "leading zero",
		`^*`:          "repeated assertion",
		`$+`:          "repeated assertion",
		`^?`:          "repeated assertion",
		`^{2}`:        "repeated assertion",
		`\b?`:         "repeated assertion",
		`\B{2}`:       "repeated assertion",
		`\B*`:         "repeated assertion",
		`(?:^)*a?{1}`: "repeat of a repeat",
		`a{2}{3}`:     "repeat of a repeat",
		`a*??`:        "repeat of a lazy repeat",
		`a+?*`:        "repeat of a lazy repeat",
		`a{2}?{3}`:    "repeat of a lazy repeat",
		`a??+`:        "repeat of a lazy repeat",
		`|*`:          "nothing to repeat",
		`(*)`:         "nothing to repeat",
		`a{1,2`:       "unclosed count",
		`a{1 }`:       "space in a count",
		`a{ 1}`:       "space in a count",
		`\-`:          "escaped hyphen outside a class",
		`[a-c-e]`:     "hyphen between members",
		`[\w-a]`:      "range from a class",
		`[]`:          "empty class",
		`[^]`:         "empty class",
		`\s`:          "white space differs between RE2 and ECMAScript",
		`\S`:          "white space differs between RE2 and ECMAScript",
		`[^\s]+`:      "white space differs between RE2 and ECMAScript",
		`[\s]`:        "white space differs between RE2 and ECMAScript",
		`.`:           "dot differs on line terminators",
		`a.b`:         "dot differs on line terminators",
		`^.*$`:        "dot differs on line terminators",
		`(?:a.)+`:     "dot differs on line terminators",
	} {
		if _, err := compilePattern(p); err == nil {
			t.Errorf("pattern %q (%s) was accepted", p, why)
		}
	}
	if _, err := compilePattern(j(`@u0041`)); err == nil {
		t.Error(`the ECMAScript-only escape \u0041 was accepted`)
	}
}

// The committed repository pattern refuses exactly the characters a browser
// reads as white space, as the answer in Go must match the answer in one.
func TestImportRecordRepositoryPatternMatchesBrowserWhiteSpace(t *testing.T) {
	r, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	const ref = "urn:hive:schema:import-record:v1#/properties/upstream/properties/repository"
	doc := func(s string) []byte {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, ok := range []string{"https://github.com/a/b", "https://x/\u00e9", "https://x/\u200b", "https://x/\u0085", "https://x/\u180e"} {
		if err := r.Validate(ref, doc(ok)); err != nil {
			t.Errorf("%q was refused: %v", ok, err)
		}
	}
	for _, space := range []string{"\t", "\n", "\u000b", "\f", "\r", " ", "\u00a0", "\u1680", "\u2000", "\u2005", "\u200a", "\u2028", "\u2029", "\u202f", "\u205f", "\u3000", "\ufeff"} {
		if err := r.Validate(ref, doc("https://x/a"+space+"b")); err == nil {
			t.Errorf("a repository holding %q was accepted", space)
		}
	}
}
