package pdf

import (
	"bytes"
	"context"
	"testing"
)

// The tests here cover [WithEncryption] on the standalone page operations.
//
// Measured before the option existed: an encrypted document put through
// Split or Merge came out readable with no password at all. The reader
// decrypts every object as it resolves it, the copier carries that plaintext
// into the output, and neither operation ever reached the encryption block
// that [Writer.WriteTo] runs, so the output had no /Encrypt dictionary. That
// is a silent confidentiality downgrade: the caller asked to split a
// protected file and got an unprotected one, with nothing in the API or the
// output to say so.
//
// Each arm below pairs the encrypted case with an unencrypted control. The
// control is what distinguishes "the option works" from "the operation now
// encrypts unconditionally", which would break every existing caller.

// splitOnePage splits page 2 out of a three-page document.
func splitOnePage(t *testing.T, ctx context.Context, src []byte, opts ...WriteOption) []byte {
	t.Helper()

	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}

	var out bytes.Buffer
	if err := Split(ctx, r, []int{2}, &out, opts...); err != nil {
		t.Fatalf("Split: %v", err)
	}
	return out.Bytes()
}

// assertPasswordProtected pins that out cannot be opened without a password
// and that the given one works.
//
// Both halves are needed. Without the first, a file that was never encrypted
// passes; without the second, so does one encrypted under a key nobody holds,
// which is data loss rather than protection.
func assertPasswordProtected(t *testing.T, ctx context.Context, out []byte, password string) *Reader {
	t.Helper()

	if !bytes.Contains(out, []byte("/Encrypt")) {
		t.Fatal("output trailer declares no /Encrypt: the output is in the clear")
	}
	if _, err := Open(ctx, bytes.NewReader(out), int64(len(out))); err == nil {
		t.Fatal("output opened with no password: it is not protected")
	}

	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)), WithPassword(password))
	if err != nil {
		t.Fatalf("output will not open with the password it was encrypted under: %v", err)
	}
	return r
}

func TestSplit_WithEncryption(t *testing.T) {
	ctx := context.Background()
	out := splitOnePage(t, ctx, threePagePDF(), WithEncryption(EncryptConfig{
		UserPassword:  "user123",
		OwnerPassword: "owner456",
		Algorithm:     EncryptAES256,
		Permissions:   -1,
	}))

	r := assertPasswordProtected(t, ctx, out, "user123")

	// The content must survive the encryption, not merely be unreadable. A
	// split that encrypted the objects but corrupted them would satisfy every
	// assertion above.
	n, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 1 {
		t.Errorf("page count: got %d, want 1", n)
	}
	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if text != "Page2" {
		t.Errorf("text: got %q, want %q", text, "Page2")
	}
}

// TestSplit_WithEncryptionAndXRefStream composes encryption with the xref
// stream format, which the other arms never exercise.
//
// ISO 32000-1, 7.5.8.2 states that a cross-reference stream is not encrypted,
// and its /ID strings in particular must stay readable: a reader has to parse
// the xref and the /Encrypt dictionary before it holds a key at all. The two
// features therefore have to be ordered against each other, and getting it
// wrong yields a file that opens nowhere rather than a wrong number, which no
// score-based gate would report.
func TestSplit_WithEncryptionAndXRefStream(t *testing.T) {
	ctx := context.Background()
	out := splitOnePage(t, ctx, threePagePDF(), WithXRefStream(), WithEncryption(EncryptConfig{
		UserPassword:  "user123",
		OwnerPassword: "owner456",
		Algorithm:     EncryptAES256,
		Permissions:   -1,
	}))

	r := assertPasswordProtected(t, ctx, out, "user123")

	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if text != "Page2" {
		t.Errorf("text: got %q, want %q", text, "Page2")
	}
}

func TestMerge_WithEncryptionAndXRefStream(t *testing.T) {
	ctx := context.Background()
	data := threePagePDF()

	r1, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r1: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r2: %v", err)
	}

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{r1, r2}, &out, WithXRefStream(), WithEncryption(EncryptConfig{
		UserPassword:  "user123",
		OwnerPassword: "owner456",
		Algorithm:     EncryptAES256,
		Permissions:   -1,
	})); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	r3 := assertPasswordProtected(t, ctx, out.Bytes(), "user123")

	n, err := r3.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 6 {
		t.Errorf("page count: got %d, want 6", n)
	}
}

func TestSplit_WithoutEncryptionStaysReadable(t *testing.T) {
	ctx := context.Background()
	out := splitOnePage(t, ctx, threePagePDF())

	if bytes.Contains(out, []byte("/Encrypt")) {
		t.Error("a split with no encryption option produced an encrypted output")
	}
	if _, err := Open(ctx, bytes.NewReader(out), int64(len(out))); err != nil {
		t.Fatalf("Open split output: %v", err)
	}
}

func TestMerge_WithEncryption(t *testing.T) {
	ctx := context.Background()
	data := threePagePDF()

	r1, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r1: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r2: %v", err)
	}

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{r1, r2}, &out, WithEncryption(EncryptConfig{
		UserPassword:  "user123",
		OwnerPassword: "owner456",
		Algorithm:     EncryptAES256,
		Permissions:   -1,
	})); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	r3 := assertPasswordProtected(t, ctx, out.Bytes(), "user123")

	n, err := r3.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 6 {
		t.Errorf("page count: got %d, want 6", n)
	}
	text, err := r3.ExtractText(ctx, 4)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if text != "Page1" {
		t.Errorf("text on page 4: got %q, want %q", text, "Page1")
	}
}

