package html

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// testSerif builds a "Test Serif" face covering printable ASCII, every
// glyph half an em wide.
func testSerif() []byte {
	glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 500, Empty: true}}
	for r := rune('!'); r <= '~'; r++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: fmt.Sprintf("uni%04X", r), Unicode: r, Advance: 500})
	}
	return fontbuild.TrueType{
		PostScriptName: "TestSerif-Regular",
		Family:         "Test Serif",
		Subfamily:      "Regular",
		NotdefAdvance:  500,
		Glyphs:         glyphs,
	}.Build()
}

var (
	sharedFontsOnce sync.Once
	sharedFonts     *text.Fonts
	errSharedFonts  error
	// sharedFontsDir is the directory testFonts made, "" when it made none.
	sharedFontsDir string
)

// TestMain removes the directory of the test face once the tests end: the
// index reads the face's file on first use, so no single test owns it.
func TestMain(m *testing.M) {
	code := m.Run()
	// Running the Once first orders this read after the write of any test.
	sharedFontsOnce.Do(func() {})
	if sharedFontsDir != "" {
		os.RemoveAll(sharedFontsDir)
	}
	os.Exit(code)
}

// testFonts returns an index of the test face, built once for the package:
// the index reads the face's file again on first use, so its directory
// lives as long as the test binary.
func testFonts(t testing.TB) *text.Fonts {
	t.Helper()
	sharedFontsOnce.Do(func() {
		dir, err := os.MkdirTemp("", "litho-html-fonts")
		if err != nil {
			errSharedFonts = err
			return
		}
		sharedFontsDir = dir
		if err := os.WriteFile(filepath.Join(dir, "test_serif.ttf"), testSerif(), 0o600); err != nil {
			errSharedFonts = err
			return
		}
		sharedFonts, errSharedFonts = text.NewFonts(context.Background(), nil, []string{dir})
	})
	if errSharedFonts != nil {
		t.Fatalf("indexing the test font: %v", errSharedFonts)
	}
	return sharedFonts
}

// prep prepares src with o, failing the test on error.
func prep(t *testing.T, src string, o Options) *prepared {
	t.Helper()
	o, err := o.withDefaults()
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	p, err := prepare(context.Background(), []byte(src), o, nil)
	if err != nil {
		t.Fatalf("prepare(%q): %v", src, err)
	}
	return p
}

// stubLayout is a layout engine that lays nothing out: one blank page, a
// Document root, and every ID of the tree anchored at the top of page 1.
func stubLayout(_ context.Context, root *layout.Node, _ *text.Fonts, opts layout.Options) (*layout.Result, error) {
	res := &layout.Result{
		Pages:     []*compose.Page{{Width: opts.Page.Width, Height: opts.Page.Height}},
		Structure: &compose.StructElem{Role: "Document", Lang: opts.Lang},
		Anchors:   map[string]compose.Dest{},
		Warnings:  []compose.Warning{{Code: "layout-stub", Message: "laid out by the stub"}},
	}
	y := 0.0
	eachNode(root, func(n *layout.Node) {
		if n.ID != "" {
			res.Anchors[n.ID] = compose.Dest{Page: 1, Y: y}
			y++
		}
	})
	return res, nil
}

// eachNode calls fn on n and its descendants in document order.
func eachNode(n *layout.Node, fn func(*layout.Node)) {
	fn(n)
	for _, c := range n.Children {
		eachNode(c, fn)
	}
}

// byID returns the node of the tree with that ID, failing the test without
// one.
func byID(t *testing.T, root *layout.Node, id string) *layout.Node {
	t.Helper()
	var found *layout.Node
	eachNode(root, func(n *layout.Node) {
		if found == nil && n.ID == id {
			found = n
		}
	})
	if found == nil {
		t.Fatalf("no node with ID %q", id)
	}
	return found
}

// treeText returns the text of n's subtree: Text nodes as they are, a line
// break as "|".
func treeText(n *layout.Node) string {
	var b strings.Builder
	eachNode(n, func(c *layout.Node) {
		switch c.Kind {
		case layout.Text:
			b.WriteString(c.Text)
		case layout.LineBreak:
			b.WriteString("|")
		case layout.Element, layout.Replaced:
		}
	})
	return b.String()
}

// codes returns the codes of ws in order.
func codes(ws []compose.Warning) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Code)
	}
	return out
}

// hasWarning reports a warning of code whose message holds sub.
func hasWarning(ws []compose.Warning, code, sub string) bool {
	for _, w := range ws {
		if w.Code == code && strings.Contains(w.Message, sub) {
			return true
		}
	}
	return false
}

// pngBytes encodes a w by h opaque PNG.
func pngBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// jpegBytes encodes a w by h JPEG.
func jpegBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// gifBytes encodes a w by h GIF.
func gifBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	img := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White})
	if err := gif.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngHeader returns the signature and IHDR chunk of a w by h PNG, without
// pixels: enough for image.DecodeConfig, not for image.Decode.
func pngHeader(t testing.TB, w, h int) []byte {
	t.Helper()
	full := pngBytes(t, 1, 1)
	// Signature (8) and IHDR (4 length, 4 type, 13 data, 4 CRC).
	head := bytes.Clone(full[:33])
	head[16], head[17], head[18], head[19] = byte(w>>24), byte(w>>16), byte(w>>8), byte(w)
	head[20], head[21], head[22], head[23] = byte(h>>24), byte(h>>16), byte(h>>8), byte(h)
	// image/png checks the CRC of the chunk it reads.
	crc := crc32.ChecksumIEEE(head[12:29])
	head[29], head[30], head[31], head[32] = byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc)
	return head
}

// zeroFS serves files of an endless run of zero bytes under any name.
type zeroFS struct{}

func (zeroFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	return &zeroFile{name: name}, nil
}

type zeroFile struct{ name string }

func (f *zeroFile) Stat() (fs.FileInfo, error) { return fstest.MapFS{f.name: {}}.Stat(f.name) }
func (*zeroFile) Close() error                 { return nil }
func (*zeroFile) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
