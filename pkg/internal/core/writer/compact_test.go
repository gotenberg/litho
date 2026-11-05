package writer

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// compactAndRewrite runs [Compact] with passes, then [WriteRewrite] with the
// placements it returned, as every write path does.
func compactAndRewrite(t *testing.T, cfg RewriteConfig, passes CompactConfig) (string, error) {
	t.Helper()
	packed, err := Compact(context.Background(), cfg.Allocator, cfg.Trailer, passes)
	if err != nil {
		return "", err
	}
	cfg.Packed = packed
	var buf bytes.Buffer
	if err := WriteRewrite(context.Background(), &buf, cfg); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// packTo is compactAndRewrite with packing alone, failing the test on an
// error.
func packTo(t *testing.T, cfg RewriteConfig, pack bool) string {
	t.Helper()
	out, err := compactAndRewrite(t, cfg, CompactConfig{PackObjectStreams: pack})
	if err != nil {
		t.Fatalf("compacting and rewriting: %v", err)
	}
	return out
}

// TestCompact_RefusesAnEncryptedDocument pins the order the passes depend on:
// encryption keys every object to its number and runs over the pending set,
// so a document whose trailer already carries /Encrypt is past the point
// where packing, dedup or recompression could still see plaintext. The same
// fixture must compact without /Encrypt, or the refusal proves nothing.
func TestCompact_RefusesAnEncryptedDocument(t *testing.T) {
	t.Parallel()
	for name, passes := range map[string]CompactConfig{
		"object streams": {PackObjectStreams: true},
		"dedup":          {DedupStreams: true},
		"recompression":  {RecompressImages: &ImageRecompression{}},
		"nothing":        {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			build := func(encrypted bool) ([]PackedObject, error) {
				alloc := NewObjAllocator(0)
				catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})
				substantialDicts(alloc, 12)
				alloc.Alloc(dupStream())
				alloc.Alloc(dupStream())
				trailer := ast.PdfDict{"Root": catalog}
				if encrypted {
					trailer["Encrypt"] = alloc.Alloc(ast.PdfDict{"Filter": ast.PdfName("Standard")})
				}
				return Compact(context.Background(), alloc, trailer, passes)
			}
			if _, err := build(true); !errors.Is(err, ErrCompactEncrypted) {
				t.Errorf("Compact on an encrypted document returned %v, want ErrCompactEncrypted", err)
			}
			if _, err := build(false); err != nil {
				t.Errorf("Compact on the same document in the clear: %v", err)
			}
		})
	}
}

// TestCompact_RunsEveryPassOnce walks the three passes together: two copies
// of one image come out as one stream, the dictionaries land in an object
// stream, and the output reopens with every stream an object of its own.
func TestCompact_RunsEveryPassOnce(t *testing.T) {
	t.Parallel()
	alloc, trailer, _ := directStreamDoc()
	substantialDicts(alloc, 12)
	alloc.Alloc(dupStream())
	alloc.Alloc(dupStream())

	out, err := compactAndRewrite(t, RewriteConfig{Allocator: alloc, Trailer: trailer}, CompactConfig{
		PackObjectStreams: true,
		DedupStreams:      true,
		RecompressImages:  &ImageRecompression{},
	})
	if err != nil {
		t.Fatalf("compacting and rewriting: %v", err)
	}
	if n := strings.Count(out, "shared image bytes"); n != 1 {
		t.Errorf("the output holds %d copies of the shared stream, want 1", n)
	}
	if !strings.Contains(out, "/Type /ObjStm") {
		t.Error("nothing was packed")
	}
	if !strings.Contains(out, "/Type /Catalog") {
		t.Error("the catalog is not an object of its own in the output")
	}
	assertEveryStreamIndirect(t, []byte(out))
}

// TestPackObjectStreams_LeavesTheCatalogUnpacked pins the one object packing
// passes over by choice: [WriteRewrite] edits the catalog after packing, when
// it knows what version the output needs, and the encryption that may run in
// between reads it, so it has to stay pending.
func TestPackObjectStreams_LeavesTheCatalogUnpacked(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})
	refs := substantialDicts(alloc, 12)

	placed, err := PackObjectStreams(context.Background(), alloc, ast.PdfDict{"Root": catalog})
	if err != nil {
		t.Fatalf("PackObjectStreams: %v", err)
	}
	if len(placed) != len(refs) {
		t.Fatalf("packed %d objects, want the %d dictionaries beside the catalog", len(placed), len(refs))
	}
	for _, p := range placed {
		if p.ObjNum == catalog.ObjectNum {
			t.Fatal("the catalog was packed")
		}
	}
	if _, still := alloc.Pending()[catalog.ObjectNum]; !still {
		t.Error("the catalog left the pending set")
	}
}
