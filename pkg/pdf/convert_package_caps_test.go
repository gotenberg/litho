package pdf

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
)

// withEntries returns the zip pkg with n empty entries added after its
// own, each a file the document never reads.
func withEntries(t *testing.T, pkg []byte, n int) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if err := zw.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	for i := range n {
		if _, err := zw.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("Unused/%d", i), Method: zip.Store}); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestConvert_PackageEntryCapIsOneForEveryFormat locks one cap on a
// package's entries, 65,536, for OpenDocument and OOXML alike: a package
// of 20,000 entries converts, one of 70,000 fails with
// [ErrConvertInputTooLarge].
func TestConvert_PackageEntryCapIsOneForEveryFormat(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	for _, tc := range []struct {
		format testFormat
		pkg    []byte
	}{
		{formatODT, targetODT(t)},
		{formatDOCX, testDOCX(t)},
	} {
		_, err := fromFormat(tc.format, bytes.NewReader(withEntries(t, tc.pkg, 20_000)), WithConvertFontDirs(dir)).WriteTo(context.Background(), io.Discard)
		if err != nil {
			t.Errorf("%s of 20,000 entries: %v, want a PDF", tc.format, err)
		}
		_, err = fromFormat(tc.format, bytes.NewReader(withEntries(t, tc.pkg, 70_000)), WithConvertFontDirs(dir)).WriteTo(context.Background(), io.Discard)
		if !errors.Is(err, ErrConvertInputTooLarge) {
			t.Errorf("%s of 70,000 entries: %v, want ErrConvertInputTooLarge", tc.format, err)
		}
	}
}
