# Remediation Full Analysis

Deep analysis of all 751 compliance rules with concrete fixer procedures.
Each rule is analyzed in isolation: target object, step-by-step fix procedure, complexity, and existing fixer reference.

Total: 751 rules across 6 specifications.

## PDF/A-1 (ISO 19005-1)

### §6.1.2

#### Test 1 — 19005_1_6_1_2_t01

**Rule:** The % character of the file header shall occur at byte offset 0 of the file. The first line of a PDF file is a header identifying the version of the PDF specification to which the file conforms

**Target:** Document trailer/xref

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 2 — 19005_1_6_1_2_t02

**Rule:** The file header line shall be immediately followed by a comment consisting of a % character followed by at least four characters, each of whose encoded byte values shall have a decimal value greater than 127

**Target:** Document trailer/xref

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

### §6.1.3

#### Test 1 — 19005_1_6_1_3_t01

**Rule:** The file trailer dictionary shall contain the ID keyword. The file trailer referred to is either the last trailer dictionary in a PDF file, as described in PDF Reference 3.4.4 and 3.4.5, or the first page trailer in a linearized PDF file, as described in PDF Reference F.2

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_trailerid.go`

**Procedure:**

1. Compute MD5 hash of the document content (excluding the ID entry itself).
2. Create a two-element /ID array with the hash as both elements.
3. Add /ID to the trailer dictionary.
4. Existing implementation: `fixer_trailerid.go`.

#### Test 2 — 19005_1_6_1_3_t02

**Rule:** The keyword Encrypt shall not be used in the trailer dictionary

**Target:** CosTrailer

**Complexity:** Low
**Existing fixer:** `fixer_trailerid.go`

**Procedure:**

1. Remove /Encrypt from the file trailer.
2. Decrypt all encrypted strings and streams.
3. Full rewrite outputs decrypted content.
4. Existing implementation: `fixer_trailerid.go`.

#### Test 3 — 19005_1_6_1_3_t03

**Rule:** No data shall follow the last end-of-file marker except a single optional end-of-line marker

**Target:** Document trailer/xref

**Complexity:** Medium

**Procedure:**

1. Full rewrite terminates at %%EOF with a single EOL marker.
2. No post-EOF data is written.

#### Test 4 — 19005_1_6_1_3_t04

**Rule:** In a linearized PDF, if the ID keyword is present in both the first page trailer dictionary and the last trailer dictionary, the value to both instances of the ID keyword shall be identical

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_trailerid.go`

**Procedure:**

1. Compute MD5 hash of the document content (excluding the ID entry itself).
2. Create a two-element /ID array with the hash as both elements.
3. Add /ID to the trailer dictionary.
4. Existing implementation: `fixer_trailerid.go`.

### §6.1.4

#### Test 1 — 19005_1_6_1_4_t01

**Rule:** In a cross reference subsection header the starting object number and the range shall be separated by a single SPACE character (20h)

**Target:** CosXRef

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 2 — 19005_1_6_1_4_t02

**Rule:** The xref keyword and the cross reference subsection header shall be separated by a single EOL marker

**Target:** CosXRef

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 3 — 19005_1_6_1_4_t03

**Rule:** Xref streams shall not be used

**Target:** PDDocument

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

### §6.1.6

#### Test 1 — 19005_1_6_1_6_t01

**Rule:** Hexadecimal strings shall contain an even number of non-white-space characters

**Target:** String object

**Complexity:** Medium

**Procedure:**

1. Full rewrite re-serializes all hex strings with even-length digit pairs.
2. Invalid characters are stripped during re-serialization.

#### Test 2 — 19005_1_6_1_6_t02

**Rule:** All non-white-space characters in hexadecimal strings shall be in the range 0 to 9, A to F or a to f

**Target:** String object

**Complexity:** Medium

**Procedure:**

1. Full rewrite re-serializes all hex strings with even-length digit pairs.
2. Invalid characters are stripped during re-serialization.

### §6.1.7

#### Test 1 — 19005_1_6_1_7_t01

**Rule:** The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED character after the stream keyword and preceding the EOL marker before the endstream keyword

**Target:** Stream object

**Complexity:** Medium

**Procedure:**

1. Full rewrite writes "stream\r\n" (CR+LF) after the stream keyword.
2. Writes EOL marker before "endstream".

#### Test 2 — 19005_1_6_1_7_t02

**Rule:** The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED character. The endstream keyword shall be preceded by an EOL marker

**Target:** Stream object

**Complexity:** Medium

**Procedure:**

1. Full rewrite writes "stream\r\n" (CR+LF) after the stream keyword.
2. Writes EOL marker before "endstream".

#### Test 3 — 19005_1_6_1_7_t03

**Rule:** A stream object dictionary shall not contain the F, FFilter, or FDecodeParms keys

**Target:** Stream object

**Complexity:** Low

**Procedure:**

1. Resolve the stream dictionary.
2. Delete /F, /FFilter, /FDecodeParms keys.
3. If /F referenced an external file, read the file data and embed it as the stream body.
4. Allocate the modified stream.

### §6.1.8

#### Test 1 — 19005_1_6_1_8_t01

**Rule:** The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded by an EOL marker. The obj and endobj keywords shall each be followed by an EOL marker

**Target:** CosIndirect

**Complexity:** Medium

**Procedure:**

1. Full rewrite serializes "N G obj\n" with single spaces and EOL markers.

### §6.1.10

#### Test 1 — 19005_1_6_1_10_t01

**Rule:** The LZWDecode filter shall not be permitted

**Target:** CosFilter

**Complexity:** Medium
**Existing fixer:** `fixer_lzw.go`

**Procedure:**

1. Decode the LZW stream to raw bytes.
2. Re-encode as FlateDecode (zlib compression).
3. Replace /Filter with /FlateDecode, remove /DecodeParms, update /Length.
4. Allocate the new stream.
5. Existing implementation: `fixer_lzw.go`.

#### Test 2 — 19005_1_6_1_10_t02

**Rule:** The LZWDecode filter shall not be permitted

**Target:** CosIIFilter

**Complexity:** Medium
**Existing fixer:** `fixer_lzw.go`

**Procedure:**

1. Decode the LZW stream to raw bytes.
2. Re-encode as FlateDecode (zlib compression).
3. Replace /Filter with /FlateDecode, remove /DecodeParms, update /Length.
4. Allocate the new stream.
5. Existing implementation: `fixer_lzw.go`.

### §6.1.11

#### Test 1 — 19005_1_6_1_11_t01

**Rule:** A file specification dictionary, as defined in PDF 3.10.2, shall not contain the EF key

**Target:** File specification dict

**Complexity:** Low

**Procedure:**

1. Resolve all file specification dicts in the document.
2. Remove /EF key from each (embedded files forbidden in PDF/A-1).
3. Allocate modified file spec dicts.

#### Test 2 — 19005_1_6_1_11_t02

**Rule:** A file's name dictionary, as defined in PDF Reference 3.6.3, shall not contain the EmbeddedFiles key

**Target:** Document trailer/xref

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

### §6.1.12

#### Test 1 — 19005_1_6_1_12_t01

**Rule:** Largest Integer value is 2,147,483,647. Smallest integer value is -2,147,483,648

**Target:** Integer

**Complexity:** Medium

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 2 — 19005_1_6_1_12_t02

**Rule:** Absolute real value must be less than or equal to 32767.0

**Target:** Real number

**Complexity:** Medium

**Procedure:**

1. Scan all objects for values exceeding the spec limit.
2. For each out-of-range value, determine if clamping is safe (coordinate vs. object reference).
3. Clamp to the spec maximum/minimum.
4. Allocate modified objects.
5. Note: clamping may alter visual appearance for coordinates, or corrupt references for object numbers.

#### Test 3 — 19005_1_6_1_12_t03

**Rule:** Maximum length of a string (in bytes) is 65535

**Target:** String object

**Complexity:** Medium

**Procedure:**

1. Scan for objects exceeding the limit.
2. Split, truncate, or restructure to fit within limits.
3. Note: structural changes may be required (splitting arrays, flattening nesting).

#### Test 4 — 19005_1_6_1_12_t04

**Rule:** Maximum length of a name (in bytes) is 127

**Target:** Name object

**Complexity:** Medium

**Procedure:**

1. Scan for objects exceeding the limit.
2. Split, truncate, or restructure to fit within limits.
3. Note: structural changes may be required (splitting arrays, flattening nesting).

#### Test 5 — 19005_1_6_1_12_t05

**Rule:** Maximum capacity of an array (in elements) is 8191

**Target:** Array

**Complexity:** Medium

**Procedure:**

1. Scan for objects exceeding the limit.
2. Split, truncate, or restructure to fit within limits.
3. Note: structural changes may be required (splitting arrays, flattening nesting).

#### Test 6 — 19005_1_6_1_12_t06

**Rule:** Maximum capacity of a dictionary (in entries) is 4095

**Target:** Dictionary

**Complexity:** Medium

**Procedure:**

1. Scan for objects exceeding the limit.
2. Split, truncate, or restructure to fit within limits.
3. Note: structural changes may be required (splitting arrays, flattening nesting).

#### Test 7 — 19005_1_6_1_12_t07

**Rule:** Maximum number of indirect objects in a PDF file is 8,388,607

**Target:** Document trailer/xref

**Complexity:** Medium

**Procedure:**

1. Scan for objects exceeding the limit.
2. Split, truncate, or restructure to fit within limits.
3. Note: structural changes may be required (splitting arrays, flattening nesting).

#### Test 8 — 19005_1_6_1_12_t08

**Rule:** Maximum depth of graphics state nesting by q and Q operators is 28

**Target:** Op_q_gsave

**Complexity:** Medium

**Procedure:**

1. Scan for objects exceeding the limit.
2. Split, truncate, or restructure to fit within limits.
3. Note: structural changes may be required (splitting arrays, flattening nesting).

#### Test 9 — 19005_1_6_1_12_t09

**Rule:** Maximum number of DeviceN components is 8

**Target:** PDDeviceN

**Complexity:** Medium

**Procedure:**

1. Scan for objects exceeding the limit.
2. Split, truncate, or restructure to fit within limits.
3. Note: structural changes may be required (splitting arrays, flattening nesting).

#### Test 10 — 19005_1_6_1_12_t10

**Rule:** Maximum value of a CID (character identifier) is 65,535

**Target:** CMap

**Complexity:** Medium

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

### §6.1.13

#### Test 1 — 19005_1_6_1_13_t01

**Rule:** The document catalog dictionary shall not contain a key with the name OCProperties

**Target:** Document trailer/xref

**Complexity:** Low
**Existing fixer:** `fixer_catalog.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Remove /OCProperties key.
3. Allocate updated catalog.
4. Existing implementation: `fixer_catalog.go`.

### §6.2.2

#### Test 1 — 19005_1_6_2_2_t01

**Rule:** A PDF/A-1 OutputIntent is an OutputIntent dictionary, as defined by PDF Reference 9.10.4, that is included in the file's OutputIntents array and has GTS_PDFA1 as the value of its S key and a valid ICC profile stream as the value its DestOutputProfile key

**Target:** ICCOutputProfile

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 2 — 19005_1_6_2_2_t02

**Rule:** If a file's OutputIntents array contains more than one entry, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream

**Target:** OutputIntent dict

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

### §6.2.3.2

#### Test 1 — 19005_1_6_2_3_2_t01

**Rule:** All ICCBased colour spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5

**Target:** ICCInputProfile

**Complexity:** High
**Existing fixer:** `fixer_icc.go`

**Procedure:**

1. Read the ICC profile stream bytes.
2. Validate header: device class (bytes 12-15), color space (bytes 16-19), version (byte 8).
3. If invalid, replace with a known-good profile (sRGB or CMYK).
4. Allocate replacement stream.
5. Existing implementation: `fixer_icc.go`.

#### Test 2 — 19005_1_6_2_3_2_t02

**Rule:** All ICCBased colour spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5. The number of color components in the color space described by the ICC profile data must match the number of components actually in the ICC profile. As of PDF 1.4, N must be 1, 3, or 4

**Target:** ICC profile stream

**Complexity:** High
**Existing fixer:** `fixer_icc.go`

**Procedure:**

1. Read the ICC profile stream bytes.
2. Validate header: device class (bytes 12-15), color space (bytes 16-19), version (byte 8).
3. If invalid, replace with a known-good profile (sRGB or CMYK).
4. Allocate replacement stream.
5. Existing implementation: `fixer_icc.go`.

### §6.2.3.3

#### Test 1 — 19005_1_6_2_3_3_t01

**Rule:** DeviceRGB may be used only if the file has a PDF/A-1 OutputIntent that uses an RGB colour space

**Target:** PDDeviceRGB

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 2 — 19005_1_6_2_3_3_t02

**Rule:** DeviceCMYK may be used only if the file has a PDF/A-1 OutputIntent that uses a CMYK colour space

**Target:** PDDeviceCMYK

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 3 — 19005_1_6_2_3_3_t03

**Rule:** If an uncalibrated colour space is used in a file then that file shall contain a PDF/A-1 OutputIntent, as defined in 6.2.2

**Target:** PDDeviceGray

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

### §6.2.4

#### Test 1 — 19005_1_6_2_4_t01

**Rule:** An Image dictionary shall not contain the Alternates key

**Target:** PDXImage

**Complexity:** Low

**Procedure:**

1. Identify Figure structure elements without /Alt.
2. Generate alt text via OCR on the image content, or set a placeholder.
3. Add /Alt to the structure element dictionary.
4. Allocate modified structure element.

#### Test 2 — 19005_1_6_2_4_t02

**Rule:** An XObject dictionary (Image or Form) shall not contain the OPI key

**Target:** PDXObject

**Complexity:** Low
**Existing fixer:** `fixer_images.go`

**Procedure:**

1. Resolve Image XObject dictionaries.
2. Remove /OPI key.
3. Allocate modified image dict.
4. Existing implementation: `fixer_images.go`.

#### Test 3 — 19005_1_6_2_4_t03

**Rule:** If an Image dictionary contains the Interpolate key, its value shall be false

**Target:** PDXImage

**Complexity:** Medium
**Existing fixer:** `fixer_images.go`

**Procedure:**

1. Resolve Image XObject dictionaries.
2. Set /Interpolate to false.
3. Allocate modified XObject.
4. Existing implementation: `fixer_images.go`.

#### Test 4 — 19005_1_6_2_4_t04

**Rule:** If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4 or 8

**Target:** PDXImage

**Complexity:** Medium

**Procedure:**

1. Resolve Image XObject dictionary.
2. If /BitsPerComponent is invalid, re-encode image data with a permitted value (1, 2, 4, or 8).
3. For image masks, set /BitsPerComponent to 1.
4. Allocate modified image stream.

#### Test 5 — 19005_1_6_2_4_t05

**Rule:** If an image mask dictionary contains the BitsPerComponent key, its value shall be 1

**Target:** PDMaskImage

**Complexity:** Medium

**Procedure:**

1. Resolve Image XObject dictionary.
2. If /BitsPerComponent is invalid, re-encode image data with a permitted value (1, 2, 4, or 8).
3. For image masks, set /BitsPerComponent to 1.
4. Allocate modified image stream.

### §6.2.5

#### Test 1 — 19005_1_6_2_5_t01

**Rule:** A form XObject dictionary shall not contain the Subtype2 key with a value of PS or the PS key

**Target:** PDXForm

**Complexity:** Low

**Procedure:**

1. Resolve Form XObject dictionaries.
2. Remove /Subtype2 and /PS keys.
3. Allocate modified Form XObject dicts.

### §6.2.6

#### Test 1 — 19005_1_6_2_6_t01

**Rule:** A conforming file shall not contain any reference XObjects

**Target:** PDXForm

**Complexity:** Low

**Procedure:**

1. Resolve XObject dictionaries.
2. Remove /Ref key from Form XObjects.
3. Or inline the referenced content.
4. Allocate modified XObject dicts.

### §6.2.7

#### Test 1 — 19005_1_6_2_7_t01

**Rule:** A conforming file shall not contain any PostScript XObjects

**Target:** PDXObject

**Complexity:** Low

**Procedure:**

1. Walk all XObject resources.
2. Remove or replace PostScript XObjects with equivalent Form XObjects.
3. Allocate modified resource dicts.

### §6.2.8

#### Test 1 — 19005_1_6_2_8_t01

**Rule:** An ExtGState dictionary shall not contain the TR key

**Target:** ExtGState dict

**Complexity:** Low
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /TR key, or set /TR2 to /Default.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

#### Test 2 — 19005_1_6_2_8_t02

**Rule:** An ExtGState dictionary shall not contain the TR2 key with a value other than Default

**Target:** ExtGState dict

**Complexity:** Low

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /TR key, or set /TR2 to /Default.
3. Allocate modified ExtGState.

### §6.2.9

#### Test 1 — 19005_1_6_2_9_t01

**Rule:** Where a rendering intent is specified, its value shall be one of the four values defined in PDF Reference RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation

**Target:** CosRenderingIntent

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Set /RI to a permitted value: RelativeColorimetric, AbsoluteColorimetric, Perceptual, or Saturation.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

### §6.2.10

#### Test 1 — 19005_1_6_2_10_t01

**Rule:** A content stream shall not contain any operators not defined in PDF Reference even if such operators are bracketed by the BX/EX compatibility operators

**Target:** Op_Undefined

**Complexity:** Low

**Procedure:**

1. Parse content stream operators.
2. Remove or replace undefined operators.
3. Re-serialize the content stream.
4. Allocate modified stream.

### §6.3.2

#### Test 1 — 19005_1_6_3_2_t01

**Rule:** All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 2 — 19005_1_6_3_2_t02

**Rule:** All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Subtype - name - (Required) The type of font; must be \

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 3 — 19005_1_6_3_2_t03

**Rule:** All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. BaseFont - name - (Required) The PostScript name of the font

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 4 — 19005_1_6_3_2_t04

**Rule:** All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array

**Target:** PDSimpleFont

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.
5. Existing implementation: `fixer_fonts.go`.

#### Test 5 — 19005_1_6_3_2_t05

**Rule:** All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array

**Target:** PDSimpleFont

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.
5. Existing implementation: `fixer_fonts.go`.

#### Test 6 — 19005_1_6_3_2_t06

**Rule:** All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths

**Target:** PDSimpleFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 7 — 19005_1_6_3_2_t07

**Rule:** All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.4 are Type1C - Type 1–equivalent font program represented in the Compact Font Format (CFF) and CIDFontType0C - Type 0 CIDFont program represented in the Compact Font Format (CFF)

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §6.3.3.1

#### Test 1 — 19005_1_6_3_3_1_t01

**Rule:** For any given composite (Type 0) font referenced within a conforming file, the CIDSystemInfo entries of its CIDFont and CMap dictionaries shall be compatible. In other words, the Registry and Ordering strings of the CIDSystemInfo dictionaries for that font shall be identical, unless the value of the Encoding key in the font dictionary is Identity-H or Identity-V

**Target:** Type 0 composite font

**Complexity:** High

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §6.3.3.2

#### Test 1 — 19005_1_6_3_3_2_t01

**Rule:** For all Type 2 CIDFonts that are used for rendering, the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in PDF Reference Table 5.13

**Target:** CIDFont

**Complexity:** Medium

**Procedure:**

1. Check if CIDFont has /CIDToGIDMap.
2. If missing, add /CIDToGIDMap /Identity (when CIDs map directly to GIDs).
3. Or generate a stream mapping CIDs to GIDs based on the font program cmap table.
4. Allocate modified CIDFont dictionary.

### §6.3.3.3

#### Test 1 — 19005_1_6_3_3_3_t01

**Rule:** All CMaps used within a conforming file, except Identity-H and Identity-V, shall be embedded in that file as described in PDF Reference 5.6.4

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.

#### Test 2 — 19005_1_6_3_3_3_t02

**Rule:** For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Read /WMode from both the CMap dictionary and the embedded CMap stream.
2. Set both to the same value (0 for horizontal, 1 for vertical).
3. Allocate modified CMap stream.

### §6.3.4

#### Test 1 — 19005_1_6_3_4_t01

**Rule:** The font programs for all fonts used within a conforming file shall be embedded within that file, as defined in PDF Reference 5.8, except when the fonts are used exclusively with text rendering mode 3

**Target:** PDFont

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

### §6.3.5

#### Test 1 — 19005_1_6_3_5_t01

**Rule:** Embedded font programs shall define all font glyphs referenced for rendering with conforming file

**Target:** Glyph

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 2 — 19005_1_6_3_5_t02

**Rule:** For all Type 1 font subsets referenced within a conforming file, the font descriptor dictionary shall include a CharSet string listing the character names defined in the font subset, as described in PDF Reference Table 5.18

**Target:** Type 1 font

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the Type 1 font program from /FontFile.
2. Parse the CharStrings dictionary to enumerate all glyph names.
3. Build a /CharSet string: "/" + glyph names joined by "/".
4. Set /CharSet on the FontDescriptor.
5. Allocate modified FontDescriptor.
6. Existing implementation: `fixer_fonts.go`.

#### Test 3 — 19005_1_6_3_5_t03

**Rule:** For all CIDFont subsets referenced within a conforming file, the font descriptor dictionary shall include a CIDSet stream identifying which CIDs are present in the embedded CIDFont file, as described in PDF Reference Table 5.20

**Target:** CIDFont

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the font program.
2. Enumerate all valid CIDs (GIDs with non-empty outlines).
3. Build a CIDSet bitfield: bit N of byte M represents CID (M*8 + 7-N).
4. Compress with FlateDecode and set as /CIDSet stream on FontDescriptor.
5. Allocate modified FontDescriptor and stream.
6. Existing implementation: `fixer_fonts.go`.

### §6.3.6

#### Test 1 — 19005_1_6_3_6_t01

**Rule:** For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract binary widths from the font program (hmtx table for TrueType, charstring widths for CFF).
2. Read /UnitsPerEm from the head table.
3. For each charcode, compute expected PDF width: binaryWidth * 1000 / unitsPerEm.
4. Compare with the /Widths array (simple fonts) or /W array (CIDFonts).
5. Replace mismatched entries with the correct values.
6. Allocate modified font dictionary.
7. Existing implementation: `fixer_fonts.go`.

### §6.3.7

#### Test 1 — 19005_1_6_3_7_t01

**Rule:** All non-symbolic TrueType fonts shall specify MacRomanEncoding or WinAnsiEncoding, either as the value of the Encoding entry in the font dictionary or as the value of the BaseEncoding entry in the dictionary that is the value of the Encoding entry in the font dictionary. If the value of the Encoding entry is a dictionary, it shall not contain a Differences entry

**Target:** TrueType font

**Complexity:** Low

**Procedure:**

1. Check if the font is symbolic (Flags bit 3 set in FontDescriptor).
2. If symbolic: remove /Encoding from the font dictionary.
3. If non-symbolic: set /Encoding to /WinAnsiEncoding or /MacRomanEncoding.
4. Allocate modified font dictionary.

#### Test 2 — 19005_1_6_3_7_t02

**Rule:** All symbolic TrueType fonts shall not specify an Encoding entry in the font dictionary

**Target:** TrueType font

**Complexity:** Medium

**Procedure:**

1. Check if the font is symbolic (Flags bit 3 set in FontDescriptor).
2. If symbolic: remove /Encoding from the font dictionary.
3. If non-symbolic: set /Encoding to /WinAnsiEncoding or /MacRomanEncoding.
4. Allocate modified font dictionary.

#### Test 3 — 19005_1_6_3_7_t03

**Rule:** Font programs' \

**Target:** TrueType font

**Complexity:** Medium

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.

### §6.3.8

#### Test 1 — 19005_1_6_3_8_t01

**Rule:** The font dictionary shall include a ToUnicode entry whose value is a CMap stream object that maps character codes to Unicode values, as described in PDF Reference 5.9, unless the font meets any of the following three conditions: (*) fonts that use the predefined encodings MacRomanEncoding, MacExpertEncoding or WinAnsiEncoding, or that use the predefined Identity-H or Identity-V CMaps; (*) Type 1 fonts whose character names are taken from the Adobe standard Latin character set or the set of named characters in the Symbol font, as defined in PDF Reference Appendix D; (*) Type 0 fonts whose descendant CIDFont uses the Adobe-GB1, Adobe-CNS1, Adobe-Japan1 or Adobe-Korea1 character collections

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the embedded font program from /FontFile, /FontFile2, or /FontFile3.
2. Parse the cmap table (TrueType) or charset (CFF) to build GID-to-Unicode mapping.
3. Construct charcode-to-Unicode mapping using the font encoding.
4. Generate a CMap stream with beginbfchar entries (chunks of 100).
5. Add the CMap as /ToUnicode on the font dictionary.
6. Allocate both the CMap stream and modified font dict.
7. Existing implementation: `fixer_fonts.go`.

### §6.4

#### Test 1 — 19005_1_6_4_t01

**Rule:** If an SMask key appears in an ExtGState dictionary, its value shall be None

**Target:** ExtGState dict

**Complexity:** Medium
**Existing fixer:** `fixer_transparency.go`

**Procedure:**

1. Resolve the object containing /SMask.
2. Set /SMask to /None (ExtGState) or remove /SMask (XObject).
3. Allocate modified object.
4. Existing implementation: `fixer_transparency.go`.

#### Test 2 — 19005_1_6_4_t02

**Rule:** An XObject dictionary shall not contain the SMask key

**Target:** PDXObject

**Complexity:** Low
**Existing fixer:** `fixer_transparency.go`

**Procedure:**

1. Resolve the object containing /SMask.
2. Set /SMask to /None (ExtGState) or remove /SMask (XObject).
3. Allocate modified object.
4. Existing implementation: `fixer_transparency.go`.

#### Test 3 — 19005_1_6_4_t03

**Rule:** A Group object with an S key with a value of Transparency shall not be included in a form XObject. A Group object with an S key with a value of Transparency shall not be included in a page dictionary

**Target:** PDGroup

**Complexity:** Medium

**Procedure:**

1. Resolve page /Group dictionary.
2. If /S is /Transparency, add /CS with a device-independent color space.
3. Or remove the /Group dictionary entirely.
4. Allocate modified page dict.

#### Test 4 — 19005_1_6_4_t04

**Rule:** If a BM key is present in an ExtGState object, its value shall be Normal or Compatible

**Target:** ExtGState dict

**Complexity:** Medium
**Existing fixer:** `fixer_transparency.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Set /BM to /Normal.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_transparency.go`.

#### Test 5 — 19005_1_6_4_t05

**Rule:** If a CA key is present in an ExtGState object, its value shall be 1.0

**Target:** ExtGState dict

**Complexity:** Medium

**Procedure:**

1. Resolve ExtGState or annotation dict.
2. Set /CA and /ca to 1.0 (fully opaque).
3. Allocate modified object.

#### Test 6 — 19005_1_6_4_t06

**Rule:** If a ca key is present in an ExtGState object, its value shall be 1.0

**Target:** ExtGState dict

**Complexity:** Medium

**Procedure:**

1. Resolve ExtGState or annotation dict.
2. Set /CA and /ca to 1.0 (fully opaque).
3. Allocate modified object.

### §6.5.2

#### Test 1 — 19005_1_6_5_2_t01

**Rule:** Annotation types not defined in PDF Reference shall not be permitted. Additionally, the FileAttachment, Sound and Movie types shall not be permitted

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Walk catalog /OpenAction, /AA, page /AA, and annotation /A and /AA entries.
2. Remove any action with /S = JavaScript, Launch, Sound, Movie, ResetForm, ImportData, or Hide.
3. For /AA dicts, remove only the entries with forbidden action types.
4. Allocate modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §6.5.3

#### Test 1 — 19005_1_6_5_3_t01

**Rule:** An annotation dictionary shall not contain the CA key with a value other than 1.0

**Target:** PDAnnot

**Complexity:** Low
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve ExtGState or annotation dict.
2. Set /CA and /ca to 1.0 (fully opaque).
3. Allocate modified object.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 19005_1_6_5_3_t02

**Rule:** An annotation dictionary shall contain the F key. The F key’s Print flag bit shall be set to 1 and its Hidden, Invisible and NoView flag bits shall be set to 0

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Set /F with Print flag (bit 3) = 1, Hidden (bit 2) = 0, Invisible (bit 1) = 0, NoView (bit 6) = 0.
3. Allocate modified annotation.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 3 — 19005_1_6_5_3_t03

**Rule:** An annotation dictionary shall not contain the C array or the IC array unless the colour space of the DestOutputProfile in the PDF/A-1 OutputIntent dictionary, defined in 6.2.2, is RGB

**Target:** PDAnnot

**Complexity:** Low
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 4 — 19005_1_6_5_3_t04

**Rule:** For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

#### Test 5 — 19005_1_6_5_3_t05

**Rule:** If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

#### Test 6 — 19005_1_6_5_3_t06

**Rule:** If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

### §6.6.1

#### Test 1 — 19005_1_6_6_1_t01

**Rule:** The Launch, Sound, Movie, ResetForm, ImportData and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted. The Hide action shall not be permitted (Corrigendum 2)

**Target:** Action dict

**Complexity:** Medium
**Existing fixer:** `fixer_javascript.go`

**Procedure:**

1. Walk catalog /OpenAction, /AA, page /AA, and annotation /A and /AA entries.
2. Remove any action with /S = JavaScript, Launch, Sound, Movie, ResetForm, ImportData, or Hide.
3. For /AA dicts, remove only the entries with forbidden action types.
4. Allocate modified objects.
5. Existing implementation: `fixer_javascript.go`.

#### Test 2 — 19005_1_6_6_1_t02

**Rule:** Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted

**Target:** Action dict

**Complexity:** Medium

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.

#### Test 3 — 19005_1_6_6_1_t03

**Rule:** Interactive form fields shall not perform actions of any type

**Target:** Widget annotation

**Complexity:** Medium

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.

### §6.6.2

#### Test 1 — 19005_1_6_6_2_t01

**Rule:** A Widget annotation dictionary shall not include an AA entry for an additional-actions dictionary

**Target:** Widget annotation

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.
5. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 19005_1_6_6_2_t02

**Rule:** A Field dictionary shall not include an AA entry for an additional-actions dictionary

**Target:** Form field dict

**Complexity:** Medium

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.

#### Test 3 — 19005_1_6_6_2_t03

**Rule:** The document catalog dictionary shall not include an AA entry for an additional-actions dictionary

**Target:** PDDocument

**Complexity:** Medium

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.

### §6.7.2

#### Test 1 — 19005_1_6_7_2_t01

**Rule:** The document catalog dictionary of a conforming file shall contain the Metadata key. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Check if catalog has /Metadata.
2. If missing, generate XMP from /Info dict values.
3. If present but invalid, repair the XMP XML structure.
4. Allocate XMP stream and update catalog.
5. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_1_6_7_2_t02

**Rule:** The Metadata object stream dictionary in the document’s catalog shall not contain the Filter key

**Target:** PDMetadata

**Complexity:** Low

**Procedure:**

1. Check if catalog has /Metadata.
2. If missing, generate XMP from /Info dict values.
3. If present but invalid, repair the XMP XML structure.
4. Allocate XMP stream and update catalog.

### §6.7.3

#### Test 1 — 19005_1_6_7_3_t01

**Rule:** The value of CreationDate entry from the document information dictionary, if present, and its analogous XMP property \

**Target:** CosInfo

**Complexity:** Medium

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.

#### Test 2 — 19005_1_6_7_3_t02

**Rule:** The value of Title entry from the document information dictionary, if present, and its analogous XMP property \

**Target:** CosInfo

**Complexity:** Medium

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.

#### Test 3 — 19005_1_6_7_3_t03

**Rule:** The value of Author entry from the document information dictionary, if present, and its analogous XMP property \

**Target:** CosInfo

**Complexity:** Medium

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.

#### Test 4 — 19005_1_6_7_3_t04

**Rule:** The value of Subject entry from the document information dictionary, if present, and its analogous XMP property \

**Target:** CosInfo

**Complexity:** Medium

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.

#### Test 5 — 19005_1_6_7_3_t05

**Rule:** The value of Keywords entry from the document information dictionary, if present, and its analogous XMP property \

**Target:** CosInfo

**Complexity:** Medium

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.

#### Test 6 — 19005_1_6_7_3_t06

**Rule:** The value of Creator entry from the document information dictionary, if present, and its analogous XMP property \

**Target:** CosInfo

**Complexity:** Medium

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.

#### Test 7 — 19005_1_6_7_3_t07

**Rule:** The value of Producer entry from the document information dictionary, if present, and its analogous XMP property \

**Target:** CosInfo

**Complexity:** Medium

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.

#### Test 8 — 19005_1_6_7_3_t08

**Rule:** The value of ModDate entry from the document information dictionary, if present, and its analogous XMP property \

**Target:** CosInfo

**Complexity:** Medium

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.

### §6.7.5

#### Test 1 — 19005_1_6_7_5_t01

**Rule:** The bytes attribute shall not be used in the header of an XMP packet

**Target:** XMP metadata stream

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 2 — 19005_1_6_7_5_t02

**Rule:** The encoding attribute shall not be used in the header of an XMP packet

**Target:** XMP metadata stream

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

### §6.7.8

#### Test 1 — 19005_1_6_7_8_t01

**Rule:** Extension schema descriptions shall be specified using the PDF/A extension schema description schema defined in this clause

**Target:** ExtensionSchemaObject

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_1_6_7_8_t02

**Rule:** The extension schema container schema uses the namespace URI \

**Target:** ExtensionSchemasContainer

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 3 — 19005_1_6_7_8_t03

**Rule:** Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 4 — 19005_1_6_7_8_t04

**Rule:** Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 5 — 19005_1_6_7_8_t05

**Rule:** Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 6 — 19005_1_6_7_8_t06

**Rule:** Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 7 — 19005_1_6_7_8_t07

**Rule:** Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 8 — 19005_1_6_7_8_t08

**Rule:** Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 9 — 19005_1_6_7_8_t09

**Rule:** Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 10 — 19005_1_6_7_8_t10

**Rule:** Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty'

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 11 — 19005_1_6_7_8_t11

**Rule:** Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 12 — 19005_1_6_7_8_t12

**Rule:** Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 13 — 19005_1_6_7_8_t13

**Rule:** Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 14 — 19005_1_6_7_8_t14

**Rule:** Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 15 — 19005_1_6_7_8_t15

**Rule:** Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 16 — 19005_1_6_7_8_t16

**Rule:** Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 17 — 19005_1_6_7_8_t17

**Rule:** Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'

**Target:** ExtensionSchemaField

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 18 — 19005_1_6_7_8_t18

**Rule:** Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined

**Target:** ExtensionSchemaField

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 19 — 19005_1_6_7_8_t19

**Rule:** Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'

**Target:** ExtensionSchemaField

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

### §6.7.9

#### Test 1 — 19005_1_6_7_9_t01

**Rule:** The metadata stream shall conform to XMP Specification and well formed PDFAExtension Schema for all extensions

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_1_6_7_9_t02

**Rule:** Properties specified in XMP form shall use either the predefined schemas defined in XMP Specification, or extension schemas that comply with XMP Specification

**Target:** XMPProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 3 — 19005_1_6_7_9_t03

**Rule:** Properties specified in XMP form shall use either the predefined schemas defined in XMP Specification, or extension schemas that comply with XMP Specification

**Target:** XMPProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

### §6.7.11

#### Test 1 — 19005_1_6_7_11_t01

**Rule:** The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_1_6_7_11_t02

**Rule:** The value of \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

#### Test 3 — 19005_1_6_7_11_t03

**Rule:** A Level A conforming file shall specify the value of \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

#### Test 4 — 19005_1_6_7_11_t04

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

#### Test 5 — 19005_1_6_7_11_t05

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

#### Test 6 — 19005_1_6_7_11_t06

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

### §6.8.2.2

#### Test 1 — 19005_1_6_8_2_2_t01

**Rule:** The document catalog dictionary shall include a MarkInfo dictionary with a Marked entry in it, whose value shall be true

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /MarkInfo << /Marked true >>.
3. Allocate updated catalog.
4. Existing implementation: `fixer_markinfo.go`.

### §6.8.3.3

#### Test 1 — 19005_1_6_8_3_3_t01

**Rule:** The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog dictionary, as described in PDF Reference 9.6

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_structtree.go`

