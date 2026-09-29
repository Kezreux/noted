// Package markdown renders note bodies to HTML for the preview pane.
//
// Note bodies are Markdown so that they stay readable without noted -- a text
// editor, or `grep`, is enough to recover a finding from the data folder if the
// app is ever unavailable.
package markdown

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Renderer converts Markdown to HTML. Create one with New and reuse it; it is
// safe for concurrent use.
type Renderer struct {
	md goldmark.Markdown
}

// New builds a renderer configured for lab notes.
func New() *Renderer {
	return &Renderer{
		md: goldmark.New(
			// GFM brings tables, which are the natural way to write a set of
			// parameter values, plus strikethrough, task lists and autolinks.
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithParserOptions(
				// Every link and image destination is checked before rendering.
				parser.WithASTTransformers(util.Prioritized(safeLinks{}, 100)),
			),
			goldmark.WithRendererOptions(
				// Show raw HTML in a note as escaped text rather than dropping it.
				withEscapedRawHTML(),
				// Notes are written like log entries, a line at a time, so a
				// single newline should show as a line break rather than being
				// folded into the previous line as standard Markdown would.
				html.WithHardWraps(),
				// Deliberately NOT html.WithUnsafe(): raw HTML in a note must never
				// become live markup in the webview. See rawhtml.go for how it is
				// shown as text instead of being discarded.
			),
		),
	}
}

// Render converts Markdown source to an HTML fragment.
func (r *Renderer) Render(source string) (string, error) {
	var buf bytes.Buffer
	if err := r.md.Convert([]byte(source), &buf); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return buf.String(), nil
}

// safeLinks rewrites link and image destinations whose URL scheme is not on the
// allowlist.
//
// Escaping raw HTML stops <script> tags, but it does not stop
// [click me](javascript:...), which goldmark will happily render into an href.
// Inside a webview that executes with the application's privileges, so
// destinations are checked here rather than trusted.
type safeLinks struct{}

func (safeLinks) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Link:
			if !safeDestination(v.Destination) {
				// Kept as a link so the text still reads sensibly, but pointing
				// nowhere.
				v.Destination = []byte("#")
			}
		case *ast.Image:
			if !safeDestination(v.Destination) {
				v.Destination = nil
			}
		}
		return ast.WalkContinue, nil
	})
}

// allowedSchemes are the URL schemes a note may link to.
//
// goldmark's own renderer independently rejects javascript:, vbscript:, data:
// and file: URLs. This allowlist is kept as well, rather than relying on that,
// because it is explicit, tested here, and does not depend on a library's
// internal policy staying the same across upgrades.
//
// file: is absent deliberately: goldmark blocks it regardless, so listing it
// would only imply a capability that does not exist. Referring to files on disk
// is what attachments are for; a path in a note still shows as text.
var allowedSchemes = []string{"http", "https", "mailto"}

// safeDestination reports whether a link destination may be rendered as-is.
//
// A destination with no scheme is relative and therefore harmless. One with a
// scheme is allowed only if it is on the list.
func safeDestination(dest []byte) bool {
	s := string(dest)

	// Browsers ignore leading and embedded control characters and whitespace when
	// resolving a scheme, so "java\tscript:x" is live. Strip them before looking.
	var b strings.Builder
	for _, r := range s {
		if r > 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
	}
	cleaned := strings.ToLower(b.String())

	colon := strings.Index(cleaned, ":")
	if colon < 0 {
		return true // relative
	}
	// A colon after a '/', '?' or '#' is part of a path or query, not a scheme:
	// "docs/a:b" is relative.
	for _, sep := range []string{"/", "?", "#"} {
		if i := strings.Index(cleaned, sep); i >= 0 && i < colon {
			return true
		}
	}

	scheme := cleaned[:colon]
	for _, allowed := range allowedSchemes {
		if scheme == allowed {
			return true
		}
	}
	return false
}
