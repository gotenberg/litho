package filter

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestDecodeJBIG2_ISO32000Example decodes the JBIG2Decode example of ISO
// 32000-1, 7.4.7 through the filter, its symbol dictionary coming from the
// JBIG2Globals stream, and checks the output is the decoded page inverted:
// JBIG2 foreground is 1, and the filter delivers it as the 0 a DeviceGray
// image paints black.
func TestDecodeJBIG2_ISO32000Example(t *testing.T) {
	t.Parallel()

	page, errP := os.ReadFile("jbig2/testdata/iso32000_jbig2_page.jb2")
	globals, errG := os.ReadFile("jbig2/testdata/iso32000_jbig2_globals.jb2")
	golden, errW := os.ReadFile("jbig2/testdata/iso32000_jbig2.pbm")
	if errP != nil || errG != nil || errW != nil {
		t.Fatalf("reading the example: %v %v %v", errP, errG, errW)
	}
	header := len("P4\n52 66\n")
	want := make([]byte, len(golden)-header)
	for i, b := range golden[header:] {
		want[i] = ^b
	}

	parms := ast.PdfDict{"JBIG2Globals": ast.NewStream(ast.PdfDict{}, globals)}
	got, err := decodeJBIG2(context.Background(), page, parms)
	if err != nil {
		t.Fatalf("decodeJBIG2: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output differs from the inverted golden page")
	}

	// Without its globals the page refers to a symbol dictionary it does not
	// hold, which is malformed rather than unsupported.
	if _, err := decodeJBIG2(context.Background(), page, nil); err == nil || errors.Is(err, ErrUnsupportedFilter) {
		t.Errorf("page without its globals: err = %v, want a decoding error that is not ErrUnsupportedFilter", err)
	}
}

// TestDecodeJBIG2_Refusals pins which failures the pipeline treats as an
// unsupported filter (skipped by [DecodeStreamChecked]) and which as errors.
func TestDecodeJBIG2_Refusals(t *testing.T) {
	t.Parallel()

	// A page information segment followed by an extension segment flagged
	// necessary (T.88, 7.4.14), which no decoder can be expected to know.
	page := binary.BigEndian.AppendUint32(nil, 0)
	page = append(page, 48, 0, 1)
	page = binary.BigEndian.AppendUint32(page, 19)
	page = append(page, 0, 0, 0, 8, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	ext := binary.BigEndian.AppendUint32(nil, 1)
	ext = append(ext, 62, 0, 1)
	ext = binary.BigEndian.AppendUint32(ext, 4)
	ext = append(ext, 0xA0, 0, 0, 1)

	if _, err := decodeJBIG2(context.Background(), append(page, ext...), nil); !errors.Is(err, ErrUnsupportedFilter) {
		t.Errorf("necessary extension: err = %v, want ErrUnsupportedFilter", err)
	}
	if _, err := decodeJBIG2(context.Background(), []byte{0x00, 0x01, 0x02, 0x03}, nil); err == nil || errors.Is(err, ErrUnsupportedFilter) {
		t.Errorf("corrupt data: err = %v, want a decoding error that is not ErrUnsupportedFilter", err)
	}
}