**Procedure:**

1. Create a minimal StructTreeRoot with a Document root element.
2. Add /ParentTree number tree for MCID-to-structure-element mapping.
3. For PDF/UA-2, add /Namespaces array with PDF 1.7 and 2.0 namespace dicts.
4. Set catalog /StructTreeRoot.
5. Allocate all new objects.
6. Existing implementation: `fixer_structtree.go`.

### §6.8.3.4

#### Test 1 — 19005_1_6_8_3_4_t01

**Rule:** All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in PDF Reference 9.7.4, in the role map dictionary of the structure tree root

**Target:** SENonStandard

**Complexity:** Medium

**Procedure:**

1. Identify custom structure element types not in the standard set.
2. Add /RoleMap entries mapping each custom type to a standard type (P, Span, Div, etc.).
3. Or add /NS on each structure element pointing to the appropriate namespace.
4. Allocate modified StructTreeRoot.

#### Test 2 — 19005_1_6_8_3_4_t02

**Rule:** A circular mapping shall not exist

**Target:** SENonStandard

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

### §6.8.4

#### Test 1 — 19005_1_6_8_4_t01

**Rule:** If the Lang entry is present in the document catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as defined by RFC 1766, Tags for the Identification of Languages, as described in PDF Reference 9.8.1

**Target:** CosLang

**Complexity:** Medium

**Procedure:**

1. Detect list structures from content analysis (bullet/number patterns).
2. Create L/LI/Lbl/LBody structure elements.
3. Mark content streams with BDC/EMC for each list item.

### §6.9

#### Test 1 — 19005_1_6_9_t01

**Rule:** The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false

**Target:** PDAcroForm

**Complexity:** High
**Existing fixer:** `fixer_needappearances.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_needappearances.go`.

#### Test 2 — 19005_1_6_9_t02

**Rule:** Every form field shall have an appearance dictionary associated with the field's data

**Target:** Widget annotation

**Complexity:** High

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.


## PDF/A-2 (ISO 19005-2)

### §6.1.2

#### Test 1 — 19005_2_6_1_2_t01

**Rule:** The file header shall begin at byte zero and shall consist of \

**Target:** Document trailer/xref

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 2 — 19005_2_6_1_2_t02

**Rule:** The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127

**Target:** Document trailer/xref

**Complexity:** Medium

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

### §6.1.3

#### Test 1 — 19005_2_6_1_3_t01

