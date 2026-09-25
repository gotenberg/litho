package document

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// TestOpenedXRefEntries pins what [Document.OpenedXRefEntries] yields once
// the raw-byte augmentation has run: the entries the table lists, in
// object-number order, with object 2, which the table misplaces, at the
// offset the augmentation moved it to, and without object 4, which the table
// leaves out and the augmentation added. Before the augmentation it yields
// what [Document.XRefEntries] yields.
func TestOpenedXRefEntries(t *testing.T) {
	t.Parallel()

	objects := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 3 0 R >>\nendobj\n",
		"2 0 obj\n<< /A 1 >>\nendobj\n",
		"3 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n",
		"4 0 obj\n<< /Unlisted true >>\nendobj\n",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int64, len(objects)+1)
	for i, obj := range objects {
		offsets[i+1] = int64(buf.Len())
		buf.WriteString(obj)
	}
	stale := offsets[1] + 2 // inside object 1, where no header starts
	xrefAt := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 4\n0000000000 65535 f \r\n%010d 00000 n \r\n%010d 00000 n \r\n%010d 00000 n \r\n", offsets[1], stale, offsets[3])
	fmt.Fprintf(&buf, "trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefAt)

	data := buf.Bytes()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	collect := func(seq func(func(int, xref.XRefEntry) bool)) ([]int, map[int]xref.XRefEntry) {
		var nums []int
		entries := make(map[int]xref.XRefEntry)
		for n, e := range seq {
			nums = append(nums, n)
			entries[n] = e
		}
		return nums, entries
	}

	openedNums, openedEntries := collect(doc.OpenedXRefEntries())
	liveNums, liveEntries := collect(doc.XRefEntries())
	if !slices.Equal(openedNums, liveNums) || !maps.Equal(openedEntries, liveEntries) {
		t.Fatalf("before the augmentation: opened %v, live %v, want the same entries", openedEntries, liveEntries)
	}

	doc.AugmentXRef(context.Background())
	if _, ok := doc.XRefEntry(4); !ok {
		t.Fatal("the augmentation did not add object 4")
	}

	openedNums, openedEntries = collect(doc.OpenedXRefEntries())
	if want := []int{0, 1, 2, 3}; !slices.Equal(openedNums, want) {
		t.Errorf("OpenedXRefEntries yields objects %v, want %v", openedNums, want)
	}
	if got := openedEntries[2].Offset; got != offsets[2] {
		t.Errorf("object 2 is at %d, want %d, where the augmentation moved it", got, offsets[2])
	}
	if got, want := doc.OpenedXRefEntryCount(), len(openedNums); got != want {
		t.Errorf("OpenedXRefEntryCount = %d, want %d", got, want)
	}
	if got, want := doc.XRefEntryCount(), len(openedNums)+1; got != want {
		t.Errorf("XRefEntryCount = %d, want %d", got, want)
	}
}
