package filter

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/plugins"
)

func TestDecodable(t *testing.T) {
	decodable := []ast.PdfName{
		"FlateDecode", "Fl", "LZWDecode", "LZW", "ASCIIHexDecode", "AHx",
		"ASCII85Decode", "A85", "CCITTFaxDecode", "CCF", "BrotliDecode",
		"RunLengthDecode", "RL", "DCTDecode", "DCT", "JBIG2Decode", "JPXDecode", "Crypt",
	}
	for _, n := range decodable {
		if !Decodable(n) {
			t.Errorf("Decodable(%q) = false, want true", n)
		}
	}

	// Unknown filters are not decodable.
	undecodable := []ast.PdfName{"FooDecode", ""}
	for _, n := range undecodable {
		if Decodable(n) {
			t.Errorf("Decodable(%q) = true, want false", n)
		}
	}
}

type fakePlugin struct{ name string }

func (p fakePlugin) FilterName() string                                 { return p.name }
func (fakePlugin) Decode(data []byte, _ map[string]any) ([]byte, error) { return data, nil }

func TestDecoderHandles(t *testing.T) {
	var nilDec *Decoder
	if nilDec.Handles("JBIG2Decode") {
		t.Error("nil Decoder should handle nothing")
	}

	d := NewDecoder([]plugins.FilterDecoder{fakePlugin{name: "JBIG2Decode"}}, limits.Limits{})
	if d == nil {
		t.Fatal("NewDecoder returned nil for a non-empty plugin set")
	}
	if !d.Handles("JBIG2Decode") {
		t.Error("Handles(JBIG2Decode) = false, want true")
	}
	if d.Handles("FlateDecode") {
		t.Error("Handles(FlateDecode) = true, want false (no plugin for it)")
	}
}
