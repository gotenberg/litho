// Package offcrypto opens password-protected Office documents: the
// MS-OFFCRYPTO compound files Word, Excel, PowerPoint and LibreOffice write
// around an encrypted OOXML package, and the encrypted entries of an
// OpenDocument package (ODF 1.3 Part 2, 3.4). It is what turns the Password
// of the Office converters' options into the package bytes their readers
// open.
//
// # OOXML
//
// [OpenOOXML] takes a document's bytes. A zip archive is an unencrypted
// package and comes back unchanged. A compound file (MS-CFB) is read for its
// EncryptionInfo and EncryptedPackage streams (MS-OFFCRYPTO, 2.3.4.4), and
// the package is decrypted:
//
//   - Agile encryption (version 4.4, MS-OFFCRYPTO 2.3.4.10 to 2.3.4.15):
//     the password key encryptor, SHA-1, SHA-256, SHA-384 or SHA-512, AES-128,
//     AES-192 or AES-256 in CBC. The password verifier is checked before the
//     key is used and the data integrity HMAC before the package is returned.
//   - Standard encryption (versions 2.2, 3.2 and 4.2, MS-OFFCRYPTO 2.3.4.5 to
//     2.3.4.9): AES-128, AES-192 or AES-256 in ECB with the SHA-1 key
//     derivation of 2.3.4.7. The password verifier is checked.
//
// RC4 CryptoAPI, Extensible encryption, a certificate key encryptor and the
// binary Word, Excel and PowerPoint formats are not read: they fail with
// office.ErrEncrypted, and the message names the scheme.
//
// # OpenDocument
//
// [OpenODFEntry] decrypts one package entry from the attributes its
// manifest:encryption-data carries, held verbatim in an [ODFParams], then
// inflates it and checks the checksum:
//
//   - AES-256 in CBC with PBKDF2 and a SHA-256 start key: ODF 1.2 and 1.3,
//     written by OpenOffice.org 3.4, LibreOffice 3.5 and later.
//   - AES-256 in GCM with Argon2id: LibreOffice 24.2 and later in its
//     default ODF 1.4 extended mode, over the whole package
//     (encrypted-package).
//   - Blowfish in CFB with PBKDF2 and a SHA-1 start key: ODF 1.0 and 1.1,
//     and ODF 1.2 by earlier versions, with the start key and checksum
//     fallbacks LibreOffice keeps for StarOffice and OpenOffice.org files.
//
// PGP encryption is not read.
//
// # Errors
//
// Every function fails with office.ErrEncrypted when the password is wrong
// or empty, or when the document uses a scheme this package does not read;
// with office.ErrMalformed when the container or its encryption descriptor
// cannot be read; with office.ErrInputTooLarge when a decrypted entry passes
// its cap. The message says which.
//
// # Cost
//
// A password check costs what the document's descriptor asks for, within
// caps: an Agile spin count of at most 10,000,000 hashes (the bound of
// MS-OFFCRYPTO, 2.3.4.10), a PBKDF2 iteration count of at most 10,000,000,
// and Argon2id at most 256 MiB of memory, 16 lanes and 2 GiB of block passes
// (passes times memory). A descriptor past a cap fails with
// office.ErrEncrypted before any work is done. The hash loops check the
// context; Argon2id runs to its end.
//
// # Concurrency
//
// Every function is safe for concurrent use: none keeps state between calls.
// None retains or modifies its byte arguments.
package offcrypto
