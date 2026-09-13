package rank

import (
	"sort"
	"testing"
)

func TestBetweenOpenEnds(t *testing.T) {
	only, err := Between("", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := Between("", only)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Between(only, "")
	if err != nil {
		t.Fatal(err)
	}
	if !(before < only && only < after) {
		t.Fatalf("expected %q < %q < %q", before, only, after)
	}
}

func TestBetweenRejectsOutOfOrder(t *testing.T) {
	for _, tc := range [][2]string{{"b", "a"}, {"a", "a"}, {"a0", "b"}, {"!", "b"}} {
		if _, err := Between(tc[0], tc[1]); err == nil {
			t.Fatalf("expected error for Between(%q, %q)", tc[0], tc[1])
		}
	}
}

// Repeatedly dropping a card into the same gap is the case that breaks a naive
// integer position, so the keys have to keep splitting without ever colliding.
func TestBetweenSurvivesRepeatedSplits(t *testing.T) {
	low, err := Between("", "")
	if err != nil {
		t.Fatal(err)
	}
	high, err := Between(low, "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{low: true, high: true}
	for i := 0; i < 200; i++ {
		mid, err := Between(low, high)
		if err != nil {
			t.Fatalf("split %d: %v", i, err)
		}
		if !(low < mid && mid < high) {
			t.Fatalf("split %d: expected %q < %q < %q", i, low, mid, high)
		}
		if seen[mid] {
			t.Fatalf("split %d: duplicate key %q", i, mid)
		}
		seen[mid] = true
		high = mid
	}
}

func TestRebalanceIsSorted(t *testing.T) {
	keys := Rebalance(50)
	if len(keys) != 50 {
		t.Fatalf("expected 50 keys, got %d", len(keys))
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatalf("expected sorted keys, got %v", keys)
	}
	for _, key := range keys {
		if !Valid(key) {
			t.Fatalf("rebalance produced invalid key %q", key)
		}
	}
}

func TestBetweenKeysStayValid(t *testing.T) {
	prev := ""
	for i := 0; i < 100; i++ {
		key, err := Between(prev, "")
		if err != nil {
			t.Fatal(err)
		}
		if !Valid(key) {
			t.Fatalf("invalid key %q", key)
		}
		prev = key
	}
}