**Rule:** The file trailer dictionary shall contain the ID keyword whose value shall be File Identifiers as defined in ISO 32000-1:2008, 14.4

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_trailerid.go`

**Procedure:**

1. Compute MD5 hash of the document content (excluding the ID entry itself).
2. Create a two-element /ID array with the hash as both elements.
3. Add /ID to the trailer dictionary.
4. Existing implementation: `fixer_trailerid.go`.

#### Test 2 — 19005_2_6_1_3_t02

**Rule:** The keyword Encrypt shall not be used in the trailer dictionary

**Target:** CosTrailer

**Complexity:** Low
**Existing fixer:** `fixer_trailerid.go`

**Procedure:**

1. Remove /Encrypt from the file trailer.
2. Decrypt all encrypted strings and streams.
3. Full rewrite outputs decrypted content.
4. Existing implementation: `fixer_trailerid.go`.

#### Test 3 — 19005_2_6_1_3_t03

**Rule:** No data can follow the last end-of-file marker except a single optional end-of-line marker as described in ISO 32000-1:2008, 7.5.5

**Target:** Document trailer/xref

**Complexity:** Medium

**Procedure:**

1. Full rewrite terminates at %%EOF with a single EOL marker.
2. No post-EOF data is written.

### §6.1.4

#### Test 2 — 19005_2_6_1_4_t02

**Rule:** The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker

**Target:** CosXRef

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

### §6.1.6

#### Test 1 — 19005_2_6_1_6_t01

**Rule:** Hexadecimal strings shall contain an even number of non-white-space characters

**Target:** String object

**Complexity:** Medium

**Procedure:**

1. Full rewrite re-serializes all hex strings with even-length digit pairs.
2. Invalid characters are stripped during re-serialization.

#### Test 2 — 19005_2_6_1_6_t02

**Rule:** A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f)

**Target:** String object

**Complexity:** Medium

**Procedure:**

1. Full rewrite re-serializes all hex strings with even-length digit pairs.
2. Invalid characters are stripped during re-serialization.

### §6.1.7.1

#### Test 1 — 19005_2_6_1_7_1_t01

**Rule:** The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword

**Target:** Stream object

**Complexity:** Medium

**Procedure:**

1. Full rewrite writes "stream\r\n" (CR+LF) after the stream keyword.
2. Writes EOL marker before "endstream".

#### Test 2 — 19005_2_6_1_7_1_t02

**Rule:** The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED (0Ah) character. The endstream keyword shall be preceded by an EOL marker

**Target:** Stream object

**Complexity:** Medium

**Procedure:**

1. Full rewrite writes "stream\r\n" (CR+LF) after the stream keyword.
2. Writes EOL marker before "endstream".

#### Test 3 — 19005_2_6_1_7_1_t03

**Rule:** A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys

**Target:** Stream object

**Complexity:** Low

**Procedure:**

1. Resolve the stream dictionary.
2. Delete /F, /FFilter, /FDecodeParms keys.
3. If /F referenced an external file, read the file data and embed it as the stream body.
4. Allocate the modified stream.

### §6.1.7.2

#### Test 1 — 19005_2_6_1_7_2_t01

**Rule:** All standard stream filters listed in ISO 32000-1:2008, 7.4, Table 6 may be used, with the exception of LZWDecode. In addition, the Crypt filter shall not be used unless the value of the Name key in the decode parameters dictionary is Identity. Filters that are not listed in ISO 32000-1:2008, 7.4, Table 6 shall not be used

**Target:** CosFilter

**Complexity:** Low
**Existing fixer:** `fixer_lzw.go`

**Procedure:**

1. Decode the LZW stream to raw bytes.
2. Re-encode as FlateDecode (zlib compression).
3. Replace /Filter with /FlateDecode, remove /DecodeParms, update /Length.
4. Allocate the new stream.
5. Existing implementation: `fixer_lzw.go`.

### §6.1.8

#### Test 1 — 19005_2_6_1_8_t01

**Rule:** Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences

**Target:** CosUnicodeName

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §6.1.9

#### Test 1 — 19005_2_6_1_9_t01

**Rule:** The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded by an EOL marker. The obj and endobj keywords shall each be followed by an EOL marker

**Target:** CosIndirect

**Complexity:** Medium

**Procedure:**

1. Full rewrite serializes "N G obj\n" with single spaces and EOL markers.

### §6.1.10

#### Test 1 — 19005_2_6_1_10_t01

**Rule:** The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-1:2008, Table 6, or an array containing any such value

**Target:** CosIIFilter

**Complexity:** Medium
**Existing fixer:** `fixer_lzw.go`

**Procedure:**

1. Decode the LZW stream to raw bytes.
2. Re-encode as FlateDecode (zlib compression).
3. Replace /Filter with /FlateDecode, remove /DecodeParms, update /Length.
4. Allocate the new stream.
5. Existing implementation: `fixer_lzw.go`.

### §6.1.12

#### Test 1 — 19005_2_6_1_12_t01

**Rule:** No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-1:2008, 12.8.4, Table 258)

**Target:** PDPerms

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 2 — 19005_2_6_1_12_t02

**Rule:** If DocMDP is present, then the Signature References dictionary (ISO 32000-1:2008, 12.8.1, Table 253) shall not contain the keys DigestLocation, DigestMethod, and DigestValue

**Target:** PDSigRef

**Complexity:** Low

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §6.1.13

#### Test 1 — 19005_2_6_1_13_t01

**Rule:** A conforming file shall not contain any integer greater than 2147483647. A conforming file shall not contain any integer less than -2147483648

**Target:** Integer

**Complexity:** Low

**Procedure:**

1. Scan all objects for values exceeding the spec limit.
2. For each out-of-range value, determine if clamping is safe (coordinate vs. object reference).
3. Clamp to the spec maximum/minimum.
4. Allocate modified objects.
5. Note: clamping may alter visual appearance for coordinates, or corrupt references for object numbers.

#### Test 2 — 19005_2_6_1_13_t02

**Rule:** A conforming file shall not contain any real number outside the range of +/-3.403 x 10^38

**Target:** Real number

**Complexity:** Low

**Procedure:**

1. Scan all objects for values exceeding the spec limit.
2. For each out-of-range value, determine if clamping is safe (coordinate vs. object reference).
3. Clamp to the spec maximum/minimum.
4. Allocate modified objects.
5. Note: clamping may alter visual appearance for coordinates, or corrupt references for object numbers.

#### Test 3 — 19005_2_6_1_13_t03

**Rule:** A conforming file shall not contain any string longer than 32767 bytes

**Target:** String object

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 4 — 19005_2_6_1_13_t04

**Rule:** A conforming file shall not contain any name longer than 127 bytes

**Target:** Name object

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 5 — 19005_2_6_1_13_t05

**Rule:** A conforming file shall not contain any real number closer to zero than +/-1.175 x 10^(-38)

**Target:** Real number

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 7 — 19005_2_6_1_13_t07

**Rule:** A conforming file shall not contain more than 8388607 indirect objects

**Target:** Document trailer/xref

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 8 — 19005_2_6_1_13_t08

**Rule:** A conforming file shall not nest q/Q pairs by more than 28 nesting levels

**Target:** Op_q_gsave

**Complexity:** Medium

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 9 — 19005_2_6_1_13_t09

**Rule:** A conforming file shall not contain a DeviceN colour space with more than 32 colourants

**Target:** PDDeviceN

**Complexity:** Low

**Procedure:**

1. Resolve the Separation/DeviceN color space array.
2. Check that the alternate color space is device-independent.
3. If device-dependent, replace with an ICCBased equivalent.
4. For DeviceN with Colorants, ensure all colorant tint transforms are consistent.
5. Allocate modified color space arrays.

#### Test 10 — 19005_2_6_1_13_t10

**Rule:** A conforming file shall not contain a CID value greater than 65535

**Target:** CMap

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 11 — 19005_2_6_1_13_t11

**Rule:** The size of any of the page boundaries described in ISO 32000-1:2008, 14.11.2 shall not be less than 3 units in either direction, nor shall it be greater than 14 400 units in either direction

**Target:** CosBBox

**Complexity:** Medium

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

### §6.2.2

#### Test 1 — 19005_2_6_2_2_t01

**Rule:** Content streams shall not contain any operators not defined in ISO 32000-1 even if such operators are bracketed by the BX/EX compatibility operators

**Target:** Op_Undefined

**Complexity:** Low

**Procedure:**

1. Parse content stream operators.
2. Remove or replace undefined operators.
3. Re-serialize the content stream.
4. Allocate modified stream.

#### Test 2 — 19005_2_6_2_2_t02

**Rule:** A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-1:2008, 7.8.3

**Target:** Content stream

**Complexity:** High

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.

### §6.2.3

#### Test 1 — 19005_2_6_2_3_t01

**Rule:** The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = \

**Target:** ICCOutputProfile

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 2 — 19005_2_6_2_3_t02

**Rule:** If a file's OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X-4 or PDF/E-1, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream

**Target:** OutputIntent dict

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 3 — 19005_2_6_2_3_t03

**Rule:** In addition, the DestOutputProfileRef key, as defined in ISO 15930-7:2010, Annex A, shall not be present in any PDF/X OutputIntent

**Target:** OutputIntent dict

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

### §6.2.4.2

#### Test 1 — 19005_2_6_2_4_2_t01

**Rule:** The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1

**Target:** ICCInputProfile

**Complexity:** Medium
**Existing fixer:** `fixer_icc.go`

**Procedure:**

1. Read the ICC profile stream bytes.
2. Validate header: device class (bytes 12-15), color space (bytes 16-19), version (byte 8).
3. If invalid, replace with a known-good profile (sRGB or CMYK).
4. Allocate replacement stream.
5. Existing implementation: `fixer_icc.go`.

#### Test 2 — 19005_2_6_2_4_2_t02

**Rule:** Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and overprinting for fill is set to true, or both

**Target:** ICCBased color space

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Read the ICC profile stream bytes.
2. Validate header: device class (bytes 12-15), color space (bytes 16-19), version (byte 8).
3. If invalid, replace with a known-good profile (sRGB or CMYK).
4. Allocate replacement stream.
5. Existing implementation: `fixer_graphics.go`.

### §6.2.4.3

#### Test 2 — 19005_2_6_2_4_3_t02

**Rule:** DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used, or if the file has a PDF/A OutputIntent that contains an RGB destination profile

**Target:** PDDeviceRGB

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 3 — 19005_2_6_2_4_3_t03

**Rule:** DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set or if a DeviceN-based DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or the file has a PDF/A OutputIntent that contains a CMYK destination profile

**Target:** PDDeviceCMYK

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 4 — 19005_2_6_2_4_3_t04

**Rule:** DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is present

**Target:** PDDeviceGray

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

### §6.2.4.4

#### Test 1 — 19005_2_6_2_4_4_t01

**Rule:** For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present

**Target:** PDDeviceN

**Complexity:** Medium

**Procedure:**

1. Resolve the Separation/DeviceN color space array.
2. Check that the alternate color space is device-independent.
3. If device-dependent, replace with an ICCBased equivalent.
4. For DeviceN with Colorants, ensure all colorant tint transforms are consistent.
5. Allocate modified color space arrays.

#### Test 2 — 19005_2_6_2_4_4_t02

**Rule:** All Separation arrays within a single PDF/A-2 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, rather than the computational result of the use of those PDF objects. Compression and whether or not an object is direct or indirect shall be ignored

**Target:** PDSeparation

**Complexity:** Medium

**Procedure:**

1. Resolve the Separation/DeviceN color space array.
2. Check that the alternate color space is device-independent.
3. If device-dependent, replace with an ICCBased equivalent.
4. For DeviceN with Colorants, ensure all colorant tint transforms are consistent.
5. Allocate modified color space arrays.

### §6.2.5

#### Test 1 — 19005_2_6_2_5_t01

**Rule:** An ExtGState dictionary shall not contain the TR key

**Target:** ExtGState dict

**Complexity:** Low
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /TR key, or set /TR2 to /Default.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

#### Test 2 — 19005_2_6_2_5_t02

**Rule:** An ExtGState dictionary shall not contain the TR2 key with a value other than Default

**Target:** ExtGState dict

**Complexity:** Low

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /TR key, or set /TR2 to /Default.
3. Allocate modified ExtGState.

#### Test 3 — 19005_2_6_2_5_t03

**Rule:** An ExtGState dictionary shall not contain the HTP key

**Target:** ExtGState dict

**Complexity:** Low

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.

#### Test 4 — 19005_2_6_2_5_t04

**Rule:** All halftones in a conforming PDF/A-2 file shall have the value 1 or 5 for the HalftoneType key

**Target:** PDHalftone

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /HT key or set to a default halftone.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

#### Test 5 — 19005_2_6_2_5_t05

**Rule:** Halftones in a conforming PDF/A-2 file shall not contain a HalftoneName key

**Target:** PDHalftone

**Complexity:** Low
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /HT key or set to a default halftone.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

#### Test 6 — 19005_2_6_2_5_t06

**Rule:** The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-1

**Target:** PDHalftone

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /HT key or set to a default halftone.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

### §6.2.6

#### Test 1 — 19005_2_6_2_6_t01

**Rule:** Where a rendering intent is specified, its value shall be one of the four values defined in ISO 32000-1:2008, Table 70: RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation

**Target:** CosRenderingIntent

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Set /RI to a permitted value: RelativeColorimetric, AbsoluteColorimetric, Perceptual, or Saturation.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

### §6.2.8

#### Test 1 — 19005_2_6_2_8_t01

**Rule:** An Image dictionary shall not contain the Alternates key

**Target:** PDXImage

**Complexity:** Low

**Procedure:**

1. Identify Figure structure elements without /Alt.
2. Generate alt text via OCR on the image content, or set a placeholder.
3. Add /Alt to the structure element dictionary.
4. Allocate modified structure element.

#### Test 2 — 19005_2_6_2_8_t02

**Rule:** An Image dictionary shall not contain the OPI key

**Target:** PDXImage

**Complexity:** Low
**Existing fixer:** `fixer_images.go`

**Procedure:**

1. Resolve Image XObject dictionaries.
2. Remove /OPI key.
3. Allocate modified image dict.
4. Existing implementation: `fixer_images.go`.

#### Test 3 — 19005_2_6_2_8_t03

**Rule:** If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key shall have a value of false

**Target:** PDXImage

**Complexity:** Medium
**Existing fixer:** `fixer_images.go`

**Procedure:**

1. Resolve Image XObject dictionaries.
2. Set /Interpolate to false.
3. Allocate modified XObject.
4. Existing implementation: `fixer_images.go`.

#### Test 4 — 19005_2_6_2_8_t04

**Rule:** If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16

**Target:** PDXImage

**Complexity:** Medium

**Procedure:**

1. Resolve Image XObject dictionary.
2. If /BitsPerComponent is invalid, re-encode image data with a permitted value (1, 2, 4, or 8).
3. For image masks, set /BitsPerComponent to 1.
4. Allocate modified image stream.

#### Test 5 — 19005_2_6_2_8_t05

**Rule:** If an image mask dictionary contains the BitsPerComponent key, its value shall be 1

**Target:** PDMaskImage

**Complexity:** Medium

**Procedure:**

1. Resolve Image XObject dictionary.
2. If /BitsPerComponent is invalid, re-encode image data with a permitted value (1, 2, 4, or 8).
3. For image masks, set /BitsPerComponent to 1.
4. Allocate modified image stream.

### §6.2.8.3

#### Test 1 — 19005_2_6_2_8_3_t01

**Rule:** The number of colour channels in the JPEG2000 data shall be 1, 3 or 4

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

#### Test 2 — 19005_2_6_2_8_3_t02

**Rule:** If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

#### Test 3 — 19005_2_6_2_8_3_t03

**Rule:** The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming reader shall use only that colour space and shall ignore all other colour space specifications

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.

#### Test 4 — 19005_2_6_2_8_3_t04

**Rule:** JPEG2000 enumerated colour space 19 (CIEJab) shall not be used

**Target:** JPEG2000

**Complexity:** Low

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

#### Test 5 — 19005_2_6_2_8_3_t05

**Rule:** The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

### §6.2.9

#### Test 1 — 19005_2_6_2_9_t01

**Rule:** A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key

**Target:** PDXForm

**Complexity:** Low
**Existing fixer:** `fixer_images.go`

**Procedure:**

1. Resolve Form XObject dictionaries.
2. Remove /Subtype2 and /PS keys.
3. Allocate modified Form XObject dicts.
4. Existing implementation: `fixer_images.go`.

#### Test 2 — 19005_2_6_2_9_t02

**Rule:** A conforming file shall not contain any reference XObjects

**Target:** PDXForm

**Complexity:** Low

**Procedure:**

1. Resolve XObject dictionaries.
2. Remove /Ref key from Form XObjects.
3. Or inline the referenced content.
4. Allocate modified XObject dicts.

#### Test 3 — 19005_2_6_2_9_t03

**Rule:** A conforming file shall not contain any PostScript XObjects

**Target:** PDXObject

**Complexity:** Low

**Procedure:**

1. Walk all XObject resources.
2. Remove or replace PostScript XObjects with equivalent Form XObjects.
3. Allocate modified resource dicts.

### §6.2.10

#### Test 1 — 19005_2_6_2_10_t01

**Rule:** Only blend modes that are specified in ISO 32000-1:2008 shall be used for the value of the BM key in an extended graphic state dictionary

**Target:** CosBM

**Complexity:** Medium
**Existing fixer:** `fixer_transparency.go`

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.
4. Existing implementation: `fixer_transparency.go`.

#### Test 2 — 19005_2_6_2_10_t02

**Rule:** If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space

**Target:** Page dict

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

### §6.2.11.2

#### Test 1 — 19005_2_6_2_11_2_t01

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 2 — 19005_2_6_2_11_2_t02

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Required) The type of font; must be \

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 3 — 19005_2_6_2_11_2_t03

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (Required) The PostScript name of the font

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 4 — 19005_2_6_2_11_2_t04

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array

**Target:** PDSimpleFont

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.
5. Existing implementation: `fixer_fonts.go`.

#### Test 5 — 19005_2_6_2_11_2_t05

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array

**Target:** PDSimpleFont

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.
5. Existing implementation: `fixer_fonts.go`.

#### Test 6 — 19005_2_6_2_11_2_t06

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths

**Target:** PDSimpleFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 7 — 19005_2_6_2_11_2_t07

**Rule:** All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.7 are Type1C - Type 1–equivalent font program represented in the Compact Font Format (CFF), CIDFontType0C - Type 0 CIDFont program represented in the Compact Font Format (CFF) and OpenType - OpenType® font program, as described in the OpenType Specification v.1.4

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §6.2.11.3.1

#### Test 1 — 19005_2_6_2_11_3_1_t01

**Rule:** For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary is Identity-H or Identity-V, any values of Registry, Ordering, and Supplement may be used in the CIDSystemInfo entry of the CIDFont. - Otherwise, the corresponding Registry and Ordering strings in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be less than or equal to the Supplement key in the CIDSystemInfo dictionary of the CMap

**Target:** Type 0 composite font

**Complexity:** High

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §6.2.11.3.2

#### Test 1 — 19005_2_6_2_11_3_2_t01

**Rule:** ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2008, 9.7.4, Table 117

**Target:** CIDFont

**Complexity:** High

**Procedure:**

1. Check if CIDFont has /CIDToGIDMap.
2. If missing, add /CIDToGIDMap /Identity (when CIDs map directly to GIDs).
3. Or generate a stream mapping CIDs to GIDs based on the font program cmap table.
4. Allocate modified CIDFont dictionary.

### §6.2.11.3.3

#### Test 1 — 19005_2_6_2_11_3_3_t01

**Rule:** All CMaps used within a PDF/A-2 file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 2 — 19005_2_6_2_11_3_3_t02

**Rule:** For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Read /WMode from both the CMap dictionary and the embedded CMap stream.
2. Set both to the same value (0 for horizontal, 1 for vertical).
3. Allocate modified CMap stream.

#### Test 3 — 19005_2_6_2_11_3_3_t03

**Rule:** A CMap shall not reference any other CMap except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §6.2.11.4.1

#### Test 1 — 19005_2_6_2_11_4_1_t01

**Rule:** The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9

**Target:** PDFont

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 2 — 19005_2_6_2_11_4_1_t02

**Rule:** Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType fonts that are to be rendered, character codes shall be able to be mapped to glyphs according to ISO 32000-1:2008, 9.6.6.4 without the use of a non-standard mapping chosen by the conforming processor

**Target:** Glyph

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

### §6.2.11.4.2

#### Test 1 — 19005_2_6_2_11_4_2_t01

**Rule:** If the FontDescriptor dictionary of an embedded Type 1 font contains a CharSet string, then it shall list the character names of all glyphs present in the font program, regardless of whether a glyph in the font is referenced or used by the PDF or not

**Target:** Type 1 font

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the Type 1 font program from /FontFile.
2. Parse the CharStrings dictionary to enumerate all glyph names.
3. Build a /CharSet string: "/" + glyph names joined by "/".
4. Set /CharSet on the FontDescriptor.
5. Allocate modified FontDescriptor.
6. Existing implementation: `fixer_fonts.go`.

#### Test 2 — 19005_2_6_2_11_4_2_t02

**Rule:** If the FontDescriptor dictionary of an embedded CID font contains a CIDSet stream, then it shall identify all CIDs which are present in the font program, regardless of whether a CID in the font is referenced or used by the PDF or not

**Target:** CIDFont

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the font program.
2. Enumerate all valid CIDs (GIDs with non-empty outlines).
3. Build a CIDSet bitfield: bit N of byte M represents CID (M*8 + 7-N).
4. Compress with FlateDecode and set as /CIDSet stream on FontDescriptor.
5. Allocate modified FontDescriptor and stream.
6. Existing implementation: `fixer_fonts.go`.

### §6.2.11.5

#### Test 1 — 19005_2_6_2_11_5_t01

**Rule:** For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract binary widths from the font program (hmtx table for TrueType, charstring widths for CFF).
2. Read /UnitsPerEm from the head table.
3. For each charcode, compute expected PDF width: binaryWidth * 1000 / unitsPerEm.
4. Compare with the /Widths array (simple fonts) or /W array (CIDFonts).
5. Replace mismatched entries with the correct values.
6. Allocate modified font dictionary.
7. Existing implementation: `fixer_fonts.go`.

### §6.2.11.6

#### Test 1 — 19005_2_6_2_11_6_t01

**Rule:** For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain one or several non-symbolic cmap entries such that all necessary glyph lookups can be carried out

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 2 — 19005_2_6_2_11_6_t02

**Rule:** All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary that is the value of the Encoding key in the Font dictionary. In addition, all non-symbolic TrueType fonts shall not define a Differences array unless all of the glyph names in the Differences array are listed in the Adobe Glyph List and, if the font program is embedded, it shall contain at least the Microsoft Unicode (3,1 – Platform ID = 3, Encoding ID = 1) encoding in the 'cmap' table

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 3 — 19005_2_6_2_11_6_t03

**Rule:** Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary

**Target:** TrueType font

**Complexity:** Low

**Procedure:**

1. Check if the font is symbolic (Flags bit 3 set in FontDescriptor).
2. If symbolic: remove /Encoding from the font dictionary.
3. If non-symbolic: set /Encoding to /WinAnsiEncoding or /MacRomanEncoding.
4. Allocate modified font dictionary.

#### Test 4 — 19005_2_6_2_11_6_t04

**Rule:** The 'cmap' table in the embedded font program for a symbolic TrueType font shall contain either exactly one encoding or it shall contain, at least, the Microsoft Symbol (3,0 - Platform ID=3, Encoding ID=0) encoding

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

### §6.2.11.7.2

#### Test 1 — 19005_2_6_2_11_7_2_t01

**Rule:** The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 19005-2, 6.2.11.7.2

**Target:** Glyph

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the embedded font program from /FontFile, /FontFile2, or /FontFile3.
2. Parse the cmap table (TrueType) or charset (CFF) to build GID-to-Unicode mapping.
3. Construct charcode-to-Unicode mapping using the font encoding.
4. Generate a CMap stream with beginbfchar entries (chunks of 100).
5. Add the CMap as /ToUnicode on the font dictionary.
6. Allocate both the CMap stream and modified font dict.
7. Existing implementation: `fixer_fonts.go`.

#### Test 2 — 19005_2_6_2_11_7_2_t02

**Rule:** The Unicode values specified in the ToUnicode CMap shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the embedded font program from /FontFile, /FontFile2, or /FontFile3.
2. Parse the cmap table (TrueType) or charset (CFF) to build GID-to-Unicode mapping.
3. Construct charcode-to-Unicode mapping using the font encoding.
4. Generate a CMap stream with beginbfchar entries (chunks of 100).
5. Add the CMap as /ToUnicode on the font dictionary.
6. Allocate both the CMap stream and modified font dict.
7. Existing implementation: `fixer_fonts.go`.

### §6.2.11.7.3

#### Test 1 — 19005_2_6_2_11_7_3_t01

**Rule:** For any character, regardless of its rendering mode, that is mapped to a code or codes in the Unicode Private Use Area (PUA), an ActualText entry as described in ISO 32000-1:2008, 14.9.4 shall be present for this character or a sequence of characters of which such a character is a part

**Target:** Glyph

**Complexity:** Medium

**Procedure:**

1. Identify structure elements with PUA-mapped characters.
2. Determine the actual Unicode text from font encoding or ToUnicode CMap.
3. Add /ActualText with the resolved text.
4. Allocate modified structure element.

### §6.2.11.8

#### Test 1 — 19005_2_6_2_11_8_t01

**Rule:** A PDF/A-2 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream

**Target:** Glyph

**Complexity:** Low

**Procedure:**

1. Parse content stream operators.
2. Remove or replace undefined operators.
3. Re-serialize the content stream.
4. Allocate modified stream.

### §6.3.1

#### Test 1 — 19005_2_6_3_1_t01

**Rule:** Annotation types not defined in ISO 32000-1 shall not be permitted. Additionally, the 3D, Sound, Screen and Movie types shall not be permitted

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Walk catalog /OpenAction, /AA, page /AA, and annotation /A and /AA entries.
2. Remove any action with /S = JavaScript, Launch, Sound, Movie, ResetForm, ImportData, or Hide.
3. For /AA dicts, remove only the entries with forbidden action types.
4. Allocate modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §6.3.2

#### Test 1 — 19005_2_6_3_2_t01

**Rule:** Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve Popup annotations.
2. Ensure /Parent references the parent annotation.
3. Allocate modified Popup annotation dict.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 19005_2_6_3_2_t02

**Rule:** If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.
4. Existing implementation: `fixer_annotflags.go`.

### §6.3.3

#### Test 1 — 19005_2_6_3_3_t01

**Rule:** Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: - annotations where the value of the Rect key consists of an array where value 1 is equal to value 3 and value 2 is equal to value 4; - annotations whose Subtype value is Popup or Link

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Detect list structures from content analysis (bullet/number patterns).
2. Create L/LI/Lbl/LBody structure elements.
3. Mark content streams with BDC/EMC for each list item.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 19005_2_6_3_3_t02

**Rule:** For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

#### Test 3 — 19005_2_6_3_3_t03

**Rule:** If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

#### Test 4 — 19005_2_6_3_3_t04

**Rule:** If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

### §6.4.1

#### Test 1 — 19005_2_6_4_1_t01

**Rule:** A Widget annotation dictionary shall not contain the A or AA keys

**Target:** Widget annotation

**Complexity:** Low
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve widget annotation dictionaries.
2. Remove /AA key.
3. Allocate modified widget dicts.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 19005_2_6_4_1_t02

**Rule:** A Field dictionary shall not contain the A or AA keys

**Target:** Form field dict

**Complexity:** Low

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 3 — 19005_2_6_4_1_t03

**Rule:** The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false

**Target:** PDAcroForm

**Complexity:** High
**Existing fixer:** `fixer_needappearances.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_needappearances.go`.

### §6.4.2

#### Test 1 — 19005_2_6_4_2_t01

**Rule:** The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-2 file, if present, shall not contain the XFA key

**Target:** PDAcroForm

**Complexity:** Low
**Existing fixer:** `fixer_xfa.go`

**Procedure:**

1. Resolve /AcroForm dictionary.
2. Remove /XFA key.
3. Allocate modified AcroForm.
4. Existing implementation: `fixer_xfa.go`.

#### Test 2 — 19005_2_6_4_2_t02

**Rule:** A document's Catalog shall not contain the NeedsRendering key

**Target:** Document trailer/xref

**Complexity:** Low

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

### §6.4.3

#### Test 1 — 19005_2_6_4_3_t01

**Rule:** When computing the digest for the file, it shall be computed over the entire file, including the signature dictionary but excluding the PDF Signature itself

**Target:** PDSignature

**Complexity:** Medium

**Procedure:**

1. Rebuild the signature with valid PKCS#7 DER-encoded data.
2. Ensure ByteRange covers the entire file excluding the signature placeholder.
3. Re-sign with the certificate and private key.
4. Note: requires cryptographic signing credentials.

#### Test 2 — 19005_2_6_4_3_t02

**Rule:** The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, it shall include the signer's X.509 signing certificate

**Target:** PKCSDataObject

**Complexity:** Medium

**Procedure:**

1. Rebuild the signature with valid PKCS#7 DER-encoded data.
2. Ensure ByteRange covers the entire file excluding the signature placeholder.
3. Re-sign with the certificate and private key.
4. Note: requires cryptographic signing credentials.

#### Test 3 — 19005_2_6_4_3_t03

**Rule:** The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, there shall only be a single signer (e.g. a single \

**Target:** PKCSDataObject

**Complexity:** Medium

**Procedure:**

1. Rebuild the signature with valid PKCS#7 DER-encoded data.
2. Ensure ByteRange covers the entire file excluding the signature placeholder.
3. Re-sign with the certificate and private key.
4. Note: requires cryptographic signing credentials.

### §6.5.1

#### Test 1 — 19005_2_6_5_1_t01

**Rule:** The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted

**Target:** Action dict

**Complexity:** Medium
**Existing fixer:** `fixer_javascript.go`

**Procedure:**

1. Walk catalog /OpenAction, /AA, page /AA, and annotation /A and /AA entries.
2. Remove any action with /S = JavaScript, Launch, Sound, Movie, ResetForm, ImportData, or Hide.
3. For /AA dicts, remove only the entries with forbidden action types.
4. Allocate modified objects.
5. Existing implementation: `fixer_javascript.go`.

#### Test 2 — 19005_2_6_5_1_t02

**Rule:** Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted

**Target:** Action dict

**Complexity:** Medium

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.

### §6.5.2

#### Test 1 — 19005_2_6_5_2_t01

**Rule:** The document's Catalog shall not include an AA entry for an additional-actions dictionary

**Target:** PDDocument

**Complexity:** Medium

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.

#### Test 2 — 19005_2_6_5_2_t02

**Rule:** The Page dictionary shall not include an AA entry for an additional-actions dictionary

**Target:** Page dict

**Complexity:** Medium

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.

### §6.6.2.1

#### Test 1 — 19005_2_6_6_2_1_t01

**Rule:** The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-1:2008, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Check if catalog has /Metadata.
2. If missing, generate XMP from /Info dict values.
3. If present but invalid, repair the XMP XML structure.
4. Allocate XMP stream and update catalog.
5. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_2_6_6_2_1_t02

**Rule:** The bytes attribute shall not be used in the header of an XMP packet

**Target:** XMP metadata stream

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 3 — 19005_2_6_6_2_1_t03

**Rule:** The encoding attribute shall not be used in the header of an XMP packet

**Target:** XMP metadata stream

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 4 — 19005_2_6_6_2_1_t04

**Rule:** All metadata streams present in the PDF shall conform to the XMP Specification. All content of all XMP packets shall be well-formed, as defined by Extensible Markup Language (XML) 1.0 (Third Edition), 2.1, and the RDF/XML Syntax Specification (Revised)

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve ExtGState or annotation dict.
2. Set /CA and /ca to 1.0 (fully opaque).
3. Allocate modified object.
4. Existing implementation: `fixer_xmp.go`.

#### Test 5 — 19005_2_6_6_2_1_t05

**Rule:** All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

### §6.6.2.3.1

#### Test 1 — 19005_2_6_6_2_3_1_t01

**Rule:** All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2

**Target:** XMPProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_2_6_6_2_3_1_t02

**Rule:** All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2

**Target:** XMPProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

### §6.6.2.3.2

#### Test 1 — 19005_2_6_6_2_3_2_t01

**Rule:** Extension schemas shall be specified using the PDF/A extension schema container schema defined in 6.6.2.3.3. All fields described in each of the tables in 6.6.2.3.3 shall be present in any extension schema container schema

**Target:** ExtensionSchemaObject

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

### §6.6.2.3.3

#### Test 1 — 19005_2_6_6_2_3_3_t01

**Rule:** The extension schema container schema uses the namespace URI \

**Target:** ExtensionSchemasContainer

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_2_6_6_2_3_3_t02

**Rule:** Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 3 — 19005_2_6_6_2_3_3_t03

**Rule:** Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 4 — 19005_2_6_6_2_3_3_t04

**Rule:** Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 5 — 19005_2_6_6_2_3_3_t05

**Rule:** Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 6 — 19005_2_6_6_2_3_3_t06

**Rule:** Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 7 — 19005_2_6_6_2_3_3_t07

**Rule:** Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 8 — 19005_2_6_6_2_3_3_t08

**Rule:** Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 9 — 19005_2_6_6_2_3_3_t09

**Rule:** Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty'

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 10 — 19005_2_6_6_2_3_3_t10

**Rule:** Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 11 — 19005_2_6_6_2_3_3_t11

**Rule:** Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 12 — 19005_2_6_6_2_3_3_t12

**Rule:** Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 13 — 19005_2_6_6_2_3_3_t13

**Rule:** Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 14 — 19005_2_6_6_2_3_3_t14

**Rule:** Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 15 — 19005_2_6_6_2_3_3_t15

**Rule:** Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 16 — 19005_2_6_6_2_3_3_t16

**Rule:** Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'

**Target:** ExtensionSchemaField

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 17 — 19005_2_6_6_2_3_3_t17

**Rule:** Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined

**Target:** ExtensionSchemaField

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 18 — 19005_2_6_6_2_3_3_t18

**Rule:** Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'

**Target:** ExtensionSchemaField

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

### §6.6.4

#### Test 1 — 19005_2_6_6_4_t01

**Rule:** The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_2_6_6_4_t02

**Rule:** The value of \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 3 — 19005_2_6_6_4_t03

**Rule:** A Level A conforming file shall specify the value of \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 4 — 19005_2_6_6_4_t04

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 5 — 19005_2_6_6_4_t05

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 6 — 19005_2_6_6_4_t06

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 7 — 19005_2_6_6_4_t07

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

### §6.7.2.2

#### Test 1 — 19005_2_6_7_2_2_t01

**Rule:** The document catalog dictionary shall include a MarkInfo dictionary containing an entry, Marked, whose value shall be true

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /MarkInfo << /Marked true >>.
3. Allocate updated catalog.
4. Existing implementation: `fixer_markinfo.go`.

### §6.7.3.3

#### Test 1 — 19005_2_6_7_3_3_t01

**Rule:** The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document's Catalog dictionary, as described in ISO 32000-1:2008, 14.7

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_structtree.go`

**Procedure:**

1. Create a minimal StructTreeRoot with a Document root element.
2. Add /ParentTree number tree for MCID-to-structure-element mapping.
3. For PDF/UA-2, add /Namespaces array with PDF 1.7 and 2.0 namespace dicts.
4. Set catalog /StructTreeRoot.
5. Allocate all new objects.
6. Existing implementation: `fixer_structtree.go`.

### §6.7.3.4

#### Test 1 — 19005_2_6_7_3_4_t01

**Rule:** All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in ISO 32000-1:2008, 14.8.4, in the role map dictionary of the structure tree root

**Target:** SENonStandard

**Complexity:** Medium

**Procedure:**

1. Identify custom structure element types not in the standard set.
2. Add /RoleMap entries mapping each custom type to a standard type (P, Span, Div, etc.).
3. Or add /NS on each structure element pointing to the appropriate namespace.
4. Allocate modified StructTreeRoot.

#### Test 2 — 19005_2_6_7_3_4_t02

**Rule:** A circular mapping shall not exist

**Target:** SENonStandard

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

#### Test 3 — 19005_2_6_7_3_4_t03

**Rule:** Standard tags shall not be remapped to a non-standard type

**Target:** SENonStandard

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

### §6.7.4

#### Test 1 — 19005_2_6_7_4_t01

**Rule:** If the Lang entry is present in the document's Catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as described in ISO 32000-1:2008, 14.9.2. A language identifier shall either be the empty text string, to indicate that the language is unknown, or a Language-Tag as defined in RFC 3066, Tags for the Identification of Languages

**Target:** CosLang

**Complexity:** Medium

**Procedure:**

1. Detect list structures from content analysis (bullet/number patterns).
2. Create L/LI/Lbl/LBody structure elements.
3. Mark content streams with BDC/EMC for each list item.

### §6.8

#### Test 2 — 19005_2_6_8_t02

**Rule:** The file specification dictionary for an embedded file shall contain the F and UF keys

**Target:** File specification dict

**Complexity:** High

**Procedure:**

1. Resolve file specification dictionaries.
2. Ensure embedded file streams have /Subtype with MIME type.
3. Add /AFRelationship if required.
4. For PDF/A-1, remove /EF entirely (embedded files forbidden).
5. Allocate modified file spec dicts.

#### Test 5 — 19005_2_6_8_t05

**Rule:** A file specification dictionary, as defined in ISO 32000-1:2008, 7.11.3, may contain the EF key, provided that the embedded file is compliant with either ISO 19005-1 or this part of ISO 19005

**Target:** Embedded file stream

**Complexity:** High

**Procedure:**

1. Resolve all file specification dicts in the document.
2. Remove /EF key from each (embedded files forbidden in PDF/A-1).
3. Allocate modified file spec dicts.

### §6.9

#### Test 1 — 19005_2_6_9_t01

**Rule:** Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key

**Target:** PDOCConfig

**Complexity:** Medium
**Existing fixer:** `fixer_catalog.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Remove /OCProperties key.
3. Allocate updated catalog.
4. Existing implementation: `fixer_catalog.go`.

#### Test 2 — 19005_2_6_9_t02

**Rule:** Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-2 file

**Target:** PDOCConfig

**Complexity:** Medium

**Procedure:**

1. Resolve OCG configuration dictionaries.
2. Add /Name key to each config dict.
3. Allocate modified config dicts.

#### Test 3 — 19005_2_6_9_t03

**Rule:** If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file

**Target:** PDOCConfig

**Complexity:** Medium

**Procedure:**

1. Resolve OCG configuration dictionaries.
2. Add /Name key to each config dict.
3. Allocate modified config dicts.

#### Test 4 — 19005_2_6_9_t04

**Rule:** The AS key shall not appear in any optional content configuration dictionary

**Target:** PDOCConfig

**Complexity:** Medium

**Procedure:**

1. Resolve OCG configuration dictionaries.
2. Add /Name key to each config dict.
3. Allocate modified config dicts.

### §6.10

#### Test 1 — 19005_2_6_10_t01

**Rule:** There shall be no AlternatePresentations entry in the document's name dictionary

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_catalog.go`

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.
3. Existing implementation: `fixer_catalog.go`.

#### Test 2 — 19005_2_6_10_t02

**Rule:** There shall be no PresSteps entry in any Page dictionary

**Target:** Page dict

**Complexity:** Medium
**Existing fixer:** `fixer_pressteps.go`

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.
3. Existing implementation: `fixer_pressteps.go`.

### §6.11

#### Test 1 — 19005_2_6_11_t01

**Rule:** The document catalog shall not contain the Requirements key

**Target:** Document trailer/xref

**Complexity:** Low
**Existing fixer:** `fixer_requirements.go`

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.
3. Existing implementation: `fixer_requirements.go`.


## PDF/A-3 (ISO 19005-3)

### §6.1.2

#### Test 1 — 19005_3_6_1_2_t01

**Rule:** The file header shall begin at byte zero and shall consist of \

**Target:** Document trailer/xref

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 2 — 19005_3_6_1_2_t02

**Rule:** The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127

**Target:** Document trailer/xref

**Complexity:** Medium

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

### §6.1.3

#### Test 1 — 19005_3_6_1_3_t01

