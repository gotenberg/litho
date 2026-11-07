package offcrypto

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// sampleDir holds the samples testdata/fetch.sh downloads and
// testdata/soffice.sh writes. The tests that read them skip when they are
// absent.
const sampleDir = "../../../test/integration/testdata/offcrypto"

func readSample(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(sampleDir, rel))
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("%s is absent: run pkg/internal/offcrypto/testdata/fetch.sh and soffice.sh", rel)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func zipEntry(t *testing.T, pkg []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		t.Fatalf("not a zip archive: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
	}
	t.Fatalf("no entry %s", name)
	return nil
}

// xmlText returns the character data of an XML document, whitespace
// collapsed.
func xmlText(t *testing.T, doc []byte) string {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(doc))
	var sb strings.Builder
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("not XML: %v", err)
		}
		if cd, ok := tok.(xml.CharData); ok {
			sb.Write(cd)
			sb.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// TestOpenOOXML_Samples decrypts the samples of Word, Excel and LibreOffice
// and compares them with the decrypted packages their sources publish.
//
// # Corpus evidence
//
//   - offcrypto: msoffcrypto-tool's Agile (SHA-512, and SHA-1) and Standard
//     samples decrypt to its published outputs byte for byte;
//     protected_page_1.docx (Standard, password foo) holds page_1.docx's
//     text and theme; LibreOffice's own writer produces Standard
//     encryption.
func TestOpenOOXML_Samples(t *testing.T) {
	const msPassword = "Password1234_"
	for _, tc := range []struct {
		in, out, password string
	}{
		{"msoffcrypto/tests/inputs/example_password.docx", "msoffcrypto/tests/outputs/example.docx", msPassword},
		{"msoffcrypto/tests/inputs/example_password.xlsx", "msoffcrypto/tests/outputs/example.xlsx", msPassword},
		{"msoffcrypto/tests/inputs/ecma376agile_sha1_password.xlsx", "msoffcrypto/tests/outputs/example.xlsx", msPassword},
		{"msoffcrypto/tests/inputs/ecma376standard_password.docx", "msoffcrypto/tests/outputs/ecma376standard_password_plain.docx", msPassword},
	} {
		t.Run(filepath.Base(tc.in), func(t *testing.T) {
			in, want := readSample(t, tc.in), readSample(t, tc.out)
			got, err := OpenOOXML(context.Background(), in, tc.password)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("decrypted %d bytes, differ from the %d of %s", len(got), len(want), tc.out)
			}
			if _, err := OpenOOXML(context.Background(), in, "password1234_"); !errors.Is(err, office.ErrEncrypted) {
				t.Errorf("wrong password: %v, want ErrEncrypted", err)
			}
		})
	}
}

func TestOpenOOXML_GotenbergProtected(t *testing.T) {
	in := readSample(t, "gotenberg/test/integration/testdata/protected_page_1.docx")
	plain := readSample(t, "gotenberg/test/integration/testdata/page_1.docx")
	got, err := OpenOOXML(context.Background(), in, "foo")
	if err != nil {
		t.Fatal(err)
	}
	// Word saved the protected copy again: the packages differ in their
	// parts and styles, the document's text and theme are page_1.docx's.
	if a, b := xmlText(t, zipEntry(t, got, "word/document.xml")), xmlText(t, zipEntry(t, plain, "word/document.xml")); a != b || a != "Page 1" {
		t.Errorf("document text %q, page_1.docx has %q", a, b)
	}
	if !bytes.Equal(zipEntry(t, got, "word/theme/theme1.xml"), zipEntry(t, plain, "word/theme/theme1.xml")) {
		t.Error("the theme differs from page_1.docx's")
	}
	for _, pw := range []string{"bar", ""} {
		if _, err := OpenOOXML(context.Background(), in, pw); !errors.Is(err, office.ErrEncrypted) {
			t.Errorf("password %q: %v, want ErrEncrypted", pw, err)
		}
	}
}

func TestOpenOOXML_BinarySamples(t *testing.T) {
	for _, name := range []string{"rc4cryptoapi_password.doc", "rc4cryptoapi_password.xls"} {
		in := readSample(t, "msoffcrypto/tests/inputs/"+name)
		if _, err := OpenOOXML(context.Background(), in, "Password1234_"); !errors.Is(err, office.ErrEncrypted) || !strings.Contains(err.Error(), "97-2003") {
			t.Errorf("%s: %v, want ErrEncrypted naming the binary format", name, err)
		}
	}
}

func TestOpenOOXML_LibreOfficeSample(t *testing.T) {
	in := readSample(t, "soffice/page_1-lo.docx")
	got, err := OpenOOXML(context.Background(), in, "foo")
	if err != nil {
		t.Fatal(err)
	}
	if txt := xmlText(t, zipEntry(t, got, "word/document.xml")); txt != "Page 1" {
		t.Errorf("document text %q, want Page 1", txt)
	}
}

