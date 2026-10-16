package mutations

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// The /Ff bits of a seed value dictionary that make an entry a requirement
// (ISO 32000-1, 12.7.4.5, Table 234, bits 1 to 7; ISO 32000-2, 12.7.5.5,
// Table 237 adds bit 8 for LockDocument and bit 9 for AppearanceFilter). The
// values match pyHanko's SigSeedValFlags (LOCK_DOCUMENT 128,
// APPEARANCE_FILTER 256) and HexaPDF's seed value bit field (lock_document 7,
// appearance_filter 8, counted from 0).
const (
	seedFilter           = 1 << 0
	seedSubFilter        = 1 << 1
	seedV                = 1 << 2
	seedReasons          = 1 << 3
	seedLegalAttestation = 1 << 4
	seedAddRevInfo       = 1 << 5
	seedDigestMethod     = 1 << 6
	seedLockDocument     = 1 << 7
	seedAppearanceFilter = 1 << 8
	seedKnown            = 1<<9 - 1
)

// The /Ff bits of a certificate seed value dictionary (ISO 32000-1,
// 12.7.4.5, Table 235); bit 5 is reserved, and bit 7 (URL) asks nothing of
// the certificate. pyHanko's SigCertConstraintFlags and HexaPDF's bit field
// give the same values.
const (
	certSeedSubject   = 1 << 0
	certSeedIssuer    = 1 << 1
	certSeedOID       = 1 << 2
	certSeedSubjectDN = 1 << 3
	certSeedKeyUsage  = 1 << 5
)

// maxSeedVersion is the highest seed value dictionary /V litho reads every
// entry of: ISO 32000-1, Table 234 defines 1 for the PDF 1.5 entries and 2
// for all the table lists, and ISO 32000-2, Table 237 adds 3 for the PDF 2.0
// ones (pyHanko's SeedValueDictVersion: PDF_1_5 1, PDF_1_7 2, PDF_2_0 3).
// litho stays at 2: of the PDF 2.0 entries it reads LockDocument and
// AppearanceFilter, but not the SignaturePolicyOID, SignaturePolicyHashValue,
// SignaturePolicyHashAlgorithm and SignaturePolicyCommitmentType entries
// ISO 32000-2 adds to the certificate seed value dictionary (HexaPDF's
// SVCert types them PDF 2.0), and it writes no signature-policy-identifier
// attribute (ETSI EN 319 122-1, 5.2.9) to meet them.
const maxSeedVersion = 2

// maxLockDepth bounds the field tree [lockFields] walks. Anti-DoS guard, not
// spec-mandated: ISO 32000-1, 12.7.3.1 makes the fields a tree of a handful of
// levels in real forms, and the walk also visits each field once.
const maxLockDepth = 64

