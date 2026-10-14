package item

import (
	"reflect"
	"testing"
)

// Translated tests. Upstream: tasks-axi 0.2.6 at revision
// 9401ff899c0d1d8ae6b4fd8727b9025abda2032c; see imports/tasks-axi.json.
//
// test/pr-url.test.ts, describe "isPrUrl":
//   - "accepts canonical PR URL %s": the four URLs of the first table, in
//     TestIsPRURLAcceptsCanonical.
//   - "rejects non-canonical URL %j": the 27 URLs of the second table, in
//     TestIsPRURLRejectsNonCanonical; the groups keep the upstream's comments.
//
// test/backends/markdown-grammar.test.ts, describe "markdown grammar" >
// "parse", the cases that call deriveLinks, in TestDeriveLinks:
//   - "derives typed links from prose"
//   - "derives a Forgejo pulls URL as the single pr link"
//   - "keeps non-canonical PR-ish URLs as doc links, never pr"
// and the order and de-duplication deriveLinks has, in TestDeriveLinksOrder.

func TestIsPRURLAcceptsCanonical(t *testing.T) {
	for _, url := range []string{
		"https://github.com/o/r/pull/42",
		"https://github.com/some-owner/some.repo/pull/1",
		"https://forge.example.org/eve/widget/pulls/39",
		"https://codeberg.org/forgejo/forgejo/pulls/1234",
	} {
		if !IsPRURL(url) {
			t.Errorf("IsPRURL(%q) = false, want true", url)
		}
	}
}

func TestIsPRURLRejectsNonCanonical(t *testing.T) {
	for _, url := range []string{
		// singular/plural route confusion
		"https://github.com/o/r/pulls/42",
		"https://forge.example.org/eve/widget/pull/39",
		// issue URLs
		"https://github.com/o/r/issues/42",
		"https://forge.example.org/o/r/issues/42",
		// number shape
		"https://github.com/o/r/pull/0",
		"https://github.com/o/r/pull/042",
		"https://github.com/o/r/pull/42abc",
		"https://forge.example.org/o/r/pulls/0",
		// scheme / decoration
		"http://github.com/o/r/pull/42",
		"https://github.com/o/r/pull/42/",
		"https://github.com/o/r/pull/42?w=1",
		"https://github.com/o/r/pull/42#top",
		" https://github.com/o/r/pull/42",
		"https://github.com/o/r/pull/42\n",
		"https://github.com/o/r/pull/4\u00002",
		"https://github.com/o/r/pull/4 2",
		// authority shape
		"https://user@forge.example.org/o/r/pulls/39",
		"https://token:secret@github.com/o/r/pull/519",
		"https://forge.example.org:8443/o/r/pulls/39",
		"https://Forge.Example.Org/o/r/pulls/39",
		"https://-bad-.example.com/o/r/pulls/39",
		// path shape
		"https://forge.example.org/o/r/extra/pulls/39",
		"https://forge.example.org/r/pulls/39",
		"https://forge.example.org//r/pulls/39",
		"https://forge.example.org/o%2Fx/r/pulls/39",
		"https://forge.example.org/../r/pulls/39",
		"https://forge.example.org/o/../pulls/39",
	} {
		if IsPRURL(url) {
			t.Errorf("IsPRURL(%q) = true, want false", url)
		}
	}
}

func TestDeriveLinks(t *testing.T) {
	t.Run("derives typed links from prose", func(t *testing.T) {
		links := DeriveLinks("shipped https://github.com/o/r/pull/42 see data/x/report.md")
		want := []Link{
			{LinkPR, "https://github.com/o/r/pull/42"},
			{LinkReport, "data/x/report.md"},
		}
		if !reflect.DeepEqual(links, want) {
			t.Fatalf("links = %v, want %v", links, want)
		}
	})
	t.Run("derives a Forgejo pulls URL as the single pr link", func(t *testing.T) {
		links := DeriveLinks("merged https://forge.example.org/eve/widget/pulls/39")
		want := []Link{{LinkPR, "https://forge.example.org/eve/widget/pulls/39"}}
		if !reflect.DeepEqual(links, want) {
			t.Fatalf("links = %v, want %v", links, want)
		}
	})
	t.Run("keeps non-canonical PR-ish URLs as doc links, never pr", func(t *testing.T) {
		for _, url := range []string{
			"https://forge.example.org/eve/widget/pull/39",
			"https://github.com/o/r/pulls/42",
			"https://forge.example.org/o/r/pulls/39?tab=files",
		} {
			links := DeriveLinks("see " + url)
			want := []Link{{LinkDoc, url}}
			if !reflect.DeepEqual(links, want) {
				t.Errorf("links of %q = %v, want %v", url, links, want)
			}
		}
	})
}

// A sentence puts punctuation after a URL, which is not part of it; pull
// requests come first, then report paths, then other URLs, each once.
func TestDeriveLinksOrder(t *testing.T) {
	links := DeriveLinks("see (https://example.org/a), data/x/report.md, https://github.com/o/r/pull/7. Again https://example.org/a; data/x/report.md")
	want := []Link{
		{LinkPR, "https://github.com/o/r/pull/7"},
		{LinkReport, "data/x/report.md"},
		{LinkDoc, "https://example.org/a"},
	}
	if !reflect.DeepEqual(links, want) {
		t.Fatalf("links = %v, want %v", links, want)
	}
	if got := DeriveLinks("no links here"); got == nil || len(got) != 0 {
		t.Fatalf("links of plain prose = %#v, want an empty list", got)
	}
	// A report path needs a whole word at both ends.
	if got := DeriveLinks("data/x/report.mdx and mydata/x/report.md"); len(got) != 0 {
		t.Fatalf("links = %v, want none", got)
	}
}
