package pkitest

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6960 CertIDs name issuers by SHA-1; nothing here is signed with it.
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

// OIDs the servers write.
var (
	oidSHA256          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidSignedData      = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidTSTInfo         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}
	oidContentType     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidMessageDigest   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSigningCertV2   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}
	oidOCSPBasic       = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 1}
	oidTSAPolicy       = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 1}
)

// maxRequestBytes bounds the request bodies the servers read. A test server
// answers requests litho builds, a few hundred bytes each.
const maxRequestBytes = 1 << 16

// TSAMode names how the timestamp authority answers.
type TSAMode int

// The timestamp authority's answers.
const (
	// TSAGrant grants each request with a token over its imprint.
	TSAGrant TSAMode = iota
	// TSAReject answers with the rejection status.
	TSAReject
	// TSAWrongNonce grants with a token carrying another nonce.
	TSAWrongNonce
	// TSAWrongImprint grants with a token over another digest.
	TSAWrongImprint
	// TSALargeToken grants with a token padded past 8 KiB by a long
	// certificate list, to outgrow the room a signature reserves for it.
	TSALargeToken
)

// PKI is a certificate hierarchy and the servers that vouch for it. Root
// issues Intermediate and the TSA certificate, and Intermediate issues Leaf.
// Leaf, Intermediate and TSA name the OCSP responder in their authority
// information access and a CRL in their distribution points; Leaf and
// Intermediate also name where their issuer is fetched from. A PKI is safe
// for concurrent use.
type PKI struct {
	Root, Intermediate, Leaf, TSA             *x509.Certificate
	RootKey, IntermediateKey, LeafKey, TSAKey *ecdsa.PrivateKey

	report func(format string, args ...any)
	server *httptest.Server

	mu       sync.Mutex
	tsaMode  TSAMode
	ocspDown bool
	revoked  map[string]time.Time // by serial number
	requests map[string]int       // by path
	serial   int64
}

// New starts a PKI whose server stops when t ends, and that reports what its
// servers fail at as errors of t. The certificates are valid from a year
// before now to ten years after.
func New(t testing.TB) *PKI {
	t.Helper()
	p, err := Start(t.Errorf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// Start starts a PKI outside a test, as [New] does: report receives what its
// servers fail at, and [PKI.Close] stops them.
func Start(report func(format string, args ...any)) (*PKI, error) {
	p := &PKI{report: report, revoked: make(map[string]time.Time), requests: make(map[string]int), serial: 100}
	mux := http.NewServeMux()
	mux.HandleFunc("/tsa", p.serveTSA)
	mux.HandleFunc("/ocsp", p.serveOCSP)
	mux.HandleFunc("/crl/", p.serveCRL)
	mux.HandleFunc("/cert/", p.serveCert)
	p.server = httptest.NewServer(p.count(mux))

	if err := p.issueAll(); err != nil {
		p.server.Close()
		return nil, err
	}
	return p, nil
}

// Close stops the PKI's server.
func (p *PKI) Close() { p.server.Close() }

// issueAll makes the keys and certificates of the hierarchy.
func (p *PKI) issueAll() error {
	var firstErr error
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	key := func() *ecdsa.PrivateKey {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			fail(err)
		}
		return k
	}
	p.RootKey, p.IntermediateKey, p.LeafKey, p.TSAKey = key(), key(), key(), key()
	if firstErr != nil {
		return firstErr
	}
	notBefore := time.Now().AddDate(-1, 0, 0).UTC().Truncate(time.Second)
	notAfter := notBefore.AddDate(11, 0, 0)
	issue := func(tmpl, parent *x509.Certificate, pub crypto.PublicKey, parentKey crypto.Signer) *x509.Certificate {
		if firstErr != nil {
			return nil
		}
		tmpl.NotBefore, tmpl.NotAfter = notBefore, notAfter
		// RFC 5280, 4.2.1.2, method (1): the SHA-1 of the public key bits,
		// which OCSP clients match a byKey ResponderID against; crypto/x509
		// derives it from SHA-256 instead.
		spki, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			fail(err)
			return nil
		}
		var info struct {
			Algorithm pkix.AlgorithmIdentifier
			PublicKey asn1.BitString
		}
		if _, err := asn1.Unmarshal(spki, &info); err != nil {
			fail(err)
			return nil
		}
		keyID := sha1.Sum(info.PublicKey.Bytes) //nolint:gosec // RFC 5280, 4.2.1.2 key identifier.
		tmpl.SubjectKeyId = keyID[:]
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, parentKey)
		if err != nil {
			fail(fmt.Errorf("issuing %s: %w", tmpl.Subject.CommonName, err))
			return nil
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			fail(err)
			return nil
		}
		return cert
	}
	url := p.server.URL
	rootTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "pkitest root"},
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true,
	}
	p.Root = issue(rootTmpl, rootTmpl, p.RootKey.Public(), p.RootKey)
	if p.Root == nil {
		return firstErr
	}
	p.Intermediate = issue(&x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "pkitest intermediate"},
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true,
		OCSPServer: []string{url + "/ocsp"}, CRLDistributionPoints: []string{url + "/crl/root"},
		IssuingCertificateURL: []string{url + "/cert/root"},
	}, p.Root, p.IntermediateKey.Public(), p.RootKey)
	p.Leaf = issue(&x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "pkitest signer"},
		KeyUsage:   x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment,
		OCSPServer: []string{url + "/ocsp"}, CRLDistributionPoints: []string{url + "/crl/intermediate"},
		IssuingCertificateURL: []string{url + "/cert/intermediate"},
	}, p.Intermediate, p.LeafKey.Public(), p.IntermediateKey)
	p.TSA = issue(&x509.Certificate{
		SerialNumber: big.NewInt(4), Subject: pkix.Name{CommonName: "pkitest timestamp authority"},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
		// RFC 3161, 2.3 makes the timeStamping extended key usage critical,
		// which crypto/x509 leaves it not.
		ExtraExtensions: []pkix.Extension{{
			Id: asn1.ObjectIdentifier{2, 5, 29, 37}, Critical: true,
			Value: p.der([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 8}}),
		}},
		OCSPServer: []string{url + "/ocsp"}, CRLDistributionPoints: []string{url + "/crl/root"},
		IssuingCertificateURL: []string{url + "/cert/root"},
	}, p.Root, p.TSAKey.Public(), p.RootKey)
	return firstErr
}

