package mutations

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// notARedactionRefusal names every sentinel this package declares that
// [RedactPageRegions] cannot return, against the entry point that owns it.
//
// The map is the point of the test rather than a wart on it. Membership is
// asserted, so a sentinel added to the package is a failure until someone
// either documents it as a refusal or states here why it is not one. An
// allow-list that fails closed is what a doc enumeration needs: the one on
// RedactPageRegions had drifted to naming 13 of the 22 refusals it can report,
// and every omission entered the same way, by a commit that added a sentinel
// and updated no list.
var notARedactionRefusal = map[string]string{
	"ErrRegionCoversAnImage":           "superseded, returned by nothing; blanking replaced it",
	"ErrCatalogUnreadable":             "CatalogDict; the redaction path takes CatalogRef, which reports no error",
	"ErrPageTreeNotReproduced":         "FullRewrite",
	"ErrAnnotationAppearanceHoldsText": "ScrubAnnotationText, document-wide rather than in-region",
	"ErrAnnotationNotAddressable":      "ScrubAnnotationText",
	"ErrFieldValueNotAddressable":      "ScrubFormFieldValues",
	"ErrOutlineItemNotAddressable":     "ScrubOutlineTitles",
	"ErrScanTokenEmpty":                "ScanOutputForTokens",
	"ErrNestedContainerUnread":         "ScanOutputForTokens, which reports the carrying stream unreadable",
	"ErrSanitizationIncomplete":        "SanitizeDocument",
	"ErrPagePruneIncomplete":           "DeletePages",
	"ErrInvalidPageNumber":             "RotatePages, Split, DeletePages, InsertPages and the three stamps; pdf.Writer.Redact checks its pages itself",
	"ErrAttachmentTreeIncomplete":      "the attachment walks",
	"ErrImageShared":                   "CheckBlankedImageUses, document-wide rather than per page",
	"ErrSignatureFieldUnavailable":     "WriteWithSignature",
	"ErrSignatureFieldLocked":          "WriteWithSignature",
	"ErrSeedValueUnmet":                "WriteWithSignature",
	"ErrSubFilterUnwritable":           "the Signer WriteWithSignature calls",
	"ErrInvalidAnnotation":             "BakeAnnotations",
}

// TestRedactPageRegionsDocNamesEveryRefusal pins the refusal list on
// [RedactPageRegions] against the sentinels this package actually declares.
//
// The doc comment reads as the list, not as examples: "it refuses whatever it
// cannot remove exactly" followed by one bullet per case. Measured when this
// test was written, it named 13 of 22 and omitted [ErrImageIsStencilMask],
// [ErrAnnotationNotPlaceable], [ErrAnnotationStraddlesRegion],
// [ErrAnnotationIsFormWidget], [ErrFormNotAddressable],
// [ErrStructElementPartlyRemoved], [ErrStructElementNotAddressable],
// [ErrSpanOutOfRange] and [ErrSpansOverlap]. Six came in with the in-region
// annotation removal and the structure-tree scrub; one came in the commit that
// taught blanking about stencils. None was deliberate, which is the shape of
// enumeration drift: a hand-maintained list does not grow with the surface.
//
// Declarations are read with go/ast rather than matched by pattern. A regular
// expression keyed on "Err" would also match a local variable or a doc mention,
// and this test exists because a list drifted from what it lists.
func TestRedactPageRegionsDocNamesEveryRefusal(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing package: %v", err)
	}

	doc := redactPageRegionsDoc(t, pkgs)
	// Without this the assertions below pass vacuously if the bullets are
	// reworded away or the function is renamed.
	if !strings.Contains(doc, "it refuses whatever it cannot remove exactly") {
		t.Fatal("RedactPageRegions no longer documents a refusal list, so this test proves nothing")
	}

	declared := 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					continue
				}
				for _, spec := range gen.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range value.Names {
						if !strings.HasPrefix(name.Name, "Err") {
							continue
						}
						declared++
						if _, excluded := notARedactionRefusal[name.Name]; excluded {
							continue
						}
						if !strings.Contains(doc, "["+name.Name+"]") {
							t.Errorf("%s is a sentinel of this package that RedactPageRegions never names "+
								"and notARedactionRefusal does not exclude: document it as a refusal, or "+
								"record there which entry point owns it", name.Name)
						}
					}
				}
			}
		}
	}

	// A parser that silently matched nothing would report no drift at all.
	// Measured at 33 sentinels when this test was written.
	if declared < 28 {
		t.Errorf("found only %d sentinels, so the parser is not reading the package", declared)
	}

	// An exclusion naming a sentinel that no longer exists is stale, and would
	// go on excusing a name nothing declares.
	for name := range notARedactionRefusal {
		if !sentinelDeclared(pkgs, name) {
			t.Errorf("notARedactionRefusal excludes %s, which this package no longer declares", name)
		}
	}
}

// redactPageRegionsDoc returns the doc comment of RedactPageRegions.
func redactPageRegionsDoc(t *testing.T, pkgs map[string]*ast.Package) string {
	t.Helper()

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "RedactPageRegions" {
					continue
				}
				if fn.Doc == nil {
					t.Fatal("RedactPageRegions carries no doc comment")
				}
				return fn.Doc.Text()
			}
		}
	}
	t.Fatal("RedactPageRegions not found, so the enumeration cannot be checked")
	return ""
}

// sentinelDeclared reports whether the package declares a package-scope var of
// this name.
func sentinelDeclared(pkgs map[string]*ast.Package, want string) bool {
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					continue
				}
				for _, spec := range gen.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range value.Names {
						if name.Name == want {
							return true
						}
					}
				}
			}
		}
	}
	return false
}
