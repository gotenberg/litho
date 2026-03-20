package compliance

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/models"
)

// mockDoc creates a minimal document-like structure for testing.
// We construct a fake Document with an in-memory xref index and trailer.
type mockDocBuilder struct {
	catalog ast.PdfDict
	encrypt bool
}

func newMockDoc() *mockDocBuilder {
	return &mockDocBuilder{
		catalog: ast.PdfDict{
			"Type": ast.PdfName("Catalog"),
			"Pages": ast.PdfDict{
				"Type":  ast.PdfName("Pages"),
				"Kids":  ast.PdfArray{},
				"Count": ast.PdfInt(0),
			},
		},
	}
}

func (b *mockDocBuilder) withOutputIntents() *mockDocBuilder {
	b.catalog["OutputIntents"] = ast.PdfArray{
		ast.PdfDict{
			"Type":              ast.PdfName("OutputIntent"),
			"S":                 ast.PdfName("GTS_PDFA1"),
			"DestOutputProfile": ast.PdfDict{}, // Presence of key is sufficient.
		},
	}
	return b
}

func (b *mockDocBuilder) withMarkInfo() *mockDocBuilder {
	b.catalog["MarkInfo"] = ast.PdfDict{
		"Marked": ast.PdfBool(true),
	}
	return b
}

func (b *mockDocBuilder) withStructTreeRoot() *mockDocBuilder {
	b.catalog["StructTreeRoot"] = ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K":    ast.PdfArray{},
	}
	return b
}

func (b *mockDocBuilder) withLang(lang string) *mockDocBuilder {
	b.catalog["Lang"] = ast.PdfString(lang)
	return b
}

func (b *mockDocBuilder) withJavaScript() *mockDocBuilder {
	b.catalog["Names"] = ast.PdfDict{
		"JavaScript": ast.PdfDict{
			"Names": ast.PdfArray{},
		},
	}
	return b
}

func (b *mockDocBuilder) withEmbeddedFiles(withRelationship bool) *mockDocBuilder {
	fs := ast.PdfDict{
		"Type": ast.PdfName("Filespec"),
		"F":    ast.PdfString("data.csv"),
		"EF":   ast.PdfDict{"F": ast.PdfDict{}},
	}
	if withRelationship {
		fs["AFRelationship"] = ast.PdfName("Data")
	}
	b.catalog["Names"] = ast.PdfDict{
		"EmbeddedFiles": ast.PdfDict{
			"Names": ast.PdfArray{
				ast.PdfString("data.csv"),
				fs,
			},
		},
	}
	return b
}

func (b *mockDocBuilder) withEmbeddedFileAFRelationship(relationship string) *mockDocBuilder {
	fs := ast.PdfDict{
		"Type": ast.PdfName("Filespec"),
		"F":    ast.PdfString("invoice.xml"),
		"EF": ast.PdfDict{
			"F": ast.PdfStream{
				Dict: ast.PdfDict{},
				Data: []byte("<invoice/>"),
			},
		},
	}
	if relationship != "" {
		fs["AFRelationship"] = ast.PdfName(relationship)
	}
	b.catalog["Names"] = ast.PdfDict{
		"EmbeddedFiles": ast.PdfDict{
			"Names": ast.PdfArray{
				ast.PdfString("invoice.xml"),
				fs,
			},
		},
	}
	return b
}

func (b *mockDocBuilder) withPageFont(fontName string, embedded bool, hasToUnicode bool) *mockDocBuilder {
	fontDict := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("TrueType"),
		"BaseFont": ast.PdfName(fontName),
	}
	fdDict := ast.PdfDict{}
	if embedded {
		fdDict["FontFile2"] = ast.PdfDict{} // Presence is sufficient.
	}
	fontDict["FontDescriptor"] = fdDict
	if hasToUnicode {
		fontDict["ToUnicode"] = ast.PdfDict{} // Presence is sufficient.
	}

	page := ast.PdfDict{
		"Type": ast.PdfName("Page"),
		"Resources": ast.PdfDict{
			"Font": ast.PdfDict{
				ast.PdfName("F1"): fontDict,
			},
		},
	}
	b.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	return b
}

func (b *mockDocBuilder) withDeviceRGBImage() *mockDocBuilder {
	page := ast.PdfDict{
		"Type": ast.PdfName("Page"),
		"Resources": ast.PdfDict{
			"XObject": ast.PdfDict{
				ast.PdfName("Im0"): ast.PdfStream{
					Dict: ast.PdfDict{
						"Subtype":    ast.PdfName("Image"),
						"ColorSpace": ast.PdfName("DeviceRGB"),
					},
				},
			},
		},
	}
	b.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	return b
}

func (b *mockDocBuilder) withTransparency() *mockDocBuilder {
	page := ast.PdfDict{
		"Type": ast.PdfName("Page"),
		"Resources": ast.PdfDict{
			"ExtGState": ast.PdfDict{
				ast.PdfName("GS0"): ast.PdfDict{
					"BM": ast.PdfName("Multiply"),
				},
			},
		},
	}
	b.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	return b
}

func (b *mockDocBuilder) withFigureNoAlt() *mockDocBuilder {
	b.catalog["StructTreeRoot"] = ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K": ast.PdfArray{
			ast.PdfDict{
				"S": ast.PdfName("Figure"),
				// No /Alt key — violation.
			},
		},
	}
	return b
}

func (b *mockDocBuilder) withFigureWithAlt() *mockDocBuilder {
	b.catalog["StructTreeRoot"] = ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K": ast.PdfArray{
			ast.PdfDict{
				"S":   ast.PdfName("Figure"),
				"Alt": ast.PdfString("A photo of a sunset"),
			},
		},
	}
	return b
}

func (b *mockDocBuilder) withHeaderStructElem() *mockDocBuilder {
	b.catalog["StructTreeRoot"] = ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K": ast.PdfArray{
			ast.PdfDict{
				"S": ast.PdfName("Header"),
			},
		},
	}
	return b
}

func (b *mockDocBuilder) withXMPTitle() *mockDocBuilder {
	b.catalog["Metadata"] = ast.PdfStream{
		Dict: ast.PdfDict{"Type": ast.PdfName("Metadata"), "Subtype": ast.PdfName("XML")},
		Data: []byte(`<x:xmpmeta><rdf:RDF><rdf:Description><dc:title>Test</dc:title></rdf:Description></rdf:RDF></x:xmpmeta>`),
	}
	return b
}

func (b *mockDocBuilder) withXMPNoTitle() *mockDocBuilder {
	b.catalog["Metadata"] = ast.PdfStream{
		Dict: ast.PdfDict{"Type": ast.PdfName("Metadata"), "Subtype": ast.PdfName("XML")},
		Data: []byte(`<x:xmpmeta><rdf:RDF><rdf:Description></rdf:Description></rdf:RDF></x:xmpmeta>`),
	}
	return b
}

func (b *mockDocBuilder) withFormField(hasTooltip bool) *mockDocBuilder {
	field := ast.PdfDict{
		"T":       ast.PdfString("field1"),
		"FT":      ast.PdfName("Tx"),
		"Subtype": ast.PdfName("Widget"),
	}
	if hasTooltip {
		field["TU"] = ast.PdfString("Enter your name")
	}
	b.catalog["AcroForm"] = ast.PdfDict{
		"Fields": ast.PdfArray{field},
	}
	return b
}

