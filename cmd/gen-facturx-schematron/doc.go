// Command gen-facturx-schematron preprocesses the bundled Factur-X
// profile Schematron files by inlining external code-list lookups.
//
// The upstream profile Schematrons reference companion codedb XML
// files via the XPath 1.0 [document] function, e.g.
//
//	test="document('FACTUR-X_EN16931_codedb.xml')/codedb/cl[@id=3]/enumeration[@value=$codeValue3]"
//
// litho's [pkg/internal/schematron] engine is backed by antchfx/xpath,
// which does not implement [document]. Rather than fork the XPath
// engine, this generator resolves the lookup at build time:
//
//  1. Parse each FACTUR-X_*_codedb.xml file into a {file, cl-id} →
//     value-list map.
//
//  2. Walk every FACTUR-X_*.sch under facturx_profiles/ and rewrite
//     each lookup into a contains-based membership test of the form
//
//     contains(' v1 v2 ... ', concat(' ', EXPR, ' '))
//
//  3. Overwrite the .sch in place; codedb XMLs are consumed at build
//     time only and are not bundled into the binary.
//
// Usage:
//
//	go run ./cmd/gen-facturx-schematron \
//	    --codedb-dir=/tmp/facturx-codedb \
//	    --sch-dir=pkg/internal/compliance/cii_schematron/facturx_profiles
package main
