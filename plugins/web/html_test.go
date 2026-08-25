package web

import "testing"

func TestHTMLToTextDropsMarkupAndKeepsStructure(t *testing.T) {
	t.Parallel()

	doc := `<!doctype html><html><head><title>Loop &amp; Turns</title>
<style>body{color:red}</style><script>var x = "<p>not text</p>";</script></head>
<body><h1>Heading</h1><p>First para with <b>bold</b> text.</p>
<ul><li>one</li><li>two</li></ul>
<!-- a comment --><div data-x="a>b">after a quoted gt</div>
<p>caf&eacute; &#233; &#x2014; &nbsp;end</p></body></html>`

	got := htmlToText(doc)
	for _, want := range []string{"Heading", "First para with bold text.", "one", "two", "after a quoted gt"} {
		if !contains(got, want) {
			t.Errorf("text missing %q\n---\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"color:red", "var x", "not text", "a comment", "data-x", "<"} {
		if contains(got, unwanted) {
			t.Errorf("text still contains %q\n---\n%s", unwanted, got)
		}
	}
	// &eacute; is not in the named table, so it stays literal; numeric refs decode.
	if !contains(got, "é —") {
		t.Errorf("numeric entities not decoded\n---\n%s", got)
	}
	if contains(got, "  ") {
		t.Errorf("whitespace not collapsed\n---\n%s", got)
	}
}

func TestHTMLToTextSeparatesBlocks(t *testing.T) {
	t.Parallel()

	got := htmlToText("<p>one</p><p>two</p>")
	if got != "one\ntwo" {
		t.Errorf("got %q, want %q", got, "one\ntwo")
	}
}

func TestHTMLToTextUnclosedSkipContainer(t *testing.T) {
	t.Parallel()

	// A truncated page must not leak script source as content.
	if got := htmlToText("<p>before</p><script>alert(1)"); got != "before" {
		t.Errorf("got %q, want %q", got, "before")
	}
}

func TestExtractTitle(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`<title>Hello &amp; bye</title>`:          "Hello & bye",
		"<title>\n  spaced\n  out\n</title>":      "spaced out",
		`<title lang="en">with attrs</title>`:     "with attrs",
		`<html><body>no title here</body></html>`: "",
		`<title>unterminated`:                     "",
	}
	for doc, want := range cases {
		if got := extractTitle(doc); got != want {
			t.Errorf("extractTitle(%q) = %q, want %q", doc, got, want)
		}
	}
}

func TestDecodeEntitiesLeavesNonEntitiesAlone(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a & b":                  "a & b",
		"x &notarealentity; y":   "x &notarealentity; y",
		"&amp;amp;":              "&amp;",
		"1 &lt; 2 &amp;&amp; ok": "1 < 2 && ok",
		"&#65;&#x42;":            "AB",
		// Out-of-range code points must not panic or produce invalid runes.
		"&#99999999;": "&#99999999;",
	}
	for in, want := range cases {
		if got := decodeEntities(in); got != want {
			t.Errorf("decodeEntities(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncateKeepsValidUTF8(t *testing.T) {
	t.Parallel()

	// Cutting mid-rune would produce invalid UTF-8 in the tool result.
	got := truncate("héllo wörld", 3)
	if got != "hé…" && got != "h…" {
		t.Errorf("truncate = %q, want a rune-aligned prefix", got)
	}
	if truncate("short", 100) != "short" {
		t.Error("truncate shortened a string under the limit")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
