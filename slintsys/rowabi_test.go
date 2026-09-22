package slintsys

import (
	"math"
	"testing"
)

// rowFromABI guards the size_t -> int conversion for rows Slint asks a Go model to
// insert or remove: anything that doesn't fit an int is out of range by definition.
func TestRowFromABI(t *testing.T) {
	for _, tc := range []struct {
		in   uint64
		want int
		ok   bool
	}{
		{0, 0, true},
		{5, 5, true},
		{uint64(math.MaxInt), math.MaxInt, true},
		{uint64(math.MaxInt) + 1, 0, false},
		{math.MaxUint64, 0, false},
	} {
		got, ok := rowFromABI(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("rowFromABI(%d) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
