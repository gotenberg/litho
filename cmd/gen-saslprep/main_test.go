package main

import (
	"errors"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strings"
	"testing"
)

// sampleRFC mirrors the RFC 3454 layout: indented table lines, rows with
// trailing columns, and a page break inside a table.
const sampleRFC = `B.1 Commonly mapped to nothing

   ----- Start Table B.1 -----
   00AD; ; Map to nothing
   034F; ; Map to nothing



Hoffman & Blanchet          Standards Track                    [Page 31]
` + "\f" + `
RFC 3454        Preparation of Internationalized Strings   December 2002


   2060; ; Map to nothing
   ----- End Table B.1 -----

   ----- Start Table C.2.1 -----
   0000-001F; [CONTROL CHARACTERS]
   007F; DELETE
   ----- End Table C.2.1 -----

   ----- Start Table C.3 -----
   E000-F8FF; [PRIVATE USE, PLANE 0]
   100000-10FFFD; [PRIVATE USE, PLANE 16]
   ----- End Table C.3 -----
`

func TestParseTables(t *testing.T) {
	t.Parallel()

	tables, err := parseTables([]byte(sampleRFC))
	if err != nil {
		t.Fatalf("parseTables: %v", err)
	}
	if got := slices.Sorted(maps.Keys(tables)); !slices.Equal(got, []string{"B.1", "C.2.1", "C.3"}) {
		t.Errorf("tables = %v", got)
	}
	if want := []codeRange{{0x00AD, 0x00AD}, {0x034F, 0x034F}, {0x2060, 0x2060}}; !slices.Equal(tables["B.1"], want) {
		t.Errorf("B.1 = %v, want %v", tables["B.1"], want)
	}
	if want := []codeRange{{0xE000, 0xF8FF}, {0x100000, 0x10FFFD}}; !slices.Equal(tables["C.3"], want) {
		t.Errorf("C.3 = %v, want %v", tables["C.3"], want)
	}
}

func TestParseTablesRefusesBadLine(t *testing.T) {
	t.Parallel()

	bad := strings.Replace(sampleRFC, "034F; ; Map to nothing", "034G; ; Map to nothing", 1)
	if _, err := parseTables([]byte(bad)); !errors.Is(err, errBadLine) {
		t.Errorf("parseTables on a misread line = %v, want errBadLine", err)
	}
}

func TestUnion(t *testing.T) {
	t.Parallel()

	tables := map[string][]codeRange{
		"X": {{0x10, 0x1F}, {0x40, 0x40}},
		"Y": {{0x20, 0x22}, {0x41, 0x50}},
	}
	got, err := union(tables, []string{"X", "Y"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []codeRange{{0x10, 0x22}, {0x40, 0x50}}; !slices.Equal(got, want) {
		t.Errorf("union = %v, want %v", got, want)
	}
	if _, err := union(tables, []string{"Z"}); !errors.Is(err, errMissingTable) {
		t.Errorf("union of a missing table = %v, want errMissingTable", err)
	}
	tables["U"] = []codeRange{{0x30, 0x30}, {0x20, 0x20}}
	if _, err := union(tables, []string{"U"}); !errors.Is(err, errUnsorted) {
		t.Errorf("union of an unsorted table = %v, want errUnsorted", err)
	}
}

func TestVerify(t *testing.T) {
	t.Parallel()

	if err := verify([]byte("x"), rfcSHA256); !errors.Is(err, errDigestMismatch) {
		t.Errorf("verify on other bytes = %v, want errDigestMismatch", err)
	}
}

func TestGenerateNeedsEveryTable(t *testing.T) {
	t.Parallel()

	tables, err := parseTables([]byte(sampleRFC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generate(tables); !errors.Is(err, errMissingTable) {
		t.Errorf("generate without A.1 = %v, want errMissingTable", err)
	}
	for _, s := range sets {
		for _, name := range s.Tables {
			if _, ok := tables[name]; !ok {
				tables[name] = []codeRange{{0x41, 0x41}}
			}
		}
	}
	code, err := generate(tables)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "tables.go", code, 0); err != nil {
		t.Fatalf("generated code does not parse: %v", err)
	}
	if !strings.Contains(string(code), "The Internet Society") {
		t.Error("generated code misses the RFC copyright notice")
	}
}
