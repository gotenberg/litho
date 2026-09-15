package compliance

import "testing"

// TestISO32005Interposer pins the container chosen for a pairing Table 5
// forbids, and that a legal pairing asks for none.
//
// Remediation moves elements under new parents, and a move can create a
// containment fault the input never had: wrapping a document's top-level
// elements in a Document turned a stray LI into "Table 5. Document-LI" in 12
// corpus files. The answer is read out of the matrix the validator applies, so
// the two cannot drift.
func TestISO32005Interposer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		parent, child string
		want          string
		wantOK        bool
	}{
		{"a list item needs a list", "Document", "LI", "L", true},
		{"a span needs a paragraph", "Document", "Span", "P", true},
		{"a caption needs a section", "Document", "Caption", "Sect", true},
		{"a legal pairing needs nothing", "Document", "P", "", false},
		{"a section under a document is legal", "Document", "Sect", "", false},
		// A cell needs a row inside a table: one container cannot bridge that,
		// and inventing a partial one would trade the fault for another.
		{"a table cell cannot be bridged by one container", "Document", "TD", "", false},
		{"an unknown type is left alone", "Document", "LithoNotAType", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ISO32005Interposer(tc.parent, tc.child)
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("ISO32005Interposer(%q, %q) = (%q, %v), want (%q, %v)",
					tc.parent, tc.child, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestISO32005InterposerIsItselfLegal pins the property that makes the answer
// usable: whatever it returns must be allowed under the parent AND allowed to
// hold the child. A candidate failing either would swap one Table 5 violation
// for another.
func TestISO32005InterposerIsItselfLegal(t *testing.T) {
	t.Parallel()
	iso32005Once.Do(iso32005BuildIndexes)

	children := []string{"LI", "Span", "Caption", "Lbl", "LBody", "Em", "Strong", "Quote", "TOCI"}
	for _, child := range children {
		container, ok := ISO32005Interposer("Document", child)
		if !ok {
			continue
		}
		if _, bad := iso32005ForbiddenParents[container]["Document"]; bad {
			t.Errorf("%s: container %s is itself forbidden under Document", child, container)
		}
		if _, bad := iso32005ForbiddenParents[child][container]; bad {
			t.Errorf("%s: container %s may not hold it", child, container)
		}
		if iso32005PassThroughTypes[container] {
			t.Errorf("%s: container %s is a pass-through type, so Table 5 looks through it",
				child, container)
		}
	}
}
