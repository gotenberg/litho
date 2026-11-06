// Package fixture reads one source of the html corpus: the document of a
// sources/<name>/ directory, index.html or index.md, and the optional
// options.json beside it that names the Chromium route's form fields the
// source is converted with (FIXTURES.md documents the schema).
//
// Three readers convert a source and must agree on its options: chromium.sh,
// which posts the fields to Gotenberg's Chromium route for the reference,
// corpus/html/build, which writes litho's committed PDF, and the diff feature
// of test/integration/cmd/corpus, which scores litho against the reference.
// The two Go readers go through this package; chromium.sh reads the same keys
// with jq.
package fixture
