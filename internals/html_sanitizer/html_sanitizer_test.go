package htmlsanitizer_test

import (
	"strings"
	"testing"

	htmlsanitizer "github.com/smil-thakur/save-the-link/internals/html_sanitizer"
)

func TestSanitizeRichTextPreservesEditorMarkupAndRemovesUnsafeContent(t *testing.T) {
	input := `<p onclick="steal()">Hello<br><strong>bold</strong><b>b</b><em>em</em><i>i</i><u>u</u><s>s</s></p>` +
		`<blockquote>quote</blockquote><ol><li>one</li></ol><ul><li>two</li></ul>` +
		`<a href="https://example.com" target="_blank" rel="noopener">safe</a>` +
		`<a href="/relative">relative</a><a href="javascript:alert(1)">unsafe</a>` +
		`<script>alert(1)</script><img src=x onerror="steal()">`

	sanitized := htmlsanitizer.SanitizeRichText(input)
	for _, allowed := range []string{"<p>", "<br>", "<strong>", "<b>", "<em>", "<i>", "<u>", "<s>", "<blockquote>", "<ol>", "<ul>", "<li>", `href="https://example.com"`, `href="/relative"`, `target="_blank"`, `rel="noopener"`} {
		if !strings.Contains(sanitized, allowed) {
			t.Fatalf("allowed rich-text markup %q was removed: %s", allowed, sanitized)
		}
	}
	for _, unsafe := range []string{"<script", "onclick", "javascript:", "<img", "onerror"} {
		if strings.Contains(strings.ToLower(sanitized), unsafe) {
			t.Fatalf("unsafe markup %q remained: %s", unsafe, sanitized)
		}
	}
}
