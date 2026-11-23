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
	{formatHTML, "html/sources/backgrounds", "ad3d9cb015938c4a8fcc5c80e69b435f51e0b80158c1b7330a8ade04cda9b606"},
	{formatHTML, "html/sources/images", "7043ac87a76f1bc2fdf788bf362d438d974ea8f16290b9c69a3259806b5d39fc"},
	{formatHTML, "html/sources/svg", "5f04742891bb4d44a5e6252129dcf7688a120539c2e01770ce81f6ae562166fe"},
	{formatHTML, "html/sources/filter-drop-shadow", "5a632d25f3458a092129aa162c9ce41de53546388e32ddc57b190270fce26f01"},
	{formatHTML, "html/sources/text-shadow-blur", "1cfa09825f827f5eda0701a3d8d03742e50a8b093e7a506287b2f8f5907ca37f"},
	{formatMarkdown, "html/sources/markdown-table-align", "0e74cf4ef240afde5f34cd448ff7c201ee8e93471e6426ddbdb2ab9d8c5eddce"},
	{formatText, "", "0e4db9f2b09fa9340fbd2159128243e13463cacc2cb782f33da207eb4ce928f6"},
	{formatCSV, "csv/sources/paper-table", "f73035983761a56c117b0c5a008bfb073d396f7b7f2318e5580dc49e8df98c0d"},
	{formatDOCX, "docx/parts/charts-line-area", "0fcabef8d539b4b26d43300527183d9d69355b195d75e168113402695892b612"},
	{formatDOCX, "docx/parts/sink-report", "80431fd48ffeec5caf4f0d449a10f45370a71a95d39466225908eecc46afd9cb"},
	{formatDOCX, "docx/parts/shapes-effects-3d", "802e8748188b548d2f00291b77873f2ad1b4b8999e567f6a7c4055ea6177d92b"},
	{formatXLSX, "xlsx/parts/real_dashboard", "639fc81b371686c7da2d112c6cf163f8f3ac8130f2701d0628ed0ef60d101f9e"},
	{formatXLSX, "xlsx/parts/charts_pie_doughnut", "184d4bbd6845d2460a8374c66123cd8a6e75097b9c074bdaff25973320797d4a"},
	{formatPPTX, "pptx/parts/chart-pie", "afa778c071982c26dd18f8179adbe2d4aab78c9370d05b3a9a40f88632cbeda4"},
	{formatPPTX, "pptx/parts/chart-3d", "450fa5ad9ef116cdc6811c7c27adfb543c1a5db8914741c78e3c332ee5fda6c3"},
	{formatPPTX, "pptx/parts/fill-gradient", "31bad285933ef607311093125b914047183d33be5a821c09a8892c46e8c69db6"},
	{formatPPTX, "pptx/parts/effects-glow-softedge-reflection", "7fdda65003c45cceedaf0430f12a425a36ae9b2aa6945eca3704ca3086c38a26"},
	{formatODT, "odt/sources/mixed-sections", "a0d798280a169ffd5893ec47d4c5856f170ae674044058b085cc132498f3dea8"},
	{formatODS, "ods/sources/mixed-sheets", "dda900462838cc6e366ba8a2a51fd3131ad6b9e12fc2cb7ebe2fba3a911fd596"},
	{formatODP, "odp/sources/wide-deck", "bcc17f4477a30c6e5c969d07c7247bf63a85a7be2a2fb0bcd4bd8f52086cf979"},
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
