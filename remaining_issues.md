1. ModDate extraction (metadata.go): Added "ModDate" to the Info dict keys list. 164→17 mismatches.
2. Language extraction (metadata.go): Added getCatalogLang() to read /Lang from the document catalog. 89→1 mismatches.
3. PDFDocEncoding (resolve.go): DecodePdfString now properly converts bytes 0x80-0xFF to Unicode runes using Windows-1252 mapping (0x80-0x9F) and direct code point mapping (0xA0-0xFF). Fixes ™, ©, ® and other special characters in
   Producer/Creator strings. 24→11 Producer mismatches.

Remaining 68 files with metadata mismatches:

- IsAcroFormPresent (29): Form detection gap (litho's GetFormData doesn't detect all form types)
- CreationDate/ModDate (37): Date format edge cases or XMP vs Info dict disagreements
- Producer/Creator (20): XMP metadata differs from Info dict (PDF.js reads XMP, litho reads Info then overlays XMP)
- Other (6): Title, Keywords, Author, Language edge cases