func (b *mockDocBuilder) withNonStandardTag(tag string, roleMapEntry string) *mockDocBuilder {
	strDict := ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K": ast.PdfArray{
			ast.PdfDict{
				"S": ast.PdfName(tag),
			},
		},
	}
	if roleMapEntry != "" {
		strDict["RoleMap"] = ast.PdfDict{
			ast.PdfName(tag): ast.PdfName(roleMapEntry),
		}
	}
	b.catalog["StructTreeRoot"] = strDict
	return b
}

func (b *mockDocBuilder) withCatalogVersion(version string) *mockDocBuilder {
	b.catalog["Version"] = ast.PdfName(version)
	return b
}

func (b *mockDocBuilder) withNamespaces(valid bool) *mockDocBuilder {
	strDict, ok := b.catalog["StructTreeRoot"].(ast.PdfDict)
	if !ok {
		strDict = ast.PdfDict{
			"Type": ast.PdfName("StructTreeRoot"),
			"K":    ast.PdfArray{},
		}
	}
	if valid {
		strDict["Namespaces"] = ast.PdfArray{
			ast.PdfDict{
				"NS": ast.PdfString("http://iso.org/pdf2/ssn"),
			},
		}
	} else {
		strDict["Namespaces"] = ast.PdfArray{
			ast.PdfDict{}, // Missing /NS.
		}
	}
	b.catalog["StructTreeRoot"] = strDict
	return b
}

func (b *mockDocBuilder) withLinkAnnotation(hasStructParent bool) *mockDocBuilder {
	annot := ast.PdfDict{
		"Type":    ast.PdfName("Annot"),
		"Subtype": ast.PdfName("Link"),
		"Rect":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(100), ast.PdfInt(100)},
	}
	if hasStructParent {
		annot["StructParent"] = ast.PdfInt(0)
	}
	page := ast.PdfDict{
		"Type":   ast.PdfName("Page"),
		"Annots": ast.PdfArray{annot},
	}
	b.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	return b
}

func (b *mockDocBuilder) withLinkStructElem() *mockDocBuilder {
	strDict, ok := b.catalog["StructTreeRoot"].(ast.PdfDict)
	if !ok {
		strDict = ast.PdfDict{
			"Type": ast.PdfName("StructTreeRoot"),
		}
	}
	strDict["K"] = ast.PdfArray{
		ast.PdfDict{
			"S": ast.PdfName("Link"),
		},
	}
	b.catalog["StructTreeRoot"] = strDict
	return b
}

func (b *mockDocBuilder) withFormulaNoAF() *mockDocBuilder {
	strDict, ok := b.catalog["StructTreeRoot"].(ast.PdfDict)
	if !ok {
		strDict = ast.PdfDict{
			"Type": ast.PdfName("StructTreeRoot"),
		}
	}
	strDict["K"] = ast.PdfArray{
		ast.PdfDict{
			"S": ast.PdfName("Formula"),
		},
	}
	b.catalog["StructTreeRoot"] = strDict
	return b
}

func (b *mockDocBuilder) withFormulaWithAF() *mockDocBuilder {
	strDict, ok := b.catalog["StructTreeRoot"].(ast.PdfDict)
	if !ok {
		strDict = ast.PdfDict{
			"Type": ast.PdfName("StructTreeRoot"),
		}
	}
	strDict["K"] = ast.PdfArray{
		ast.PdfDict{
			"S":  ast.PdfName("Formula"),
			"AF": ast.PdfArray{ast.PdfDict{"F": ast.PdfString("formula.mml")}},
		},
	}
	b.catalog["StructTreeRoot"] = strDict
	return b
}

func (b *mockDocBuilder) withPageFontEncoding(fontName string, embedded bool, encoding string) *mockDocBuilder {
	fontDict := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("TrueType"),
		"BaseFont": ast.PdfName(fontName),
	}
	fdDict := ast.PdfDict{}
	if embedded {
		fdDict["FontFile2"] = ast.PdfDict{}
	}
	fontDict["FontDescriptor"] = fdDict
	if encoding != "" {
		fontDict["Encoding"] = ast.PdfName(encoding)
	}

	page := ast.PdfDict{
		"Type": ast.PdfName("Page"),
		"Resources": ast.PdfDict{
			"Font": ast.PdfDict{
				ast.PdfName("F1"): fontDict,
			},
		},
	}
	b.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	return b
}

func (b *mockDocBuilder) withPageFontEncodingDict(fontName string, embedded bool, baseEncoding string) *mockDocBuilder {
	fontDict := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("TrueType"),
		"BaseFont": ast.PdfName(fontName),
	}
	fdDict := ast.PdfDict{}
	if embedded {
		fdDict["FontFile2"] = ast.PdfDict{}
	}
	fontDict["FontDescriptor"] = fdDict
	fontDict["Encoding"] = ast.PdfDict{
		"Type":         ast.PdfName("Encoding"),
		"BaseEncoding": ast.PdfName(baseEncoding),
	}

	page := ast.PdfDict{
		"Type": ast.PdfName("Page"),
		"Resources": ast.PdfDict{
			"Font": ast.PdfDict{
				ast.PdfName("F1"): fontDict,
			},
		},
	}
	b.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	return b
}

func (b *mockDocBuilder) withFigureStructTree(hasAlt bool) *mockDocBuilder {
	figDict := ast.PdfDict{
		"S": ast.PdfName("Figure"),
	}
	if hasAlt {
		figDict["Alt"] = ast.PdfString("Description of figure")
	}
	b.catalog["StructTreeRoot"] = ast.PdfDict{
		"Type": ast.PdfName("StructTreeRoot"),
		"K": ast.PdfArray{
			figDict,
		},
	}
	return b
}

func (b *mockDocBuilder) withEmbeddedPDFFile(pdfData []byte, withRelationship bool) *mockDocBuilder {
	fs := ast.PdfDict{
		"Type": ast.PdfName("Filespec"),
		"F":    ast.PdfString("embedded.pdf"),
		"EF": ast.PdfDict{
			"F": ast.PdfStream{
				Dict: ast.PdfDict{},
				Data: pdfData,
			},
		},
	}
	if withRelationship {
		fs["AFRelationship"] = ast.PdfName("Data")
	}
	b.catalog["Names"] = ast.PdfDict{
		"EmbeddedFiles": ast.PdfDict{
			"Names": ast.PdfArray{
				ast.PdfString("embedded.pdf"),
				fs,
			},
		},
	}
	return b
}

func (b *mockDocBuilder) withNonPDFEmbeddedFile() *mockDocBuilder {
	fs := ast.PdfDict{
		"Type":           ast.PdfName("Filespec"),
		"F":              ast.PdfString("data.xlsx"),
		"AFRelationship": ast.PdfName("Data"),
		"EF": ast.PdfDict{
			"F": ast.PdfStream{
				Dict: ast.PdfDict{},
				Data: []byte("PK\x03\x04not-a-pdf-file"), // ZIP/Office magic bytes.
			},
		},
	}
	b.catalog["Names"] = ast.PdfDict{
		"EmbeddedFiles": ast.PdfDict{
			"Names": ast.PdfArray{
				ast.PdfString("data.xlsx"),
				fs,
			},
		},
	}
	return b
}

