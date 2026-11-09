// Package fixture reads one source of a converter corpus that Gotenberg
// converts too, and the optional options.json beside it that names the
// route's form fields the source is converted with: an html corpus source,
// index.html or index.md, for the Chromium route, and an Office corpus
// source, one document, for the LibreOffice route (litho/html's and
// litho/docx's FIXTURES.md document the schema).
//
// Two readers convert a source and must agree on its options: corpus
// references, which posts the fields to Gotenberg for the reference, and the
// diff feature of test/integration/cmd/corpus, which scores litho against
// the reference. Both go through this package.
package fixture
