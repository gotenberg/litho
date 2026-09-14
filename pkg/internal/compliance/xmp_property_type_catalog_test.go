package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestXMPPropertyType_UndefinedNameHasNoType pins that a property the part's
// predefined catalog rejects also fails the value-type test. veraPDF answers
// both tests of ISO 19005-2 §6.6.2.3.1 from one SchemasDefinition, so an
// undefined property has no defined type and t2 fails wherever t1 does.
//
// govdocs thread_300_300116.pdf writes photoshop:ColorMode, photoshop:History,
// photoshop:ICCProfile, exif:NativeDigest, tiff:NativeDigest and pdf:Trapped;
// veraPDF reports all six under t1 and t2 while litho, whose type registry
// carries types the catalog does not predefine, reported them under t1 alone.
func TestXMPPropertyType_UndefinedNameHasNoType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:photoshop="http://ns.adobe.com/photoshop/1.0/"
      photoshop:ColorMode="4"/>`)

	var got []Violation
	pooled := xmpPooledDeclaredProperties(ctx, nopDoc(), ast.PdfDict{}, data, 2)
	xmpCheckPredefinedProperties(ctx, data, 2, nil, pooled, &got)
	xmpCheckPropertyTypes(data, 2, ast.PdfStream{}, nil, &got)

	if !hasRule(got, Rule_19005_2_6_6_2_3_1_t01) {
		t.Errorf("photoshop:ColorMode must report the undefined name, got %v", got)
	}
	if !hasRule(got, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("photoshop:ColorMode must report the missing value type, got %v", got)
	}
}

// TestXMPPropertyType_PredefinedNameKeepsItsType pins the other side: a name the
// catalog does predefine keeps the type its schema gives it and stays silent
// when the value matches.
func TestXMPPropertyType_PredefinedNameKeepsItsType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdf="http://ns.adobe.com/pdf/1.3/"
      pdf:Keywords="report"/>`)

	var got []Violation
	pooled := xmpPooledDeclaredProperties(ctx, nopDoc(), ast.PdfDict{}, data, 2)
	xmpCheckPredefinedProperties(ctx, data, 2, nil, pooled, &got)
	xmpCheckPropertyTypes(data, 2, ast.PdfStream{}, nil, &got)

	if len(got) != 0 {
		t.Errorf("a predefined, correctly typed property must stay silent, got %v", got)
	}
}

// TestXMPPropertyType_RegistryNeverOutrunsTheCatalog pins the invariant as a
// whole rather than one name at a time: [xmpPropertyRegistry] is a superset of
// the predefined catalog (207 names for part 2), and every name only the
// registry knows must answer t2 as an unknown one.
func TestXMPPropertyType_RegistryNeverOutrunsTheCatalog(t *testing.T) {
	t.Parallel()

	for _, part := range []int{1, 2, 3} {
		catalog := xmpPredefinedPropertyCatalog(part)
		if catalog == nil {
			continue
		}
		for ns, props := range xmpPropertyRegistry {
			known, nsKnown := catalog[ns]
			for name := range props {
				if _, defined := known[name]; nsKnown && defined {
					continue
				}
				details := xmpPropertyTypeDetails(part, ns, name, xmpPropertyValue{}, props)
				if len(details) == 0 {
					t.Errorf("part %d: %s:%s is not predefined yet kept a value type",
						part, ns, name)
				}
			}
		}
	}
}