func (b *mockDocBuilder) withOCProperties(hasAS bool) *mockDocBuilder {
	ocDict := ast.PdfDict{
		"OCGs": ast.PdfArray{
			ast.PdfDict{
				"Type": ast.PdfName("OCG"),
				"Name": ast.PdfString("Layer1"),
			},
		},
		"D": ast.PdfDict{
			"Name": ast.PdfString("Default"),
		},
	}
	if hasAS {
		ocDict["AS"] = ast.PdfArray{
			ast.PdfDict{
				"Event":    ast.PdfName("View"),
				"Category": ast.PdfArray{ast.PdfName("Print")},
				"OCGs":     ast.PdfArray{},
			},
		}
	}
	b.catalog["OCProperties"] = ocDict
	return b
}

func (b *mockDocBuilder) withGroupTransparency() *mockDocBuilder {
	page := ast.PdfDict{
		"Type": ast.PdfName("Page"),
		"Group": ast.PdfDict{
			"S":  ast.PdfName("Transparency"),
			"CS": ast.PdfName("DeviceRGB"),
		},
	}
	b.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	return b
}

func (b *mockDocBuilder) withJPXImage() *mockDocBuilder {
	page := ast.PdfDict{
		"Type": ast.PdfName("Page"),
		"Resources": ast.PdfDict{
			"XObject": ast.PdfDict{
				ast.PdfName("Im0"): ast.PdfStream{
					Dict: ast.PdfDict{
						"Type":    ast.PdfName("XObject"),
						"Subtype": ast.PdfName("Image"),
						"Filter":  ast.PdfName("JPXDecode"),
					},
				},
			},
		},
	}
	b.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	return b
}

func (b *mockDocBuilder) with3DAnnotation(subtype ast.PdfName, hasFallback bool) *mockDocBuilder {
	annot := ast.PdfDict{
		"Type":    ast.PdfName("Annot"),
		"Subtype": ast.PdfName("3D"),
		"3DD": ast.PdfStream{
			Dict: ast.PdfDict{
				"Subtype": subtype,
			},
		},
	}
	if hasFallback {
		annot["AP"] = ast.PdfDict{
			"N": ast.PdfStream{Dict: ast.PdfDict{}},
		}
	}
	page := ast.PdfDict{
		"Type":   ast.PdfName("Page"),
		"Annots": ast.PdfArray{annot},
	}
	b.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	return b
}

func (b *mockDocBuilder) withRichMediaAnnotation(streamSubtype ast.PdfName, hasFallback bool) *mockDocBuilder {
	annot := ast.PdfDict{
		"Type":    ast.PdfName("Annot"),
		"Subtype": ast.PdfName("RichMedia"),
		"RichMediaContent": ast.PdfDict{
			"Assets": ast.PdfDict{
				"Names": ast.PdfArray{
					ast.PdfString("model.u3d"),
					ast.PdfDict{
						"Type": ast.PdfName("Filespec"),
						"F":    ast.PdfString("model.u3d"),
						"EF": ast.PdfDict{
							"F": ast.PdfStream{
								Dict: ast.PdfDict{
									"Subtype": streamSubtype,
								},
							},
						},
					},
				},
			},
		},
	}
	if hasFallback {
		annot["AP"] = ast.PdfDict{
			"N": ast.PdfStream{Dict: ast.PdfDict{}},
		}
	}
	page := ast.PdfDict{
		"Type":   ast.PdfName("Page"),
		"Annots": ast.PdfArray{annot},
	}
	b.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	return b
}

func (b *mockDocBuilder) build() *mockDocument {
	return &mockDocument{
		catalog: b.catalog,
		encrypt: b.encrypt,
	}
}

// mockDocument is a minimal stand-in for document.Document.
// Since the compliance package accesses doc.Index.Trailer and doc.IsEncrypted,
// we need to provide those. We do this by building a real-ish xref.Index.
type mockDocument struct {
	catalog ast.PdfDict
	encrypt bool
}

// --- Tests ---

func TestPDFA_InvalidProfile(t *testing.T) {
	errs := ValidatePDFA(context.Background(), buildTestDoc(newMockDoc().build()), models.PDFAProfile("bogus"))
	if len(errs) != 1 || !errors.Is(errs[0], models.ErrInvalidProfile) {
		t.Errorf("expected models.ErrInvalidProfile, got %v", errs)
	}
}

func TestPDFA1b_MinimalValid(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	// No pages, no fonts — should pass base checks.
	if len(errs) != 0 {
		t.Errorf("expected no errors for minimal valid PDF/A-1b, got %v", errs)
	}
}

func TestPDFA_NoOutputIntents(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withDeviceRGBImage().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrMissingICCProfile) {
		t.Error("expected models.ErrMissingICCProfile")
	}
}

func TestPDFA_NoOutputIntents_DeviceIndependent(t *testing.T) {
	// When no device-dependent color spaces are used, OutputIntents is not required.
	doc := buildTestDoc(newMockDoc().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if containsError(errs, models.ErrMissingICCProfile) {
		t.Error("unexpected models.ErrMissingICCProfile for device-independent document")
	}
}

func TestPDFA_Encrypted(t *testing.T) {
	m := newMockDoc()
	m.encrypt = true
	doc := buildTestDoc(m.withOutputIntents().build())
	doc.IsEncrypted = true
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrEncryptionForbidden) {
		t.Error("expected models.ErrEncryptionForbidden")
	}
}

func TestPDFA_JavaScript(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withJavaScript().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrActiveContentForbidden) {
		t.Error("expected models.ErrActiveContentForbidden")
	}
}

func TestPDFA1_Transparency(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withTransparency().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrTransparencyForbidden) {
		t.Error("expected models.ErrTransparencyForbidden for PDF/A-1")
	}
}

func TestPDFA2_TransparencyAllowed(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withTransparency().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if containsError(errs, models.ErrTransparencyForbidden) {
		t.Error("PDF/A-2 should allow transparency")
	}
}

// --- PDF/A-1 version check tests ---

func TestPDFA1_VersionCheckCatalog15(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("1.5").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrVersionTooHighForA1) {
		t.Error("expected models.ErrVersionTooHighForA1 for catalog /Version 1.5")
	}
}

func TestPDFA1_VersionCheckCatalog20(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrVersionTooHighForA1) {
		t.Error("expected models.ErrVersionTooHighForA1 for catalog /Version 2.0")
	}
}

func TestPDFA1_VersionCheckCatalog14Passes(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("1.4").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if containsError(errs, models.ErrVersionTooHighForA1) {
		t.Error("PDF version 1.4 should pass PDF/A-1 version check")
	}
}

func TestPDFA1_VersionCheckCatalog13Passes(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("1.3").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if containsError(errs, models.ErrVersionTooHighForA1) {
		t.Error("PDF version 1.3 should pass PDF/A-1 version check")
	}
}

