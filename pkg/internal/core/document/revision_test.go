package document

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // ISO 32000-1, 7.6.3.3 defines the standard security handler key in terms of MD5.
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// revisionFixture builds a file whose first revision gives stream 4 the
// content first and whose incremental update (ISO 32000-1, 7.5.6) rewrites it
// to second. With a user password, both revisions are encrypted under one
// /V 1, /R 2 standard security handler (7.6.3) that the empty password does
// not open. It returns the file and the length of the first revision.
func revisionFixture(t *testing.T, userPassword, first, second string) ([]byte, int) {
	t.Helper()
	const docID = "0123456789abcdef"
	pad := func(pw string) []byte { return append([]byte(pw), passwordPad...)[:32] }

	var trailer string
	seal := func(_ int, data []byte) []byte { return data }
	var encDict string
	if userPassword != "" {
		ownerDigest := md5.Sum(pad(userPassword)) //nolint:gosec // See the import note.
		owner := rc4Cipher(ownerDigest[:5], pad(userPassword))
		h := md5.New() //nolint:gosec // See the import note.
		h.Write(pad(userPassword))
		h.Write(owner)
		h.Write([]byte{0xFC, 0xFF, 0xFF, 0xFF}) // /P -4, little endian.
		h.Write([]byte(docID))
		fileKey := h.Sum(nil)[:5]
		user := rc4Cipher(fileKey, passwordPad)
		encDict = fmt.Sprintf("<< /Filter /Standard /V 1 /R 2 /P -4 /O <%x> /U <%x> >>", owner, user)
		trailer = fmt.Sprintf("/Encrypt 5 0 R /ID [<%x> <%x>] ", docID, docID)
		seal = func(objNum int, data []byte) []byte {
			return rc4Cipher(rc4ObjectKey(fileKey, objNum, 0), data)
		}
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := map[int]int{}
	put := func(num int, body string) {
		offs[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	stream := func(text string) string {
		data := seal(4, []byte(text))
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(data), data)
	}
	put(1, "<< /Type /Catalog /Pages 2 0 R >>")
	put(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	put(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Contents 4 0 R >>")
	put(4, stream(first))
	nums := []int{1, 2, 3, 4}
	if encDict != "" {
		put(5, encDict)
		nums = append(nums, 5)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(nums)+1)
	for _, n := range nums {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offs[n])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R %s>>\nstartxref\n%d\n%%%%EOF\n", len(nums)+1, trailer, xref)
	signed := buf.Len()

	put(4, stream(second))
	update := buf.Len()
	fmt.Fprintf(&buf, "xref\n4 1\n%010d 00000 n \ntrailer\n<< /Size %d /Root 1 0 R /Prev %d %s>>\nstartxref\n%d\n%%%%EOF\n",
		offs[4], len(nums)+1, xref, trailer, update)
	return buf.Bytes(), signed
}

// contentOf decodes stream 4 of doc.
func contentOf(t *testing.T, doc *Document) string {
	t.Helper()
	ctx := context.Background()
	obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving object 4: %v", err)
	}
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		t.Fatalf("object 4 is %T, want a stream", obj)
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		t.Fatalf("decoding object 4: %v", err)
	}
	return string(data)
}

func TestRevision_ReadsTheEarlierRevision(t *testing.T) {
	t.Parallel()

	data, signed := revisionFixture(t, "", "before", "after")
	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	rev, err := doc.Revision(ctx, int64(signed))
	if err != nil {
		t.Fatalf("Revision: %v", err)
	}
	if got := contentOf(t, rev); got != "before" {
		t.Errorf("revision content = %q, want %q", got, "before")
	}
	if got := contentOf(t, doc); got != "after" {
		t.Errorf("current content = %q, want %q", got, "after")
	}
}

// TestRevision_RefusesWhatNoRevisionEnds pins that an offset inside a revision,
// where no cross-reference section ends the bytes, is refused rather than
// rebuilt from a raw scan.
func TestRevision_RefusesWhatNoRevisionEnds(t *testing.T) {
	t.Parallel()

	data, signed := revisionFixture(t, "", "before", "after")
	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	for _, end := range []int64{0, int64(signed) / 2, int64(len(data)) + 1} {
		if _, err := doc.Revision(ctx, end); !errors.Is(err, ErrNoRevision) {
			t.Errorf("Revision(%d) error = %v, want ErrNoRevision", end, err)
		}
	}
}

// TestRevision_SharesTheAuthenticatedKey covers a revision the empty password
// does not open: it reads with the key the current document was opened with,
// since both revisions carry the same security handler parameters.
func TestRevision_SharesTheAuthenticatedKey(t *testing.T) {
	t.Parallel()

	data, signed := revisionFixture(t, "secret", "before", "after")
	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Crypto == nil || doc.Authenticated {
		t.Fatalf("fixture: encrypted %v, opened without a password %v; want encrypted and closed", doc.Crypto != nil, doc.Authenticated)
	}
	if err := doc.Authenticate("secret"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	rev, err := doc.Revision(ctx, int64(signed))
	if err != nil {
		t.Fatalf("Revision: %v", err)
	}
	if !rev.Authenticated {
		t.Fatal("revision is not authenticated")
	}
	if got := contentOf(t, rev); got != "before" {
		t.Errorf("revision content = %q, want %q", got, "before")
	}
}
