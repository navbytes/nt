package tui

import "strings"

// fuzzyMatch reports whether filter matches haystack. The filter is split on
// whitespace into terms, and every term must appear as a case-insensitive
// subsequence of the haystack (fzf-style AND of fuzzy terms). So "fmb" matches
// "fix my bug", and "bug fix" matches "fix the bug" (terms are order-
// independent). An empty filter matches everything.
//
// This drives the task list's `/` live filter (replacing a plain substring
// test) so partial, abbreviated, and out-of-order queries still find tasks.
func fuzzyMatch(haystack, filter string) bool {
	terms := strings.Fields(strings.ToLower(filter))
	if len(terms) == 0 {
		return true
	}
	hay := []rune(strings.ToLower(haystack))
	for _, term := range terms {
		if !subsequence(hay, []rune(term)) {
			return false
		}
	}
	return true
}

// subsequence reports whether every rune of needle appears in haystack in order
// (not necessarily contiguously). Both are assumed already lower-cased.
func subsequence(haystack, needle []rune) bool {
	j := 0
	for i := 0; i < len(haystack) && j < len(needle); i++ {
		if haystack[i] == needle[j] {
			j++
		}
	}
	return j == len(needle)
}