func TestPDFA1_VersionCheckNoCatalogVersionPasses(t *testing.T) {
	// No catalog /Version and no real file header — should assume compliant.
	doc := buildTestDoc(newMockDoc().withOutputIntents().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if containsError(errs, models.ErrVersionTooHighForA1) {
		t.Error("document without catalog /Version should pass version check")
	}
}

func TestPDFA2_SkipsVersionCheck(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if containsError(errs, models.ErrVersionTooHighForA1) {
		t.Error("PDF/A-2 should not enforce PDF/A-1 version constraint")
	}
}

// --- PDF/A-1 /Group transparency test ---

func TestPDFA1_GroupTransparencyForbidden(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withGroupTransparency().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrTransparencyForbidden) {
		t.Error("expected models.ErrTransparencyForbidden for page /Group /S /Transparency")
	}
}

func TestPDFA2_GroupTransparencyAllowed(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withGroupTransparency().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if containsError(errs, models.ErrTransparencyForbidden) {
		t.Error("PDF/A-2 should allow /Group transparency")
	}
}

// --- PDF/A-1 JPEG2000 test ---

func TestPDFA1_JPEG2000Forbidden(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withJPXImage().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrJPEG2000Forbidden) {
		t.Error("expected models.ErrJPEG2000Forbidden for JPXDecode image in PDF/A-1")
	}
}

func TestPDFA2_JPEG2000Allowed(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withJPXImage().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if containsError(errs, models.ErrJPEG2000Forbidden) {
		t.Error("PDF/A-2 should allow JPEG2000 images")
	}
}

func TestPDFA1_AttachmentsForbidden(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withEmbeddedFiles(false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrEmbeddedFilesForbidden) {
		t.Error("expected models.ErrEmbeddedFilesForbidden for PDF/A-1")
	}
}

func TestPDFA3_MissingAFRelationship(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withEmbeddedFiles(false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if !containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("expected models.ErrMissingAFRelationship for PDF/A-3")
	}
}

func TestPDFA3_ValidAFRelationship(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withEmbeddedFiles(true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("PDF/A-3 with valid /AFRelationship should pass")
	}
}

// --- PDF/A-3 AFRelationship value validation tests ---

func TestPDFA3_AFRelationshipSource(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withEmbeddedFileAFRelationship("Source").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("/AFRelationship /Source should pass")
	}
}

func TestPDFA3_AFRelationshipAlternative(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withEmbeddedFileAFRelationship("Alternative").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("/AFRelationship /Alternative should pass")
	}
}

func TestPDFA3_AFRelationshipSupplement(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withEmbeddedFileAFRelationship("Supplement").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("/AFRelationship /Supplement should pass")
	}
}

func TestPDFA3_AFRelationshipUnspecified(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withEmbeddedFileAFRelationship("Unspecified").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("/AFRelationship /Unspecified should pass")
	}
}

func TestPDFA3_AFRelationshipInvalid(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withEmbeddedFileAFRelationship("BogusValue").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if !containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("expected models.ErrMissingAFRelationship for invalid /AFRelationship value")
	}
}

func TestPDFA3_AFRelationshipMissing(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withEmbeddedFileAFRelationship("").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if !containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("expected models.ErrMissingAFRelationship when key is absent")
	}
}

// --- PDF/A-3 allows any file type (no models.ErrNonPDFAAttachment) ---

func TestPDFA3_NonPDFAttachmentAllowed(t *testing.T) {
	// PDF/A-3 allows non-PDF attachments — only /AFRelationship is required.
	fs := ast.PdfDict{
		"Type":           ast.PdfName("Filespec"),
		"F":              ast.PdfString("data.xlsx"),
		"AFRelationship": ast.PdfName("Data"),
		"EF": ast.PdfDict{
			"F": ast.PdfStream{
				Dict: ast.PdfDict{},
				Data: []byte("PK\x03\x04spreadsheet-data"),
			},
		},
	}
	m := newMockDoc().withOutputIntents()
	m.catalog["Names"] = ast.PdfDict{
		"EmbeddedFiles": ast.PdfDict{
			"Names": ast.PdfArray{
				ast.PdfString("data.xlsx"),
				fs,
			},
		},
	}
	doc := buildTestDoc(m.build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if containsError(errs, models.ErrNonPDFAAttachment) {
		t.Error("PDF/A-3 should allow non-PDF attachments")
	}
	if containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("attachment with valid /AFRelationship should pass")
	}
}

func TestPDFA3_NoEmbeddedFilesPasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("document without embedded files should not trigger AFRelationship error")
	}
}

// --- PDF/A-3 OCG check (inherited from A-2) ---

