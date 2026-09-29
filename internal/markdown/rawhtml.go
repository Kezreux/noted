package markdown

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// escapeRawHTML renders raw HTML found in a note as visible, escaped text.
//
// goldmark's default behaviour, with raw HTML disabled, is to replace it with the
// comment "<!-- raw HTML omitted -->". That is safe but wrong for a logbook: a
// note quoting an XML register map or an HTML fragment from a test report would
// have that part of its own content silently disappear from the preview, and the
// whole purpose of this application is getting information back out again.
//
// So instead of omitting the markup, it is escaped and shown. The security
// property is the same -- nothing becomes live markup in the webview -- while the
// note stays readable.
type escapeRawHTML struct{}

// priority is lower than the default HTML renderer's 1000. goldmark registers
// node renderers from the highest priority number to the lowest, so the last
// registration -- the lowest number -- is the one that takes effect.
const escapeRawHTMLPriority = 1

func withEscapedRawHTML() renderer.Option {
	return renderer.WithNodeRenderers(util.Prioritized(&escapeRawHTML{}, escapeRawHTMLPriority))
}

func (e *escapeRawHTML) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindRawHTML, e.renderInline)
	reg.Register(ast.KindHTMLBlock, e.renderBlock)
}

// renderInline handles markup inside a paragraph, such as a stray <br> someone
// pasted mid-sentence. It stays inline so the sentence still reads.
func (e *escapeRawHTML) renderInline(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	node := n.(*ast.RawHTML)
	for i := 0; i < node.Segments.Len(); i++ {
		seg := node.Segments.At(i)
		_, _ = w.Write(util.EscapeHTML(seg.Value(source)))
	}
	return ast.WalkSkipChildren, nil
}

// renderBlock handles a standalone block of markup. It becomes a <pre> block,
// because a pasted fragment is usually multi-line and its indentation carries
// meaning -- the same treatment a fenced code block would get.
func (e *escapeRawHTML) renderBlock(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	node := n.(*ast.HTMLBlock)
	if entering {
		_, _ = w.WriteString("<pre><code>")
		lines := node.Lines()
		for i := 0; i < lines.Len(); i++ {
			line := lines.At(i)
			_, _ = w.Write(util.EscapeHTML(line.Value(source)))
		}
		return ast.WalkContinue, nil
	}
	if node.HasClosure() {
		_, _ = w.Write(util.EscapeHTML(node.ClosureLine.Value(source)))
	}
	_, _ = w.WriteString("</code></pre>\n")
	return ast.WalkContinue, nil
}
