package export

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

func sampleDocument() *Document {
	return &Document{
		Metadata: map[string]string{
			"Title":  "Test PDF",
			"Author": "litho",
		},
		Bookmarks: []models.Bookmark{
			{Title: "Chapter 1", PageNumber: 1},
		},
		FormData: map[string]string{
			"name": "John",
		},
		Pages: []PageData{
			{
				Page: models.Page{
					PageNumber: 1,
					Width:      612,
					Height:     792,
					MediaBox:   [4]float64{0, 0, 612, 792},
					CropBox:    [4]float64{0, 0, 612, 792},
				},
				Text: "Hello, World!",
				TextElements: []models.TextElement{
					{Text: "Hello,", X: 72, Y: 720, FontSize: 12},
					{Text: "World!", X: 120, Y: 720, FontSize: 12},
				},
				Annotations: []models.Annotation{
					{Subtype: "Link", URI: "https://example.com", Rect: [4]float64{72, 700, 200, 720}},
				},
				Tables: []models.Table{
					{
						Rows: [][]string{{"A", "B"}, {"1", "2"}},
						BBox: [4]float64{72, 500, 500, 600},
					},
				},
			},
		},
		Signatures: []models.SignatureValidation{
			{FieldName: "Sig1", HashValid: true, CertChainValid: true, SignerCN: "Test Signer"},
		},
		DSS: &models.DSSInfo{
			CertCount: 2,
			CRLCount:  1,
		},
		Lexicons: []models.PronunciationLexicon{
			{XML: "<lexicon/>"},
		},
		Formulas: []models.FormulaElement{
			{MathML: "<math><mi>x</mi></math>", MCIDs: []int{1, 2}},
		},
		IsPortfolio:  false,
		IsLinearized: true,
		Linearization: &models.LinearizationParams{
			FileLength: 12345,
			NumPages:   1,
		},
	}
}

func TestJSON(t *testing.T) {
	doc := sampleDocument()
	var buf bytes.Buffer
	if err := JSON(&buf, doc); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if len(output) == 0 {
		t.Fatal("JSON output is empty")
	}

	// Should be valid JSON.
	var parsed Document
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON output is not valid JSON: %v", err)
	}

	// Spot-check fields.
	if parsed.Metadata["Title"] != "Test PDF" {
		t.Errorf("metadata Title = %q, want %q", parsed.Metadata["Title"], "Test PDF")
	}
	if len(parsed.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(parsed.Pages))
	}
	if parsed.Pages[0].Text != "Hello, World!" {
		t.Errorf("page text = %q, want %q", parsed.Pages[0].Text, "Hello, World!")
	}
	if len(parsed.Pages[0].TextElements) != 2 {
		t.Errorf("text elements = %d, want 2", len(parsed.Pages[0].TextElements))
	}
	if parsed.IsLinearized != true {
		t.Error("IsLinearized should be true")
	}
	if parsed.Linearization.FileLength != 12345 {
		t.Errorf("linearization file length = %d, want 12345", parsed.Linearization.FileLength)
	}
}

func TestJSONCompact(t *testing.T) {
	doc := sampleDocument()
	var buf bytes.Buffer
	if err := JSONCompact(&buf, doc); err != nil {
		t.Fatal(err)
	}

	output := buf.String()

	// Compact JSON should not contain newlines within the JSON object itself.
	// (json.Encoder appends a trailing newline, so check for indentation instead.)
	var pretty bytes.Buffer
	if err := JSON(&pretty, doc); err != nil {
		t.Fatal(err)
	}
	if len(output) >= pretty.Len() {
		t.Error("compact JSON should be shorter than pretty-printed JSON")
	}

	// Verify it's valid JSON.
	var parsed Document
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("compact JSON is not valid: %v", err)
	}
}

func TestMarshalJSON(t *testing.T) {
	doc := sampleDocument()
	data, err := MarshalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}

	var parsed Document
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Metadata["Author"] != "litho" {
		t.Errorf("Author = %q, want %q", parsed.Metadata["Author"], "litho")
	}
}

func TestMarshalJSONCompact(t *testing.T) {
	doc := sampleDocument()
	data, err := MarshalJSONCompact(doc)
	if err != nil {
		t.Fatal(err)
	}

	var parsed Document
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Bookmarks) != 1 {
		t.Errorf("bookmarks = %d, want 1", len(parsed.Bookmarks))
	}
	if parsed.Bookmarks[0].Title != "Chapter 1" {
		t.Errorf("bookmark title = %q, want %q", parsed.Bookmarks[0].Title, "Chapter 1")
	}
}