// TestMerge_WithEncryptionAcrossDifferentPasswords pins what [WithEncryption]'s
// godoc claims and no other test exercises: encryption describes the OUTPUT, so
// sources opened under DIFFERENT passwords merge into one file under the single
// key the option supplies.
//
// Every other merge test here uses the same fixture twice, which cannot tell
// that apart from "all sources must share a key". The two source passwords are
// also asserted NOT to open the result, because a merged file that still
// answered to an input's password would mean a source key leaked into the
// output.
func TestMerge_WithEncryptionAcrossDifferentPasswords(t *testing.T) {
	ctx := context.Background()

	encryptWith := func(password string) []byte {
		t.Helper()
		r, err := Open(ctx, bytes.NewReader(threePagePDF()), int64(len(threePagePDF())))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		w := NewWriter(r)
		w.Encrypt(EncryptConfig{
			UserPassword:  password,
			OwnerPassword: password + "-owner",
			Algorithm:     EncryptAES256,
			Permissions:   -1,
		})
		var buf bytes.Buffer
		if err := w.WriteTo(ctx, &buf); err != nil {
			t.Fatalf("WriteTo (encrypt %s): %v", password, err)
		}
		return buf.Bytes()
	}

	alphaBytes, betaBytes := encryptWith("alpha"), encryptWith("beta")

	alpha, err := Open(ctx, bytes.NewReader(alphaBytes), int64(len(alphaBytes)), WithPassword("alpha"))
	if err != nil {
		t.Fatalf("Open alpha: %v", err)
	}
	beta, err := Open(ctx, bytes.NewReader(betaBytes), int64(len(betaBytes)), WithPassword("beta"))
	if err != nil {
		t.Fatalf("Open beta: %v", err)
	}

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{alpha, beta}, &out, WithEncryption(EncryptConfig{
		UserPassword:  "merged",
		OwnerPassword: "merged-owner",
		Algorithm:     EncryptAES256,
		Permissions:   -1,
	})); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	r := assertPasswordProtected(t, ctx, out.Bytes(), "merged")

	for _, stale := range []string{"alpha", "beta"} {
		if _, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword(stale)); err == nil {
			t.Errorf("a source password (%q) opens the merged output", stale)
		}
	}

	n, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 6 {
		t.Errorf("page count: got %d, want 6", n)
	}
}

func TestMerge_WithoutEncryptionStaysReadable(t *testing.T) {
	ctx := context.Background()
	data := threePagePDF()

	r1, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r1: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r2: %v", err)
	}

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{r1, r2}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if bytes.Contains(out.Bytes(), []byte("/Encrypt")) {
		t.Error("a merge with no encryption option produced an encrypted output")
	}
	if _, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len())); err != nil {
		t.Fatalf("Open merged output: %v", err)
	}
}

// TestWriteTo_WithEncryptionCoversThePageOperations pins that the option
// reaches the [Writer] path, which is where delete, insert and rotate live:
// each queues a mutation and writes through [Writer.WriteTo] rather than
// through [Split] or [Merge]. Without this arm, "an option everywhere" would
// rest on the untested claim that one shared path serves all of them.
func TestWriteTo_WithEncryptionCoversThePageOperations(t *testing.T) {
	ctx := context.Background()
	data := threePagePDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if err := w.DeletePages(3); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out, WithEncryption(EncryptConfig{
		UserPassword:  "user123",
		OwnerPassword: "owner456",
		Algorithm:     EncryptAES256,
		Permissions:   -1,
	})); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2 := assertPasswordProtected(t, ctx, out.Bytes(), "user123")

	// The queued deletion must still have happened: an encrypted output that
	// silently dropped the mutation would satisfy every assertion above.
	n, err := r2.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 2 {
		t.Errorf("page count: got %d, want 2", n)
	}
}

// TestSplit_EncryptedSourceStaysProtectedWhenAsked is the measured defect
// itself: splitting a protected document used to hand back an unprotected
// one. The source here is genuinely encrypted, opened with its password, and
// re-encrypted under a DIFFERENT one, which is what shows the output carries
// a freshly derived key rather than the source's.
func TestSplit_EncryptedSourceStaysProtectedWhenAsked(t *testing.T) {
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(threePagePDF()), int64(len(threePagePDF())))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	w.Encrypt(EncryptConfig{
		UserPassword:  "source-user",
		OwnerPassword: "source-owner",
		Algorithm:     EncryptAES256,
		Permissions:   -1,
	})
	var enc bytes.Buffer
	if err := w.WriteTo(ctx, &enc); err != nil {
		t.Fatalf("WriteTo (encrypt): %v", err)
	}

	src, err := Open(ctx, bytes.NewReader(enc.Bytes()), int64(enc.Len()), WithPassword("source-user"))
	if err != nil {
		t.Fatalf("Open encrypted source: %v", err)
	}

	var out bytes.Buffer
	if err := Split(ctx, src, []int{2}, &out, WithEncryption(EncryptConfig{
		UserPassword:  "split-user",
		OwnerPassword: "split-owner",
		Algorithm:     EncryptAES256,
		Permissions:   -1,
	})); err != nil {
		t.Fatalf("Split: %v", err)
	}

	r2 := assertPasswordProtected(t, ctx, out.Bytes(), "split-user")

	// The source's password must NOT open the output: the /ID the output
	// carries is the one its own key was derived from, so a carried-over key
	// would show up here.
	if _, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("source-user")); err == nil {
		t.Error("the source's password opens the split output: the output reuses the source key")
	}

	text, err := r2.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if text != "Page2" {
		t.Errorf("text: got %q, want %q", text, "Page2")
	}
}