// manifestParams reads META-INF/manifest.xml into the ODFParams of each
// encrypted entry, the way the odf package does: each attribute verbatim,
// the Argon2id ones in either namespace.
func manifestParams(t *testing.T, pkg []byte) map[string]ODFParams {
	t.Helper()
	type elem struct {
		XMLName  xml.Name
		Attrs    []xml.Attr `xml:",any,attr"`
		Children []elem     `xml:",any"`
	}
	var m elem
	if err := xml.Unmarshal(zipEntry(t, pkg, "META-INF/manifest.xml"), &m); err != nil {
		t.Fatal(err)
	}
	attr := func(e elem, local string) string {
		for _, a := range e.Attrs {
			if a.Name.Local == local {
				return a.Value
			}
		}
		return ""
	}
	out := map[string]ODFParams{}
	for _, fe := range m.Children {
		for _, ed := range fe.Children {
			if ed.XMLName.Local != "encryption-data" {
				continue
			}
			p := ODFParams{Size: attr(fe, "size"), ChecksumType: attr(ed, "checksum-type"), Checksum: attr(ed, "checksum")}
			for _, c := range ed.Children {
				switch c.XMLName.Local {
				case "algorithm":
					p.AlgorithmName, p.InitialisationVector = attr(c, "algorithm-name"), attr(c, "initialisation-vector")
				case "start-key-generation":
					p.StartKeyGenerationName, p.StartKeySize = attr(c, "start-key-generation-name"), attr(c, "key-size")
				case "key-derivation":
					p.KeyDerivationName, p.KeySize = attr(c, "key-derivation-name"), attr(c, "key-size")
					p.IterationCount, p.Salt = attr(c, "iteration-count"), attr(c, "salt")
					p.Argon2Iterations, p.Argon2Memory, p.Argon2Lanes = attr(c, "argon2-iterations"), attr(c, "argon2-memory"), attr(c, "argon2-lanes")
				}
			}
			out[attr(fe, "full-path")] = p
		}
	}
	return out
}

// storedEntry returns an entry's bytes as the archive stores them.
func storedEntry(t *testing.T, pkg []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			if f.Method != zip.Store {
				t.Fatalf("%s is not STORED", name)
			}
			return zipEntry(t, pkg, name)
		}
	}
	t.Fatalf("no entry %s", name)
	return nil
}

// TestOpenODFEntry_LibreOfficeSamples decrypts the documents the pinned
// Gotenberg image's LibreOffice encrypts in its three schemes, and checks
// they hold the same text.
func TestOpenODFEntry_LibreOfficeSamples(t *testing.T) {
	texts := map[string]string{}
	for _, tc := range []struct {
		name, algorithm string
	}{
		{"memo-blowfish.odt", odfBlowfish},
		{"memo-aes-cbc.odt", "http://www.w3.org/2001/04/xmlenc#aes256-cbc"},
		{"memo-gcm.odt", "http://www.w3.org/2009/xmlenc11#aes256-gcm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := readSample(t, "soffice/"+tc.name)
			params := manifestParams(t, pkg)
			if len(params) == 0 {
				t.Fatal("no encrypted entry")
			}
			if p, ok := params["encrypted-package"]; ok {
				if p.AlgorithmName != tc.algorithm {
					t.Fatalf("algorithm %s, want %s", p.AlgorithmName, tc.algorithm)
				}
				inner, err := OpenODFEntry(context.Background(), storedEntry(t, pkg, "encrypted-package"), p, "foo")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := OpenODFEntry(context.Background(), storedEntry(t, pkg, "encrypted-package"), p, "bar"); !errors.Is(err, office.ErrEncrypted) {
					t.Errorf("wrong password: %v", err)
				}
				texts[tc.name] = xmlText(t, zipEntry(t, inner, "content.xml"))
				return
			}
			for name, p := range params {
				if p.AlgorithmName != tc.algorithm {
					t.Fatalf("%s: algorithm %s, want %s", name, p.AlgorithmName, tc.algorithm)
				}
				got, err := OpenODFEntry(context.Background(), storedEntry(t, pkg, name), p, "foo")
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if strings.HasSuffix(name, ".xml") {
					txt := xmlText(t, got)
					if name == "content.xml" {
						texts[tc.name] = txt
					}
				}
				if _, err := OpenODFEntry(context.Background(), storedEntry(t, pkg, name), p, "bar"); !errors.Is(err, office.ErrEncrypted) {
					t.Errorf("%s: wrong password: %v", name, err)
				}
			}
		})
	}
	if len(texts) == 3 && (texts["memo-blowfish.odt"] != texts["memo-aes-cbc.odt"] || texts["memo-aes-cbc.odt"] != texts["memo-gcm.odt"]) {
		t.Errorf("the three encryptions hold different text:\n%q\n%q\n%q", texts["memo-blowfish.odt"], texts["memo-aes-cbc.odt"], texts["memo-gcm.odt"])
	}
	if len(texts) == 3 && !strings.Contains(texts["memo-gcm.odt"], "This memo records the Q4 decisions.") {
		t.Errorf("content text %.200q is not memo.odt's", texts["memo-gcm.odt"])
	}
}
