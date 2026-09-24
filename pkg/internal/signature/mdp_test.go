package signature

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

func TestExtractMDPPolicy_DocMDP(t *testing.T) {
	sigDict := ast.PdfDict{
		"Reference": ast.PdfArray{
			ast.PdfDict{
				"TransformMethod": ast.PdfName("DocMDP"),
				"TransformParams": ast.PdfDict{
					"P": ast.PdfInt(1),
				},
			},
		},
	}

	p := extractMDPPolicy(context.Background(), nil, sigDict)
	if p != 1 {
		t.Errorf("expected P=1, got %d", p)
	}
}

func TestExtractMDPPolicy_FieldMDP(t *testing.T) {
	sigDict := ast.PdfDict{
		"Reference": ast.PdfArray{
			ast.PdfDict{
				"TransformMethod": ast.PdfName("FieldMDP"),
				"TransformParams": ast.PdfDict{
					"P": ast.PdfInt(2),
				},
			},
		},
	}

	p := extractMDPPolicy(context.Background(), nil, sigDict)
	if p != 2 {
		t.Errorf("expected P=2, got %d", p)
	}
}

func TestExtractMDPPolicy_DefaultsTo2ForDocMDP(t *testing.T) {
	// DocMDP without /P defaults to 2.
	sigDict := ast.PdfDict{
		"Reference": ast.PdfArray{
			ast.PdfDict{
				"TransformMethod": ast.PdfName("DocMDP"),
				"TransformParams": ast.PdfDict{},
			},
		},
	}

	p := extractMDPPolicy(context.Background(), nil, sigDict)
	if p != 2 {
		t.Errorf("expected default P=2, got %d", p)
	}
}

// TestExtractMDPPolicy_RealP covers ISO 32000-1, 12.8.2.2, Table 254 typing P
// as a number: a real equal to 1, 2 or 3 names that permission, and any other
// value reads as an absent P.
func TestExtractMDPPolicy_RealP(t *testing.T) {
	tests := []struct {
		name   string
		method ast.PdfName
		p      ast.PdfObject
		want   int
	}{
		{"DocMDP 1.0", "DocMDP", ast.PdfReal(1.0), 1},
		{"DocMDP 2.0", "DocMDP", ast.PdfReal(2.0), 2},
		{"DocMDP 3.0", "DocMDP", ast.PdfReal(3.0), 3},
		{"DocMDP 2.5 defaults", "DocMDP", ast.PdfReal(2.5), 2},
		{"DocMDP 0.0 defaults", "DocMDP", ast.PdfReal(0), 2},
		{"DocMDP 4 defaults", "DocMDP", ast.PdfInt(4), 2},
		{"DocMDP name defaults", "DocMDP", ast.PdfName("1"), 2},
		{"FieldMDP 3.0", "FieldMDP", ast.PdfReal(3.0), 3},
		{"FieldMDP 2.5 has no default", "FieldMDP", ast.PdfReal(2.5), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sigDict := ast.PdfDict{
				"Reference": ast.PdfArray{
					ast.PdfDict{
						"TransformMethod": tc.method,
						"TransformParams": ast.PdfDict{"P": tc.p},
					},
				},
			}
			if got := extractMDPPolicy(context.Background(), nil, sigDict); got != tc.want {
				t.Errorf("/P %v: got P=%d, want %d", tc.p, got, tc.want)
			}
		})
	}
}

func TestExtractMDPPolicy_NoReference(t *testing.T) {
	sigDict := ast.PdfDict{
		"ByteRange": ast.PdfArray{ast.PdfInt(0)},
	}

	p := extractMDPPolicy(context.Background(), nil, sigDict)
	if p != 0 {
		t.Errorf("expected P=0, got %d", p)
	}
}

func TestExtractMDPPolicy_NonMDPTransform(t *testing.T) {
	sigDict := ast.PdfDict{
		"Reference": ast.PdfArray{
			ast.PdfDict{
				"TransformMethod": ast.PdfName("UR"),
				"TransformParams": ast.PdfDict{
					"P": ast.PdfInt(1),
				},
			},
		},
	}

	p := extractMDPPolicy(context.Background(), nil, sigDict)
	if p != 0 {
		t.Errorf("expected P=0 for non-MDP transform, got %d", p)
	}
}

func TestClassifyModifications_P1_RejectsAny(t *testing.T) {
	objects := []appendedObject{
		{ObjNum: 10, Type: "Font"},
	}

	violation := classifyModifications(objects, 1)
	if violation == "" {
		t.Error("expected violation for P=1 with any appended object")
	}
}

