#!/bin/sh
# Regenerates the PKCS #12 test fixtures with OpenSSL 3 (its legacy provider
# for the -legacy files). Every run issues fresh keys, so the files change;
# the tests rely only on what the comment above each file states.
set -eu
cd "$(dirname "$0")"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# A root CA and two signing certificates it issues, one EC P-256, one RSA.
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
	-keyout "$work/root.key" -out root.pem -days 36500 -subj "/CN=litho pkcs12 root" \
	-addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign"
printf 'keyUsage=critical,digitalSignature,nonRepudiation\n' >"$work/leaf.ext"
for kind in ec rsa; do
	case $kind in
	ec) openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$work/$kind.key" ;;
	rsa) openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$work/$kind.key" ;;
	esac
	openssl req -new -key "$work/$kind.key" -subj "/CN=litho pkcs12 $kind signer" -out "$work/$kind.csr"
	openssl x509 -req -in "$work/$kind.csr" -CA root.pem -CAkey "$work/root.key" \
		-set_serial "0x$(openssl rand -hex 8)" -days 36500 -extfile "$work/leaf.ext" -out "$work/$kind.pem"
done

pack() { # out kind password [openssl pkcs12 options]
	out=$1 kind=$2 pass=$3
	shift 3
	openssl pkcs12 -export -inkey "$work/$kind.key" -in "$work/$kind.pem" -certfile root.pem \
		-passout "pass:$pass" -out "$out" "$@"
}

# OpenSSL 3 defaults: PBES2 (PBKDF2 hmacWithSHA256, AES-256-CBC), SHA-256 MAC.
pack aes256.p12 ec litho-test
# -legacy: RC2-40 over the certificates, 3DES over the key, SHA-1 MAC. A
# password outside ASCII exercises the BMPString conversion.
pack legacy.p12 rsa 'pässwörd' -legacy
# RC2-128 over the certificates, PBES2 AES-128-CBC over the key, SHA-512 MAC.
pack mixed.p12 ec litho-test -legacy -certpbe PBE-SHA1-RC2-128 -keypbe AES-128-CBC -macalg sha512
# Nothing encrypted and no MAC: a plain keyBag, the certificates in the clear.
pack plain.p12 ec litho-test -keypbe NONE -certpbe NONE -nomac
# The empty password, OpenSSL defaults.
pack empty.p12 ec ''
# Certificates only, no private key.
openssl pkcs12 -export -nokeys -in root.pem -passout pass:litho-test -out nokey.p12
