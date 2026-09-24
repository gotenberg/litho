package writer

import (
	"bytes"
	"context"
	"regexp"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// writeUpdateWithGenerations appends an incremental update that rewrites object
// 4 and adds object 9, reporting generations through the config the way the
// public writer wires the source document in.
func writeUpdateWithGenerations(t *testing.T, gen func(objNum int) int) []byte {
	t.Helper()

	src := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n" +
		"trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n9\n%%EOF\n")

	alloc := NewObjAllocator(8)
	alloc.AllocAt(4, ast.PdfDict{"Title": ast.PdfString("new")})
	alloc.Alloc(ast.PdfDict{"Fresh": ast.PdfBool(true)})

	var out bytes.Buffer
	err := WriteIncremental(context.Background(), &out, IncrementalConfig{
		Source:          bytes.NewReader(src),
		SourceSize:      int64(len(src)),
		StartXRefOffset: 9,
		Allocator:       alloc,
		Trailer:         ast.PdfDict{"Root": ast.PdfIndirectRef{ObjectNum: 1}},
		Generation:      gen,
	})
	if err != nil {
		t.Fatalf("WriteIncremental failed: %v", err)
	}
	return out.Bytes()[len(src):]
}

// TestWriteIncrementalKeepsSourceGeneration checks that replacing an object
// keeps the generation its references name. An indirect reference identifies an
// object by number AND generation (ISO 32000-1, 7.3.10), so writing "4 0 obj"
// over a source "4 3 obj" leaves every "4 3 R" pointing at nothing.
//
// # Corpus evidence
//
//   - pdfjs bug1393476.pdf: came out of a rotate that litho reported and no
//     other reader could see.
func TestWriteIncrementalKeepsSourceGeneration(t *testing.T) {
	appended := writeUpdateWithGenerations(t, func(objNum int) int {
		if objNum == 4 {
			return 3
		}
		return 0
	})

	if !bytes.Contains(appended, []byte("4 3 obj")) {
		t.Errorf("appended update lost the source generation, want \"4 3 obj\":\n%s", appended)
	}
	if bytes.Contains(appended, []byte("4 0 obj")) {
		t.Errorf("appended update wrote object 4 at generation 0:\n%s", appended)
	}

	// The cross-reference entry has to agree with the object header, or a
	// reader that trusts the table still misses the replacement.
	entry := regexp.MustCompile(`(?m)^\d{10} (\d{5}) n`)
	matches := entry.FindAllSubmatch(appended, -1)
	if len(matches) == 0 {
		t.Fatalf("no in-use xref entries in the appended update:\n%s", appended)
	}
	var gens []string
	for _, m := range matches {
		gens = append(gens, string(m[1]))
	}
	if gens[0] != "00003" {
		t.Errorf("xref entry for object 4 has generation %q, want \"00003\" (entries: %v)", gens[0], gens)
	}
}

// TestWriteIncrementalFreshObjectsAtGenerationZero checks a newly allocated
// object still gets generation 0: it has no prior identity to preserve.
func TestWriteIncrementalFreshObjectsAtGenerationZero(t *testing.T) {
	appended := writeUpdateWithGenerations(t, func(objNum int) int {
		if objNum == 4 {
			return 3
		}
		return 0
	})

	if !bytes.Contains(appended, []byte("9 0 obj")) {
		t.Errorf("newly allocated object 9 not written at generation 0:\n%s", appended)
	}
}

// TestWriteIncrementalNilGenerationWritesZero checks the field stays optional,
// so a caller with no source document behaves as before.
func TestWriteIncrementalNilGenerationWritesZero(t *testing.T) {
	appended := writeUpdateWithGenerations(t, nil)

	if !bytes.Contains(appended, []byte("4 0 obj")) {
		t.Errorf("nil Generation should write generation 0:\n%s", appended)
	}
}