func TestClassifyModifications_P1_AllowsXRef(t *testing.T) {
	objects := []appendedObject{
		{ObjNum: 10, Type: "XRef", IsXRef: true},
	}

	violation := classifyModifications(objects, 1)
	if violation != "" {
		t.Errorf("unexpected violation for P=1 with only XRef: %s", violation)
	}
}

func TestClassifyModifications_P2_AllowsFormField(t *testing.T) {
	objects := []appendedObject{
		{ObjNum: 10, FT: "Tx"},
		{ObjNum: 11, Type: "Sig"},
	}

	violation := classifyModifications(objects, 2)
	if violation != "" {
		t.Errorf("unexpected violation for P=2 with form fields: %s", violation)
	}
}

func TestClassifyModifications_P2_RejectsPage(t *testing.T) {
	// Pages are allowed (structural update for signatures), but new content
	// types like Font are not.
	objects := []appendedObject{
		{ObjNum: 10, Type: "Font"},
	}

	violation := classifyModifications(objects, 2)
	if violation == "" {
		t.Error("expected violation for P=2 with Font object")
	}
}

func TestClassifyModifications_P2_RejectsNonWidgetAnnotation(t *testing.T) {
	objects := []appendedObject{
		{ObjNum: 10, Type: "Annot", Subtype: "Text", IsAnnot: true},
	}

	violation := classifyModifications(objects, 2)
	if violation == "" {
		t.Error("expected violation for P=2 with a /Text annotation")
	}
}

func TestClassifyModifications_P3_AllowsAnnotation(t *testing.T) {
	objects := []appendedObject{
		{ObjNum: 10, Type: "Annot", Subtype: "Text", IsAnnot: true},
		{ObjNum: 11, FT: "Tx"},
	}

	violation := classifyModifications(objects, 3)
	if violation != "" {
		t.Errorf("unexpected violation for P=3 with annotations: %s", violation)
	}
}

func TestClassifyModifications_P3_RejectsFont(t *testing.T) {
	objects := []appendedObject{
		{ObjNum: 10, Type: "Font"},
	}

	violation := classifyModifications(objects, 3)
	if violation == "" {
		t.Error("expected violation for P=3 with Font object")
	}
}

func TestClassifyModifications_EmptyObjects(t *testing.T) {
	violation := classifyModifications(nil, 1)
	if violation != "" {
		t.Errorf("unexpected violation for empty objects: %s", violation)
	}
}

func TestEvaluateMDP_NoPolicy(t *testing.T) {
	sig := models.Signature{MDPPolicy: 0}
	result := models.SignatureValidation{}
	evaluateMDP(context.Background(), nil, sig, &result)

	if result.MDPPolicy != 0 {
		t.Errorf("expected MDPPolicy=0, got %d", result.MDPPolicy)
	}
	if result.MDPInfo != "" {
		t.Errorf("expected empty MDPInfo, got %q", result.MDPInfo)
	}
}

func TestIsAllowedP2(t *testing.T) {
	tests := []struct {
		name string
		obj  appendedObject
		want bool
	}{
		{"Sig type", appendedObject{Type: "Sig"}, true},
		{"Widget annot", appendedObject{Type: "Annot", Subtype: "Widget", IsAnnot: true}, true},
		{"Typeless widget", appendedObject{Subtype: "Widget", FT: "Tx", IsAnnot: true}, true},
		{"Text annot", appendedObject{Type: "Annot", Subtype: "Text", IsAnnot: true}, false},
		{"Typeless FreeText annot", appendedObject{Subtype: "FreeText", IsAnnot: true}, false},
		{"Annot without subtype", appendedObject{Type: "Annot", IsAnnot: true}, false},
		{"Catalog", appendedObject{Type: "Catalog"}, true},
		{"Page", appendedObject{Type: "Page"}, true},
		{"Form field Tx", appendedObject{FT: "Tx"}, true},
		{"Form field Sig", appendedObject{FT: "Sig"}, true},
		{"Untyped", appendedObject{}, true},
		{"Font type", appendedObject{Type: "Font"}, false},
		{"Image type", appendedObject{Type: "XObject"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isAllowedP2(tc.obj)
			if got != tc.want {
				t.Errorf("isAllowedP2(%+v) = %v, want %v", tc.obj, got, tc.want)
			}
		})
	}
}

func TestIsAllowedP3(t *testing.T) {
	tests := []struct {
		name string
		obj  appendedObject
		want bool
	}{
		{"Text annot", appendedObject{Type: "Annot", Subtype: "Text", IsAnnot: true}, true},
		{"Typeless FreeText annot", appendedObject{Subtype: "FreeText", IsAnnot: true}, true},
		{"Widget annot", appendedObject{Type: "Annot", Subtype: "Widget", IsAnnot: true}, true},
		{"Form field", appendedObject{FT: "Tx"}, true},
		{"Font", appendedObject{Type: "Font"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isAllowedP3(tc.obj)
			if got != tc.want {
				t.Errorf("isAllowedP3(%+v) = %v, want %v", tc.obj, got, tc.want)
			}
		})
	}
}

