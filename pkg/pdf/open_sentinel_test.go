package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// TestOpen_TranslatesOpenFailuresToPublicSentinels pins the two remaining
// translations in the switch at the top of [Open].
//
// Those branches turn an internal sentinel into a public one:
// document.ErrMissingHeader becomes [ErrNotAPDF], and document.ErrDamagedXRef
// becomes [ErrDamagedXRef]. The internal error is folded in with %v rather than
// %w, deliberately, so no pkg/internal value appears in the public chain. That
// is exactly why a test in pkg/internal cannot protect this: the internal
// package's own assertions keep passing while the public contract disappears.
//
// The fixtures were measured before this test was written, because asserting
// the wrong sentinel would still have passed. A file that merely lacks an xref
// table does NOT fail, which is what the "recovers" case below records.
func TestOpen_TranslatesOpenFailuresToPublicSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"empty input", []byte{}, ErrNotAPDF},
		{"not a PDF at all", []byte("this is plainly not a PDF file at all\n"), ErrNotAPDF},
		{"header with no objects", []byte("%PDF-1.4\n%%EOF\n"), ErrDamagedXRef},
		{"header then garbage", []byte("%PDF-1.7\ngarbage garbage garbage\n"), ErrDamagedXRef},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := Open(context.Background(), bytes.NewReader(tt.data), int64(len(tt.data)))
			if err == nil {
				r.Close()
				t.Fatalf("Open(%s) succeeded, want %v", tt.name, tt.want)
			}
			if !errors.Is(err, tt.want) {
				t.Errorf("Open(%s): err = %v, want errors.Is(err, %v)", tt.name, err, tt.want)
			}
		})
	}
}

// TestOpen_RecoversAFileWithNoXRefTable is the control for the test above.
//
// Without it, the sentinel assertions would also pass if Open had started
// rejecting everything. A document carrying a header and an object but no xref
// table and no trailer is rebuilt rather than refused, so this case must open.
func TestOpen_RecoversAFileWithNoXRefTable(t *testing.T) {
	t.Parallel()

	data := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n")

	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v, want a rebuilt document: if this now fails, the sentinel "+
			"assertions in TestOpen_TranslatesOpenFailuresToPublicSentinels no longer "+
			"discriminate between a damaged file and any file", err)
	}
	r.Close()
}
