// Package item holds what the Pool knows about an item that does not depend on
// where it is stored. This file recognizes the links an item's prose carries.
//
// It is translated from tasks-axi (see imports/tasks-axi.json): the pull
// request URL classification of src/pr-url.ts, and the derivation of typed
// links from prose in src/backends/markdown-grammar.ts.
package item

import (
	"regexp"
	"strings"
)

// LinkKind is the kind of a link as the legacy backlog words it. The import
// maps these words to the Pool's own kinds; this package only recognizes them.
type LinkKind string

const (
	// LinkPR is a canonical pull request URL.
	LinkPR LinkKind = "pr"
	// LinkReport is a data/<id>/report.md path.
	LinkReport LinkKind = "report"
	// LinkDoc is any other http or https URL.
	LinkDoc LinkKind = "doc"
)

// Link is one link recognized in prose.
type Link struct {
	Kind LinkKind
	URL  string
}

// PRURLExpected says what a pull request URL must be, for the message of a
// refusal.
const PRURLExpected = "a canonical pull request URL: https://github.com/<owner>/<repo>/pull/<n> (GitHub) or https://<host>/<owner>/<repo>/pulls/<n> (Forgejo)"

// Ws is a character class holding what a JavaScript regular expression's \s
// holds, which the legacy grammar is defined by. Go's \s is narrower.
const Ws = `[\t\n\x0b\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

// NonWs is the complement of Ws, JavaScript's \S.
const NonWs = `[^\t\n\x0b\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

const (
	hostLabel = `[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`
	segment   = `[A-Za-z0-9._-]+`
)

var prURLRe = regexp.MustCompile(`^https://(` + hostLabel + `(?:\.` + hostLabel + `)*)/(` + segment + `)/(` + segment + `)/(pull|pulls)/([1-9][0-9]*)$`)

// IsPRURL reports whether url is, byte for byte, a canonical pull request URL:
// https://github.com/<owner>/<repo>/pull/<n> on GitHub, or
// https://<lowercase-dns-host>/<owner>/<repo>/pulls/<n> on any other host, with
// <n> a positive number without leading zeros. An issue URL, a mixed-up route,
// a trailing slash, a query, a fragment, whitespace, user information, a port
// or an encoded separator makes it something else.
func IsPRURL(url string) bool {
	m := prURLRe.FindStringSubmatch(url)
	if m == nil {
		return false
	}
	host, owner, repo, route := m[1], m[2], m[3], m[4]
	if owner == "." || owner == ".." || repo == "." || repo == ".." {
		return false
	}
	if route == "pull" {
		return host == "github.com"
	}
	return host != "github.com"
}

var (
	reportLinkRe = regexp.MustCompile(`\bdata/` + NonWs + `+?/report\.md\b`)
	genericURLRe = regexp.MustCompile(`https?://` + NonWs + `+`)
)

// TrimURL drops the punctuation a sentence puts after a URL.
func TrimURL(url string) string {
	return strings.TrimRight(url, ").,;")
}

// DeriveLinks scans prose for links, in the order pull requests, report paths,
// other URLs, each once. Links live in the prose of a legacy entry, never in a
// tag of their own.
func DeriveLinks(text string) []Link {
	links := []Link{}
	seen := map[string]bool{}
	add := func(kind LinkKind, raw string) {
		url := TrimURL(raw)
		if seen[url] {
			return
		}
		seen[url] = true
		links = append(links, Link{Kind: kind, URL: url})
	}
	for _, m := range genericURLRe.FindAllString(text, -1) {
		if IsPRURL(TrimURL(m)) {
			add(LinkPR, m)
		}
	}
	for _, m := range reportLinkRe.FindAllString(text, -1) {
		add(LinkReport, m)
	}
	for _, m := range genericURLRe.FindAllString(text, -1) {
		if !IsPRURL(TrimURL(m)) {
			add(LinkDoc, m)
		}
	}
	return links
}