func TestIsAnnotationDict(t *testing.T) {
	rect := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(10), ast.PdfInt(10)}
	tests := []struct {
		name string
		dict ast.PdfDict
		want bool
	}{
		{"typed", ast.PdfDict{"Type": ast.PdfName("Annot"), "Subtype": ast.PdfName("Text"), "Rect": rect}, true},
		{"typeless", ast.PdfDict{"Subtype": ast.PdfName("FreeText"), "Rect": rect}, true},
		{"typeless unlisted subtype", ast.PdfDict{"Subtype": ast.PdfName("Arrow"), "Rect": rect}, true},
		{"typeless without Rect", ast.PdfDict{"Subtype": ast.PdfName("Type1")}, false},
		{"typeless without Subtype", ast.PdfDict{"FT": ast.PdfName("Tx"), "Rect": rect}, false},
		{"other type", ast.PdfDict{"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form"), "Rect": rect}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAnnotationDict(tc.dict); got != tc.want {
				t.Errorf("isAnnotationDict(%v) = %v, want %v", tc.dict, got, tc.want)
			}
		})
	}
}

// updatedObject is one object an incremental update defines.
type updatedObject struct {
	num  int
	body string
}

// appendUpdate appends an incremental update (ISO 32000-1, 7.5.6) that defines
// objs to base, chaining its cross-reference section to base's by /Prev.
func appendUpdate(t *testing.T, base []byte, objs ...updatedObject) []byte {
	t.Helper()

	marker := []byte("startxref\n")
	at := bytes.LastIndex(base, marker)
	if at < 0 {
		t.Fatal("base has no startxref")
	}
	var prev int
	if _, err := fmt.Sscanf(string(base[at+len(marker):]), "%d", &prev); err != nil {
		t.Fatalf("reading base startxref: %v", err)
	}

	var buf bytes.Buffer
	buf.Write(base)
	offs := make([]int, len(objs))
	size := 0
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", o.num, o.body)
		size = max(size, o.num+1)
	}
	xref := buf.Len()
	buf.WriteString("xref\n")
	for i, o := range objs {
		fmt.Fprintf(&buf, "%d 1\n%010d 00000 n \r\n", o.num, offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", size, prev, xref)
	return buf.Bytes()
}

// TestEvaluateMDP_AppendedAnnotations covers ISO 32000-1, 12.8.2.2, Table 254:
// P=2 permits filling in forms and signing, which touch only widget
// annotations, and P=3 adds annotation creation and modification.
func TestEvaluateMDP_AppendedAnnotations(t *testing.T) {
	base := buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 6 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 10 10] /P 3 0 R /V 5 0 R >>",
		"<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P 2 /V /1.2 >> >>] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R >>",
	}, "")
	page := func(annots string) updatedObject {
		return updatedObject{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [" + annots + "] >>"}
	}
	filledWidget := []updatedObject{
		{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /V (Jane) >>"},
	}
	textNote := []updatedObject{
		{7, "<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /P 3 0 R /Contents (note) >>"},
		page("4 0 R 6 0 R 7 0 R"),
	}
	typelessFreeText := []updatedObject{
		{7, "<< /Subtype /FreeText /Rect [200 200 300 220] /DA (/Helv 12 Tf 0 g) /Contents (typed) >>"},
		page("4 0 R 6 0 R 7 0 R"),
	}

	tests := []struct {
		name   string
		policy int
		update []updatedObject
		want   bool
	}{
		{"P=2 filled widget", 2, filledWidget, true},
		{"P=2 Text annotation", 2, textNote, false},
		{"P=2 typeless FreeText annotation", 2, typelessFreeText, false},
		{"P=3 filled widget", 3, filledWidget, true},
		{"P=3 Text annotation", 3, textNote, true},
		{"P=3 typeless FreeText annotation", 3, typelessFreeText, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			data := appendUpdate(t, base, tc.update...)
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}

			// The signature covers every byte of base, so the update is all
			// that follows it.
			sig := models.Signature{
				MDPPolicy: tc.policy,
				ByteRange: [4]int64{0, 1, 1, int64(len(base)) - 1},
			}
			var result models.SignatureValidation
			evaluateMDP(ctx, doc, sig, &result)

			if result.MDPValid != tc.want {
				t.Errorf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if !tc.want && !strings.Contains(result.MDPInfo, "violation") {
				t.Errorf("MDPInfo %q does not report the violation", result.MDPInfo)
			}
		})
	}
}
