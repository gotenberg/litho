package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestOpen_AES128WithoutTopLevelLength pins that an AES-128 document opens
// when its encryption dictionary carries no top-level /Length.
//
// ISO 32000-1, Table 20 defines that entry only for V 2 and 3; a V 4 handler
// states its key length in the crypt filter (Table 25). litho defaulted the
// missing entry to 40 bits, derived a 5-byte key, and reported the correct
// password as wrong. The fixture is litho's own AES-128 output with the entry
// overwritten by spaces, so every offset in the file still holds.
func TestOpen_AES128WithoutTopLevelLength(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := redactableTextPDF("")
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	w.Encrypt(EncryptConfig{UserPassword: "user", OwnerPassword: "owner", Algorithm: EncryptAES128, Permissions: -1})
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	encrypted := out.Bytes()
	entry := []byte("/Filter /Standard /Length 128")
	at := bytes.Index(encrypted, entry)
	if at < 0 {
		t.Fatalf("the encryption dictionary no longer reads %q; update the fixture", entry)
	}
	stripped := bytes.Clone(encrypted)
	length := at + len("/Filter /Standard ")
	copy(stripped[length:], strings.Repeat(" ", len("/Length 128")))

	reopened, err := Open(ctx, bytes.NewReader(stripped), int64(len(stripped)), WithPassword("user"))
	if err != nil {
		t.Fatalf("Open with the user password: %v", err)
	}
	text, err := reopened.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "SECRET") || !strings.Contains(text, "KEEP") {
		t.Fatalf("ExtractText = %q, want the document's text", text)
	}
}
