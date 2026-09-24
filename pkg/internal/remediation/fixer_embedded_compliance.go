package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchEmbeddedCompliance matches the two rules requiring an embedded file to
// be a conforming PDF/A document in its own right: ISO 19005-2 §6.8 t5 and
// ISO 19005-4 §6.9 t3.
//
// Both are matched on the whole specification/clause/test triple, never on the
// clause number alone. Clause 6.9 names three unrelated requirements across the
// parts: interactive forms in ISO 19005-1, optional content configuration in
// ISO 19005-2 and 19005-3, embedded files in ISO 19005-4. ISO 19005-2 §6.9 t3
// is therefore an optional-content rule sharing this clause AND test number
// exactly, and a clause-prefix match would claim it and repair the wrong thing.
func matchEmbeddedCompliance(v compliance.Violation) bool {
	switch {
	case v.Rule.Specification == "ISO_19005_2" && v.Rule.Clause == "6.8" && v.Rule.TestNumber == 5:
		return true
	case v.Rule.Specification == "ISO_19005_4" && v.Rule.Clause == "6.9" && v.Rule.TestNumber == 3:
		return true
	default:
		return false
	}
}

// parseEmbeddedObjectContext reads the file specification's object number from
// a violation context of the form "object 6: embedded file is not a PDF
// document", the shape every failure path in the embedded-file check emits.
//
// A file specification written as a direct dictionary has no object number of
// its own and is reported as object 0, which addresses nothing. ok is false for
// that and for any context this fixer did not shape, so an unreadable context
// repairs nothing rather than writing to a guessed object.
func parseEmbeddedObjectContext(s string) (int, bool) {
	const prefix = "object "
	if !strings.HasPrefix(s, prefix) {
		return 0, false
	}
	rest := s[len(prefix):]
	end := strings.IndexByte(rest, ':')
	if end <= 0 {
		return 0, false
	}
	objNum, err := strconv.Atoi(rest[:end])
	if err != nil || objNum <= 0 {
		return 0, false
	}
	return objNum, true
}

// fixEmbeddedCompliance removes /EF from each file specification whose embedded
// file is not itself PDF/A-conforming.
//
// ISO 19005-2, 6.8 permits /EF only when the embedded file complies with
// ISO 19005-1 or 19005-2; ISO 19005-4, 6.9 only when it complies with
// ISO 19005-1, 19005-2 or 19005-4. A payload that is not a PDF at all (a text
// or XML attachment) has no conforming form: the attachment cannot be repaired
// into compliance, so dropping it is the only repair available.
//
// That destroys content the author deliberately attached, so PreserveContent
// refuses. PDF/A-3 has no such rule by design, arbitrary attachments being its
// purpose and Factur-X depending on them, and never reaches this fixer because
// the rules matched here do not exist for part 3.
//
// Only /EF is removed. The specification, the /Names/EmbeddedFiles tree and any
// /AF reference all survive: the check gathers candidates with a knownKey test
// on /EF, so clearing that one key takes the specification out of scope, while
// leaving the tree in place avoids introducing the separate PDF/A-4f
// requirement that /Names carry an /EmbeddedFiles entry.
//
// # Corpus evidence
//
//   - All corpora: the case that reaches this fixer is a payload that is not a
//     PDF at all, a text or XML attachment.
func fixEmbeddedCompliance(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, violations []compliance.Violation, _ string) ([]string, error) {
	if opts.PreserveContent {
		return nil, nil
	}

	seen := make(map[int]bool, len(violations))
	var objNums []int
	for _, v := range violations {
		objNum, ok := parseEmbeddedObjectContext(v.Context)
		if !ok || seen[objNum] {
			continue
		}
		seen[objNum] = true
		objNums = append(objNums, objNum)
	}
	// A violation slice is ordered by the check that produced it, not by object
	// number, and the writer must emit the same bytes for the same input.
	slices.Sort(objNums)

	var fixes []string
	for _, objNum := range objNums {
		if ctx.Err() != nil {
			break
		}

		fsDict := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if fsDict == nil {
			continue
		}
		if _, ok := fsDict["EF"]; !ok {
			continue
		}

		newFS := make(ast.PdfDict, len(fsDict))
		maps.Copy(newFS, fsDict)
		delete(newFS, "EF")
		alloc.AllocAt(objNum, newFS)

		fixes = append(fixes, fmt.Sprintf(
			"removed /EF from non-conforming embedded file spec (object %d)", objNum))
	}

	return fixes, nil
}
