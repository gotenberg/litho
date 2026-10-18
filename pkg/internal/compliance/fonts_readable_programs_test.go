package compliance

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// Type 1 encryption constants, Adobe Type 1 Font Format, 7.1.
const (
	testEexecKey      = 55665
	testCharStringKey = 4330
)

// testType1Encrypt encrypts plain under key with the Type 1 cipher, after the
// four zero bytes every encrypted portion starts with.
func testType1Encrypt(plain []byte, key uint16) []byte {
	r := key
	out := make([]byte, 0, len(plain)+4)
	for _, p := range append([]byte{0, 0, 0, 0}, plain...) {
		c := p ^ byte(r>>8)
		out = append(out, c)
		r = (uint16(c)+r)*52845 + 22719
	}
	return out
}

// readableType1 completes the clear-text portion of a Type 1 font program
// with an eexec-encrypted Private dictionary whose CharStrings define .notdef,
// A and B, so the program reads ([fontProgramReadable]). clear is the
// clear-text portion; a trailing "currentfile eexec" line is added when it has
// none.
func readableType1(clear string) string {
	if !strings.HasSuffix(clear, "currentfile eexec\n") {
		clear += "currentfile eexec\n"
	}
	// 0 600 hsbw endchar, a glyph that only advances.
	glyph := testType1Encrypt([]byte{139, 248, 236, 13, 14}, testCharStringKey)
	private := "dup /Private 8 dict dup begin\n/RD{string currentfile exch readstring pop}executeonly def\n" +
		"/ND{noaccess def}executeonly def\n/lenIV 4 def\n2 index /CharStrings 3 dict dup begin\n"
	for _, name := range []string{".notdef", "A", "B"} {
		private += "/" + name + " 9 RD " + string(glyph) + " ND\n"
	}
	private += "end\nend\nreadonly put\nnoaccess put\ndup /FontName get exch definefont pop\nmark currentfile closefile\n"
	return clear + string(testType1Encrypt([]byte(private), testEexecKey)) + strings.Repeat("0", 512) + "\ncleartomark\n"
}

// readableTrueType returns a TrueType program holding .notdef, A and B, with
// the 'hhea' and 'hmtx' tables a reader needs ([fontProgramReadable]).
func readableTrueType() string {
	return string(fontbuild.TrueType{
		PostScriptName: "Test", Family: "Test", Subfamily: "Regular",
		Glyphs: []fontbuild.Glyph{{Name: "A", Unicode: 'A', Advance: 722}, {Name: "B", Unicode: 'B', Advance: 667}},
	}.Build())
}

// readableCFF returns a bare CFF program holding .notdef, A and B.
func readableCFF() string {
	return string(fontbuild.CFF{
		FontName: "Test",
		Glyphs:   []fontbuild.Glyph{{Name: "A", Advance: 722}, {Name: "B", Advance: 667}},
	}.Build())
}

// TestReadableTestPrograms pins that the programs the font tests embed read.
func TestReadableTestPrograms(t *testing.T) {
	t.Parallel()
	if !fontProgramReadable("FontFile", []byte(readableType1("%!PS-AdobeFont-1.0: Test\n"))) {
		t.Error("readableType1 does not read")
	}
	if !fontProgramReadable("FontFile2", []byte(readableTrueType())) {
		t.Error("readableTrueType does not read")
	}
	if !fontProgramReadable("FontFile3", []byte(readableCFF())) {
		t.Error("readableCFF does not read")
	}
}
