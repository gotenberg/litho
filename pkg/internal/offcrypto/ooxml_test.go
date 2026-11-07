package offcrypto

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// testPackage returns a small zip archive of n bytes of content or more, the
// stand-in for an OOXML package.
func testPackage(t testing.TB, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "word/document.xml", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(bytes.Repeat([]byte("<w:p/>"), n/6+1))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestOpenOOXML_ZipUnchanged(t *testing.T) {
	pkg := testPackage(t, 10)
	got, err := OpenOOXML(context.Background(), pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	if &got[0] != &pkg[0] {
		t.Error("a zip package was copied")
	}
}

func TestOpenOOXML_AgileRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    agileOptions
		size int
	}{
		{"SHA1-AES128", agileOptions{"SHA1", 128, 1000}, 100},
		{"SHA256-AES192", agileOptions{"SHA256", 192, 1000}, 4096},
		{"SHA384-AES256", agileOptions{"SHA384", 256, 1000}, 10000},
		{"SHA512-AES256", agileOptions{"SHA512", 256, 100000}, 5000},
		{"zero-spin", agileOptions{"SHA512", 128, 0}, 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := testPackage(t, tc.size)
			info, enc := agileEncrypt(t, pkg, "pässwörd", tc.o, 1)
			doc := encryptedOOXML(t, info, enc)
			got, err := OpenOOXML(context.Background(), doc, "pässwörd")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, pkg) {
				t.Fatalf("decrypted %d bytes, want the %d of the package", len(got), len(pkg))
			}
			if _, err := OpenOOXML(context.Background(), doc, "wrong"); !errors.Is(err, office.ErrEncrypted) {
				t.Errorf("wrong password: %v, want ErrEncrypted", err)
			}
			if _, err := OpenOOXML(context.Background(), doc, ""); !errors.Is(err, office.ErrEncrypted) || !strings.Contains(err.Error(), "none was given") {
				t.Errorf("empty password: %v, want ErrEncrypted naming the missing password", err)
			}
		})
	}
}

func TestOpenOOXML_StandardRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name  string
		major uint16
		alg   uint32
	}{
		{"2.2-AES128", 2, algAES128},
		{"3.2-AES128-implicit", 3, 0},
		{"3.2-AES192", 3, algAES192},
		{"4.2-AES256", 4, algAES256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := testPackage(t, 7000)
			info, enc := standardEncrypt(t, pkg, "secret", tc.major, tc.alg, 2)
			doc := encryptedOOXML(t, info, enc)
			got, err := OpenOOXML(context.Background(), doc, "secret")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, pkg) {
				t.Fatal("decrypted package differs")
			}
			if _, err := OpenOOXML(context.Background(), doc, "Secret"); !errors.Is(err, office.ErrEncrypted) {
				t.Errorf("wrong password: %v, want ErrEncrypted", err)
			}
		})
	}
}

func TestOpenOOXML_VelvetSweatshop(t *testing.T) {
	pkg := testPackage(t, 100)
	info, enc := standardEncrypt(t, pkg, velvetSweatshop, 4, algAES128, 3)
	doc := encryptedOOXML(t, info, enc)
	for _, pw := range []string{"", "anything", velvetSweatshop} {
		got, err := OpenOOXML(context.Background(), doc, pw)
		if err != nil || !bytes.Equal(got, pkg) {
			t.Errorf("password %q: %v, want the package through the default password", pw, err)
		}
	}
}

func TestOpenOOXML_Integrity(t *testing.T) {
	pkg := testPackage(t, 3000)
	info, enc := agileEncrypt(t, pkg, "pw", agileOptions{"SHA256", 128, 10}, 4)
	enc[len(enc)-1] ^= 1
	_, err := OpenOOXML(context.Background(), encryptedOOXML(t, info, enc), "pw")
	if !errors.Is(err, office.ErrMalformed) || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("tampered package: %v, want ErrMalformed for the integrity check", err)
	}
}

