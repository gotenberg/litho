// Package ooxml reads Office Open XML packages (ECMA-376 Part 2, the Open
// Packaging Conventions) and the markup the WordprocessingML,
// SpreadsheetML and PresentationML converters share: the content types,
// the package and part relationships, a bounded XML element tree with
// markup compatibility resolved (ECMA-376 Part 3), the theme, the simple
// types (twips, EMUs, colours, on/off values) and the DrawingML pictures
// with their decoders.
//
// # Safety
//
// A package is untrusted input. [Open] caps the number of zip entries,
// every entry's uncompressed size and the bytes read from the package in
// all; [Package.ParseXML] caps the elements and the nesting of a part and
// of the package. XML is read with encoding/xml in strict mode: a
// document type declaration is refused, so no DTD and no external entity
// is ever read, and the only entities are the five XML predefines.
//
// # Strict documents
//
// ISO/IEC 29500 Strict documents name their parts and markup with the
// purl.oclc.org namespaces; the parser maps them to the Transitional ones,
// so a consumer matches one set of names. Relationship types are mapped
// the same way.
//
// # Concurrency
//
// A [Package] is not safe for concurrent use: it counts the bytes and
// elements it reads.
package ooxml
