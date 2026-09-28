package signature

import (
	"encoding/asn1"
	"fmt"
	"slices"
)

// maxCMSElements bounds the elements read from one SignedData, SignerInfo or
// unsigned attributes SET. Anti-DoS guard, not spec-mandated: RFC 5652, 5.1
// and 5.3 give a SignedData six elements and a SignerInfo seven, and a
// SignerInfo carries a handful of unsigned attributes, timestamps and
// countersignatures.
const maxCMSElements = 64

// signerInfoParts is a CMS ContentInfo split around its first SignerInfo, so
// that SignerInfo can be rewritten and the whole encoded again.
type signerInfoParts struct {
	// signedData holds the SignedData's elements but its SignerInfos.
	signedData []asn1.RawValue
	// signerInfo holds the first SignerInfo's elements, and others the
	// SignerInfos after it.
	signerInfo []asn1.RawValue
	others     []byte
}

// splitSignerInfo splits the DER ContentInfo der around its first SignerInfo.
// BER, which a SignedData may use but DER does not, is refused with
// [ErrSignatureMalformed].
func splitSignerInfo(der []byte) (*signerInfoParts, error) {
	var ci contentInfo
	// What follows the ContentInfo is ignored: a /Contents string pads it with
	// zeros to its placeholder's length (ISO 32000-1, 12.8.1).
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		return nil, fmt.Errorf("%w: the SignedData is no DER ContentInfo", ErrSignatureMalformed)
	}
	if !ci.ContentType.Equal(oidCMSSignedData) {
		return nil, fmt.Errorf("%w: the ContentInfo holds a %v, not a SignedData", ErrSignatureMalformed, ci.ContentType)
	}
	var sd asn1.RawValue
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, fmt.Errorf("%w: SignedData: %v", ErrSignatureMalformed, err)
	}
	elems, err := rawElements(sd.Bytes)
	if err != nil {
		return nil, err
	}
	// RFC 5652, 5.1: the SignerInfos SET closes the SignedData.
	if len(elems) == 0 || elems[len(elems)-1].Tag != asn1.TagSet {
		return nil, fmt.Errorf("%w: the SignedData ends with no SignerInfos", ErrSignatureMalformed)
	}
	infos := elems[len(elems)-1]
	var first asn1.RawValue
	rest, err := asn1.Unmarshal(infos.Bytes, &first)
	if err != nil {
		return nil, fmt.Errorf("%w: no SignerInfo: %v", ErrSignatureMalformed, err)
	}
	siElems, err := rawElements(first.Bytes)
	if err != nil {
		return nil, err
	}
	return &signerInfoParts{signedData: elems[:len(elems)-1], signerInfo: siElems, others: rest}, nil
}

// rawElements returns the DER elements content holds, one after another.
func rawElements(content []byte) ([]asn1.RawValue, error) {
	var out []asn1.RawValue
	for rest := content; len(rest) > 0; {
		if len(out) == maxCMSElements {
			return nil, fmt.Errorf("%w: more than %d elements", ErrSignatureMalformed, maxCMSElements)
		}
		var v asn1.RawValue
		var err error
		if rest, err = asn1.Unmarshal(rest, &v); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSignatureMalformed, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// SignatureValue returns the contents octets of the signature field of the
// first SignerInfo of der, the value RFC 3161, Appendix A has a signature
// timestamp imprint.
func SignatureValue(der []byte) ([]byte, error) {
	parts, err := splitSignerInfo(der)
	if err != nil {
		return nil, err
	}
	// RFC 5652, 5.3: the signature is the one OCTET STRING at the top level
	// of a SignerInfo; the sid, the algorithms and the attributes are
	// SEQUENCEs, SETs and tagged elements.
	for _, e := range parts.signerInfo {
		if e.Class == asn1.ClassUniversal && e.Tag == asn1.TagOctetString {
			return e.Bytes, nil
		}
	}
	return nil, fmt.Errorf("%w: the SignerInfo holds no signature value", ErrSignatureMalformed)
}

// AddSignatureTimestamp returns der with token, an RFC 3161 TimeStampToken
// over its first SignerInfo's signature value, added to that SignerInfo's
// unsigned attributes as the id-aa-signatureTimeStampToken attribute (RFC
// 3161, Appendix A). The signed attributes and the signature are kept byte
// for byte, so the signature still verifies.
func AddSignatureTimestamp(der, token []byte) ([]byte, error) {
	parts, err := splitSignerInfo(der)
	if err != nil {
		return nil, err
	}
	oid, err := asn1.Marshal(oidAttrTimeStampToken)
	if err != nil {
		return nil, fmt.Errorf("encoding the timestamp attribute type: %w", err)
	}
	values, err := derWrap(asn1.ClassUniversal, asn1.TagSet, token)
	if err != nil {
		return nil, err
	}
	attr, err := derWrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(oid, values))
	if err != nil {
		return nil, err
	}

	// RFC 5652, 5.3: unsignedAttrs is the [1] IMPLICIT SET OF Attribute
	// closing the SignerInfo. An existing one gains the attribute; DER sorts
	// a SET OF (X.690, 11.6).
	var si []byte
	added := false
	for _, e := range parts.signerInfo {
		if e.Class == asn1.ClassContextSpecific && e.Tag == 1 {
			attrs, err := rawElements(e.Bytes)
			if err != nil {
				return nil, err
			}
			encoded := [][]byte{attr}
			for _, a := range attrs {
				encoded = append(encoded, a.FullBytes)
			}
			slices.SortFunc(encoded, compareDER)
			unsigned, err := derWrap(asn1.ClassContextSpecific, 1, slices.Concat(encoded...))
			if err != nil {
				return nil, err
			}
			si = append(si, unsigned...)
			added = true
			continue
		}
		si = append(si, e.FullBytes...)
	}
	if !added {
		unsigned, err := derWrap(asn1.ClassContextSpecific, 1, attr)
		if err != nil {
			return nil, err
		}
		si = append(si, unsigned...)
	}

	signerInfo, err := derWrap(asn1.ClassUniversal, asn1.TagSequence, si)
	if err != nil {
		return nil, err
	}
	infos, err := derWrap(asn1.ClassUniversal, asn1.TagSet, slices.Concat(signerInfo, parts.others))
	if err != nil {
		return nil, err
	}
	var sd []byte
	for _, e := range parts.signedData {
		sd = append(sd, e.FullBytes...)
	}
	signed, err := derWrap(asn1.ClassUniversal, asn1.TagSequence, append(sd, infos...))
	if err != nil {
		return nil, err
	}
	signedType, err := asn1.Marshal(oidCMSSignedData)
	if err != nil {
		return nil, fmt.Errorf("encoding the content type: %w", err)
	}
	content, err := derWrap(asn1.ClassContextSpecific, 0, signed)
	if err != nil {
		return nil, err
	}
	return derWrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(signedType, content))
}

// compareDER orders two DER encodings as X.690, 11.6 orders the elements of
// a SET OF: as octet strings, the shorter one padded with zeros.
func compareDER(a, b []byte) int {
	n := max(len(a), len(b))
	for i := range n {
		var x, y byte
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return int(x) - int(y)
		}
	}
	return 0
}