// URL returns the base URL of the PKI's server.
func (p *PKI) URL() string { return p.server.URL }

// TSAURL returns the URL of the timestamp authority.
func (p *PKI) TSAURL() string { return p.server.URL + "/tsa" }

// Roots returns a pool trusting the root.
func (p *PKI) Roots() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(p.Root)
	return pool
}

// SetTSAMode sets how the timestamp authority answers.
func (p *PKI) SetTSAMode(m TSAMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tsaMode = m
}

// SetOCSPDown has the OCSP responder answer every request with a server
// error, so a client falls back to the CRLs.
func (p *PKI) SetOCSPDown(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ocspDown = down
}

// Revoke has the OCSP responder and the CRLs report cert revoked at at.
func (p *PKI) Revoke(cert *x509.Certificate, at time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revoked[cert.SerialNumber.String()] = at
}

// Requests returns how many requests the server answered on path, such as
// "/tsa" or "/ocsp".
func (p *PKI) Requests(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[path]
}

// count records each request's path before serving it.
func (p *PKI) count(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.requests[r.URL.Path]++
		p.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// nextSerial returns a fresh serial number.
func (p *PKI) nextSerial() *big.Int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.serial++
	return big.NewInt(p.serial)
}

// messageImprint is the MessageImprint of RFC 3161, 2.4.1.
type messageImprint struct {
	HashAlgorithm pkix.AlgorithmIdentifier
	HashedMessage []byte
}

// timeStampReq is the TimeStampReq of RFC 3161, 2.4.1.
type timeStampReq struct {
	Version        int
	MessageImprint messageImprint
	ReqPolicy      asn1.ObjectIdentifier `asn1:"optional"`
	Nonce          *big.Int              `asn1:"optional"`
	CertReq        bool                  `asn1:"optional"`
}

// tstInfo is the TSTInfo of RFC 3161, 2.4.2, without the optional fields
// the authority does not write.
type tstInfo struct {
	Version        int
	Policy         asn1.ObjectIdentifier
	MessageImprint messageImprint
	SerialNumber   *big.Int
	GenTime        time.Time `asn1:"generalized"`
	Nonce          *big.Int  `asn1:"optional"`
}

