package warn

import (
	"regexp"
	"testing"
)

func TestAll(t *testing.T) {
	t.Parallel()

	kebab := regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)
	seen := map[string]bool{}
	for _, c := range All() {
		name := c.String()
		if !kebab.MatchString(name) {
			t.Errorf("code %q is not kebab-case", name)
		}
		if seen[name] {
			t.Errorf("code %q is listed twice", name)
		}
		seen[name] = true
	}
	if seen[Code{}.String()] {
		t.Error("the zero Code is listed")
	}
}

func TestAllReturnsACopy(t *testing.T) {
	t.Parallel()

	a := All()
	a[0] = LimitReached
	if All()[0] != MissingGlyph {
		t.Error("changing the slice All returned changed the list")
	}
}
