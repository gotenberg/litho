package signature

import (
	"encoding/asn1"
	"fmt"
)

// oidCMSAttrSigningCertV1 is the ESS signing-certificate attribute (RFC
// 2634, 5.4), which names the signing certificate by its SHA-1 hash.
var oidCMSAttrSigningCertV1 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 12}

// CheckCAdES returns [ErrNotCAdES] when der, the SignedData a signer
// returned, cannot sit under an ETSI.CAdES.detached signature dictionary as a
// PAdES baseline signature: ETSI EN 319 142-1, 6.3, Table 1 has its first
// SignerInfo carry the content-type, message-digest and ESS
// signing-certificate or signing-certificate-v2 signed attributes and no
// signing-time, the claimed time being the signature dictionary's /M
// (Table 1, requirement g). Clause numbers are those of V1.2.1 (2024-01).
func CheckCAdES(der []byte) error {
	var ci contentInfo
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		return fmt.Errorf("%w: ContentInfo: %v", ErrSignatureMalformed, err)
	}
	var sd signedDataFull
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return fmt.Errorf("%w: SignedData: %v", ErrSignatureMalformed, err)
	}
	if len(sd.SignerInfos) == 0 {
		return fmt.Errorf("%w: no SignerInfo present", ErrSignatureMalformed)
	}
	attrs := sd.SignerInfos[0].SignedAttrs
	if len(attrs.Bytes) == 0 {
		return fmt.Errorf("%w: the SignerInfo has no signed attributes", ErrNotCAdES)
	}
	if hasAttribute(attrs, oidCMSAttrSigningTime) {
		return fmt.Errorf("%w: it carries a signing-time attribute (ETSI EN 319 142-1, 6.3, Table 1)", ErrNotCAdES)
	}
	for _, need := range []struct {
		name string
		oids []asn1.ObjectIdentifier
	}{
		{"content-type", []asn1.ObjectIdentifier{oidCMSAttrContentType}},
		{"message-digest", []asn1.ObjectIdentifier{oidAttrMessageDigest}},
		{"ESS signing-certificate-v2", []asn1.ObjectIdentifier{oidCMSAttrSigningCert, oidCMSAttrSigningCertV1}},
	} {
		found := false
		for _, oid := range need.oids {
			found = found || hasAttribute(attrs, oid)
		}
		if !found {
			return fmt.Errorf("%w: it lacks the %s attribute (ETSI EN 319 142-1, 6.3, Table 1)", ErrNotCAdES, need.name)
		}
	}
	return nil
}

// hasAttribute reports whether the SignedAttributes signedAttrs hold an
// attribute of type oid.
func hasAttribute(signedAttrs asn1.RawValue, oid asn1.ObjectIdentifier) bool {
	rest := signedAttrs.Bytes
	for len(rest) > 0 {
		var a attribute
		var err error
		if rest, err = asn1.Unmarshal(rest, &a); err != nil {
			return false
		}
		if a.Type.Equal(oid) {
			return true
		}
	}
	return false
}
