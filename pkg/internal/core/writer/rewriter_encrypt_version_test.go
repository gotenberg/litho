package writer

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// Encryption carries the same self-description problem object streams did.
// litho wrote AES-128 (/V 4, the AESV2 crypt filter, PDF 1.6) and AES-256
// (/V 5 /R 6, AESV3, ISO 32000-2) under whatever version the source declared,
// so encrypting a 1.4 document produced "%PDF-1.4" over a PDF 2.0 encryption
// dictionary. litho's own reader reported such a file as version 1.4.
//
// qpdf 12.4.1 on the same input writes %PDF-1.6 for AES-128, and %PDF-1.7 with
// an /ADBE extension dictionary for AES-256 rather than %PDF-2.0. litho
// matches that: AES-256 is an extension to 1.7, not a feature of it, and
// saying 2.0 would overstate a document whose body is unchanged.

// encryptDict builds an /Encrypt dictionary carrying the given crypt filter
// method, or none at all when cfm is empty, which is what the RC4 algorithms
// produce.
func encryptDict(cfm string) ast.PdfDict {
	dict := ast.PdfDict{"Filter": ast.PdfName("Standard")}
	if cfm == "" {
		return dict
	}
	dict["CF"] = ast.PdfDict{
		"StdCF": ast.PdfDict{
			"CFM":       ast.PdfName(cfm),
			"AuthEvent": ast.PdfName("DocOpen"),
		},
	}
	return dict
}

// encryptedRewrite writes a one-object document whose trailer selects the
// given crypt filter, mirroring how applyEncryption stores the dictionary: as
// an indirect reference, not inline.
func encryptedRewrite(t *testing.T, cfm, version string, catalog ast.PdfDict) string {
	t.Helper()
	alloc := NewObjAllocator(0)
	root := alloc.Alloc(catalog)
	trailer := ast.PdfDict{"Root": root}
	trailer["Encrypt"] = alloc.Alloc(encryptDict(cfm))

	return rewriteTo(t, RewriteConfig{
		Version:   version,
		Allocator: alloc,
		Trailer:   trailer,
	})
}

func plainCatalog() ast.PdfDict {
	return ast.PdfDict{"Type": ast.PdfName("Catalog")}
}

func TestWriteRewrite_EncryptionDeclaresItsOwnVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		cfm        string
		source     string
		wantHeader string
		wantADBE   bool
	}{
		// AESV3 is ISO 32000-2, declared as an extension to 1.7.
		{"AES-256 raises to 1.7 with an extension", "AESV3", "1.4", "%PDF-1.7", true},
		// AESV2 is a plain PDF 1.6 feature and needs no extension entry.
		{"AES-128 raises to 1.6 bare", "AESV2", "1.4", "%PDF-1.6", false},
		// RC4 selects /V 1 or /V 2 and writes no /CF at all. /V 2 is PDF 1.4,
		// so the source version is already honest and must not move.
		{"RC4 leaves the version alone", "", "1.4", "%PDF-1.4", false},
		// The floor never lowers a document that already declares more.
		{"a later source version survives AES-256", "AESV3", "2.0", "%PDF-2.0", true},
		{"a later source version survives AES-128", "AESV2", "1.7", "%PDF-1.7", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := encryptedRewrite(t, tc.cfm, tc.source, plainCatalog())

			if !strings.HasPrefix(out, tc.wantHeader+"\n") {
				t.Errorf("header = %q, want %s", out[:min(len(out), 12)], tc.wantHeader)
			}
			if got := strings.Contains(out, "/ADBE"); got != tc.wantADBE {
				t.Errorf("/ADBE extension present = %v, want %v", got, tc.wantADBE)
			}
		})
	}
}

// TestWriteRewrite_AES256WritesTheExtensionEntry pins the pair qpdf writes.
// The base version alone is only half the declaration: /V 5 is an extension to
// PDF 1.7, so a reader is entitled to be told which extension.
func TestWriteRewrite_AES256WritesTheExtensionEntry(t *testing.T) {
	t.Parallel()
	out := encryptedRewrite(t, "AESV3", "1.4", plainCatalog())

	for _, want := range []string{"/Extensions", "/ADBE", "/BaseVersion /1.7", "/ExtensionLevel 3"} {
		if !strings.Contains(out, want) && !strings.Contains(out, strings.ReplaceAll(want, " ", "")) {
			t.Errorf("output does not carry %q:\n%s", want, out[:min(len(out), 500)])
		}
	}
}

