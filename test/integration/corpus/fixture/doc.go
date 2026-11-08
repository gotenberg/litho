// Package fixture reads one source of a converter corpus that Gotenberg
// converts too, and the optional options.json beside it that names the
// route's form fields the source is converted with: an html corpus source,
// index.html or index.md, for the Chromium route, and an Office corpus
// source, one document, for the LibreOffice route (litho/html's and each
// gotenberg Office corpus's FIXTURES.md document the schema).
//
// Three readers convert a source and must agree on its options: the script
// that posts the fields to Gotenberg for the reference (lib/chromium.sh,
// lib/libreoffice.sh), the builder that writes litho's committed PDF
// (fixture/htmlbuild, fixture/build), and the diff feature of
// test/integration/cmd/corpus, which scores litho against the reference. The
// two Go readers go through this package; the scripts read the same keys
// with jq.
package fixture
