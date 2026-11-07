package offcrypto

import (
	"context"
	"encoding/binary"
	"strconv"
	"testing"
)

// FuzzReadCFB reads arbitrary bytes as a compound file and looks up the
// streams OpenOOXML reads: no input may panic or loop.
func FuzzReadCFB(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		cf, err := readCFB(context.Background(), data)
		if err != nil {
			return
		}
		for _, name := range []string{"EncryptionInfo", "EncryptedPackage", "WordDocument", "Workbook"} {
			if _, _, err := cf.stream(name); err != nil {
				return
			}
		}
		if err := classifyBinary(cf); err == nil {
			t.Fatal("classifyBinary returned no error")
		}
	})
}

// fuzzMaxSpin keeps the fuzzer's password checks short: a descriptor asking
// for more is parsed but not decrypted.
const fuzzMaxSpin = 1000

// FuzzParseEncryptionInfo parses arbitrary EncryptionInfo streams and, when
// one parses, decrypts a short package with it.
func FuzzParseEncryptionInfo(f *testing.F) {
	f.Fuzz(func(t *testing.T, info, pkg []byte) {
		s, err := parseEncryptionInfo(info)
		if err != nil {
			return
		}
		if a, ok := s.(*agileScheme); ok && a.spinCount > fuzzMaxSpin {
			return
		}
		out, err := s.decrypt(context.Background(), pkg, "pw")
		if err == nil && len(pkg) >= 8 && uint64(len(out)) != binary.LittleEndian.Uint64(pkg) {
			t.Fatalf("decrypted %d bytes, the package declares %d", len(out), binary.LittleEndian.Uint64(pkg))
		}
	})
}

// FuzzOpenODFEntry decrypts arbitrary entries with arbitrary manifest
// attributes, keeping the key derivation cheap.
func FuzzOpenODFEntry(f *testing.F) {
	f.Fuzz(func(t *testing.T, stored []byte, algorithm, iv, startKey, kdf, iterations, salt, keySize, checksumType, checksum, size string) {
		p := ODFParams{
			AlgorithmName: algorithm, InitialisationVector: iv, StartKeyGenerationName: startKey,
			KeyDerivationName: kdf, IterationCount: iterations, Salt: salt, KeySize: keySize,
			ChecksumType: checksumType, Checksum: checksum, Size: size,
			Argon2Iterations: "1", Argon2Memory: "8", Argon2Lanes: "1",
		}
		if n, err := strconv.Atoi(iterations); err == nil && n > fuzzMaxSpin {
			return
		}
		out, err := OpenODFEntry(context.Background(), stored, p, "pw")
		if err != nil {
			return
		}
		if n, perr := strconv.ParseInt(size, 10, 64); perr == nil && int64(len(out)) != n {
			t.Fatalf("entry of %d bytes, manifest:size %d", len(out), n)
		}
	})
}