func TestPDFA3_OCGMissingAS(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withOCProperties(false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if !containsError(errs, models.ErrMissingOCGUsageApp) {
		t.Error("expected models.ErrMissingOCGUsageApp for PDF/A-3 when /OCProperties has no /AS")
	}
}

func TestPDFA3_OCGWithAS(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withOCProperties(true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if containsError(errs, models.ErrMissingOCGUsageApp) {
		t.Error("PDF/A-3 /OCProperties with /AS should pass")
	}
}

// --- PDF/A-2 embedded file validation tests ---

func TestPDFA2_NonPDFAttachmentRejected(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withNonPDFEmbeddedFile().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if !containsError(errs, models.ErrNonPDFAAttachment) {
		t.Error("expected models.ErrNonPDFAAttachment for non-PDF embedded file")
	}
}

func TestPDFA2_EmbeddedFileNoEFStream(t *testing.T) {
	// Embedded file spec with no /EF dict → non-compliant.
	doc := buildTestDoc(newMockDoc().withOutputIntents().withEmbeddedFiles(true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if !containsError(errs, models.ErrNonPDFAAttachment) {
		t.Error("expected models.ErrNonPDFAAttachment for file spec without stream data")
	}
}

func TestPDFA2_EmbeddedPDFMalformedRejected(t *testing.T) {
	// Looks like PDF (has header) but cannot be parsed.
	malformedPDF := []byte("%PDF-1.4\n%%EOF\n")
	doc := buildTestDoc(newMockDoc().withOutputIntents().withEmbeddedPDFFile(malformedPDF, true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if !containsError(errs, models.ErrNonPDFAAttachment) {
		t.Error("expected models.ErrNonPDFAAttachment for malformed embedded PDF")
	}
}

func TestPDFA2_NoEmbeddedFilesPasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if containsError(errs, models.ErrNonPDFAAttachment) {
		t.Error("document without embedded files should not trigger attachment errors")
	}
}

func TestPDFA1_SkipsRecursiveAttachmentCheck(t *testing.T) {
	// PDF/A-1 uses checkNoAttachments (total ban), not recursive validation.
	doc := buildTestDoc(newMockDoc().withOutputIntents().withNonPDFEmbeddedFile().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if containsError(errs, models.ErrNonPDFAAttachment) {
		t.Error("PDF/A-1 should use models.ErrEmbeddedFilesForbidden, not models.ErrNonPDFAAttachment")
	}
	if !containsError(errs, models.ErrEmbeddedFilesForbidden) {
		t.Error("expected models.ErrEmbeddedFilesForbidden for PDF/A-1")
	}
}

// --- PDF/A-2 OCG/Layer tests ---

func TestPDFA2_OCGMissingAS(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withOCProperties(false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if !containsError(errs, models.ErrMissingOCGUsageApp) {
		t.Error("expected models.ErrMissingOCGUsageApp when /OCProperties has no /AS")
	}
}

func TestPDFA2_OCGWithAS(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withOCProperties(true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if containsError(errs, models.ErrMissingOCGUsageApp) {
		t.Error("/OCProperties with /AS should pass")
	}
}

func TestPDFA2_NoOCGPasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if containsError(errs, models.ErrMissingOCGUsageApp) {
		t.Error("document without /OCProperties should not trigger OCG error")
	}
}

func TestPDFA1_SkipsOCGCheck(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withOCProperties(false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if containsError(errs, models.ErrMissingOCGUsageApp) {
		t.Error("PDF/A-1 should not check OCG /AS requirements")
	}
}

// --- PDF/A-2 allows transparency and JPEG2000 ---

func TestPDFA2_TransparencySMaskAllowed(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withTransparency().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if containsError(errs, models.ErrTransparencyForbidden) {
		t.Error("PDF/A-2 should allow transparency")
	}
}

func TestPDFA2_JPXAllowed(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withJPXImage().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if containsError(errs, models.ErrJPEG2000Forbidden) {
		t.Error("PDF/A-2 should allow JPEG2000")
	}
}

func TestPDFA_FontNotEmbedded(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFont("CustomFont", false, true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrFontNotEmbedded) {
		t.Error("expected models.ErrFontNotEmbedded")
	}
}

func TestPDFA_FontEmbedded(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFont("CustomFont", true, true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if containsError(errs, models.ErrFontNotEmbedded) {
		t.Error("embedded font should pass")
	}
}

func TestPDFA_Standard14SkipsEmbedding(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFont("Helvetica", false, false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	// Standard 14 fonts don't need embedding.
	if containsError(errs, models.ErrFontNotEmbedded) {
		t.Error("Standard 14 font should not require embedding")
	}
}

func TestPDFA2u_RequiresUnicodeMapping(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFont("CustomFont", true, false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2u)
	if !containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("expected models.ErrMissingUnicodeMapping for conformance 'u'")
	}
}

func TestPDFA1a_RequiresMarkInfoAndStructTree(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withLang("en").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1a)
	if !containsError(errs, models.ErrMissingMarkInfo) {
		t.Error("expected models.ErrMissingMarkInfo for conformance 'a'")
	}
	if !containsError(errs, models.ErrMissingStructTreeRoot) {
		t.Error("expected models.ErrMissingStructTreeRoot for conformance 'a'")
	}
}

func TestPDFA1a_WithMarkInfoAndStructTree(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withLang("en").withMarkInfo().withStructTreeRoot().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1a)
	if containsError(errs, models.ErrMissingMarkInfo) {
		t.Error("should not get models.ErrMissingMarkInfo when MarkInfo is present")
	}
	if containsError(errs, models.ErrMissingStructTreeRoot) {
		t.Error("should not get models.ErrMissingStructTreeRoot when StructTreeRoot is present")
	}
}

// --- PDF/A Level 'b' base engine tests ---

func TestPDFA_TrailerEncryptKey(t *testing.T) {
	// Even without doc.IsEncrypted, the /Encrypt key in trailer triggers the error.
	m := newMockDoc()
	doc := buildTestDoc(m.withOutputIntents().build())
	doc.Index.Trailer["Encrypt"] = ast.PdfDict{"V": ast.PdfInt(1)}
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrEncryptionForbidden) {
		t.Error("expected models.ErrEncryptionForbidden when /Encrypt in trailer")
	}
}

func TestPDFA_OutputIntentsEmptyArray(t *testing.T) {
	m := newMockDoc().withDeviceRGBImage()
	m.catalog["OutputIntents"] = ast.PdfArray{}
	doc := buildTestDoc(m.build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrMissingICCProfile) {
		t.Error("expected models.ErrMissingICCProfile for empty /OutputIntents array")
	}
}

func TestPDFA_OutputIntentsNoICC(t *testing.T) {
	m := newMockDoc().withDeviceRGBImage()
	m.catalog["OutputIntents"] = ast.PdfArray{
		ast.PdfDict{
			"Type": ast.PdfName("OutputIntent"),
			"S":    ast.PdfName("GTS_PDFA1"),
			// No /DestOutputProfile.
		},
	}
	doc := buildTestDoc(m.build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrMissingICCProfile) {
		t.Error("expected models.ErrMissingICCProfile when /DestOutputProfile is absent")
	}
}

func (b *mockDocBuilder) withLaunchAction() *mockDocBuilder {
	b.catalog["OpenAction"] = ast.PdfDict{
		"S": ast.PdfName("Launch"),
		"F": ast.PdfString("/bin/calc"),
	}
	return b
}

func (b *mockDocBuilder) withSoundAnnotAction() *mockDocBuilder {
	annot := ast.PdfDict{
		"Type":    ast.PdfName("Annot"),
		"Subtype": ast.PdfName("Widget"),
		"A": ast.PdfDict{
			"S": ast.PdfName("Sound"),
		},
	}
	page := ast.PdfDict{
		"Type":   ast.PdfName("Page"),
		"Annots": ast.PdfArray{annot},
	}
	b.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	return b
}

func (b *mockDocBuilder) withMovieAction() *mockDocBuilder {
	b.catalog["OpenAction"] = ast.PdfDict{
		"S": ast.PdfName("Movie"),
	}
	return b
}

func TestPDFA_LaunchActionForbidden(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withLaunchAction().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrActiveContentForbidden) {
		t.Error("expected models.ErrActiveContentForbidden for /Launch action")
	}
}

func TestPDFA_SoundAnnotActionForbidden(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withSoundAnnotAction().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrActiveContentForbidden) {
		t.Error("expected models.ErrActiveContentForbidden for /Sound annotation action")
	}
}

func TestPDFA_MovieActionForbidden(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withMovieAction().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if !containsError(errs, models.ErrActiveContentForbidden) {
		t.Error("expected models.ErrActiveContentForbidden for /Movie action")
	}
}

func TestPDFA_NoActiveContent(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if containsError(errs, models.ErrActiveContentForbidden) {
		t.Error("clean document should not trigger models.ErrActiveContentForbidden")
	}
}

func TestPDFA_FontFile3Accepted(t *testing.T) {
	// A font with /FontFile3 (CFF/OpenType) should count as embedded.
	fontDict := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("CustomCFF"),
		"FontDescriptor": ast.PdfDict{
			"FontFile3": ast.PdfDict{}, // CFF font data.
		},
		"ToUnicode": ast.PdfDict{},
	}
	page := ast.PdfDict{
		"Type": ast.PdfName("Page"),
		"Resources": ast.PdfDict{
			"Font": ast.PdfDict{
				ast.PdfName("F1"): fontDict,
			},
		},
	}
	m := newMockDoc()
	m.catalog["OutputIntents"] = ast.PdfArray{
		ast.PdfDict{
			"Type":              ast.PdfName("OutputIntent"),
			"S":                 ast.PdfName("GTS_PDFA1"),
			"DestOutputProfile": ast.PdfDict{},
		},
	}
	m.catalog["Pages"] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{page},
		"Count": ast.PdfInt(1),
	}
	doc := buildTestDoc(m.build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if containsError(errs, models.ErrFontNotEmbedded) {
		t.Error("font with /FontFile3 should be considered embedded")
	}
}

// --- PDF/A Level 'u' (Unicode) tests ---

func TestPDFA2u_ToUnicodePasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFont("CustomFont", true, true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2u)
	if containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("font with /ToUnicode should pass Level 'u'")
	}
}

func TestPDFA2u_WinAnsiEncodingPasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFontEncoding("CustomFont", true, "WinAnsiEncoding").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2u)
	if containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("font with /WinAnsiEncoding should pass Level 'u'")
	}
}

