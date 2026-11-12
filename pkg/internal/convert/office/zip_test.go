package office

import "testing"

// TestInflateLimit pins the bound an entry inflates under: the floor for a
// small entry, [MaxInflateRatio] times the compressed size above it, and
// never more than the caller's limit.
func TestInflateLimit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		compressed uint64
		limit      int64
		want       int64
	}{
		{"under the floor", 10, 1 << 30, inflateRatioFloor},
		{"the ratio", 4096, 1 << 30, 4096 * MaxInflateRatio},
		{"past the limit", 1 << 40, 1 << 30, 1 << 30},
		{"no limit", 4096, 0, 0},
	} {
		if got := InflateLimit(tc.compressed, tc.limit); got != tc.want {
			t.Errorf("%s: InflateLimit(%d, %d) = %d, want %d", tc.name, tc.compressed, tc.limit, got, tc.want)
		}
	}
}
