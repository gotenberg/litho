// Package pkitest runs a public key infrastructure for tests: a root and an
// intermediate certificate authority, a signing certificate, an RFC 3161
// timestamp authority, an RFC 6960 OCSP responder and the CRLs and
// certificates the certificates point at, all served by one local
// [net/http/httptest] server. Tests of signing reach no network.
package pkitest
