package signature

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
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

func TestClassifyModifications_P3_AllowsAnnotation(t *testing.T) {
	objects := []appendedObject{
		{ObjNum: 10, Type: "Annot"},
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
		{"Annot type", appendedObject{Type: "Annot"}, true},
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
		{"Annot", appendedObject{Type: "Annot"}, true},
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
