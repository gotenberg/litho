package models

import "testing"

func TestSignature_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Signature{
		FieldName:    "Signature1",
		ByteRange:    [4]int64{0, 100, 200, 300},
		Contents:     "DEADBEEF",
		DocumentHash: "abc123",
		SigningTime:  "D:20260101000000+00'00",
		MDPPolicy:    1,
	})
}

func TestSignatureValidation_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, SignatureValidation{
		FieldName:       "Signature1",
		HashValid:       true,
		CertChainValid:  true,
		SignerCN:        "Alice",
		Issuer:          "CA",
		NotBefore:       "2025-01-01",
		NotAfter:        "2027-01-01",
		SigningTime:     "2025-06-01",
		TimestampValid:  true,
		ErrorMessage:    "none",
		HasDSS:          true,
		HasTimestamp:    true,
		TimestampTime:   "2025-06-01",
		TimestampIssuer: "TSA",
		LTVEnabled:      true,
		RevocationOK:    true,
		RevocationInfo:  "ok",
		MDPPolicy:       2,
		MDPValid:        true,
		MDPInfo:         "ok",
	})
}

func TestDSSInfo_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, DSSInfo{CertCount: 3, CRLCount: 1, OCSPCount: 2, VRICount: 1})
}
