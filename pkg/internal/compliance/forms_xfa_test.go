package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCheckXFA_DynamicOnlyUnderPDFUA1 pins the profile split for the /XFA key.
// Every PDF/A part and PDF/UA-2 forbid the key outright, but ISO 14289-1, 7.15
// forbids only DYNAMIC XFA: its test reads `dynamicRender != 'required'`.
// litho fired on the mere presence of /XFA, which reported the clause against
// the static XFA that real forms carry.
func TestCheckXFA_DynamicOnlyUnderPDFUA1(t *testing.T) {
	ctx := context.Background()

	// Object 4 is the XFA packet stream; object 5 names it in an array.
	build := func(packet string) (*document.Document, ast.PdfDict) {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [] /XFA 5 0 R >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Length "+itoa(len(packet))+" >>\nstream\n"+packet+"\nendstream",
			"[ (config) 4 0 R ]",
		)
		catalog, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			t.Fatalf("ResolveCatalog: %v", err)
		}
		acro, err := document.ResolveDict(ctx, doc, catalog.Dict["AcroForm"])
		if err != nil {
			t.Fatalf("resolve AcroForm: %v", err)
		}
		return doc, acro
	}

	static := "<config><present><pdf><interactive>1</interactive></pdf></present></config>"
	dynamic := "<config><present><pdf><dynamicRender>required</dynamicRender></pdf></present></config>"

	run := func(packet, profile string, part int, ua bool) []Violation {
		doc, acro := build(packet)
		var results []Violation
		checkXFA(ctx, doc, acro, profile, part, ua, &results)
		return results
	}

	t.Run("static XFA conforms under PDF/UA-1", func(t *testing.T) {
		if got := run(static, "PDF/UA-1", 0, true); len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})

	t.Run("dynamic XFA fails under PDF/UA-1", func(t *testing.T) {
		if got := run(dynamic, "PDF/UA-1", 0, true); !hasRule(got, Rule_14289_1_7_15_t01) {
			t.Errorf("got %v, want §7.15 t1", got)
		}
	})

	t.Run("static XFA still fails under PDF/UA-2", func(t *testing.T) {
		if got := run(static, "PDF/UA-2", 0, true); !hasRule(got, Rule_14289_2_8_10_1_t03) {
			t.Errorf("got %v, want §8.10.1 t3", got)
		}
	})

	t.Run("static XFA still fails under PDF/A-2", func(t *testing.T) {
		if got := run(static, "PDF/A-2b", 2, false); !hasRule(got, Rule_19005_2_6_4_2_t01) {
			t.Errorf("got %v, want §6.4.2 t1", got)
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
