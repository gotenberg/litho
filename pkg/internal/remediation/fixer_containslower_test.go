package remediation

import (
	"strings"
	"testing"
)

func TestContainsLowerMatchesLoweredContains(t *testing.T) {
	texts := []string{
		"", "a", "StructTreeRoot", "The document catalog shall contain a StructTreeRoot",
		"Font widths", "ÉLAN Widget", "straße", "İstanbul", "KELVIN K", "TU of a widget",
	}
	subs := []string{"", "a", "structtreeroot", "STRUCTTREEROOT", "widget", "élan", "STRASSE", "i̇stanbul", "kelvin k", "K", "tu", "x"}
	for _, s := range texts {
		for _, sub := range subs {
			want := strings.Contains(strings.ToLower(s), strings.ToLower(sub))
			if got := containsLower(s, sub); got != want {
				t.Errorf("containsLower(%q, %q) = %v, want %v", s, sub, got, want)
			}
		}
	}
}

func TestContainsLowerFoldsASCIIWithoutAllocating(t *testing.T) {
	n := testing.AllocsPerRun(100, func() {
		if !containsLower("The document catalog shall contain a StructTreeRoot", "STRUCTTREEROOT") {
			t.Fatal("containsLower missed the substring")
		}
	})
	if n != 0 {
		t.Errorf("containsLower allocates %v times, want 0", n)
	}
}
