package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// inheritedResourcesViolation is the violation litho reports for a page whose
// content stream can only name a resource through the page tree.
func inheritedResourcesViolation() compliance.Violation {
	return compliance.Violation{
		Rule: compliance.Rule{
			Specification: "ISO_19005_4",
			Clause:        "6.2.2",
			TestNumber:    2,
			Description: "A content stream that references other objects, such as images and fonts " +
				"that are necessary to fully render or process the stream, shall have an explicitly " +
				"associated Resources dictionary",
		},
		Context: `page 1: cs operator resolves "/CS0" only through inherited Resources/ColorSpace`,
	}
}

// TestMatchPageResourcesMatchesTheInheritedReport pins that the fixer claims
// the violation the detector actually writes.
//
// fixPageResources exists to materialise an inherited /Resources, which
// ISO 19005-4, 6.2.2 test 2 requires, but it only recognised the wording of the
// rule description. The detector reports "resolves %q only through inherited
// Resources/...", so the fixer stayed idle on precisely the documents it was
// written for, and every other fixer then failed to see those resources too: a
// page inheriting an ICCBased space kept its 6.2.4.2 test 3 violation as well.
func TestMatchPageResourcesMatchesTheInheritedReport(t *testing.T) {
	t.Parallel()

	if !matchPageResources(inheritedResourcesViolation()) {
		t.Error("the inherited-Resources violation is not claimed by any page-resources fixer")
	}
}

// TestMatchPageResourcesStillMatchesAMissingResource is the control for the
// other arm: a resource absent from the page's own dictionary.
func TestMatchPageResourcesStillMatchesAMissingResource(t *testing.T) {
	t.Parallel()

	v := inheritedResourcesViolation()
	v.Context = `page 1: cs operator references "/CS0" not in Resources/ColorSpace`
	if !matchPageResources(v) {
		t.Error("the missing-resource violation is no longer claimed")
	}
}

// TestMatchPageResourcesIgnoresOtherClauses is the control against the fixer
// claiming violations it cannot repair.
func TestMatchPageResourcesIgnoresOtherClauses(t *testing.T) {
	t.Parallel()

	v := inheritedResourcesViolation()
	v.Rule.Clause = "6.2.4.2"
	if matchPageResources(v) {
		t.Error("a colour space violation was claimed by the page-resources fixer")
	}
}
