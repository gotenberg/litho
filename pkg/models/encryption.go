package models

// Permissions represents the document's permission flags as defined by
// ISO 32000-2, Table 22. For unencrypted documents, all flags are true.
// Concurrent reads of a value are safe; a write needs synchronization.
type Permissions struct {
	Print             bool // Bit 3: Print the document.
	ModifyContent     bool // Bit 4: Modify document contents.
	ExtractText       bool // Bit 5: Copy or extract text and graphics.
	ModifyAnnotations bool // Bit 6: Add or modify annotations, fill form fields.
	FillForms         bool // Bit 9: Fill form fields (even if bit 6 is clear).
	Assemble          bool // Bit 11: Assemble the document.
	PrintHighQuality  bool // Bit 12: Print high-quality (bit 3 must also be set).
}

// Mask builds the 32-bit /P permission value for these flags, suitable for
// [EncryptConfig.Permissions], and the inverse of the decoding in
// [github.com/gotenberg/litho/pkg/pdf.Reader.Permissions]. It sets the
// reserved bits ISO 32000-2, Table 22 requires to be 1: bits 7-8, and bits
// 13-32 for security handlers of revision 3 or greater. The table names no
// value of bits 13-32 for the revision 2 handler, whose mask gets them set as
// well. It clears the reserved low bits 1-2, and sets each named permission
// bit only when its flag is true. Bit 10, the permission to extract content
// for accessibility, has no flag here and stays set. The writer sets the
// reserved bits of any mask the same way (see [EncryptConfig.Permissions]), so
// -1 grants what a Mask with every flag true grants.
func (p Permissions) Mask() int32 {
	// Start from all bits set, then clear the reserved low bits and any
	// denied permission. The high reserved bits stay 1 as the spec wants.
	var m int32 = -1
	m &^= 0b11 // reserved bits 1-2 must be 0
	clear := func(bit int32, granted bool) {
		if !granted {
			m &^= bit
		}
	}
	clear(4, p.Print)
	clear(8, p.ModifyContent)
	clear(16, p.ExtractText)
	clear(32, p.ModifyAnnotations)
	clear(256, p.FillForms)
	clear(1024, p.Assemble)
	clear(2048, p.PrintHighQuality)
	return m
}

// EncryptAlgorithm identifies a PDF standard-security-handler algorithm.
// Values cover the ISO 32000 V=1..5 security handlers. The zero value is
// [EncryptAES256], the recommended one; the RC4 variants are provided for
// legacy-compatibility writes only, and a caller names them to get them.
// EncryptAlgorithm values are immutable and safe for concurrent use.
type EncryptAlgorithm int

// Each algorithm carries a minimum PDF version, and the output declares it:
// a file selecting a handler its declared version does not define misdescribes
// itself. RC4 needs nothing beyond PDF 1.4, AES-128 raises the output to 1.6,
// and AES-256 raises it to 1.7 and records the Adobe extension that defines
// it. See [EncryptAES256] for why 1.7 rather than 2.0.
//
// The constants are listed from the strongest handler down, and the
// strongest is the zero value, so an [EncryptConfig] that names no algorithm
// never selects a weak one.
const (
	// EncryptAES256 uses 256-bit AES-CBC (V=5, R=6) through the AESV3 crypt
	// filter. Recommended, and the zero value.
	//
	// ISO 32000-2 defines this handler, but the output declares PDF 1.7 plus
	// an /Extensions entry naming Adobe extension level 3 rather than PDF
	// 2.0. AES-256 is an extension to 1.7, not a feature of it, and declaring
	// 2.0 would overstate a document whose body is unchanged.
	EncryptAES256 EncryptAlgorithm = iota
	// EncryptAES128 uses 128-bit AES-CBC (V=4, R=4) through the AESV2 crypt
	// filter, which ISO 32000-1, Table 25 introduces in PDF 1.6. The output
	// declares at least that version.
	EncryptAES128
	// EncryptRC4_128 uses 128-bit RC4 (V=2, R=3), a PDF 1.4 handler. Weak.
	// Legacy only.
	EncryptRC4_128
	// EncryptRC4_40 uses 40-bit RC4 (V=1, R=2), a PDF 1.1 handler. Weak.
	// Legacy only.
	EncryptRC4_40
)

// EncryptConfig specifies the parameters for password-based encryption.
// Concurrent reads of a value are safe; a write needs synchronization.
type EncryptConfig struct {
	// UserPassword unlocks the document for reading. Empty string means
	// the document can be opened without a password, but [Permissions]
	// still apply to viewers that enforce them.
	UserPassword string
	// OwnerPassword grants full access. Writers should pick a different
	// value from [EncryptConfig.UserPassword]; callers that supply identical
	// passwords get the same effective permissions as an
	// empty-user-password doc.
	//
	// Under [EncryptAES256] both passwords are prepared with SASLprep (RFC
	// 4013) before they are hashed, as ISO 32000-2, 7.6.4.3.3 requires, so "Ⅸ"
	// and "IX" name the same password. A password SASLprep rejects yields
	// [github.com/gotenberg/litho/pkg/pdf.ErrPasswordRejected]. The other
	// algorithms hash the password bytes as given (ISO 32000-1, 7.6.3.3).
	OwnerPassword string
	// Algorithm selects the standard security handler variant. The zero
	// value selects [EncryptAES256]; [EncryptRC4_40] and the other weaker
	// handlers are written only when named.
	Algorithm EncryptAlgorithm
	// Permissions is the 32-bit PDF permission mask. Pass -1 for "all
	// permissions granted", or build one with [Permissions.Mask].
	//
	// The P entry the output carries holds the permission bits of the mask
	// and the reserved bits at the values ISO 32000-2, 7.6.4.2, Table 22
	// gives them, whatever the mask holds there: bits 1-2 cleared ("Reserved.
	// Must be zero (0)."), bits 7-8 set ("Reserved. Must be 1.") and bits
	// 13-32 set ("(Security handlers of revision 3 or greater) Reserved. Must
	// be 1."). The table names no value of bits 13-32 for the revision 2
	// handler, [EncryptRC4_40], which gets them set as well, as the example
	// of ISO 32000-1, 7.6.3.2 shows. -1 is written as -4, and 0, every
	// permission denied, as -3904. Readers ignore those bits (ISO 32000-1,
	// 7.6.3.2), so this changes no permission.
	//
	// An output that declares PDF 2.0 sets bit 10 whatever the mask says: ISO
	// 32000-2, Table 22 marks the bit "Not used" and reads "PDF writers shall
	// always set this bit to 1". Below 2.0 the mask is written as given, and
	// bit 10 is the permission to extract text and graphics in support of
	// accessibility (ISO 32000-1, Table 22).
	Permissions int32
}
