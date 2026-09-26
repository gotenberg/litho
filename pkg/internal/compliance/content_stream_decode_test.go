package compliance

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// decodeProbe is content that fails three rules wherever a reader takes its
// operators: it shows text in an unembedded font, fills in DeviceRGB with no
// OutputIntent, and runs an undefined operator.
const decodeProbe = "BT /F1 12 Tf 10 10 Td (A) Tj ET 1 0 0 rg 0 0 5 5 re f zzfoo\n"

// decodeProbeRules lists, by profile, the clauses the probe fails and how
// often veraPDF 1.30 fails them for one reading of it: the embedding rule, the
// device colour rules and the undefined-operator rule.
//
//nolint:gochecknoglobals // test table.
var decodeProbeRules = map[models.PDFAProfile]map[string]int{
	models.PDFA1b: {"6.3.4": 1, "6.2.3.3": 3, "6.2.10": 1},
	models.PDFA2b: {"6.2.11.4.1": 1, "6.2.4.3": 3, "6.2.2": 1},
}

// contentPart is one content stream of a page: the entries its dictionary adds
// to /Length, and its data.
type contentPart struct {
	dict string
	data []byte
}

// decodeProbePDF builds a one-page PDF whose content is parts, a stream or,
// for more than one, an array of them, over resources that name the probe's
// font.
func decodeProbePDF(parts ...contentPart) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"", // the page, once its parts are numbered
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	refs := make([]string, 0, len(parts))
	for _, part := range parts {
		objs = append(objs, fmt.Sprintf("<< /Length %d %s >>\nstream\n%s\nendstream", len(part.data), part.dict, part.data))
		refs = append(refs, fmt.Sprintf("%d 0 R", len(objs)))
	}
	contents := refs[0]
	if len(refs) > 1 {
		contents = "[" + strings.Join(refs, " ") + "]"
	}
	objs[2] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] " +
		"/Resources << /Font << /F1 4 0 R >> >> /Contents " + contents + " >>"
	return cyclePDF(objs...)
}

// decodeProbeCounts validates data under profile and returns how often it
// fails the clauses of [decodeProbeRules].
func decodeProbeCounts(t *testing.T, data []byte, profile models.PDFAProfile) map[string]int {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(ctx, doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	counts := make(map[string]int)
	for _, e := range viols {
		if v, ok := e.(Violation); ok {
			if _, probed := decodeProbeRules[profile][v.Rule.Clause]; probed {
				counts[v.Rule.Clause]++
			}
		}
	}
	return counts
}

// TestContentStreamDecode_ReadsAsVeraPDF pins which content streams compliance
// reads operators from ([decodeContentStream]): each case is a page whose
// content holds [decodeProbe], and reads says whether veraPDF 1.30 fails the
// probe's rules on it, measured at PDF/A-1b and PDF/A-2b. veraPDF reads no
// operator from a stream whose last filter it does not decode, or whose
// decoding fails before the first byte, and reads the bytes decoded before
// damage that comes later. litho judged every font of a page whose content did
// not decode, and read the operators of streams in the abbreviated filters
// that veraPDF does not decode.
func TestContentStreamDecode_ReadsAsVeraPDF(t *testing.T) {
	t.Parallel()
	probe := []byte(decodeProbe)
	// Operators that do not repeat, so the compressed data past the probe
	// is long enough for a cut halfway to leave the probe whole.
	padded := []byte(decodeProbe)
	for i := range 400 {
		padded = fmt.Appendf(padded, "%d %d m\n", i, i)
	}
	flate := flateEncoded(t, probe)
	cut := flateEncoded(t, padded)
	cut = cut[:len(cut)/2]
	badCheck := bytes.Clone(flate)
	badCheck[len(badCheck)-1] ^= 0xFF
	hexProbe := []byte(hex.EncodeToString(probe) + ">")

	cases := []struct {
		name  string
		parts []contentPart
		reads bool
	}{
		{"no filter", []contentPart{{"", probe}}, true},
		{"Flate data that is no zlib stream", []contentPart{{"/Filter /FlateDecode", probe}}, false},
		{"Flate data whose first block is invalid", []contentPart{{"/Filter /FlateDecode", []byte("\x78\x9c\x07" + strings.Repeat("\x00", 20))}}, false},
		{"Flate data cut short", []contentPart{{"/Filter /FlateDecode", cut}}, true},
		{"Flate data with a wrong checksum", []contentPart{{"/Filter /FlateDecode", badCheck}}, true},
		{"abbreviated AHx", []contentPart{{"/Filter /AHx", hexProbe}}, false},
		{"abbreviated A85", []contentPart{{"/Filter /A85", ascii85Encoded(t, probe)}}, false},
		{"abbreviated Fl", []contentPart{{"/Filter /Fl", flate}}, false},
		{"ASCIIHexDecode damaged at its start", []contentPart{{"/Filter /ASCIIHexDecode", append([]byte("zz"), hexProbe...)}}, false},
		{"ASCIIHexDecode damaged after the probe", []contentPart{{"/Filter /ASCIIHexDecode", []byte(hex.EncodeToString(probe) + "zz20>")}}, true},
		{"ASCII85Decode damaged at its start", []contentPart{{"/Filter /ASCII85Decode", append([]byte("\x7f\x7f"), ascii85Encoded(t, probe)...)}}, false},
		{"unknown filter", []contentPart{{"/Filter /Foo", probe}}, false},
		{"image codec", []contentPart{{"/Filter /DCTDecode", probe}}, false},
		{"unknown filter before Flate", []contentPart{{"/Filter [/Foo /FlateDecode]", flate}}, true},
		{"unknown filter after Flate", []contentPart{{"/Filter [/FlateDecode /Foo]", flate}}, false},
		// A predictor given to /Foo would scramble the probe if Flate took it.
		{"unknown filter before Flate, with parameters", []contentPart{{"/Filter [/Foo /FlateDecode] /DecodeParms [<< /Predictor 12 /Columns 4 >> null]", flate}}, true},
		{"one parameter dictionary for two filters", []contentPart{{"/Filter [/Foo /FlateDecode] /DecodeParms << /Predictor 12 /Columns 4 >>", flate}}, false},
		{"two parameter dictionaries for one filter", []contentPart{{"/Filter /FlateDecode /DecodeParms [<< >> << >>]", flate}}, false},
		{"parameters that are no dictionary", []contentPart{{"/Filter /FlateDecode /DecodeParms [5]", flate}}, false},
		{"parameters that are null", []contentPart{{"/Filter /FlateDecode /DecodeParms [null]", flate}}, true},
		{"parameters that are no array", []contentPart{{"/Filter /FlateDecode /DecodeParms 5", flate}}, true},
		{"undecodable part before a readable one", []contentPart{{"/Filter /FlateDecode", probe}, {"", probe}}, true},
		{"unknown filter part before a readable one", []contentPart{{"/Filter /Foo", probe}, {"", probe}}, true},
		{"readable part before an undecodable one", []contentPart{{"", probe}, {"/Filter /FlateDecode", probe}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := decodeProbePDF(tc.parts...)
			for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b} {
				got := decodeProbeCounts(t, data, profile)
				for clause, n := range decodeProbeRules[profile] {
					want := 0
					if tc.reads {
						want = n
					}
					if got[clause] != want {
						t.Errorf("%s: clause %s fails %d times, want %d", profile, clause, got[clause], want)
					}
				}
			}
		})
	}
}
