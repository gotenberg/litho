package structure

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

const sampleMathML = `<math xmlns="http://www.w3.org/1998/Math/MathML">
  <mrow><mi>E</mi><mo>=</mo><mi>m</mi><msup><mi>c</mi><mn>2</mn></msup></mrow>
</math>`

// mockDoc creates a minimal Document with a structure tree in the trailer.
// The structure tree is injected via inline dicts (no actual PDF byte parsing).
func mockDoc(structTree ast.PdfDict) *document.Document {
	doc := &document.Document{}
	doc.Index.Trailer = ast.PdfDict{
		"Root": ast.PdfDict{
			"Type":           ast.PdfName("Catalog"),
			"StructTreeRoot": structTree,
		},
	}
	return doc
}

func TestExtractFormulas_WithMathML(t *testing.T) {
	mathmlStream := ast.NewStream(ast.PdfDict{
		"Subtype": ast.PdfName("application/mathml+xml"),
	}, []byte(sampleMathML))

	fileSpec := ast.PdfDict{
		"AFRelationship": ast.PdfName("Supplement"),
		"EF": ast.PdfDict{
			"F": mathmlStream,
		},
	}

	structTree := ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K": ast.PdfArray{
			ast.PdfDict{
				"S":  ast.PdfName("Formula"),
				"AF": ast.PdfArray{fileSpec},
				"K":  ast.PdfInt(42), // MCID
			},
		},
	}

	doc := mockDoc(structTree)
	formulas, err := ExtractFormulas(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(formulas) != 1 {
		t.Fatalf("expected 1 formula, got %d", len(formulas))
	}

	f := formulas[0]
	if f.MathML != sampleMathML {
		t.Errorf("MathML mismatch:\ngot:  %q\nwant: %q", f.MathML, sampleMathML)
	}
	if len(f.MCIDs) != 1 || f.MCIDs[0] != 42 {
		t.Errorf("MCIDs = %v, want [42]", f.MCIDs)
	}
}

func TestExtractFormulas_NoAF(t *testing.T) {
	structTree := ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K": ast.PdfArray{
			ast.PdfDict{
				"S": ast.PdfName("Formula"),
				"K": ast.PdfInt(10),
			},
		},
	}

	doc := mockDoc(structTree)
	formulas, err := ExtractFormulas(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(formulas) != 1 {
		t.Fatalf("expected 1 formula (with MCID but no MathML), got %d", len(formulas))
	}
	if formulas[0].MathML != "" {
		t.Errorf("expected empty MathML, got %q", formulas[0].MathML)
	}
	if len(formulas[0].MCIDs) != 1 || formulas[0].MCIDs[0] != 10 {
		t.Errorf("MCIDs = %v, want [10]", formulas[0].MCIDs)
	}
}

func TestExtractFormulas_WrongAFRelationship(t *testing.T) {
	mathmlStream := ast.NewStream(ast.PdfDict{
		"Subtype": ast.PdfName("application/mathml+xml"),
	}, []byte(sampleMathML))

	fileSpec := ast.PdfDict{
		"AFRelationship": ast.PdfName("Source"), // Not "Supplement"
		"EF": ast.PdfDict{
			"F": mathmlStream,
		},
	}

	structTree := ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K": ast.PdfArray{
			ast.PdfDict{
				"S":  ast.PdfName("Formula"),
				"AF": ast.PdfArray{fileSpec},
				"K":  ast.PdfInt(5),
			},
		},
	}

	doc := mockDoc(structTree)
	formulas, err := ExtractFormulas(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(formulas) != 1 {
		t.Fatalf("expected 1 formula, got %d", len(formulas))
	}
	if formulas[0].MathML != "" {
		t.Errorf("expected empty MathML for wrong relationship, got %q", formulas[0].MathML)
	}
}

func TestExtractFormulas_NestedFormula(t *testing.T) {
	mathmlStream := ast.NewStream(ast.PdfDict{
		"Subtype": ast.PdfName("application/mathml+xml"),
	}, []byte("<math><mi>x</mi></math>"))

	structTree := ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K": ast.PdfArray{
			ast.PdfDict{
				"S": ast.PdfName("Sect"),
				"K": ast.PdfArray{
					ast.PdfDict{
						"S": ast.PdfName("P"),
						"K": ast.PdfInt(1),
					},
					ast.PdfDict{
						"S": ast.PdfName("Formula"),
						"AF": ast.PdfArray{
							ast.PdfDict{
								"AFRelationship": ast.PdfName("Supplement"),
								"EF":             ast.PdfDict{"F": mathmlStream},
							},
						},
						"K": ast.PdfArray{ast.PdfInt(2), ast.PdfInt(3)},
					},
				},
			},
		},
	}

	doc := mockDoc(structTree)
	formulas, err := ExtractFormulas(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(formulas) != 1 {
		t.Fatalf("expected 1 formula, got %d", len(formulas))
	}
	if formulas[0].MathML != "<math><mi>x</mi></math>" {
		t.Errorf("MathML = %q", formulas[0].MathML)
	}
	if len(formulas[0].MCIDs) != 2 {
		t.Errorf("MCIDs = %v, want 2 entries", formulas[0].MCIDs)
	}
}