**Rule:** The file trailer dictionary shall contain the ID keyword whose value shall be File Identifiers as defined in ISO 32000-1:2008, 14.4

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_trailerid.go`

**Procedure:**

1. Compute MD5 hash of the document content (excluding the ID entry itself).
2. Create a two-element /ID array with the hash as both elements.
3. Add /ID to the trailer dictionary.
4. Existing implementation: `fixer_trailerid.go`.

#### Test 2 — 19005_3_6_1_3_t02

**Rule:** The keyword Encrypt shall not be used in the trailer dictionary

**Target:** CosTrailer

**Complexity:** Low
**Existing fixer:** `fixer_trailerid.go`

**Procedure:**

1. Remove /Encrypt from the file trailer.
2. Decrypt all encrypted strings and streams.
3. Full rewrite outputs decrypted content.
4. Existing implementation: `fixer_trailerid.go`.

#### Test 3 — 19005_3_6_1_3_t03

**Rule:** No data can follow the last end-of-file marker except a single optional end-of-line marker as described in ISO 32000-1:2008, 7.5.5

**Target:** Document trailer/xref

**Complexity:** Medium

**Procedure:**

1. Full rewrite terminates at %%EOF with a single EOL marker.
2. No post-EOF data is written.

### §6.1.4

#### Test 2 — 19005_3_6_1_4_t02

**Rule:** The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker

**Target:** CosXRef

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

### §6.1.6

#### Test 1 — 19005_3_6_1_6_t01

**Rule:** Hexadecimal strings shall contain an even number of non-white-space characters

**Target:** String object

**Complexity:** Medium

**Procedure:**

1. Full rewrite re-serializes all hex strings with even-length digit pairs.
2. Invalid characters are stripped during re-serialization.

#### Test 2 — 19005_3_6_1_6_t02

**Rule:** A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f)

**Target:** String object

**Complexity:** Medium

**Procedure:**

1. Full rewrite re-serializes all hex strings with even-length digit pairs.
2. Invalid characters are stripped during re-serialization.

### §6.1.7.1

#### Test 1 — 19005_3_6_1_7_1_t01

**Rule:** The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword

**Target:** Stream object

**Complexity:** Medium

**Procedure:**

1. Full rewrite writes "stream\r\n" (CR+LF) after the stream keyword.
2. Writes EOL marker before "endstream".

#### Test 2 — 19005_3_6_1_7_1_t02

**Rule:** The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED (0Ah) character. The endstream keyword shall be preceded by an EOL marker

**Target:** Stream object

**Complexity:** Medium

**Procedure:**

1. Full rewrite writes "stream\r\n" (CR+LF) after the stream keyword.
2. Writes EOL marker before "endstream".

#### Test 3 — 19005_3_6_1_7_1_t03

**Rule:** A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys

**Target:** Stream object

**Complexity:** Low

**Procedure:**

1. Resolve the stream dictionary.
2. Delete /F, /FFilter, /FDecodeParms keys.
3. If /F referenced an external file, read the file data and embed it as the stream body.
4. Allocate the modified stream.

### §6.1.7.2

#### Test 1 — 19005_3_6_1_7_2_t01

**Rule:** All standard stream filters listed in ISO 32000-1:2008, 7.4, Table 6 may be used, with the exception of LZWDecode. In addition, the Crypt filter shall not be used unless the value of the Name key in the decode parameters dictionary is Identity. Filters that are not listed in ISO 32000-1:2008, 7.4, Table 6 shall not be used

**Target:** CosFilter

**Complexity:** Low
**Existing fixer:** `fixer_lzw.go`

**Procedure:**

1. Decode the LZW stream to raw bytes.
2. Re-encode as FlateDecode (zlib compression).
3. Replace /Filter with /FlateDecode, remove /DecodeParms, update /Length.
4. Allocate the new stream.
5. Existing implementation: `fixer_lzw.go`.

### §6.1.8

#### Test 1 — 19005_3_6_1_8_t01

**Rule:** Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences

**Target:** CosUnicodeName

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §6.1.9

#### Test 1 — 19005_3_6_1_9_t01

**Rule:** The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded by an EOL marker. The obj and endobj keywords shall each be followed by an EOL marker

**Target:** CosIndirect

**Complexity:** Medium

**Procedure:**

1. Full rewrite serializes "N G obj\n" with single spaces and EOL markers.

### §6.1.10

#### Test 1 — 19005_3_6_1_10_t01

**Rule:** The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-1:2008, Table 6, or an array containing any such value

**Target:** CosIIFilter

**Complexity:** Medium
**Existing fixer:** `fixer_lzw.go`

**Procedure:**

1. Decode the LZW stream to raw bytes.
2. Re-encode as FlateDecode (zlib compression).
3. Replace /Filter with /FlateDecode, remove /DecodeParms, update /Length.
4. Allocate the new stream.
5. Existing implementation: `fixer_lzw.go`.

### §6.1.12

#### Test 1 — 19005_3_6_1_12_t01

**Rule:** No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-1:2008, 12.8.4, Table 258)

**Target:** PDPerms

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 2 — 19005_3_6_1_12_t02

**Rule:** If DocMDP is present, then the Signature References dictionary (ISO 32000-1:2008, 12.8.1, Table 253) shall not contain the keys DigestLocation, DigestMethod, and DigestValue

**Target:** PDSigRef

**Complexity:** Low

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §6.1.13

#### Test 1 — 19005_3_6_1_13_t01

**Rule:** A conforming file shall not contain any integer greater than 2147483647. A conforming file shall not contain any integer less than -2147483648

**Target:** Integer

**Complexity:** Low

**Procedure:**

1. Scan all objects for values exceeding the spec limit.
2. For each out-of-range value, determine if clamping is safe (coordinate vs. object reference).
3. Clamp to the spec maximum/minimum.
4. Allocate modified objects.
5. Note: clamping may alter visual appearance for coordinates, or corrupt references for object numbers.

#### Test 2 — 19005_3_6_1_13_t02

**Rule:** A conforming file shall not contain any real number outside the range of +/-3.403 x 10^38

**Target:** Real number

**Complexity:** Low

**Procedure:**

1. Scan all objects for values exceeding the spec limit.
2. For each out-of-range value, determine if clamping is safe (coordinate vs. object reference).
3. Clamp to the spec maximum/minimum.
4. Allocate modified objects.
5. Note: clamping may alter visual appearance for coordinates, or corrupt references for object numbers.

#### Test 3 — 19005_3_6_1_13_t03

**Rule:** A conforming file shall not contain any string longer than 32767 bytes

**Target:** String object

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 4 — 19005_3_6_1_13_t04

**Rule:** A conforming file shall not contain any name longer than 127 bytes

**Target:** Name object

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 5 — 19005_3_6_1_13_t05

**Rule:** A conforming file shall not contain any real number closer to zero than +/-1.175 x 10^(-38)

**Target:** Real number

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 7 — 19005_3_6_1_13_t07

**Rule:** A conforming file shall not contain more than 8388607 indirect objects

**Target:** Document trailer/xref

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 8 — 19005_3_6_1_13_t08

**Rule:** A conforming file shall not nest q/Q pairs by more than 28 nesting levels

**Target:** Op_q_gsave

**Complexity:** Medium

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 9 — 19005_3_6_1_13_t09

**Rule:** A conforming file shall not contain a DeviceN colour space with more than 32 colourants

**Target:** PDDeviceN

**Complexity:** Low

**Procedure:**

1. Resolve the Separation/DeviceN color space array.
2. Check that the alternate color space is device-independent.
3. If device-dependent, replace with an ICCBased equivalent.
4. For DeviceN with Colorants, ensure all colorant tint transforms are consistent.
5. Allocate modified color space arrays.

#### Test 10 — 19005_3_6_1_13_t10

**Rule:** A conforming file shall not contain a CID value greater than 65535

**Target:** CMap

**Complexity:** Low

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

#### Test 11 — 19005_3_6_1_13_t11

**Rule:** The size of any of the page boundaries described in ISO 32000-1:2008, 14.11.2 shall not be less than 3 units in either direction, nor shall it be greater than 14 400 units in either direction

**Target:** CosBBox

**Complexity:** Medium

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

### §6.2.2

#### Test 1 — 19005_3_6_2_2_t01

**Rule:** Content streams shall not contain any operators not defined in ISO 32000-1 even if such operators are bracketed by the BX/EX compatibility operators

**Target:** Op_Undefined

**Complexity:** Low

**Procedure:**

1. Parse content stream operators.
2. Remove or replace undefined operators.
3. Re-serialize the content stream.
4. Allocate modified stream.

#### Test 2 — 19005_3_6_2_2_t02

**Rule:** A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-1:2008, 7.8.3

**Target:** Content stream

**Complexity:** High

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.

### §6.2.3

#### Test 1 — 19005_3_6_2_3_t01

**Rule:** The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = \

**Target:** ICCOutputProfile

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 2 — 19005_3_6_2_3_t02

**Rule:** If a file's OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X-4 or PDF/E-1, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream

**Target:** OutputIntent dict

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 3 — 19005_3_6_2_3_t03

**Rule:** In addition, the DestOutputProfileRef key, as defined in ISO 15930-7:2010, Annex A, shall not be present in any PDF/X OutputIntent

**Target:** OutputIntent dict

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

### §6.2.4.2

#### Test 1 — 19005_3_6_2_4_2_t01

**Rule:** The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1

**Target:** ICCInputProfile

**Complexity:** Medium
**Existing fixer:** `fixer_icc.go`

**Procedure:**

1. Read the ICC profile stream bytes.
2. Validate header: device class (bytes 12-15), color space (bytes 16-19), version (byte 8).
3. If invalid, replace with a known-good profile (sRGB or CMYK).
4. Allocate replacement stream.
5. Existing implementation: `fixer_icc.go`.

#### Test 2 — 19005_3_6_2_4_2_t02

**Rule:** Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and overprinting for fill is set to true, or both

**Target:** ICCBased color space

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Read the ICC profile stream bytes.
2. Validate header: device class (bytes 12-15), color space (bytes 16-19), version (byte 8).
3. If invalid, replace with a known-good profile (sRGB or CMYK).
4. Allocate replacement stream.
5. Existing implementation: `fixer_graphics.go`.

### §6.2.4.3

#### Test 2 — 19005_3_6_2_4_3_t02

**Rule:** DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used, or if the file has a PDF/A OutputIntent that contains an RGB destination profile

**Target:** PDDeviceRGB

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 3 — 19005_3_6_2_4_3_t03

**Rule:** DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set or if a DeviceN-based DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or the file has a PDF/A OutputIntent that contains a CMYK destination profile

**Target:** PDDeviceCMYK

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 4 — 19005_3_6_2_4_3_t04

**Rule:** DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is present

**Target:** PDDeviceGray

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

### §6.2.4.4

#### Test 1 — 19005_3_6_2_4_4_t01

**Rule:** For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present

**Target:** PDDeviceN

**Complexity:** Medium

**Procedure:**

1. Resolve the Separation/DeviceN color space array.
2. Check that the alternate color space is device-independent.
3. If device-dependent, replace with an ICCBased equivalent.
4. For DeviceN with Colorants, ensure all colorant tint transforms are consistent.
5. Allocate modified color space arrays.

#### Test 2 — 19005_3_6_2_4_4_t02

**Rule:** All Separation arrays within a single PDF/A-3 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, rather than the computational result of the use of those PDF objects. Compression and whether or not an object is direct or indirect shall be ignored

**Target:** PDSeparation

**Complexity:** Medium

**Procedure:**

1. Resolve the Separation/DeviceN color space array.
2. Check that the alternate color space is device-independent.
3. If device-dependent, replace with an ICCBased equivalent.
4. For DeviceN with Colorants, ensure all colorant tint transforms are consistent.
5. Allocate modified color space arrays.

### §6.2.5

#### Test 1 — 19005_3_6_2_5_t01

**Rule:** An ExtGState dictionary shall not contain the TR key

**Target:** ExtGState dict

**Complexity:** Low
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /TR key, or set /TR2 to /Default.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

#### Test 2 — 19005_3_6_2_5_t02

**Rule:** An ExtGState dictionary shall not contain the TR2 key with a value other than Default

**Target:** ExtGState dict

**Complexity:** Low

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /TR key, or set /TR2 to /Default.
3. Allocate modified ExtGState.

#### Test 3 — 19005_3_6_2_5_t03

**Rule:** An ExtGState dictionary shall not contain the HTP key

**Target:** ExtGState dict

**Complexity:** Low

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.

#### Test 4 — 19005_3_6_2_5_t04

**Rule:** All halftones in a conforming PDF/A-3 file shall have the value 1 or 5 for the HalftoneType key

**Target:** PDHalftone

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /HT key or set to a default halftone.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

#### Test 5 — 19005_3_6_2_5_t05

**Rule:** Halftones in a conforming PDF/A-3 file shall not contain a HalftoneName key

**Target:** PDHalftone

**Complexity:** Low
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /HT key or set to a default halftone.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

#### Test 6 — 19005_3_6_2_5_t06

**Rule:** The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-1

**Target:** PDHalftone

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /HT key or set to a default halftone.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

### §6.2.6

#### Test 1 — 19005_3_6_2_6_t01

**Rule:** Where a rendering intent is specified, its value shall be one of the four values defined in ISO 32000-1:2008, Table 70: RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation

**Target:** CosRenderingIntent

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Set /RI to a permitted value: RelativeColorimetric, AbsoluteColorimetric, Perceptual, or Saturation.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

### §6.2.8

#### Test 1 — 19005_3_6_2_8_t01

**Rule:** An Image dictionary shall not contain the Alternates key

**Target:** PDXImage

**Complexity:** Low

**Procedure:**

1. Identify Figure structure elements without /Alt.
2. Generate alt text via OCR on the image content, or set a placeholder.
3. Add /Alt to the structure element dictionary.
4. Allocate modified structure element.

#### Test 2 — 19005_3_6_2_8_t02

**Rule:** An Image dictionary shall not contain the OPI key

**Target:** PDXImage

**Complexity:** Low
**Existing fixer:** `fixer_images.go`

**Procedure:**

1. Resolve Image XObject dictionaries.
2. Remove /OPI key.
3. Allocate modified image dict.
4. Existing implementation: `fixer_images.go`.

#### Test 3 — 19005_3_6_2_8_t03

**Rule:** If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key shall have a value of false

**Target:** PDXImage

**Complexity:** Medium
**Existing fixer:** `fixer_images.go`

**Procedure:**

1. Resolve Image XObject dictionaries.
2. Set /Interpolate to false.
3. Allocate modified XObject.
4. Existing implementation: `fixer_images.go`.

#### Test 4 — 19005_3_6_2_8_t04

**Rule:** If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16

**Target:** PDXImage

**Complexity:** Medium

**Procedure:**

1. Resolve Image XObject dictionary.
2. If /BitsPerComponent is invalid, re-encode image data with a permitted value (1, 2, 4, or 8).
3. For image masks, set /BitsPerComponent to 1.
4. Allocate modified image stream.

#### Test 5 — 19005_3_6_2_8_t05

**Rule:** If an image mask dictionary contains the BitsPerComponent key, its value shall be 1

**Target:** PDMaskImage

**Complexity:** Medium

**Procedure:**

1. Resolve Image XObject dictionary.
2. If /BitsPerComponent is invalid, re-encode image data with a permitted value (1, 2, 4, or 8).
3. For image masks, set /BitsPerComponent to 1.
4. Allocate modified image stream.

### §6.2.8.3

#### Test 1 — 19005_3_6_2_8_3_t01

**Rule:** The number of colour channels in the JPEG2000 data shall be 1, 3 or 4

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

#### Test 2 — 19005_3_6_2_8_3_t02

**Rule:** If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

#### Test 3 — 19005_3_6_2_8_3_t03

**Rule:** The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming reader shall use only that colour space and shall ignore all other colour space specifications

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.

#### Test 4 — 19005_3_6_2_8_3_t04

**Rule:** JPEG2000 enumerated colour space 19 (CIEJab) shall not be used

**Target:** JPEG2000

**Complexity:** Low

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

#### Test 5 — 19005_3_6_2_8_3_t05

**Rule:** The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

### §6.2.9

#### Test 1 — 19005_3_6_2_9_t01

**Rule:** A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key

**Target:** PDXForm

**Complexity:** Low
**Existing fixer:** `fixer_images.go`

**Procedure:**

1. Resolve Form XObject dictionaries.
2. Remove /Subtype2 and /PS keys.
3. Allocate modified Form XObject dicts.
4. Existing implementation: `fixer_images.go`.

#### Test 2 — 19005_3_6_2_9_t02

**Rule:** A conforming file shall not contain any reference XObjects

**Target:** PDXForm

**Complexity:** Low

**Procedure:**

1. Resolve XObject dictionaries.
2. Remove /Ref key from Form XObjects.
3. Or inline the referenced content.
4. Allocate modified XObject dicts.

#### Test 3 — 19005_3_6_2_9_t03

**Rule:** A conforming file shall not contain any PostScript XObjects

**Target:** PDXObject

**Complexity:** Low

**Procedure:**

1. Walk all XObject resources.
2. Remove or replace PostScript XObjects with equivalent Form XObjects.
3. Allocate modified resource dicts.

### §6.2.10

#### Test 1 — 19005_3_6_2_10_t01

**Rule:** Only blend modes that are specified in ISO 32000-1:2008 shall be used for the value of the BM key in an extended graphic state dictionary

**Target:** CosBM

**Complexity:** Medium
**Existing fixer:** `fixer_transparency.go`

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.
4. Existing implementation: `fixer_transparency.go`.

#### Test 2 — 19005_3_6_2_10_t02

**Rule:** If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space

**Target:** Page dict

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

### §6.2.11.2

#### Test 1 — 19005_3_6_2_11_2_t01

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 2 — 19005_3_6_2_11_2_t02

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Required) The type of font; must be \

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 3 — 19005_3_6_2_11_2_t03

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (Required) The PostScript name of the font

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 4 — 19005_3_6_2_11_2_t04

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array

**Target:** PDSimpleFont

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.
5. Existing implementation: `fixer_fonts.go`.

#### Test 5 — 19005_3_6_2_11_2_t05

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array

**Target:** PDSimpleFont

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.
5. Existing implementation: `fixer_fonts.go`.

#### Test 6 — 19005_3_6_2_11_2_t06

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths

**Target:** PDSimpleFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 7 — 19005_3_6_2_11_2_t07

**Rule:** All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.7 are Type1C - Type 1–equivalent font program represented in the Compact Font Format (CFF), CIDFontType0C - Type 0 CIDFont program represented in the Compact Font Format (CFF) and OpenType - OpenType® font program, as described in the OpenType Specification v.1.4

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §6.2.11.3.1

#### Test 1 — 19005_3_6_2_11_3_1_t01

**Rule:** For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary is Identity-H or Identity-V, any values of Registry, Ordering, and Supplement may be used in the CIDSystemInfo entry of the CIDFont. - Otherwise, the corresponding Registry and Ordering strings in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be less than or equal to the Supplement key in the CIDSystemInfo dictionary of the CMap

**Target:** Type 0 composite font

**Complexity:** High

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §6.2.11.3.2

#### Test 1 — 19005_3_6_2_11_3_2_t01

**Rule:** ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2008, 9.7.4, Table 117

**Target:** CIDFont

**Complexity:** High

**Procedure:**

1. Check if CIDFont has /CIDToGIDMap.
2. If missing, add /CIDToGIDMap /Identity (when CIDs map directly to GIDs).
3. Or generate a stream mapping CIDs to GIDs based on the font program cmap table.
4. Allocate modified CIDFont dictionary.

### §6.2.11.3.3

#### Test 1 — 19005_3_6_2_11_3_3_t01

**Rule:** All CMaps used within a PDF/A-3 file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 2 — 19005_3_6_2_11_3_3_t02

**Rule:** For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Read /WMode from both the CMap dictionary and the embedded CMap stream.
2. Set both to the same value (0 for horizontal, 1 for vertical).
3. Allocate modified CMap stream.

#### Test 3 — 19005_3_6_2_11_3_3_t03

**Rule:** A CMap shall not reference any other CMap except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §6.2.11.4.1

#### Test 1 — 19005_3_6_2_11_4_1_t01

**Rule:** The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9

**Target:** PDFont

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 2 — 19005_3_6_2_11_4_1_t02

**Rule:** Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType fonts that are to be rendered, character codes shall be able to be mapped to glyphs according to ISO 32000-1:2008, 9.6.6.4 without the use of a non-standard mapping chosen by the conforming processor

**Target:** Glyph

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

### §6.2.11.4.2

#### Test 1 — 19005_3_6_2_11_4_2_t01

**Rule:** If the FontDescriptor dictionary of an embedded Type 1 font contains a CharSet string, then it shall list the character names of all glyphs present in the font program, regardless of whether a glyph in the font is referenced or used by the PDF or not

**Target:** Type 1 font

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the Type 1 font program from /FontFile.
2. Parse the CharStrings dictionary to enumerate all glyph names.
3. Build a /CharSet string: "/" + glyph names joined by "/".
4. Set /CharSet on the FontDescriptor.
5. Allocate modified FontDescriptor.
6. Existing implementation: `fixer_fonts.go`.

#### Test 2 — 19005_3_6_2_11_4_2_t02

**Rule:** If the FontDescriptor dictionary of an embedded CID font contains a CIDSet stream, then it shall identify all CIDs which are present in the font program, regardless of whether a CID in the font is referenced or used by the PDF or not

**Target:** CIDFont

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the font program.
2. Enumerate all valid CIDs (GIDs with non-empty outlines).
3. Build a CIDSet bitfield: bit N of byte M represents CID (M*8 + 7-N).
4. Compress with FlateDecode and set as /CIDSet stream on FontDescriptor.
5. Allocate modified FontDescriptor and stream.
6. Existing implementation: `fixer_fonts.go`.

### §6.2.11.5

#### Test 1 — 19005_3_6_2_11_5_t01

**Rule:** For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract binary widths from the font program (hmtx table for TrueType, charstring widths for CFF).
2. Read /UnitsPerEm from the head table.
3. For each charcode, compute expected PDF width: binaryWidth * 1000 / unitsPerEm.
4. Compare with the /Widths array (simple fonts) or /W array (CIDFonts).
5. Replace mismatched entries with the correct values.
6. Allocate modified font dictionary.
7. Existing implementation: `fixer_fonts.go`.

### §6.2.11.6

#### Test 1 — 19005_3_6_2_11_6_t01

**Rule:** For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain one or several non-symbolic cmap entries such that all necessary glyph lookups can be carried out

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 2 — 19005_3_6_2_11_6_t02

**Rule:** All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary that is the value of the Encoding key in the Font dictionary. In addition, all non-symbolic TrueType fonts shall not define a Differences array unless all of the glyph names in the Differences array are listed in the Adobe Glyph List and, if the font program is embedded, it shall contain at least the Microsoft Unicode (3,1 – Platform ID = 3, Encoding ID = 1) encoding in the 'cmap' table

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 3 — 19005_3_6_2_11_6_t03

**Rule:** Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary

**Target:** TrueType font

**Complexity:** Low

**Procedure:**

1. Check if the font is symbolic (Flags bit 3 set in FontDescriptor).
2. If symbolic: remove /Encoding from the font dictionary.
3. If non-symbolic: set /Encoding to /WinAnsiEncoding or /MacRomanEncoding.
4. Allocate modified font dictionary.

#### Test 4 — 19005_3_6_2_11_6_t04

**Rule:** The 'cmap' table in the embedded font program for a symbolic TrueType font shall contain either exactly one encoding or it shall contain, at least, the Microsoft Symbol (3,0 - Platform ID=3, Encoding ID=0) encoding

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

### §6.2.11.7.2

#### Test 1 — 19005_3_6_2_11_7_2_t01

**Rule:** The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 19005-3, 6.2.11.7.2

**Target:** Glyph

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the embedded font program from /FontFile, /FontFile2, or /FontFile3.
2. Parse the cmap table (TrueType) or charset (CFF) to build GID-to-Unicode mapping.
3. Construct charcode-to-Unicode mapping using the font encoding.
4. Generate a CMap stream with beginbfchar entries (chunks of 100).
5. Add the CMap as /ToUnicode on the font dictionary.
6. Allocate both the CMap stream and modified font dict.
7. Existing implementation: `fixer_fonts.go`.

#### Test 2 — 19005_3_6_2_11_7_2_t02

**Rule:** The Unicode values specified in the ToUnicode CMap shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the embedded font program from /FontFile, /FontFile2, or /FontFile3.
2. Parse the cmap table (TrueType) or charset (CFF) to build GID-to-Unicode mapping.
3. Construct charcode-to-Unicode mapping using the font encoding.
4. Generate a CMap stream with beginbfchar entries (chunks of 100).
5. Add the CMap as /ToUnicode on the font dictionary.
6. Allocate both the CMap stream and modified font dict.
7. Existing implementation: `fixer_fonts.go`.

### §6.2.11.7.3

#### Test 1 — 19005_3_6_2_11_7_3_t01

**Rule:** For any character, regardless of its rendering mode, that is mapped to a code or codes in the Unicode Private Use Area (PUA), an ActualText entry as described in ISO 32000-1:2008, 14.9.4 shall be present for this character or a sequence of characters of which such a character is a part

**Target:** Glyph

**Complexity:** Medium

**Procedure:**

1. Identify structure elements with PUA-mapped characters.
2. Determine the actual Unicode text from font encoding or ToUnicode CMap.
3. Add /ActualText with the resolved text.
4. Allocate modified structure element.

### §6.2.11.8

#### Test 1 — 19005_3_6_2_11_8_t01

**Rule:** A PDF/A-3 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream

**Target:** Glyph

**Complexity:** Low

**Procedure:**

1. Parse content stream operators.
2. Remove or replace undefined operators.
3. Re-serialize the content stream.
4. Allocate modified stream.

### §6.3.1

#### Test 1 — 19005_3_6_3_1_t01

**Rule:** Annotation types not defined in ISO 32000-1 shall not be permitted. Additionally, the 3D, Sound, Screen and Movie types shall not be permitted

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Walk catalog /OpenAction, /AA, page /AA, and annotation /A and /AA entries.
2. Remove any action with /S = JavaScript, Launch, Sound, Movie, ResetForm, ImportData, or Hide.
3. For /AA dicts, remove only the entries with forbidden action types.
4. Allocate modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §6.3.2

#### Test 1 — 19005_3_6_3_2_t01

**Rule:** Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve Popup annotations.
2. Ensure /Parent references the parent annotation.
3. Allocate modified Popup annotation dict.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 19005_3_6_3_2_t02

**Rule:** If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.
4. Existing implementation: `fixer_annotflags.go`.

### §6.3.3

#### Test 1 — 19005_3_6_3_3_t01

**Rule:** Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: - annotations where the value of the Rect key consists of an array where value 1 is equal to value 3 and value 2 is equal to value 4; - annotations whose Subtype value is Popup or Link

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Detect list structures from content analysis (bullet/number patterns).
2. Create L/LI/Lbl/LBody structure elements.
3. Mark content streams with BDC/EMC for each list item.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 19005_3_6_3_3_t02

**Rule:** For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

#### Test 3 — 19005_3_6_3_3_t03

**Rule:** If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

#### Test 4 — 19005_3_6_3_3_t04

**Rule:** If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

### §6.4.1

#### Test 1 — 19005_3_6_4_1_t01

**Rule:** A Widget annotation dictionary shall not contain the A or AA keys

**Target:** Widget annotation

**Complexity:** Low
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve widget annotation dictionaries.
2. Remove /AA key.
3. Allocate modified widget dicts.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 19005_3_6_4_1_t02

**Rule:** A Field dictionary shall not contain the A or AA keys

**Target:** Form field dict

**Complexity:** Low

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 3 — 19005_3_6_4_1_t03

**Rule:** The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false

**Target:** PDAcroForm

**Complexity:** High
**Existing fixer:** `fixer_needappearances.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_needappearances.go`.

### §6.4.2

#### Test 1 — 19005_3_6_4_2_t01

**Rule:** The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-3 file, if present, shall not contain the XFA key

**Target:** PDAcroForm

**Complexity:** Low
**Existing fixer:** `fixer_xfa.go`

**Procedure:**

1. Resolve /AcroForm dictionary.
2. Remove /XFA key.
3. Allocate modified AcroForm.
4. Existing implementation: `fixer_xfa.go`.

#### Test 2 — 19005_3_6_4_2_t02

**Rule:** A document's Catalog shall not contain the NeedsRendering key

**Target:** Document trailer/xref

**Complexity:** Low

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

### §6.4.3

#### Test 1 — 19005_3_6_4_3_t01

**Rule:** When computing the digest for the file, it shall be computed over the entire file, including the signature dictionary but excluding the PDF Signature itself

**Target:** PDSignature

**Complexity:** Medium

**Procedure:**

1. Rebuild the signature with valid PKCS#7 DER-encoded data.
2. Ensure ByteRange covers the entire file excluding the signature placeholder.
3. Re-sign with the certificate and private key.
4. Note: requires cryptographic signing credentials.

#### Test 2 — 19005_3_6_4_3_t02

**Rule:** The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, it shall include the signer's X.509 signing certificate

**Target:** PKCSDataObject

**Complexity:** Medium

**Procedure:**

1. Rebuild the signature with valid PKCS#7 DER-encoded data.
2. Ensure ByteRange covers the entire file excluding the signature placeholder.
3. Re-sign with the certificate and private key.
4. Note: requires cryptographic signing credentials.

#### Test 3 — 19005_3_6_4_3_t03

**Rule:** The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, there shall only be a single signer (e.g. a single \

**Target:** PKCSDataObject

**Complexity:** Medium

**Procedure:**

1. Rebuild the signature with valid PKCS#7 DER-encoded data.
2. Ensure ByteRange covers the entire file excluding the signature placeholder.
3. Re-sign with the certificate and private key.
4. Note: requires cryptographic signing credentials.

### §6.5.1

#### Test 1 — 19005_3_6_5_1_t01

**Rule:** The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted

**Target:** Action dict

**Complexity:** Medium
**Existing fixer:** `fixer_javascript.go`

**Procedure:**

1. Walk catalog /OpenAction, /AA, page /AA, and annotation /A and /AA entries.
2. Remove any action with /S = JavaScript, Launch, Sound, Movie, ResetForm, ImportData, or Hide.
3. For /AA dicts, remove only the entries with forbidden action types.
4. Allocate modified objects.
5. Existing implementation: `fixer_javascript.go`.

#### Test 2 — 19005_3_6_5_1_t02

**Rule:** Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted

**Target:** Action dict

**Complexity:** Medium

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.

### §6.5.2

#### Test 1 — 19005_3_6_5_2_t01

**Rule:** The document's Catalog shall not include an AA entry for an additional-actions dictionary

**Target:** PDDocument

**Complexity:** Medium

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.

#### Test 2 — 19005_3_6_5_2_t02

**Rule:** The Page dictionary shall not include an AA entry for an additional-actions dictionary

**Target:** Page dict

**Complexity:** Medium

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.

### §6.6.2.1

#### Test 1 — 19005_3_6_6_2_1_t01

**Rule:** The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-1:2008, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Check if catalog has /Metadata.
2. If missing, generate XMP from /Info dict values.
3. If present but invalid, repair the XMP XML structure.
4. Allocate XMP stream and update catalog.
5. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_3_6_6_2_1_t02

**Rule:** The bytes attribute shall not be used in the header of an XMP packet

**Target:** XMP metadata stream

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 3 — 19005_3_6_6_2_1_t03

**Rule:** The encoding attribute shall not be used in the header of an XMP packet

**Target:** XMP metadata stream

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 4 — 19005_3_6_6_2_1_t04

**Rule:** All metadata streams present in the PDF shall conform to the XMP Specification. All content of all XMP packets shall be well-formed, as defined by Extensible Markup Language (XML) 1.0 (Third Edition), 2.1, and the RDF/XML Syntax Specification (Revised)

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve ExtGState or annotation dict.
2. Set /CA and /ca to 1.0 (fully opaque).
3. Allocate modified object.
4. Existing implementation: `fixer_xmp.go`.

#### Test 5 — 19005_3_6_6_2_1_t05

**Rule:** All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

### §6.6.2.3.1

#### Test 1 — 19005_3_6_6_2_3_1_t01

**Rule:** All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2

**Target:** XMPProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_3_6_6_2_3_1_t02

**Rule:** All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2

**Target:** XMPProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

### §6.6.2.3.2

#### Test 1 — 19005_3_6_6_2_3_2_t01

**Rule:** Extension schemas shall be specified using the PDF/A extension schema container schema defined in 6.6.2.3.3. All fields described in each of the tables in 6.6.2.3.3 shall be present in any extension schema container schema

**Target:** ExtensionSchemaObject

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

### §6.6.2.3.3

#### Test 1 — 19005_3_6_6_2_3_3_t01

**Rule:** The extension schema container schema uses the namespace URI \

**Target:** ExtensionSchemasContainer

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_3_6_6_2_3_3_t02

**Rule:** Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 3 — 19005_3_6_6_2_3_3_t03

**Rule:** Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 4 — 19005_3_6_6_2_3_3_t04

**Rule:** Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 5 — 19005_3_6_6_2_3_3_t05

**Rule:** Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 6 — 19005_3_6_6_2_3_3_t06

**Rule:** Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema'

**Target:** ExtensionSchemaDefinition

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 7 — 19005_3_6_6_2_3_3_t07

**Rule:** Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 8 — 19005_3_6_6_2_3_3_t08

**Rule:** Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 9 — 19005_3_6_6_2_3_3_t09

**Rule:** Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty'

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 10 — 19005_3_6_6_2_3_3_t10

**Rule:** Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'

**Target:** ExtensionSchemaProperty

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 11 — 19005_3_6_6_2_3_3_t11

**Rule:** Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 12 — 19005_3_6_6_2_3_3_t12

**Rule:** Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 13 — 19005_3_6_6_2_3_3_t13

**Rule:** Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 14 — 19005_3_6_6_2_3_3_t14

**Rule:** Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 15 — 19005_3_6_6_2_3_3_t15

**Rule:** Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType'

**Target:** ExtensionSchemaValueType

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 16 — 19005_3_6_6_2_3_3_t16

**Rule:** Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'

**Target:** ExtensionSchemaField

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 17 — 19005_3_6_6_2_3_3_t17

**Rule:** Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined

**Target:** ExtensionSchemaField

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 18 — 19005_3_6_6_2_3_3_t18

**Rule:** Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'

**Target:** ExtensionSchemaField

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

### §6.6.4

#### Test 1 — 19005_3_6_6_4_t01

**Rule:** The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_3_6_6_4_t02

**Rule:** The value of \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 3 — 19005_3_6_6_4_t03

**Rule:** A Level A conforming file shall specify the value of \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 4 — 19005_3_6_6_4_t04

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 5 — 19005_3_6_6_4_t05

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 6 — 19005_3_6_6_4_t06

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

#### Test 7 — 19005_3_6_6_4_t07

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

### §6.7.2.2

#### Test 1 — 19005_3_6_7_2_2_t01

**Rule:** The document catalog dictionary shall include a MarkInfo dictionary containing an entry, Marked, whose value shall be true

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /MarkInfo << /Marked true >>.
3. Allocate updated catalog.
4. Existing implementation: `fixer_markinfo.go`.

### §6.7.3.3

#### Test 1 — 19005_3_6_7_3_3_t01

**Rule:** The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document's Catalog dictionary, as described in ISO 32000-1:2008, 14.7

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_structtree.go`

