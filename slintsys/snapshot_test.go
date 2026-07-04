package slintsys

import "testing"

// TestSnapshotLen pins the §3.8 overflow guard: C.GoBytes takes a 32-bit length,
// so w*h*4 must be computed in uint64 and refused past 2^31-1 rather than wrapped
// (panic) or truncated (silent corruption).
func TestSnapshotLen(t *testing.T) {
	cases := []struct {
		name   string
		w, h   uint32
		want   uint64
		wantOK bool
	}{
		{"empty", 0, 0, 0, true},
		{"full HD", 1920, 1080, 1920 * 1080 * 4, true},
		{"just under the limit", 23170, 23170, 23170 * 23170 * 4, true}, // 2,147,395,600
		{"just over the limit", 23171, 23170, 23171 * 23170 * 4, false}, // 2,147,488,280
		{"huge", 65535, 65535, 65535 * 65535 * 4, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := snapshotLen(tc.w, tc.h)
			if n != tc.want || ok != tc.wantOK {
				t.Errorf("snapshotLen(%d, %d) = (%d, %v), want (%d, %v)", tc.w, tc.h, n, ok, tc.want, tc.wantOK)
			}
		})
	}
}
