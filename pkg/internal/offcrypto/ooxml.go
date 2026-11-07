package offcrypto

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// velvetSweatshop is the password Excel encrypts a workbook with when its
// structure is protected but no open password is set. LibreOffice tries it
// before asking for one (oox/source/core/filterdetect.cxx); so does
// [OpenOOXML].
const velvetSweatshop = "VelvetSweatshop"

// zipSignatures open a zip archive: a local file header, or the end of
// central directory record of an empty archive (APPNOTE.TXT, 4.3.7 and
// 4.3.16).
var zipSignatures = [][]byte{[]byte("PK\x03\x04"), []byte("PK\x05\x06")}

// errWrongPassword is what a scheme returns when its verifier refuses the
// password; [OpenOOXML] wraps it in office.ErrEncrypted.
var errWrongPassword = errors.New("wrong password")

// OpenOOXML returns the OOXML package data holds, decrypted with password
// when data is an encrypted one.
//
// A zip archive is returned unchanged, without a copy. A compound file
// (MS-CFB) holding the EncryptionInfo and EncryptedPackage streams of
// MS-OFFCRYPTO, 2.3.4.4 is decrypted with Agile (2.3.4.10) or Standard
// (2.3.4.5) encryption and the package returned in a new slice. When
// password is empty, or does not open the document, Excel's built-in
// VelvetSweatshop password is tried too.
//
// It fails with office.ErrEncrypted when the password does not open the
// document, or when the document is encrypted with a scheme this package
// does not read (RC4 CryptoAPI, Extensible encryption, a certificate key
// encryptor, an encrypted binary .doc, .xls or .ppt); with
// office.ErrMalformed when data is neither a zip archive nor a compound
// file, when the compound file or its descriptor cannot be read, or when
// the compound file is an unencrypted binary document. It returns ctx's
// error when ctx ends first.
func OpenOOXML(ctx context.Context, data []byte, password string) ([]byte, error) {
	for _, sig := range zipSignatures {
		if bytes.HasPrefix(data, sig) {
			return data, nil
		}
	}
	if !bytes.HasPrefix(data, cfbSignature) {
		return nil, fmt.Errorf("%w: neither a zip package nor a compound file", office.ErrMalformed)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cf, err := readCFB(ctx, data)
	if err != nil {
		return nil, err
	}
	info, ok, err := cf.stream("EncryptionInfo")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, classifyBinary(cf)
	}
	pkg, ok, err := cf.stream("EncryptedPackage")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: EncryptionInfo without an EncryptedPackage stream", office.ErrMalformed)
	}
	scheme, err := parseEncryptionInfo(info)
	if err != nil {
		return nil, err
	}
	candidates := []string{password}
	if password != velvetSweatshop {
		candidates = append(candidates, velvetSweatshop)
	}
	if password == "" {
		candidates = candidates[1:]
	}
	for _, pw := range candidates {
		out, err := scheme.decrypt(ctx, pkg, pw)
		switch {
		case err == nil:
			if !bytes.HasPrefix(out, zipSignatures[0]) {
				return nil, fmt.Errorf("%w: the decrypted %s package is not a zip archive", office.ErrMalformed, scheme.name())
			}
			return out, nil
		case errors.Is(err, errWrongPassword):
			continue
		default:
			return nil, err
		}
	}
	if password == "" {
		return nil, fmt.Errorf("%w: the document is protected by a password (%s) and none was given", office.ErrEncrypted, scheme.name())
	}
	return nil, fmt.Errorf("%w: the password does not open the document (%s)", office.ErrEncrypted, scheme.name())
}

// packageScheme is an encryption scheme read from EncryptionInfo.
type packageScheme interface {
	// name names the scheme in messages.
	name() string
	// decrypt returns the package EncryptedPackage holds, or an error
	// wrapping errWrongPassword when password fails the verifier.
	decrypt(ctx context.Context, encrypted []byte, password string) ([]byte, error)
}

