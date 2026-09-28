// Package pkcs12 reads the private keys and certificates of a PKCS #12 key
// store (RFC 7292), the .p12 or .pfx file a certificate authority or a
// browser exports: the PFX with its password-integrity MAC, the
// AuthenticatedSafe, plain and password-encrypted SafeContents, KeyBag,
// PKCS8ShroudedKeyBag and CertBag. It decrypts PBES2 (PBKDF2 with AES-CBC
// or DES-EDE3-CBC, RFC 8018) and the legacy password-based schemes of RFC
// 7292, Appendix C: pbeWithSHAAnd3-KeyTripleDES-CBC,
// pbeWithSHAAnd2-KeyTripleDES-CBC and pbeWithSHAAnd128BitRC2-CBC and
// pbeWithSHAAnd40BitRC2-CBC, whose RC2 (RFC 2268) this package implements
// for decryption only, since only old key stores still use it.
//
// It writes nothing, and reads no public-key privacy or integrity mode
// (enveloped or signed authenticated safes), which exporters do not produce.
package pkcs12