// serveTSA answers an RFC 3161 request over HTTP (RFC 3161, 3.4).
func (p *PKI) serveTSA(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req timeStampReq
	if _, err := asn1.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	mode := p.tsaMode
	p.mu.Unlock()

	w.Header().Set("Content-Type", "application/timestamp-reply")
	if mode == TSAReject {
		// PKIStatusInfo { rejection (2), statusString } (RFC 3161, 2.4.2).
		text, err := asn1.MarshalWithParams("refused by pkitest", "utf8")
		if err != nil {
			p.report("pkitest: %v", err)
		}
		status := p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(p.der(2), p.wrap(asn1.ClassUniversal, asn1.TagSequence, text)))
		p.write(w, p.wrap(asn1.ClassUniversal, asn1.TagSequence, status))
		return
	}
	imprint := req.MessageImprint
	nonce := req.Nonce
	switch mode {
	case TSAWrongNonce:
		nonce = new(big.Int).Add(nonce, big.NewInt(1))
	case TSAWrongImprint:
		imprint.HashedMessage = bytes.Repeat([]byte{0xAB}, len(imprint.HashedMessage))
	case TSAGrant, TSAReject, TSALargeToken:
	}
	tst := p.der(tstInfo{
		Version: 1, Policy: oidTSAPolicy, MessageImprint: imprint, SerialNumber: p.nextSerial(),
		GenTime: time.Now().UTC().Truncate(time.Second), Nonce: nonce,
	})
	certs := []*x509.Certificate{p.TSA}
	if mode == TSALargeToken {
		for range 20 {
			certs = append(certs, p.Root, p.Intermediate)
		}
	}
	token := p.signedData(oidTSTInfo, tst, p.TSA, p.TSAKey, certs, req.CertReq)
	status := p.der(struct{ Status int }{0})
	p.write(w, p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(status, token)))
}

// signedData returns a ContentInfo holding a SignedData that encapsulates
// content of type contentType, signed by cert with key under signed
// content-type, message-digest and ESS signing-certificate-v2 attributes, with
// certs in its certificates field when withCerts is set.
func (p *PKI) signedData(contentType asn1.ObjectIdentifier, content []byte, cert *x509.Certificate, key crypto.Signer, certs []*x509.Certificate, withCerts bool) []byte {
	digest := sha256.Sum256(content)
	certHash := sha256.Sum256(cert.Raw)
	issuerSerial := p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(
		p.wrap(asn1.ClassUniversal, asn1.TagSequence, p.wrap(asn1.ClassContextSpecific, 4, cert.RawIssuer)),
		p.der(cert.SerialNumber)))
	essCert := p.wrap(asn1.ClassUniversal, asn1.TagSequence, p.wrap(asn1.ClassUniversal, asn1.TagSequence,
		p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(p.der(certHash[:]), issuerSerial))))
	attr := func(oid asn1.ObjectIdentifier, value []byte) []byte {
		return p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(p.der(oid), p.wrap(asn1.ClassUniversal, asn1.TagSet, value)))
	}
	attrs := [][]byte{
		attr(oidContentType, p.der(contentType)),
		attr(oidMessageDigest, p.der(digest[:])),
		attr(oidSigningCertV2, essCert),
	}
	slices.SortFunc(attrs, bytes.Compare)
	attrSet := p.wrap(asn1.ClassUniversal, asn1.TagSet, slices.Concat(attrs...))
	h := sha256.Sum256(attrSet)
	sig, err := key.Sign(rand.Reader, h[:], crypto.SHA256)
	if err != nil {
		p.report("pkitest: signing: %v", err)
	}
	signedAttrs := slices.Clone(attrSet)
	signedAttrs[0] = 0xA0 // [0] IMPLICIT, constructed

	sha256Alg := p.der(pkix.AlgorithmIdentifier{Algorithm: oidSHA256})
	sigAlg := p.der(pkix.AlgorithmIdentifier{Algorithm: oidECDSAWithSHA256})
	sid := p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(cert.RawIssuer, p.der(cert.SerialNumber)))
	signerInfo := p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(p.der(1), sid, sha256Alg, signedAttrs, sigAlg, p.der(sig)))

	encap := p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(p.der(contentType),
		p.wrap(asn1.ClassContextSpecific, 0, p.der(content))))
	var raw []byte
	for _, c := range certs {
		raw = append(raw, c.Raw...)
	}
	body := slices.Concat(p.der(3), p.wrap(asn1.ClassUniversal, asn1.TagSet, sha256Alg), encap)
	if withCerts {
		body = append(body, p.wrap(asn1.ClassContextSpecific, 0, raw)...)
	}
	body = append(body, p.wrap(asn1.ClassUniversal, asn1.TagSet, signerInfo)...)
	return p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(p.der(oidSignedData),
		p.wrap(asn1.ClassContextSpecific, 0, p.wrap(asn1.ClassUniversal, asn1.TagSequence, body))))
}