// applySeedValues reconciles info with sv, the seed value dictionary of the
// signature field named field (ISO 32000-1, 12.7.4.5, Table 234): an entry
// whose /Ff bit is set is a requirement the signature meets or the signing is
// refused with [ErrSeedValueUnmet], and litho follows the others when the
// caller asked nothing else. The signing certificate and the chain signer
// knows are checked against the certificate seed value dictionary (Table
// 235) the same way.
//
// What the signature can be is known here: the Adobe.PPKLite filter, the
// adbe.pkcs7.detached or ETSI.CAdES.detached subfilter, a SHA-256 digest, an
// approval signature without legal attestations or revocation information in
// its SignedData, and a timestamp from the authority the caller or the seed
// value names.
func applySeedValues(ctx context.Context, doc *document.Document, sv ast.PdfDict, info *SignatureInfo, signer Signer, field string) error {
	unmet := func(format string, args ...any) error {
		return fmt.Errorf("%w: field %q: %s", ErrSeedValueUnmet, field, fmt.Sprintf(format, args...))
	}
	ff := seedInt(ctx, doc, sv["Ff"])
	if info.DocTimeStamp {
		if ff != 0 {
			return unmet("its seed values (/SV /Ff %d) are for a signature, not a document timestamp", ff)
		}
		return nil
	}
	if extra := ff &^ seedKnown; extra != 0 {
		return unmet("it requires seed values of /Ff bits %#x, which neither ISO 32000-1, Table 234 nor ISO 32000-2 defines", extra)
	}

	if ff&seedFilter != 0 {
		if name, err := document.ResolveName(ctx, doc, sv["Filter"]); err == nil && name != "" && name != "Adobe.PPKLite" {
			return unmet("it requires the %s signature handler; litho signs with Adobe.PPKLite", name)
		}
	}
	if ff&seedV != 0 {
		if v := seedInt(ctx, doc, sv["V"]); v > maxSeedVersion {
			return unmet("it requires a seed value parser of version %d; litho reads the entries of version %d", v, maxSeedVersion)
		}
	}

	if subFilters := seedNames(ctx, doc, sv["SubFilter"]); len(subFilters) > 0 {
		supported := []ast.PdfName{SubFilterPKCS7Detached, SubFilterCAdESDetached}
		switch {
		case info.SubFilter != "":
			if ff&seedSubFilter != 0 && !slices.Contains(subFilters, info.SubFilter) {
				return unmet("it requires one of the subfilters %v, not the %s asked for", subFilters, info.SubFilter)
			}
		default:
			// Table 234: "The first name in the array that matches an
			// encoding supported by the signature handler shall be the one
			// used", and /Ff bit 2 makes the list binding. Bound, the
			// signature takes that first name or none: pyHanko's validator
			// reports any other as a seed value violation ("the standard
			// mandates that we take the first available subfilter"). Unbound,
			// the names are recommendations, so the signature falls back
			// through the others and then litho's default when a caller's
			// Signer, whose SignedData shows its form only once it signs,
			// cannot write the first (see [WriteWithSignature]).
			var acceptable []ast.PdfName
			for _, n := range subFilters {
				if slices.Contains(supported, n) && !slices.Contains(acceptable, n) {
					acceptable = append(acceptable, n)
				}
			}
			required := ff&seedSubFilter != 0
			switch {
			case len(acceptable) == 0 && required:
				return unmet("it requires one of the subfilters %v; litho writes %v", subFilters, supported)
			case required:
				acceptable = acceptable[:1]
			case !slices.Contains(acceptable, SubFilterPKCS7Detached):
				acceptable = append(acceptable, SubFilterPKCS7Detached)
			}
			info.SubFilter = acceptable[0]
			info.seedSubFilter = &seedSubFilterChoice{field: field, listed: subFilters, required: required, fallbacks: acceptable[1:]}
		}
	}

	if ff&seedDigestMethod != 0 {
		if methods := seedNames(ctx, doc, sv["DigestMethod"]); len(methods) > 0 && !slices.Contains(methods, "SHA256") {
			return unmet("it requires one of the digests %v; litho signs over SHA-256", methods)
		}
	}

	if reasons := seedStrings(ctx, doc, sv["Reasons"]); len(reasons) > 0 && ff&seedReasons != 0 {
		switch {
		case info.Reason == "":
			// A single empty reason asks for none (Table 234).
			info.Reason = reasons[0]
		case !slices.Contains(reasons, info.Reason):
			return unmet("it requires one of the reasons %q, not %q", reasons, info.Reason)
		}
	}

	if ff&seedLegalAttestation != 0 && len(seedStrings(ctx, doc, sv["LegalAttestation"])) > 0 {
		return unmet("it requires a legal attestation, which only a certification signature carries and litho writes approval signatures")
	}
	if ff&seedAddRevInfo != 0 {
		if add, ok := resolved(ctx, doc, sv["AddRevInfo"]).(ast.PdfBool); ok && bool(add) {
			return unmet("it requires revocation information in the SignedData (/AddRevInfo), which litho adds to the document security store instead (WithLTV)")
		}
	}
	if ff&seedLockDocument != 0 {
		if name, err := document.ResolveName(ctx, doc, sv["LockDocument"]); err == nil && name == "true" {
			return unmet("it requires the signature to lock the document (/LockDocument true), which only a certification signature does")
		}
	}
	if ff&seedAppearanceFilter != 0 {
		if _, ok := sv["AppearanceFilter"]; ok {
			return unmet("it requires a named appearance (/AppearanceFilter); litho keeps the field's appearance as it stands")
		}
	}
	// Table 234, MDP: a /P of 1 to 3 asks for a certification signature,
	// whatever /Ff says.
	if mdp, err := resolveDict(ctx, doc, sv["MDP"]); err == nil && mdp != nil {
		if p := seedInt(ctx, doc, mdp["P"]); p != 0 {
			return unmet("it requires a certification signature with /P %d; litho writes approval signatures", p)
		}
	}

	// Table 234, TimeStamp: the authority to timestamp with, required when
	// its /Ff is 1.
	if ts, err := resolveDict(ctx, doc, sv["TimeStamp"]); err == nil && ts != nil {
		url := ""
		if s, err := document.ResolveString(ctx, doc, ts["URL"]); err == nil {
			url = document.DecodePdfString(s)
		}
		if info.TimestampURL == "" {
			info.TimestampURL = url
		}
		if seedInt(ctx, doc, ts["Ff"])&1 != 0 && info.TimestampURL == "" {
			return unmet("it requires a signature timestamp and names no timestamp authority")
		}
	}

	if cert, err := resolveDict(ctx, doc, sv["Cert"]); err == nil && cert != nil {
		if err := checkCertSeed(ctx, doc, cert, signer); err != nil {
			return unmet("%v", err)
		}
	}
	return nil
}

