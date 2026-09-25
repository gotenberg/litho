package compliance

import (
	"context"
	"testing"
)

// FuzzFileStructureBytes feeds arbitrary file bytes to the raw-buffer readers
// in the file-structure checks.
//
// These read the document's own bytes rather than parsed objects: the header
// check judges the first line and the binary comment that must follow it, and
// the stream scanners walk a window of the file looking for where stream data
// begins and whether the declared /Length lands on "endstream". Every byte they
// index comes from the file.
func FuzzFileStructureBytes(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n"))
	f.Add([]byte("%PDF-2.0\r\n%\x80\x81\x82\x83\r\n1 0 obj\n<< /Length 5 >>\nstream\nhello\nendstream\nendobj\n"))
	f.Add([]byte("%PDF-1.4")) // header with no EOL
	f.Add([]byte("garbage before %PDF-1.5\n"))
	f.Add([]byte("1 0 obj\n<< /Length 99999 >>\nstream\nshort\nendstream\n"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()

		for _, profile := range []string{"PDF/A-1b", "PDF/A-2b", "PDF/A-4", "PDF/UA-1"} {
			_ = checkHeaderBytes(data, profile)
		}
		_ = findStreamDataStart(ctx, data)
		for _, off := range []int{0, 1, len(data) / 2} {
			_ = streamLengthMatches(data, off, 0)
			_ = streamLengthMatches(data, off, len(data))
		}
	})
}
