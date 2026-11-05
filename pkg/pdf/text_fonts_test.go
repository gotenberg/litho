package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// cyrillicFormPDF builds a one-page form whose only field holds a Cyrillic
// value and no appearance stream, the shape set-form leaves.
func cyrillicFormPDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /NeedAppearances true /DA (/Helv 9 Tf 0 g) >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Annots [4 0 R] /Contents 5 0 R >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (СУВОРОВ) /Rect [10 10 190 40] /F 4 >>",
		"<< /Length 0 >>\nstream\n\nendstream",
	})
}

// cyrillicFontFS serves one TrueType program covering the Cyrillic letters of
// "СУВОРОВ" and no Latin.
func cyrillicFontFS() fstest.MapFS {
	var glyphs []fontbuild.Glyph
	seen := map[rune]bool{}
	for _, r := range "СУВОРОВ" {
		if !seen[r] {
			seen[r] = true
			glyphs = append(glyphs, fontbuild.Glyph{Name: fmt.Sprintf("uni%04X", r), Unicode: r, Advance: 600})
		}
	}
	program := fontbuild.TrueType{PostScriptName: "TestSans", Family: "Test Sans", Subfamily: "Regular", Glyphs: glyphs, NotdefAdvance: 500}.Build()
	return fstest.MapFS{"cyrillic/TestSans.ttf": &fstest.MapFile{Data: program}}
}

// TestWriter_FlattenForm_RefusesAValueNoFontCovers pins that a value the
// form's font cannot draw fails the write, naming the field, rather than
// leaving a widget behind.
func TestWriter_FlattenForm_RefusesAValueNoFontCovers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, opts := range map[string][]FlattenFormOption{
		"no fonts":            nil,
		"fonts that cover it": {WithFlattenFonts(NewFontFileSource(fstest.MapFS{"latin.ttf": &fstest.MapFile{Data: fontbuild.TrueType{PostScriptName: "Latin", Family: "Latin", Subfamily: "Regular", Glyphs: []fontbuild.Glyph{{Name: "A", Unicode: 'A', Advance: 600}}, NotdefAdvance: 500}.Build()}}))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := NewWriter(openRecompressSource(t, cyrillicFormPDF()))
			w.FlattenForm(opts...)
			var out bytes.Buffer
			err := w.WriteTo(ctx, &out)
			if !errors.Is(err, ErrFlattenIncomplete) || !errors.Is(err, ErrNoFontCovers) {
				t.Fatalf("WriteTo returned %v, want ErrFlattenIncomplete wrapping ErrNoFontCovers", err)
			}
			if !strings.Contains(err.Error(), `"name"`) {
				t.Errorf("the error does not name the field: %v", err)
			}
			if out.Len() != 0 {
				t.Errorf("WriteTo wrote %d bytes on a refusal", out.Len())
			}
		})
	}
}

// TestWriter_FlattenForm_DrawsAValueWithTheFontsOffered pins the end-to-end
// contract: with a source that covers the value, the flatten draws it, the
// form is gone, and the text extracts from the page.
func TestWriter_FlattenForm_DrawsAValueWithTheFontsOffered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := NewWriter(openRecompressSource(t, cyrillicFormPDF()))
	w.FlattenForm(WithFlattenFonts(NewFontFileSource(cyrillicFontFS())))
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if bytes.Contains(out.Bytes(), []byte("/AcroForm")) || bytes.Contains(out.Bytes(), []byte("/Widget")) {
		t.Error("the output still carries the form or its widget")
	}
	text, err := openRecompressSource(t, out.Bytes()).ExtractAllText(ctx)
	if err != nil {
		t.Fatalf("ExtractAllText: %v", err)
	}
	if !strings.Contains(text, "СУВОРОВ") {
		t.Errorf("the flattened page extracts %q, want the value", text)
	}
	if !bytes.Contains(out.Bytes(), []byte("/Identity-H")) || !bytes.Contains(out.Bytes(), []byte("+TestSans")) {
		t.Error("the value is not drawn with an embedded subset of the offered font")
	}
}

// TestWriter_StampText_DrawsOtherScriptsWithTheFontsOffered pins the same
// contract on a stamp: a standard font refuses the text on its own, and draws
// it with a program [TextStamp.Fonts] offers.
func TestWriter_StampText_DrawsOtherScriptsWithTheFontsOffered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	w := NewWriter(openRecompressSource(t, data))
	if err := w.StampText(TextStamp{Text: "СУВОРОВ", X: 20, Y: 20}); !errors.Is(err, ErrTextNotEncodable) {
		t.Fatalf("StampText without fonts returned %v, want ErrTextNotEncodable", err)
	}

	w = NewWriter(openRecompressSource(t, data))
	stamp := TextStamp{Text: "СУВОРОВ", X: 20, Y: 20, Font: FontTimesBold, Fonts: NewFontFileSource(cyrillicFontFS())}
	if err := w.StampText(stamp); err != nil {
		t.Fatalf("StampText: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	// A stamp is an artifact, which the default extraction leaves out.
	text, err := openRecompressSource(t, out.Bytes()).ExtractAllText(ctx, WithArtifacts())
	if err != nil {
		t.Fatalf("ExtractAllText: %v", err)
	}
	if strings.Count(text, "СУВОРОВ") != 3 {
		t.Errorf("the stamped pages extract %q, want the text on each of the 3 pages", text)
	}

	w = NewWriter(openRecompressSource(t, data))
	stamp.Text = "СУВОРОВ and Latin"
	if err := w.StampText(stamp); err != nil {
		t.Fatalf("StampText: %v", err)
	}
	err = w.WriteTo(ctx, &bytes.Buffer{})
	if !errors.Is(err, ErrNoFontCovers) || !strings.Contains(err.Error(), "'a'") {
		t.Errorf("WriteTo returned %v, want ErrNoFontCovers naming 'a'", err)
	}
}

// TestNewFontProviderSource pins that a provider's one program serves, asked
// with the standard font's name and the text's characters.
func TestNewFontProviderSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fsys := cyrillicFontFS()
	var asked FontRequest
	provider := FontProviderFunc(func(_ context.Context, req FontRequest) ([]byte, error) {
		asked = req
		return fsys["cyrillic/TestSans.ttf"].Data, nil
	})
	w := NewWriter(openRecompressSource(t, cyrillicFormPDF()))
	w.FlattenForm(WithFlattenFonts(NewFontProviderSource(provider)))
	if err := w.WriteTo(ctx, &bytes.Buffer{}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if asked.BaseFont != "Helvetica" || asked.Subtype != "TrueType" || len(asked.Codes) != len([]rune("СУВОРОВ")) {
		t.Errorf("the provider was asked %+v, want Helvetica, TrueType and the text's characters", asked)
	}
	if NewFontProviderSource(nil).source != nil {
		t.Error("a nil provider is not the zero source")
	}
}
