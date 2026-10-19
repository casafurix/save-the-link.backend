package htmlsanitizer

import "github.com/microcosm-cc/bluemonday"

var richTextPolicy = func() *bluemonday.Policy {
	policy := bluemonday.NewPolicy()
	policy.AllowElements("p", "br", "strong", "b", "em", "i", "u", "s", "blockquote", "ol", "ul", "li", "a")
	policy.AllowAttrs("href", "target", "rel").OnElements("a")
	policy.AllowURLSchemes("http", "https", "mailto")
	policy.AllowRelativeURLs(true)
	return policy
}()

func SanitizeRichText(value string) string {
	return richTextPolicy.Sanitize(value)
}