func TestJSON_EmptyDocument(t *testing.T) {
	doc := &Document{
		Pages: []PageData{},
	}
	var buf bytes.Buffer
	if err := JSON(&buf, doc); err != nil {
		t.Fatal(err)
	}

	var parsed Document
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Pages) != 0 {
		t.Errorf("pages = %d, want 0", len(parsed.Pages))
	}
	if parsed.Metadata != nil {
		t.Error("expected nil metadata for empty doc")
	}
}

func TestJSON_OmitsEmptyFields(t *testing.T) {
	doc := &Document{
		Pages: []PageData{
			{
				Page: models.Page{PageNumber: 1},
			},
		},
	}
	data, err := MarshalJSONCompact(doc)
	if err != nil {
		t.Fatal(err)
	}

	// Fields with omitempty should not appear when zero.
	s := string(data)
	if bytes.Contains(data, []byte(`"text"`)) {
		t.Errorf("empty text should be omitted, got: %s", s)
	}
	if bytes.Contains(data, []byte(`"bookmarks"`)) {
		t.Errorf("nil bookmarks should be omitted, got: %s", s)
	}
	if bytes.Contains(data, []byte(`"signatures"`)) {
		t.Errorf("nil signatures should be omitted, got: %s", s)
	}
}

func TestJSON_EscapeHTML_Disabled(t *testing.T) {
	doc := &Document{
		Pages: []PageData{
			{
				Page: models.Page{PageNumber: 1},
				Text: "a < b & c > d",
			},
		},
	}
	var buf bytes.Buffer
	if err := JSON(&buf, doc); err != nil {
		t.Fatal(err)
	}

	// With SetEscapeHTML(false), < > & should NOT be escaped to unicode escapes.
	output := buf.String()
	if bytes.Contains(buf.Bytes(), []byte(`\u003c`)) {
		t.Errorf("HTML escaping should be disabled, got: %s", output)
	}
}

func TestJSON_RoundTrip_AllFields(t *testing.T) {
	doc := sampleDocument()
	doc.EmbeddedFiles = []models.EmbeddedFile{
		{Name: "test.txt", MIMEType: "text/plain", Data: []byte("hello")},
	}
	doc.HintTable = []models.PageOffsetEntry{
		{ObjCount: 5, PageLength: 1024, ContentStreamOffset: 200},
	}
	doc.Pages[0].Images = []models.ImageMeta{
		{Filename: "img0", Width: 100, Height: 50, ColorSpace: "DeviceRGB", BitsPerComponent: 8},
	}
	doc.Pages[0].Measurements = []models.Measurement{
		{Source: "Viewport", ScaleX: models.ScaleAxis{Unit: "ft", ConversionFactor: 0.5}},
	}

	data, err := MarshalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}

	var parsed Document
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	// Check embedded file round-trip (base64 encoded in JSON).
	if len(parsed.EmbeddedFiles) != 1 {
		t.Fatalf("embedded files = %d, want 1", len(parsed.EmbeddedFiles))
	}
	if string(parsed.EmbeddedFiles[0].Data) != "hello" {
		t.Errorf("embedded file data = %q, want %q", parsed.EmbeddedFiles[0].Data, "hello")
	}

	// Check hint table.
	if len(parsed.HintTable) != 1 || parsed.HintTable[0].ObjCount != 5 {
		t.Errorf("hint table mismatch")
	}

	// Check images.
	if len(parsed.Pages[0].Images) != 1 || parsed.Pages[0].Images[0].Width != 100 {
		t.Errorf("images mismatch")
	}

	// Check measurements.
	if len(parsed.Pages[0].Measurements) != 1 || parsed.Pages[0].Measurements[0].ScaleX.Unit != "ft" {
		t.Errorf("measurements mismatch")
	}

	// Check formulas.
	if len(parsed.Formulas) != 1 || parsed.Formulas[0].MathML != "<math><mi>x</mi></math>" {
		t.Errorf("formulas mismatch")
	}
	if len(parsed.Formulas[0].MCIDs) != 2 {
		t.Errorf("formula MCIDs = %v, want [1 2]", parsed.Formulas[0].MCIDs)
	}

	// Check DSS.
	if parsed.DSS == nil || parsed.DSS.CertCount != 2 {
		t.Errorf("DSS mismatch")
	}
}