**Procedure:**

1. Create a minimal StructTreeRoot with a Document root element.
2. Add /ParentTree number tree for MCID-to-structure-element mapping.
3. For PDF/UA-2, add /Namespaces array with PDF 1.7 and 2.0 namespace dicts.
4. Set catalog /StructTreeRoot.
5. Allocate all new objects.
6. Existing implementation: `fixer_structtree.go`.

### §6.7.3.4

#### Test 1 — 19005_3_6_7_3_4_t01

**Rule:** All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in ISO 32000-1:2008, 14.8.4, in the role map dictionary of the structure tree root

**Target:** SENonStandard

**Complexity:** Medium

**Procedure:**

1. Identify custom structure element types not in the standard set.
2. Add /RoleMap entries mapping each custom type to a standard type (P, Span, Div, etc.).
3. Or add /NS on each structure element pointing to the appropriate namespace.
4. Allocate modified StructTreeRoot.

#### Test 2 — 19005_3_6_7_3_4_t02

**Rule:** A circular mapping shall not exist

**Target:** SENonStandard

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

#### Test 3 — 19005_3_6_7_3_4_t03

**Rule:** Standard tags shall not be remapped to a non-standard type

**Target:** SENonStandard

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

### §6.7.4

#### Test 1 — 19005_3_6_7_4_t01

**Rule:** If the Lang entry is present in the document's Catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as described in ISO 32000-1:2008, 14.9.2. A language identifier shall either be the empty text string, to indicate that the language is unknown, or a Language-Tag as defined in RFC 3066, Tags for the Identification of Languages

**Target:** CosLang

**Complexity:** Medium

**Procedure:**

1. Detect list structures from content analysis (bullet/number patterns).
2. Create L/LI/Lbl/LBody structure elements.
3. Mark content streams with BDC/EMC for each list item.

### §6.8

#### Test 1 — 19005_3_6_8_t01

**Rule:** The MIME type of an embedded file, or a subset of a file, shall be specified using the Subtype key of the file specification dictionary. If the MIME type is not known, the \

**Target:** Embedded file stream

**Complexity:** High

**Procedure:**

1. Resolve file specification dictionaries.
2. Ensure embedded file streams have /Subtype with MIME type.
3. Add /AFRelationship if required.
4. For PDF/A-1, remove /EF entirely (embedded files forbidden).
5. Allocate modified file spec dicts.

#### Test 2 — 19005_3_6_8_t02

**Rule:** The file specification dictionary for an embedded file shall contain the F and UF keys

**Target:** File specification dict

**Complexity:** High

**Procedure:**

1. Resolve file specification dictionaries.
2. Ensure embedded file streams have /Subtype with MIME type.
3. Add /AFRelationship if required.
4. For PDF/A-1, remove /EF entirely (embedded files forbidden).
5. Allocate modified file spec dicts.

#### Test 3 — 19005_3_6_8_t03

**Rule:** The file specification dictionary shall contain key AFRelationship of type Name identifying the relationship between the embedded file and the content of the document

**Target:** File specification dict

**Complexity:** High

**Procedure:**

1. Resolve file specification dictionaries.
2. Ensure embedded file streams have /Subtype with MIME type.
3. Add /AFRelationship if required.
4. For PDF/A-1, remove /EF entirely (embedded files forbidden).
5. Allocate modified file spec dicts.

#### Test 4 — 19005_3_6_8_t04

**Rule:** The additional information provided for associated files as well as the usage requirements for associated files indicate the relationship between the embedded file and the PDF document or the part of the PDF document with which it is associated

**Target:** File specification dict

**Complexity:** High

**Procedure:**

1. Resolve file specification dictionaries.
2. Ensure embedded file streams have /Subtype with MIME type.
3. Add /AFRelationship if required.
4. For PDF/A-1, remove /EF entirely (embedded files forbidden).
5. Allocate modified file spec dicts.

### §6.9

#### Test 1 — 19005_3_6_9_t01

**Rule:** Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key

**Target:** PDOCConfig

**Complexity:** Medium
**Existing fixer:** `fixer_catalog.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Remove /OCProperties key.
3. Allocate updated catalog.
4. Existing implementation: `fixer_catalog.go`.

#### Test 2 — 19005_3_6_9_t02

**Rule:** Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-3 file

**Target:** PDOCConfig

**Complexity:** Medium

**Procedure:**

1. Resolve OCG configuration dictionaries.
2. Add /Name key to each config dict.
3. Allocate modified config dicts.

#### Test 3 — 19005_3_6_9_t03

**Rule:** If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file

**Target:** PDOCConfig

**Complexity:** Medium

**Procedure:**

1. Resolve OCG configuration dictionaries.
2. Add /Name key to each config dict.
3. Allocate modified config dicts.

#### Test 4 — 19005_3_6_9_t04

**Rule:** The AS key shall not appear in any optional content configuration dictionary

**Target:** PDOCConfig

**Complexity:** Medium

**Procedure:**

1. Resolve OCG configuration dictionaries.
2. Add /Name key to each config dict.
3. Allocate modified config dicts.

### §6.10

#### Test 1 — 19005_3_6_10_t01

**Rule:** There shall be no AlternatePresentations entry in the document's name dictionary

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_catalog.go`

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.
3. Existing implementation: `fixer_catalog.go`.

#### Test 2 — 19005_3_6_10_t02

**Rule:** There shall be no PresSteps entry in any Page dictionary

**Target:** Page dict

**Complexity:** Medium
**Existing fixer:** `fixer_pressteps.go`

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.
3. Existing implementation: `fixer_pressteps.go`.

### §6.11

#### Test 1 — 19005_3_6_11_t01

**Rule:** The document catalog shall not contain the Requirements key

**Target:** Document trailer/xref

**Complexity:** Low
**Existing fixer:** `fixer_requirements.go`

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.
3. Existing implementation: `fixer_requirements.go`.


## PDF/A-4 (ISO 19005-4)

### §6.1.2

#### Test 1 — 19005_4_6_1_2_t01

**Rule:** The file header shall begin at byte zero and shall consist of \

**Target:** Document trailer/xref

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 2 — 19005_4_6_1_2_t02

**Rule:** The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127

**Target:** Document trailer/xref

**Complexity:** Medium

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

### §6.1.3

#### Test 1 — 19005_4_6_1_3_t01

**Rule:** File identifiers shall be defined by the ID entry in a PDF file’s trailer dictionary

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_trailerid.go`

**Procedure:**

1. Compute MD5 hash of the document content (excluding the ID entry itself).
2. Create a two-element /ID array with the hash as both elements.
3. Add /ID to the trailer dictionary.
4. Existing implementation: `fixer_trailerid.go`.

#### Test 2 — 19005_4_6_1_3_t02

**Rule:** The Encrypt key shall not be present in the trailer dictionary

**Target:** CosTrailer

**Complexity:** Medium
**Existing fixer:** `fixer_trailerid.go`

**Procedure:**

1. Remove /Encrypt from the file trailer.
2. Decrypt all encrypted strings and streams.
3. Full rewrite outputs decrypted content.
4. Existing implementation: `fixer_trailerid.go`.

#### Test 3 — 19005_4_6_1_3_t03

**Rule:** No data shall follow the last end-of-file marker as described in ISO 32000-2:2020, 7.5.5

**Target:** Document trailer/xref

**Complexity:** Medium

**Procedure:**

1. Full rewrite terminates at %%EOF with a single EOL marker.
2. No post-EOF data is written.

#### Test 4 — 19005_4_6_1_3_t04

**Rule:** The Info key shall not be present in the trailer dictionary of PDF/A-4 conforming files unless there exists a PieceInfo entry in the document catalog dictionary

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_trailerid.go`

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.
3. Existing implementation: `fixer_trailerid.go`.

#### Test 5 — 19005_4_6_1_3_t05

**Rule:** If a document information dictionary is present, it shall only contain a ModDate entry

**Target:** CosInfo

**Complexity:** Medium

**Procedure:**

1. Read the value from /Info dictionary.
2. Find the corresponding XMP property (dc:title, dc:creator, xmp:CreatorTool, pdf:Producer, xmp:CreateDate, xmp:ModifyDate).
3. If the XMP property is missing, inject it.
4. If it differs, replace with the /Info value (or vice versa).
5. For dates, convert PDF date format (D:YYYYMMDDHHmmSSOHH'mm') to ISO 8601.
6. Allocate updated XMP stream.

### §6.1.4

#### Test 1 — 19005_4_6_1_4_t01

**Rule:** The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker

**Target:** CosXRef

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

### §6.1.5

#### Test 1 — 19005_4_6_1_5_t01

**Rule:** Hexadecimal strings shall contain an even number of non-white-space characters

**Target:** String object

**Complexity:** Medium

**Procedure:**

1. Full rewrite re-serializes all hex strings with even-length digit pairs.
2. Invalid characters are stripped during re-serialization.

#### Test 2 — 19005_4_6_1_5_t02

**Rule:** A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f)

**Target:** String object

**Complexity:** Medium

**Procedure:**

1. Full rewrite re-serializes all hex strings with even-length digit pairs.
2. Invalid characters are stripped during re-serialization.

### §6.1.6.1

#### Test 1 — 19005_4_6_1_6_1_t01

**Rule:** The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword

**Target:** Stream object

**Complexity:** Medium

**Procedure:**

1. Full rewrite writes "stream\r\n" (CR+LF) after the stream keyword.
2. Writes EOL marker before "endstream".

#### Test 2 — 19005_4_6_1_6_1_t02

**Rule:** A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys

**Target:** Stream object

**Complexity:** Low

**Procedure:**

1. Resolve the stream dictionary.
2. Delete /F, /FFilter, /FDecodeParms keys.
3. If /F referenced an external file, read the file data and embed it as the stream body.
4. Allocate the modified stream.

#### Test 3 — 19005_4_6_1_6_1_t03

**Rule:** The Subtype entry in a 3D stream dictionary (ISO 32000-2:2020, 13.6.3) shall have a value which is either U3D or PRC as described in Annex B

**Target:** PD3DStream

**Complexity:** Medium

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.

### §6.1.6.2

#### Test 1 — 19005_4_6_1_6_2_t01

**Rule:** All standard stream filters listed in ISO 32000-2:2020, 7.4, Table 6 may be used, with the exception of LZWDecode. Filters that are not listed in ISO 32000-2:2020, 7.4, Table 6 shall not be used. In addition, the Crypt filter shall not be used unless the value of the Name key in the decode parameters dictionary is Identity

**Target:** CosFilter

**Complexity:** Low
**Existing fixer:** `fixer_lzw.go`

**Procedure:**

1. Decode the LZW stream to raw bytes.
2. Re-encode as FlateDecode (zlib compression).
3. Replace /Filter with /FlateDecode, remove /DecodeParms, update /Length.
4. Allocate the new stream.
5. Existing implementation: `fixer_lzw.go`.

### §6.1.7

#### Test 1 — 19005_4_6_1_7_t01

**Rule:** Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences

**Target:** CosUnicodeName

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §6.1.8

#### Test 1 — 19005_4_6_1_8_t01

**Rule:** The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded by an EOL marker. The obj and endobj keywords shall each be followed by an EOL marker

**Target:** CosIndirect

**Complexity:** Medium

**Procedure:**

1. Full rewrite serializes "N G obj\n" with single spaces and EOL markers.

### §6.1.9

#### Test 1 — 19005_4_6_1_9_t01

**Rule:** The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-2:2020, 8.9.7, Table 92, or an array containing any such value

**Target:** CosIIFilter

**Complexity:** Medium
**Existing fixer:** `fixer_lzw.go`

**Procedure:**

1. Decode the LZW stream to raw bytes.
2. Re-encode as FlateDecode (zlib compression).
3. Replace /Filter with /FlateDecode, remove /DecodeParms, update /Length.
4. Allocate the new stream.
5. Existing implementation: `fixer_lzw.go`.

### §6.1.11

#### Test 1 — 19005_4_6_1_11_t01

**Rule:** No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-2:2020, 12.8.6, Table 263)

**Target:** PDPerms

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §6.1.12

#### Test 1 — 19005_4_6_1_12_t01

**Rule:** If the Version key is present in the document catalog dictionary, the first character in its value shall be a 2 (32h) and the second character of its value shall be a PERIOD (2Eh) (decimal point). The third character shall be a decimal digit. The number of characters of the value of the Version key shall be exactly 3

**Target:** PDDocument

**Complexity:** Medium

**Procedure:**

1. Set catalog /Version to the required value (e.g., /2.0 for PDF/A-4).
2. Full rewrite updates the file header to match.

### §6.2.2

#### Test 1 — 19005_4_6_2_2_t01

**Rule:** Content streams shall not contain any operators not defined in ISO 32000-2:2020 even if such operators are bracketed by the BX/EX compatibility operators

**Target:** Op_Undefined

**Complexity:** Low

**Procedure:**

1. Parse content stream operators.
2. Remove or replace undefined operators.
3. Re-serialize the content stream.
4. Allocate modified stream.

#### Test 2 — 19005_4_6_2_2_t02

**Rule:** A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-2:2020, 7.8.3. Such resource dictionaries shall define all named resources referenced by this content stream

**Target:** Content stream

**Complexity:** High

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.

#### Test 3 — 19005_4_6_2_2_t03

**Rule:** A content stream's named resources shall be defined by a resource dictionary, which shall enumerate the named resources needed by the operators in the content stream and the names by which they can be referred to

**Target:** Content stream

**Complexity:** High

**Procedure:**

1. Parse content stream operators.
2. Remove or replace undefined operators.
3. Re-serialize the content stream.
4. Allocate modified stream.

### §6.2.3

#### Test 1 — 19005_4_6_2_3_t01

**Rule:** The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = \

**Target:** ICCOutputProfile

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 2 — 19005_4_6_2_3_t02

**Rule:** If any OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X or PDF/E, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream

**Target:** OutputIntent dict

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 3 — 19005_4_6_2_3_t03

**Rule:** The DestOutputProfileRef key, as defined in ISO 32000-2:2020, 14.11.5, Table 401, shall not be present in any output intent dictionary

**Target:** OutputIntent dict

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

### §6.2.4.2

#### Test 1 — 19005_4_6_2_4_2_t01

**Rule:** The profile that forms the stream of an ICCBased colour space shall conform to ISO 32000-2:2020, 8.6.5.5

**Target:** ICCInputProfile

**Complexity:** Medium
**Existing fixer:** `fixer_icc.go`

**Procedure:**

1. Read the ICC profile stream bytes.
2. Validate header: device class (bytes 12-15), color space (bytes 16-19), version (byte 8).
3. If invalid, replace with a known-good profile (sRGB or CMYK).
4. Allocate replacement stream.
5. Existing implementation: `fixer_icc.go`.

#### Test 2 — 19005_4_6_2_4_2_t02

**Rule:** Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and overprinting for fill is set to true, or both

**Target:** ICCBased color space

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Read the ICC profile stream bytes.
2. Validate header: device class (bytes 12-15), color space (bytes 16-19), version (byte 8).
3. If invalid, replace with a known-good profile (sRGB or CMYK).
4. Allocate replacement stream.
5. Existing implementation: `fixer_graphics.go`.

#### Test 3 — 19005_4_6_2_4_2_t03

**Rule:** An ICCBased colour space shall not be used where the profile is a CMYK destination profile and is identical to that in the current PDF/A OutputIntent or the current transparency blending colorspace

**Target:** ICCBased color space

**Complexity:** Low
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

### §6.2.4.3

#### Test 2 — 19005_4_6_2_4_3_t02

**Rule:** DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used or if the current transparency blending space, when the DeviceRGB colour space is used, is a device independent RGB-based colour space or the current PDF/A OutputIntent, when the DeviceRGB colour space is used, contains an 'RGB ' destination profile

**Target:** PDDeviceRGB

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 3 — 19005_4_6_2_4_3_t03

**Rule:** DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or if the current transparency blending space, when the DeviceCMYK colour space is used, is a device independent CMYK-based colour space or the current PDF/A OutputIntent, when the DeviceCMYK colour space is used, contains a 'CMYK' destination profile

**Target:** PDDeviceCMYK

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

#### Test 4 — 19005_4_6_2_4_3_t04

**Rule:** DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is in effect

**Target:** PDDeviceGray

**Complexity:** Medium
**Existing fixer:** `fixer_outputintent.go`

**Procedure:**

1. Resolve catalog /OutputIntents array.
2. If missing or empty, create an OutputIntent with an embedded ICC profile.
3. Select sRGB (3-component) for DeviceRGB documents, SWOP CMYK (4-component) for DeviceCMYK.
4. Set /S to GTS_PDFA1 (parts 1-3) or GTS_PDFA (part 4).
5. Set /OutputConditionIdentifier to the profile name.
6. Allocate ICC stream, OutputIntent dict, and updated catalog.
7. Existing implementation: `fixer_outputintent.go`.

### §6.2.4.4

#### Test 1 — 19005_4_6_2_4_4_t01

**Rule:** For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present

**Target:** PDDeviceN

**Complexity:** Medium

**Procedure:**

1. Resolve the Separation/DeviceN color space array.
2. Check that the alternate color space is device-independent.
3. If device-dependent, replace with an ICCBased equivalent.
4. For DeviceN with Colorants, ensure all colorant tint transforms are consistent.
5. Allocate modified color space arrays.

#### Test 2 — 19005_4_6_2_4_4_t02

**Rule:** All Separation arrays within a single conforming PDF/A-4 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, rather than the computational result of the use of those PDF objects. Compression and whether or not an object is direct or indirect shall be ignored

**Target:** PDSeparation

**Complexity:** Medium

**Procedure:**

1. Resolve the Separation/DeviceN color space array.
2. Check that the alternate color space is device-independent.
3. If device-dependent, replace with an ICCBased equivalent.
4. For DeviceN with Colorants, ensure all colorant tint transforms are consistent.
5. Allocate modified color space arrays.

### §6.2.5

#### Test 1 — 19005_4_6_2_5_t01

**Rule:** A graphics state parameter dictionary (ISO 32000-2:2020, 8.4.5) shall not contain the TR key

**Target:** ExtGState dict

**Complexity:** Low
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /TR key, or set /TR2 to /Default.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

#### Test 2 — 19005_4_6_2_5_t02

**Rule:** A graphics state parameter dictionary shall not contain the TR2 key with a value other than Default

**Target:** ExtGState dict

**Complexity:** Low

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /TR key, or set /TR2 to /Default.
3. Allocate modified ExtGState.

#### Test 3 — 19005_4_6_2_5_t03

**Rule:** A graphics state parameter dictionary shall not contain the HTO key

**Target:** ExtGState dict

**Complexity:** Low

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.

#### Test 4 — 19005_4_6_2_5_t04

**Rule:** All halftones in a conforming PDF/A-4 file shall have the value 1 or 5 for the HalftoneType key

**Target:** PDHalftone

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /HT key or set to a default halftone.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

#### Test 5 — 19005_4_6_2_5_t05

**Rule:** Halftones in a conforming PDF/A-4 file shall not contain a HalftoneName key

**Target:** PDHalftone

**Complexity:** Low
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /HT key or set to a default halftone.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

#### Test 6 — 19005_4_6_2_5_t06

**Rule:** The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-2:2020

**Target:** PDHalftone

**Complexity:** Medium
**Existing fixer:** `fixer_graphics.go`

**Procedure:**

1. Resolve ExtGState dictionaries.
2. Remove /HT key or set to a default halftone.
3. Allocate modified ExtGState.
4. Existing implementation: `fixer_graphics.go`.

### §6.2.7.1

#### Test 1 — 19005_4_6_2_7_1_t01

**Rule:** An Image dictionary shall not contain the Alternates key

**Target:** PDXImage

**Complexity:** Low

**Procedure:**

1. Identify Figure structure elements without /Alt.
2. Generate alt text via OCR on the image content, or set a placeholder.
3. Add /Alt to the structure element dictionary.
4. Allocate modified structure element.

#### Test 2 — 19005_4_6_2_7_1_t02

**Rule:** An Image dictionary shall not contain the OPI key

**Target:** PDXImage

**Complexity:** Low
**Existing fixer:** `fixer_images.go`

**Procedure:**

1. Resolve Image XObject dictionaries.
2. Remove /OPI key.
3. Allocate modified image dict.
4. Existing implementation: `fixer_images.go`.

#### Test 3 — 19005_4_6_2_7_1_t03

**Rule:** If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key, if present, shall have a value of false

**Target:** PDXImage

**Complexity:** Medium
**Existing fixer:** `fixer_images.go`

**Procedure:**

1. Resolve Image XObject dictionaries.
2. Set /Interpolate to false.
3. Allocate modified XObject.
4. Existing implementation: `fixer_images.go`.

#### Test 4 — 19005_4_6_2_7_1_t04

**Rule:** If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16

**Target:** PDXImage

**Complexity:** Medium

**Procedure:**

1. Resolve Image XObject dictionary.
2. If /BitsPerComponent is invalid, re-encode image data with a permitted value (1, 2, 4, or 8).
3. For image masks, set /BitsPerComponent to 1.
4. Allocate modified image stream.

#### Test 5 — 19005_4_6_2_7_1_t05

**Rule:** If an image mask dictionary contains the BitsPerComponent key, its value shall be 1

**Target:** PDMaskImage

**Complexity:** Medium

**Procedure:**

1. Resolve Image XObject dictionary.
2. If /BitsPerComponent is invalid, re-encode image data with a permitted value (1, 2, 4, or 8).
3. For image masks, set /BitsPerComponent to 1.
4. Allocate modified image stream.

### §6.2.7.3

#### Test 1 — 19005_4_6_2_7_3_t01

**Rule:** The number of colour channels in the JPEG2000 data shall be 1, 3 or 4

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

#### Test 2 — 19005_4_6_2_7_3_t02

**Rule:** If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

#### Test 3 — 19005_4_6_2_7_3_t03

**Rule:** The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming processor shall use only that colour space and shall ignore all other colour space specifications

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.

#### Test 4 — 19005_4_6_2_7_3_t04

**Rule:** JPEG2000 enumerated colour space 19 (CIEJab) shall not be used

**Target:** JPEG2000

**Complexity:** Low

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

#### Test 5 — 19005_4_6_2_7_3_t05

**Rule:** The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth

**Target:** JPEG2000

**Complexity:** Medium

**Procedure:**

1. Decode the JPEG2000 image stream.
2. Re-encode with compliant parameters (number of tiles, bit depth, color transform).
3. Allocate replacement image stream.
4. Note: requires JPEG2000 encoder (libopenjp2).

### §6.2.8.1

#### Test 1 — 19005_4_6_2_8_1_t01

**Rule:** A form XObject dictionary shall not contain an OPI key

**Target:** PDXForm