// parseEncryptionInfo reads the version of an EncryptionInfo stream
// (MS-OFFCRYPTO, 2.3.4.4) and dispatches on it: 4.4 with reserved 0x40 is
// Agile (2.3.4.10); 2.2, 3.2 and 4.2 are Standard (2.3.4.5) or RC4
// CryptoAPI (2.3.5.1) by their algorithm; 3.3 and 4.3 are Extensible
// (2.3.4.6).
func parseEncryptionInfo(info []byte) (packageScheme, error) {
	if len(info) < 8 {
		return nil, fmt.Errorf("%w: EncryptionInfo of %d bytes", office.ErrMalformed, len(info))
	}
	le := binary.LittleEndian
	major, minor := le.Uint16(info), le.Uint16(info[2:])
	switch {
	case major == 4 && minor == 4:
		if le.Uint32(info[4:]) != 0x40 {
			return nil, fmt.Errorf("%w: Agile EncryptionInfo reserved field %#x", office.ErrMalformed, le.Uint32(info[4:]))
		}
		return parseAgile(info[8:])
	case (major == 2 || major == 3 || major == 4) && minor == 2:
		return parseStandard(info[4:])
	case (major == 3 || major == 4) && minor == 3:
		return nil, fmt.Errorf("%w: Extensible encryption (MS-OFFCRYPTO 2.3.4.6) is not read", office.ErrEncrypted)
	default:
		return nil, fmt.Errorf("%w: EncryptionInfo version %d.%d is not read", office.ErrEncrypted, major, minor)
	}
}

// classifyBinary names what a compound file without EncryptionInfo is: a
// binary Word, Excel or PowerPoint document, encrypted (office.ErrEncrypted)
// or not (office.ErrMalformed), or something else (office.ErrMalformed).
func classifyBinary(cf *cfbFile) error {
	if wd, ok, err := cf.stream("WordDocument"); err != nil {
		return err
	} else if ok {
		// MS-DOC, 2.5.2: FibBase.fEncrypted is bit 8 of the flags at offset
		// 0x0A of the FIB at the start of the WordDocument stream.
		if len(wd) >= 0x0C && binary.LittleEndian.Uint16(wd[0x0A:])&0x0100 != 0 {
			return fmt.Errorf("%w: an encrypted Word 97-2003 binary document (MS-DOC), whose encryption is not read", office.ErrEncrypted)
		}
		return fmt.Errorf("%w: a Word 97-2003 binary document (MS-DOC), not an OOXML package", office.ErrMalformed)
	}
	for _, name := range []string{"Workbook", "Book"} {
		wb, ok, err := cf.stream(name)
		if err != nil {
			return err
		}
		if ok {
			if xlsFilePass(wb) {
				return fmt.Errorf("%w: an encrypted Excel 97-2003 binary workbook (MS-XLS), whose encryption is not read", office.ErrEncrypted)
			}
			return fmt.Errorf("%w: an Excel 97-2003 binary workbook (MS-XLS), not an OOXML package", office.ErrMalformed)
		}
	}
	if _, ok, err := cf.stream("PowerPoint Document"); err != nil {
		return err
	} else if ok {
		// MS-PPT, 2.3.7: an encrypted presentation carries the
		// EncryptedSummary stream.
		if _, enc, err := cf.stream("EncryptedSummary"); err != nil {
			return err
		} else if enc {
			return fmt.Errorf("%w: an encrypted PowerPoint 97-2003 binary presentation (MS-PPT), whose encryption is not read", office.ErrEncrypted)
		}
		return fmt.Errorf("%w: a PowerPoint 97-2003 binary presentation (MS-PPT), not an OOXML package", office.ErrMalformed)
	}
	return fmt.Errorf("%w: a compound file without an EncryptionInfo stream", office.ErrMalformed)
}

// xlsFilePass reports whether the workbook globals substream of a BIFF8
// Workbook stream carries a FilePass record (MS-XLS, 2.4.117), which
// follows the BOF record when the workbook is encrypted. The scan stops at
// the substream's EOF record.
func xlsFilePass(wb []byte) bool {
	const (
		recFilePass = 0x002F
		recEOF      = 0x000A
	)
	le := binary.LittleEndian
	for off := 0; off+4 <= len(wb); {
		typ, size := le.Uint16(wb[off:]), int(le.Uint16(wb[off+2:]))
		switch typ {
		case recFilePass:
			return true
		case recEOF:
			return false
		}
		off += 4 + size
	}
	return false
}