func TestPDFA2u_MacRomanEncodingPasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFontEncoding("CustomFont", true, "MacRomanEncoding").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2u)
	if containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("font with /MacRomanEncoding should pass Level 'u'")
	}
}

func TestPDFA2u_StandardEncodingPasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFontEncoding("CustomFont", true, "StandardEncoding").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2u)
	if containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("font with /StandardEncoding should pass Level 'u'")
	}
}

func TestPDFA2u_EncodingDictWithBaseEncoding(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFontEncodingDict("CustomFont", true, "WinAnsiEncoding").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2u)
	if containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("font with encoding dict /BaseEncoding WinAnsiEncoding should pass Level 'u'")
	}
}

func TestPDFA2u_NoEncodingNoToUnicodeFails(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFontEncoding("CustomFont", true, "").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2u)
	if !containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("expected models.ErrMissingUnicodeMapping for font without /ToUnicode or encoding")
	}
}

func TestPDFA2u_NonStandardEncodingFails(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFontEncoding("CustomFont", true, "Identity-H").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2u)
	if !containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("expected models.ErrMissingUnicodeMapping for non-standard encoding without /ToUnicode")
	}
}

func TestPDFA3a_ImpliesLevelU(t *testing.T) {
	// Level 'a' implies 'u': a font without Unicode mapping should fail.
	doc := buildTestDoc(newMockDoc().withOutputIntents().withLang("en").withMarkInfo().withStructTreeRoot().withPageFontEncoding("CustomFont", true, "").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA3a)
	if !containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("Level 'a' should also enforce Level 'u' Unicode mapping checks")
	}
}

func TestPDFA1b_SkipsUnicodeCheck(t *testing.T) {
	// Level 'b' should NOT require Unicode mapping.
	doc := buildTestDoc(newMockDoc().withOutputIntents().withPageFont("CustomFont", true, false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("Level 'b' should not enforce Unicode mapping")
	}
}

// --- PDF/A Level 'a' (Accessible) tests ---

func TestPDFA1a_RequiresLang(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withMarkInfo().withStructTreeRoot().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1a)
	if !containsError(errs, models.ErrMissingLang) {
		t.Error("expected models.ErrMissingLang for conformance 'a'")
	}
}

func TestPDFA1a_WithLang(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withLang("en-US").withMarkInfo().withStructTreeRoot().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1a)
	if containsError(errs, models.ErrMissingLang) {
		t.Error("should not get models.ErrMissingLang when /Lang is present")
	}
}

func TestPDFA2a_FigureMissingAlt(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withLang("en").withMarkInfo().withFigureStructTree(false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2a)
	if !containsError(errs, models.ErrUntaggedVisualElement) {
		t.Error("expected models.ErrUntaggedVisualElement for Figure without /Alt")
	}
}

func TestPDFA2a_FigureWithAlt(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withLang("en").withMarkInfo().withFigureStructTree(true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2a)
	if containsError(errs, models.ErrUntaggedVisualElement) {
		t.Error("Figure with /Alt should pass Level 'a'")
	}
}

func TestPDFA2a_MinimalValid(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withLang("en").withMarkInfo().withStructTreeRoot().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2a)
	if len(errs) != 0 {
		t.Errorf("expected no errors for minimal valid PDF/A-2a, got %v", errs)
	}
}

func TestPDFA1b_SkipsFigureAltCheck(t *testing.T) {
	// Level 'b' should NOT check for Figure alt text.
	doc := buildTestDoc(newMockDoc().withOutputIntents().withFigureStructTree(false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if containsError(errs, models.ErrUntaggedVisualElement) {
		t.Error("Level 'b' should not enforce Figure alt text")
	}
}

func TestPDFA2u_SkipsFigureAltCheck(t *testing.T) {
	// Level 'u' should NOT check for Figure alt text (only 'a' does).
	doc := buildTestDoc(newMockDoc().withOutputIntents().withFigureStructTree(false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA2u)
	if containsError(errs, models.ErrUntaggedVisualElement) {
		t.Error("Level 'u' should not enforce Figure alt text")
	}
}

// --- PDF/A-4 version check tests ---

func TestPDFA4_RequiresVersion20(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("1.7").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if !containsError(errs, models.ErrVersionNotPDF20) {
		t.Error("expected models.ErrVersionNotPDF20 for PDF version 1.7")
	}
}

func TestPDFA4_Version20Passes(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if containsError(errs, models.ErrVersionNotPDF20) {
		t.Error("PDF 2.0 should pass PDF/A-4 version check")
	}
}

func TestPDFA4_NoCatalogVersionFails(t *testing.T) {
	// No catalog /Version and no real file header — should fail for Part 4.
	doc := buildTestDoc(newMockDoc().withOutputIntents().build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if !containsError(errs, models.ErrVersionNotPDF20) {
		t.Error("expected models.ErrVersionNotPDF20 when no version 2.0 declared")
	}
}

// --- PDF/A-4 conformance deprecation tests ---

func TestPDFA4_SkipsMarkInfoCheck(t *testing.T) {
	// Part 4 does not enforce 'a' conformance — no MarkInfo required.
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if containsError(errs, models.ErrMissingMarkInfo) {
		t.Error("PDF/A-4 should not enforce MarkInfo (no a/b/u conformance)")
	}
}

func TestPDFA4_SkipsStructTreeRoot(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if containsError(errs, models.ErrMissingStructTreeRoot) {
		t.Error("PDF/A-4 should not enforce StructTreeRoot")
	}
}

func TestPDFA4_SkipsLangCheck(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if containsError(errs, models.ErrMissingLang) {
		t.Error("PDF/A-4 should not enforce /Lang")
	}
}

// --- PDF/A-4 natively requires Unicode ---

func TestPDFA4_RequiresUnicodeMapping(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").withPageFont("CustomFont", true, false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if !containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("expected models.ErrMissingUnicodeMapping for PDF/A-4")
	}
}

func TestPDFA4_UnicodeMappingPasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").withPageFont("CustomFont", true, true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if containsError(errs, models.ErrMissingUnicodeMapping) {
		t.Error("font with /ToUnicode should pass PDF/A-4")
	}
}

// --- PDF/A-4 OCG check (inherited) ---

func TestPDFA4_OCGMissingAS(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").withOCProperties(false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if !containsError(errs, models.ErrMissingOCGUsageApp) {
		t.Error("expected models.ErrMissingOCGUsageApp for PDF/A-4")
	}
}

