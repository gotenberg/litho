package mutations

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestStructPageTargetsReadsAnIndirectStructureType pins that an element whose
// /S is an indirect reference to a name is an element (ISO 32000-1, 14.7.2,
// Table 323 lets any value be one), both where the walk meets it and where an
// element's first content item is looked for through it: the Sect (5) and the
// paragraph it holds (6) name their type that way, and the paragraph begins on
// page 2 (9).
func TestStructPageTargetsReadsAnIndirectStructureType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := attachDoc(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 9 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 10 0 R >>",
		"<< /Type /StructElem /S 7 0 R /P 10 0 R /K 6 0 R >>",
		"<< /Type /StructElem /S 8 0 R /P 5 0 R /Pg 9 0 R /K 0 >>",
		"/Sect",
		"/P",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [11 0 R 5 0 R] >>",
		"<< /Type /StructElem /S /P /P 10 0 R /Pg 3 0 R /K 0 >>",
	)
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	root, err := document.ResolveDict(ctx, doc, catalog.Dict["StructTreeRoot"])
	if err != nil {
		t.Fatalf("resolving /StructTreeRoot: %v", err)
	}
	resolve := func(obj ast.PdfObject) ast.PdfObject {
		resolved, err := document.ResolveObject(ctx, doc, obj)
		if err != nil {
			return nil
		}
		return resolved
	}
	targets := StructPageTargets(ctx, resolve, root["K"])
	if got := targets[3]; got.ObjectNum != 10 {
		t.Errorf("page 1 opens %v, want the Document (10)", got)
	}
	if got := targets[9]; got.ObjectNum != 5 {
		t.Errorf("page 2 opens %v, want the Sect (5), the outermost element beginning on it", got)
	}
}
