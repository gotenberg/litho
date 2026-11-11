package pdf

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConvert_SpoolKeepsTheBytes converts one document of every format
// with the spool file forced on and forced off, and checks that both write
// the same bytes and that no file is left in the temp directory. The
// Office documents come from the provisioned corpora and are skipped when
// those are absent.
func TestConvert_SpoolKeepsTheBytes(t *testing.T) {
	t.Parallel()
	testdata := filepath.Join("..", "..", "test", "integration", "testdata")
	for _, tc := range []struct {
		format Format
		path   string
		src    string
	}{
		{format: FormatText, src: strings.Repeat("Spooled text over many lines.\n", 400)},
		{format: FormatMarkdown, src: "# Title\n\nA paragraph with *emphasis*.\n\n![dot](img/dot.png)\n"},
		{format: FormatHTML, src: convertHTMLPage},
		{format: FormatCSV, src: "a,b,c\n1,2,3\n4,5,6\n"},
		{format: FormatDOCX, path: "libreoffice/docx/sources/ooxmlimport/n764745-alignment.docx"},
		{format: FormatODT, path: "libreoffice/odt/sources/layout/tdf119875.odt"},
		{format: FormatXLSX, path: "libreoffice/xlsx/sources/sc/cell-borders.xlsx"},
		{format: FormatODS, path: "libreoffice/ods/sources/sc/fdo62729.ods"},
		{format: FormatPPTX, path: "libreoffice/pptx/sources/oox/tdf163803_image_with_fill.pptx"},
		{format: FormatODP, path: "libreoffice/odp/sources/sd/16-9.odp"},
	} {
		t.Run(string(tc.format), func(t *testing.T) {
			t.Parallel()
			src := []byte(tc.src)
			if tc.path != "" {
				data, err := os.ReadFile(filepath.Join(testdata, tc.path))
				if errors.Is(err, os.ErrNotExist) {
					t.Skipf("%s is not provisioned", tc.path)
				}
				if err != nil {
					t.Fatal(err)
				}
				src = data
			}
			fonts, tmp := convertFontDir(t), t.TempDir()
			var outs [2][]byte
			for i, held := range []int64{-1, 1} {
				var out bytes.Buffer
				opts := []ConvertOption{WithConvertFontDirs(fonts), WithResources(convertHTMLResources(t)), WithConvertTempDir(tmp), func(o *convertOptions) { o.spool = held }}
				if _, err := Convert(context.Background(), bytes.NewReader(src), tc.format, &out, opts...); err != nil {
					t.Fatalf("Convert with spool %d: %v", held, err)
				}
				outs[i] = out.Bytes()
				entries, err := os.ReadDir(tmp)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Errorf("%d files left in the temp directory", len(entries))
				}
			}
			if !bytes.Equal(outs[0], outs[1]) {
				t.Errorf("the spool changes the output: %d bytes against %d", len(outs[1]), len(outs[0]))
			}
		})
	}
}
