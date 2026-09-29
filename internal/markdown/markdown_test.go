package markdown

import (
	"strings"
	"testing"
)

func render(t *testing.T, src string) string {
	t.Helper()
	out, err := New().Render(src)
	if err != nil {
		t.Fatalf("Render(%q): %v", src, err)
	}
	return out
}

func TestRendersCommonMarkdown(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"heading", "# Trim procedure", "<h1"},
		{"bold", "**important**", "<strong>important</strong>"},
		{"inline code", "set `VDD_TRIM_OFFSET`", "<code>VDD_TRIM_OFFSET</code>"},
		{"fenced code", "```\nregister dump\n```", "<pre><code>"},
		{"list", "- first\n- second", "<li>first</li>"},
		{"blockquote", "> measured twice", "<blockquote>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := render(t, tc.src); !strings.Contains(got, tc.want) {
				t.Errorf("Render(%q) = %q, want it to contain %q", tc.src, got, tc.want)
			}
		})
	}
}

// Parameter values are naturally tabular, so GFM tables have to work.
func TestRendersTables(t *testing.T) {
	src := "| param | before | after |\n|---|---|---|\n| VDD_TRIM | 0x1A | 0x12 |"
	got := render(t, src)
	for _, want := range []string{"<table>", "<th>param</th>", "<td>0x1A</td>"} {
		if !strings.Contains(got, want) {
			t.Errorf("table render missing %q; got %q", want, got)
		}
	}
}

// Notes are written a line at a time, like a log, so a single newline should be
// a line break rather than being folded into the previous line.
func TestSingleNewlineIsALineBreak(t *testing.T) {
	if got := render(t, "first line\nsecond line"); !strings.Contains(got, "<br") {
		t.Errorf("Render = %q, want a <br> for the single newline", got)
	}
}

// A note may well contain a pasted HTML fragment from a log or a datasheet. It
// must be shown as text, not become live markup in the webview.
func TestRawHTMLIsEscaped(t *testing.T) {
	cases := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`<iframe src="https://example.com"></iframe>`,
		`<div onclick="alert(1)">click</div>`,
		`<svg><desc onload="alert(1)"></desc></svg>`,
	}
	// Only tags the renderer itself emits may appear unescaped. An event-handler
	// attribute such as onerror= is inert once its tag's angle brackets are
	// escaped, so what matters is that no tag from the note survives as a tag.
	ours := map[string]bool{"p": true, "pre": true, "code": true, "/p": true, "/pre": true, "/code": true}

	for _, src := range cases {
		got := render(t, src)
		for _, tag := range liveTags(got) {
			if !ours[tag] {
				t.Errorf("Render(%q) = %q\nleaked <%s> as live markup", src, got, tag)
			}
		}
		if !strings.Contains(got, "&lt;") {
			t.Errorf("Render(%q) = %q, want the angle brackets escaped", src, got)
		}
	}
}

// Escaping raw HTML does not stop [text](javascript:...), which goldmark would
// otherwise render straight into an href.
func TestDangerousLinkSchemesAreNeutralised(t *testing.T) {
	cases := []string{
		`[click](javascript:alert(1))`,
		`[click](JavaScript:alert(1))`,
		`[click](java&#09;script:alert(1))`,
		"[click](java\tscript:alert(1))",
		`[click](vbscript:msgbox(1))`,
		`[click](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)`,
	}
	for _, src := range cases {
		got := render(t, src)
		lower := strings.ToLower(got)
		for _, scheme := range []string{"javascript:", "vbscript:", "data:text/html"} {
			if strings.Contains(lower, scheme) {
				t.Errorf("Render(%q) = %q\nleaked the %q scheme", src, got, scheme)
			}
		}
	}
}

func TestDangerousImageSourcesAreNeutralised(t *testing.T) {
	got := strings.ToLower(render(t, `![x](javascript:alert(1))`))
	if strings.Contains(got, "javascript:") {
		t.Errorf("Render = %q, want the javascript: source removed", got)
	}
}

// The filter must not break the links a lab note legitimately contains.
func TestSafeLinksSurvive(t *testing.T) {
	cases := map[string]string{
		`[docs](https://example.com/ds.pdf)`: "https://example.com/ds.pdf",
		`[docs](http://intranet/spec)`:       "http://intranet/spec",
		`[mail](mailto:someone@example.com)`: "mailto:someone@example.com",
		`[rel](./notes/cal.md)`:              "./notes/cal.md",
		`[anchor](#trim)`:                    "#trim",
		// A colon inside a relative path is not a scheme.
		`[odd](docs/a:b)`: "docs/a:b",
	}
	for src, want := range cases {
		if got := render(t, src); !strings.Contains(got, want) {
			t.Errorf("Render(%q) = %q, want it to keep %q", src, got, want)
		}
	}
}

func TestEmptyInput(t *testing.T) {
	if got := render(t, ""); strings.TrimSpace(got) != "" {
		t.Errorf("Render(\"\") = %q, want empty output", got)
	}
}

func TestSafeDestination(t *testing.T) {
	cases := map[string]bool{
		"https://example.com":  true,
		"http://example.com":   true,
		"mailto:a@b.c":         true,
		"file:///tmp/x":        false, // goldmark blocks file: regardless; see allowedSchemes
		"relative/path":        true,
		"./relative":           true,
		"#anchor":              true,
		"?q=1":                 true,
		"docs/a:b":             true, // colon after a slash is a path
		"javascript:alert(1)":  false,
		"JAVASCRIPT:alert(1)":  false,
		"java\tscript:alert":   false, // control characters stripped before checking
		" javascript:alert(1)": false,
		"vbscript:x":           false,
		"data:text/html,x":     false,
	}
	for dest, want := range cases {
		if got := safeDestination([]byte(dest)); got != want {
			t.Errorf("safeDestination(%q) = %v, want %v", dest, got, want)
		}
	}
}

// Raw HTML must be shown as text, not replaced by goldmark's
// "<!-- raw HTML omitted -->" placeholder: a note quoting an XML register map
// would otherwise lose that part of itself in the preview.
func TestRawHTMLIsShownNotDiscarded(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"block", "<register addr=\"0x1A\" value=\"12\"/>", `&lt;register addr=&quot;0x1A&quot; value=&quot;12&quot;/&gt;`},
		{"inline", "the tag <br> appears mid-sentence", "&lt;br&gt;"},
		{"script text", "<script>alert(1)</script>", "&lt;script&gt;"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := render(t, tc.src)
			if strings.Contains(got, "raw HTML omitted") {
				t.Errorf("Render(%q) = %q\ncontent was discarded instead of escaped", tc.src, got)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("Render(%q) = %q, want it to contain %q", tc.src, got, tc.want)
			}
		})
	}
}

// liveTags returns the tag names that appear unescaped in html -- that is, the
// markup a browser would actually parse.
func liveTags(html string) []string {
	var out []string
	for i := 0; i < len(html); i++ {
		if html[i] != '<' {
			continue
		}
		j := i + 1
		for j < len(html) && html[j] != '>' && html[j] != ' ' {
			j++
		}
		out = append(out, strings.ToLower(html[i+1:j]))
	}
	return out
}
