package crypto

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// This package had NO fuzz target while sitting on the decrypt path: every
// value it reads comes out of the file. /O, /U, /OE and /UE arrive as raw
// strings from the encryption dictionary, the trailer supplies /ID, and
// DecryptFunc is handed the encrypted bytes of every stream and string.
//
// The earlier coverage sweep missed it twice over. It enumerated EXPORTED
// entry points whose FIRST non-ctx parameter is []byte, and crypto's entry is
// NewHandler(encryptDict, trailer): two dictionaries, no []byte in sight.
// A later note dismissed the package on padPassword and truncate, which do
// encode litho's own data; the other ten functions were never adjudicated.
//
// One target per function, per this repo's convention: a single body driving
// several of them can stall the engine while every function is fine.

// FuzzNewHandler asserts that building a handler from a crafted encryption
// dictionary never panics. NewHandler calls Authenticate("") itself, so this
// also drives hashV5, hash2B, the fixed-offset /U and /O slicing of Algorithm
// 2.A, and the legacy Algorithm 3.3/3.1/3.2 paths.
//
// Every dictionary value is fuzzed rather than pinned: /V and /R select the
// algorithm, and the 48-byte length guard on /U, /O and /UE is the thing most
// worth attacking, so a target holding them at a sane size would never reach it.
func FuzzNewHandler(f *testing.F) {
	f.Add(1, 2, 40, -4, []byte("0123456789abcdef0123456789abcdef"), []byte("0123456789abcdef0123456789abcdef"), []byte(""), []byte(""), []byte("fileid"))
	f.Add(5, 6, 256, -4, make([]byte, 48), make([]byte, 48), make([]byte, 32), make([]byte, 32), []byte("id"))
	f.Add(4, 4, 128, -1, []byte("short"), []byte("short"), []byte(""), []byte(""), []byte(""))
	f.Add(0, 0, 0, 0, []byte(""), []byte(""), []byte(""), []byte(""), []byte(""))

	f.Fuzz(func(_ *testing.T, v, r, length, p int, o, u, oe, ue, id []byte) {
		encDict := ast.PdfDict{
			"V":      ast.PdfInt(v),
			"R":      ast.PdfInt(r),
			"Length": ast.PdfInt(length),
			"P":      ast.PdfInt(p),
			"O":      ast.PdfString(o),
			"U":      ast.PdfString(u),
			"OE":     ast.PdfString(oe),
			"UE":     ast.PdfString(ue),
		}
		trailer := ast.PdfDict{"ID": ast.PdfArray{ast.PdfString(id)}}

		h, err := NewHandler(encDict, trailer)
		if err != nil || h == nil {
			return
		}
		_ = h.Authenticate("")
		_ = h.PermissionFlags()
	})
}

// aesDecrypt has NO direct target on purpose, and the omission is not a gap to
// fill: FuzzDecryptFunc reaches it through both AES branches (/V 5, and /V 4
// with AESV2 or AESV3), covering 7 of its 11 blocks from the seed corpus alone
// and the rest once fuzzing starts.
//
// A direct FuzzAESDecrypt(key, data) was written and removed. It WEDGED the
// fuzz engine every run: execs climbed to 1-3 million, then froze at one value
// for the remaining 16-22 seconds while the run still reported PASS and exit 0
// (the failure mode recorded as "a WEDGED go test -fuzz worker still prints
// PASS"). Four hypotheses were measured and refuted: decrypting the engine's
// buffer in place (copying the input made it WORSE), allocation pressure (its
// corpus was SMALLER than FuzzDecryptFunc's, 8351 vs 10333 bytes), a
// pathological cached input (a fresh 5-seed corpus wedged identically, and
// throughput recovered mid-run, which no permanently hung worker does), and
// worker death (no signal or "terminated unexpectedly" in any log). With the
// function covered transitively and healthily, a target that silently stops
// fuzzing is worse than no target. Do not re-add it without a cause.

// FuzzDecryptFunc asserts the per-object decrypt closure never panics. It is
// also where aesDecrypt gets its coverage (see the note above), and it reaches
// the RC4 branch and ObjKey besides. The object and generation numbers are
// fuzzed because ObjKey folds them into the key.
func FuzzDecryptFunc(f *testing.F) {
	f.Add(make([]byte, 5), 1, 4, "V2", 1, 0, []byte("ciphertext"))
	f.Add(make([]byte, 16), 4, 4, "AESV2", 5, 0, make([]byte, 48))
	// A /V 4 RC4 handler with a 40-bit key, the issue19484 shape whose key
	// objKeyBase zero-extends.
	f.Add(make([]byte, 5), 4, 40, "V2", 13, 0, []byte("ciphertext"))
	f.Add(make([]byte, 32), 5, 6, "AESV3", 1, 0, make([]byte, 32))
	f.Add([]byte(""), 0, 0, "", 0, 0, []byte(""))

	f.Fuzz(func(_ *testing.T, encKey []byte, v, length int, cfm string, objNum, genNum int, data []byte) {
		h := &Handler{EncryptionKey: encKey, V: v, CFM: cfm, Length: length}
		decrypt := h.DecryptFunc(objNum, genNum)
		// The AES branch decrypts in place, so pass a copy: the engine's
		// buffers must not be modified (see the aesDecrypt note above).
		_, _ = decrypt(append([]byte(nil), data...))
		_ = h.ObjKey(objNum, genNum)
	})
}
