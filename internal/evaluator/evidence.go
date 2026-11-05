package evaluator

import (
	"strings"
	"unicode"
)

// normalizeForQuote lowercases, collapses all whitespace (including around
// punctuation and line breaks) to single spaces. Matching stays substring-
// based, so invented quotations cannot pass: normalization can never add
// words that are not present in the submission.
func normalizeForQuote(s string) string {
	var b strings.Builder
	prevSpace := true // leading whitespace collapse
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		case r == '\u2019' || r == '’':
			b.WriteRune('\'')
			prevSpace = false
		default:
			b.WriteRune(unicode.ToLower(r))
			prevSpace = false
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// QuoteContains reports whether the quoted text appears in the submission,
// using normalized whitespace matching. Empty quotes never match.
func QuoteContains(submission, quote string) bool { return quoteContains(submission, quote) }

// quoteContains reports whether the quoted text appears in the submission,
// using normalized whitespace matching. Empty quotes never match.
func quoteContains(submission, quote string) bool {
	if strings.TrimSpace(quote) == "" {
		return false
	}
	nSub := normalizeForQuote(submission)
	nQuote := normalizeForQuote(quote)
	if nQuote == "" || len(nQuote) > len(nSub) {
		return false
	}
	return strings.Contains(nSub, nQuote)
}