// seedSubFilterChoice is the subfilter a seed value dictionary chose (ISO
// 32000-1, 12.7.4.5, Table 234): the field holding it, its /SubFilter list,
// whether /Ff bit 2 makes the list a requirement, and the acceptable
// subfilters, in preference order, left to fall back to, none when it does.
type seedSubFilterChoice struct {
	field     string
	listed    []ast.PdfName
	required  bool
	fallbacks []ast.PdfName
}

// fallBack returns the next acceptable subfilter once the signer could not
// write the one tried, cause saying why, and consumes it. With none left it
// returns the refusal: [ErrSeedValueUnmet] wrapping cause when the list is a
// requirement, cause alone otherwise.
func (c *seedSubFilterChoice) fallBack(cause error) (ast.PdfName, error) {
	if len(c.fallbacks) == 0 {
		if c.required {
			return "", fmt.Errorf("%w: field %q: its subfilters %v are required, which makes the first litho writes binding (ISO 32000-1, Table 234), and the signer cannot write it: %w", ErrSeedValueUnmet, c.field, c.listed, cause)
		}
		return "", cause
	}
	next := c.fallbacks[0]
	c.fallbacks = c.fallbacks[1:]
	return next, nil
}

// checkCertSeed checks the signing certificate against the requirements of
// the certificate seed value dictionary cert (ISO 32000-1, 12.7.4.5, Table
// 235), and returns what fails.
func checkCertSeed(ctx context.Context, doc *document.Document, cert ast.PdfDict, signer Signer) error {
	ff := seedInt(ctx, doc, cert["Ff"])
	leaf := signer.Certificate()
	if ff != 0 && leaf == nil {
		return fmt.Errorf("its certificate seed values (/Ff %d) name a signing certificate this signer does not show before signing", ff)
	}
	if ff&certSeedSubject != 0 {
		if subjects := seedBytes(ctx, doc, cert["Subject"]); len(subjects) > 0 &&
			!slices.ContainsFunc(subjects, func(s string) bool { return bytes.Equal([]byte(s), leaf.Raw) }) {
			return fmt.Errorf("the signing certificate %q is none of the %d it accepts (/Cert /Subject)", leaf.Subject.CommonName, len(subjects))
		}
	}
	if ff&certSeedIssuer != 0 {
		if issuers := seedBytes(ctx, doc, cert["Issuer"]); len(issuers) > 0 && !issuedByOneOf(leaf, signer.Chain(), issuers) {
			return fmt.Errorf("the chain of %q holds none of the %d issuers it accepts (/Cert /Issuer)", leaf.Subject.CommonName, len(issuers))
		}
	}
	if ff&certSeedOID != 0 {
		if oids := seedBytes(ctx, doc, cert["OID"]); len(oids) > 0 && !hasPolicy(leaf, oids) {
			return fmt.Errorf("the signing certificate %q carries none of the policies %q (/Cert /OID)", leaf.Subject.CommonName, oids)
		}
	}
	if ff&certSeedSubjectDN != 0 {
		if dns, err := resolveArray(ctx, doc, cert["SubjectDN"]); err == nil && len(dns) > 0 {
			ok, err := subjectDNMatches(ctx, doc, leaf, dns)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("the subject of %q matches none of the names it accepts (/Cert /SubjectDN)", leaf.Subject.CommonName)
			}
		}
	}
	if ff&certSeedKeyUsage != 0 {
		if usages := seedStrings(ctx, doc, cert["KeyUsage"]); len(usages) > 0 &&
			!slices.ContainsFunc(usages, func(u string) bool { return keyUsageMatches(leaf.KeyUsage, u) }) {
			return fmt.Errorf("the key usage of %q matches none of %q (/Cert /KeyUsage)", leaf.Subject.CommonName, usages)
		}
	}
	// /URL names where to get an acceptable certificate (Table 235), which
	// asks nothing of the certificate itself.
	return nil
}

