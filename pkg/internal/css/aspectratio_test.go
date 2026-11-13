package css

import "testing"

// TestAspectRatio_Parse holds aspect-ratio to CSS Sizing 4, 4.1: auto ||
// <ratio>, a <ratio> being a non-negative number with an optional slash
// and second number (CSS Values 4, 5.7). A degenerate ratio behaves as
// auto; an invalid value leaves the initial auto.
func TestAspectRatio_Parse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl  string
		ratio float64
		auto  bool
	}{
		{"aspect-ratio: auto", 0, false},
		{"aspect-ratio: 2", 2, false},
		{"aspect-ratio: 16 / 9", 16.0 / 9, false},
		{"aspect-ratio: 1/2", 0.5, false},
		{"aspect-ratio: auto 1/2", 0.5, true},
		{"aspect-ratio: 1/2 auto", 0.5, true},
		{"aspect-ratio: calc(1 + 1) / 4", 0.5, false},
		{"aspect-ratio: 0", 0, false},
		{"aspect-ratio: 1 / 0", 0, false},
		{"aspect-ratio: auto 0/1", 0, false},
		{"aspect-ratio: -1", 0, false},
		{"aspect-ratio: 1 / -2", 0, false},
		{"aspect-ratio: auto auto", 0, false},
		{"aspect-ratio: 1 / 2 / 3", 0, false},
		{"aspect-ratio: 1px", 0, false},
		{"aspect-ratio: auto 1 auto", 0, false},
	}
	for _, tt := range tests {
		got := styleOf(t, tt.decl).AspectRatio
		if !near(got.Ratio(), tt.ratio) || got.Auto() != tt.auto {
			t.Errorf("%s = %v auto %v, want %v auto %v", tt.decl, got.Ratio(), got.Auto(), tt.ratio, tt.auto)
		}
	}
}

// TestAspectRatio_NotInherited holds that aspect-ratio is not inherited
// (CSS Sizing 4, 4.1).
func TestAspectRatio_NotInherited(t *testing.T) {
	t.Parallel()
	doc := `<body><div id=p><div id=c></div></div></body>`
	c := compute(t, doc, `#p { aspect-ratio: 2 }`)
	if got := c.of("c").AspectRatio; got != (AspectRatio{}) {
		t.Errorf("child aspect-ratio %v, want auto", got.Ratio())
	}
	if got := c.of("p").AspectRatio.Ratio(); got != 2 {
		t.Errorf("parent aspect-ratio %v, want 2", got)
	}
}