**Complexity:** Low
**Existing fixer:** `fixer_images.go`

**Procedure:**

1. Resolve Image XObject dictionaries.
2. Remove /OPI key.
3. Allocate modified image dict.
4. Existing implementation: `fixer_images.go`.

### §6.2.8.2

#### Test 1 — 19005_4_6_2_8_2_t01

**Rule:** A conforming file shall not contain any reference XObjects

**Target:** PDXForm

**Complexity:** Low

**Procedure:**

1. Resolve XObject dictionaries.
2. Remove /Ref key from Form XObjects.
3. Or inline the referenced content.
4. Allocate modified XObject dicts.

### §6.2.9

#### Test 1 — 19005_4_6_2_9_t01

**Rule:** Only blend modes that are specified in ISO 32000-2:2020 shall be used for the value of the BM key in a graphic state dictionary or an annotation dictionary

**Target:** CosBM

**Complexity:** Medium
**Existing fixer:** `fixer_transparency.go`

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.
4. Existing implementation: `fixer_transparency.go`.

#### Test 2 — 19005_4_6_2_9_t02

**Rule:** If the document does not contain a PDF/A output intent, then all pages that contain transparency shall either have a page-level PDF/A output intent or the page dictionary shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space

**Target:** Page dict

**Complexity:** Medium

**Procedure:**

1. Resolve page /Group dictionary.
2. If /S is /Transparency, add /CS with a device-independent color space.
3. Or remove the /Group dictionary entirely.
4. Allocate modified page dict.

### §6.2.10.2

#### Test 1 — 19005_4_6_2_10_2_t01

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 2 — 19005_4_6_2_10_2_t02

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Required) The type of font; must be \

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 3 — 19005_4_6_2_10_2_t03

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (Required) The PostScript name of the font

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 4 — 19005_4_6_2_10_2_t04

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array

**Target:** PDSimpleFont

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.
5. Existing implementation: `fixer_fonts.go`.

#### Test 5 — 19005_4_6_2_10_2_t05

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array

**Target:** PDSimpleFont

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.
5. Existing implementation: `fixer_fonts.go`.

#### Test 6 — 19005_4_6_2_10_2_t06

**Rule:** All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths

**Target:** PDSimpleFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

#### Test 7 — 19005_4_6_2_10_2_t07

**Rule:** All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 2.0 are Type1C - Type 1–equivalent font program represented in the Compact Font Format (CFF), CIDFontType0C - Type 0 CIDFont program represented in the Compact Font Format (CFF) and OpenType - OpenType® font program, as described in the ISO/IEC 14496-22

**Target:** PDFont

**Complexity:** Medium

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §6.2.10.3.1

#### Test 1 — 19005_4_6_2_10_3_1_t01

**Rule:** For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary has a value of Identity-H or Identity-V, then any values for the Registry, Ordering, and Supplement keys may be used in the CIDSystemInfo dictionary of the CIDFont. - Otherwise the corresponding values of the Registry and Ordering keys in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be less than or equal to the value of the Supplement key in the CIDSystemInfo dictionary of the CMap

**Target:** Type 0 composite font

**Complexity:** High

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §6.2.10.3.2

#### Test 1 — 19005_4_6_2_10_3_2_t01

**Rule:** ISO 32000-2:2020, 9.7.4 Table 115 requires that all embedded Type 2 CIDFonts, the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-2:2020, 9.7.4 Table 115

**Target:** CIDFont

**Complexity:** High

**Procedure:**

1. Check if CIDFont has /CIDToGIDMap.
2. If missing, add /CIDToGIDMap /Identity (when CIDs map directly to GIDs).
3. Or generate a stream mapping CIDs to GIDs based on the font program cmap table.
4. Allocate modified CIDFont dictionary.

### §6.2.10.3.3

#### Test 1 — 19005_4_6_2_10_3_3_t01

**Rule:** All CMaps used within a conforming PDF/A-4 file, except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116, shall be embedded in that file as described in ISO 32000-2:2020, 9.7.5

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 2 — 19005_4_6_2_10_3_3_t02

**Rule:** For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Read /WMode from both the CMap dictionary and the embedded CMap stream.
2. Set both to the same value (0 for horizontal, 1 for vertical).
3. Allocate modified CMap stream.

#### Test 3 — 19005_4_6_2_10_3_3_t03

**Rule:** A CMap shall not reference any other CMap except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §6.2.10.4.1

#### Test 1 — 19005_4_6_2_10_4_1_t01

**Rule:** The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-2:2020, 9.9

**Target:** PDFont

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 2 — 19005_4_6_2_10_4_1_t02

**Rule:** Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType fonts that are to be rendered, character codes shall be able to be mapped to glyphs according to ISO 32000-2:2020, 9.6.5 without the use of a non-standard mapping chosen by the conforming processor

**Target:** Glyph

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

### §6.2.10.5

#### Test 1 — 19005_4_6_2_10_5_t01

**Rule:** For every font embedded in a conforming file, the glyph width information in the font dictionary and in the embedded font program shall be consistent for every glyph referenced for rendering. Glyphs that are referenced only with rendering mode 3 are exempt from this requirement

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract binary widths from the font program (hmtx table for TrueType, charstring widths for CFF).
2. Read /UnitsPerEm from the head table.
3. For each charcode, compute expected PDF width: binaryWidth * 1000 / unitsPerEm.
4. Compare with the /Widths array (simple fonts) or /W array (CIDFonts).
5. Replace mismatched entries with the correct values.
6. Allocate modified font dictionary.
7. Existing implementation: `fixer_fonts.go`.

### §6.2.10.6

#### Test 1 — 19005_4_6_2_10_6_t01

**Rule:** For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain at least Microsoft Unicode (3,1 – Platform ID=3, Encoding ID=1), or Macintosh Roman (1,0 – Platform ID=1, Encoding ID=0) 'cmap' subtable that all necessary glyph lookups are able to be carried out

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 2 — 19005_4_6_2_10_6_t02

**Rule:** All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the Font dictionary. In addition, no non-symbolic TrueType font shall define a Differences array unless all of the glyph names in the Differences array are listed in the Adobe Glyph List and, if the font program is embedded, it shall contain at least the Microsoft Unicode (3,1 – Platform ID = 3, Encoding ID = 1) encoding in the 'cmap' subtable

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 3 — 19005_4_6_2_10_6_t03

**Rule:** Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary

**Target:** TrueType font

**Complexity:** Low

**Procedure:**

1. Check if the font is symbolic (Flags bit 3 set in FontDescriptor).
2. If symbolic: remove /Encoding from the font dictionary.
3. If non-symbolic: set /Encoding to /WinAnsiEncoding or /MacRomanEncoding.
4. Allocate modified font dictionary.

#### Test 4 — 19005_4_6_2_10_6_t04

**Rule:** The 'cmap' subtable in the embedded font program for a symbolic TrueType font shall contain either the Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0 – Platform ID=1, Encoding ID=0) encoding

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

### §6.2.10.7

#### Test 1 — 19005_4_6_2_10_7_t01

**Rule:** If a ToUnicode CMap is present, the Unicode values specified there shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the embedded font program from /FontFile, /FontFile2, or /FontFile3.
2. Parse the cmap table (TrueType) or charset (CFF) to build GID-to-Unicode mapping.
3. Construct charcode-to-Unicode mapping using the font encoding.
4. Generate a CMap stream with beginbfchar entries (chunks of 100).
5. Add the CMap as /ToUnicode on the font dictionary.
6. Allocate both the CMap stream and modified font dict.
7. Existing implementation: `fixer_fonts.go`.

### §6.2.10.8

#### Test 1 — 19005_4_6_2_10_8_t01

**Rule:** The ActualText entry shall not contain any PUA values

**Target:** CosActualText

**Complexity:** Low

**Procedure:**

1. Identify structure elements with PUA-mapped characters.
2. Determine the actual Unicode text from font encoding or ToUnicode CMap.
3. Add /ActualText with the resolved text.
4. Allocate modified structure element.

### §6.2.10.9

#### Test 1 — 19005_4_6_2_10_9_t01

**Rule:** A PDF/A-4 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream

**Target:** Glyph

**Complexity:** Low

**Procedure:**

1. Parse content stream operators.
2. Remove or replace undefined operators.
3. Re-serialize the content stream.
4. Allocate modified stream.

### §6.3.1

#### Test 1 — 19005_4_6_3_1_t01

**Rule:** Annotation types not defined in ISO 32000-2:2020, 12.5.6.1, Table 171 shall not be permitted. Additionally, the Sound, Screen, Movie, 3D, RichMedia and FileAttachment types shall not be permitted

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.
7. Existing implementation: `fixer_annotflags.go`.

### §6.3.2

#### Test 1 — 19005_4_6_3_2_t01

**Rule:** Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve Popup annotations.
2. Ensure /Parent references the parent annotation.
3. Allocate modified Popup annotation dict.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 19005_4_6_3_2_t02

**Rule:** If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the target graphics or font object.
2. Add, remove, or correct the key specified by the rule.
3. Allocate the modified object.
4. Existing implementation: `fixer_annotflags.go`.

### §6.3.3

#### Test 1 — 19005_4_6_3_3_t01

**Rule:** Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: Annotations where the value of the Rect key consists of an array where the value at index 1 is equal to the value at index 3 and the value at index 2 is equal to the value at index 4; - annotations whose Subtype value is Popup, Link or Projection

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Detect list structures from content analysis (bullet/number patterns).
2. Create L/LI/Lbl/LBody structure elements.
3. Mark content streams with BDC/EMC for each list item.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 19005_4_6_3_3_t02

**Rule:** For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

#### Test 3 — 19005_4_6_3_3_t03

**Rule:** If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

#### Test 4 — 19005_4_6_3_3_t04

**Rule:** If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream

**Target:** PDAnnot

**Complexity:** High
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_annotflags.go`.

### §6.4.1

#### Test 1 — 19005_4_6_4_1_t01

**Rule:** A Widget annotation dictionary shall not contain the A key

**Target:** Widget annotation

**Complexity:** Low
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 19005_4_6_4_1_t02

**Rule:** The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false

**Target:** PDAcroForm

**Complexity:** High
**Existing fixer:** `fixer_needappearances.go`

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.
7. Existing implementation: `fixer_needappearances.go`.

### §6.4.2

#### Test 1 — 19005_4_6_4_2_t01

**Rule:** The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-4 file, if present, shall not contain the XFA key

**Target:** PDAcroForm

**Complexity:** Low
**Existing fixer:** `fixer_xfa.go`

**Procedure:**

1. Resolve /AcroForm dictionary.
2. Remove /XFA key.
3. Allocate modified AcroForm.
4. Existing implementation: `fixer_xfa.go`.

#### Test 2 — 19005_4_6_4_2_t02

**Rule:** A document's Catalog shall not contain the NeedsRendering key

**Target:** Document trailer/xref

**Complexity:** Low

**Procedure:**

1. Resolve the target annotation, action, or metadata object.
2. Remove forbidden entries or add required entries per the rule.
3. Allocate the modified object.

### §6.6.1

#### Test 1 — 19005_4_6_6_1_t01

**Rule:** The Launch, Sound, Movie, ResetForm, ImportData, Hide, Rendition, Trans, SetOCGState and GoTo3DView actions shall not be permitted. Additionally, the obsoleted set-state and no-op actions shall not be permitted

**Target:** Action dict

**Complexity:** Medium
**Existing fixer:** `fixer_javascript.go`

**Procedure:**

1. Walk catalog /OpenAction, /AA, page /AA, and annotation /A and /AA entries.
2. Remove any action with /S = JavaScript, Launch, Sound, Movie, ResetForm, ImportData, or Hide.
3. For /AA dicts, remove only the entries with forbidden action types.
4. Allocate modified objects.
5. Existing implementation: `fixer_javascript.go`.

#### Test 2 — 19005_4_6_6_1_t02

**Rule:** Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted

**Target:** Action dict

**Complexity:** Medium

**Procedure:**

1. Identify the forbidden action type from the violation context.
2. Walk all action locations (catalog, pages, annotations, form fields).
3. Remove the forbidden action entry.
4. Allocate modified objects.

### §6.6.3

#### Test 1 — 19005_4_6_6_3_t01

**Rule:** If a document catalog dictionary or page dictionary or an annotation dictionary (other than a Widget annotation dictionary) include an AA entry, its value (which is an additional-actions dictionary) shall only contain keys from the following list: E, X, D, U, Fo, and Bl

**Target:** Action dict

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Detect list structures from content analysis (bullet/number patterns).
2. Create L/LI/Lbl/LBody structure elements.
3. Mark content streams with BDC/EMC for each list item.
4. Existing implementation: `fixer_annotflags.go`.

### §6.7.2.1

#### Test 1 — 19005_4_6_7_2_1_t01

**Rule:** The document catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-2:2020, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Check if catalog has /Metadata.
2. If missing, generate XMP from /Info dict values.
3. If present but invalid, repair the XMP XML structure.
4. Allocate XMP stream and update catalog.
5. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_4_6_7_2_1_t02

**Rule:** The bytes attribute shall not be used in the header of an XMP packet

**Target:** XMP metadata stream

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 3 — 19005_4_6_7_2_1_t03

**Rule:** The encoding attribute shall not be used in the header of an XMP packet

**Target:** XMP metadata stream

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 4 — 19005_4_6_7_2_1_t04

**Rule:** All content of all XMP packets located in any metadata stream present in the PDF shall be well-formed as defined by XMP (ISO 16684-1)

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 5 — 19005_4_6_7_2_1_t05

**Rule:** All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

### §6.7.3

#### Test 1 — 19005_4_6_7_3_t01

**Rule:** The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema. The PDF/A Identification schema defined in Table 2 uses the namespace URI \

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 19005_4_6_7_3_t02

**Rule:** The value of \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

#### Test 3 — 19005_4_6_7_3_t03

**Rule:** A file that does not conform to either PDF/A-4e or PDF/A-4f shall not provide any \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

#### Test 4 — 19005_4_6_7_3_t04

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

#### Test 5 — 19005_4_6_7_3_t05

**Rule:** The value of \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

#### Test 6 — 19005_4_6_7_3_t06

**Rule:** Property \

**Target:** PDF/A identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

### §6.9

#### Test 1 — 19005_4_6_9_t01

**Rule:** The embedded file stream dictionary shall include a valid MIME type value for the Subtype key. If the MIME type is not known, the value \

**Target:** Embedded file stream

**Complexity:** High

**Procedure:**

1. Resolve file specification dictionaries.
2. Ensure embedded file streams have /Subtype with MIME type.
3. Add /AFRelationship if required.
4. For PDF/A-1, remove /EF entirely (embedded files forbidden).
5. Allocate modified file spec dicts.

#### Test 2 — 19005_4_6_9_t02

**Rule:** The file specification dictionary for an embedded file shall contain the F and UF keys

**Target:** File specification dict

**Complexity:** High

**Procedure:**

1. Resolve file specification dictionaries.
2. Ensure embedded file streams have /Subtype with MIME type.
3. Add /AFRelationship if required.
4. For PDF/A-1, remove /EF entirely (embedded files forbidden).
5. Allocate modified file spec dicts.

#### Test 3 — 19005_4_6_9_t03

**Rule:** All of the embedded files shall be compliant with ISO 19005-1, 19005-2 or 19005-4

**Target:** Embedded file stream

**Complexity:** High

**Procedure:**

1. Resolve file specification dictionaries.
2. Ensure embedded file streams have /Subtype with MIME type.
3. Add /AFRelationship if required.
4. For PDF/A-1, remove /EF entirely (embedded files forbidden).
5. Allocate modified file spec dicts.

#### Test 4 — 19005_4_6_9_t04

**Rule:** Each embedded file’s file specification dictionary shall contain an AFRelationship key (ISO 32000-2, 7.11.3) that describes how this embedded file relates to the content of the PDF

**Target:** File specification dict

**Complexity:** High

**Procedure:**

1. Resolve file specification dictionaries.
2. Ensure embedded file streams have /Subtype with MIME type.
3. Add /AFRelationship if required.
4. For PDF/A-1, remove /EF entirely (embedded files forbidden).
5. Allocate modified file spec dicts.

#### Test 5 — 19005_4_6_9_t05

**Rule:** A PDF/A-4f conforming file shall contain an EmbeddedFiles key in the name dictionary of the document catalog dictionary

**Target:** Document trailer/xref

**Complexity:** High

**Procedure:**

1. Resolve the target metadata, form, or embedded file object.
2. Correct entries per the rule specification.
3. Allocate the modified object.

### §6.10

#### Test 1 — 19005_4_6_10_t01

**Rule:** Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key

**Target:** PDOCConfig

**Complexity:** Medium
**Existing fixer:** `fixer_catalog.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Remove /OCProperties key.
3. Allocate updated catalog.
4. Existing implementation: `fixer_catalog.go`.

#### Test 2 — 19005_4_6_10_t02

**Rule:** Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-4 file

**Target:** PDOCConfig

**Complexity:** Medium

**Procedure:**

1. Resolve OCG configuration dictionaries.
2. Add /Name key to each config dict.
3. Allocate modified config dicts.

#### Test 3 — 19005_4_6_10_t03

**Rule:** If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file

**Target:** PDOCConfig

**Complexity:** Medium

**Procedure:**

1. Resolve OCG configuration dictionaries.
2. Add /Name key to each config dict.
3. Allocate modified config dicts.

### §6.11

#### Test 1 — 19005_4_6_11_t01

**Rule:** There shall be no AlternatePresentations entry in the document's name dictionary

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_catalog.go`

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.
3. Existing implementation: `fixer_catalog.go`.

#### Test 2 — 19005_4_6_11_t02

**Rule:** There shall be no PresSteps entry in any Page dictionary

**Target:** Page dict

**Complexity:** Medium
**Existing fixer:** `fixer_pressteps.go`

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.
3. Existing implementation: `fixer_pressteps.go`.

### §6.12

#### Test 1 — 19005_4_6_12_t01

**Rule:** The document catalog shall not contain the Requirements key

**Target:** Document trailer/xref

**Complexity:** Low
**Existing fixer:** `fixer_requirements.go`

**Procedure:**

1. Full rewrite corrects file-level formatting.
2. Re-serializes all objects with correct spacing, EOL markers, and stream lengths.
3. Existing implementation: `fixer_requirements.go`.


## PDF/UA-1 (ISO 14289-1)

### §5

#### Test 1 — 14289_1_5_t01

**Rule:** The PDF/UA version and conformance level of a file shall be specified using the PDF/UA Identification extension schema

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 14289_1_5_t02

**Rule:** The value of \

**Target:** PDF/UA identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Patch XMP metadata identification properties.
2. Ensure pdfaid/pdfuaid namespace and values are correct.
3. Allocate updated /Metadata stream.

#### Test 3 — 14289_1_5_t03

**Rule:** Property \

**Target:** PDF/UA identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Patch XMP metadata identification properties.
2. Ensure pdfaid/pdfuaid namespace and values are correct.
3. Allocate updated /Metadata stream.

#### Test 4 — 14289_1_5_t04

**Rule:** Property \

**Target:** PDF/UA identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Patch XMP metadata identification properties.
2. Ensure pdfaid/pdfuaid namespace and values are correct.
3. Allocate updated /Metadata stream.

#### Test 5 — 14289_1_5_t05

**Rule:** Property \

**Target:** PDF/UA identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Patch XMP metadata identification properties.
2. Ensure pdfaid/pdfuaid namespace and values are correct.
3. Allocate updated /Metadata stream.

### §6.1

#### Test 1 — 14289_1_6_1_t01

**Rule:** The file header shall consist of \

**Target:** Document trailer/xref

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

### §6.2

#### Test 1 — 14289_1_6_2_t01

**Rule:** The document catalog dictionary shall include a MarkInfo dictionary containing an entry, Marked, whose value shall be true

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /MarkInfo << /Marked true >>.
3. Allocate updated catalog.
4. Existing implementation: `fixer_markinfo.go`.

### §7.1

#### Test 1 — 14289_1_7_1_t01

**Rule:** Content marked as Artifact should not be present inside tagged content

**Target:** SEMarkedContent

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Resolve page content streams.
2. Check if content has any BDC/BMC marked-content operators.
3. If untagged, prepend "/Document <</MCID N>> BDC\n" and append "\nEMC".
4. Add MCID N to the Document structure element /K array.
5. Update /ParentTree with page-to-structure-element mapping.
6. Set /StructParents on the page dict.
7. Allocate new content stream and modified structure objects.
8. Existing implementation: `fixer_markinfo.go`.

#### Test 2 — 14289_1_7_1_t02

**Rule:** Tagged content should not be present inside content marked as Artifact

**Target:** SEMarkedContent

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Resolve page content streams.
2. Check if content has any BDC/BMC marked-content operators.
3. If untagged, prepend "/Document <</MCID N>> BDC\n" and append "\nEMC".
4. Add MCID N to the Document structure element /K array.
5. Update /ParentTree with page-to-structure-element mapping.
6. Set /StructParents on the page dict.
7. Allocate new content stream and modified structure objects.
8. Existing implementation: `fixer_markinfo.go`.

#### Test 3 — 14289_1_7_1_t03

**Rule:** Content shall be marked as Artifact or tagged as real content

**Target:** SESimpleContentItem

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Resolve page content streams.
2. Check if content has any BDC/BMC marked-content operators.
3. If untagged, prepend "/Document <</MCID N>> BDC\n" and append "\nEMC".
4. Add MCID N to the Document structure element /K array.
5. Update /ParentTree with page-to-structure-element mapping.
6. Set /StructParents on the page dict.
7. Allocate new content stream and modified structure objects.
8. Existing implementation: `fixer_markinfo.go`.

#### Test 4 — 14289_1_7_1_t04

**Rule:** Files shall have a Suspects value of false(ISO 32000-1:2008, Table 321)

**Target:** Document trailer/xref

**Complexity:** Medium

**Procedure:**

1. Resolve /MarkInfo dictionary.
2. Set /Suspects to false or remove the key.
3. Allocate modified catalog.

#### Test 5 — 14289_1_7_1_t05

**Rule:** All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in ISO 32000-1:2008, 14.8.4, in the role map dictionary of the structure tree root. This mapping may be indirect; within the role map a non-standard type can map directly to another non-standard type, but eventually the mapping shall terminate at a standard type

**Target:** SENonStandard

**Complexity:** Medium

**Procedure:**

1. Identify custom structure element types not in the standard set.
2. Add /RoleMap entries mapping each custom type to a standard type (P, Span, Div, etc.).
3. Or add /NS on each structure element pointing to the appropriate namespace.
4. Allocate modified StructTreeRoot.

#### Test 6 — 14289_1_7_1_t06

**Rule:** A circular mapping shall not exist

**Target:** Structure element

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 7 — 14289_1_7_1_t07

**Rule:** Standard tags defined in ISO 32000-1:2008, 14.8.4, shall not be remapped

**Target:** Structure element

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 8 — 14289_1_7_1_t08

**Rule:** The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-1:2008, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Check if catalog has /Metadata.
2. If missing, generate XMP from /Info dict values.
3. If present but invalid, repair the XMP XML structure.
4. Allocate XMP stream and update catalog.
5. Existing implementation: `fixer_xmp.go`.

#### Test 9 — 14289_1_7_1_t09

**Rule:** The Metadata stream in the document's catalog dictionary shall contain a dc:title entry, where dc is the recommended prefix for the Dublin Core metadata schema as defined in the XMP specification, which clearly identifies the document

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Resolve catalog /Metadata stream.
2. Parse XMP XML.
3. Add or correct the identification property (pdfaid:part, pdfaid:conformance, pdfuaid:part, etc.).
4. Ensure namespace declarations are correct.
5. Re-serialize XMP and allocate updated stream.
6. Existing implementation: `fixer_xmp.go`.

#### Test 10 — 14289_1_7_1_t10

**Rule:** The document catalog dictionary shall include a ViewerPreferences dictionary containing a DisplayDocTitle key, whose value shall be true

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_viewerprefs.go`

**Procedure:**

1. Resolve or create /ViewerPreferences dictionary.
2. Set /DisplayDocTitle to true.
3. Add /ViewerPreferences to catalog if missing.
4. Allocate modified catalog.
5. Existing implementation: `fixer_viewerprefs.go`.

#### Test 11 — 14289_1_7_1_t11

**Rule:** The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog dictionary, as described in ISO 32000-1:2008, 14.7

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_structtree.go`

**Procedure:**

1. Create a minimal StructTreeRoot with a Document root element.
2. Add /ParentTree number tree for MCID-to-structure-element mapping.
3. For PDF/UA-2, add /Namespaces array with PDF 1.7 and 2.0 namespace dicts.
4. Set catalog /StructTreeRoot.
5. Allocate all new objects.
6. Existing implementation: `fixer_structtree.go`.

#### Test 12 — 14289_1_7_1_t12

**Rule:** A structure element dictionary shall contain the P (parent) entry according to ISO 32000-1:2008, 14.7.2, Table 355

**Target:** Structure element

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §7.2

#### Test 2 — 14289_1_7_2_t02

**Rule:** Natural language in the Outline entries shall be determined

**Target:** PDOutline

**Complexity:** Medium
**Existing fixer:** `fixer_cataloglang.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /Lang with a BCP 47 language tag (e.g., "en", "fr", "de").
3. For structure elements, set /Lang on elements with different languages.
4. Allocate modified catalog or structure elements.
5. Existing implementation: `fixer_cataloglang.go`.

#### Test 3 — 14289_1_7_2_t03

**Rule:** Table element may contain only TR, THead, TBody, TFoot and Caption elements

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 4 — 14289_1_7_2_t04

**Rule:** TR element should be contained in Table, THead, TBody or TFoot element

**Target:** SETR

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 5 — 14289_1_7_2_t05

**Rule:** THead element should be contained in Table element

**Target:** SETHead

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 6 — 14289_1_7_2_t06

**Rule:** TBody element should be contained in Table element

**Target:** SETBody

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 7 — 14289_1_7_2_t07

**Rule:** TFoot element should be contained in Table element

**Target:** SETFoot

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 8 — 14289_1_7_2_t08

**Rule:** TH element should be contained in TR element

**Target:** SETH

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 9 — 14289_1_7_2_t09

**Rule:** TD element should be contained in TR element

**Target:** SETD

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 10 — 14289_1_7_2_t10

**Rule:** TR element may contain only TH and TD elements

**Target:** SETR

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 11 — 14289_1_7_2_t11

**Rule:** Table element should contain zero or one THead kid

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 12 — 14289_1_7_2_t12

**Rule:** Table element should contain zero or one TFoot kid

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 13 — 14289_1_7_2_t13

**Rule:** If Table element contains TFoot kid, Table element should contain one or more TBody kids

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 14 — 14289_1_7_2_t14

**Rule:** If Table element contains THead kid, Table element should contain one or more TBody kids

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 15 — 14289_1_7_2_t15

**Rule:** A table cell shall not have intersection with other cells

**Target:** SETableCell

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 16 — 14289_1_7_2_t16

**Rule:** Table element may contain a Caption element as its first or last kid

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 17 — 14289_1_7_2_t17

**Rule:** LI element should be contained in L element

**Target:** SELI

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 18 — 14289_1_7_2_t18

**Rule:** LBody element should be contained in LI element

**Target:** SELBody

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 19 — 14289_1_7_2_t19

**Rule:** L element may contain only L, LI and Caption elements

**Target:** SEL

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 20 — 14289_1_7_2_t20

**Rule:** LI element may contain only Lbl and LBody elements