// issuedByOneOf reports whether one of the DER certificates issuers is in the
// chain of leaf, or issued leaf or a certificate of chain.
func issuedByOneOf(leaf *x509.Certificate, chain []*x509.Certificate, issuers []string) bool {
	certs := append([]*x509.Certificate{leaf}, chain...)
	for _, raw := range issuers {
		issuer, err := x509.ParseCertificate([]byte(raw))
		if err != nil {
			continue
		}
		for _, c := range certs {
			if bytes.Equal(c.Raw, issuer.Raw) || c.CheckSignatureFrom(issuer) == nil {
				return true
			}
		}
	}
	return false
}

// hasPolicy reports whether cert carries one of the certificate policies
// oids, each dotted text or DER.
func hasPolicy(cert *x509.Certificate, oids []string) bool {
	for _, s := range oids {
		oid, ok := parseOID(s)
		if !ok {
			continue
		}
		if slices.ContainsFunc(cert.PolicyIdentifiers, oid.Equal) {
			return true
		}
	}
	return false
}

// parseOID reads s as a dotted OID, or failing that as a DER one with or
// without its tag and length.
func parseOID(s string) (asn1.ObjectIdentifier, bool) {
	var oid asn1.ObjectIdentifier
	if parts := strings.Split(s, "."); len(parts) >= 2 {
		for _, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 {
				oid = nil
				break
			}
			oid = append(oid, n)
		}
		if oid != nil {
			return oid, true
		}
	}
	if _, err := asn1.Unmarshal([]byte(s), &oid); err == nil {
		return oid, true
	}
	if der, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagOID, Bytes: []byte(s)}); err == nil {
		if _, err := asn1.Unmarshal(der, &oid); err == nil {
			return oid, true
		}
	}
	return nil, false
}

// dnAttributes maps the attribute names a /SubjectDN dictionary uses to their
// OIDs (RFC 5280, 4.1.2.4 and Appendix A).
var dnAttributes = map[string]asn1.ObjectIdentifier{
	"CN": {2, 5, 4, 3}, "SERIALNUMBER": {2, 5, 4, 5}, "C": {2, 5, 4, 6}, "L": {2, 5, 4, 7},
	"ST": {2, 5, 4, 8}, "O": {2, 5, 4, 10}, "OU": {2, 5, 4, 11}, "T": {2, 5, 4, 12},
	"E": {1, 2, 840, 113549, 1, 9, 1}, "EMAIL": {1, 2, 840, 113549, 1, 9, 1},
	"DC": {0, 9, 2342, 19200300, 100, 1, 25},
}

// subjectDNMatches reports whether the subject of cert holds every attribute
// of one of the dictionaries dns (Table 235, SubjectDN). An attribute named
// neither by a name litho knows nor by a dotted OID is an error.
func subjectDNMatches(ctx context.Context, doc *document.Document, cert *x509.Certificate, dns ast.PdfArray) (bool, error) {
	for _, obj := range dns {
		dn, err := resolveDict(ctx, doc, obj)
		if err != nil || dn == nil {
			continue
		}
		all := true
		for _, key := range slices.Sorted(maps.Keys(dn)) {
			oid, ok := dnAttributes[strings.ToUpper(string(key))]
			if !ok {
				if oid, ok = parseOID(string(key)); !ok {
					return false, fmt.Errorf("its /Cert /SubjectDN names the attribute %s, which litho cannot check", key)
				}
			}
			want := ""
			if s, err := document.ResolveString(ctx, doc, dn[key]); err == nil {
				want = document.DecodePdfString(s)
			}
			found := false
			for _, n := range cert.Subject.Names {
				if v, ok := n.Value.(string); ok && n.Type.Equal(oid) && v == want {
					found = true
				}
			}
			all = all && found
		}
		if all {
			return true, nil
		}
	}
	return false, nil
}