// certID is the CertID of RFC 6960, 4.1.1.
type certID struct {
	HashAlgorithm  pkix.AlgorithmIdentifier
	IssuerNameHash []byte
	IssuerKeyHash  []byte
	SerialNumber   *big.Int
}

// ocspRequest is the OCSPRequest of RFC 6960, 4.1.1, without the optional
// signature.
type ocspRequest struct {
	TBSRequest struct {
		Version       int           `asn1:"optional,explicit,tag:0"`
		RequestorName asn1.RawValue `asn1:"optional,explicit,tag:1"`
		RequestList   []struct {
			ReqCert    certID
			Extensions asn1.RawValue `asn1:"optional,explicit,tag:0"`
		}
		Extensions asn1.RawValue `asn1:"optional,explicit,tag:2"`
	}
}

// serveOCSP answers an RFC 6960 request POSTed to it (RFC 6960, A.1), for a
// certificate the root or the intermediate issued, with a response the
// issuer signs itself.
func (p *PKI) serveOCSP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	down := p.ocspDown
	p.mu.Unlock()
	if down {
		http.Error(w, "pkitest: OCSP responder down", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req ocspRequest
	if _, err := asn1.Unmarshal(body, &req); err != nil || len(req.TBSRequest.RequestList) == 0 {
		http.Error(w, "no OCSP request", http.StatusBadRequest)
		return
	}
	id := req.TBSRequest.RequestList[0].ReqCert
	var issuer *x509.Certificate
	var issuerKey crypto.Signer
	for _, c := range []struct {
		cert *x509.Certificate
		key  crypto.Signer
	}{{p.Root, p.RootKey}, {p.Intermediate, p.IntermediateKey}} {
		nameHash := sha1.Sum(c.cert.RawSubject)      //nolint:gosec // RFC 6960, 4.1.1 CertID hash.
		keyHash := sha1.Sum(p.publicKeyBits(c.cert)) //nolint:gosec // RFC 6960, 4.1.1 CertID hash.
		if bytes.Equal(id.IssuerNameHash, nameHash[:]) && bytes.Equal(id.IssuerKeyHash, keyHash[:]) {
			issuer, issuerKey = c.cert, c.key
		}
	}
	w.Header().Set("Content-Type", "application/ocsp-response")
	if issuer == nil {
		// unauthorized (6), RFC 6960, 2.3.
		p.write(w, p.der(struct{ Status asn1.Enumerated }{6}))
		return
	}

	now := time.Now().UTC().Truncate(time.Second)
	p.mu.Lock()
	revokedAt, revoked := p.revoked[id.SerialNumber.String()]
	p.mu.Unlock()
	status := asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0}
	if revoked {
		status = asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 1, IsCompound: true, Bytes: p.generalized(revokedAt)}
	}
	single := p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(
		p.der(id), p.der(status), p.generalized(now),
		p.wrap(asn1.ClassContextSpecific, 0, p.generalized(now.Add(24*time.Hour)))))
	keyHash := sha1.Sum(p.publicKeyBits(issuer)) //nolint:gosec // RFC 6960, 4.2.1 ResponderID byKey.
	tbs := p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(
		p.wrap(asn1.ClassContextSpecific, 2, p.der(keyHash[:])),
		p.generalized(now),
		p.wrap(asn1.ClassUniversal, asn1.TagSequence, single)))
	h := sha256.Sum256(tbs)
	sig, err := issuerKey.Sign(rand.Reader, h[:], crypto.SHA256)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	basic := p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(
		tbs, p.der(pkix.AlgorithmIdentifier{Algorithm: oidECDSAWithSHA256}),
		p.der(asn1.BitString{Bytes: sig, BitLength: 8 * len(sig)})))
	responseBytes := p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(p.der(oidOCSPBasic), p.der(basic)))
	p.write(w, p.wrap(asn1.ClassUniversal, asn1.TagSequence, slices.Concat(p.der(asn1.Enumerated(0)), p.wrap(asn1.ClassContextSpecific, 0, responseBytes))))
}

