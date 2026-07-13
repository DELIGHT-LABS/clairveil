package main

import "testing"

func TestParseMarkdownUsesCommonMarkStructure(t *testing.T) {
	source := []byte(`# Heading (One)

[inline](docs/a_(b).md#target)
[reference][probe]

[probe]:
  <docs/reference (one).md>
  "title"

<!--
## v9.9.9 - 2026-01-02
[ignored](missing-comment.md)
-->

~~~text
## v9.9.9 - 2026-01-02
[ignored](missing-fence.md)
~~~
`)
	document := parseMarkdown("test.md", source)
	if len(document.Links) != 2 {
		t.Fatalf("links = %d, want 2: %#v", len(document.Links), document.Links)
	}
	if got := document.Links[0].Destination; got != "docs/a_(b).md#target" {
		t.Fatalf("inline destination = %q", got)
	}
	if got := document.Links[1].Destination; got != "docs/reference (one).md" {
		t.Fatalf("reference destination = %q", got)
	}
	if len(document.Headings) != 1 {
		t.Fatalf("headings = %d, want 1: %#v", len(document.Headings), document.Headings)
	}
	if got := document.Headings[0].ID; got != "heading-one" {
		t.Fatalf("heading id = %q, want heading-one", got)
	}
}

func TestParseMarkdownGeneratesUnicodeAndDuplicateHeadingIDs(t *testing.T) {
	document := parseMarkdown("test.md", []byte("## 현재 상태\n\n## 현재 상태\n"))
	if len(document.Headings) != 2 {
		t.Fatalf("headings = %d, want 2", len(document.Headings))
	}
	if got := document.Headings[0].ID; got != "현재-상태" {
		t.Fatalf("first heading id = %q", got)
	}
	if got := document.Headings[1].ID; got != "현재-상태-1" {
		t.Fatalf("second heading id = %q", got)
	}
}
