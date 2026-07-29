package document

import (
	"bytes"
	"context"
	"math"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// TestNextNormalObjectOffset pins the binary search that caps endobj scanning,
// including its rebuild when the entry set grows (lazy xref augmentation).
// Compressed entries (no byte offset) are ignored.
func TestNextNormalObjectOffset(t *testing.T) {
	d := &Document{Index: xref.Index{Entries: map[int]xref.XRefEntry{
		1: {Type: xref.XRefTypeNormal, Offset: 100},
		2: {Type: xref.XRefTypeNormal, Offset: 250},
		3: {Type: xref.XRefTypeNormal, Offset: 400},
		4: {Type: xref.XRefTypeCompressed, Offset: 0},
	}}}

	cases := []struct{ off, want int64 }{
		{50, 100}, {100, 250}, {150, 250}, {249, 250}, {250, 400}, {400, math.MaxInt64}, {999, math.MaxInt64},
	}
	for _, c := range cases {
		if got := d.nextNormalObjectOffset(c.off); got != c.want {
			t.Errorf("nextNormalObjectOffset(%d) = %d, want %d", c.off, got, c.want)
		}
	}

	// Growing the entry set must invalidate the cached slice and rebuild.
	d.Index.Entries[5] = xref.XRefEntry{Type: xref.XRefTypeNormal, Offset: 175}
	if got := d.nextNormalObjectOffset(100); got != 175 {
		t.Errorf("after adding offset 175: nextNormalObjectOffset(100) = %d, want 175", got)
	}
}

// cyclicPrevPDF returns a PDF whose xref trailer /Prev points back at the xref
// table's own offset (109), forming a self-referential chain. The trailer sits
// after the xref, so adding /Prev leaves the offset unchanged. Reproduces pdfjs
// bug1393476, which hung the reader before the /Prev cycle guard.
func cyclicPrevPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
			"xref\n0 3\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n" +
			"trailer\n<< /Size 3 /Root 1 0 R /Prev 109 >>\nstartxref\n109\n%%EOF\n")
}

// TestNewDocument_CyclicPrevTerminates pins the /Prev cycle guard in
// parseXRefChain: a self-referential (or mutually cyclic) /Prev must not loop
// forever. The document still opens, keeping the entries parsed before the
// cycle is detected. Without the guard this hangs; the timeout catches a
// regression instead of hanging the whole suite.
func TestNewDocument_CyclicPrevTerminates(t *testing.T) {
	data := cyclicPrevPDF()

	done := make(chan error, 1)
	go func() {
		_, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("NewDocument on cyclic /Prev returned error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("NewDocument hung on a cyclic /Prev chain: cycle guard missing")
	}
}

// TestValidateXRefTableFormat_SurplusEOLDoesNotMaskSpacing pins that a defect
// between the xref keyword and the subsection header does not hide a defect in
// the header itself. ISO 19005-1, 6.1.4 states the two as separate tests, t2
// and t1, so a file carrying both must report both. The scanner used to stop on
// the surplus EOL, read no digits from the header and give up, so the spacing
// defect went unreported and only the pair revealed it.
func TestValidateXRefTableFormat_SurplusEOLDoesNotMaskSpacing(t *testing.T) {
	table := func(header string) *Document {
		body := "xref" + header + "0000000000 65535 f \r\ntrailer\n<< /Size 1 >>\n"
		return &Document{r: bytes.NewReader([]byte(body))}
	}

	count := func(violations []error, needle string) int {
		n := 0
		for _, v := range violations {
			if bytes.Contains([]byte(v.Error()), []byte(needle)) {
				n++
			}
		}
		return n
	}

	t.Run("surplus EOL alone", func(t *testing.T) {
		got := table("\n\n0 1\n").ValidateXRefTableFormat(0)
		if count(got, "multiple EOL markers") != 1 {
			t.Errorf("got %v, want the EOL defect", got)
		}
		if count(got, "spaces between start and count") != 0 {
			t.Errorf("got %v, want no spacing defect", got)
		}
	})

	t.Run("wide separator alone", func(t *testing.T) {
		got := table("\n0  1\n").ValidateXRefTableFormat(0)
		if count(got, "spaces between start and count") != 1 {
			t.Errorf("got %v, want the spacing defect", got)
		}
	})

	t.Run("both are reported", func(t *testing.T) {
		got := table("\n\n0  1\n").ValidateXRefTableFormat(0)
		if count(got, "multiple EOL markers") != 1 || count(got, "spaces between start and count") != 1 {
			t.Errorf("got %v, want both the EOL and the spacing defect", got)
		}
	})

	t.Run("a conforming header reports nothing", func(t *testing.T) {
		if got := table("\n0 1\n").ValidateXRefTableFormat(0); len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})
}