// serveCRL answers /crl/root and /crl/intermediate with the CRL of that
// authority, listing what [PKI.Revoke] revoked.
func (p *PKI) serveCRL(w http.ResponseWriter, r *http.Request) {
	issuer := p.Root
	if r.URL.Path == "/crl/intermediate" {
		issuer = p.Intermediate
	}
	der, err := p.crl(issuer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/pkix-crl")
	p.write(w, der)
}

// CRL returns the current CRL of issuer, the root or the intermediate, as
// its distribution point serves it.
func (p *PKI) CRL(issuer *x509.Certificate) []byte {
	der, err := p.crl(issuer)
	if err != nil {
		p.report("pkitest: %v", err)
	}
	return der
}

// crl returns the CRL of issuer, the root or the intermediate, listing what
// [PKI.Revoke] revoked.
func (p *PKI) crl(issuer *x509.Certificate) ([]byte, error) {
	key := p.RootKey
	if issuer == p.Intermediate {
		key = p.IntermediateKey
	}
	now := time.Now().UTC().Truncate(time.Second)
	tmpl := &x509.RevocationList{Number: p.nextSerial(), ThisUpdate: now, NextUpdate: now.Add(24 * time.Hour)}
	p.mu.Lock()
	for serial, at := range p.revoked {
		if n, ok := new(big.Int).SetString(serial, 10); ok {
			tmpl.RevokedCertificateEntries = append(tmpl.RevokedCertificateEntries, x509.RevocationListEntry{SerialNumber: n, RevocationTime: at})
		}
	}
	p.mu.Unlock()
	return x509.CreateRevocationList(rand.Reader, tmpl, issuer, key)
}

// serveCert answers /cert/root and /cert/intermediate with that
// certificate's DER, as an authority information access caIssuers URL does.
func (p *PKI) serveCert(w http.ResponseWriter, r *http.Request) {
	cert := p.Root
	if r.URL.Path == "/cert/intermediate" {
		cert = p.Intermediate
	}
	w.Header().Set("Content-Type", "application/pkix-cert")
	p.write(w, cert.Raw)
}

// publicKeyBits returns the subjectPublicKey BIT STRING contents of cert, the
// key RFC 6960, 4.1.1 hashes.
func (p *PKI) publicKeyBits(cert *x509.Certificate) []byte {
	var spki struct {
		Algorithm pkix.AlgorithmIdentifier
		PublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(cert.RawSubjectPublicKeyInfo, &spki); err != nil {
		p.report("pkitest: reading the public key of %s: %v", cert.Subject.CommonName, err)
	}
	return spki.PublicKey.Bytes
}

// write sends data as the answer, failing the test when it cannot.
func (p *PKI) write(w http.ResponseWriter, data []byte) {
	if _, err := w.Write(data); err != nil {
		p.report("pkitest: answering: %v", err)
	}
}

// der returns the DER of v, failing the test when it has none.
func (p *PKI) der(v any) []byte {
	der, err := asn1.Marshal(v)
	if err != nil {
		p.report("pkitest: encoding %T: %v", v, err)
	}
	return der
}

// generalized returns the DER GeneralizedTime of t.
func (p *PKI) generalized(t time.Time) []byte {
	der, err := asn1.MarshalWithParams(t.UTC(), "generalized")
	if err != nil {
		p.report("pkitest: encoding %v: %v", t, err)
	}
	return der
}

// wrap returns the DER of a constructed element of class and tag holding
// content.
func (p *PKI) wrap(class, tag int, content []byte) []byte {
	return p.der(asn1.RawValue{Class: class, Tag: tag, IsCompound: true, Bytes: content})
}
