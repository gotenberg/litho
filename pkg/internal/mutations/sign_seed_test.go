package mutations

import "testing"

// TestSeedValueFlags pins the /Ff bits of the seed value dictionaries to ISO
// 32000-2, Table 237 and ISO 32000-1, Table 235, as pyHanko's SigSeedValFlags
// and SigCertConstraintFlags and HexaPDF's seed value bit fields give them,
// the highest /V litho meets, and the ESIC levels of ETSI EN 319 142-1
// V1.2.1, 5.6.
func TestSeedValueFlags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		got, want int
	}{
		{"Filter", seedFilter, 1},
		{"SubFilter", seedSubFilter, 2},
		{"V", seedV, 4},
		{"Reasons", seedReasons, 8},
		{"LegalAttestation", seedLegalAttestation, 16},
		{"AddRevInfo", seedAddRevInfo, 32},
		{"DigestMethod", seedDigestMethod, 64},
		{"LockDocument", seedLockDocument, 128},
		{"AppearanceFilter", seedAppearanceFilter, 256},
		{"every defined bit", seedKnown, 511},
		{"Cert Subject", certSeedSubject, 1},
		{"Cert Issuer", certSeedIssuer, 2},
		{"Cert OID", certSeedOID, 4},
		{"Cert SubjectDN", certSeedSubjectDN, 8},
		{"Cert KeyUsage", certSeedKeyUsage, 32},
		{"highest /V", maxSeedVersion, 2},
		{"ESIC level of the DSS and document timestamps", ESICLevelLTV, 1},
		{"ESIC level of ETSI.CAdES.detached", ESICLevelCAdES, 2},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}
