package odt

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1" //nolint:gosec // ODF 1.3 Part 2, 4.16.6 names SHA-1 as a start key
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/pbkdf2"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// encryptOptions selects how [encryptODT] encrypts.
type encryptOptions struct {
	sha1Start bool // the start key is SHA-1 of the password, else SHA-256
	whole     bool // LibreOffice's whole-package encryption (encrypted-package)
}

// encryptODT returns the package pkg with every entry but the mimetype and
// the manifest encrypted with password as ODF 1.3 Part 2, 3.4 says: the
// entry deflated, AES-256-CBC with a key PBKDF2-HMAC-SHA1 derives from the
// password's SHA-256 (or SHA-1) start key, padded as xmlenc-core 5.2, and
// the SHA-256 of its first 1024 deflated bytes as the checksum. The salts
// and IVs come from a fixed counter: the test is deterministic.
func encryptODT(t *testing.T, pkg []byte, password string, o encryptOptions) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		t.Fatal(err)
	}
	type entry struct {
		name string
		data []byte
	}
	var entries []entry
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry{f.Name, b})
	}
	seed := byte(1)
	next := func(n int) []byte {
		out := make([]byte, n)
		for i := range out {
			out[i] = seed
			seed++
		}
		return out
	}
	b64 := base64.StdEncoding.EncodeToString
	encrypt := func(plain []byte) ([]byte, string) {
		var z bytes.Buffer
		w, _ := flate.NewWriter(&z, flate.BestCompression)
		w.Write(plain)
		w.Close()
		deflated := z.Bytes()
		var start []byte
		startName := "http://www.w3.org/2000/09/xmldsig#sha256"
		if o.sha1Start {
			s := sha1.Sum([]byte(password)) //nolint:gosec // the start key ODF 1.3 Part 2, 4.16.6 names
			start, startName = s[:], "http://www.w3.org/2000/09/xmldsig#sha1"
		} else {
			s := sha256.Sum256([]byte(password))
			start = s[:]
		}
		salt, iv := next(16), next(16)
		key := pbkdf2.Key(start, salt, 1024, 32, sha1.New)
		sum := sha256.Sum256(deflated[:min(len(deflated), 1024)])
		n := 16 - len(deflated)%16
		padded := append(bytes.Clone(deflated), bytes.Repeat([]byte{byte(n)}, n)...)
		block, _ := aes.NewCipher(key)
		out := make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
		data := fmt.Sprintf(`<manifest:encryption-data manifest:checksum-type="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0#sha256-1k" manifest:checksum="%s">`+
			`<manifest:algorithm manifest:algorithm-name="http://www.w3.org/2001/04/xmlenc#aes256-cbc" manifest:initialisation-vector="%s"/>`+
			`<manifest:start-key-generation manifest:start-key-generation-name="%s" manifest:key-size="%d"/>`+
			`<manifest:key-derivation manifest:key-derivation-name="PBKDF2" manifest:key-size="32" manifest:iteration-count="1024" manifest:salt="%s"/>`+
			`</manifest:encryption-data>`, b64(sum[:]), b64(iv), startName, len(start), b64(salt))
		return out, data
	}
	const mani = `<?xml version="1.0" encoding="UTF-8"?><manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.3">`
	write := func(files []entry, stored map[string]bool) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, e := range files {
			m := zip.Deflate
			if stored[e.name] {
				m = zip.Store
			}
			w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: m})
			if err != nil {
				t.Fatal(err)
			}
			w.Write(e.data)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	if o.whole {
		inner := write(entries, map[string]bool{"mimetype": true})
		enc, data := encrypt(inner)
		m := mani + `<manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.text"/>` +
			fmt.Sprintf(`<manifest:file-entry manifest:full-path="encrypted-package" manifest:media-type="" manifest:size="%d">`, len(inner)) + data +
			`</manifest:file-entry></manifest:manifest>`
		return write([]entry{{"mimetype", []byte("application/vnd.oasis.opendocument.text")}, {"META-INF/manifest.xml", []byte(m)}, {"encrypted-package", enc}},
			map[string]bool{"mimetype": true, "encrypted-package": true})
	}
	var sb strings.Builder
	sb.WriteString(mani + `<manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.text"/>`)
	stored := map[string]bool{"mimetype": true}
	var out []entry
	for _, e := range entries {
		switch e.name {
		case "mimetype":
			out = append(out, e)
		case "META-INF/manifest.xml":
		default:
			enc, data := encrypt(e.data)
			fmt.Fprintf(&sb, `<manifest:file-entry manifest:full-path="%s" manifest:media-type="text/xml" manifest:size="%d">%s</manifest:file-entry>`, e.name, len(e.data), data)
			out = append(out, entry{e.name, enc})
			stored[e.name] = true
		}
	}
	sb.WriteString(`</manifest:manifest>`)
	out = append(out, entry{"META-INF/manifest.xml", []byte(sb.String())})
	return write(out, stored)
}

