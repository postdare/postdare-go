// Package rank produces the ordering keys a board column stores on each issue.
//
// Positions are fractional indexes: base-36 strings compared lexicographically,
// where a key can always be generated strictly between any two neighbours. That
// is what keeps a drag cheap -- dropping a card writes one row instead of
// renumbering the column -- and what keeps two people reordering at once from
// stepping on each other, since neither has to touch the cards they did not move.
package rank

import (
	"errors"
	"strings"
)

const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// ErrOutOfOrder is returned when prev is not strictly less than next, which
// means the caller read the column in an order the stored keys do not agree
// with. Callers recover by rebalancing rather than by writing a broken key.
var ErrOutOfOrder = errors.New("rank: prev must sort before next")

// First is the key handed to the first card in an empty column.
func First() string { return string(alphabet[len(alphabet)/2]) }

// Between returns a key that sorts strictly after prev and strictly before
// next. An empty prev means "start of the column" and an empty next means "end
// of the column", so Between("", "") is the key for the only card in a column.
func Between(prev, next string) (string, error) {
	prev, next = strings.TrimSpace(prev), strings.TrimSpace(next)
	if !valid(prev) || !valid(next) {
		return "", ErrOutOfOrder
	}
	if prev == "" && next == "" {
		return First(), nil
	}
	if next != "" && prev >= next {
		return "", ErrOutOfOrder
	}
	return midpoint(prev, next), nil
}

// Rebalance returns count evenly spaced keys, for rebuilding a column whose
// stored keys have become unusable.
func Rebalance(count int) []string {
	keys := make([]string, 0, count)
	prev := ""
	for i := 0; i < count; i++ {
		key, err := Between(prev, "")
		if err != nil {
			key = First()
		}
		keys = append(keys, key)
		prev = key
	}
	return keys
}

// midpoint assumes prev < next (with an empty next meaning positive infinity)
// and that neither ends in the lowest digit, which valid() has already checked.
func midpoint(prev, next string) string {
	if next != "" {
		shared := 0
		for shared < len(next) && digitAt(prev, shared) == next[shared] {
			shared++
		}
		if shared > 0 {
			return next[:shared] + midpoint(trim(prev, shared), next[shared:])
		}
	}
	low := 0
	if prev != "" {
		low = index(prev[0])
	}
	high := len(alphabet)
	if next != "" {
		high = index(next[0])
	}
	if high-low > 1 {
		return string(alphabet[(low+high)/2])
	}
	// The two digits are adjacent, so the new key has to grow: either next has
	// room to be truncated, or we keep prev's digit and recurse into its tail.
	if len(next) > 1 {
		return next[:1]
	}
	return string(alphabet[low]) + midpoint(trim(prev, 1), "")
}

// digitAt reads s[i], treating positions past the end as the lowest digit so a
// shorter prev compares as if padded with zeros.
func digitAt(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return alphabet[0]
}

func trim(s string, n int) string {
	if n >= len(s) {
		return ""
	}
	return s[n:]
}

func index(c byte) int {
	return strings.IndexByte(alphabet, c)
}

// valid reports whether s is a key midpoint can reason about: base-36 digits
// with no trailing lowest digit, since "1" and "10" name the same fraction and
// a trailing zero would leave no room below it.
func valid(s string) bool {
	if s == "" {
		return true
	}
	for i := 0; i < len(s); i++ {
		if index(s[i]) < 0 {
			return false
		}
	}
	return s[len(s)-1] != alphabet[0]
}

// Valid reports whether s is a well-formed ordering key.
func Valid(s string) bool { return valid(strings.TrimSpace(s)) }