// TestWriteRewrite_EncryptionRaisesTheCatalogVersion covers the entry that
// silently defeats a header-only fix, the same way it did for packing: per
// ISO 32000-1, 7.5.2 the catalog /Version overrides the header, and litho's
// reader honors it downward.
func TestWriteRewrite_EncryptionRaisesTheCatalogVersion(t *testing.T) {
	t.Parallel()
	out := encryptedRewrite(t, "AESV3", "1.4", ast.PdfDict{
		"Type":    ast.PdfName("Catalog"),
		"Version": ast.PdfName("1.4"),
	})

	if !containsEntry(out, "/Version", "/1.7") {
		t.Error("catalog /Version was not raised; the file still reports PDF 1.4")
	}
	if containsEntry(out, "/Version", "/1.4") {
		t.Error("catalog still declares /Version /1.4 alongside an AESV3 handler")
	}
}

// TestEnsureADBEExtension_RaisesNeverLowers: a source declaring a later base
// version or a higher extension level knows something this writer does not.
func TestEnsureADBEExtension_RaisesNeverLowers(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	root := alloc.Alloc(ast.PdfDict{
		"Type": ast.PdfName("Catalog"),
		"Extensions": ast.PdfDict{
			"ADBE": ast.PdfDict{
				"BaseVersion":    ast.PdfName("2.0"),
				"ExtensionLevel": ast.PdfInt(8),
			},
			// An unrelated vendor prefix must survive untouched.
			"TEST": ast.PdfDict{"BaseVersion": ast.PdfName("1.7")},
		},
	})
	trailer := ast.PdfDict{"Root": root}

	ensureADBEExtension(alloc, trailer, minAESV3Version, aesV3ExtensionLevel)

	catalog, ok := alloc.Pending()[root.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatal("catalog left the pending set")
	}
	ext, ok := catalog["Extensions"].(ast.PdfDict)
	if !ok {
		t.Fatal("no /Extensions dictionary")
	}
	if _, ok := ext["TEST"]; !ok {
		t.Error("an unrelated extension prefix was dropped")
	}
	adbe, ok := ext["ADBE"].(ast.PdfDict)
	if !ok {
		t.Fatal("no /ADBE dictionary")
	}
	if adbe["BaseVersion"] != ast.PdfName("2.0") {
		t.Errorf("BaseVersion = %v, want 2.0 preserved", adbe["BaseVersion"])
	}
	if adbe["ExtensionLevel"] != ast.PdfInt(8) {
		t.Errorf("ExtensionLevel = %v, want 8 preserved", adbe["ExtensionLevel"])
	}
}

// TestEnsureADBEExtension_FillsAnEmptyCatalog is the ordinary path: no
// existing /Extensions at all.
func TestEnsureADBEExtension_FillsAnEmptyCatalog(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	root := alloc.Alloc(plainCatalog())

	ensureADBEExtension(alloc, ast.PdfDict{"Root": root}, minAESV3Version, aesV3ExtensionLevel)

	catalog, ok := alloc.Pending()[root.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatal("catalog left the pending set")
	}
	ext, ok := catalog["Extensions"].(ast.PdfDict)
	if !ok {
		t.Fatal("no /Extensions dictionary was written")
	}
	adbe, ok := ext["ADBE"].(ast.PdfDict)
	if !ok {
		t.Fatal("no /ADBE dictionary was written")
	}
	if adbe["BaseVersion"] != ast.PdfName("1.7") {
		t.Errorf("BaseVersion = %v, want 1.7", adbe["BaseVersion"])
	}
	if adbe["ExtensionLevel"] != ast.PdfInt(3) {
		t.Errorf("ExtensionLevel = %v, want 3", adbe["ExtensionLevel"])
	}
}

func TestEncryptionVersionFloor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cfm  string
		want string
	}{
		{"AESV3 needs 1.7", "AESV3", minAESV3Version},
		{"AESV2 needs 1.6", "AESV2", minAESV2Version},
		{"RC4 writes no crypt filter", "", ""},
		{"an unknown method claims nothing", "Whatever", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			alloc := NewObjAllocator(0)
			trailer := ast.PdfDict{"Encrypt": alloc.Alloc(encryptDict(tc.cfm))}
			if got := encryptionVersionFloor(alloc, trailer); got != tc.want {
				t.Errorf("encryptionVersionFloor = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEncryptionVersionFloor_NoEncryptionAtAll: an unencrypted trailer must
// answer "", not a floor, or every plain rewrite would drift upward.
func TestEncryptionVersionFloor_NoEncryptionAtAll(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	if got := encryptionVersionFloor(alloc, ast.PdfDict{}); got != "" {
		t.Errorf("encryptionVersionFloor on a plain trailer = %q, want empty", got)
	}
}
