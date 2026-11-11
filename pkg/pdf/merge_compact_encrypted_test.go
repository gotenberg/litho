package pdf

import (
	"bytes"
	"testing"
)

// TestMerge_PacksAndDedupsAnEncryptedOutput pins that Merge applies object
// streams and dedup together to an output it encrypts: the write engine
// compacts before it encrypts, so the merged file carries an object stream,
// refuses to open without its password and opens with it.
func TestMerge_PacksAndDedupsAnEncryptedOutput(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	data := threePagePDF()
	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{openColorPDF(t, data), openColorPDF(t, data)}, &out,
		WithObjectStreams(), WithDedup(),
		WithEncryption(EncryptConfig{UserPassword: "s", OwnerPassword: "o"})); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(objStmMarker)) {
		t.Error("the encrypted merge carries no object stream")
	}
	if opensWithoutAPassword(t, out.Bytes()) {
		t.Fatal("the merge opens without its password, so it is not encrypted")
	}
	r := openColorPDF(t, out.Bytes(), WithPassword("s"))
	if n, err := r.PageCount(ctx); err != nil || n != 6 {
		t.Errorf("PageCount = %d, %v; want 6", n, err)
	}
}