**Target:** SELI

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 21 — 14289_1_7_2_t21

**Rule:** Natural language for text in ActualText attribute shall be determined

**Target:** Structure element

**Complexity:** Medium
**Existing fixer:** `fixer_cataloglang.go`

**Procedure:**

1. Identify structure elements with PUA-mapped characters.
2. Determine the actual Unicode text from font encoding or ToUnicode CMap.
3. Add /ActualText with the resolved text.
4. Allocate modified structure element.
5. Existing implementation: `fixer_cataloglang.go`.

#### Test 22 — 14289_1_7_2_t22

**Rule:** Natural language for text in Alt attribute shall be determined

**Target:** Structure element

**Complexity:** Medium
**Existing fixer:** `fixer_cataloglang.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /Lang with a BCP 47 language tag (e.g., "en", "fr", "de").
3. For structure elements, set /Lang on elements with different languages.
4. Allocate modified catalog or structure elements.
5. Existing implementation: `fixer_cataloglang.go`.

#### Test 23 — 14289_1_7_2_t23

**Rule:** Natural language for text in E attribute shall be determined

**Target:** Structure element

**Complexity:** Medium
**Existing fixer:** `fixer_cataloglang.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /Lang with a BCP 47 language tag (e.g., "en", "fr", "de").
3. For structure elements, set /Lang on elements with different languages.
4. Allocate modified catalog or structure elements.
5. Existing implementation: `fixer_cataloglang.go`.

#### Test 24 — 14289_1_7_2_t24

**Rule:** Natural language in the Contents entry for annotations shall be determined

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_cataloglang.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /Lang with a BCP 47 language tag (e.g., "en", "fr", "de").
3. For structure elements, set /Lang on elements with different languages.
4. Allocate modified catalog or structure elements.
5. Existing implementation: `fixer_cataloglang.go`.

#### Test 25 — 14289_1_7_2_t25

**Rule:** Natural language in the TU key for form fields shall be determined

**Target:** Form field dict

**Complexity:** Medium
**Existing fixer:** `fixer_cataloglang.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /Lang with a BCP 47 language tag (e.g., "en", "fr", "de").
3. For structure elements, set /Lang on elements with different languages.
4. Allocate modified catalog or structure elements.
5. Existing implementation: `fixer_cataloglang.go`.

#### Test 26 — 14289_1_7_2_t26

**Rule:** TOCI element should be contained in TOC element

**Target:** SETOCI

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 27 — 14289_1_7_2_t27

**Rule:** TOC element may contain only TOC, TOCI and Caption elements

**Target:** SETOC

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 28 — 14289_1_7_2_t28

**Rule:** TOC element may contain a Caption element only as its first kid

**Target:** SETOC

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 29 — 14289_1_7_2_t29

**Rule:** If the Lang entry is present in the document's Catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as described in ISO 32000-1:2008, 14.9.2. A language identifier shall be a Language-Tag as defined in RFC 3066, Tags for the Identification of Languages

**Target:** CosLang

**Complexity:** Medium

**Procedure:**

1. Detect list structures from content analysis (bullet/number patterns).
2. Create L/LI/Lbl/LBody structure elements.
3. Mark content streams with BDC/EMC for each list item.

#### Test 30 — 14289_1_7_2_t30

**Rule:** Natural language for text in ActualText attribute in Span Marked Content shall be determined

**Target:** SEMarkedContent

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Identify structure elements with PUA-mapped characters.
2. Determine the actual Unicode text from font encoding or ToUnicode CMap.
3. Add /ActualText with the resolved text.
4. Allocate modified structure element.
5. Existing implementation: `fixer_markinfo.go`.

#### Test 31 — 14289_1_7_2_t31

**Rule:** Natural language for text in Alt attribute in Span Marked Content shall be determined

**Target:** SEMarkedContent

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /Lang with a BCP 47 language tag (e.g., "en", "fr", "de").
3. For structure elements, set /Lang on elements with different languages.
4. Allocate modified catalog or structure elements.
5. Existing implementation: `fixer_markinfo.go`.

#### Test 32 — 14289_1_7_2_t32

**Rule:** Natural language for text in E attribute in Span Marked Content shall be determined

**Target:** SEMarkedContent

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /Lang with a BCP 47 language tag (e.g., "en", "fr", "de").
3. For structure elements, set /Lang on elements with different languages.
4. Allocate modified catalog or structure elements.
5. Existing implementation: `fixer_markinfo.go`.

#### Test 33 — 14289_1_7_2_t33

**Rule:** Natural language for document metadata shall be determined

**Target:** XMPLangAlt

**Complexity:** Medium
**Existing fixer:** `fixer_cataloglang.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /Lang with a BCP 47 language tag (e.g., "en", "fr", "de").
3. For structure elements, set /Lang on elements with different languages.
4. Allocate modified catalog or structure elements.
5. Existing implementation: `fixer_cataloglang.go`.

#### Test 34 — 14289_1_7_2_t34

**Rule:** Natural language for text in page content shall be determined

**Target:** SETextItem

**Complexity:** Medium
**Existing fixer:** `fixer_cataloglang.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /Lang with a BCP 47 language tag (e.g., "en", "fr", "de").
3. For structure elements, set /Lang on elements with different languages.
4. Allocate modified catalog or structure elements.
5. Existing implementation: `fixer_cataloglang.go`.

#### Test 36 — 14289_1_7_2_t36

**Rule:** THead element may contain only TR elements

**Target:** SETHead

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 37 — 14289_1_7_2_t37

**Rule:** TBody element may contain only TR elements

**Target:** SETBody

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 38 — 14289_1_7_2_t38

**Rule:** TFoot element may contain only TR elements

**Target:** SETFoot

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 39 — 14289_1_7_2_t39

**Rule:** Table element may contain only one Caption element

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 40 — 14289_1_7_2_t40

**Rule:** L element may contain a Caption element only as its first kid

**Target:** SEL

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 41 — 14289_1_7_2_t41

**Rule:** Table columns shall have the same number of rows (taking into account row spans)

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 42 — 14289_1_7_2_t42

**Rule:** Table rows shall have the same number of columns (taking into account column spans)

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 43 — 14289_1_7_2_t43

**Rule:** Table rows shall have the same number of columns (taking into account column spans)

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §7.3

#### Test 1 — 14289_1_7_3_t01

**Rule:** Figure tags shall include an alternative representation or replacement text that represents the contents marked with the Figure tag as noted in ISO 32000-1:2008, 14.7.2, Table 323

**Target:** SEFigure

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.
7. Existing implementation: `fixer_markinfo.go`.

### §7.4.2

#### Test 1 — 14289_1_7_4_2_t01

**Rule:** For documents that are not strongly structured, as described in ISO 32000-1:2008, 14.8.4.3.5, heading tags shall be used as follows: (*) If any heading tags are used, H1 shall be the first. (*) A document may use more than one instance of any specific tag level. For example, a tag level may be repeated if document content requires it. (*) If document semantics require a descending sequence of headers, such a sequence shall proceed in strict numerical order and shall not skip an intervening heading level. (*) A document may increment its heading sequence without restarting at H1 if document semantics require it

**Target:** SEHn

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

### §7.4.4

#### Test 1 — 14289_1_7_4_4_t01

**Rule:** Each node in the tag tree shall contain at most one child H tag

**Target:** Structure element

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 2 — 14289_1_7_4_4_t02

**Rule:** All documents shall be either strongly or weakly structured, but not both

**Target:** SEH

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 3 — 14289_1_7_4_4_t03

**Rule:** All documents shall be either strongly or weakly structured, but not both

**Target:** SEHn

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §7.5

#### Test 1 — 14289_1_7_5_t01

**Rule:** If the table's structure is not determinable via Headers and IDs, then structure elements of type TH shall have a Scope attribute

**Target:** SETD

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 2 — 14289_1_7_5_t02

**Rule:** If the table's structure is not determinable via Headers and IDs, then structure elements of type TH shall have a Scope attribute

**Target:** SETD

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

### §7.7

#### Test 1 — 14289_1_7_7_t01

**Rule:** All mathematical expressions shall be enclosed within a Formula tag as detailed in ISO 32000-1:2008, 14.8.4.5 and shall have Alt or ActualText attributes

**Target:** SEFormula

**Complexity:** Medium

**Procedure:**

1. Identify structure elements with PUA-mapped characters.
2. Determine the actual Unicode text from font encoding or ToUnicode CMap.
3. Add /ActualText with the resolved text.
4. Allocate modified structure element.

### §7.9

#### Test 1 — 14289_1_7_9_t01

**Rule:** Note tag shall have ID entry

**Target:** SENote

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 2 — 14289_1_7_9_t02

**Rule:** Each Note tag shall have unique ID key

**Target:** SENote

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §7.10

#### Test 1 — 14289_1_7_10_t01

**Rule:** Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key

**Target:** PDOCConfig

**Complexity:** Medium
**Existing fixer:** `fixer_catalog.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Remove /OCProperties key.
3. Allocate updated catalog.
4. Existing implementation: `fixer_catalog.go`.

#### Test 2 — 14289_1_7_10_t02

**Rule:** The AS key shall not appear in any optional content configuration dictionary

**Target:** PDOCConfig

**Complexity:** Medium

**Procedure:**

1. Resolve OCG configuration dictionaries.
2. Add /Name key to each config dict.
3. Allocate modified config dicts.

### §7.11

#### Test 1 — 14289_1_7_11_t01

**Rule:** The file specification dictionary for an embedded file shall contain the non-empty F and UF keys

**Target:** File specification dict

**Complexity:** High

**Procedure:**

1. Resolve file specification dictionaries.
2. Ensure embedded file streams have /Subtype with MIME type.
3. Add /AFRelationship if required.
4. For PDF/A-1, remove /EF entirely (embedded files forbidden).
5. Allocate modified file spec dicts.

### §7.15

#### Test 1 — 14289_1_7_15_t01

**Rule:** Dynamic XFA forms shall not be used

**Target:** PDAcroForm

**Complexity:** Low
**Existing fixer:** `fixer_xfa.go`

**Procedure:**

1. Resolve /AcroForm dictionary.
2. Remove /XFA key.
3. Allocate modified AcroForm.
4. Existing implementation: `fixer_xfa.go`.

### §7.16

#### Test 1 — 14289_1_7_16_t01

**Rule:** An encrypted conforming file shall contain a P key in its encryption dictionary (ISO 32000-1:2008, 7.6.3.2, Table 21). The 10th bit position of the P key shall be true

**Target:** PDEncryption

**Complexity:** Medium
**Existing fixer:** `fixer_catalog.go`

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.
7. Existing implementation: `fixer_catalog.go`.

### §7.18.1

#### Test 1 — 14289_1_7_18_1_t01

**Rule:** An annotation, excluding annotations of subtype Widget, PrinterMark or Link, shall be nested within an Annot tag

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Create a Link structure element for each Link annotation.
2. Nest the Link annotation inside the Link structure element via OBJR.
3. Add to StructTreeRoot.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 14289_1_7_18_1_t02

**Rule:** An annotation (except Widget annotations or hidden annotations, or those having rectangle outside the crop-box) shall have either Contents key or an Alt entry in the enclosing structure element

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve form field / widget annotation dictionaries.
2. Add /TU with a descriptive tooltip, or /Contents with a description.
3. Use the field /T (name) as fallback text if no better description is available.
4. Allocate modified field dicts.
5. Existing implementation: `fixer_annotflags.go`.

#### Test 3 — 14289_1_7_18_1_t03

**Rule:** A form field shall have a TU key present or all its Widget annotations shall have alternative descriptions (in the form of an Alt entry in the enclosing structure elements)

**Target:** Widget annotation

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve form field / widget annotation dictionaries.
2. Add /TU with a descriptive tooltip, or /Contents with a description.
3. Use the field /T (name) as fallback text if no better description is available.
4. Allocate modified field dicts.
5. Existing implementation: `fixer_annotflags.go`.

### §7.18.2

#### Test 1 — 14289_1_7_18_2_t01

**Rule:** Annotations of subtype TrapNet shall not be permitted

**Target:** PDTrapNetAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Walk page /Annots arrays.
2. Remove annotations with /Subtype /TrapNet.
3. Allocate modified page dicts.
4. Existing implementation: `fixer_annotflags.go`.

### §7.18.3

#### Test 1 — 14289_1_7_18_3_t01

**Rule:** Every page on which there is an annotation shall contain in its page dictionary the key Tabs, and its value shall be S

**Target:** Page dict

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve each page dictionary.
2. Add /Tabs /S (structure order).
3. Allocate modified page dicts.
4. Existing implementation: `fixer_annotflags.go`.

### §7.18.4

#### Test 1 — 14289_1_7_18_4_t01

**Rule:** A Widget annotation shall be nested within a Form tag per ISO 32000-1:2008, 14.8.4.5, Table 340

**Target:** Widget annotation

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.
7. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 14289_1_7_18_4_t02

**Rule:** If the Form element omits a Role attribute (Table 348), it shall have only one child: an object reference (14.7.4.3) identifying the widget annotation per ISO 32000-1:2008, 14.8.4.5, Table 340

**Target:** SEForm

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.
7. Existing implementation: `fixer_annotflags.go`.

### §7.18.5

#### Test 1 — 14289_1_7_18_5_t01

**Rule:** Links shall be tagged according to ISO 32000-1:2008, 14.8.4.4.2, Link Element

**Target:** Link annotation

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 2 — 14289_1_7_18_5_t02

**Rule:** Links shall contain an alternate description via their Contents key as described in ISO 32000-1:2008, 14.9.3

**Target:** Link annotation

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §7.18.6.2

#### Test 1 — 14289_1_7_18_6_2_t01

**Rule:** In the media clip data dictionary, the optional CT key (ISO 32000-1:2008, 13.2.4.2, Table 274) is required

**Target:** PDMediaClip

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 2 — 14289_1_7_18_6_2_t02

**Rule:** In the media clip data dictionary, the optional Alt key (ISO 32000-1:2008, 13.2.4.2, Table 274) is required

**Target:** PDMediaClip

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §7.18.8

#### Test 1 — 14289_1_7_18_8_t01

**Rule:** PrinterMark annotations, if present, shall be considered Incidental Artifacts, as if they are hidden page elements as defined in ISO 32000-1:2008, 14.8.2.2.3

**Target:** PDPrinterMarkAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_contenttag.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_contenttag.go`.

### §7.20

#### Test 1 — 14289_1_7_20_t01

**Rule:** A conforming file shall not contain any reference XObjects

**Target:** PDXForm

**Complexity:** Low

**Procedure:**

1. Resolve XObject dictionaries.
2. Remove /Ref key from Form XObjects.
3. Or inline the referenced content.
4. Allocate modified XObject dicts.

#### Test 2 — 14289_1_7_20_t02

**Rule:** The content of Form XObjects shall be incorporated into structure elements according to ISO 32000-1:2008, 14.7.2

**Target:** PDXForm

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §7.21.3.1

#### Test 1 — 14289_1_7_21_3_1_t01

**Rule:** For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary is Identity-H or Identity-V, any values of Registry, Ordering, and Supplement may be used in the CIDSystemInfo entry of the CIDFont. - Otherwise, the corresponding Registry and Ordering strings in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be less than or equal to the Supplement key in the CIDSystemInfo dictionary of the CMap

**Target:** Type 0 composite font

**Complexity:** High

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §7.21.3.2

#### Test 1 — 14289_1_7_21_3_2_t01

**Rule:** ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2008, 9.7.4, Table 117

**Target:** CIDFont

**Complexity:** High

**Procedure:**

1. Check if CIDFont has /CIDToGIDMap.
2. If missing, add /CIDToGIDMap /Identity (when CIDs map directly to GIDs).
3. Or generate a stream mapping CIDs to GIDs based on the font program cmap table.
4. Allocate modified CIDFont dictionary.

### §7.21.3.3

#### Test 1 — 14289_1_7_21_3_3_t01

**Rule:** All CMaps used within a PDF/UA file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 2 — 14289_1_7_21_3_3_t02

**Rule:** For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Read /WMode from both the CMap dictionary and the embedded CMap stream.
2. Set both to the same value (0 for horizontal, 1 for vertical).
3. Allocate modified CMap stream.

#### Test 3 — 14289_1_7_21_3_3_t03

**Rule:** A CMap shall not reference any other CMap except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §7.21.4.1

#### Test 1 — 14289_1_7_21_4_1_t01

**Rule:** The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9

**Target:** PDFont

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 2 — 14289_1_7_21_4_1_t02

**Rule:** Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced solely in text rendering mode 3 is not rendered and is thus exempt from the requirements that impact the visual representation of the glyphs of a font. In all cases for TrueType fonts that are to be rendered, character codes shall be able to be mapped to glyphs according to ISO 32000-1:2008, 9.6.6.4 without the use of a non-standard mapping chosen by the conforming processor

**Target:** Glyph

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

### §7.21.4.2

#### Test 1 — 14289_1_7_21_4_2_t01

**Rule:** If the FontDescriptor dictionary of an embedded Type 1 font contains a CharSet string, then it shall list the character names of all glyphs present in the font program, regardless of whether a glyph in the font is referenced or used by the PDF or not

**Target:** Type 1 font

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the Type 1 font program from /FontFile.
2. Parse the CharStrings dictionary to enumerate all glyph names.
3. Build a /CharSet string: "/" + glyph names joined by "/".
4. Set /CharSet on the FontDescriptor.
5. Allocate modified FontDescriptor.
6. Existing implementation: `fixer_fonts.go`.

#### Test 2 — 14289_1_7_21_4_2_t02

**Rule:** If the FontDescriptor dictionary of an embedded CID font contains a CIDSet stream, then it shall identify all CIDs which are present in the font program, regardless of whether a CID in the font is referenced or used by the PDF or not

**Target:** CIDFont

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the font program.
2. Enumerate all valid CIDs (GIDs with non-empty outlines).
3. Build a CIDSet bitfield: bit N of byte M represents CID (M*8 + 7-N).
4. Compress with FlateDecode and set as /CIDSet stream on FontDescriptor.
5. Allocate modified FontDescriptor and stream.
6. Existing implementation: `fixer_fonts.go`.

### §7.21.5

#### Test 1 — 14289_1_7_21_5_t01

**Rule:** For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract binary widths from the font program (hmtx table for TrueType, charstring widths for CFF).
2. Read /UnitsPerEm from the head table.
3. For each charcode, compute expected PDF width: binaryWidth * 1000 / unitsPerEm.
4. Compare with the /Widths array (simple fonts) or /W array (CIDFonts).
5. Replace mismatched entries with the correct values.
6. Allocate modified font dictionary.
7. Existing implementation: `fixer_fonts.go`.

### §7.21.6

#### Test 1 — 14289_1_7_21_6_t01

**Rule:** For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain one or several non-symbolic cmap entries such that all necessary glyph lookups can be carried out

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 2 — 14289_1_7_21_6_t02

**Rule:** All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the Font dictionary. In addition, no non-symbolic TrueType font shall define a Differences array unless all of the glyph names in the Differences array are listed in the Adobe Glyph List and, if the font program is embedded, it shall contain at least the Microsoft Unicode (3,1 – Platform ID = 3, Encoding ID = 1) encoding in the 'cmap' table

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 3 — 14289_1_7_21_6_t03

**Rule:** Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary

**Target:** TrueType font

**Complexity:** Low

**Procedure:**

1. Check if the font is symbolic (Flags bit 3 set in FontDescriptor).
2. If symbolic: remove /Encoding from the font dictionary.
3. If non-symbolic: set /Encoding to /WinAnsiEncoding or /MacRomanEncoding.
4. Allocate modified font dictionary.

#### Test 4 — 14289_1_7_21_6_t04

**Rule:** The 'cmap' table in the embedded font program for a symbolic TrueType font shall contain either exactly one encoding or it shall contain, at least, the Microsoft Symbol (3,0 - Platform ID=3, Encoding ID=0) encoding

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

### §7.21.7

#### Test 1 — 14289_1_7_21_7_t01

**Rule:** The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 14289-1, 7.21.7

**Target:** Glyph

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the embedded font program from /FontFile, /FontFile2, or /FontFile3.
2. Parse the cmap table (TrueType) or charset (CFF) to build GID-to-Unicode mapping.
3. Construct charcode-to-Unicode mapping using the font encoding.
4. Generate a CMap stream with beginbfchar entries (chunks of 100).
5. Add the CMap as /ToUnicode on the font dictionary.
6. Allocate both the CMap stream and modified font dict.
7. Existing implementation: `fixer_fonts.go`.

#### Test 2 — 14289_1_7_21_7_t02

**Rule:** The Unicode values specified in the ToUnicode CMap shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the embedded font program from /FontFile, /FontFile2, or /FontFile3.
2. Parse the cmap table (TrueType) or charset (CFF) to build GID-to-Unicode mapping.
3. Construct charcode-to-Unicode mapping using the font encoding.
4. Generate a CMap stream with beginbfchar entries (chunks of 100).
5. Add the CMap as /ToUnicode on the font dictionary.
6. Allocate both the CMap stream and modified font dict.
7. Existing implementation: `fixer_fonts.go`.

### §7.21.8

#### Test 1 — 14289_1_7_21_8_t01

**Rule:** A PDF/UA compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream

**Target:** Glyph

**Complexity:** Low

**Procedure:**

1. Parse content stream operators.
2. Remove or replace undefined operators.
3. Re-serialize the content stream.
4. Allocate modified stream.


## PDF/UA-2 (ISO 14289-2)

### §5

#### Test 1 — 14289_2_5_t01

**Rule:** The PDF/UA version of a file shall be specified in the value of the Metadata entry in the document catalog using the PDF/UA identification schema

**Target:** XMP metadata stream

**Complexity:** Medium

**Procedure:**

1. Set catalog /Version to the required value (e.g., /2.0 for PDF/A-4).
2. Full rewrite updates the file header to match.

#### Test 2 — 14289_2_5_t02

**Rule:** The value of \

**Target:** PDF/UA identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Patch XMP metadata identification properties.
2. Ensure pdfaid/pdfuaid namespace and values are correct.
3. Allocate updated /Metadata stream.

#### Test 3 — 14289_2_5_t03

**Rule:** Property \

**Target:** PDF/UA identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Patch XMP metadata identification properties.
2. Ensure pdfaid/pdfuaid namespace and values are correct.
3. Allocate updated /Metadata stream.

#### Test 4 — 14289_2_5_t04

**Rule:** Property \

**Target:** PDF/UA identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Patch XMP metadata identification properties.
2. Ensure pdfaid/pdfuaid namespace and values are correct.
3. Allocate updated /Metadata stream.

#### Test 5 — 14289_2_5_t05

**Rule:** The value of \

**Target:** PDF/UA identification (XMP)

**Complexity:** Medium

**Procedure:**

1. Patch XMP metadata identification properties.
2. Ensure pdfaid/pdfuaid namespace and values are correct.
3. Allocate updated /Metadata stream.

### §6.2

#### Test 1 — 14289_2_6_2_t01

**Rule:** The document catalog dictionary shall include a MarkInfo dictionary containing an entry, Marked, whose value shall be true

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_markinfo.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /MarkInfo << /Marked true >>.
3. Allocate updated catalog.
4. Existing implementation: `fixer_markinfo.go`.

### §8.2.1

#### Test 1 — 14289_2_8_2_1_t01

**Rule:** The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog dictionary, as described in ISO 32000-2:2020, 14.7

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_structtree.go`

**Procedure:**

1. Create a minimal StructTreeRoot with a Document root element.
2. Add /ParentTree number tree for MCID-to-structure-element mapping.
3. For PDF/UA-2, add /Namespaces array with PDF 1.7 and 2.0 namespace dicts.
4. Set catalog /StructTreeRoot.
5. Allocate all new objects.
6. Existing implementation: `fixer_structtree.go`.

#### Test 2 — 14289_2_8_2_1_t02

**Rule:** A structure element dictionary shall contain the P (parent) entry according to ISO 32000-2:2020, 14.7.2, Table 323

**Target:** Structure element

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §8.2.2

#### Test 1 — 14289_2_8_2_2_t01

**Rule:** Content that is not considered real shall be an artifact

**Target:** SESimpleContentItem

**Complexity:** Medium
**Existing fixer:** `fixer_contenttag.go`

**Procedure:**

1. Resolve page content streams.
2. Check if content has any BDC/BMC marked-content operators.
3. If untagged, prepend "/Document <</MCID N>> BDC\n" and append "\nEMC".
4. Add MCID N to the Document structure element /K array.
5. Update /ParentTree with page-to-structure-element mapping.
6. Set /StructParents on the page dict.
7. Allocate new content stream and modified structure objects.
8. Existing implementation: `fixer_contenttag.go`.

### §8.2.4

#### Test 1 — 14289_2_8_2_4_t01

**Rule:** All structure elements shall belong to, or be role mapped to, at least one of the following namespaces specified in ISO 32000-2:2020, 14.8.6: — the PDF 1.7 namespace; — the PDF 2.0 namespace; — the MathML namespace

**Target:** SENonStandard

**Complexity:** Medium
**Existing fixer:** `fixer_structtree.go`

**Procedure:**

1. Identify custom structure element types not in the standard set.
2. Add /RoleMap entries mapping each custom type to a standard type (P, Span, Div, etc.).
3. Or add /NS on each structure element pointing to the appropriate namespace.
4. Allocate modified StructTreeRoot.
5. Existing implementation: `fixer_structtree.go`.

#### Test 2 — 14289_2_8_2_4_t02

**Rule:** A circular mapping shall not exist

**Target:** Structure element

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 3 — 14289_2_8_2_4_t03

**Rule:** Within a given explicitly provided namespace, structure types shall not be role mapped to other structure types in the same namespace

**Target:** Structure element

**Complexity:** Medium
**Existing fixer:** `fixer_structtree.go`

**Procedure:**

1. Identify custom structure element types not in the standard set.
2. Add /RoleMap entries mapping each custom type to a standard type (P, Span, Div, etc.).
3. Or add /NS on each structure element pointing to the appropriate namespace.
4. Allocate modified StructTreeRoot.
5. Existing implementation: `fixer_structtree.go`.

#### Test 4 — 14289_2_8_2_4_t04

**Rule:** All structure elements shall belong to, or be role mapped to, at least one of the following namespaces specified in ISO 32000-2:2020, 14.8.6: — the PDF 1.7 namespace; — the PDF 2.0 namespace; — the MathML namespace

**Target:** SENonStandard

**Complexity:** Medium
**Existing fixer:** `fixer_structtree.go`

**Procedure:**

1. Identify custom structure element types not in the standard set.
2. Add /RoleMap entries mapping each custom type to a standard type (P, Span, Div, etc.).
3. Or add /NS on each structure element pointing to the appropriate namespace.
4. Allocate modified StructTreeRoot.
5. Existing implementation: `fixer_structtree.go`.

### §8.2.5.2

#### Test 1 — 14289_2_8_2_5_2_t01

**Rule:** The structure tree root shall contain a single Document structure element as its only child, as specified in ISO 32000-2:2020, Annex L and ISO/TS 32005

**Target:** StructTreeRoot

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 2 — 14289_2_8_2_5_2_t02

**Rule:** The structure tree root shall contain a single Document structure element as its only child. The namespace for that element shall be specified as the PDF 2.0 namespace

**Target:** StructTreeRoot

**Complexity:** Medium
**Existing fixer:** `fixer_structtree.go`

**Procedure:**