// keyUsageMatches reports whether usage satisfies pattern, nine characters
// for digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment,
// keyAgreement, keyCertSign, cRLSign, encipherOnly and decipherOnly, each 1
// (must be set), 0 (must not be) or X (either) (Table 235). The bits of
// [x509.KeyUsage] run in the same order.
func keyUsageMatches(usage x509.KeyUsage, pattern string) bool {
	for i, c := range pattern {
		set := usage&(1<<i) != 0
		switch c {
		case '1':
			if !set {
				return false
			}
		case '0':
			if set {
				return false
			}
		}
	}
	return true
}

// lockFields turns read-only the fields lock, the /Lock of the signature
// field being signed, names (ISO 32000-1, 12.7.4.5, Table 233), by setting
// the ReadOnly bit of the /Ff (Table 221) of each terminal field among them in
// alloc, and returns the FieldMDP
// signature reference dictionary (12.8.2.4, Tables 253 and 256) that records
// the lock in the signature. fields are the form's root fields, and
// catalogRef names the catalog the transform's /Data refers to.
//
// # Corpus evidence
//
//   - All corpora: 72 signature fields, 8 of them with a /Lock (pdfjs
//     issue15813.pdf and issue8047.pdf, and six verapdf 6-1-12 and 6-1-11
//     files), every one signed already; none carries an /SV. The unit tests
//     are what reach an unsigned locked or seeded field.
func lockFields(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fields ast.PdfArray, lock ast.PdfDict, catalogRef ast.PdfIndirectRef) (ast.PdfDict, error) {
	action, err := document.ResolveName(ctx, doc, lock["Action"])
	if err != nil {
		action = ""
	}
	var names []string
	switch action {
	case "All":
	case "Include", "Exclude":
		names = seedStrings(ctx, doc, lock["Fields"])
	default:
		return nil, fmt.Errorf("%w: its /Lock names the action %q, not All, Include or Exclude (ISO 32000-1, Table 233)", ErrSignatureFieldUnavailable, string(action))
	}

	type node struct {
		obj    ast.PdfObject
		prefix string
		named  bool // the node or an ancestor is in names
	}
	visited := make(map[int]bool)
	stack := make([]node, 0, len(fields))
	for _, f := range slices.Backward(fields) {
		stack = append(stack, node{obj: f})
	}
	for steps := 0; len(stack) > 0; steps++ {
		if steps%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("locking fields: %w", err)
			}
		}
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		ref, isRef := top.obj.(ast.PdfIndirectRef)
		if isRef {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}
		field, err := resolveDictAllocFirst(ctx, alloc, doc, top.obj)
		if err != nil || field == nil {
			continue
		}
		t, hasT := field["T"]
		name := top.prefix
		if hasT {
			if s, err := document.ResolveString(ctx, doc, t); err == nil && s != "" {
				if partial := document.DecodePdfString(s); name == "" {
					name = partial
				} else {
					name += "." + partial
				}
			}
		}
		named := top.named || (hasT && slices.Contains(names, name))
		locked := action == "All" || (action == "Include" && named) || (action == "Exclude" && !named)
		// /Ff is inheritable (ISO 32000-1, 12.7.3.1, Table 220), so a
		// non-terminal field's ReadOnly bit reaches every kid that sets no
		// /Ff of its own, an Exclude-named kid among them. Each terminal
		// field takes the bit itself instead.
		if hasT && locked && !hasKidFields(ctx, alloc, doc, field) {
			if !isRef {
				doc.Logger.Debug("signing: a locked field written directly cannot be updated", slog.String("field", name))
			} else {
				updated := maps.Clone(field)
				// Table 221: bit 1 of /Ff is ReadOnly.
				updated["Ff"] = ast.PdfInt(seedInt(ctx, doc, field["Ff"]) | 1)
				alloc.AllocAt(ref.ObjectNum, updated)
			}
		}
		if !hasT && top.prefix != "" {
			// A kid with no partial name is a widget of its parent field
			// (ISO 32000-1, 12.7.3.1), and the walk ends there.
			continue
		}
		if strings.Count(name, ".") >= maxLockDepth {
			continue
		}
		kidsObj, ok := field["Kids"]
		if !ok || document.VisitedBefore(kidsObj, visited) {
			continue
		}
		kids, err := resolveArrayAllocFirst(ctx, alloc, doc, kidsObj)
		if err != nil {
			continue
		}
		for _, kid := range slices.Backward(kids) {
			stack = append(stack, node{obj: kid, prefix: name, named: named})
		}
	}

	params := ast.PdfDict{
		"Type":   ast.PdfName("TransformParams"),
		"Action": action,
		"V":      ast.PdfName("1.2"),
	}
	if action != "All" {
		params["Fields"] = lock["Fields"]
	}
	// ISO 32000-2 gives the lock a /P, the access permissions granted after
	// signing, which the FieldMDP transform carries.
	if p, ok := lock["P"]; ok {
		params["P"] = p
	}
	return ast.PdfDict{
		"Type":            ast.PdfName("SigRef"),
		"TransformMethod": ast.PdfName("FieldMDP"),
		"TransformParams": params,
		"Data":            catalogRef,
	}, nil
}