func TestConvert_Encrypted(t *testing.T) {
	plain := doc{
		body:     `<text:p>Secret memo</text:p><text:p><draw:frame text:anchor-type="as-char" svg:width="2cm" svg:height="1cm"><draw:image xlink:href="Pictures/a.png"/></draw:frame></text:p>`,
		meta:     `<dc:title>Locked</dc:title>`,
		pictures: map[string][]byte{"Pictures/a.png": tinyPNG(t)},
	}.pack(t)
	for _, tc := range []struct {
		name string
		o    encryptOptions
	}{
		{"aes256-cbc sha256 start key", encryptOptions{}},
		{"aes256-cbc sha1 start key", encryptOptions{sha1Start: true}},
		{"whole package", encryptOptions{whole: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := encryptODT(t, plain, "s3cret", tc.o)
			d := convertDoc(t, data, office.Options{Password: "s3cret"})
			if got := docText(d); got != "Secret memo" {
				t.Errorf("text = %q", got)
			}
			if d.Metadata.Title != "Locked" {
				t.Errorf("title = %q", d.Metadata.Title)
			}
			for _, w := range d.Warnings {
				if strings.Contains(w.Message, "a.png") {
					t.Errorf("the encrypted picture did not load: %v", w)
				}
			}
			for _, pw := range []string{"", "wrong"} {
				_, err := Convert(context.Background(), bytes.NewReader(data), fixtureFonts(t), office.Options{Password: pw})
				if !errors.Is(err, office.ErrEncrypted) {
					t.Errorf("password %q: err = %v, want office.ErrEncrypted", pw, err)
				}
			}
		})
	}
}

func TestConvert_FlatIgnoresPassword(t *testing.T) {
	d := doc{body: `<text:p>Flat text</text:p>`}
	flat := `<?xml version="1.0" encoding="UTF-8"?><office:document ` + nsDecl + ` office:mimetype="application/vnd.oasis.opendocument.text">` +
		testPageLayout + `<office:body><office:text>` + d.body + `</office:text></office:body></office:document>`
	for _, pw := range []string{"", "anything"} {
		got := convertDoc(t, []byte(flat), office.Options{Password: pw})
		if txt := docText(got); txt != "Flat text" {
			t.Errorf("password %q: text = %q", pw, txt)
		}
	}
}

// TestConvert_LibreOfficeEncrypted opens the memo the pinned image's
// LibreOffice encrypted with the password "foo" in its three schemes
// (pkg/internal/offcrypto/testdata/soffice.sh writes them), skipping when
// they are absent.
func TestConvert_LibreOfficeEncrypted(t *testing.T) {
	for _, name := range []string{"memo-blowfish.odt", "memo-aes-cbc.odt", "memo-gcm.odt"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "test", "integration", "testdata", "offcrypto", "soffice", name))
			if errors.Is(err, os.ErrNotExist) {
				t.Skipf("%s is absent: run pkg/internal/offcrypto/testdata/soffice.sh", name)
			}
			if err != nil {
				t.Fatal(err)
			}
			d := convertDoc(t, data, office.Options{Password: "foo"})
			if got := docText(d); !strings.Contains(got, "This memo records the Q4 decisions.") {
				t.Errorf("text = %q", got)
			}
			if _, err := Convert(context.Background(), bytes.NewReader(data), fixtureFonts(t), office.Options{Password: "bar"}); !errors.Is(err, office.ErrEncrypted) {
				t.Errorf("wrong password: err = %v", err)
			}
		})
	}
}
