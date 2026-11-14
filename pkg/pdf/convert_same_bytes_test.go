package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sameBytesCorpus is the directory of the converter corpora's committed
// sources, from this package.
const sameBytesCorpus = "../../test/integration/corpus/litho"

// sameBytesCases are the conversions [TestConvert_SameBytesOnEveryMachine]
// pins: text, tables, pictures, gradients, charts and effect bitmaps in
// every format. path is a document or a source directory under
// [sameBytesCorpus]; an Office Open XML case names the directory of its
// parts, zipped by the test.
var sameBytesCases = []struct {
	format testFormat
	path   string
	want   string
}{
	{formatHTML, "html/sources/grid-invoice", "bbeeb8f26e10a8824d8bbfed9c2219b76caa0ac1b2e5a4373f97b822e23c232b"},
	{formatHTML, "html/sources/backgrounds", "fcf7344290db71856ff67b898c692e6dbcbb7fc637ebc69f1d1fb3b6724ace46"},
	{formatHTML, "html/sources/images", "7043ac87a76f1bc2fdf788bf362d438d974ea8f16290b9c69a3259806b5d39fc"},
	{formatHTML, "html/sources/svg", "b6f7e33b535a9f6978b8a749430112d5ef7e1069753cb7c8ce84209c310f5a56"},
	{formatHTML, "html/sources/filter-drop-shadow", "5a632d25f3458a092129aa162c9ce41de53546388e32ddc57b190270fce26f01"},
	{formatHTML, "html/sources/text-shadow-blur", "1cfa09825f827f5eda0701a3d8d03742e50a8b093e7a506287b2f8f5907ca37f"},
	{formatMarkdown, "html/sources/markdown-table-align", "0e74cf4ef240afde5f34cd448ff7c201ee8e93471e6426ddbdb2ab9d8c5eddce"},
	{formatText, "", "0e4db9f2b09fa9340fbd2159128243e13463cacc2cb782f33da207eb4ce928f6"},
	{formatCSV, "csv/sources/paper-table", "f73035983761a56c117b0c5a008bfb073d396f7b7f2318e5580dc49e8df98c0d"},
	{formatDOCX, "docx/parts/charts-line-area", "110ecd48625d50d59458933e0fbcf8865805e5b1132b7c98708c5e77343a1668"},
	{formatDOCX, "docx/parts/sink-report", "79767a75505748446f3058cfe16554898eb5d481977ee8c8a64d5ca442edd1eb"},
	{formatDOCX, "docx/parts/shapes-effects-3d", "521b61b3032d1134589ef5b724d373ea295d0aaf2df717c2c53aff658a05801b"},
	{formatXLSX, "xlsx/parts/real_dashboard", "63f2ffe66bba4aab421ddf90dfba9cc5cad836df3c59b0c1ed2abac29e9396c0"},
	{formatXLSX, "xlsx/parts/charts_pie_doughnut", "4d0a8cb72acfd7e8c491966d35080567867e3841b38c06d4b04601aaab464b1f"},
	{formatPPTX, "pptx/parts/chart-pie", "176168b9aa2840baf904072c8afe2b7950cd873a654822cbb8d4fb0fae65bc9b"},
	{formatPPTX, "pptx/parts/chart-3d", "ffde85123b9f605a29d3f22c44ce5aa617285117713f8dbd4bcf420bd5e6b166"},
	{formatPPTX, "pptx/parts/fill-gradient", "9a5c1b3bc781fa3a7260e61877b8b7b279efe210f29c84625a3e9413590541a8"},
	{formatPPTX, "pptx/parts/effects-glow-softedge-reflection", "5f65a9c482a857a0ec3628d936f21776848215d58aa896fb9661c464b024de76"},
	{formatODT, "odt/sources/mixed-sections", "a0d798280a169ffd5893ec47d4c5856f170ae674044058b085cc132498f3dea8"},
	{formatODS, "ods/sources/mixed-sheets", "dda900462838cc6e366ba8a2a51fd3131ad6b9e12fc2cb7ebe2fba3a911fd596"},
	{formatODP, "odp/sources/wide-deck", "daab83de9e768130c87af5631420319d1554f6d195d1f103347eceabc06f6c67"},
}

// sameBytesText is the source of the plain text case.
const sameBytesText = "Identical input gives identical bytes.\n\tTabbed line\nA line long enough to wrap at the right margin of the default page, so that the converter breaks it once at least, and then once more for good measure.\n"

// TestConvert_SameBytesOnEveryMachine converts a fixed set of documents
// with the fonts of testdata/fonts and compares the SHA-256 of each PDF
// with the one pinned for it. Every machine must write these bytes: amd64,
// arm64, 386 and arm, whatever the CPU. A pin moves only with a change
// that means to move that conversion's output; the change then states why
// and sets the new pin, which the test prints, on every machine that runs
// it. A pin that moves on one machine alone is a fault: an expression
// whose result depends on the machine (see detmath's
// TestPackages_FuseNoMultiplyAdd).
func TestConvert_SameBytesOnEveryMachine(t *testing.T) {
	t.Parallel()
	fonts, err := filepath.Abs(filepath.Join("testdata", "fonts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range sameBytesCases {
		name := string(tc.format)
		if tc.path != "" {
			name += " " + filepath.Base(tc.path)
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := sameBytesConvert(t, tc.format, tc.path, fonts)
			sum := sha256.Sum256(data)
			if got := hex.EncodeToString(sum[:]); got != tc.want {
				t.Errorf("%s: SHA-256 %s, pinned %s", name, got, tc.want)
			}
		})
	}
}

// sameBytesConvert converts the case at path and returns the PDF.
func sameBytesConvert(t *testing.T, format testFormat, path, fonts string) []byte {
	t.Helper()
	ctx := context.Background()
	var c *Conversion
	switch format {
	case formatText:
		c = FromText(strings.NewReader(sameBytesText), WithConvertFontDirs(fonts))
	case formatHTML, formatMarkdown:
		dir := filepath.Join(sameBytesCorpus, path)
		name := "index.html"
		if format == formatMarkdown {
			name = "index.md"
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if format == formatMarkdown {
			c = FromMarkdown(bytes.NewReader(src), WithConvertFontDirs(fonts), WithResources(os.DirFS(dir)))
		} else {
			c = FromHTML(bytes.NewReader(src), WithConvertFontDirs(fonts), WithResources(os.DirFS(dir)))
		}
	case formatDOCX, formatXLSX, formatPPTX:
		c = fromFormat(format, bytes.NewReader(sameBytesZip(t, filepath.Join(sameBytesCorpus, path))), WithConvertFontDirs(fonts))
	default:
		dir := filepath.Join(sameBytesCorpus, path)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var doc string
		for _, e := range entries {
			if !e.IsDir() && e.Name() != "options.json" {
				doc = filepath.Join(dir, e.Name())
			}
		}
		src, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		c = fromFormat(format, bytes.NewReader(src), WithConvertFontDirs(fonts))
	}
	var out bytes.Buffer
	if _, err := c.WriteTo(ctx, &out); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// sameBytesZip returns the package of the parts under dir.
func sameBytesZip(t *testing.T, dir string) []byte {
	t.Helper()
	parts := make(map[string]string)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "options.json" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		parts[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return zipParts(t, parts)
}
