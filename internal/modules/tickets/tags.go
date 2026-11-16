package tickets

import (
	"strings"
	"unicode/utf8"
)

// Tag limits. A tag is trimmed and lower-cased so "Billing" and "billing " are one tag.
const (
	maxTagLength     = 50
	maxTagsPerTicket = 20
)

// TagCount is one tag of the tenant and how many recent tickets carry it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// NormalizeTag trims, lower-cases and caps one tag; a blank tag is "".
func NormalizeTag(tag string) string {
	tag = strings.ToLower(strings.Join(strings.Fields(tag), " "))
	if utf8.RuneCountInString(tag) > maxTagLength {
		tag = strings.TrimSpace(string([]rune(tag)[:maxTagLength]))
	}
	return tag
}

// NormalizeTags normalises tags, drops blanks and duplicates, keeps the first order and caps the
// count at maxTagsPerTicket. nil in gives nil out, so an update without tags leaves them alone.
func NormalizeTags(tags []string) []string {
	if tags == nil {
		return nil
	}
	return MergeTags(nil, tags)
}

// MergeTags adds tags to existing ones (both normalised), without duplicates, capped at
// maxTagsPerTicket.
func MergeTags(existing, add []string) []string {
	out := make([]string, 0, len(existing)+len(add))
	seen := make(map[string]bool, cap(out))
	for _, list := range [][]string{existing, add} {
		for _, t := range list {
			t = NormalizeTag(t)
			if t == "" || seen[t] || len(out) >= maxTagsPerTicket {
				continue
			}
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// ParseTagFilter reads ?tags=a,b into normalised tags; a ticket must carry all of them.
func ParseTagFilter(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return NormalizeTags(strings.Split(raw, ","))
}