func TestPDFA4_OCGWithAS(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").withOCProperties(true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if containsError(errs, models.ErrMissingOCGUsageApp) {
		t.Error("PDF/A-4 /OCProperties with /AS should pass")
	}
}

// --- PDF/A-4f tests ---

func TestPDFA4f_RequiresAFRelationship(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").withEmbeddedFileAFRelationship("").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4f)
	if !containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("expected models.ErrMissingAFRelationship for PDF/A-4f")
	}
}

func TestPDFA4f_ValidAFRelationship(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").withEmbeddedFileAFRelationship("Data").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4f)
	if containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("PDF/A-4f with valid /AFRelationship should pass")
	}
}

func TestPDFA4f_NonPDFAttachmentAllowed(t *testing.T) {
	// PDF/A-4f allows any file type (like A-3).
	fs := ast.PdfDict{
		"Type":           ast.PdfName("Filespec"),
		"F":              ast.PdfString("data.xlsx"),
		"AFRelationship": ast.PdfName("Data"),
		"EF": ast.PdfDict{
			"F": ast.PdfStream{
				Dict: ast.PdfDict{},
				Data: []byte("PK\x03\x04spreadsheet-data"),
			},
		},
	}
	m := newMockDoc().withOutputIntents().withCatalogVersion("2.0")
	m.catalog["Names"] = ast.PdfDict{
		"EmbeddedFiles": ast.PdfDict{
			"Names": ast.PdfArray{
				ast.PdfString("data.xlsx"),
				fs,
			},
		},
	}
	doc := buildTestDoc(m.build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4f)
	if containsError(errs, models.ErrNonPDFAAttachment) {
		t.Error("PDF/A-4f should allow non-PDF attachments")
	}
}

func TestPDFA4_BaseSkipsAFRelationship(t *testing.T) {
	// Base models.PDFA4 (no 'f' suffix) should not check /AFRelationship.
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").withEmbeddedFileAFRelationship("").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if containsError(errs, models.ErrMissingAFRelationship) {
		t.Error("base PDF/A-4 should not check /AFRelationship")
	}
}

// --- PDF/A-4e 3D stream compliance tests ---

func TestPDFA4e_Valid3DAnnotation(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").with3DAnnotation("U3D", true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4e)
	if containsError(errs, models.ErrMissing3DSubtype) {
		t.Error("valid /3D annotation with U3D subtype should pass")
	}
	if containsError(errs, models.ErrMissing3DFallback) {
		t.Error("valid /3D annotation with /AP /N should pass")
	}
}

func TestPDFA4e_PRCSubtypePasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").with3DAnnotation("PRC", true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4e)
	if containsError(errs, models.ErrMissing3DSubtype) {
		t.Error("PRC subtype should pass")
	}
}

func TestPDFA4e_Invalid3DSubtype(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").with3DAnnotation("STEP", true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4e)
	if !containsError(errs, models.ErrMissing3DSubtype) {
		t.Error("expected models.ErrMissing3DSubtype for invalid 3D stream /Subtype")
	}
}

func TestPDFA4e_Missing3DFallback(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").with3DAnnotation("U3D", false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4e)
	if !containsError(errs, models.ErrMissing3DFallback) {
		t.Error("expected models.ErrMissing3DFallback when /AP /N is absent")
	}
}

func TestPDFA4e_Missing3DSubtypeAndFallback(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").with3DAnnotation("STEP", false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4e)
	if !containsError(errs, models.ErrMissing3DSubtype) {
		t.Error("expected models.ErrMissing3DSubtype")
	}
	if !containsError(errs, models.ErrMissing3DFallback) {
		t.Error("expected models.ErrMissing3DFallback")
	}
}

func TestPDFA4e_RichMediaValid(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").withRichMediaAnnotation("U3D", true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4e)
	if containsError(errs, models.ErrMissing3DSubtype) {
		t.Error("valid RichMedia with U3D should pass")
	}
	if containsError(errs, models.ErrMissing3DFallback) {
		t.Error("RichMedia with /AP /N should pass")
	}
}

func TestPDFA4e_RichMediaPRCPasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").withRichMediaAnnotation("PRC", true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4e)
	if containsError(errs, models.ErrMissing3DSubtype) {
		t.Error("RichMedia with PRC subtype should pass")
	}
}

func TestPDFA4e_RichMediaInvalidSubtype(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").withRichMediaAnnotation("STEP", true).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4e)
	if !containsError(errs, models.ErrMissing3DSubtype) {
		t.Error("expected models.ErrMissing3DSubtype for RichMedia with invalid stream subtype")
	}
}

func TestPDFA4e_RichMediaMissingFallback(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").withRichMediaAnnotation("U3D", false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4e)
	if !containsError(errs, models.ErrMissing3DFallback) {
		t.Error("expected models.ErrMissing3DFallback for RichMedia without /AP /N")
	}
}

func TestPDFA4_BaseSkips3DCheck(t *testing.T) {
	// Base models.PDFA4 (no 'e' suffix) should not check 3D annotations.
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").with3DAnnotation("STEP", false).build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4)
	if containsError(errs, models.ErrMissing3DSubtype) || containsError(errs, models.ErrMissing3DFallback) {
		t.Error("base PDF/A-4 should not enforce 3D stream compliance")
	}
}

func TestPDFA4e_NoAnnotationsPasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4e)
	if containsError(errs, models.ErrMissing3DSubtype) || containsError(errs, models.ErrMissing3DFallback) {
		t.Error("document without 3D annotations should pass")
	}
}

func TestPDFA4e_MinimalValid(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4e)
	if len(errs) != 0 {
		t.Errorf("expected no errors for minimal valid PDF/A-4e, got %v", errs)
	}
}

func TestPDFA4f_MinimalValid(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withOutputIntents().withCatalogVersion("2.0").build())
	errs := ValidatePDFA(context.Background(), doc, models.PDFA4f)
	if len(errs) != 0 {
		t.Errorf("expected no errors for minimal valid PDF/A-4f, got %v", errs)
	}
}

// --- PDF/UA tests ---

func TestPDFUA_InvalidProfile(t *testing.T) {
	errs := ValidatePDFUA(context.Background(), buildTestDoc(newMockDoc().build()), models.PDFUAProfile("bogus"))
	if len(errs) != 1 || !errors.Is(errs[0], models.ErrInvalidProfile) {
		t.Errorf("expected models.ErrInvalidProfile, got %v", errs)
	}
}

func TestPDFUA1_MinimalValid(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withFigureWithAlt().withXMPTitle().build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid PDF/UA-1, got %v", errs)
	}
}

func TestPDFUA_MissingLang(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withMarkInfo().withStructTreeRoot().build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if !containsError(errs, models.ErrMissingLang) {
		t.Error("expected models.ErrMissingLang")
	}
}

func TestPDFUA_FigureMissingAlt(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withFigureNoAlt().build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if !containsError(errs, models.ErrImageMissingAltText) {
		t.Error("expected models.ErrImageMissingAltText")
	}
}

func TestPDFUA_FigureWithAlt(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withFigureWithAlt().withXMPTitle().build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if containsError(errs, models.ErrImageMissingAltText) {
		t.Error("figure with /Alt should pass")
	}
}