1. Add /Namespaces array to StructTreeRoot.
2. Include PDF 1.7 (http://iso.org/pdf/ssn) and PDF 2.0 (http://iso.org/pdf2/ssn) namespace dicts.
3. Set /NS on structure elements to the appropriate namespace.
4. Allocate modified StructTreeRoot and structure elements.
5. Existing implementation: `fixer_structtree.go`.

### §8.2.5.8

#### Test 1 — 14289_2_8_2_5_8_t01

**Rule:** Each TOCI in the table of contents shall identify the target of the reference using the Ref entry, either directly on the TOCI structure element itself or on one of its child structure elements

**Target:** SETOCI

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §8.2.5.12

#### Test 1 — 14289_2_8_2_5_12_t01

**Rule:** Conforming files shall not use the H structure type

**Target:** SEH

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §8.2.5.14

#### Test 1 — 14289_2_8_2_5_14_t01

**Rule:** The Note standard structure type shall not be present in conforming documents unless role mapped to a structure element in the PDF 2.0 namespace

**Target:** SENote

**Complexity:** Medium
**Existing fixer:** `fixer_structtree.go`

**Procedure:**

1. Identify custom structure element types not in the standard set.
2. Add /RoleMap entries mapping each custom type to a standard type (P, Span, Div, etc.).
3. Or add /NS on each structure element pointing to the appropriate namespace.
4. Allocate modified StructTreeRoot.
5. Existing implementation: `fixer_structtree.go`.

#### Test 2 — 14289_2_8_2_5_14_t02

**Rule:** Real content that refers to footnotes or endnotes (real content enclosed in FENote structure elements) shall use the Ref entry on the referring structure element to reference the FENote. The corresponding FENote shall also use the Ref entry to identify all citations that reference it

**Target:** SEFENote

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 3 — 14289_2_8_2_5_14_t03

**Rule:** Real content that refers to footnotes or endnotes (real content enclosed in FENote structure elements) shall use the Ref entry on the referring structure element to reference the FENote. The corresponding FENote shall also use the Ref entry to identify all citations that reference it

**Target:** SEFENote

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 4 — 14289_2_8_2_5_14_t04

**Rule:** The value of the NoteType attribute of the FENote structure element shall be Footnote, Endnote or None

**Target:** SEFENote

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §8.2.5.20

#### Test 1 — 14289_2_8_2_5_20_t01

**Rule:** A link annotation and its associated content shall be enclosed in either a Link or Reference structure element

**Target:** Link annotation

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Create a Link structure element for each Link annotation.
2. Nest the Link annotation inside the Link structure element via OBJR.
3. Add to StructTreeRoot.
4. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 14289_2_8_2_5_20_t02

**Rule:** Link annotations that target different locations shall be in separate Link or Reference structure elements

**Target:** Link annotation

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Create a Link structure element for each Link annotation.
2. Nest the Link annotation inside the Link structure element via OBJR.
3. Add to StructTreeRoot.
4. Existing implementation: `fixer_annotflags.go`.

### §8.2.5.23

#### Test 1 — 14289_2_8_2_5_23_t01

**Rule:** A Ruby structure element shall contain a single RB structure element and a single RT structure element or a Ruby structure element shall consist of a four-element subsequence: RB, RP, RT, RP

**Target:** SERuby

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §8.2.5.24

#### Test 1 — 14289_2_8_2_5_24_t01

**Rule:** Content typeset as warichu shall be tagged in a three-element sequence consisting of the structure elements WP, WT and WP, grouped inside a Warichu structure element

**Target:** SEWarichu

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §8.2.5.25

#### Test 1 — 14289_2_8_2_5_25_t01

**Rule:** If Lbl structure elements are present, the ListNumbering attribute shall be present on the respective L structure element; in such cases the value None shall not be used

**Target:** SEL

**Complexity:** Low

**Procedure:**

1. Detect list structures from content analysis (bullet/number patterns).
2. Create L/LI/Lbl/LBody structure elements.
3. Mark content streams with BDC/EMC for each list item.

#### Test 2 — 14289_2_8_2_5_25_t02

**Rule:** Any real content within an LI structure element that is not enclosed in an Lbl structure element shall be enclosed in an LBody structure element

**Target:** SELI

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §8.2.5.26

#### Test 1 — 14289_2_8_2_5_26_t01

**Rule:** Tables shall be regular. A table cell shall not have intersection with other cells

**Target:** SETableCell

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 2 — 14289_2_8_2_5_26_t02

**Rule:** Tables shall be regular. Row groupings formed by THead, TBody and TFoot structure elements shall be regular. Table columns shall have the same number of rows (taking into account row spans) in table and each row groupings formed by THead, TBody and TFoot structure elements

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 3 — 14289_2_8_2_5_26_t03

**Rule:** Tables shall be regular. Table rows shall have the same number of columns (taking into account column spans)

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 4 — 14289_2_8_2_5_26_t04

**Rule:** Tables shall be regular. Table rows shall have the same number of columns (taking into account column spans)

**Target:** SETable

**Complexity:** Medium

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 5 — 14289_2_8_2_5_26_t05

**Rule:** When a table contains header cells, that table shall provide sufficient semantic information to allow accurate determination of which of its table header cells (structure element TH) pertain to other cells as specified in ISO 32000-2:2020, 14.8.5.7

**Target:** SETD

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

#### Test 6 — 14289_2_8_2_5_26_t06

**Rule:** When a table contains header cells, that table shall provide sufficient semantic information to allow accurate determination of which of its table header cells (structure element TH) pertain to other cells as specified in ISO 32000-2:2020, 14.8.5.7

**Target:** SETD

**Complexity:** Low
**Existing fixer:** `fixer_filestructure.go`

**Procedure:**

1. Full rewrite writes "%PDF-X.Y" at byte 0 followed by a binary comment line.
2. The writer automatically generates the correct header format.
3. Existing implementation: `fixer_filestructure.go`.

### §8.2.5.27

#### Test 1 — 14289_2_8_2_5_27_t01

**Rule:** When present, the Caption element shall be the first or the last child of the structure element

**Target:** Structure element

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §8.2.5.28.2

#### Test 1 — 14289_2_8_2_5_28_2_t01

**Rule:** A Figure structure element shall have at least one of the following properties: a) an alternate description (Alt property), as specified in ISO 32000-2:2020, 14.9.3; b) a replacement text (ActualText property) that represents the content enclosed by the Figure structure element

**Target:** SEFigure

**Complexity:** Medium

**Procedure:**

1. Identify Figure structure elements without /Alt.
2. Generate alt text via OCR on the image content, or set a placeholder.
3. Add /Alt to the structure element dictionary.
4. Allocate modified structure element.

### §8.2.5.29

#### Test 1 — 14289_2_8_2_5_29_t01

**Rule:** The math structure type shall occur only as a child of a Formula structure element

**Target:** Structure element

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §8.4.3

#### Test 1 — 14289_2_8_4_3_t01

**Rule:** In all cases, where real content maps to Unicode PUA values, an ActualText or Alt entry shall be present

**Target:** Glyph

**Complexity:** Medium

**Procedure:**

1. Identify structure elements with PUA-mapped characters.
2. Determine the actual Unicode text from font encoding or ToUnicode CMap.
3. Add /ActualText with the resolved text.
4. Allocate modified structure element.

#### Test 2 — 14289_2_8_4_3_t02

**Rule:** The ActualText entry shall not contain any PUA values

**Target:** CosActualText

**Complexity:** Low

**Procedure:**

1. Identify structure elements with PUA-mapped characters.
2. Determine the actual Unicode text from font encoding or ToUnicode CMap.
3. Add /ActualText with the resolved text.
4. Allocate modified structure element.

#### Test 3 — 14289_2_8_4_3_t03

**Rule:** The Alt entry shall not contain any PUA values

**Target:** CosAlt

**Complexity:** Low

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §8.4.4

#### Test 1 — 14289_2_8_4_4_t01

**Rule:** The default natural language for content and text strings shall be specified using the Lang entry, with a non-empty value, in the catalog dictionary

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_cataloglang.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Add /Lang with a BCP 47 language tag (e.g., "en", "fr", "de").
3. For structure elements, set /Lang on elements with different languages.
4. Allocate modified catalog or structure elements.
5. Existing implementation: `fixer_cataloglang.go`.

#### Test 2 — 14289_2_8_4_4_t02

**Rule:** If the Lang entry is present in the document's Catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as described in ISO 32000-2:2020, 14.9.2. A language identifier shall be a Language-Tag as defined in RFC 3066, Tags for the Identification of Languages

**Target:** CosLang

**Complexity:** Medium

**Procedure:**

1. Detect list structures from content analysis (bullet/number patterns).
2. Create L/LI/Lbl/LBody structure elements.
3. Mark content streams with BDC/EMC for each list item.

### §8.4.5.3.1

#### Test 1 — 14289_2_8_4_5_3_1_t01

**Rule:** For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary has a value of Identity-H or Identity-V, then any values for the Registry, Ordering, and Supplement keys may be used in the CIDSystemInfo dictionary of the CIDFont. - Otherwise the corresponding values of the Registry and Ordering keys in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be less than or equal to the value of the Supplement key in the CIDSystemInfo dictionary of the CMap

**Target:** Type 0 composite font

**Complexity:** High

**Procedure:**

1. Check if the required key exists in the font dictionary.
2. If missing, compute from the font program: /BaseFont from name table, /FirstChar//LastChar from encoding range.
3. Add the missing key.
4. Allocate modified font dictionary.

### §8.4.5.3.2

#### Test 1 — 14289_2_8_4_5_3_2_t01

**Rule:** All embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-2:2020, Table 115

**Target:** CIDFont

**Complexity:** High

**Procedure:**

1. Check if CIDFont has /CIDToGIDMap.
2. If missing, add /CIDToGIDMap /Identity (when CIDs map directly to GIDs).
3. Or generate a stream mapping CIDs to GIDs based on the font program cmap table.
4. Allocate modified CIDFont dictionary.

### §8.4.5.4

#### Test 1 — 14289_2_8_4_5_4_t01

**Rule:** All CMaps used within a file that conforms to PDF/UA-2, except those listed in ISO 32000-2:2020, Table 116, shall be embedded in that file, as described in ISO 32000-2:2020, 9.7.5

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

#### Test 2 — 14289_2_8_4_5_4_t02

**Rule:** For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Read /WMode from both the CMap dictionary and the embedded CMap stream.
2. Set both to the same value (0 for horizontal, 1 for vertical).
3. Allocate modified CMap stream.

#### Test 3 — 14289_2_8_4_5_4_t03

**Rule:** A CMap shall not reference any other CMap except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116

**Target:** CMap

**Complexity:** High

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.

### §8.4.5.5.1

#### Test 1 — 14289_2_8_4_5_5_1_t01

**Rule:** The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-2:2020, 9.9

**Target:** PDFont

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 2 — 14289_2_8_4_5_5_1_t02

**Rule:** Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced solely in text rendering mode 3 is not rendered and is thus exempt from the requirements that impact the visual representation of the glyphs of a font. In all cases for TrueType fonts that are to be rendered, character codes shall be able to be mapped to glyphs in accordance with ISO 32000-2:2020, 9.6.5, without the use of a non-standard mapping chosen by the conforming processor

**Target:** Glyph

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

### §8.4.5.6

#### Test 1 — 14289_2_8_4_5_6_t01

**Rule:** For every font embedded in a conforming file and referenced for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent for every glyph

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract binary widths from the font program (hmtx table for TrueType, charstring widths for CFF).
2. Read /UnitsPerEm from the head table.
3. For each charcode, compute expected PDF width: binaryWidth * 1000 / unitsPerEm.
4. Compare with the /Widths array (simple fonts) or /W array (CIDFonts).
5. Replace mismatched entries with the correct values.
6. Allocate modified font dictionary.
7. Existing implementation: `fixer_fonts.go`.

### §8.4.5.7

#### Test 1 — 14289_2_8_4_5_7_t01

**Rule:** For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain at least Microsoft Unicode (3,1 – Platform ID=3, Encoding ID=1), or Macintosh Roman (1,0 – Platform ID=1, Encoding ID=0) 'cmap' subtable

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 2 — 14289_2_8_4_5_7_t02

**Rule:** All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the Font dictionary. In addition, no non-symbolic TrueType font shall define a Differences array unless all of the glyph names in the Differences array are listed in the Adobe Glyph List and, if the font program is embedded, it shall contain at least the Microsoft Unicode (3,1 – Platform ID = 3, Encoding ID = 1) encoding in the 'cmap' table

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

#### Test 3 — 14289_2_8_4_5_7_t03

**Rule:** Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary

**Target:** TrueType font

**Complexity:** Low

**Procedure:**

1. Check if the font is symbolic (Flags bit 3 set in FontDescriptor).
2. If symbolic: remove /Encoding from the font dictionary.
3. If non-symbolic: set /Encoding to /WinAnsiEncoding or /MacRomanEncoding.
4. Allocate modified font dictionary.

#### Test 4 — 14289_2_8_4_5_7_t04

**Rule:** The 'cmap' subtable in the embedded font program shall either contain the Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0 – Platform ID=1, Encoding ID=1) encoding

**Target:** TrueType font

**Complexity:** High

**Procedure:**

1. Identify the font program format (TrueType, Type 1, CFF/OpenType).
2. Read the font program bytes from the system font directory or font substitution.
3. Create a stream with the font data.
4. Set /FontFile (Type 1), /FontFile2 (TrueType), or /FontFile3 (CFF) on FontDescriptor.
5. Allocate the stream and modified FontDescriptor.
6. Note: requires access to the original font file. System font matching by /BaseFont name.

### §8.4.5.8

#### Test 1 — 14289_2_8_4_5_8_t01

**Rule:** The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 14289-2, 8.4.5.8

**Target:** Glyph

**Complexity:** Medium
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the embedded font program from /FontFile, /FontFile2, or /FontFile3.
2. Parse the cmap table (TrueType) or charset (CFF) to build GID-to-Unicode mapping.
3. Construct charcode-to-Unicode mapping using the font encoding.
4. Generate a CMap stream with beginbfchar entries (chunks of 100).
5. Add the CMap as /ToUnicode on the font dictionary.
6. Allocate both the CMap stream and modified font dict.
7. Existing implementation: `fixer_fonts.go`.

#### Test 2 — 14289_2_8_4_5_8_t02

**Rule:** If a ToUnicode CMap is present, the Unicode values it specifies shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE

**Target:** Glyph

**Complexity:** High
**Existing fixer:** `fixer_fonts.go`

**Procedure:**

1. Extract the embedded font program from /FontFile, /FontFile2, or /FontFile3.
2. Parse the cmap table (TrueType) or charset (CFF) to build GID-to-Unicode mapping.
3. Construct charcode-to-Unicode mapping using the font encoding.
4. Generate a CMap stream with beginbfchar entries (chunks of 100).
5. Add the CMap as /ToUnicode on the font dictionary.
6. Allocate both the CMap stream and modified font dict.
7. Existing implementation: `fixer_fonts.go`.

### §8.4.5.9

#### Test 1 — 14289_2_8_4_5_9_t01

**Rule:** A file in conformance with PDF/UA-2 shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream

**Target:** Glyph

**Complexity:** Low

**Procedure:**

1. Parse content stream operators.
2. Remove or replace undefined operators.
3. Re-serialize the content stream.
4. Allocate modified stream.

### §8.6

#### Test 1 — 14289_2_8_6_t01

**Rule:** Text strings intended to be human readable shall not use the Unicode PUA

**Target:** CosTextString

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §8.7

#### Test 1 — 14289_2_8_7_t01

**Rule:** All optional content configuration dictionaries in the document, including the default optional content configuration dictionary, shall contain a Name entry (see ISO 32000-2:2020, Table 96) whose value is a non-empty text string when: a) a document contains a Configs entry in the OCProperties entry of the catalog dictionary (see ISO 32000-2:2020, Table 29), and b) the Configs entry contains at least one optional content configuration dictionary

**Target:** PDOCConfig

**Complexity:** Medium
**Existing fixer:** `fixer_catalog.go`

**Procedure:**

1. Resolve catalog dictionary.
2. Remove /OCProperties key.
3. Allocate updated catalog.
4. Existing implementation: `fixer_catalog.go`.

#### Test 2 — 14289_2_8_7_t02

**Rule:** The AS key shall not appear in any optional content configuration dictionary

**Target:** PDOCConfig

**Complexity:** Medium

**Procedure:**

1. Resolve OCG configuration dictionaries.
2. Add /Name key to each config dict.
3. Allocate modified config dicts.

### §8.8

#### Test 1 — 14289_2_8_8_t01

**Rule:** All destinations whose target lies within the current document shall be structure destinations

**Target:** PDDestination

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

#### Test 2 — 14289_2_8_8_t02

**Rule:** All destinations whose target lies within the current document shall be structure destinations

**Target:** Action dict

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §8.9.2.2

#### Test 1 — 14289_2_8_9_2_2_t01

**Rule:** Annotations shall be artifacts, if the Invisible flag is set in accordance with ISO 32000-2:2020, Table 167

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_contenttag.go`

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.
7. Existing implementation: `fixer_contenttag.go`.

#### Test 2 — 14289_2_8_9_2_2_t02

**Rule:** Annotations shall be artifacts if the NoView flag is set and the ToggleNoView flag is not set in accordance with ISO 32000-2:2020, Table 167

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_contenttag.go`

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.
7. Existing implementation: `fixer_contenttag.go`.

### §8.9.2.3

#### Test 1 — 14289_2_8_9_2_3_t01

**Rule:** Markup annotations shall be enclosed within Annot structure elements

**Target:** PDMarkupAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 14289_2_8_9_2_3_t02

**Rule:** When both RC and Contents entries are present for markup annotation, they shall be textually equivalent

**Target:** PDMarkupAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §8.9.2.4.7

#### Test 1 — 14289_2_8_9_2_4_7_t01

**Rule:** If the Name entry is insufficient to describe the intent of the stamp annotation, a Contents entry describing the author’s intent shall be provided

**Target:** PDRubberStampAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §8.9.2.4.8

#### Test 1 — 14289_2_8_9_2_4_8_t01

**Rule:** A Contents entry describing the author’s intent shall be provided for Ink annotation

**Target:** PDInkAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §8.9.2.4.9

#### Test 1 — 14289_2_8_9_2_4_9_t01

**Rule:** Popup annotations shall not be present in the structure tree

**Target:** PDPopupAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve Popup annotations.
2. Ensure /Parent references the parent annotation.
3. Allocate modified Popup annotation dict.
4. Existing implementation: `fixer_annotflags.go`.

### §8.9.2.4.10

#### Test 1 — 14289_2_8_9_2_4_10_t01

**Rule:** When a file attachment annotation references a file specification dictionary, the file specification dictionary shall include an AFRelationship entry

**Target:** PDFileAttachmentAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve file specification dictionaries.
2. Add /AFRelationship /Unspecified (or more specific value if determinable).
3. Allocate modified file spec dicts.
4. Existing implementation: `fixer_annotflags.go`.

### §8.9.2.4.11

#### Test 1 — 14289_2_8_9_2_4_11_t01

**Rule:** Sound annotation cannot be used when conforming to PDF/UA-2

**Target:** PDSoundAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Walk catalog /OpenAction, /AA, page /AA, and annotation /A and /AA entries.
2. Remove any action with /S = JavaScript, Launch, Sound, Movie, ResetForm, ImportData, or Hide.
3. For /AA dicts, remove only the entries with forbidden action types.
4. Allocate modified objects.
5. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 14289_2_8_9_2_4_11_t02

**Rule:** Movie annotation cannot be used when conforming to PDF/UA-2

**Target:** PDMovieAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Walk catalog /OpenAction, /AA, page /AA, and annotation /A and /AA entries.
2. Remove any action with /S = JavaScript, Launch, Sound, Movie, ResetForm, ImportData, or Hide.
3. For /AA dicts, remove only the entries with forbidden action types.
4. Allocate modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §8.9.2.4.12

#### Test 1 — 14289_2_8_9_2_4_12_t01

**Rule:** Screen annotations shall include a Contents entry

**Target:** PDScreenAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §8.9.2.4.13

#### Test 1 — 14289_2_8_9_2_4_13_t01

**Rule:** A widget annotation of zero height and width shall be an artifact

**Target:** Widget annotation

**Complexity:** Medium
**Existing fixer:** `fixer_contenttag.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_contenttag.go`.

### §8.9.2.4.14

#### Test 1 — 14289_2_8_9_2_4_14_t01

**Rule:** A printer's mark annotation shall be an artifact

**Target:** PDPrinterMarkAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_contenttag.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_contenttag.go`.

### §8.9.2.4.15

#### Test 1 — 14289_2_8_9_2_4_15_t01

**Rule:** Trap network annotations shall not be used in files conforming to PDF/UA-2

**Target:** PDTrapNetAnnot

**Complexity:** Low
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §8.9.2.4.16

#### Test 1 — 14289_2_8_9_2_4_16_t01

**Rule:** When used as real content, Watermark annotations shall be enclosed within Annot structure elements

**Target:** PDWatermarkAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §8.9.2.4.19

#### Test 1 — 14289_2_8_9_2_4_19_t01

**Rule:** 3D annotation shall include alternate description in respective Contents entry

**Target:** PD3DAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 14289_2_8_9_2_4_19_t02

**Rule:** RichMedia annotation shall include alternate description in respective Contents entry

**Target:** PDRichMediaAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §8.9.3.3

#### Test 1 — 14289_2_8_9_3_3_t01

**Rule:** Every page that includes an annotation shall contain a Tabs entry in its page dictionary in accordance with ISO 32000-2:2020, Table 31, and its value shall be A, W or S

**Target:** Page dict

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.
7. Existing implementation: `fixer_annotflags.go`.

### §8.9.4.2

#### Test 1 — 14289_2_8_9_4_2_t01

**Rule:** Where an annotation has a Contents entry and the directly enclosing structure element has an Alt entry, the values of Alt and Contents shall be identical

**Target:** PDAnnot

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.
5. Existing implementation: `fixer_annotflags.go`.

### §8.10.1

#### Test 1 — 14289_2_8_10_1_t01

**Rule:** Each widget annotation shall be enclosed by a Form structure element unless the widget annotation is an artifact

**Target:** Widget annotation

**Complexity:** Medium
**Existing fixer:** `fixer_contenttag.go`

**Procedure:**

1. Resolve form field / widget annotation dictionaries.
2. Add /TU with a descriptive tooltip, or /Contents with a description.
3. Use the field /T (name) as fallback text if no better description is available.
4. Allocate modified field dicts.
5. Existing implementation: `fixer_contenttag.go`.

#### Test 2 — 14289_2_8_10_1_t02

**Rule:** A Form structure element shall contain at most one widget annotation

**Target:** SEForm

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve form field / widget annotation dictionaries.
2. Add /TU with a descriptive tooltip, or /Contents with a description.
3. Use the field /T (name) as fallback text if no better description is available.
4. Allocate modified field dicts.
5. Existing implementation: `fixer_annotflags.go`.

#### Test 3 — 14289_2_8_10_1_t03

**Rule:** XFA forms shall not be present

**Target:** PDAcroForm

**Complexity:** Medium
**Existing fixer:** `fixer_xfa.go`

**Procedure:**

1. Resolve /AcroForm dictionary.
2. Remove /XFA key.
3. Allocate modified AcroForm.
4. Existing implementation: `fixer_xfa.go`.

### §8.10.2.3

#### Test 1 — 14289_2_8_10_2_3_t01

**Rule:** If a label for a widget annotation is not present, a Contents entry shall be provided to supply description and context for the widget

**Target:** Widget annotation

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve form field / widget annotation dictionaries.
2. Add /TU with a descriptive tooltip, or /Contents with a description.
3. Use the field /T (name) as fallback text if no better description is available.
4. Allocate modified field dicts.
5. Existing implementation: `fixer_annotflags.go`.

#### Test 2 — 14289_2_8_10_2_3_t02

**Rule:** If an additional action (AA) entry is present in a widget annotation dictionary, the respective widget's Contents entry shall be present

**Target:** Widget annotation

**Complexity:** Medium
**Existing fixer:** `fixer_annotflags.go`

**Procedure:**

1. Resolve form field / widget annotation dictionaries.
2. Add /TU with a descriptive tooltip, or /Contents with a description.
3. Use the field /T (name) as fallback text if no better description is available.
4. Allocate modified field dicts.
5. Existing implementation: `fixer_annotflags.go`.

### §8.10.3.3

#### Test 1 — 14289_2_8_10_3_3_t01

**Rule:** For text fields, when an RV entry is present a V entry shall also be present, and they shall be textually equivalent

**Target:** PDTextField

**Complexity:** Medium

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

### §8.10.3.5

#### Test 1 — 14289_2_8_10_3_5_t01

**Rule:** If a portion of the appearance of a signature is represented by a graphic, alternative text shall be provided for that graphic

**Target:** SEGraphicContentItem

**Complexity:** High

**Procedure:**

1. Resolve annotation dictionary.
2. Create an /AP dictionary with /N (normal appearance) as a Form XObject stream.
3. Render the annotation visual content into the Form XObject (border, text, check marks).
4. Set /AP on the annotation.
5. Allocate Form XObject stream and modified annotation.
6. Note: requires rendering logic specific to each annotation subtype.

### §8.11.1

#### Test 1 — 14289_2_8_11_1_t01

**Rule:** The Metadata stream as specified in ISO 32000-2:2020, 14.3 in the document catalog dictionary shall contain a dc:title entry

**Target:** XMP metadata stream

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Check if catalog has /Metadata.
2. If missing, generate XMP from /Info dict values.
3. If present but invalid, repair the XMP XML structure.
4. Allocate XMP stream and update catalog.
5. Existing implementation: `fixer_xmp.go`.

#### Test 2 — 14289_2_8_11_1_t02

**Rule:** The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-2:2020, 14.3. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML

**Target:** PDDocument

**Complexity:** Medium
**Existing fixer:** `fixer_xmp.go`

**Procedure:**

1. Check if catalog has /Metadata.
2. If missing, generate XMP from /Info dict values.
3. If present but invalid, repair the XMP XML structure.
4. Allocate XMP stream and update catalog.
5. Existing implementation: `fixer_xmp.go`.

### §8.11.2

#### Test 1 — 14289_2_8_11_2_t01

**Rule:** The ViewerPreferences dictionary of the document catalog dictionary shall be present and shall contain at least the DisplayDocTitle key with a value of true, as described in ISO 32000-2:2020, Table 147

**Target:** Document trailer/xref

**Complexity:** Medium
**Existing fixer:** `fixer_viewerprefs.go`

**Procedure:**

1. Detect table structures from spatial analysis of text elements and ruled lines.
2. Create Table/TR/TH/TD structure elements.
3. Add /Scope attribute to TH cells (Row, Column, or Both).
4. Add /ColSpan and /RowSpan for merged cells.
5. Mark content streams with BDC/EMC for each cell.
6. Note: requires heuristic table detection. Existing infrastructure in heuristics/table.go.
7. Existing implementation: `fixer_viewerprefs.go`.

### §8.14.1

#### Test 1 — 14289_2_8_14_1_t01

**Rule:** The Desc entry shall be present on all file specification dictionaries present in the EmbeddedFiles name tree of a conforming document

**Target:** File specification dict

**Complexity:** High

**Procedure:**

1. Resolve the structure tree or accessibility-related object.
2. Add or correct the required entry for PDF/UA compliance.
3. Update the structure tree if structural changes are needed.
4. Allocate all modified objects.