func TestExtractFormulas_NoStructTree(t *testing.T) {
	doc := &document.Document{}
	doc.Index.Trailer = ast.PdfDict{
		"Root": ast.PdfDict{
			"Type": ast.PdfName("Catalog"),
		},
	}

	formulas, err := ExtractFormulas(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if formulas != nil {
		t.Errorf("expected nil for no structure tree, got %d", len(formulas))
	}
}

func TestExtractFormulas_NonFormulaElements(t *testing.T) {
	structTree := ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K": ast.PdfArray{
			ast.PdfDict{
				"S": ast.PdfName("P"),
				"K": ast.PdfInt(1),
			},
			ast.PdfDict{
				"S": ast.PdfName("H1"),
				"K": ast.PdfInt(2),
			},
		},
	}

	doc := mockDoc(structTree)
	formulas, err := ExtractFormulas(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if formulas != nil {
		t.Errorf("expected nil for no formulas, got %d", len(formulas))
	}
}

func TestExtractFormulas_MultipleFormulas(t *testing.T) {
	mk := func(mathml string, mcid int) ast.PdfDict {
		return ast.PdfDict{
			"S": ast.PdfName("Formula"),
			"AF": ast.PdfArray{
				ast.PdfDict{
					"AFRelationship": ast.PdfName("Supplement"),
					"EF": ast.PdfDict{
						"F": ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("application/mathml+xml")}, []byte(mathml)),
					},
				},
			},
			"K": ast.PdfInt(mcid),
		}
	}

	structTree := ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K": ast.PdfArray{
			mk("<math><mi>a</mi></math>", 1),
			mk("<math><mi>b</mi></math>", 2),
		},
	}

	doc := mockDoc(structTree)
	formulas, err := ExtractFormulas(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(formulas) != 2 {
		t.Fatalf("expected 2 formulas, got %d", len(formulas))
	}
	if formulas[0].MathML != "<math><mi>a</mi></math>" {
		t.Errorf("first formula MathML = %q", formulas[0].MathML)
	}
	if formulas[1].MathML != "<math><mi>b</mi></math>" {
		t.Errorf("second formula MathML = %q", formulas[1].MathML)
	}
}

func TestCollectMCIDs(t *testing.T) {
	tests := []struct {
		name string
		obj  ast.PdfObject
		want []int
	}{
		{"direct int", ast.PdfInt(5), []int{5}},
		{"MCR dict", ast.PdfDict{"MCID": ast.PdfInt(3)}, []int{3}},
		{"array of ints", ast.PdfArray{ast.PdfInt(1), ast.PdfInt(2)}, []int{1, 2}},
		{"nested dict", ast.PdfDict{"K": ast.PdfArray{ast.PdfInt(10), ast.PdfDict{"MCID": ast.PdfInt(11)}}}, []int{10, 11}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mcids []int
			collectMCIDs(context.Background(), nil, tc.obj, make(map[int]bool), 0, &mcids)
			if len(mcids) != len(tc.want) {
				t.Fatalf("got %v, want %v", mcids, tc.want)
			}
			for i := range mcids {
				if mcids[i] != tc.want[i] {
					t.Errorf("mcids[%d] = %d, want %d", i, mcids[i], tc.want[i])
				}
			}
		})
	}
}

func TestExtractMathMLFromAF_SingleFileSpec(t *testing.T) {
	dict := ast.PdfDict{
		"AF": ast.PdfDict{
			"AFRelationship": ast.PdfName("Supplement"),
			"EF": ast.PdfDict{
				"F": ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("application/mathml+xml")}, []byte("<math/>")),
			},
		},
	}

	doc := &document.Document{Logger: document.NopLogger()}
	got := extractMathMLFromAF(context.Background(), doc, dict)
	if got != "<math/>" {
		t.Errorf("got %q, want '<math/>'", got)
	}
}

func TestFormulaElement_ApplyToTextElements(t *testing.T) {
	fe := models.FormulaElement{
		MathML: "<math><mi>x</mi></math>",
		MCIDs:  []int{5, 6},
	}

	elements := []models.TextElement{
		{Text: "x", MCID: 5},
		{Text: "=", MCID: 5},
		{Text: "2", MCID: 6},
		{Text: "y", MCID: 7}, // Not part of formula
	}

	// Build MCID set for the formula.
	mcidSet := make(map[int]bool)
	for _, id := range fe.MCIDs {
		mcidSet[id] = true
	}

	// Apply MathML to matching elements.
	for i := range elements {
		if mcidSet[elements[i].MCID] {
			elements[i].MathML = fe.MathML
		}
	}

	for i, el := range elements {
		if mcidSet[el.MCID] {
			if el.MathML != fe.MathML {
				t.Errorf("elements[%d].MathML = %q, want formula MathML", i, el.MathML)
			}
		} else {
			if el.MathML != "" {
				t.Errorf("elements[%d].MathML = %q, want empty", i, el.MathML)
			}
		}
	}
}