func TestPDFUA_HeaderMustBeArtifact(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withHeaderStructElem().build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if !containsError(errs, models.ErrArtifactTagMissing) {
		t.Error("expected models.ErrArtifactTagMissing for /Header struct elem")
	}
}

func TestPDFUA_MissingXMPTitle(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().withXMPNoTitle().build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if !containsError(errs, models.ErrMissingXMPTitle) {
		t.Error("expected models.ErrMissingXMPTitle when dc:title is missing from XMP")
	}
}

func TestPDFUA_WithXMPTitle(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().withXMPTitle().build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if containsError(errs, models.ErrMissingXMPTitle) {
		t.Error("should not get models.ErrMissingXMPTitle when dc:title is present")
	}
}

func TestPDFUA_NoMetadataStream(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if !containsError(errs, models.ErrMissingXMPTitle) {
		t.Error("expected models.ErrMissingXMPTitle when no /Metadata stream exists")
	}
}

func TestPDFUA_FormFieldMissingTooltip(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().withXMPTitle().withFormField(false).build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if !containsError(errs, models.ErrMissingFormFieldTooltip) {
		t.Error("expected models.ErrMissingFormFieldTooltip")
	}
}

func TestPDFUA_FormFieldWithTooltip(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().withXMPTitle().withFormField(true).build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if containsError(errs, models.ErrMissingFormFieldTooltip) {
		t.Error("form field with /TU should pass")
	}
}

func TestPDFUA1_NonStandardTagNoRoleMap(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withXMPTitle().withNonStandardTag("CustomTag", "").build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if !containsError(errs, models.ErrNonStandardTag) {
		t.Error("expected models.ErrNonStandardTag for unmapped custom tag")
	}
}

func TestPDFUA1_NonStandardTagWithRoleMap(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withXMPTitle().withNonStandardTag("CustomTag", "P").build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if containsError(errs, models.ErrNonStandardTag) {
		t.Error("custom tag with /RoleMap entry should pass")
	}
}

func TestPDFUA1_StandardTagPasses(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withXMPTitle().withNonStandardTag("P", "").build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if containsError(errs, models.ErrNonStandardTag) {
		t.Error("standard tag /P should pass without /RoleMap")
	}
}

func TestPDFUA2_RequiresVersion20(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().withXMPTitle().withNamespaces(true).build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if !containsError(errs, models.ErrInvalidVersionForUA2) {
		t.Error("expected models.ErrInvalidVersionForUA2 when no version 2.0 declared")
	}
}

func TestPDFUA2_WithVersion20(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().withXMPTitle().withCatalogVersion("2.0").withNamespaces(true).build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if containsError(errs, models.ErrInvalidVersionForUA2) {
		t.Error("should pass with catalog /Version 2.0")
	}
}

func TestPDFUA2_MissingNamespaces(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().withXMPTitle().withCatalogVersion("2.0").build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if !containsError(errs, models.ErrMissingNamespaces) {
		t.Error("expected models.ErrMissingNamespaces")
	}
}

func TestPDFUA2_InvalidNamespace(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().withXMPTitle().withCatalogVersion("2.0").withNamespaces(false).build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if !containsError(errs, models.ErrInvalidStructureNamespace) {
		t.Error("expected models.ErrInvalidStructureNamespace for namespace missing /NS")
	}
}

func TestPDFUA2_ValidNamespaces(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().withXMPTitle().withCatalogVersion("2.0").withNamespaces(true).build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if containsError(errs, models.ErrMissingNamespaces) || containsError(errs, models.ErrInvalidStructureNamespace) {
		t.Error("valid namespaces should pass")
	}
}

func TestPDFUA2_LinkAnnotMissingStructParent(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().withXMPTitle().withCatalogVersion("2.0").withNamespaces(true).withLinkAnnotation(false).build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if !containsError(errs, models.ErrLinkNotNested) {
		t.Error("expected models.ErrLinkNotNested for link annot without /StructParent")
	}
}

func TestPDFUA2_LinkAnnotWithStructParentAndStructElem(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withXMPTitle().withCatalogVersion("2.0").withNamespaces(true).withLinkAnnotation(true).withLinkStructElem().build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if containsError(errs, models.ErrLinkNotNested) {
		t.Error("link annot with /StructParent and /Link struct elem should pass")
	}
}

func TestPDFUA2_FormulaMissingAF(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withXMPTitle().withCatalogVersion("2.0").withNamespaces(true).withFormulaNoAF().build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if !containsError(errs, models.ErrFormulaMissingMathML) {
		t.Error("expected models.ErrFormulaMissingMathML for /Formula without /AF")
	}
}

func TestPDFUA2_FormulaWithAF(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withXMPTitle().withCatalogVersion("2.0").withNamespaces(true).withFormulaWithAF().build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if containsError(errs, models.ErrFormulaMissingMathML) {
		t.Error("formula with /AF should pass")
	}
}

func TestPDFUA2_MinimalValid(t *testing.T) {
	doc := buildTestDoc(newMockDoc().withLang("en").withMarkInfo().withStructTreeRoot().withXMPTitle().withCatalogVersion("2.0").withNamespaces(true).build())
	errs := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid PDF/UA-2, got %v", errs)
	}
}

// --- Profile helpers ---

func TestPdfaPart(t *testing.T) {
	tests := []struct {
		profile models.PDFAProfile
		want    int
	}{
		{models.PDFA1b, 1},
		{models.PDFA1a, 1},
		{models.PDFA2b, 2},
		{models.PDFA2u, 2},
		{models.PDFA2a, 2},
		{models.PDFA3b, 3},
		{models.PDFA3u, 3},
		{models.PDFA3a, 3},
		{models.PDFA4, 4},
		{models.PDFA4e, 4},
		{models.PDFA4f, 4},
	}
	for _, tt := range tests {
		if got := PdfaPart(tt.profile); got != tt.want {
			t.Errorf("PdfaPart(%s) = %d, want %d", tt.profile, got, tt.want)
		}
	}
}

func TestPdfaConformance(t *testing.T) {
	tests := []struct {
		profile models.PDFAProfile
		want    byte
	}{
		{models.PDFA1b, 'b'},
		{models.PDFA1a, 'a'},
		{models.PDFA2u, 'u'},
		{models.PDFA2a, 'a'},
		{models.PDFA4, 0},
		{models.PDFA4e, 'e'},
		{models.PDFA4f, 'f'},
	}
	for _, tt := range tests {
		if got := PdfaConformance(tt.profile); got != tt.want {
			t.Errorf("PdfaConformance(%s) = %c, want %c", tt.profile, got, tt.want)
		}
	}
}

// --- Test helpers ---

func containsError(errs []error, target error) bool {
	for _, e := range errs {
		if errors.Is(e, target) {
			return true
		}
	}
	return false
}

// buildTestDoc creates a real document.Document from a mock, injecting the
// catalog dict into the trailer as /Root so the compliance functions can access it.
func buildTestDoc(m *mockDocument) *document.Document {
	doc := &document.Document{
		IsEncrypted: m.encrypt,
		Logger:      document.NopLogger(),
	}
	doc.Index = xref.Index{
		Entries: make(map[int]xref.XRefEntry),
		Trailer: ast.PdfDict{
			"Root": m.catalog,
		},
	}
	return doc
}