// hasKidFields reports whether field is a non-terminal field: a kid of it
// carries a partial name, where a widget kid carries none (ISO 32000-1,
// 12.7.3.1).
func hasKidFields(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, field ast.PdfDict) bool {
	kidsObj, ok := field["Kids"]
	if !ok {
		return false
	}
	kids, err := resolveArrayAllocFirst(ctx, alloc, doc, kidsObj)
	if err != nil {
		return false
	}
	for _, kid := range kids {
		if d, err := resolveDictAllocFirst(ctx, alloc, doc, kid); err == nil && d != nil {
			if _, ok := d["T"]; ok {
				return true
			}
		}
	}
	return false
}

// seedInt returns obj as an integer, 0 when it is none.
func seedInt(ctx context.Context, doc *document.Document, obj ast.PdfObject) int {
	if n, ok := resolved(ctx, doc, obj).(ast.PdfInt); ok {
		return int(n)
	}
	return 0
}

// seedNames returns the names of the array obj.
func seedNames(ctx context.Context, doc *document.Document, obj ast.PdfObject) []ast.PdfName {
	arr, err := resolveArray(ctx, doc, obj)
	if err != nil {
		return nil
	}
	var out []ast.PdfName
	for _, e := range arr {
		if n, err := document.ResolveName(ctx, doc, e); err == nil && n != "" {
			out = append(out, n)
		}
	}
	return out
}

// seedStrings returns the strings of the array obj, text strings decoded.
func seedStrings(ctx context.Context, doc *document.Document, obj ast.PdfObject) []string {
	arr, err := resolveArray(ctx, doc, obj)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range arr {
		if s, err := document.ResolveString(ctx, doc, e); err == nil {
			out = append(out, document.DecodePdfString(s))
		}
	}
	return out
}

// seedBytes returns the byte strings of the array obj as they stand, for the
// entries Table 235 types as byte strings: DER certificates and OIDs.
func seedBytes(ctx context.Context, doc *document.Document, obj ast.PdfObject) []string {
	arr, err := resolveArray(ctx, doc, obj)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range arr {
		if s, err := document.ResolveString(ctx, doc, e); err == nil {
			out = append(out, string(s))
		}
	}
	return out
}

// resolved returns obj with a reference chased, nil when it cannot be.
func resolved(ctx context.Context, doc *document.Document, obj ast.PdfObject) ast.PdfObject {
	if obj == nil {
		return nil
	}
	v, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil
	}
	return v
}

// resolveDict resolves obj to a dictionary, nil when absent.
func resolveDict(ctx context.Context, doc *document.Document, obj ast.PdfObject) (ast.PdfDict, error) {
	if obj == nil {
		return nil, nil
	}
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving a seed value dictionary: %w", err)
	}
	return d, nil
}

// resolveArray resolves obj to an array, nil when absent.
func resolveArray(ctx context.Context, doc *document.Document, obj ast.PdfObject) (ast.PdfArray, error) {
	if obj == nil {
		return nil, nil
	}
	a, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving a seed value array: %w", err)
	}
	return a, nil
}