func TestOpenOOXML_Refusals(t *testing.T) {
	le := binary.LittleEndian
	pkg := testPackage(t, 100)
	agileInfo, agileEnc := agileEncrypt(t, pkg, "pw", agileOptions{"SHA1", 128, 1}, 5)
	stdInfo, stdEnc := standardEncrypt(t, pkg, "pw", 3, algAES128, 5)
	rc4 := bytes.Clone(stdInfo)
	le.PutUint32(rc4[12+8:], algRC4)
	external := bytes.Clone(stdInfo)
	le.PutUint32(external[4:], flagCryptoAPI|flagAES|flagExternal)
	badKeyBits := bytes.Clone(stdInfo)
	le.PutUint32(badKeyBits[12+16:], 256)
	replace := func(old, repl string) []byte {
		return []byte(strings.Replace(string(agileInfo), old, repl, 1))
	}
	spin := replace(`spinCount="1"`, `spinCount="10000001"`)
	md5 := []byte(strings.ReplaceAll(string(agileInfo), `hashAlgorithm="SHA1"`, `hashAlgorithm="MD5"`))
	cert := replace(`uri="http://schemas.microsoft.com/office/2006/keyEncryptor/password"`, `uri="http://schemas.microsoft.com/office/2006/keyEncryptor/certificate"`)
	cfb := replace(`ChainingModeCBC`, `ChainingModeCFB`)
	badB64 := replace(`encryptedKeyValue="`, `encryptedKeyValue="!`)
	for _, tc := range []struct {
		name string
		doc  []byte
		want error
		msg  string
	}{
		{"neither", []byte("%PDF-1.7"), office.ErrMalformed, "neither"},
		{"no encryption info", buildCFB(t, 3, []cfbStream{{"Other", []byte("x")}}), office.ErrMalformed, "without an EncryptionInfo"},
		{"no package", buildCFB(t, 3, []cfbStream{{"EncryptionInfo", stdInfo}}), office.ErrMalformed, "EncryptedPackage"},
		{"short info", encryptedOOXML(t, []byte{4, 0}, stdEnc), office.ErrMalformed, "EncryptionInfo of"},
		{"rc4", encryptedOOXML(t, rc4, stdEnc), office.ErrEncrypted, "RC4"},
		{"external", encryptedOOXML(t, external, stdEnc), office.ErrEncrypted, "external"},
		{"key bits", encryptedOOXML(t, badKeyBits, stdEnc), office.ErrMalformed, "key size"},
		{"extensible", encryptedOOXML(t, []byte{4, 0, 3, 0, 0, 0, 0, 0}, stdEnc), office.ErrEncrypted, "Extensible"},
		{"version", encryptedOOXML(t, []byte{9, 0, 9, 0, 0, 0, 0, 0}, stdEnc), office.ErrEncrypted, "version 9.9"},
		{"agile reserved", encryptedOOXML(t, append([]byte{4, 0, 4, 0, 0, 0, 0, 0}, agileInfo[8:]...), agileEnc), office.ErrMalformed, "reserved"},
		{"agile xml", encryptedOOXML(t, append(bytes.Clone(agileInfo[:8]), "<encryption"...), agileEnc), office.ErrMalformed, "descriptor"},
		{"spin count", encryptedOOXML(t, spin, agileEnc), office.ErrEncrypted, "spin count"},
		{"md5", encryptedOOXML(t, md5, agileEnc), office.ErrEncrypted, "MD5"},
		{"certificate", encryptedOOXML(t, cert, agileEnc), office.ErrEncrypted, "certificate"},
		{"chaining", encryptedOOXML(t, cfb, agileEnc), office.ErrEncrypted, "ChainingModeCFB"},
		{"base64", encryptedOOXML(t, badB64, agileEnc), office.ErrMalformed, "encryptedKeyValue"},
		{"short package", encryptedOOXML(t, stdInfo, stdEnc[:4]), office.ErrMalformed, "EncryptedPackage"},
		{"oversized package", encryptedOOXML(t, stdInfo, append(le.AppendUint64(nil, 1<<40), stdEnc[8:]...)), office.ErrMalformed, "declares"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := OpenOOXML(context.Background(), tc.doc, "pw")
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("got %v, want %v mentioning %q", err, tc.want, tc.msg)
			}
		})
	}
}

func TestOpenOOXML_BinaryFormats(t *testing.T) {
	fib := make([]byte, 0x20)
	encFIB := bytes.Clone(fib)
	binary.LittleEndian.PutUint16(encFIB[0x0A:], 0x0100)
	bof := []byte{0x09, 0x08, 0x02, 0x00, 0, 0}
	filePass := append(bytes.Clone(bof), 0x2F, 0x00, 0x00, 0x00)
	eof := []byte{0x0A, 0x00, 0x00, 0x00}
	for _, tc := range []struct {
		name    string
		streams []cfbStream
		want    error
	}{
		{"doc", []cfbStream{{"WordDocument", fib}}, office.ErrMalformed},
		{"encrypted doc", []cfbStream{{"WordDocument", encFIB}}, office.ErrEncrypted},
		{"xls", []cfbStream{{"Workbook", append(bytes.Clone(bof), eof...)}}, office.ErrMalformed},
		{"encrypted xls", []cfbStream{{"Workbook", filePass}}, office.ErrEncrypted},
		{"biff5 xls", []cfbStream{{"Book", bof}}, office.ErrMalformed},
		{"ppt", []cfbStream{{"PowerPoint Document", []byte{1}}}, office.ErrMalformed},
		{"encrypted ppt", []cfbStream{{"PowerPoint Document", []byte{1}}, {"EncryptedSummary", []byte{1}}}, office.ErrEncrypted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := OpenOOXML(context.Background(), buildCFB(t, 3, tc.streams), "pw")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestOpenOOXML_Context(t *testing.T) {
	pkg := testPackage(t, 100)
	info, enc := agileEncrypt(t, pkg, "pw", agileOptions{"SHA512", 256, 1}, 6)
	doc := encryptedOOXML(t, info, enc)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenOOXML(ctx, doc, "pw"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestIteratedHash_Cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := iteratedHash(ctx, agileHash("SHA512"), []byte{1}, "pw", maxAgileSpinCount); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}
