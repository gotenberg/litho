# VeraPDF Violations — Complete Rule Set by Profile

Exhaustive list of **all** validation rules defined in the VeraPDF validation profiles,
organized by profile. These are all possible violations that VeraPDF can report.

**Source:** [veraPDF/veraPDF-validation-profiles](https://github.com/veraPDF/veraPDF-validation-profiles)
**VeraPDF version:** 1.28.2
**Generated:** 2026-03-21

## Summary

| Profile | Specification | Total Rules |
|---------|---------------|-------------|
| PDF/A-1a | ISO 19005-1:2005, Cor.1:2007 and Cor.2:2011, Level A | 135 |
| PDF/A-1b | ISO 19005-1:2005, Cor.1:2007 and Cor.2:2011, Level B | 129 |
| PDF/A-2a | ISO 19005-2:2011, Level A | 153 |
| PDF/A-2b | ISO 19005-2:2011, Level B | 144 |
| PDF/A-2u | ISO 19005-2:2011, Level U | 146 |
| PDF/A-3b | ISO 19005-3:2012, Level B | 146 |
| PDF/A-4 | ISO 19005-4:2020 | 109 |
| PDF/A-4e | ISO 19005-4:2020, Level E | 109 |
| PDF/A-4f | ISO 19005-4:2020, Level F | 109 |
| PDF/UA-1 | ISO 14289-1:2014 | 106 |
| PDF/UA-2 | ISO 14289-2:2024 | 91 |
| **Total** | | **1377** |

## PDF/A-1a

**PDF/A-1A validation profile**
*Validation rules against ISO 19005-1:2005, Cor.1:2007 and Cor.2:2011, Level A*
**Total rules: 135**

### Clause 6.1

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.1.2 | 1 | CosDocument | The % character of the file header shall occur at byte offset 0 of the file. The first line of a PDF file is a header identifying the version of the PDF specification to which the file conforms | File header %1 (offset = %2) starts at non-zero offset or does not match the pattern %PDF-n.m |
| 6.1.2 | 2 | CosDocument | The file header line shall be immediately followed by a comment consisting of a % character followed by at least four characters, each of whose encoded byte values shall have a decimal value greater than 127 | Binary comment in the file header is missing or does not start with 4 bytes with byte values above 127 (first four bytes = %1, %2, %3, %4) |
| 6.1.3 | 1 | CosDocument | The file trailer dictionary shall contain the ID keyword. The file trailer referred to is either the last trailer dictionary in a PDF file, as described in PDF Reference 3.4.4 and 3.4.5, or the first page trailer in a linearized PDF file, as descr... | Missing ID in the document trailer |
| 6.1.3 | 2 | CosTrailer | The keyword Encrypt shall not be used in the trailer dictionary | Encrypt keyword is present in the trailer dictionary |
| 6.1.3 | 3 | CosDocument | No data shall follow the last end-of-file marker except a single optional end-of-line marker | %1 byte(s) of data is present after the last end-of-file marker |
| 6.1.3 | 4 | CosDocument | In a linearized PDF, if the ID keyword is present in both the first page trailer dictionary and the last trailer dictionary, the value to both instances of the ID keyword shall be identical | Last ID (%1) is present in the Linearized PDF and does not match the first page ID (%2) |
| 6.1.4 | 1 | CosXRef | In a cross reference subsection header the starting object number and the range shall be separated by a single SPACE character (20h) | Extra or missing spacings around in a subsection header of the cross reference table |
| 6.1.4 | 2 | CosXRef | The xref keyword and the cross reference subsection header shall be separated by a single EOL marker | Extra spacings or missing EOL characters after the 'xref' keyword in the cross reference table |
| 6.1.4 | 3 | PDDocument | Xref streams shall not be used | The document uses xref streams |
| 6.1.6 | 1 | CosString | Hexadecimal strings shall contain an even number of non-white-space characters | A hexadecimal string contains odd number (%1) of non-white-space characters |
| 6.1.6 | 2 | CosString | All non-white-space characters in hexadecimal strings shall be in the range 0 to 9, A to F or a to f | Hexadecimal string contains non-white-space characters outside the range 0 to 9, A to F or a to f |
| 6.1.7 | 1 | CosStream | The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED character after the stream keyword and preceding the EOL marker before the endstream keyword | Actual length of the stream (%1 byte(s)) does not match the value of the Length key in the Stream dictionary (%2 byte(s)) |
| 6.1.7 | 2 | CosStream | The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED character. The endstream keyword shall be preceded by an EOL marker | Extra spacings or missing EOL characters around keywords 'stream' and 'endstream' |
| 6.1.7 | 3 | CosStream | A stream object dictionary shall not contain the F, FFilter, or FDecodeParms keys | A stream object dictionary contains %1 key(s) |
| 6.1.8 | 1 | CosIndirect | The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded b... | Extra spacings or missing EOL characters around indirect object/generation number or keywords 'obj' and 'endobj' |
| 6.1.10 | 1 | CosFilter | The LZWDecode filter shall not be permitted | Unknown or not permitted Stream filter %1 is used |
| 6.1.10 | 2 | CosIIFilter | The LZWDecode filter shall not be permitted | LZW compression is used in the inline image |
| 6.1.11 | 1 | CosFileSpecification | A file specification dictionary, as defined in PDF 3.10.2, shall not contain the EF key | A file specification dictionary contains the EF key |
| 6.1.11 | 2 | CosDocument | A file's name dictionary, as defined in PDF Reference 3.6.3, shall not contain the EmbeddedFiles key | The document contains embedded files (EmbeddedFiles key is present in the file's name dictionary) |
| 6.1.12 | 1 | CosInteger | Largest Integer value is 2,147,483,647. Smallest integer value is -2,147,483,648 | Integer value %1 out of range |
| 6.1.12 | 2 | CosReal | Absolute real value must be less than or equal to 32767.0 | Real value %1 out of range |
| 6.1.12 | 3 | CosString | Maximum length of a string (in bytes) is 65535 | String length (%1) exceeded 65535 |
| 6.1.12 | 4 | CosName | Maximum length of a name (in bytes) is 127 | Name length (%1) exceeded 127 |
| 6.1.12 | 5 | CosArray | Maximum capacity of an array (in elements) is 8191 | Array capacity (%1) exceeded 8191 |
| 6.1.12 | 6 | CosDict | Maximum capacity of a dictionary (in entries) is 4095 | Dictionary capacity (%1) exceeded 4095 |
| 6.1.12 | 7 | CosDocument | Maximum number of indirect objects in a PDF file is 8,388,607 | Number of indirect objects in a PDF file (%1) exceeded 8,388,607 |
| 6.1.12 | 8 | Op_q_gsave | Maximum depth of graphics state nesting by q and Q operators is 28 | Depth of graphics state nesting of q/Q operators (%1) exceeded 28 |
| 6.1.12 | 9 | PDDeviceN | Maximum number of DeviceN components is 8 | Number of DeviceN components (%1) exceeded 8 |
| 6.1.12 | 10 | CMapFile | Maximum value of a CID (character identifier) is 65,535 | Value of a CID (%1) exceeded 65,535 |
| 6.1.13 | 1 | CosDocument | The document catalog dictionary shall not contain a key with the name OCProperties | The document catalog dictionary contains the OCProperties entry |
### Clause 6.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.2.2 | 1 | ICCOutputProfile | A PDF/A-1 OutputIntent is an OutputIntent dictionary, as defined by PDF Reference 9.10.4, that is included in the file's OutputIntents array and has GTS_PDFA1 as the value of its S key and a valid ICC profile stream as the value its DestOutputProf... | The embedded PDF/A Output Intent colour profile is either invalid or does not provide BToA information |
| 6.2.2 | 2 | OutputIntents | If a file's OutputIntents array contains more than one entry, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream | File's OutputIntents array contains output intent dictionaries with non-matching destination output profiles (indirect keys %1) |
| 6.2.3.2 | 1 | ICCInputProfile | All ICCBased colour spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5 | The embedded ICC profile (Device Class = %1, color space = %2, version = %3) is either invalid or does not satisfy PDF 1.4 requirements |
| 6.2.3.2 | 2 | ICCProfile | All ICCBased colour spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5. The number of color components in the color space described by the ICC profile data must match the number of components actually in the ICC prof... | The N entry (value %1) in the ICC profile dictionary is missing or does not match the number of components in the embedded ICC profile (color space... |
| 6.2.3.3 | 1 | PDDeviceRGB | DeviceRGB may be used only if the file has a PDF/A-1 OutputIntent that uses an RGB colour space | DeviceRGB colour space is used without RGB output intent profile |
| 6.2.3.3 | 2 | PDDeviceCMYK | DeviceCMYK may be used only if the file has a PDF/A-1 OutputIntent that uses a CMYK colour space | DeviceCMYK colour space is used without CMYK output intent profile |
| 6.2.3.3 | 3 | PDDeviceGray | If an uncalibrated colour space is used in a file then that file shall contain a PDF/A-1 OutputIntent, as defined in 6.2.2 | DeviceGray colour space is used without output intent profile |
| 6.2.4 | 1 | PDXImage | An Image dictionary shall not contain the Alternates key | Alternates key is present in the Image dictionary |
| 6.2.4 | 2 | PDXObject | An XObject dictionary (Image or Form) shall not contain the OPI key | OPI key is present in the XObject dictionary |
| 6.2.4 | 3 | PDXImage | If an Image dictionary contains the Interpolate key, its value shall be false | The value of the Interpolate key in the Image dictionary is true |
| 6.2.4 | 4 | PDXImage | If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4 or 8 | The value of the BitsPerComponent key in the Image dictionary is %1 |
| 6.2.4 | 5 | PDMaskImage | If an image mask dictionary contains the BitsPerComponent key, its value shall be 1 | The value of the BitsPerComponent key in the image mask dictionary is %1 |
| 6.2.5 | 1 | PDXForm | A form XObject dictionary shall not contain the Subtype2 key with a value of PS or the PS key | The form XObject dictionary contains a PS key or Subtype2 key with value PS |
| 6.2.6 | 1 | PDXForm | A conforming file shall not contain any reference XObjects | The document contains a reference XObject (Ref key in the form XObject dictionary) |
| 6.2.7 | 1 | PDXObject | A conforming file shall not contain any PostScript XObjects | The document contains a PostScript XObject |
| 6.2.8 | 1 | PDExtGState | An ExtGState dictionary shall not contain the TR key | An ExtGState dictionary contains the TR key |
| 6.2.8 | 2 | PDExtGState | An ExtGState dictionary shall not contain the TR2 key with a value other than Default | An ExtGState dictionary contains the TR2 key with a value other than Default |
| 6.2.9 | 1 | CosRenderingIntent | Where a rendering intent is specified, its value shall be one of the four values defined in PDF Reference RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation | A rendering intent with non-standard value %1 is used |
| 6.2.10 | 1 | Op_Undefined | A content stream shall not contain any operators not defined in PDF Reference even if such operators are bracketed by the BX/EX compatibility operators | A content stream contains operator %1 not defined in PDF Reference |
### Clause 6.3

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.3.2 | 1 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary | A Font dictionary has value %1 of Type entry instead of Font |
| 6.3.2 | 2 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Subtype - name - (Required) The type of font; must be "Type1" for Type 1 fonts, "MMType1" for multiple master fonts, "TrueType" for TrueType... | Invalid Subtype entry in font dictionary with value %1 |
| 6.3.2 | 3 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. BaseFont - name - (Required) The PostScript name of the font | A BaseFont entry is missing or has invalid type |
| 6.3.2 | 4 | PDSimpleFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array | A non-standard simple font dictionary has missing or invalid FirstChar entry |
| 6.3.2 | 5 | PDSimpleFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array | A non-standard simple font dictionary has missing or invalid LastChar entry |
| 6.3.2 | 6 | PDSimpleFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths | Widths array is missing or has invalid size %1 instead of %2 |
| 6.3.2 | 7 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.4 ar... | Unsupported font file format %1 of the embedded font |
| 6.3.3.1 | 1 | PDType0Font | For any given composite (Type 0) font referenced within a conforming file, the CIDSystemInfo entries of its CIDFont and CMap dictionaries shall be compatible. In other words, the Registry and Ordering strings of the CIDSystemInfo dictionaries for ... | Registry and Ordering entries in the CIDFont and CMap dictionaries of a Type 0 font are not compatible (CIDSystemInfo Ordering = %1, CMap Ordering ... |
| 6.3.3.2 | 1 | PDCIDFont | For all Type 2 CIDFonts that are used for rendering, the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in PDF Reference Table 5.13 | A Type 2 CIDFont dictionary has missing or invalid CIDToGIDMap entry |
| 6.3.3.3 | 1 | PDCMap | All CMaps used within a conforming file, except Identity-H and Identity-V, shall be embedded in that file as described in PDF Reference 5.6.4 | A CMap is different from "Identity-H" or "Identity-V" and is not embedded |
| 6.3.3.3 | 2 | CMapFile | For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream | WMode entry (value %1) in the embedded CMap and in the CMap dictionary (value %2) are not identical |
| 6.3.4 | 1 | PDFont | The font programs for all fonts used within a conforming file shall be embedded within that file, as defined in PDF Reference 5.8, except when the fonts are used exclusively with text rendering mode 3 | The font program is not embedded |
| 6.3.5 | 1 | Glyph | Embedded font programs shall define all font glyphs referenced for rendering with conforming file | Not all glyphs referenced for rendering are present in the embedded font program |
| 6.3.5 | 2 | PDType1Font | For all Type 1 font subsets referenced within a conforming file, the font descriptor dictionary shall include a CharSet string listing the character names defined in the font subset, as described in PDF Reference Table 5.18 | A Type1 font subset does not define CharSet entry in its Descriptor dictionary |
| 6.3.5 | 3 | PDCIDFont | For all CIDFont subsets referenced within a conforming file, the font descriptor dictionary shall include a CIDSet stream identifying which CIDs are present in the embedded CIDFont file, as described in PDF Reference Table 5.20 | A CIDSet entry in the Font descriptor is missing or does not correctly identify all glyphs present in the embedded font subset and used for rendering |
| 6.3.6 | 1 | Glyph | For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent | Glyph width %1 in the embedded font program is not consistent with the Widths entry of the font dictionary (value %2) |
| 6.3.7 | 1 | PDTrueTypeFont | All non-symbolic TrueType fonts shall specify MacRomanEncoding or WinAnsiEncoding, either as the value of the Encoding entry in the font dictionary or as the value of the BaseEncoding entry in the dictionary that is the value of the Encoding entry... | A non-symbolic TrueType font encoding does not define a correct mapping to the Adobe Glyph List (Encoding = %1, Encoding entry contains a Differenc... |
| 6.3.7 | 2 | PDTrueTypeFont | All symbolic TrueType fonts shall not specify an Encoding entry in the font dictionary | A symbolic TrueType font specifies an Encoding entry in its dictionary |
| 6.3.7 | 3 | TrueTypeFontProgram | Font programs' "cmap" tables for all symbolic TrueType fonts shall contain exactly one encoding | The embedded font program for a symbolic TrueType font contains %1 cmap subtables |
| 6.3.8 | 1 | Glyph | The font dictionary shall include a ToUnicode entry whose value is a CMap stream object that maps character codes to Unicode values, as described in PDF Reference 5.9, unless the font meets any of the following three conditions: (*) fonts that use... | The font does not define Unicode character map |
### Clause 6.4

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.4 | 1 | PDExtGState | If an SMask key appears in an ExtGState dictionary, its value shall be None | An ExtGState contains SMask key with a value other than None |
| 6.4 | 2 | PDXObject | An XObject dictionary shall not contain the SMask key | An XObject contains an SMask key |
| 6.4 | 3 | PDGroup | A Group object with an S key with a value of Transparency shall not be included in a form XObject. A Group object with an S key with a value of Transparency shall not be included in a page dictionary | A transparency group is present in a form XObject or page dictionary |
| 6.4 | 4 | PDExtGState | If a BM key is present in an ExtGState object, its value shall be Normal or Compatible | An ExtGState dictionary contains the BM key (blend mode) with value %1 that is neither Normal or Compatible |
| 6.4 | 5 | PDExtGState | If a CA key is present in an ExtGState object, its value shall be 1.0 | An ExtGState dictionary contains the CA key (stroke alpha) with value %1 other than 1.0 |
| 6.4 | 6 | PDExtGState | If a ca key is present in an ExtGState object, its value shall be 1.0 | An ExtGState dictionary contains the ca key (fill alpha) with value %1 other than 1.0 |
### Clause 6.5

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.5.2 | 1 | PDAnnot | Annotation types not defined in PDF Reference shall not be permitted. Additionally, the FileAttachment, Sound and Movie types shall not be permitted | Unknown or not permitted Annotation type %1 |
| 6.5.3 | 1 | PDAnnot | An annotation dictionary shall not contain the CA key with a value other than 1.0 | An annotation dictionary contains the CA key with value %1 instead of 1.0 |
| 6.5.3 | 2 | PDAnnot | An annotation dictionary shall contain the F key. The F key’s Print flag bit shall be set to 1 and its Hidden, Invisible and NoView flag bits shall be set to 0 | Annotation flags are either missing or set the annotation to be hidden/invisible or non-printable (F = %1, Print = %2, Hidden = %3, Invisible = %4,... |
| 6.5.3 | 3 | PDAnnot | An annotation dictionary shall not contain the C array or the IC array unless the colour space of the DestOutputProfile in the PDF/A-1 OutputIntent dictionary, defined in 6.2.2, is RGB | Annotation's color (C present = %1) or interior color (IC present = %2) is used without specifying RGB-based destination output profile |
| 6.5.3 | 4 | PDAnnot | For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key | Annotation's appearance dictionary contains entries %1 other than N |
| 6.5.3 | 5 | PDAnnot | If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary | An annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, but the value of the N key is %1 instead of appeara... |
| 6.5.3 | 6 | PDAnnot | If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream | An annotation dictionary's Subtype key has a value %1 and its FT key has a value %2, but the value of the N key is not an appearance stream |
### Clause 6.6

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.6.1 | 1 | PDAction | The Launch, Sound, Movie, ResetForm, ImportData and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted. The Hide action shall not be permitted (Corrigendum 2) | Unknown or not permitted Action type %1 |
| 6.6.1 | 2 | PDNamedAction | Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted | Unknown or not permitted named action %1 |
| 6.6.1 | 3 | PDWidgetAnnot | Interactive form fields shall not perform actions of any type | Interactive form field contains an action object ('A' key) |
| 6.6.2 | 1 | PDWidgetAnnot | A Widget annotation dictionary shall not include an AA entry for an additional-actions dictionary | A Widget annotation contains an additional-actions dictionary (AA entry) |
| 6.6.2 | 2 | PDFormField | A Field dictionary shall not include an AA entry for an additional-actions dictionary | A Field dictionary contains an additional-actions dictionary (AA entry) |
| 6.6.2 | 3 | PDDocument | The document catalog dictionary shall not include an AA entry for an additional-actions dictionary | The document catalog dictionary contains an additional-actions dictionary (AA entry) |
### Clause 6.7

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.7.2 | 1 | PDDocument | The document catalog dictionary of a conforming file shall contain the Metadata key. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML | The document catalog dictionary doesn't contain metadata key or metadata stream dictionary does not contain either entry Type with value /Metadata ... |
| 6.7.2 | 2 | PDMetadata | The Metadata object stream dictionary in the document’s catalog shall not contain the Filter key | The catalog metadata object stream dictionary contains the Filter key |
| 6.7.3 | 1 | CosInfo | The value of CreationDate entry from the document information dictionary, if present, and its analogous XMP property "xmp:CreateDate" shall be equivalent | The value of CreationDate entry from the document Info dictionary and its matching XMP property "xmp:CreateDate" are not equivalent (Info /Creation... |
| 6.7.3 | 2 | CosInfo | The value of Title entry from the document information dictionary, if present, and its analogous XMP property "dc:title['x-default']" shall be equivalent | The value of Title entry from the document Info dictionary and its matching XMP property "dc:title['x-default']" are not equivalent (Info /Title = ... |
| 6.7.3 | 3 | CosInfo | The value of Author entry from the document information dictionary, if present, and its analogous XMP property "dc:creator" shall be equivalent. dc:creator shall contain exactly one entry | The value of Author entry from the document Info dictionary and its matching XMP property "dc:creator" are not equivalent (Info /Author = %1, XMP d... |
| 6.7.3 | 4 | CosInfo | The value of Subject entry from the document information dictionary, if present, and its analogous XMP property "dc:description['x-default']" shall be equivalent | The value of Subject entry from the document Info dictionary and its matching XMP property "dc:description['x-default']" are not equivalent (Info /... |
| 6.7.3 | 5 | CosInfo | The value of Keywords entry from the document information dictionary, if present, and its analogous XMP property "pdf:Keywords" shall be equivalent | The value of Keywords entry from the document Info dictionary and its matching XMP property "pdf:Keywords" are not equivalent (Info /Keywords = %1,... |
| 6.7.3 | 6 | CosInfo | The value of Creator entry from the document information dictionary, if present, and its analogous XMP property "xmp:CreatorTool" shall be equivalent | The value of Creator entry from the document Info dictionary and its matching XMP property "xmp:CreatorTool" are not equivalent (Info /Creator = %1... |
| 6.7.3 | 7 | CosInfo | The value of Producer entry from the document information dictionary, if present, and its analogous XMP property "pdf:Producer" shall be equivalent | The value of Producer entry from the document Info dictionary and its matching XMP property "pdf:Producer" are not equivalent (Info /Producer = %1,... |
| 6.7.3 | 8 | CosInfo | The value of ModDate entry from the document information dictionary, if present, and its analogous XMP property "xmp:ModifyDate" shall be equivalent | The value of ModDate entry from the document Info dictionary and its matching XMP property "xmp:ModifyDate" are not equivalent (Info /ModDate = %1,... |
| 6.7.5 | 1 | XMPPackage | The bytes attribute shall not be used in the header of an XMP packet | The XMP Package contains bytes attribute |
| 6.7.5 | 2 | XMPPackage | The encoding attribute shall not be used in the header of an XMP packet | The XMP Package contains encoding attribute |
| 6.7.8 | 1 | ExtensionSchemaObject | Extension schema descriptions shall be specified using the PDF/A extension schema description schema defined in this clause | An extension schema object contains field(s) %1 not defined by the specification |
| 6.7.8 | 2 | ExtensionSchemasContainer | The extension schema container schema uses the namespace URI "http://www.aiim.org/pdfa/ns/extension/". The required schema namespace prefix is pdfaExtension. pdfaExtension:schemas - Bag Schema - Description of extension schemas | The extension schema container has invalid type (isValidBag = %1) or doesn't have prefix 'pdfaExtension' (prefix = %2) |
| 6.7.8 | 3 | ExtensionSchemaDefinition | Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'schema' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (schema prefix = %1) or doesn't have type Text (i... |
| 6.7.8 | 4 | ExtensionSchemaDefinition | Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema' | Field 'namespaceURI' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (namespaceURI prefix = %1) or doesn't have ... |
| 6.7.8 | 5 | ExtensionSchemaDefinition | Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'prefix' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (prefix prefix = %1) or doesn't have type Text (i... |
| 6.7.8 | 6 | ExtensionSchemaDefinition | Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema' | Field 'property' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (property prefix = %1) or doesn't have type Seq Property (isProper... |
| 6.7.8 | 7 | ExtensionSchemaDefinition | Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema' | Field 'valueType' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (valueType prefix = %1) or doesn't have type Seq ValueType (isVal... |
| 6.7.8 | 8 | ExtensionSchemaProperty | Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'name' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (name prefix = %1) or doesn't have type Text (i... |
| 6.7.8 | 9 | ExtensionSchemaProperty | Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (valueType prefix = %1) or doesn't have ty... |
| 6.7.8 | 10 | ExtensionSchemaProperty | Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty' | Field 'category' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (category prefix = %1) or doesn't have type... |
| 6.7.8 | 11 | ExtensionSchemaProperty | Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'description' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (description prefix = %1) or doesn't hav... |
| 6.7.8 | 12 | ExtensionSchemaValueType | Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'type' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (type prefix = %1) or doesn't have type Text (isTy... |
| 6.7.8 | 13 | ExtensionSchemaValueType | Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType' | Field 'namespaceURI' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (namespaceURI prefix = %1) or doesn't have... |
| 6.7.8 | 14 | ExtensionSchemaValueType | Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'prefix' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (prefix prefix = %1) or doesn't have type Text (... |
| 6.7.8 | 15 | ExtensionSchemaValueType | Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'description' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (description prefix = %1) or doesn't have t... |
| 6.7.8 | 16 | ExtensionSchemaValueType | Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType' | Field 'field' of the PDF/A ValueType value type doesn't have prefix 'pdfaType' (field prefix = %1) or doesn't have type Seq Field (isFieldValidSeq ... |
| 6.7.8 | 17 | ExtensionSchemaField | Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'name' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (name prefix = %1) or doesn't have type Text (isNameV... |
| 6.7.8 | 18 | ExtensionSchemaField | Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (valueType prefix = %1) or doesn't have type Tex... |
| 6.7.8 | 19 | ExtensionSchemaField | Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'description' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (description prefix = %1) or doesn't have type... |
| 6.7.9 | 1 | XMPPackage | The metadata stream shall conform to XMP Specification and well formed PDFAExtension Schema for all extensions | The serialization of the metadata stream does not conform to XMP Specification |
| 6.7.9 | 2 | XMPProperty | Properties specified in XMP form shall use either the predefined schemas defined in XMP Specification, or extension schemas that comply with XMP Specification | A property is either not defined in XMP Specification, or is not defined in any of the extension schemas |
| 6.7.9 | 3 | XMPProperty | Properties specified in XMP form shall use either the predefined schemas defined in XMP Specification, or extension schemas that comply with XMP Specification | XMP property does not correspond to type %1 |
| 6.7.11 | 1 | MainXMPPackage | The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema | The document metadata stream doesn't contain PDF/A Identification Schema |
| 6.7.11 | 2 | PDFAIdentification | The value of "pdfaid:part" shall be the part number of ISO 19005 to which the file conforms | The "part" property of the PDF/A Identification Schema is %1 instead of 1 for PDF/A-1 conforming file |
| 6.7.11 | 3 | PDFAIdentification | A Level A conforming file shall specify the value of "pdfaid:conformance" as A | The "conformance" property of the PDF/A Identification Schema is %1 instead of "A" for PDF/A-1a conforming file |
| 6.7.11 | 4 | PDFAIdentification | Property "part" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "part" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.7.11 | 5 | PDFAIdentification | Property "conformance" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "conformance" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.7.11 | 6 | PDFAIdentification | Property "amd" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "amd" of the PDF/A Identification Schema has invalid namespace prefix %1 |
### Clause 6.8

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.8.2.2 | 1 | CosDocument | The document catalog dictionary shall include a MarkInfo dictionary with a Marked entry in it, whose value shall be true | MarkInfo dictionary is not present in the document catalog, or Marked entry is set to false or is not present in the MarkInfo dictionary (MarkInfo ... |
| 6.8.3.3 | 1 | PDDocument | The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog dictionary, as described in PDF Reference 9.6 | StructTreeRoot entry is not present in the document catalog |
| 6.8.3.4 | 1 | SENonStandard | All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in PDF Reference 9.7.4, in the role map dictionary of the structure tree root | Non-standard structure type %1 is not mapped to a standard type |
| 6.8.3.4 | 2 | SENonStandard | A circular mapping shall not exist | A circular mapping exists for %1 structure type |
| 6.8.4 | 1 | CosLang | If the Lang entry is present in the document catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as defined by RFC 1766, Tags for the Identification of Languages, as described in PDF R... | Value %1 of the Lang entry is not a Language-Tag |
### Clause 6.9

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.9 | 1 | PDAcroForm | The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false | The interactive form dictionary contains the NeedAppearances flag with value true |
| 6.9 | 2 | PDWidgetAnnot | Every form field shall have an appearance dictionary associated with the field's data | Form field does not have an appearance dictionary associated with the field's data |

## PDF/A-1b

**PDF/A-1B validation profile**
*Validation rules against ISO 19005-1:2005, Cor.1:2007 and Cor.2:2011, Level B*
**Total rules: 129**

### Clause 6.1

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.1.2 | 1 | CosDocument | The % character of the file header shall occur at byte offset 0 of the file. The first line of a PDF file is a header identifying the version of the PDF specification to which the file conforms | File header %1 (offset = %2) starts at non-zero offset or does not match the pattern %PDF-n.m |
| 6.1.2 | 2 | CosDocument | The file header line shall be immediately followed by a comment consisting of a % character followed by at least four characters, each of whose encoded byte values shall have a decimal value greater than 127 | Binary comment in the file header is missing or does not start with 4 bytes with byte values above 127 (first four bytes = %1, %2, %3, %4) |
| 6.1.3 | 1 | CosDocument | The file trailer dictionary shall contain the ID keyword. The file trailer referred to is either the last trailer dictionary in a PDF file, as described in PDF Reference 3.4.4 and 3.4.5, or the first page trailer in a linearized PDF file, as descr... | Missing ID in the document trailer |
| 6.1.3 | 2 | CosTrailer | The keyword Encrypt shall not be used in the trailer dictionary | Encrypt keyword is present in the trailer dictionary |
| 6.1.3 | 3 | CosDocument | No data shall follow the last end-of-file marker except a single optional end-of-line marker | %1 byte(s) of data is present after the last end-of-file marker |
| 6.1.3 | 4 | CosDocument | In a linearized PDF, if the ID keyword is present in both the first page trailer dictionary and the last trailer dictionary, the value to both instances of the ID keyword shall be identical | Last ID (%1) is present in the Linearized PDF and does not match the first page ID (%2) |
| 6.1.4 | 1 | CosXRef | In a cross reference subsection header the starting object number and the range shall be separated by a single SPACE character (20h) | Extra or missing spacings around in a subsection header of the cross reference table |
| 6.1.4 | 2 | CosXRef | The xref keyword and the cross reference subsection header shall be separated by a single EOL marker | Extra spacings or missing EOL characters after the 'xref' keyword in the cross reference table |
| 6.1.4 | 3 | PDDocument | Xref streams shall not be used | The document uses xref streams |
| 6.1.6 | 1 | CosString | Hexadecimal strings shall contain an even number of non-white-space characters | A hexadecimal string contains odd number (%1) of non-white-space characters |
| 6.1.6 | 2 | CosString | All non-white-space characters in hexadecimal strings shall be in the range 0 to 9, A to F or a to f | Hexadecimal string contains non-white-space characters outside the range 0 to 9, A to F or a to f |
| 6.1.7 | 1 | CosStream | The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED character after the stream keyword and preceding the EOL marker before the endstream keyword | Actual length of the stream (%1 byte(s)) does not match the value of the Length key in the Stream dictionary (%2 byte(s)) |
| 6.1.7 | 2 | CosStream | The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED character. The endstream keyword shall be preceded by an EOL marker | Extra spacings or missing EOL characters around keywords 'stream' and 'endstream' |
| 6.1.7 | 3 | CosStream | A stream object dictionary shall not contain the F, FFilter, or FDecodeParms keys | A stream object dictionary contains %1 key(s) |
| 6.1.8 | 1 | CosIndirect | The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded b... | Extra spacings or missing EOL characters around indirect object/generation number or keywords 'obj' and 'endobj' |
| 6.1.10 | 1 | CosFilter | The LZWDecode filter shall not be permitted | Unknown or not permitted Stream filter %1 is used |
| 6.1.10 | 2 | CosIIFilter | The LZWDecode filter shall not be permitted | LZW compression is used in the inline image |
| 6.1.11 | 1 | CosFileSpecification | A file specification dictionary, as defined in PDF 3.10.2, shall not contain the EF key | A file specification dictionary contains the EF key |
| 6.1.11 | 2 | CosDocument | A file's name dictionary, as defined in PDF Reference 3.6.3, shall not contain the EmbeddedFiles key | The document contains embedded files (EmbeddedFiles key is present in the file's name dictionary) |
| 6.1.12 | 1 | CosInteger | Largest Integer value is 2,147,483,647. Smallest integer value is -2,147,483,648 | Integer value %1 out of range |
| 6.1.12 | 2 | CosReal | Absolute real value must be less than or equal to 32767.0 | Real value %1 out of range |
| 6.1.12 | 3 | CosString | Maximum length of a string (in bytes) is 65535 | String length (%1) exceeded 65535 |
| 6.1.12 | 4 | CosName | Maximum length of a name (in bytes) is 127 | Name length (%1) exceeded 127 |
| 6.1.12 | 5 | CosArray | Maximum capacity of an array (in elements) is 8191 | Array capacity (%1) exceeded 8191 |
| 6.1.12 | 6 | CosDict | Maximum capacity of a dictionary (in entries) is 4095 | Dictionary capacity (%1) exceeded 4095 |
| 6.1.12 | 7 | CosDocument | Maximum number of indirect objects in a PDF file is 8,388,607 | Number of indirect objects in a PDF file (%1) exceeded 8,388,607 |
| 6.1.12 | 8 | Op_q_gsave | Maximum depth of graphics state nesting by q and Q operators is 28 | Depth of graphics state nesting of q/Q operators (%1) exceeded 28 |
| 6.1.12 | 9 | PDDeviceN | Maximum number of DeviceN components is 8 | Number of DeviceN components (%1) exceeded 8 |
| 6.1.12 | 10 | CMapFile | Maximum value of a CID (character identifier) is 65,535 | Value of a CID (%1) exceeded 65,535 |
| 6.1.13 | 1 | CosDocument | The document catalog dictionary shall not contain a key with the name OCProperties | The document catalog dictionary contains the OCProperties entry |
### Clause 6.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.2.2 | 1 | ICCOutputProfile | A PDF/A-1 OutputIntent is an OutputIntent dictionary, as defined by PDF Reference 9.10.4, that is included in the file's OutputIntents array and has GTS_PDFA1 as the value of its S key and a valid ICC profile stream as the value its DestOutputProf... | The embedded PDF/A Output Intent colour profile is either invalid or does not provide BToA information |
| 6.2.2 | 2 | OutputIntents | If a file's OutputIntents array contains more than one entry, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream | File's OutputIntents array contains output intent dictionaries with non-matching destination output profiles (indirect keys %1) |
| 6.2.3.2 | 1 | ICCInputProfile | All ICCBased colour spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5 | The embedded ICC profile (Device Class = %1, color space = %2, version = %3) is either invalid or does not satisfy PDF 1.4 requirements |
| 6.2.3.2 | 2 | ICCProfile | All ICCBased colour spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5. The number of color components in the color space described by the ICC profile data must match the number of components actually in the ICC prof... | The N entry (value %1) in the ICC profile dictionary is missing or does not match the number of components in the embedded ICC profile (color space... |
| 6.2.3.3 | 1 | PDDeviceRGB | DeviceRGB may be used only if the file has a PDF/A-1 OutputIntent that uses an RGB colour space | DeviceRGB colour space is used without RGB output intent profile |
| 6.2.3.3 | 2 | PDDeviceCMYK | DeviceCMYK may be used only if the file has a PDF/A-1 OutputIntent that uses a CMYK colour space | DeviceCMYK colour space is used without CMYK output intent profile |
| 6.2.3.3 | 3 | PDDeviceGray | If an uncalibrated colour space is used in a file then that file shall contain a PDF/A-1 OutputIntent, as defined in 6.2.2 | DeviceGray colour space is used without output intent profile |
| 6.2.4 | 1 | PDXImage | An Image dictionary shall not contain the Alternates key | Alternates key is present in the Image dictionary |
| 6.2.4 | 2 | PDXObject | An XObject dictionary (Image or Form) shall not contain the OPI key | OPI key is present in the XObject dictionary |
| 6.2.4 | 3 | PDXImage | If an Image dictionary contains the Interpolate key, its value shall be false | The value of the Interpolate key in the Image dictionary is true |
| 6.2.4 | 4 | PDXImage | If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4 or 8 | The value of the BitsPerComponent key in the Image dictionary is %1 |
| 6.2.4 | 5 | PDMaskImage | If an image mask dictionary contains the BitsPerComponent key, its value shall be 1 | The value of the BitsPerComponent key in the image mask dictionary is %1 |
| 6.2.5 | 1 | PDXForm | A form XObject dictionary shall not contain the Subtype2 key with a value of PS or the PS key | The form XObject dictionary contains a PS key or Subtype2 key with value PS |
| 6.2.6 | 1 | PDXForm | A conforming file shall not contain any reference XObjects | The document contains a reference XObject (Ref key in the form XObject dictionary) |
| 6.2.7 | 1 | PDXObject | A conforming file shall not contain any PostScript XObjects | The document contains a PostScript XObject |
| 6.2.8 | 1 | PDExtGState | An ExtGState dictionary shall not contain the TR key | An ExtGState dictionary contains the TR key |
| 6.2.8 | 2 | PDExtGState | An ExtGState dictionary shall not contain the TR2 key with a value other than Default | An ExtGState dictionary contains the TR2 key with a value other than Default |
| 6.2.9 | 1 | CosRenderingIntent | Where a rendering intent is specified, its value shall be one of the four values defined in PDF Reference RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation | A rendering intent with non-standard value %1 is used |
| 6.2.10 | 1 | Op_Undefined | A content stream shall not contain any operators not defined in PDF Reference even if such operators are bracketed by the BX/EX compatibility operators | A content stream contains operator %1 not defined in PDF Reference |
### Clause 6.3

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.3.2 | 1 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary | A Font dictionary has value %1 of Type entry instead of Font |
| 6.3.2 | 2 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Subtype - name - (Required) The type of font; must be "Type1" for Type 1 fonts, "MMType1" for multiple master fonts, "TrueType" for TrueType... | Invalid Subtype entry in font dictionary with value %1 |
| 6.3.2 | 3 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. BaseFont - name - (Required) The PostScript name of the font | A BaseFont entry is missing or has invalid type |
| 6.3.2 | 4 | PDSimpleFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array | A non-standard simple font dictionary has missing or invalid FirstChar entry |
| 6.3.2 | 5 | PDSimpleFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array | A non-standard simple font dictionary has missing or invalid LastChar entry |
| 6.3.2 | 6 | PDSimpleFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths | Widths array is missing or has invalid size %1 instead of %2 |
| 6.3.2 | 7 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.4 ar... | Unsupported font file format %1 of the embedded font |
| 6.3.3.1 | 1 | PDType0Font | For any given composite (Type 0) font referenced within a conforming file, the CIDSystemInfo entries of its CIDFont and CMap dictionaries shall be compatible. In other words, the Registry and Ordering strings of the CIDSystemInfo dictionaries for ... | Registry and Ordering entries in the CIDFont and CMap dictionaries of a Type 0 font are not compatible (CIDSystemInfo Ordering = %1, CMap Ordering ... |
| 6.3.3.2 | 1 | PDCIDFont | For all Type 2 CIDFonts that are used for rendering, the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in PDF Reference Table 5.13 | A Type 2 CIDFont dictionary has missing or invalid CIDToGIDMap entry |
| 6.3.3.3 | 1 | PDCMap | All CMaps used within a conforming file, except Identity-H and Identity-V, shall be embedded in that file as described in PDF Reference 5.6.4 | A CMap is different from "Identity-H" or "Identity-V" and is not embedded |
| 6.3.3.3 | 2 | CMapFile | For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream | WMode entry (value %1) in the embedded CMap and in the CMap dictionary (value %2) are not identical |
| 6.3.4 | 1 | PDFont | The font programs for all fonts used within a conforming file shall be embedded within that file, as defined in PDF Reference 5.8, except when the fonts are used exclusively with text rendering mode 3 | The font program is not embedded |
| 6.3.5 | 1 | Glyph | Embedded font programs shall define all font glyphs referenced for rendering with conforming file | Not all glyphs referenced for rendering are present in the embedded font program |
| 6.3.5 | 2 | PDType1Font | For all Type 1 font subsets referenced within a conforming file, the font descriptor dictionary shall include a CharSet string listing the character names defined in the font subset, as described in PDF Reference Table 5.18 | A Type1 font subset does not define CharSet entry in its Descriptor dictionary |
| 6.3.5 | 3 | PDCIDFont | For all CIDFont subsets referenced within a conforming file, the font descriptor dictionary shall include a CIDSet stream identifying which CIDs are present in the embedded CIDFont file, as described in PDF Reference Table 5.20 | A CIDSet entry in the Font descriptor is missing or does not correctly identify all glyphs present in the embedded font subset and used for rendering |
| 6.3.6 | 1 | Glyph | For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent | Glyph width %1 in the embedded font program is not consistent with the Widths entry of the font dictionary (value %2) |
| 6.3.7 | 1 | PDTrueTypeFont | All non-symbolic TrueType fonts shall specify MacRomanEncoding or WinAnsiEncoding, either as the value of the Encoding entry in the font dictionary or as the value of the BaseEncoding entry in the dictionary that is the value of the Encoding entry... | A non-symbolic TrueType font encoding does not define a correct mapping to the Adobe Glyph List (Encoding = %1, Encoding entry contains a Differenc... |
| 6.3.7 | 2 | PDTrueTypeFont | All symbolic TrueType fonts shall not specify an Encoding entry in the font dictionary | A symbolic TrueType font specifies an Encoding entry in its dictionary |
| 6.3.7 | 3 | TrueTypeFontProgram | Font programs' "cmap" tables for all symbolic TrueType fonts shall contain exactly one encoding | The embedded font program for a symbolic TrueType font contains %1 cmap subtables |
### Clause 6.4

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.4 | 1 | PDExtGState | If an SMask key appears in an ExtGState dictionary, its value shall be None | An ExtGState contains SMask key with a value other than None |
| 6.4 | 2 | PDXObject | An XObject dictionary shall not contain the SMask key | An XObject contains an SMask key |
| 6.4 | 3 | PDGroup | A Group object with an S key with a value of Transparency shall not be included in a form XObject. A Group object with an S key with a value of Transparency shall not be included in a page dictionary | A transparency group is present in a form XObject or page dictionary |
| 6.4 | 4 | PDExtGState | If a BM key is present in an ExtGState object, its value shall be Normal or Compatible | An ExtGState dictionary contains the BM key (blend mode) with value %1 that is neither Normal or Compatible |
| 6.4 | 5 | PDExtGState | If a CA key is present in an ExtGState object, its value shall be 1.0 | An ExtGState dictionary contains the CA key (stroke alpha) with value %1 other than 1.0 |
| 6.4 | 6 | PDExtGState | If a ca key is present in an ExtGState object, its value shall be 1.0 | An ExtGState dictionary contains the ca key (fill alpha) with value %1 other than 1.0 |
### Clause 6.5

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.5.2 | 1 | PDAnnot | Annotation types not defined in PDF Reference shall not be permitted. Additionally, the FileAttachment, Sound and Movie types shall not be permitted | Unknown or not permitted Annotation type %1 |
| 6.5.3 | 1 | PDAnnot | An annotation dictionary shall not contain the CA key with a value other than 1.0 | An annotation dictionary contains the CA key with value %1 instead of 1.0 |
| 6.5.3 | 2 | PDAnnot | An annotation dictionary shall contain the F key. The F key’s Print flag bit shall be set to 1 and its Hidden, Invisible and NoView flag bits shall be set to 0 | Annotation flags are either missing or set the annotation to be hidden/invisible or non-printable (F = %1, Print = %2, Hidden = %3, Invisible = %4,... |
| 6.5.3 | 3 | PDAnnot | An annotation dictionary shall not contain the C array or the IC array unless the colour space of the DestOutputProfile in the PDF/A-1 OutputIntent dictionary, defined in 6.2.2, is RGB | Annotation's color (C present = %1) or interior color (IC present = %2) is used without specifying RGB-based destination output profile |
| 6.5.3 | 4 | PDAnnot | For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key | Annotation's appearance dictionary contains entries %1 other than N |
| 6.5.3 | 5 | PDAnnot | If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary | An annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, but the value of the N key is %1 instead of appeara... |
| 6.5.3 | 6 | PDAnnot | If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream | An annotation dictionary's Subtype key has a value %1 and its FT key has a value %2, but the value of the N key is not an appearance stream |
### Clause 6.6

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.6.1 | 1 | PDAction | The Launch, Sound, Movie, ResetForm, ImportData and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted. The Hide action shall not be permitted (Corrigendum 2) | Unknown or not permitted Action type %1 |
| 6.6.1 | 2 | PDNamedAction | Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted | Unknown or not permitted named action %1 |
| 6.6.1 | 3 | PDWidgetAnnot | Interactive form fields shall not perform actions of any type | Interactive form field contains an action object ('A' key) |
| 6.6.2 | 1 | PDWidgetAnnot | A Widget annotation dictionary shall not include an AA entry for an additional-actions dictionary | A Widget annotation contains an additional-actions dictionary (AA entry) |
| 6.6.2 | 2 | PDFormField | A Field dictionary shall not include an AA entry for an additional-actions dictionary | A Field dictionary contains an additional-actions dictionary (AA entry) |
| 6.6.2 | 3 | PDDocument | The document catalog dictionary shall not include an AA entry for an additional-actions dictionary | The document catalog dictionary contains an additional-actions dictionary (AA entry) |
### Clause 6.7

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.7.2 | 1 | PDDocument | The document catalog dictionary of a conforming file shall contain the Metadata key. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML | The document catalog dictionary doesn't contain metadata key or metadata stream dictionary does not contain either entry Type with value /Metadata ... |
| 6.7.2 | 2 | PDMetadata | The Metadata object stream dictionary in the document’s catalog shall not contain the Filter key | The catalog metadata object stream dictionary contains the Filter key |
| 6.7.3 | 1 | CosInfo | The value of CreationDate entry from the document information dictionary, if present, and its analogous XMP property "xmp:CreateDate" shall be equivalent | The value of CreationDate entry from the document Info dictionary and its matching XMP property "xmp:CreateDate" are not equivalent (Info /Creation... |
| 6.7.3 | 2 | CosInfo | The value of Title entry from the document information dictionary, if present, and its analogous XMP property "dc:title['x-default']" shall be equivalent | The value of Title entry from the document Info dictionary and its matching XMP property "dc:title['x-default']" are not equivalent (Info /Title = ... |
| 6.7.3 | 3 | CosInfo | The value of Author entry from the document information dictionary, if present, and its analogous XMP property "dc:creator" shall be equivalent. dc:creator shall contain exactly one entry | The value of Author entry from the document Info dictionary and its matching XMP property "dc:creator" are not equivalent (Info /Author = %1, XMP d... |
| 6.7.3 | 4 | CosInfo | The value of Subject entry from the document information dictionary, if present, and its analogous XMP property "dc:description['x-default']" shall be equivalent | The value of Subject entry from the document Info dictionary and its matching XMP property "dc:description['x-default']" are not equivalent (Info /... |
| 6.7.3 | 5 | CosInfo | The value of Keywords entry from the document information dictionary, if present, and its analogous XMP property "pdf:Keywords" shall be equivalent | The value of Keywords entry from the document Info dictionary and its matching XMP property "pdf:Keywords" are not equivalent (Info /Keywords = %1,... |
| 6.7.3 | 6 | CosInfo | The value of Creator entry from the document information dictionary, if present, and its analogous XMP property "xmp:CreatorTool" shall be equivalent | The value of Creator entry from the document Info dictionary and its matching XMP property "xmp:CreatorTool" are not equivalent (Info /Creator = %1... |
| 6.7.3 | 7 | CosInfo | The value of Producer entry from the document information dictionary, if present, and its analogous XMP property "pdf:Producer" shall be equivalent | The value of Producer entry from the document Info dictionary and its matching XMP property "pdf:Producer" are not equivalent (Info /Producer = %1,... |
| 6.7.3 | 8 | CosInfo | The value of ModDate entry from the document information dictionary, if present, and its analogous XMP property "xmp:ModifyDate" shall be equivalent | The value of ModDate entry from the document Info dictionary and its matching XMP property "xmp:ModifyDate" are not equivalent (Info /ModDate = %1,... |
| 6.7.5 | 1 | XMPPackage | The bytes attribute shall not be used in the header of an XMP packet | The XMP Package contains bytes attribute |
| 6.7.5 | 2 | XMPPackage | The encoding attribute shall not be used in the header of an XMP packet | The XMP Package contains encoding attribute |
| 6.7.8 | 1 | ExtensionSchemaObject | Extension schema descriptions shall be specified using the PDF/A extension schema description schema defined in this clause | An extension schema object contains field(s) %1 not defined by the specification |
| 6.7.8 | 2 | ExtensionSchemasContainer | The extension schema container schema uses the namespace URI "http://www.aiim.org/pdfa/ns/extension/". The required schema namespace prefix is pdfaExtension. pdfaExtension:schemas - Bag Schema - Description of extension schemas | The extension schema container has invalid type (isValidBag = %1) or doesn't have prefix 'pdfaExtension' (prefix = %2) |
| 6.7.8 | 3 | ExtensionSchemaDefinition | Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'schema' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (schema prefix = %1) or doesn't have type Text (i... |
| 6.7.8 | 4 | ExtensionSchemaDefinition | Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema' | Field 'namespaceURI' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (namespaceURI prefix = %1) or doesn't have ... |
| 6.7.8 | 5 | ExtensionSchemaDefinition | Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'prefix' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (prefix prefix = %1) or doesn't have type Text (i... |
| 6.7.8 | 6 | ExtensionSchemaDefinition | Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema' | Field 'property' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (property prefix = %1) or doesn't have type Seq Property (isProper... |
| 6.7.8 | 7 | ExtensionSchemaDefinition | Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema' | Field 'valueType' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (valueType prefix = %1) or doesn't have type Seq ValueType (isVal... |
| 6.7.8 | 8 | ExtensionSchemaProperty | Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'name' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (name prefix = %1) or doesn't have type Text (i... |
| 6.7.8 | 9 | ExtensionSchemaProperty | Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (valueType prefix = %1) or doesn't have ty... |
| 6.7.8 | 10 | ExtensionSchemaProperty | Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty' | Field 'category' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (category prefix = %1) or doesn't have type... |
| 6.7.8 | 11 | ExtensionSchemaProperty | Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'description' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (description prefix = %1) or doesn't hav... |
| 6.7.8 | 12 | ExtensionSchemaValueType | Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'type' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (type prefix = %1) or doesn't have type Text (isTy... |
| 6.7.8 | 13 | ExtensionSchemaValueType | Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType' | Field 'namespaceURI' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (namespaceURI prefix = %1) or doesn't have... |
| 6.7.8 | 14 | ExtensionSchemaValueType | Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'prefix' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (prefix prefix = %1) or doesn't have type Text (... |
| 6.7.8 | 15 | ExtensionSchemaValueType | Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'description' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (description prefix = %1) or doesn't have t... |
| 6.7.8 | 16 | ExtensionSchemaValueType | Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType' | Field 'field' of the PDF/A ValueType value type doesn't have prefix 'pdfaType' (field prefix = %1) or doesn't have type Seq Field (isFieldValidSeq ... |
| 6.7.8 | 17 | ExtensionSchemaField | Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'name' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (name prefix = %1) or doesn't have type Text (isNameV... |
| 6.7.8 | 18 | ExtensionSchemaField | Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (valueType prefix = %1) or doesn't have type Tex... |
| 6.7.8 | 19 | ExtensionSchemaField | Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'description' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (description prefix = %1) or doesn't have type... |
| 6.7.9 | 1 | XMPPackage | The metadata stream shall conform to XMP Specification and well formed PDFAExtension Schema for all extensions | The serialization of the metadata stream does not conform to XMP Specification |
| 6.7.9 | 2 | XMPProperty | Properties specified in XMP form shall use either the predefined schemas defined in XMP Specification, or extension schemas that comply with XMP Specification | A property is either not defined in XMP Specification, or is not defined in any of the extension schemas |
| 6.7.9 | 3 | XMPProperty | Properties specified in XMP form shall use either the predefined schemas defined in XMP Specification, or extension schemas that comply with XMP Specification | XMP property does not correspond to type %1 |
| 6.7.11 | 1 | MainXMPPackage | The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema | The document metadata stream doesn't contain PDF/A Identification Schema |
| 6.7.11 | 2 | PDFAIdentification | The value of "pdfaid:part" shall be the part number of ISO 19005 to which the file conforms | The "part" property of the PDF/A Identification Schema is %1 instead of 1 for PDF/A-1 conforming file |
| 6.7.11 | 3 | PDFAIdentification | A Level A conforming file shall specify the value of "pdfaid:conformance" as A. A Level B conforming file shall specify the value of "pdfaid:conformance" as B | The "conformance" property of the PDF/A Identification Schema is %1 instead of "B" for PDF/A-1b conforming file |
| 6.7.11 | 4 | PDFAIdentification | Property "part" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "part" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.7.11 | 5 | PDFAIdentification | Property "conformance" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "conformance" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.7.11 | 6 | PDFAIdentification | Property "amd" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "amd" of the PDF/A Identification Schema has invalid namespace prefix %1 |
### Clause 6.9

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.9 | 1 | PDAcroForm | The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false | The interactive form dictionary contains the NeedAppearances flag with value true |
| 6.9 | 2 | PDWidgetAnnot | Every form field shall have an appearance dictionary associated with the field's data | Form field does not have an appearance dictionary associated with the field's data |

## PDF/A-2a

**PDF/A-2A validation profile**
*Validation rules against ISO 19005-2:2011, Level A*
**Total rules: 153**

### Clause 6.1

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.1.2 | 1 | CosDocument | The file header shall begin at byte zero and shall consist of "%PDF-1.n" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h) | File header %1 (offset = %2) starts at non-zero offset or does not match the pattern %PDF-1.n, where 'n' is a single digit number between 0 and 7 |
| 6.1.2 | 2 | CosDocument | The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127 | Binary comment in the file header is missing or does not start with 4 bytes with byte values above 127 (first four bytes = %1, %2, %3, %4) |
| 6.1.3 | 1 | CosDocument | The file trailer dictionary shall contain the ID keyword whose value shall be File Identifiers as defined in ISO 32000-1:2008, 14.4 | Missing or empty ID in the document trailer |
| 6.1.3 | 2 | CosTrailer | The keyword Encrypt shall not be used in the trailer dictionary | Encrypt keyword is present in the trailer dictionary |
| 6.1.3 | 3 | CosDocument | No data can follow the last end-of-file marker except a single optional end-of-line marker as described in ISO 32000-1:2008, 7.5.5 | %1 byte(s) of data is present after the last end-of-file marker |
| 6.1.4 | 2 | CosXRef | The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker | Extra spacings or missing EOL characters after the 'xref' keyword in the cross reference table |
| 6.1.6 | 1 | CosString | Hexadecimal strings shall contain an even number of non-white-space characters | A hexadecimal string contains odd number (%1) of non-white-space characters |
| 6.1.6 | 2 | CosString | A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f) | Hexadecimal string contains non-white-space characters outside the range 0 to 9, A to F or a to f |
| 6.1.7.1 | 1 | CosStream | The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword | Actual length of the stream (%1 byte(s)) does not match the value of the Length key in the Stream dictionary (%2 byte(s)) |
| 6.1.7.1 | 2 | CosStream | The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED (0Ah) character. The endstream keyword shall be preceded by an EOL marker | Extra spacings or missing EOL characters around keywords 'stream' and 'endstream' |
| 6.1.7.1 | 3 | CosStream | A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys | A stream object dictionary contains %1 key(s) |
| 6.1.7.2 | 1 | CosFilter | All standard stream filters listed in ISO 32000-1:2008, 7.4, Table 6 may be used, with the exception of LZWDecode. In addition, the Crypt filter shall not be used unless the value of the Name key in the decode parameters dictionary is Identity. Fi... | Unknown or not permitted Stream filter %1 is used |
| 6.1.8 | 1 | CosUnicodeName | Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences | The name value %1 does not represent a correct UTF-8 character sequence |
| 6.1.9 | 1 | CosIndirect | The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded b... | Extra spacings or missing EOL characters around indirect object/generation number or keywords 'obj' and 'endobj' |
| 6.1.10 | 1 | CosIIFilter | The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-1:2008, Table 6, or an array containing any such value | Inline image uses not permitted or unknown filter %1 |
| 6.1.12 | 1 | PDPerms | No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-1:2008, 12.8.4, Table 258) | The document permissions dictionary contains key(s) %1 other than UR3 and DocMDP |
| 6.1.12 | 2 | PDSigRef | If DocMDP is present, then the Signature References dictionary (ISO 32000-1:2008, 12.8.1, Table 253) shall not contain the keys DigestLocation, DigestMethod, and DigestValue | The Signature References dictionary contains %1 key(s) in presence of DocMDP entry in the permissions dictionary |
| 6.1.13 | 1 | CosInteger | A conforming file shall not contain any integer greater than 2147483647. A conforming file shall not contain any integer less than -2147483648 | Integer value %1 out of range |
| 6.1.13 | 2 | CosReal | A conforming file shall not contain any real number outside the range of +/-3.403 x 10^38 | Real value %1 out of range |
| 6.1.13 | 3 | CosString | A conforming file shall not contain any string longer than 32767 bytes | String length (%1) exceeded 32767 |
| 6.1.13 | 4 | CosName | A conforming file shall not contain any name longer than 127 bytes | Name length (%1) exceeded 127 |
| 6.1.13 | 5 | CosReal | A conforming file shall not contain any real number closer to zero than +/-1.175 x 10^(-38) | Non-zero real value %1 is too close to 0.0 |
| 6.1.13 | 7 | CosDocument | A conforming file shall not contain more than 8388607 indirect objects | Number of indirect objects in a PDF file (%1) exceeded 8,388,607 |
| 6.1.13 | 8 | Op_q_gsave | A conforming file shall not nest q/Q pairs by more than 28 nesting levels | Depth of graphics state nesting of q/Q operators (%1) exceeded 28 |
| 6.1.13 | 9 | PDDeviceN | A conforming file shall not contain a DeviceN colour space with more than 32 colourants | Number of DeviceN components (%1) exceeded 32 |
| 6.1.13 | 10 | CMapFile | A conforming file shall not contain a CID value greater than 65535 | Value of a CID (%1) exceeded 65,535 |
| 6.1.13 | 11 | CosBBox | The size of any of the page boundaries described in ISO 32000-1:2008, 14.11.2 shall not be less than 3 units in either direction, nor shall it be greater than 14 400 units in either direction | One of the page boundaries is out of range (valid range: 3 - 14400, height = %1, width = %2) |
### Clause 6.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.2.2 | 1 | Op_Undefined | Content streams shall not contain any operators not defined in ISO 32000-1 even if such operators are bracketed by the BX/EX compatibility operators | A content stream contains operator %1 not defined in ISO 32000-1 |
| 6.2.2 | 2 | PDContentStream | A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-1:2008, 7.8.3 | A content stream refers to resource(s) %1 not defined in an explicitly associated Resources dictionary |
| 6.2.3 | 1 | ICCOutputProfile | The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = "prtr") or a monitor profile (Device Class = "mntr"). The profiles shall have a colour space of either "GRAY", "RGB", or "CMYK" | The embedded PDF/A Output Intent colour profile has invalid header (Device Class = %1, color space = %2, version = %3) |
| 6.2.3 | 2 | OutputIntents | If a file's OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X-4 or PDF/E-1, then all entries that contain a DestOutputProfile key shall have... | File's OutputIntents array contains output intent dictionaries with non-matching destination output profiles (indirect keys %1) |
| 6.2.3 | 3 | PDOutputIntent | In addition, the DestOutputProfileRef key, as defined in ISO 15930-7:2010, Annex A, shall not be present in any PDF/X OutputIntent | The PDF/X output intent dictionary contains forbidden entry DestOutputProfileRef |
| 6.2.4.2 | 1 | ICCInputProfile | The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1 | The embedded ICC profile (Device Class = %1, color space = %2, version = %3) is either invalid or does not satisfy PDF 1.7 requirements |
| 6.2.4.2 | 2 | PDICCBasedCMYK | Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and over... | Overprint mode (OPM) is set to %1 instead of 0 when an ICCBased CMYK colour space is used with enabled overprinting |
| 6.2.4.3 | 2 | PDDeviceRGB | DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used, or if the file has a PDF/A OutputIntent that contains an RGB destination profile | DeviceRGB colour space is used without RGB output intent profile |
| 6.2.4.3 | 3 | PDDeviceCMYK | DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set or if a DeviceN-based DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or the file has a PDF/A OutputIntent that contains a C... | DeviceCMYK colour space is used without CMYK output intent profile |
| 6.2.4.3 | 4 | PDDeviceGray | DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is present | DeviceGray colour space is used without output intent profile |
| 6.2.4.4 | 1 | PDDeviceN | For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present | A colorant of the DeviceN or NChannel color space is not defined in the Colorants dictionary |
| 6.2.4.4 | 2 | PDSeparation | All Separation arrays within a single PDF/A-2 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, rather than ... | Several occurrences of a Separation colour space with the same name are not consistent |
| 6.2.5 | 1 | PDExtGState | An ExtGState dictionary shall not contain the TR key | An ExtGState dictionary contains the TR key |
| 6.2.5 | 2 | PDExtGState | An ExtGState dictionary shall not contain the TR2 key with a value other than Default | An ExtGState dictionary contains the TR2 key with a value other than Default |
| 6.2.5 | 3 | PDExtGState | An ExtGState dictionary shall not contain the HTP key | An ExtGState dictionary contains the HTP key |
| 6.2.5 | 4 | PDHalftone | All halftones in a conforming PDF/A-2 file shall have the value 1 or 5 for the HalftoneType key | A Halftone has type %1 instead of 1 or 5 |
| 6.2.5 | 5 | PDHalftone | Halftones in a conforming PDF/A-2 file shall not contain a HalftoneName key | A Halftone dictionary contains the HalftoneName key |
| 6.2.5 | 6 | PDHalftone | The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-1 | Custom TransferFunction in a Halftone dictionary is not permitted for primary (CMYK) colorants |
| 6.2.6 | 1 | CosRenderingIntent | Where a rendering intent is specified, its value shall be one of the four values defined in ISO 32000-1:2008, Table 70: RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation | A rendering intent with non-standard value %1 is used |
| 6.2.8 | 1 | PDXImage | An Image dictionary shall not contain the Alternates key | Alternates key is present in the Image dictionary |
| 6.2.8 | 2 | PDXImage | An Image dictionary shall not contain the OPI key | OPI key is present in the Image dictionary |
| 6.2.8 | 3 | PDXImage | If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key shall have a value of false | The value of the Interpolate key in the Image dictionary is true |
| 6.2.8 | 4 | PDXImage | If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16 | The value of the BitsPerComponent key in the Image dictionary is %1 |
| 6.2.8 | 5 | PDMaskImage | If an image mask dictionary contains the BitsPerComponent key, its value shall be 1 | The value of the BitsPerComponent key in the image mask dictionary is %1 |
| 6.2.8.3 | 1 | JPEG2000 | The number of colour channels in the JPEG2000 data shall be 1, 3 or 4 | JPEG2000 image has %1 colour channels that is neither 1, 3 or 4 |
| 6.2.8.3 | 2 | JPEG2000 | If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field | The JPEG2000 image contains %1 colour specifications with the best colour fidelity (value 0x01 in the APPROX field) |
| 6.2.8.3 | 3 | JPEG2000 | The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming reader shall use only that colour space and shall ignore all other colour space specifications | Invalid JPEG2000 image: the value of the METH entry (%1) in its 'colr' box is different from 0x01, 0x02 or 0x03 |
| 6.2.8.3 | 4 | JPEG2000 | JPEG2000 enumerated colour space 19 (CIEJab) shall not be used | JPEG2000 image uses enumerated colour space 19 (CIEJab), which is not allowed in PDF/A |
| 6.2.8.3 | 5 | JPEG2000 | The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth | JPEG2000 image has different bit-depth parameters in 'bpcc' box, or bit depth is out of range ('bpcc' box present = %1, bit depth = %2) |
| 6.2.9 | 1 | PDXForm | A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key | The form XObject dictionary contains a PS key, or a Subtype2 key with value PS, or an OPI key |
| 6.2.9 | 2 | PDXForm | A conforming file shall not contain any reference XObjects | The document contains a reference XObject (Ref key in the form XObject dictionary) |
| 6.2.9 | 3 | PDXObject | A conforming file shall not contain any PostScript XObjects | The document contains a PostScript XObject |
| 6.2.10 | 1 | CosBM | Only blend modes that are specified in ISO 32000-1:2008 shall be used for the value of the BM key in an extended graphic state dictionary | The document uses the blend mode %1 not defined in ISO 32000-1:2008 |
| 6.2.10 | 2 | PDPage | If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be u... | The page contains transparent objects with no blending colour space defined |
| 6.2.11.2 | 1 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Requi... | A Font dictionary has value %1 of Type entry instead of Font |
| 6.2.11.2 | 2 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Re... | Invalid Subtype entry in font dictionary with value %1 |
| 6.2.11.2 | 3 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (R... | A BaseFont entry is missing or has invalid type |
| 6.2.11.2 | 4 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer ... | A non-standard simple font dictionary has missing or invalid FirstChar entry |
| 6.2.11.2 | 5 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer -... | A non-standard simple font dictionary has missing or invalid LastChar entry |
| 6.2.11.2 | 6 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Re... | Widths array is missing or has invalid size %1 instead of %2 |
| 6.2.11.2 | 7 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.7 ar... | Unsupported font file format %1 of the embedded font |
| 6.2.11.3.1 | 1 | PDType0Font | For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary is Identity-H ... | CIDSystemInfo entries the CIDFont and CMap dictionaries of a Type 0 font are not compatible (CIDSystemInfo Ordering = %1, CMap Ordering = %2, CIDSy... |
| 6.2.11.3.2 | 1 | PDCIDFont | ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2... | A Type 2 CIDFont dictionary has missing or invalid CIDToGIDMap entry |
| 6.2.11.3.3 | 1 | PDCMap | All CMaps used within a PDF/A-2 file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5 | A non-standard CMap %1 is not embedded |
| 6.2.11.3.3 | 2 | CMapFile | For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream | WMode entry (value %1) in the embedded CMap and in the CMap dictionary (value %2) are not identical |
| 6.2.11.3.3 | 3 | PDReferencedCMap | A CMap shall not reference any other CMap except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118 | A CMap references another non-standard CMap %1 |
| 6.2.11.4.1 | 1 | PDFont | The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9 | The font program is not embedded |
| 6.2.11.4.1 | 2 | Glyph | Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType... | Not all glyphs referenced for rendering are present in the embedded font program |
| 6.2.11.4.2 | 1 | PDType1Font | If the FontDescriptor dictionary of an embedded Type 1 font contains a CharSet string, then it shall list the character names of all glyphs present in the font program, regardless of whether a glyph in the font is referenced or used by the PDF or not | A CharSet entry in the Descriptor dictionary of a Type1 font incorrectly lists glyphs present in the font program |
| 6.2.11.4.2 | 2 | PDCIDFont | If the FontDescriptor dictionary of an embedded CID font contains a CIDSet stream, then it shall identify all CIDs which are present in the font program, regardless of whether a CID in the font is referenced or used by the PDF or not | A CIDSet entry in the Font descriptor does not correctly identify all glyphs present in the embedded font subset |
| 6.2.11.5 | 1 | Glyph | For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent | Glyph width %1 in the embedded font program is not consistent with the Widths entry of the font dictionary (value %2) |
| 6.2.11.6 | 1 | TrueTypeFontProgram | For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain one or several non-symbolic cmap entries such that all necessary glyph lookups can be carried out | The embedded font program for a non-symbolic TrueType font does not contain non-symbolic cmap entries |
| 6.2.11.6 | 2 | PDTrueTypeFont | All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary that is the value of the Encoding key in the ... | A non-symbolic TrueType font encoding does not define a correct mapping to the Adobe Glyph List (Encoding = %1, Encoding entry contains a Differenc... |
| 6.2.11.6 | 3 | PDTrueTypeFont | Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary | A symbolic TrueType font specifies an Encoding entry in its dictionary |
| 6.2.11.6 | 4 | TrueTypeFontProgram | The 'cmap' table in the embedded font program for a symbolic TrueType font shall contain either exactly one encoding or it shall contain, at least, the Microsoft Symbol (3,0 - Platform ID=3, Encoding ID=0) encoding | The embedded font program for a symbolic TrueType font contains %1 cmap subtables and does not contain Microsoft Symbol (3,0 – Platform ID=3, Encod... |
| 6.2.11.7.2 | 1 | Glyph | The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 19005-2, 6.2.11.7.2 | The glyph can not be mapped to Unicode |
| 6.2.11.7.2 | 2 | Glyph | The Unicode values specified in the ToUnicode CMap shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE | The glyph has Unicode value 0, U+FEFF or U+FFFE, which is invalid by Unicode standard |
| 6.2.11.7.3 | 1 | Glyph | For any character, regardless of its rendering mode, that is mapped to a code or codes in the Unicode Private Use Area (PUA), an ActualText entry as described in ISO 32000-1:2008, 14.9.4 shall be present for this character or a sequence of charact... | The character has Unicode value from Private Use Area, and no replacement text present |
| 6.2.11.8 | 1 | Glyph | A PDF/A-2 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream | The document contains a reference to the .notdef glyph |
### Clause 6.3

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.3.1 | 1 | PDAnnot | Annotation types not defined in ISO 32000-1 shall not be permitted. Additionally, the 3D, Sound, Screen and Movie types shall not be permitted | Unknown or not permitted Annotation type %1 |
| 6.3.2 | 1 | PDAnnot | Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key | A dictionary of %1 annotation does not contain F key |
| 6.3.2 | 2 | PDAnnot | If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0 | Annotation flags are set the annotation to be hidden/invisible or non-printable (F = %1, Print = %2, Hidden = %3, Invisible = %4, NoView = %5, Togg... |
| 6.3.3 | 1 | PDAnnot | Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: - annotations where the value of the Rect key consists of an array whe... | An annotation does not contain an appearance dictionary |
| 6.3.3 | 2 | PDAnnot | For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key | Annotation's appearance dictionary contains entries %1 other than N |
| 6.3.3 | 3 | PDAnnot | If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary | An annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, but the value of the N key is %1 instead of appeara... |
| 6.3.3 | 4 | PDAnnot | If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream | An annotation dictionary's Subtype key has a value %1 and its FT key has a value %2, but the value of the N key is not an appearance stream |
### Clause 6.4

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.4.1 | 1 | PDWidgetAnnot | A Widget annotation dictionary shall not contain the A or AA keys | A Widget annotation contains %1 key(s) |
| 6.4.1 | 2 | PDFormField | A Field dictionary shall not contain the A or AA keys | A Form field dictionary contains the AA entry |
| 6.4.1 | 3 | PDAcroForm | The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false | The interactive form dictionary contains the NeedAppearances flag with value true |
| 6.4.2 | 1 | PDAcroForm | The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-2 file, if present, shall not contain the XFA key | The interactive form dictionary contains the XFA key |
| 6.4.2 | 2 | CosDocument | A document's Catalog shall not contain the NeedsRendering key | A document's Catalog contains NeedsRendering flag set to true |
| 6.4.3 | 1 | PDSignature | When computing the digest for the file, it shall be computed over the entire file, including the signature dictionary but excluding the PDF Signature itself | ByteRange array of the digital signature does not cover the entire file (excluding the PDF Signature itself) |
| 6.4.3 | 2 | PKCSDataObject | The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, it shall include the signer's X.5... | The DER-encoded PKCS#7 binary data object representing a PDF Signature does not include the signer's X.509 signing certificate |
| 6.4.3 | 3 | PKCSDataObject | The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, there shall only be a single sign... | The DER-encoded PKCS#7 binary data object representing a PDF Signature has %1 signer(s) in the PDF Signature instead of one |
### Clause 6.5

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.5.1 | 1 | PDAction | The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted | Unknown or not permitted Action type %1 |
| 6.5.1 | 2 | PDNamedAction | Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted | Unknown or not permitted named action %1 |
| 6.5.2 | 1 | PDDocument | The document's Catalog shall not include an AA entry for an additional-actions dictionary | The document catalog dictionary contains an additional-actions dictionary (AA entry) |
| 6.5.2 | 2 | PDPage | The Page dictionary shall not include an AA entry for an additional-actions dictionary | The Page dictionary contains an additional-actions dictionary (AA entry) |
### Clause 6.6

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.6.2.1 | 1 | PDDocument | The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-1:2008, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with... | The document catalog dictionary doesn't contain metadata key or metadata stream dictionary does not contain either entry Type with value /Metadata ... |
| 6.6.2.1 | 2 | XMPPackage | The bytes attribute shall not be used in the header of an XMP packet | The XMP Package contains bytes attribute |
| 6.6.2.1 | 3 | XMPPackage | The encoding attribute shall not be used in the header of an XMP packet | The XMP Package contains encoding attribute |
| 6.6.2.1 | 4 | XMPPackage | All metadata streams present in the PDF shall conform to the XMP Specification. All content of all XMP packets shall be well-formed, as defined by Extensible Markup Language (XML) 1.0 (Third Edition), 2.1, and the RDF/XML Syntax Specification (Rev... | A metadata stream is serialized incorrectly and can not be parsed |
| 6.6.2.1 | 5 | XMPPackage | All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8 | The XMP package uses encoding %1 different from UTF-8 |
| 6.6.2.3.1 | 1 | XMPProperty | All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2 | XMP property is either not predefined, or is not defined in any XMP extension schema |
| 6.6.2.3.1 | 2 | XMPProperty | All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2 | XMP property does not correspond to type %1 |
| 6.6.2.3.2 | 1 | ExtensionSchemaObject | Extension schemas shall be specified using the PDF/A extension schema container schema defined in 6.6.2.3.3. All fields described in each of the tables in 6.6.2.3.3 shall be present in any extension schema container schema | An extension schema object contains field(s) %1 not defined by the specification |
| 6.6.2.3.3 | 1 | ExtensionSchemasContainer | The extension schema container schema uses the namespace URI "http://www.aiim.org/pdfa/ns/extension/". The required schema namespace prefix is pdfaExtension. pdfaExtension:schemas - Bag Schema - Description of extension schemas | The extension schema container has invalid type (isValidBag = %1) or doesn't have prefix 'pdfaExtension' (prefix = %2) |
| 6.6.2.3.3 | 2 | ExtensionSchemaDefinition | Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'schema' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (schema prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 3 | ExtensionSchemaDefinition | Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema' | Field 'namespaceURI' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (namespaceURI prefix = %1) or doesn't have ... |
| 6.6.2.3.3 | 4 | ExtensionSchemaDefinition | Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'prefix' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (prefix prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 5 | ExtensionSchemaDefinition | Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema' | Field 'property' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (property prefix = %1) or doesn't have type Seq Property (isProper... |
| 6.6.2.3.3 | 6 | ExtensionSchemaDefinition | Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema' | Field 'valueType' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (valueType prefix = %1) or doesn't have type Seq ValueType (isVal... |
| 6.6.2.3.3 | 7 | ExtensionSchemaProperty | Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'name' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (name prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 8 | ExtensionSchemaProperty | Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (valueType prefix = %1) or doesn't have ty... |
| 6.6.2.3.3 | 9 | ExtensionSchemaProperty | Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty' | Field 'category' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (category prefix = %1) or doesn't have type... |
| 6.6.2.3.3 | 10 | ExtensionSchemaProperty | Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'description' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (description prefix = %1) or doesn't hav... |
| 6.6.2.3.3 | 11 | ExtensionSchemaValueType | Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'type' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (type prefix = %1) or doesn't have type Text (isTy... |
| 6.6.2.3.3 | 12 | ExtensionSchemaValueType | Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType' | Field 'namespaceURI' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (namespaceURI prefix = %1) or doesn't have... |
| 6.6.2.3.3 | 13 | ExtensionSchemaValueType | Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'prefix' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (prefix prefix = %1) or doesn't have type Text (... |
| 6.6.2.3.3 | 14 | ExtensionSchemaValueType | Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'description' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (description prefix = %1) or doesn't have t... |
| 6.6.2.3.3 | 15 | ExtensionSchemaValueType | Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType' | Field 'field' of the PDF/A ValueType value type doesn't have prefix 'pdfaType' (field prefix = %1) or doesn't have type Seq Field (isFieldValidSeq ... |
| 6.6.2.3.3 | 16 | ExtensionSchemaField | Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'name' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (name prefix = %1) or doesn't have type Text (isNameV... |
| 6.6.2.3.3 | 17 | ExtensionSchemaField | Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (valueType prefix = %1) or doesn't have type Tex... |
| 6.6.2.3.3 | 18 | ExtensionSchemaField | Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'description' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (description prefix = %1) or doesn't have type... |
| 6.6.4 | 1 | MainXMPPackage | The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema | The document metadata stream doesn't contain PDF/A Identification Schema |
| 6.6.4 | 2 | PDFAIdentification | The value of "pdfaid:part" shall be the part number of ISO 19005 to which the file conforms | The "part" property of the PDF/A Identification Schema is %1 instead of 2 for PDF/A-2 conforming file |
| 6.6.4 | 3 | PDFAIdentification | A Level A conforming file shall specify the value of "pdfaid:conformance" as A. A Level B conforming file shall specify the value of "pdfaid:conformance" as B. A Level U conforming file shall specify the value of "pdfaid:conformance" as U | The "conformance" property of the PDF/A Identification Schema is %1 instead of "A" for PDF/A-2A conforming file |
| 6.6.4 | 4 | PDFAIdentification | Property "part" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "part" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 5 | PDFAIdentification | Property "conformance" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "conformance" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 6 | PDFAIdentification | Property "amd" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "amd" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 7 | PDFAIdentification | Property "corr" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "corr" of the PDF/A Identification Schema has invalid namespace prefix %1 |
### Clause 6.7

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.7.2.2 | 1 | CosDocument | The document catalog dictionary shall include a MarkInfo dictionary containing an entry, Marked, whose value shall be true | MarkInfo dictionary is not present in the document catalog, or Marked entry is set to false or is not present in the MarkInfo dictionary (MarkInfo ... |
| 6.7.3.3 | 1 | PDDocument | The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document's Catalog dictionary, as described in ISO 32000-1:2008, 14.7 | StructTreeRoot entry is not present in the document catalog |
| 6.7.3.4 | 1 | SENonStandard | All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in ISO 32000-1:2008, 14.8.4, in the role map dictionary of the structure tree root | Non-standard structure type %1 is not mapped to a standard type |
| 6.7.3.4 | 2 | SENonStandard | A circular mapping shall not exist | A circular mapping exists for %1 structure type |
| 6.7.3.4 | 3 | SENonStandard | Standard tags shall not be remapped to a non-standard type | The standard structure type %1 is remapped to a non-standard type |
| 6.7.4 | 1 | CosLang | If the Lang entry is present in the document's Catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as described in ISO 32000-1:2008, 14.9.2. A language identifier shall either be the e... | Value %1 of the Lang entry is not a Language-Tag |
### Clause 6.8

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.8 | 2 | CosFileSpecification | The file specification dictionary for an embedded file shall contain the F and UF keys | The file specification dictionary for an embedded file does not contain either F or UF key (F = %1, UF = %2) |
| 6.8 | 5 | EmbeddedFile | A file specification dictionary, as defined in ISO 32000-1:2008, 7.11.3, may contain the EF key, provided that the embedded file is compliant with either ISO 19005-1 or this part of ISO 19005 | An embedded file does not comply to either ISO 19005-1 or ISO 19005-2 |
### Clause 6.9

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.9 | 1 | PDOCConfig | Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key | Missing or empty Name entry of the optional content configuration dictionary |
| 6.9 | 2 | PDOCConfig | Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-2 file | Optional content configuration dictionary has duplicated name %1 |
| 6.9 | 3 | PDOCConfig | If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file | Optional content group(s) %1 not present in the Order entry of the optional content configuration dictionary |
| 6.9 | 4 | PDOCConfig | The AS key shall not appear in any optional content configuration dictionary | AS key is present in the optional content configuration dictionary |
### Clause 6.10

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.10 | 1 | PDDocument | There shall be no AlternatePresentations entry in the document's name dictionary | The document's name dictionary contains the AlternatePresentations entry |
| 6.10 | 2 | PDPage | There shall be no PresSteps entry in any Page dictionary | A Page dictionary contains the PresSteps entry |
### Clause 6.11

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.11 | 1 | CosDocument | The document catalog shall not contain the Requirements key | The document catalog contains the Requirements key |

## PDF/A-2b

**PDF/A-2B validation profile**
*Validation rules against ISO 19005-2:2011, Level B*
**Total rules: 144**

### Clause 6.1

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.1.2 | 1 | CosDocument | The file header shall begin at byte zero and shall consist of "%PDF-1.n" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h) | File header %1 (offset = %2) starts at non-zero offset or does not match the pattern %PDF-1.n, where 'n' is a single digit number between 0 and 7 |
| 6.1.2 | 2 | CosDocument | The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127 | Binary comment in the file header is missing or does not start with 4 bytes with byte values above 127 (first four bytes = %1, %2, %3, %4) |
| 6.1.3 | 1 | CosDocument | The file trailer dictionary shall contain the ID keyword whose value shall be File Identifiers as defined in ISO 32000-1:2008, 14.4 | Missing or empty ID in the document trailer |
| 6.1.3 | 2 | CosTrailer | The keyword Encrypt shall not be used in the trailer dictionary | Encrypt keyword is present in the trailer dictionary |
| 6.1.3 | 3 | CosDocument | No data can follow the last end-of-file marker except a single optional end-of-line marker as described in ISO 32000-1:2008, 7.5.5 | %1 byte(s) of data is present after the last end-of-file marker |
| 6.1.4 | 2 | CosXRef | The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker | Extra spacings or missing EOL characters after the 'xref' keyword in the cross reference table |
| 6.1.6 | 1 | CosString | Hexadecimal strings shall contain an even number of non-white-space characters | A hexadecimal string contains odd number (%1) of non-white-space characters |
| 6.1.6 | 2 | CosString | A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f) | Hexadecimal string contains non-white-space characters outside the range 0 to 9, A to F or a to f |
| 6.1.7.1 | 1 | CosStream | The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword | Actual length of the stream (%1 byte(s)) does not match the value of the Length key in the Stream dictionary (%2 byte(s)) |
| 6.1.7.1 | 2 | CosStream | The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED (0Ah) character. The endstream keyword shall be preceded by an EOL marker | Extra spacings or missing EOL characters around keywords 'stream' and 'endstream' |
| 6.1.7.1 | 3 | CosStream | A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys | A stream object dictionary contains %1 key(s) |
| 6.1.7.2 | 1 | CosFilter | All standard stream filters listed in ISO 32000-1:2008, 7.4, Table 6 may be used, with the exception of LZWDecode. In addition, the Crypt filter shall not be used unless the value of the Name key in the decode parameters dictionary is Identity. Fi... | Unknown or not permitted Stream filter %1 is used |
| 6.1.8 | 1 | CosUnicodeName | Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences | The name value %1 does not represent a correct UTF-8 character sequence |
| 6.1.9 | 1 | CosIndirect | The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded b... | Extra spacings or missing EOL characters around indirect object/generation number or keywords 'obj' and 'endobj' |
| 6.1.10 | 1 | CosIIFilter | The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-1:2008, Table 6, or an array containing any such value | Inline image uses not permitted or unknown filter %1 |
| 6.1.12 | 1 | PDPerms | No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-1:2008, 12.8.4, Table 258) | The document permissions dictionary contains key(s) %1 other than UR3 and DocMDP |
| 6.1.12 | 2 | PDSigRef | If DocMDP is present, then the Signature References dictionary (ISO 32000-1:2008, 12.8.1, Table 253) shall not contain the keys DigestLocation, DigestMethod, and DigestValue | The Signature References dictionary contains %1 key(s) in presence of DocMDP entry in the permissions dictionary |
| 6.1.13 | 1 | CosInteger | A conforming file shall not contain any integer greater than 2147483647. A conforming file shall not contain any integer less than -2147483648 | Integer value %1 out of range |
| 6.1.13 | 2 | CosReal | A conforming file shall not contain any real number outside the range of +/-3.403 x 10^38 | Real value %1 out of range |
| 6.1.13 | 3 | CosString | A conforming file shall not contain any string longer than 32767 bytes | String length (%1) exceeded 32767 |
| 6.1.13 | 4 | CosName | A conforming file shall not contain any name longer than 127 bytes | Name length (%1) exceeded 127 |
| 6.1.13 | 5 | CosReal | A conforming file shall not contain any real number closer to zero than +/-1.175 x 10^(-38) | Non-zero real value %1 is too close to 0.0 |
| 6.1.13 | 7 | CosDocument | A conforming file shall not contain more than 8388607 indirect objects | Number of indirect objects in a PDF file (%1) exceeded 8,388,607 |
| 6.1.13 | 8 | Op_q_gsave | A conforming file shall not nest q/Q pairs by more than 28 nesting levels | Depth of graphics state nesting of q/Q operators (%1) exceeded 28 |
| 6.1.13 | 9 | PDDeviceN | A conforming file shall not contain a DeviceN colour space with more than 32 colourants | Number of DeviceN components (%1) exceeded 32 |
| 6.1.13 | 10 | CMapFile | A conforming file shall not contain a CID value greater than 65535 | Value of a CID (%1) exceeded 65,535 |
| 6.1.13 | 11 | CosBBox | The size of any of the page boundaries described in ISO 32000-1:2008, 14.11.2 shall not be less than 3 units in either direction, nor shall it be greater than 14 400 units in either direction | One of the page boundaries is out of range (valid range: 3 - 14400, height = %1, width = %2) |
### Clause 6.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.2.2 | 1 | Op_Undefined | Content streams shall not contain any operators not defined in ISO 32000-1 even if such operators are bracketed by the BX/EX compatibility operators | A content stream contains operator %1 not defined in ISO 32000-1 |
| 6.2.2 | 2 | PDContentStream | A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-1:2008, 7.8.3 | A content stream refers to resource(s) %1 not defined in an explicitly associated Resources dictionary |
| 6.2.3 | 1 | ICCOutputProfile | The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = "prtr") or a monitor profile (Device Class = "mntr"). The profiles shall have a colour space of either "GRAY", "RGB", or "CMYK" | The embedded PDF/A Output Intent colour profile has invalid header (Device Class = %1, color space = %2, version = %3) |
| 6.2.3 | 2 | OutputIntents | If a file's OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X-4 or PDF/E-1, then all entries that contain a DestOutputProfile key shall have... | File's OutputIntents array contains output intent dictionaries with non-matching destination output profiles (indirect keys %1) |
| 6.2.3 | 3 | PDOutputIntent | In addition, the DestOutputProfileRef key, as defined in ISO 15930-7:2010, Annex A, shall not be present in any PDF/X OutputIntent | The PDF/X output intent dictionary contains forbidden entry DestOutputProfileRef |
| 6.2.4.2 | 1 | ICCInputProfile | The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1 | The embedded ICC profile (Device Class = %1, color space = %2, version = %3) is either invalid or does not satisfy PDF 1.7 requirements |
| 6.2.4.2 | 2 | PDICCBasedCMYK | Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and over... | Overprint mode (OPM) is set to %1 instead of 0 when an ICCBased CMYK colour space is used with enabled overprinting |
| 6.2.4.3 | 2 | PDDeviceRGB | DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used, or if the file has a PDF/A OutputIntent that contains an RGB destination profile | DeviceRGB colour space is used without RGB output intent profile |
| 6.2.4.3 | 3 | PDDeviceCMYK | DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set or if a DeviceN-based DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or the file has a PDF/A OutputIntent that contains a C... | DeviceCMYK colour space is used without CMYK output intent profile |
| 6.2.4.3 | 4 | PDDeviceGray | DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is present | DeviceGray colour space is used without output intent profile |
| 6.2.4.4 | 1 | PDDeviceN | For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present | A colorant of the DeviceN or NChannel color space is not defined in the Colorants dictionary |
| 6.2.4.4 | 2 | PDSeparation | All Separation arrays within a single PDF/A-2 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, rather than ... | Several occurrences of a Separation colour space with the same name are not consistent |
| 6.2.5 | 1 | PDExtGState | An ExtGState dictionary shall not contain the TR key | An ExtGState dictionary contains the TR key |
| 6.2.5 | 2 | PDExtGState | An ExtGState dictionary shall not contain the TR2 key with a value other than Default | An ExtGState dictionary contains the TR2 key with a value other than Default |
| 6.2.5 | 3 | PDExtGState | An ExtGState dictionary shall not contain the HTP key | An ExtGState dictionary contains the HTP key |
| 6.2.5 | 4 | PDHalftone | All halftones in a conforming PDF/A-2 file shall have the value 1 or 5 for the HalftoneType key | A Halftone has type %1 instead of 1 or 5 |
| 6.2.5 | 5 | PDHalftone | Halftones in a conforming PDF/A-2 file shall not contain a HalftoneName key | A Halftone dictionary contains the HalftoneName key |
| 6.2.5 | 6 | PDHalftone | The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-1 | Custom TransferFunction in a Halftone dictionary is not permitted for primary (CMYK) colorants |
| 6.2.6 | 1 | CosRenderingIntent | Where a rendering intent is specified, its value shall be one of the four values defined in ISO 32000-1:2008, Table 70: RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation | A rendering intent with non-standard value %1 is used |
| 6.2.8 | 1 | PDXImage | An Image dictionary shall not contain the Alternates key | Alternates key is present in the Image dictionary |
| 6.2.8 | 2 | PDXImage | An Image dictionary shall not contain the OPI key | OPI key is present in the Image dictionary |
| 6.2.8 | 3 | PDXImage | If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key shall have a value of false | The value of the Interpolate key in the Image dictionary is true |
| 6.2.8 | 4 | PDXImage | If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16 | The value of the BitsPerComponent key in the Image dictionary is %1 |
| 6.2.8 | 5 | PDMaskImage | If an image mask dictionary contains the BitsPerComponent key, its value shall be 1 | The value of the BitsPerComponent key in the image mask dictionary is %1 |
| 6.2.8.3 | 1 | JPEG2000 | The number of colour channels in the JPEG2000 data shall be 1, 3 or 4 | JPEG2000 image has %1 colour channels that is neither 1, 3 or 4 |
| 6.2.8.3 | 2 | JPEG2000 | If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field | The JPEG2000 image contains %1 colour specifications with the best colour fidelity (value 0x01 in the APPROX field) |
| 6.2.8.3 | 3 | JPEG2000 | The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming reader shall use only that colour space and shall ignore all other colour space specifications | Invalid JPEG2000 image: the value of the METH entry (%1) in its 'colr' box is different from 0x01, 0x02 or 0x03 |
| 6.2.8.3 | 4 | JPEG2000 | JPEG2000 enumerated colour space 19 (CIEJab) shall not be used | JPEG2000 image uses enumerated colour space 19 (CIEJab), which is not allowed in PDF/A |
| 6.2.8.3 | 5 | JPEG2000 | The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth | JPEG2000 image has different bit-depth parameters in 'bpcc' box, or bit depth is out of range ('bpcc' box present = %1, bit depth = %2) |
| 6.2.9 | 1 | PDXForm | A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key | The form XObject dictionary contains a PS key, or a Subtype2 key with value PS, or an OPI key |
| 6.2.9 | 2 | PDXForm | A conforming file shall not contain any reference XObjects | The document contains a reference XObject (Ref key in the form XObject dictionary) |
| 6.2.9 | 3 | PDXObject | A conforming file shall not contain any PostScript XObjects | The document contains a PostScript XObject |
| 6.2.10 | 1 | CosBM | Only blend modes that are specified in ISO 32000-1:2008 shall be used for the value of the BM key in an extended graphic state dictionary | The document uses the blend mode %1 not defined in ISO 32000-1:2008 |
| 6.2.10 | 2 | PDPage | If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be u... | The page contains transparent objects with no blending colour space defined |
| 6.2.11.2 | 1 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Requi... | A Font dictionary has value %1 of Type entry instead of Font |
| 6.2.11.2 | 2 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Re... | Invalid Subtype entry in font dictionary with value %1 |
| 6.2.11.2 | 3 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (R... | A BaseFont entry is missing or has invalid type |
| 6.2.11.2 | 4 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer ... | A non-standard simple font dictionary has missing or invalid FirstChar entry |
| 6.2.11.2 | 5 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer -... | A non-standard simple font dictionary has missing or invalid LastChar entry |
| 6.2.11.2 | 6 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Re... | Widths array is missing or has invalid size %1 instead of %2 |
| 6.2.11.2 | 7 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.7 ar... | Unsupported font file format %1 of the embedded font |
| 6.2.11.3.1 | 1 | PDType0Font | For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary is Identity-H ... | CIDSystemInfo entries the CIDFont and CMap dictionaries of a Type 0 font are not compatible (CIDSystemInfo Ordering = %1, CMap Ordering = %2, CIDSy... |
| 6.2.11.3.2 | 1 | PDCIDFont | ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2... | A Type 2 CIDFont dictionary has missing or invalid CIDToGIDMap entry |
| 6.2.11.3.3 | 1 | PDCMap | All CMaps used within a PDF/A-2 file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5 | A non-standard CMap %1 is not embedded |
| 6.2.11.3.3 | 2 | CMapFile | For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream | WMode entry (value %1) in the embedded CMap and in the CMap dictionary (value %2) are not identical |
| 6.2.11.3.3 | 3 | PDReferencedCMap | A CMap shall not reference any other CMap except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118 | A CMap references another non-standard CMap %1 |
| 6.2.11.4.1 | 1 | PDFont | The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9 | The font program is not embedded |
| 6.2.11.4.1 | 2 | Glyph | Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType... | Not all glyphs referenced for rendering are present in the embedded font program |
| 6.2.11.4.2 | 1 | PDType1Font | If the FontDescriptor dictionary of an embedded Type 1 font contains a CharSet string, then it shall list the character names of all glyphs present in the font program, regardless of whether a glyph in the font is referenced or used by the PDF or not | A CharSet entry in the Descriptor dictionary of a Type1 font incorrectly lists glyphs present in the font program |
| 6.2.11.4.2 | 2 | PDCIDFont | If the FontDescriptor dictionary of an embedded CID font contains a CIDSet stream, then it shall identify all CIDs which are present in the font program, regardless of whether a CID in the font is referenced or used by the PDF or not | A CIDSet entry in the Font descriptor does not correctly identify all glyphs present in the embedded font subset |
| 6.2.11.5 | 1 | Glyph | For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent | Glyph width %1 in the embedded font program is not consistent with the Widths entry of the font dictionary (value %2) |
| 6.2.11.6 | 1 | TrueTypeFontProgram | For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain one or several non-symbolic cmap entries such that all necessary glyph lookups can be carried out | The embedded font program for a non-symbolic TrueType font does not contain non-symbolic cmap entries |
| 6.2.11.6 | 2 | PDTrueTypeFont | All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary that is the value of the Encoding key in the ... | A non-symbolic TrueType font encoding does not define a correct mapping to the Adobe Glyph List (Encoding = %1, Encoding entry contains a Differenc... |
| 6.2.11.6 | 3 | PDTrueTypeFont | Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary | A symbolic TrueType font specifies an Encoding entry in its dictionary |
| 6.2.11.6 | 4 | TrueTypeFontProgram | The 'cmap' table in the embedded font program for a symbolic TrueType font shall contain either exactly one encoding or it shall contain, at least, the Microsoft Symbol (3,0 - Platform ID=3, Encoding ID=0) encoding | The embedded font program for a symbolic TrueType font contains %1 cmap subtables and does not contain Microsoft Symbol (3,0 – Platform ID=3, Encod... |
| 6.2.11.8 | 1 | Glyph | A PDF/A-2 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream | The document contains a reference to the .notdef glyph |
### Clause 6.3

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.3.1 | 1 | PDAnnot | Annotation types not defined in ISO 32000-1 shall not be permitted. Additionally, the 3D, Sound, Screen and Movie types shall not be permitted | Unknown or not permitted Annotation type %1 |
| 6.3.2 | 1 | PDAnnot | Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key | A dictionary of %1 annotation does not contain F key |
| 6.3.2 | 2 | PDAnnot | If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0 | Annotation flags are set the annotation to be hidden/invisible or non-printable (F = %1, Print = %2, Hidden = %3, Invisible = %4, NoView = %5, Togg... |
| 6.3.3 | 1 | PDAnnot | Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: - annotations where the value of the Rect key consists of an array whe... | An annotation does not contain an appearance dictionary |
| 6.3.3 | 2 | PDAnnot | For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key | Annotation's appearance dictionary contains entries %1 other than N |
| 6.3.3 | 3 | PDAnnot | If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary | An annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, but the value of the N key is %1 instead of appeara... |
| 6.3.3 | 4 | PDAnnot | If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream | An annotation dictionary's Subtype key has a value %1 and its FT key has a value %2, but the value of the N key is not an appearance stream |
### Clause 6.4

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.4.1 | 1 | PDWidgetAnnot | A Widget annotation dictionary shall not contain the A or AA keys | A Widget annotation contains %1 key(s) |
| 6.4.1 | 2 | PDFormField | A Field dictionary shall not contain the A or AA keys | A Form field dictionary contains the AA entry |
| 6.4.1 | 3 | PDAcroForm | The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false | The interactive form dictionary contains the NeedAppearances flag with value true |
| 6.4.2 | 1 | PDAcroForm | The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-2 file, if present, shall not contain the XFA key | The interactive form dictionary contains the XFA key |
| 6.4.2 | 2 | CosDocument | A document's Catalog shall not contain the NeedsRendering key | A document's Catalog contains NeedsRendering flag set to true |
| 6.4.3 | 1 | PDSignature | When computing the digest for the file, it shall be computed over the entire file, including the signature dictionary but excluding the PDF Signature itself | ByteRange array of the digital signature does not cover the entire file (excluding the PDF Signature itself) |
| 6.4.3 | 2 | PKCSDataObject | The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, it shall include the signer's X.5... | The DER-encoded PKCS#7 binary data object representing a PDF Signature does not include the signer's X.509 signing certificate |
| 6.4.3 | 3 | PKCSDataObject | The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, there shall only be a single sign... | The DER-encoded PKCS#7 binary data object representing a PDF Signature has %1 signer(s) in the PDF Signature instead of one |
### Clause 6.5

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.5.1 | 1 | PDAction | The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted | Unknown or not permitted Action type %1 |
| 6.5.1 | 2 | PDNamedAction | Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted | Unknown or not permitted named action %1 |
| 6.5.2 | 1 | PDDocument | The document's Catalog shall not include an AA entry for an additional-actions dictionary | The document catalog dictionary contains an additional-actions dictionary (AA entry) |
| 6.5.2 | 2 | PDPage | The Page dictionary shall not include an AA entry for an additional-actions dictionary | The Page dictionary contains an additional-actions dictionary (AA entry) |
### Clause 6.6

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.6.2.1 | 1 | PDDocument | The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-1:2008, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with... | The document catalog dictionary doesn't contain metadata key or metadata stream dictionary does not contain either entry Type with value /Metadata ... |
| 6.6.2.1 | 2 | XMPPackage | The bytes attribute shall not be used in the header of an XMP packet | The XMP Package contains bytes attribute |
| 6.6.2.1 | 3 | XMPPackage | The encoding attribute shall not be used in the header of an XMP packet | The XMP Package contains encoding attribute |
| 6.6.2.1 | 4 | XMPPackage | All metadata streams present in the PDF shall conform to the XMP Specification. All content of all XMP packets shall be well-formed, as defined by Extensible Markup Language (XML) 1.0 (Third Edition), 2.1, and the RDF/XML Syntax Specification (Rev... | A metadata stream is serialized incorrectly and can not be parsed |
| 6.6.2.1 | 5 | XMPPackage | All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8 | The XMP package uses encoding %1 different from UTF-8 |
| 6.6.2.3.1 | 1 | XMPProperty | All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2 | XMP property is either not predefined, or is not defined in any XMP extension schema |
| 6.6.2.3.1 | 2 | XMPProperty | All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2 | XMP property does not correspond to type %1 |
| 6.6.2.3.2 | 1 | ExtensionSchemaObject | Extension schemas shall be specified using the PDF/A extension schema container schema defined in 6.6.2.3.3. All fields described in each of the tables in 6.6.2.3.3 shall be present in any extension schema container schema | An extension schema object contains field(s) %1 not defined by the specification |
| 6.6.2.3.3 | 1 | ExtensionSchemasContainer | The extension schema container schema uses the namespace URI "http://www.aiim.org/pdfa/ns/extension/". The required schema namespace prefix is pdfaExtension. pdfaExtension:schemas - Bag Schema - Description of extension schemas | The extension schema container has invalid type (isValidBag = %1) or doesn't have prefix 'pdfaExtension' (prefix = %2) |
| 6.6.2.3.3 | 2 | ExtensionSchemaDefinition | Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'schema' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (schema prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 3 | ExtensionSchemaDefinition | Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema' | Field 'namespaceURI' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (namespaceURI prefix = %1) or doesn't have ... |
| 6.6.2.3.3 | 4 | ExtensionSchemaDefinition | Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'prefix' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (prefix prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 5 | ExtensionSchemaDefinition | Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema' | Field 'property' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (property prefix = %1) or doesn't have type Seq Property (isProper... |
| 6.6.2.3.3 | 6 | ExtensionSchemaDefinition | Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema' | Field 'valueType' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (valueType prefix = %1) or doesn't have type Seq ValueType (isVal... |
| 6.6.2.3.3 | 7 | ExtensionSchemaProperty | Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'name' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (name prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 8 | ExtensionSchemaProperty | Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (valueType prefix = %1) or doesn't have ty... |
| 6.6.2.3.3 | 9 | ExtensionSchemaProperty | Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty' | Field 'category' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (category prefix = %1) or doesn't have type... |
| 6.6.2.3.3 | 10 | ExtensionSchemaProperty | Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'description' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (description prefix = %1) or doesn't hav... |
| 6.6.2.3.3 | 11 | ExtensionSchemaValueType | Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'type' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (type prefix = %1) or doesn't have type Text (isTy... |
| 6.6.2.3.3 | 12 | ExtensionSchemaValueType | Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType' | Field 'namespaceURI' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (namespaceURI prefix = %1) or doesn't have... |
| 6.6.2.3.3 | 13 | ExtensionSchemaValueType | Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'prefix' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (prefix prefix = %1) or doesn't have type Text (... |
| 6.6.2.3.3 | 14 | ExtensionSchemaValueType | Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'description' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (description prefix = %1) or doesn't have t... |
| 6.6.2.3.3 | 15 | ExtensionSchemaValueType | Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType' | Field 'field' of the PDF/A ValueType value type doesn't have prefix 'pdfaType' (field prefix = %1) or doesn't have type Seq Field (isFieldValidSeq ... |
| 6.6.2.3.3 | 16 | ExtensionSchemaField | Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'name' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (name prefix = %1) or doesn't have type Text (isNameV... |
| 6.6.2.3.3 | 17 | ExtensionSchemaField | Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (valueType prefix = %1) or doesn't have type Tex... |
| 6.6.2.3.3 | 18 | ExtensionSchemaField | Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'description' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (description prefix = %1) or doesn't have type... |
| 6.6.4 | 1 | MainXMPPackage | The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema | The document metadata stream doesn't contain PDF/A Identification Schema |
| 6.6.4 | 2 | PDFAIdentification | The value of "pdfaid:part" shall be the part number of ISO 19005 to which the file conforms | The "part" property of the PDF/A Identification Schema is %1 instead of 2 for PDF/A-2 conforming file |
| 6.6.4 | 3 | PDFAIdentification | A Level A conforming file shall specify the value of "pdfaid:conformance" as A. A Level B conforming file shall specify the value of "pdfaid:conformance" as B. A Level U conforming file shall specify the value of "pdfaid:conformance" as U | The "conformance" property of the PDF/A Identification Schema is %1 instead of "B" for PDF/A-2B conforming file |
| 6.6.4 | 4 | PDFAIdentification | Property "part" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "part" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 5 | PDFAIdentification | Property "conformance" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "conformance" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 6 | PDFAIdentification | Property "amd" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "amd" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 7 | PDFAIdentification | Property "corr" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "corr" of the PDF/A Identification Schema has invalid namespace prefix %1 |
### Clause 6.8

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.8 | 2 | CosFileSpecification | The file specification dictionary for an embedded file shall contain the F and UF keys | The file specification dictionary for an embedded file does not contain either F or UF key (F = %1, UF = %2) |
| 6.8 | 5 | EmbeddedFile | A file specification dictionary, as defined in ISO 32000-1:2008, 7.11.3, may contain the EF key, provided that the embedded file is compliant with either ISO 19005-1 or this part of ISO 19005 | An embedded file does not comply to either ISO 19005-1 or ISO 19005-2 |
### Clause 6.9

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.9 | 1 | PDOCConfig | Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key | Missing or empty Name entry of the optional content configuration dictionary |
| 6.9 | 2 | PDOCConfig | Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-2 file | Optional content configuration dictionary has duplicated name %1 |
| 6.9 | 3 | PDOCConfig | If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file | Optional content group(s) %1 not present in the Order entry of the optional content configuration dictionary |
| 6.9 | 4 | PDOCConfig | The AS key shall not appear in any optional content configuration dictionary | AS key is present in the optional content configuration dictionary |
### Clause 6.10

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.10 | 1 | PDDocument | There shall be no AlternatePresentations entry in the document's name dictionary | The document's name dictionary contains the AlternatePresentations entry |
| 6.10 | 2 | PDPage | There shall be no PresSteps entry in any Page dictionary | A Page dictionary contains the PresSteps entry |
### Clause 6.11

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.11 | 1 | CosDocument | The document catalog shall not contain the Requirements key | The document catalog contains the Requirements key |

## PDF/A-2u

**PDF/A-2U validation profile**
*Validation rules against ISO 19005-2:2011, Level U*
**Total rules: 146**

### Clause 6.1

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.1.2 | 1 | CosDocument | The file header shall begin at byte zero and shall consist of "%PDF-1.n" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h) | File header %1 (offset = %2) starts at non-zero offset or does not match the pattern %PDF-1.n, where 'n' is a single digit number between 0 and 7 |
| 6.1.2 | 2 | CosDocument | The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127 | Binary comment in the file header is missing or does not start with 4 bytes with byte values above 127 (first four bytes = %1, %2, %3, %4) |
| 6.1.3 | 1 | CosDocument | The file trailer dictionary shall contain the ID keyword whose value shall be File Identifiers as defined in ISO 32000-1:2008, 14.4 | Missing or empty ID in the document trailer |
| 6.1.3 | 2 | CosTrailer | The keyword Encrypt shall not be used in the trailer dictionary | Encrypt keyword is present in the trailer dictionary |
| 6.1.3 | 3 | CosDocument | No data can follow the last end-of-file marker except a single optional end-of-line marker as described in ISO 32000-1:2008, 7.5.5 | %1 byte(s) of data is present after the last end-of-file marker |
| 6.1.4 | 2 | CosXRef | The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker | Extra spacings or missing EOL characters after the 'xref' keyword in the cross reference table |
| 6.1.6 | 1 | CosString | Hexadecimal strings shall contain an even number of non-white-space characters | A hexadecimal string contains odd number (%1) of non-white-space characters |
| 6.1.6 | 2 | CosString | A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f) | Hexadecimal string contains non-white-space characters outside the range 0 to 9, A to F or a to f |
| 6.1.7.1 | 1 | CosStream | The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword | Actual length of the stream (%1 byte(s)) does not match the value of the Length key in the Stream dictionary (%2 byte(s)) |
| 6.1.7.1 | 2 | CosStream | The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED (0Ah) character. The endstream keyword shall be preceded by an EOL marker | Extra spacings or missing EOL characters around keywords 'stream' and 'endstream' |
| 6.1.7.1 | 3 | CosStream | A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys | A stream object dictionary contains %1 key(s) |
| 6.1.7.2 | 1 | CosFilter | All standard stream filters listed in ISO 32000-1:2008, 7.4, Table 6 may be used, with the exception of LZWDecode. In addition, the Crypt filter shall not be used unless the value of the Name key in the decode parameters dictionary is Identity. Fi... | Unknown or not permitted Stream filter %1 is used |
| 6.1.8 | 1 | CosUnicodeName | Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences | The name value %1 does not represent a correct UTF-8 character sequence |
| 6.1.9 | 1 | CosIndirect | The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded b... | Extra spacings or missing EOL characters around indirect object/generation number or keywords 'obj' and 'endobj' |
| 6.1.10 | 1 | CosIIFilter | The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-1:2008, Table 6, or an array containing any such value | Inline image uses not permitted or unknown filter %1 |
| 6.1.12 | 1 | PDPerms | No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-1:2008, 12.8.4, Table 258) | The document permissions dictionary contains key(s) %1 other than UR3 and DocMDP |
| 6.1.12 | 2 | PDSigRef | If DocMDP is present, then the Signature References dictionary (ISO 32000-1:2008, 12.8.1, Table 253) shall not contain the keys DigestLocation, DigestMethod, and DigestValue | The Signature References dictionary contains %1 key(s) in presence of DocMDP entry in the permissions dictionary |
| 6.1.13 | 1 | CosInteger | A conforming file shall not contain any integer greater than 2147483647. A conforming file shall not contain any integer less than -2147483648 | Integer value %1 out of range |
| 6.1.13 | 2 | CosReal | A conforming file shall not contain any real number outside the range of +/-3.403 x 10^38 | Real value %1 out of range |
| 6.1.13 | 3 | CosString | A conforming file shall not contain any string longer than 32767 bytes | String length (%1) exceeded 32767 |
| 6.1.13 | 4 | CosName | A conforming file shall not contain any name longer than 127 bytes | Name length (%1) exceeded 127 |
| 6.1.13 | 5 | CosReal | A conforming file shall not contain any real number closer to zero than +/-1.175 x 10^(-38) | Non-zero real value %1 is too close to 0.0 |
| 6.1.13 | 7 | CosDocument | A conforming file shall not contain more than 8388607 indirect objects | Number of indirect objects in a PDF file (%1) exceeded 8,388,607 |
| 6.1.13 | 8 | Op_q_gsave | A conforming file shall not nest q/Q pairs by more than 28 nesting levels | Depth of graphics state nesting of q/Q operators (%1) exceeded 28 |
| 6.1.13 | 9 | PDDeviceN | A conforming file shall not contain a DeviceN colour space with more than 32 colourants | Number of DeviceN components (%1) exceeded 32 |
| 6.1.13 | 10 | CMapFile | A conforming file shall not contain a CID value greater than 65535 | Value of a CID (%1) exceeded 65,535 |
| 6.1.13 | 11 | CosBBox | The size of any of the page boundaries described in ISO 32000-1:2008, 14.11.2 shall not be less than 3 units in either direction, nor shall it be greater than 14 400 units in either direction | One of the page boundaries is out of range (valid range: 3 - 14400, height = %1, width = %2) |
### Clause 6.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.2.2 | 1 | Op_Undefined | Content streams shall not contain any operators not defined in ISO 32000-1 even if such operators are bracketed by the BX/EX compatibility operators | A content stream contains operator %1 not defined in ISO 32000-1 |
| 6.2.2 | 2 | PDContentStream | A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-1:2008, 7.8.3 | A content stream refers to resource(s) %1 not defined in an explicitly associated Resources dictionary |
| 6.2.3 | 1 | ICCOutputProfile | The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = "prtr") or a monitor profile (Device Class = "mntr"). The profiles shall have a colour space of either "GRAY", "RGB", or "CMYK" | The embedded PDF/A Output Intent colour profile has invalid header (Device Class = %1, color space = %2, version = %3) |
| 6.2.3 | 2 | OutputIntents | If a file's OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X-4 or PDF/E-1, then all entries that contain a DestOutputProfile key shall have... | File's OutputIntents array contains output intent dictionaries with non-matching destination output profiles (indirect keys %1) |
| 6.2.3 | 3 | PDOutputIntent | In addition, the DestOutputProfileRef key, as defined in ISO 15930-7:2010, Annex A, shall not be present in any PDF/X OutputIntent | The PDF/X output intent dictionary contains forbidden entry DestOutputProfileRef |
| 6.2.4.2 | 1 | ICCInputProfile | The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1 | The embedded ICC profile (Device Class = %1, color space = %2, version = %3) is either invalid or does not satisfy PDF 1.7 requirements |
| 6.2.4.2 | 2 | PDICCBasedCMYK | Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and over... | Overprint mode (OPM) is set to %1 instead of 0 when an ICCBased CMYK colour space is used with enabled overprinting |
| 6.2.4.3 | 2 | PDDeviceRGB | DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used, or if the file has a PDF/A OutputIntent that contains an RGB destination profile | DeviceRGB colour space is used without RGB output intent profile |
| 6.2.4.3 | 3 | PDDeviceCMYK | DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set or if a DeviceN-based DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or the file has a PDF/A OutputIntent that contains a C... | DeviceCMYK colour space is used without CMYK output intent profile |
| 6.2.4.3 | 4 | PDDeviceGray | DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is present | DeviceGray colour space is used without output intent profile |
| 6.2.4.4 | 1 | PDDeviceN | For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present | A colorant of the DeviceN or NChannel color space is not defined in the Colorants dictionary |
| 6.2.4.4 | 2 | PDSeparation | All Separation arrays within a single PDF/A-2 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, rather than ... | Several occurrences of a Separation colour space with the same name are not consistent |
| 6.2.5 | 1 | PDExtGState | An ExtGState dictionary shall not contain the TR key | An ExtGState dictionary contains the TR key |
| 6.2.5 | 2 | PDExtGState | An ExtGState dictionary shall not contain the TR2 key with a value other than Default | An ExtGState dictionary contains the TR2 key with a value other than Default |
| 6.2.5 | 3 | PDExtGState | An ExtGState dictionary shall not contain the HTP key | An ExtGState dictionary contains the HTP key |
| 6.2.5 | 4 | PDHalftone | All halftones in a conforming PDF/A-2 file shall have the value 1 or 5 for the HalftoneType key | A Halftone has type %1 instead of 1 or 5 |
| 6.2.5 | 5 | PDHalftone | Halftones in a conforming PDF/A-2 file shall not contain a HalftoneName key | A Halftone dictionary contains the HalftoneName key |
| 6.2.5 | 6 | PDHalftone | The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-1 | Custom TransferFunction in a Halftone dictionary is not permitted for primary (CMYK) colorants |
| 6.2.6 | 1 | CosRenderingIntent | Where a rendering intent is specified, its value shall be one of the four values defined in ISO 32000-1:2008, Table 70: RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation | A rendering intent with non-standard value %1 is used |
| 6.2.8 | 1 | PDXImage | An Image dictionary shall not contain the Alternates key | Alternates key is present in the Image dictionary |
| 6.2.8 | 2 | PDXImage | An Image dictionary shall not contain the OPI key | OPI key is present in the Image dictionary |
| 6.2.8 | 3 | PDXImage | If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key shall have a value of false | The value of the Interpolate key in the Image dictionary is true |
| 6.2.8 | 4 | PDXImage | If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16 | The value of the BitsPerComponent key in the Image dictionary is %1 |
| 6.2.8 | 5 | PDMaskImage | If an image mask dictionary contains the BitsPerComponent key, its value shall be 1 | The value of the BitsPerComponent key in the image mask dictionary is %1 |
| 6.2.8.3 | 1 | JPEG2000 | The number of colour channels in the JPEG2000 data shall be 1, 3 or 4 | JPEG2000 image has %1 colour channels that is neither 1, 3 or 4 |
| 6.2.8.3 | 2 | JPEG2000 | If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field | The JPEG2000 image contains %1 colour specifications with the best colour fidelity (value 0x01 in the APPROX field) |
| 6.2.8.3 | 3 | JPEG2000 | The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming reader shall use only that colour space and shall ignore all other colour space specifications | Invalid JPEG2000 image: the value of the METH entry (%1) in its 'colr' box is different from 0x01, 0x02 or 0x03 |
| 6.2.8.3 | 4 | JPEG2000 | JPEG2000 enumerated colour space 19 (CIEJab) shall not be used | JPEG2000 image uses enumerated colour space 19 (CIEJab), which is not allowed in PDF/A |
| 6.2.8.3 | 5 | JPEG2000 | The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth | JPEG2000 image has different bit-depth parameters in 'bpcc' box, or bit depth is out of range ('bpcc' box present = %1, bit depth = %2) |
| 6.2.9 | 1 | PDXForm | A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key | The form XObject dictionary contains a PS key, or a Subtype2 key with value PS, or an OPI key |
| 6.2.9 | 2 | PDXForm | A conforming file shall not contain any reference XObjects | The document contains a reference XObject (Ref key in the form XObject dictionary) |
| 6.2.9 | 3 | PDXObject | A conforming file shall not contain any PostScript XObjects | The document contains a PostScript XObject |
| 6.2.10 | 1 | CosBM | Only blend modes that are specified in ISO 32000-1:2008 shall be used for the value of the BM key in an extended graphic state dictionary | The document uses the blend mode %1 not defined in ISO 32000-1:2008 |
| 6.2.10 | 2 | PDPage | If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be u... | The page contains transparent objects with no blending colour space defined |
| 6.2.11.2 | 1 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Requi... | A Font dictionary has value %1 of Type entry instead of Font |
| 6.2.11.2 | 2 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Re... | Invalid Subtype entry in font dictionary with value %1 |
| 6.2.11.2 | 3 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (R... | A BaseFont entry is missing or has invalid type |
| 6.2.11.2 | 4 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer ... | A non-standard simple font dictionary has missing or invalid FirstChar entry |
| 6.2.11.2 | 5 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer -... | A non-standard simple font dictionary has missing or invalid LastChar entry |
| 6.2.11.2 | 6 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Re... | Widths array is missing or has invalid size %1 instead of %2 |
| 6.2.11.2 | 7 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.7 ar... | Unsupported font file format %1 of the embedded font |
| 6.2.11.3.1 | 1 | PDType0Font | For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary is Identity-H ... | CIDSystemInfo entries the CIDFont and CMap dictionaries of a Type 0 font are not compatible (CIDSystemInfo Ordering = %1, CMap Ordering = %2, CIDSy... |
| 6.2.11.3.2 | 1 | PDCIDFont | ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2... | A Type 2 CIDFont dictionary has missing or invalid CIDToGIDMap entry |
| 6.2.11.3.3 | 1 | PDCMap | All CMaps used within a PDF/A-2 file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5 | A non-standard CMap %1 is not embedded |
| 6.2.11.3.3 | 2 | CMapFile | For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream | WMode entry (value %1) in the embedded CMap and in the CMap dictionary (value %2) are not identical |
| 6.2.11.3.3 | 3 | PDReferencedCMap | A CMap shall not reference any other CMap except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118 | A CMap references another non-standard CMap %1 |
| 6.2.11.4.1 | 1 | PDFont | The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9 | The font program is not embedded |
| 6.2.11.4.1 | 2 | Glyph | Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType... | Not all glyphs referenced for rendering are present in the embedded font program |
| 6.2.11.4.2 | 1 | PDType1Font | If the FontDescriptor dictionary of an embedded Type 1 font contains a CharSet string, then it shall list the character names of all glyphs present in the font program, regardless of whether a glyph in the font is referenced or used by the PDF or not | A CharSet entry in the Descriptor dictionary of a Type1 font incorrectly lists glyphs present in the font program |
| 6.2.11.4.2 | 2 | PDCIDFont | If the FontDescriptor dictionary of an embedded CID font contains a CIDSet stream, then it shall identify all CIDs which are present in the font program, regardless of whether a CID in the font is referenced or used by the PDF or not | A CIDSet entry in the Font descriptor does not correctly identify all glyphs present in the embedded font subset |
| 6.2.11.5 | 1 | Glyph | For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent | Glyph width %1 in the embedded font program is not consistent with the Widths entry of the font dictionary (value %2) |
| 6.2.11.6 | 1 | TrueTypeFontProgram | For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain one or several non-symbolic cmap entries such that all necessary glyph lookups can be carried out | The embedded font program for a non-symbolic TrueType font does not contain non-symbolic cmap entries |
| 6.2.11.6 | 2 | PDTrueTypeFont | All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary that is the value of the Encoding key in the ... | A non-symbolic TrueType font encoding does not define a correct mapping to the Adobe Glyph List (Encoding = %1, Encoding entry contains a Differenc... |
| 6.2.11.6 | 3 | PDTrueTypeFont | Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary | A symbolic TrueType font specifies an Encoding entry in its dictionary |
| 6.2.11.6 | 4 | TrueTypeFontProgram | The 'cmap' table in the embedded font program for a symbolic TrueType font shall contain either exactly one encoding or it shall contain, at least, the Microsoft Symbol (3,0 - Platform ID=3, Encoding ID=0) encoding | The embedded font program for a symbolic TrueType font contains %1 cmap subtables and does not contain Microsoft Symbol (3,0 – Platform ID=3, Encod... |
| 6.2.11.7.2 | 1 | Glyph | The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 19005-2, 6.2.11.7.2 | The glyph can not be mapped to Unicode |
| 6.2.11.7.2 | 2 | Glyph | The Unicode values specified in the ToUnicode CMap shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE | The glyph has Unicode value 0, U+FEFF or U+FFFE, which is invalid by Unicode standard |
| 6.2.11.8 | 1 | Glyph | A PDF/A-2 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream | The document contains a reference to the .notdef glyph |
### Clause 6.3

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.3.1 | 1 | PDAnnot | Annotation types not defined in ISO 32000-1 shall not be permitted. Additionally, the 3D, Sound, Screen and Movie types shall not be permitted | Unknown or not permitted Annotation type %1 |
| 6.3.2 | 1 | PDAnnot | Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key | A dictionary of %1 annotation does not contain F key |
| 6.3.2 | 2 | PDAnnot | If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0 | Annotation flags are set the annotation to be hidden/invisible or non-printable (F = %1, Print = %2, Hidden = %3, Invisible = %4, NoView = %5, Togg... |
| 6.3.3 | 1 | PDAnnot | Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: - annotations where the value of the Rect key consists of an array whe... | An annotation does not contain an appearance dictionary |
| 6.3.3 | 2 | PDAnnot | For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key | Annotation's appearance dictionary contains entries %1 other than N |
| 6.3.3 | 3 | PDAnnot | If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary | An annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, but the value of the N key is %1 instead of appeara... |
| 6.3.3 | 4 | PDAnnot | If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream | An annotation dictionary's Subtype key has a value %1 and its FT key has a value %2, but the value of the N key is not an appearance stream |
### Clause 6.4

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.4.1 | 1 | PDWidgetAnnot | A Widget annotation dictionary shall not contain the A or AA keys | A Widget annotation contains %1 key(s) |
| 6.4.1 | 2 | PDFormField | A Field dictionary shall not contain the A or AA keys | A Form field dictionary contains the AA entry |
| 6.4.1 | 3 | PDAcroForm | The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false | The interactive form dictionary contains the NeedAppearances flag with value true |
| 6.4.2 | 1 | PDAcroForm | The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-2 file, if present, shall not contain the XFA key | The interactive form dictionary contains the XFA key |
| 6.4.2 | 2 | CosDocument | A document's Catalog shall not contain the NeedsRendering key | A document's Catalog contains NeedsRendering flag set to true |
| 6.4.3 | 1 | PDSignature | When computing the digest for the file, it shall be computed over the entire file, including the signature dictionary but excluding the PDF Signature itself | ByteRange array of the digital signature does not cover the entire file (excluding the PDF Signature itself) |
| 6.4.3 | 2 | PKCSDataObject | The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, it shall include the signer's X.5... | The DER-encoded PKCS#7 binary data object representing a PDF Signature does not include the signer's X.509 signing certificate |
| 6.4.3 | 3 | PKCSDataObject | The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, there shall only be a single sign... | The DER-encoded PKCS#7 binary data object representing a PDF Signature has %1 signer(s) in the PDF Signature instead of one |
### Clause 6.5

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.5.1 | 1 | PDAction | The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted | Unknown or not permitted Action type %1 |
| 6.5.1 | 2 | PDNamedAction | Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted | Unknown or not permitted named action %1 |
| 6.5.2 | 1 | PDDocument | The document's Catalog shall not include an AA entry for an additional-actions dictionary | The document catalog dictionary contains an additional-actions dictionary (AA entry) |
| 6.5.2 | 2 | PDPage | The Page dictionary shall not include an AA entry for an additional-actions dictionary | The Page dictionary contains an additional-actions dictionary (AA entry) |
### Clause 6.6

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.6.2.1 | 1 | PDDocument | The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-1:2008, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with... | The document catalog dictionary doesn't contain metadata key or metadata stream dictionary does not contain either entry Type with value /Metadata ... |
| 6.6.2.1 | 2 | XMPPackage | The bytes attribute shall not be used in the header of an XMP packet | The XMP Package contains bytes attribute |
| 6.6.2.1 | 3 | XMPPackage | The encoding attribute shall not be used in the header of an XMP packet | The XMP Package contains encoding attribute |
| 6.6.2.1 | 4 | XMPPackage | All metadata streams present in the PDF shall conform to the XMP Specification. All content of all XMP packets shall be well-formed, as defined by Extensible Markup Language (XML) 1.0 (Third Edition), 2.1, and the RDF/XML Syntax Specification (Rev... | A metadata stream is serialized incorrectly and can not be parsed |
| 6.6.2.1 | 5 | XMPPackage | All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8 | The XMP package uses encoding %1 different from UTF-8 |
| 6.6.2.3.1 | 1 | XMPProperty | All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2 | XMP property is either not predefined, or is not defined in any XMP extension schema |
| 6.6.2.3.1 | 2 | XMPProperty | All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2 | XMP property does not correspond to type %1 |
| 6.6.2.3.2 | 1 | ExtensionSchemaObject | Extension schemas shall be specified using the PDF/A extension schema container schema defined in 6.6.2.3.3. All fields described in each of the tables in 6.6.2.3.3 shall be present in any extension schema container schema | An extension schema object contains field(s) %1 not defined by the specification |
| 6.6.2.3.3 | 1 | ExtensionSchemasContainer | The extension schema container schema uses the namespace URI "http://www.aiim.org/pdfa/ns/extension/". The required schema namespace prefix is pdfaExtension. pdfaExtension:schemas - Bag Schema - Description of extension schemas | The extension schema container has invalid type (isValidBag = %1) or doesn't have prefix 'pdfaExtension' (prefix = %2) |
| 6.6.2.3.3 | 2 | ExtensionSchemaDefinition | Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'schema' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (schema prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 3 | ExtensionSchemaDefinition | Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema' | Field 'namespaceURI' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (namespaceURI prefix = %1) or doesn't have ... |
| 6.6.2.3.3 | 4 | ExtensionSchemaDefinition | Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'prefix' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (prefix prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 5 | ExtensionSchemaDefinition | Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema' | Field 'property' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (property prefix = %1) or doesn't have type Seq Property (isProper... |
| 6.6.2.3.3 | 6 | ExtensionSchemaDefinition | Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema' | Field 'valueType' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (valueType prefix = %1) or doesn't have type Seq ValueType (isVal... |
| 6.6.2.3.3 | 7 | ExtensionSchemaProperty | Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'name' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (name prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 8 | ExtensionSchemaProperty | Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (valueType prefix = %1) or doesn't have ty... |
| 6.6.2.3.3 | 9 | ExtensionSchemaProperty | Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty' | Field 'category' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (category prefix = %1) or doesn't have type... |
| 6.6.2.3.3 | 10 | ExtensionSchemaProperty | Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'description' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (description prefix = %1) or doesn't hav... |
| 6.6.2.3.3 | 11 | ExtensionSchemaValueType | Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'type' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (type prefix = %1) or doesn't have type Text (isTy... |
| 6.6.2.3.3 | 12 | ExtensionSchemaValueType | Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType' | Field 'namespaceURI' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (namespaceURI prefix = %1) or doesn't have... |
| 6.6.2.3.3 | 13 | ExtensionSchemaValueType | Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'prefix' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (prefix prefix = %1) or doesn't have type Text (... |
| 6.6.2.3.3 | 14 | ExtensionSchemaValueType | Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'description' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (description prefix = %1) or doesn't have t... |
| 6.6.2.3.3 | 15 | ExtensionSchemaValueType | Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType' | Field 'field' of the PDF/A ValueType value type doesn't have prefix 'pdfaType' (field prefix = %1) or doesn't have type Seq Field (isFieldValidSeq ... |
| 6.6.2.3.3 | 16 | ExtensionSchemaField | Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'name' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (name prefix = %1) or doesn't have type Text (isNameV... |
| 6.6.2.3.3 | 17 | ExtensionSchemaField | Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (valueType prefix = %1) or doesn't have type Tex... |
| 6.6.2.3.3 | 18 | ExtensionSchemaField | Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'description' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (description prefix = %1) or doesn't have type... |
| 6.6.4 | 1 | MainXMPPackage | The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema | The document metadata stream doesn't contain PDF/A Identification Schema |
| 6.6.4 | 2 | PDFAIdentification | The value of "pdfaid:part" shall be the part number of ISO 19005 to which the file conforms | The "part" property of the PDF/A Identification Schema is %1 instead of 2 for PDF/A-2 conforming file |
| 6.6.4 | 3 | PDFAIdentification | A Level A conforming file shall specify the value of "pdfaid:conformance" as A. A Level B conforming file shall specify the value of "pdfaid:conformance" as B. A Level U conforming file shall specify the value of "pdfaid:conformance" as U | The "conformance" property of the PDF/A Identification Schema is %1 instead of "U" for PDF/A-2U conforming file |
| 6.6.4 | 4 | PDFAIdentification | Property "part" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "part" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 5 | PDFAIdentification | Property "conformance" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "conformance" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 6 | PDFAIdentification | Property "amd" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "amd" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 7 | PDFAIdentification | Property "corr" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "corr" of the PDF/A Identification Schema has invalid namespace prefix %1 |
### Clause 6.8

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.8 | 2 | CosFileSpecification | The file specification dictionary for an embedded file shall contain the F and UF keys | The file specification dictionary for an embedded file does not contain either F or UF key (F = %1, UF = %2) |
| 6.8 | 5 | EmbeddedFile | A file specification dictionary, as defined in ISO 32000-1:2008, 7.11.3, may contain the EF key, provided that the embedded file is compliant with either ISO 19005-1 or this part of ISO 19005 | An embedded file does not comply to either ISO 19005-1 or ISO 19005-2 |
### Clause 6.9

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.9 | 1 | PDOCConfig | Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key | Missing or empty Name entry of the optional content configuration dictionary |
| 6.9 | 2 | PDOCConfig | Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-2 file | Optional content configuration dictionary has duplicated name %1 |
| 6.9 | 3 | PDOCConfig | If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file | Optional content group(s) %1 not present in the Order entry of the optional content configuration dictionary |
| 6.9 | 4 | PDOCConfig | The AS key shall not appear in any optional content configuration dictionary | AS key is present in the optional content configuration dictionary |
### Clause 6.10

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.10 | 1 | PDDocument | There shall be no AlternatePresentations entry in the document's name dictionary | The document's name dictionary contains the AlternatePresentations entry |
| 6.10 | 2 | PDPage | There shall be no PresSteps entry in any Page dictionary | A Page dictionary contains the PresSteps entry |
### Clause 6.11

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.11 | 1 | CosDocument | The document catalog shall not contain the Requirements key | The document catalog contains the Requirements key |

## PDF/A-3b

**PDF/A-3B validation profile**
*Validation rules against ISO 19005-3:2012, Level B*
**Total rules: 146**

### Clause 6.1

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.1.2 | 1 | CosDocument | The file header shall begin at byte zero and shall consist of "%PDF-1.n" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h) | File header %1 (offset = %2) starts at non-zero offset or does not match the pattern %PDF-1.n, where 'n' is a single digit number between 0 and 7 |
| 6.1.2 | 2 | CosDocument | The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127 | Binary comment in the file header is missing or does not start with 4 bytes with byte values above 127 (first four bytes = %1, %2, %3, %4) |
| 6.1.3 | 1 | CosDocument | The file trailer dictionary shall contain the ID keyword whose value shall be File Identifiers as defined in ISO 32000-1:2008, 14.4 | Missing or empty ID in the document trailer |
| 6.1.3 | 2 | CosTrailer | The keyword Encrypt shall not be used in the trailer dictionary | Encrypt keyword is present in the trailer dictionary |
| 6.1.3 | 3 | CosDocument | No data can follow the last end-of-file marker except a single optional end-of-line marker as described in ISO 32000-1:2008, 7.5.5 | %1 byte(s) of data is present after the last end-of-file marker |
| 6.1.4 | 2 | CosXRef | The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker | Extra spacings or missing EOL characters after the 'xref' keyword in the cross reference table |
| 6.1.6 | 1 | CosString | Hexadecimal strings shall contain an even number of non-white-space characters | A hexadecimal string contains odd number (%1) of non-white-space characters |
| 6.1.6 | 2 | CosString | A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f) | Hexadecimal string contains non-white-space characters outside the range 0 to 9, A to F or a to f |
| 6.1.7.1 | 1 | CosStream | The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword | Actual length of the stream (%1 byte(s)) does not match the value of the Length key in the Stream dictionary (%2 byte(s)) |
| 6.1.7.1 | 2 | CosStream | The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED (0Ah) character. The endstream keyword shall be preceded by an EOL marker | Extra spacings or missing EOL characters around keywords 'stream' and 'endstream' |
| 6.1.7.1 | 3 | CosStream | A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys | A stream object dictionary contains %1 key(s) |
| 6.1.7.2 | 1 | CosFilter | All standard stream filters listed in ISO 32000-1:2008, 7.4, Table 6 may be used, with the exception of LZWDecode. In addition, the Crypt filter shall not be used unless the value of the Name key in the decode parameters dictionary is Identity. Fi... | Unknown or not permitted Stream filter %1 is used |
| 6.1.8 | 1 | CosUnicodeName | Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences | The name value %1 does not represent a correct UTF-8 character sequence |
| 6.1.9 | 1 | CosIndirect | The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded b... | Extra spacings or missing EOL characters around indirect object/generation number or keywords 'obj' and 'endobj' |
| 6.1.10 | 1 | CosIIFilter | The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-1:2008, Table 6, or an array containing any such value | Inline image uses not permitted or unknown filter %1 |
| 6.1.12 | 1 | PDPerms | No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-1:2008, 12.8.4, Table 258) | The document permissions dictionary contains key(s) %1 other than UR3 and DocMDP |
| 6.1.12 | 2 | PDSigRef | If DocMDP is present, then the Signature References dictionary (ISO 32000-1:2008, 12.8.1, Table 253) shall not contain the keys DigestLocation, DigestMethod, and DigestValue | The Signature References dictionary contains %1 key(s) in presence of DocMDP entry in the permissions dictionary |
| 6.1.13 | 1 | CosInteger | A conforming file shall not contain any integer greater than 2147483647. A conforming file shall not contain any integer less than -2147483648 | Integer value %1 out of range |
| 6.1.13 | 2 | CosReal | A conforming file shall not contain any real number outside the range of +/-3.403 x 10^38 | Real value %1 out of range |
| 6.1.13 | 3 | CosString | A conforming file shall not contain any string longer than 32767 bytes | String length (%1) exceeded 32767 |
| 6.1.13 | 4 | CosName | A conforming file shall not contain any name longer than 127 bytes | Name length (%1) exceeded 127 |
| 6.1.13 | 5 | CosReal | A conforming file shall not contain any real number closer to zero than +/-1.175 x 10^(-38) | Non-zero real value %1 is too close to 0.0 |
| 6.1.13 | 7 | CosDocument | A conforming file shall not contain more than 8388607 indirect objects | Number of indirect objects in a PDF file (%1) exceeded 8,388,607 |
| 6.1.13 | 8 | Op_q_gsave | A conforming file shall not nest q/Q pairs by more than 28 nesting levels | Depth of graphics state nesting of q/Q operators (%1) exceeded 28 |
| 6.1.13 | 9 | PDDeviceN | A conforming file shall not contain a DeviceN colour space with more than 32 colourants | Number of DeviceN components (%1) exceeded 32 |
| 6.1.13 | 10 | CMapFile | A conforming file shall not contain a CID value greater than 65535 | Value of a CID (%1) exceeded 65,535 |
| 6.1.13 | 11 | CosBBox | The size of any of the page boundaries described in ISO 32000-1:2008, 14.11.2 shall not be less than 3 units in either direction, nor shall it be greater than 14 400 units in either direction | One of the page boundaries is out of range (valid range: 3 - 14400, height = %1, width = %2) |
### Clause 6.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.2.2 | 1 | Op_Undefined | Content streams shall not contain any operators not defined in ISO 32000-1 even if such operators are bracketed by the BX/EX compatibility operators | A content stream contains operator %1 not defined in ISO 32000-1 |
| 6.2.2 | 2 | PDContentStream | A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-1:2008, 7.8.3 | A content stream refers to resource(s) %1 not defined in an explicitly associated Resources dictionary |
| 6.2.3 | 1 | ICCOutputProfile | The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = "prtr") or a monitor profile (Device Class = "mntr"). The profiles shall have a colour space of either "GRAY", "RGB", or "CMYK" | The embedded PDF/A Output Intent colour profile has invalid header (Device Class = %1, color space = %2, version = %3) |
| 6.2.3 | 2 | OutputIntents | If a file's OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X-4 or PDF/E-1, then all entries that contain a DestOutputProfile key shall have... | File's OutputIntents array contains output intent dictionaries with non-matching destination output profiles (indirect keys %1) |
| 6.2.3 | 3 | PDOutputIntent | In addition, the DestOutputProfileRef key, as defined in ISO 15930-7:2010, Annex A, shall not be present in any PDF/X OutputIntent | The PDF/X output intent dictionary contains forbidden entry DestOutputProfileRef |
| 6.2.4.2 | 1 | ICCInputProfile | The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1 | The embedded ICC profile (Device Class = %1, color space = %2, version = %3) is either invalid or does not satisfy PDF 1.7 requirements |
| 6.2.4.2 | 2 | PDICCBasedCMYK | Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and over... | Overprint mode (OPM) is set to %1 instead of 0 when an ICCBased CMYK colour space is used with enabled overprinting |
| 6.2.4.3 | 2 | PDDeviceRGB | DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used, or if the file has a PDF/A OutputIntent that contains an RGB destination profile | DeviceRGB colour space is used without RGB output intent profile |
| 6.2.4.3 | 3 | PDDeviceCMYK | DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set or if a DeviceN-based DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or the file has a PDF/A OutputIntent that contains a C... | DeviceCMYK colour space is used without CMYK output intent profile |
| 6.2.4.3 | 4 | PDDeviceGray | DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is present | DeviceGray colour space is used without output intent profile |
| 6.2.4.4 | 1 | PDDeviceN | For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present | A colorant of the DeviceN or NChannel color space is not defined in the Colorants dictionary |
| 6.2.4.4 | 2 | PDSeparation | All Separation arrays within a single PDF/A-3 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, rather than ... | Several occurrences of a Separation colour space with the same name are not consistent |
| 6.2.5 | 1 | PDExtGState | An ExtGState dictionary shall not contain the TR key | An ExtGState dictionary contains the TR key |
| 6.2.5 | 2 | PDExtGState | An ExtGState dictionary shall not contain the TR2 key with a value other than Default | An ExtGState dictionary contains the TR2 key with a value other than Default |
| 6.2.5 | 3 | PDExtGState | An ExtGState dictionary shall not contain the HTP key | An ExtGState dictionary contains the HTP key |
| 6.2.5 | 4 | PDHalftone | All halftones in a conforming PDF/A-3 file shall have the value 1 or 5 for the HalftoneType key | A Halftone has type %1 instead of 1 or 5 |
| 6.2.5 | 5 | PDHalftone | Halftones in a conforming PDF/A-3 file shall not contain a HalftoneName key | A Halftone dictionary contains the HalftoneName key |
| 6.2.5 | 6 | PDHalftone | The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-1 | Custom TransferFunction in a Halftone dictionary is not permitted for primary (CMYK) colorants |
| 6.2.6 | 1 | CosRenderingIntent | Where a rendering intent is specified, its value shall be one of the four values defined in ISO 32000-1:2008, Table 70: RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation | A rendering intent with non-standard value %1 is used |
| 6.2.8 | 1 | PDXImage | An Image dictionary shall not contain the Alternates key | Alternates key is present in the Image dictionary |
| 6.2.8 | 2 | PDXImage | An Image dictionary shall not contain the OPI key | OPI key is present in the Image dictionary |
| 6.2.8 | 3 | PDXImage | If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key shall have a value of false | The value of the Interpolate key in the Image dictionary is true |
| 6.2.8 | 4 | PDXImage | If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16 | The value of the BitsPerComponent key in the Image dictionary is %1 |
| 6.2.8 | 5 | PDMaskImage | If an image mask dictionary contains the BitsPerComponent key, its value shall be 1 | The value of the BitsPerComponent key in the image mask dictionary is %1 |
| 6.2.8.3 | 1 | JPEG2000 | The number of colour channels in the JPEG2000 data shall be 1, 3 or 4 | JPEG2000 image has %1 colour channels that is neither 1, 3 or 4 |
| 6.2.8.3 | 2 | JPEG2000 | If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field | The JPEG2000 image contains %1 colour specifications with the best colour fidelity (value 0x01 in the APPROX field) |
| 6.2.8.3 | 3 | JPEG2000 | The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming reader shall use only that colour space and shall ignore all other colour space specifications | Invalid JPEG2000 image: the value of the METH entry (%1) in its 'colr' box is different from 0x01, 0x02 or 0x03 |
| 6.2.8.3 | 4 | JPEG2000 | JPEG2000 enumerated colour space 19 (CIEJab) shall not be used | JPEG2000 image uses enumerated colour space 19 (CIEJab), which is not allowed in PDF/A |
| 6.2.8.3 | 5 | JPEG2000 | The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth | JPEG2000 image has different bit-depth parameters in 'bpcc' box, or bit depth is out of range ('bpcc' box present = %1, bit depth = %2) |
| 6.2.9 | 1 | PDXForm | A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key | The form XObject dictionary contains a PS key, or a Subtype2 key with value PS, or an OPI key |
| 6.2.9 | 2 | PDXForm | A conforming file shall not contain any reference XObjects | The document contains a reference XObject (Ref key in the form XObject dictionary) |
| 6.2.9 | 3 | PDXObject | A conforming file shall not contain any PostScript XObjects | The document contains a PostScript XObject |
| 6.2.10 | 1 | CosBM | Only blend modes that are specified in ISO 32000-1:2008 shall be used for the value of the BM key in an extended graphic state dictionary | The document uses the blend mode %1 not defined in ISO 32000-1:2008 |
| 6.2.10 | 2 | PDPage | If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be u... | The page contains transparent objects with no blending colour space defined |
| 6.2.11.2 | 1 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Requi... | A Font dictionary has value %1 of Type entry instead of Font |
| 6.2.11.2 | 2 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Re... | Invalid Subtype entry in font dictionary with value %1 |
| 6.2.11.2 | 3 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (R... | A BaseFont entry is missing or has invalid type |
| 6.2.11.2 | 4 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer ... | A non-standard simple font dictionary has missing or invalid FirstChar entry |
| 6.2.11.2 | 5 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer -... | A non-standard simple font dictionary has missing or invalid LastChar entry |
| 6.2.11.2 | 6 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Re... | Widths array is missing or has invalid size %1 instead of %2 |
| 6.2.11.2 | 7 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.7 ar... | Unsupported font file format %1 of the embedded font |
| 6.2.11.3.1 | 1 | PDType0Font | For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary is Identity-H ... | CIDSystemInfo entries the CIDFont and CMap dictionaries of a Type 0 font are not compatible (CIDSystemInfo Ordering = %1, CMap Ordering = %2, CIDSy... |
| 6.2.11.3.2 | 1 | PDCIDFont | ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2... | A Type 2 CIDFont dictionary has missing or invalid CIDToGIDMap entry |
| 6.2.11.3.3 | 1 | PDCMap | All CMaps used within a PDF/A-3 file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5 | A non-standard CMap %1 is not embedded |
| 6.2.11.3.3 | 2 | CMapFile | For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream | WMode entry (value %1) in the embedded CMap and in the CMap dictionary (value %2) are not identical |
| 6.2.11.3.3 | 3 | PDReferencedCMap | A CMap shall not reference any other CMap except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118 | A CMap references another non-standard CMap %1 |
| 6.2.11.4.1 | 1 | PDFont | The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9 | The font program is not embedded |
| 6.2.11.4.1 | 2 | Glyph | Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType... | Not all glyphs referenced for rendering are present in the embedded font program |
| 6.2.11.4.2 | 1 | PDType1Font | If the FontDescriptor dictionary of an embedded Type 1 font contains a CharSet string, then it shall list the character names of all glyphs present in the font program, regardless of whether a glyph in the font is referenced or used by the PDF or not | A CharSet entry in the Descriptor dictionary of a Type1 font incorrectly lists glyphs present in the font program |
| 6.2.11.4.2 | 2 | PDCIDFont | If the FontDescriptor dictionary of an embedded CID font contains a CIDSet stream, then it shall identify all CIDs which are present in the font program, regardless of whether a CID in the font is referenced or used by the PDF or not | A CIDSet entry in the Font descriptor does not correctly identify all glyphs present in the embedded font subset |
| 6.2.11.5 | 1 | Glyph | For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent | Glyph width %1 in the embedded font program is not consistent with the Widths entry of the font dictionary (value %2) |
| 6.2.11.6 | 1 | TrueTypeFontProgram | For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain one or several non-symbolic cmap entries such that all necessary glyph lookups can be carried out | The embedded font program for a non-symbolic TrueType font does not contain non-symbolic cmap entries |
| 6.2.11.6 | 2 | PDTrueTypeFont | All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary that is the value of the Encoding key in the ... | A non-symbolic TrueType font encoding does not define a correct mapping to the Adobe Glyph List (Encoding = %1, Encoding entry contains a Differenc... |
| 6.2.11.6 | 3 | PDTrueTypeFont | Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary | A symbolic TrueType font specifies an Encoding entry in its dictionary |
| 6.2.11.6 | 4 | TrueTypeFontProgram | The 'cmap' table in the embedded font program for a symbolic TrueType font shall contain either exactly one encoding or it shall contain, at least, the Microsoft Symbol (3,0 - Platform ID=3, Encoding ID=0) encoding | The embedded font program for a symbolic TrueType font contains %1 cmap subtables and does not contain Microsoft Symbol (3,0 – Platform ID=3, Encod... |
| 6.2.11.8 | 1 | Glyph | A PDF/A-3 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream | The document contains a reference to the .notdef glyph |
### Clause 6.3

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.3.1 | 1 | PDAnnot | Annotation types not defined in ISO 32000-1 shall not be permitted. Additionally, the 3D, Sound, Screen and Movie types shall not be permitted | Unknown or not permitted Annotation type %1 |
| 6.3.2 | 1 | PDAnnot | Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key | A dictionary of %1 annotation does not contain F key |
| 6.3.2 | 2 | PDAnnot | If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0 | Annotation flags are set the annotation to be hidden/invisible or non-printable (F = %1, Print = %2, Hidden = %3, Invisible = %4, NoView = %5, Togg... |
| 6.3.3 | 1 | PDAnnot | Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: - annotations where the value of the Rect key consists of an array whe... | An annotation does not contain an appearance dictionary |
| 6.3.3 | 2 | PDAnnot | For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key | Annotation's appearance dictionary contains entries %1 other than N |
| 6.3.3 | 3 | PDAnnot | If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary | An annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, but the value of the N key is %1 instead of appeara... |
| 6.3.3 | 4 | PDAnnot | If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream | An annotation dictionary's Subtype key has a value %1 and its FT key has a value %2, but the value of the N key is not an appearance stream |
### Clause 6.4

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.4.1 | 1 | PDWidgetAnnot | A Widget annotation dictionary shall not contain the A or AA keys | A Widget annotation contains %1 key(s) |
| 6.4.1 | 2 | PDFormField | A Field dictionary shall not contain the A or AA keys | A Form field dictionary contains the AA entry |
| 6.4.1 | 3 | PDAcroForm | The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false | The interactive form dictionary contains the NeedAppearances flag with value true |
| 6.4.2 | 1 | PDAcroForm | The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-3 file, if present, shall not contain the XFA key | The interactive form dictionary contains the XFA key |
| 6.4.2 | 2 | CosDocument | A document's Catalog shall not contain the NeedsRendering key | A document's Catalog contains NeedsRendering flag set to true |
| 6.4.3 | 1 | PDSignature | When computing the digest for the file, it shall be computed over the entire file, including the signature dictionary but excluding the PDF Signature itself | ByteRange array of the digital signature does not cover the entire file (excluding the PDF Signature itself) |
| 6.4.3 | 2 | PKCSDataObject | The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, it shall include the signer's X.5... | The DER-encoded PKCS#7 binary data object representing a PDF Signature does not include the signer's X.509 signing certificate |
| 6.4.3 | 3 | PKCSDataObject | The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, there shall only be a single sign... | The DER-encoded PKCS#7 binary data object representing a PDF Signature has %1 signer(s) in the PDF Signature instead of one |
### Clause 6.5

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.5.1 | 1 | PDAction | The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted | Unknown or not permitted Action type %1 |
| 6.5.1 | 2 | PDNamedAction | Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted | Unknown or not permitted named action %1 |
| 6.5.2 | 1 | PDDocument | The document's Catalog shall not include an AA entry for an additional-actions dictionary | The document catalog dictionary contains an additional-actions dictionary (AA entry) |
| 6.5.2 | 2 | PDPage | The Page dictionary shall not include an AA entry for an additional-actions dictionary | The Page dictionary contains an additional-actions dictionary (AA entry) |
### Clause 6.6

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.6.2.1 | 1 | PDDocument | The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-1:2008, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with... | The document catalog dictionary doesn't contain metadata key or metadata stream dictionary does not contain either entry Type with value /Metadata ... |
| 6.6.2.1 | 2 | XMPPackage | The bytes attribute shall not be used in the header of an XMP packet | The XMP Package contains bytes attribute |
| 6.6.2.1 | 3 | XMPPackage | The encoding attribute shall not be used in the header of an XMP packet | The XMP Package contains encoding attribute |
| 6.6.2.1 | 4 | XMPPackage | All metadata streams present in the PDF shall conform to the XMP Specification. All content of all XMP packets shall be well-formed, as defined by Extensible Markup Language (XML) 1.0 (Third Edition), 2.1, and the RDF/XML Syntax Specification (Rev... | A metadata stream is serialized incorrectly and can not be parsed |
| 6.6.2.1 | 5 | XMPPackage | All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8 | The XMP package uses encoding %1 different from UTF-8 |
| 6.6.2.3.1 | 1 | XMPProperty | All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2 | XMP property is either not predefined, or is not defined in any XMP extension schema |
| 6.6.2.3.1 | 2 | XMPProperty | All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2 | XMP property does not correspond to type %1 |
| 6.6.2.3.2 | 1 | ExtensionSchemaObject | Extension schemas shall be specified using the PDF/A extension schema container schema defined in 6.6.2.3.3. All fields described in each of the tables in 6.6.2.3.3 shall be present in any extension schema container schema | An extension schema object contains field(s) %1 not defined by the specification |
| 6.6.2.3.3 | 1 | ExtensionSchemasContainer | The extension schema container schema uses the namespace URI "http://www.aiim.org/pdfa/ns/extension/". The required schema namespace prefix is pdfaExtension. pdfaExtension:schemas - Bag Schema - Description of extension schemas | The extension schema container has invalid type (isValidBag = %1) or doesn't have prefix 'pdfaExtension' (prefix = %2) |
| 6.6.2.3.3 | 2 | ExtensionSchemaDefinition | Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'schema' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (schema prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 3 | ExtensionSchemaDefinition | Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema' | Field 'namespaceURI' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (namespaceURI prefix = %1) or doesn't have ... |
| 6.6.2.3.3 | 4 | ExtensionSchemaDefinition | Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema' | Field 'prefix' of the PDF/A Schema value type is not present, or doesn't have prefix 'pdfaSchema' (prefix prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 5 | ExtensionSchemaDefinition | Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema' | Field 'property' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (property prefix = %1) or doesn't have type Seq Property (isProper... |
| 6.6.2.3.3 | 6 | ExtensionSchemaDefinition | Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema' | Field 'valueType' of the PDF/A Schema value type doesn't have prefix 'pdfaSchema' (valueType prefix = %1) or doesn't have type Seq ValueType (isVal... |
| 6.6.2.3.3 | 7 | ExtensionSchemaProperty | Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'name' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (name prefix = %1) or doesn't have type Text (i... |
| 6.6.2.3.3 | 8 | ExtensionSchemaProperty | Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (valueType prefix = %1) or doesn't have ty... |
| 6.6.2.3.3 | 9 | ExtensionSchemaProperty | Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty' | Field 'category' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (category prefix = %1) or doesn't have type... |
| 6.6.2.3.3 | 10 | ExtensionSchemaProperty | Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty' | Field 'description' of the PDF/A Property value type is not present, or doesn't have prefix 'pdfaProperty' (description prefix = %1) or doesn't hav... |
| 6.6.2.3.3 | 11 | ExtensionSchemaValueType | Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'type' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (type prefix = %1) or doesn't have type Text (isTy... |
| 6.6.2.3.3 | 12 | ExtensionSchemaValueType | Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType' | Field 'namespaceURI' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (namespaceURI prefix = %1) or doesn't have... |
| 6.6.2.3.3 | 13 | ExtensionSchemaValueType | Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'prefix' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (prefix prefix = %1) or doesn't have type Text (... |
| 6.6.2.3.3 | 14 | ExtensionSchemaValueType | Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType' | Field 'description' of the PDF/A ValueType value type is not present, or doesn't have prefix 'pdfaType' (description prefix = %1) or doesn't have t... |
| 6.6.2.3.3 | 15 | ExtensionSchemaValueType | Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType' | Field 'field' of the PDF/A ValueType value type doesn't have prefix 'pdfaType' (field prefix = %1) or doesn't have type Seq Field (isFieldValidSeq ... |
| 6.6.2.3.3 | 16 | ExtensionSchemaField | Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'name' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (name prefix = %1) or doesn't have type Text (isNameV... |
| 6.6.2.3.3 | 17 | ExtensionSchemaField | Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined | Field 'valueType' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (valueType prefix = %1) or doesn't have type Tex... |
| 6.6.2.3.3 | 18 | ExtensionSchemaField | Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField' | Field 'description' of the PDF/A Field value type is not present, or doesn't have prefix 'pdfaField' (description prefix = %1) or doesn't have type... |
| 6.6.4 | 1 | MainXMPPackage | The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema | The document metadata stream doesn't contain PDF/A Identification Schema |
| 6.6.4 | 2 | PDFAIdentification | The value of "pdfaid:part" shall be the part number of ISO 19005 to which the file conforms | The "part" property of the PDF/A Identification Schema is %1 instead of 3 for PDF/A-3 conforming file |
| 6.6.4 | 3 | PDFAIdentification | A Level A conforming file shall specify the value of pdfaid:conformance as A. A Level B conforming file shall specify the value of pdfaid:conformance as B. A Level U conforming file shall specify the value of pdfaid:conformance as U | The "conformance" property of the PDF/A Identification Schema is %1 instead of "B" for PDF/A-3B conforming file |
| 6.6.4 | 4 | PDFAIdentification | Property "part" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "part" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 5 | PDFAIdentification | Property "conformance" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "conformance" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 6 | PDFAIdentification | Property "amd" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "amd" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.6.4 | 7 | PDFAIdentification | Property "corr" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "corr" of the PDF/A Identification Schema has invalid namespace prefix %1 |
### Clause 6.8

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.8 | 1 | EmbeddedFile | The MIME type of an embedded file, or a subset of a file, shall be specified using the Subtype key of the file specification dictionary. If the MIME type is not known, the "application/octet-stream" shall be used | MIME type %1 of an embedded file is missing or invalid |
| 6.8 | 2 | CosFileSpecification | The file specification dictionary for an embedded file shall contain the F and UF keys | The file specification dictionary for an embedded file does not contain either F or UF key (F = %1, UF = %2) |
| 6.8 | 3 | CosFileSpecification | The file specification dictionary shall contain key AFRelationship of type Name identifying the relationship between the embedded file and the content of the document | The file specification dictionary for an embedded file does not contain the AFRelationship key or it has invalid value type |
| 6.8 | 4 | CosFileSpecification | The additional information provided for associated files as well as the usage requirements for associated files indicate the relationship between the embedded file and the PDF document or the part of the PDF document with which it is associated | The file specification dictionary for an embedded file is not associated with the PDF document or any of its parts |
### Clause 6.9

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.9 | 1 | PDOCConfig | Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key | Missing or empty Name entry of the optional content configuration dictionary |
| 6.9 | 2 | PDOCConfig | Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-3 file | Optional content configuration dictionary has duplicated name %1 |
| 6.9 | 3 | PDOCConfig | If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file | Optional content group(s) %1 not present in the Order entry of the optional content configuration dictionary |
| 6.9 | 4 | PDOCConfig | The AS key shall not appear in any optional content configuration dictionary | AS key is present in the optional content configuration dictionary |
### Clause 6.10

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.10 | 1 | PDDocument | There shall be no AlternatePresentations entry in the document's name dictionary | The document's name dictionary contains the AlternatePresentations entry |
| 6.10 | 2 | PDPage | There shall be no PresSteps entry in any Page dictionary | A Page dictionary contains the PresSteps entry |
### Clause 6.11

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.11 | 1 | CosDocument | The document catalog shall not contain the Requirements key | The document catalog contains the Requirements key |

## PDF/A-4

**PDF/A-4 validation profile**
*Validation rules against ISO 19005-4:2020*
**Total rules: 109**

### Clause 6.1

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.1.2 | 1 | CosDocument | The file header shall begin at byte zero and shall consist of "%PDF-2.n" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 9 (39h) | File header %1 (offset = %2) starts at non-zero offset or does not match the pattern %PDF-2.n, where 'n' is a single digit number between 0 and 9 |
| 6.1.2 | 2 | CosDocument | The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127 | Binary comment in the file header is missing or does not start with 4 bytes with byte values above 127 (first four bytes = %1, %2, %3, %4) |
| 6.1.3 | 1 | CosDocument | File identifiers shall be defined by the ID entry in a PDF file’s trailer dictionary | Missing or empty ID in the document trailer |
| 6.1.3 | 2 | CosTrailer | The Encrypt key shall not be present in the trailer dictionary | The Encrypt key is present in the trailer dictionary |
| 6.1.3 | 3 | CosDocument | No data shall follow the last end-of-file marker as described in ISO 32000-2:2020, 7.5.5 | %1 byte(s) of data is present after the last end-of-file marker |
| 6.1.3 | 4 | CosDocument | The Info key shall not be present in the trailer dictionary of PDF/A-4 conforming files unless there exists a PieceInfo entry in the document catalog dictionary | The Info key present in the trailer dictionary, but PieceInfo entry does not present in the document catalog dictionary |
| 6.1.3 | 5 | CosInfo | If a document information dictionary is present, it shall only contain a ModDate entry | Document information dictionary is present and contains %1 key(s), but allowed only ModDate |
| 6.1.4 | 1 | CosXRef | The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker | Extra spacings or missing EOL characters after the 'xref' keyword in the cross reference table |
| 6.1.5 | 1 | CosString | Hexadecimal strings shall contain an even number of non-white-space characters | A hexadecimal string contains odd number (%1) of non-white-space characters |
| 6.1.5 | 2 | CosString | A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f) | Hexadecimal string contains non-white-space characters outside the range 0 to 9, A to F or a to f |
| 6.1.6.1 | 1 | CosStream | The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword | Actual length of the stream (%1 byte(s)) does not match the value of the Length key in the Stream dictionary (%2 byte(s)) |
| 6.1.6.1 | 2 | CosStream | A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys | A stream object dictionary contains %1 key(s) |
| 6.1.6.2 | 1 | CosFilter | All standard stream filters listed in ISO 32000-2:2020, 7.4, Table 6 may be used, with the exception of LZWDecode. Filters that are not listed in ISO 32000-2:2020, 7.4, Table 6 shall not be used. In addition, the Crypt filter shall not be used unl... | Unknown or not permitted Stream filter %1 is used |
| 6.1.7 | 1 | CosUnicodeName | Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences | The name value %1 does not represent a correct UTF-8 character sequence |
| 6.1.8 | 1 | CosIndirect | The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded b... | Extra spacings or missing EOL characters around indirect object/generation number or keywords 'obj' and 'endobj' |
| 6.1.9 | 1 | CosIIFilter | The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-2:2020, 8.9.7, Table 92, or an array containing any such value | Inline image uses not permitted or unknown filter %1 |
| 6.1.11 | 1 | PDPerms | No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-2:2020, 12.8.6, Table 263) | The document permissions dictionary contains key(s) %1 other than UR3 and DocMDP |
| 6.1.12 | 1 | PDDocument | If the Version key is present in the document catalog dictionary, the first character in its value shall be a 2 (32h) and the second character of its value shall be a PERIOD (2Eh) (decimal point). The third character shall be a decimal digit. The ... | The Version key with value %1 does not match the pattern 2.n, where 'n' is a single digit number between 0 and 9 |
### Clause 6.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.2.2 | 1 | Op_Undefined | Content streams shall not contain any operators not defined in ISO 32000-2:2020 even if such operators are bracketed by the BX/EX compatibility operators | A content stream contains operator %1 not defined in ISO 32000-2:2020 |
| 6.2.2 | 2 | PDContentStream | A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-2:2020, 7.8.3. Such resource dict... | A content stream refers to resource(s) %1 not defined in an explicitly associated Resources dictionary |
| 6.2.2 | 3 | PDContentStream | A content stream's named resources shall be defined by a resource dictionary, which shall enumerate the named resources needed by the operators in the content stream and the names by which they can be referred to | A content stream references named resource(s) %1 not defined by a resource dictionary |
| 6.2.3 | 1 | ICCOutputProfile | The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = "prtr") or a monitor profile (Device Class = "mntr"). The profiles shall have a colour space of either "GRAY", "RGB", or "CMYK" | The embedded PDF/A Output Intent colour profile has invalid header (Device Class = %1, color space = %2, version = %3) |
| 6.2.3 | 2 | OutputIntents | If any OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X or PDF/E, then all entries that contain a DestOutputProfile key shall have as the v... | OutputIntents array contains output intent dictionaries with non-matching destination output profiles (indirect keys %1) |
| 6.2.3 | 3 | PDOutputIntent | The DestOutputProfileRef key, as defined in ISO 32000-2:2020, 14.11.5, Table 401, shall not be present in any output intent dictionary | The output intent dictionary contains forbidden entry DestOutputProfileRef |
| 6.2.4.2 | 1 | ICCInputProfile | The profile that forms the stream of an ICCBased colour space shall conform to ISO 32000-2:2020, 8.6.5.5 | The embedded ICC profile (Device Class = %1, color space = %2, version = %3) is either invalid or does not satisfy PDF 2.0 requirements |
| 6.2.4.2 | 2 | PDICCBasedCMYK | Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and over... | Overprint mode (OPM) is set to %1 instead of 0 when an ICCBased CMYK colour space is used with enabled overprinting |
| 6.2.4.2 | 3 | PDICCBasedCMYK | An ICCBased colour space shall not be used where the profile is a CMYK destination profile and is identical to that in the current PDF/A OutputIntent or the current transparency blending colorspace | An ICCBased CMYK color space is identical to the current PDF/A OutputIntent color profile or the current transparency blending color space |
| 6.2.4.3 | 2 | PDDeviceRGB | DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used or if the current transparency blending space, when the DeviceRGB colour space is used, is a device independent RGB-b... | DeviceRGB colour space is used without RGB output intent profile and without current transparency blending space being Device-independent RGB |
| 6.2.4.3 | 3 | PDDeviceCMYK | DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or if the current transparency blending space, when the DeviceCMYK colour space is used, is a device independent C... | DeviceCMYK colour space is used without CMYK output intent profile and without current transparency blending space being Device-independent CMYK |
| 6.2.4.3 | 4 | PDDeviceGray | DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is in effect | DeviceGray colour space is used without output intent profile |
| 6.2.4.4 | 1 | PDDeviceN | For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present | A colorant of the DeviceN or NChannel color space is not defined in the Colorants dictionary |
| 6.2.4.4 | 2 | PDSeparation | All Separation arrays within a single conforming PDF/A-4 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, r... | Several occurrences of a Separation colour space with the same name are not consistent |
| 6.2.5 | 1 | PDExtGState | A graphics state parameter dictionary (ISO 32000-2:2020, 8.4.5) shall not contain the TR key | A graphics state parameter dictionary contains the TR key |
| 6.2.5 | 2 | PDExtGState | A graphics state parameter dictionary shall not contain the TR2 key with a value other than Default | A graphics state parameter dictionary contains the TR2 key with a value other than Default |
| 6.2.5 | 3 | PDExtGState | A graphics state parameter dictionary shall not contain the HTO key | A graphics state parameter dictionary contains the HTO key |
| 6.2.5 | 4 | PDHalftone | All halftones in a conforming PDF/A-4 file shall have the value 1 or 5 for the HalftoneType key | A Halftone has type %1 instead of 1 or 5 |
| 6.2.5 | 5 | PDHalftone | Halftones in a conforming PDF/A-4 file shall not contain a HalftoneName key | A Halftone dictionary contains the HalftoneName key |
| 6.2.5 | 6 | PDHalftone | The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-2:2020 | Custom TransferFunction in a Halftone dictionary is not permitted for primary (CMYK) colorants |
| 6.2.7.1 | 1 | PDXImage | An Image dictionary shall not contain the Alternates key | Alternates key is present in the Image dictionary |
| 6.2.7.1 | 2 | PDXImage | An Image dictionary shall not contain the OPI key | OPI key is present in the Image dictionary |
| 6.2.7.1 | 3 | PDXImage | If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key, if present, shall have a value of false | The value of the Interpolate key in the Image dictionary is true |
| 6.2.7.1 | 4 | PDXImage | If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16 | The value of the BitsPerComponent key in the Image dictionary is %1 |
| 6.2.7.1 | 5 | PDMaskImage | If an image mask dictionary contains the BitsPerComponent key, its value shall be 1 | The value of the BitsPerComponent key in the image mask dictionary is %1 |
| 6.2.7.3 | 1 | JPEG2000 | The number of colour channels in the JPEG2000 data shall be 1, 3 or 4 | JPEG2000 image has %1 colour channels that is neither 1, 3 or 4 |
| 6.2.7.3 | 2 | JPEG2000 | If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field | The JPEG2000 image contains %1 colour specifications with the best colour fidelity (value 0x01 in the APPROX field) |
| 6.2.7.3 | 3 | JPEG2000 | The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming processor shall use only that colour space and shall ignore all other colour space specifications | Invalid JPEG2000 image: the value of the METH entry (%1) in its 'colr' box is different from 0x01, 0x02 or 0x03 |
| 6.2.7.3 | 4 | JPEG2000 | JPEG2000 enumerated colour space 19 (CIEJab) shall not be used | JPEG2000 image uses enumerated colour space 19 (CIEJab), which is not allowed in PDF/A |
| 6.2.7.3 | 5 | JPEG2000 | The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth | JPEG2000 image has different bit-depth parameters in 'bpcc' box, or bit depth is out of range ('bpcc' box present = %1, bit depth = %2) |
| 6.2.8.1 | 1 | PDXForm | A form XObject dictionary shall not contain an OPI key | The form XObject dictionary contains an OPI key |
| 6.2.8.2 | 1 | PDXForm | A conforming file shall not contain any reference XObjects | The document contains a reference XObject (Ref key in the form XObject dictionary) |
| 6.2.9 | 1 | CosBM | Only blend modes that are specified in ISO 32000-2:2020 shall be used for the value of the BM key in a graphic state dictionary or an annotation dictionary | The document uses the blend mode %1 not defined in ISO 32000-2:2020 |
| 6.2.9 | 2 | PDPage | If the document does not contain a PDF/A output intent, then all pages that contain transparency shall either have a page-level PDF/A output intent or the page dictionary shall include the Group key, and the attribute dictionary that forms the val... | The page contains transparent objects with no blending colour space defined |
| 6.2.10.2 | 1 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Requi... | A Font dictionary has value %1 of Type entry instead of Font |
| 6.2.10.2 | 2 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Re... | Invalid Subtype entry in font dictionary with value %1 |
| 6.2.10.2 | 3 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (R... | A BaseFont entry is missing or has invalid type |
| 6.2.10.2 | 4 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer ... | A non-standard simple font dictionary has missing or invalid FirstChar entry |
| 6.2.10.2 | 5 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer -... | A non-standard simple font dictionary has missing or invalid LastChar entry |
| 6.2.10.2 | 6 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Re... | Widths array is missing or has invalid size %1 instead of %2 |
| 6.2.10.2 | 7 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 2.0 ar... | Unsupported font file format %1 of the embedded font |
| 6.2.10.3.1 | 1 | PDType0Font | For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary has a value of... | CIDSystemInfo entries the CIDFont and CMap dictionaries of a Type 0 font are not compatible (CIDSystemInfo Ordering = %1, CMap Ordering = %2, CIDSy... |
| 6.2.10.3.2 | 1 | PDCIDFont | ISO 32000-2:2020, 9.7.4 Table 115 requires that all embedded Type 2 CIDFonts, the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-2:2020... | A Type 2 CIDFont dictionary has missing or invalid CIDToGIDMap entry |
| 6.2.10.3.3 | 1 | PDCMap | All CMaps used within a conforming PDF/A-4 file, except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116, shall be embedded in that file as described in ISO 32000-2:2020, 9.7.5 | A non-standard CMap %1 is not embedded |
| 6.2.10.3.3 | 2 | CMapFile | For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream | WMode entry (value %1) in the embedded CMap and in the CMap dictionary (value %2) are not identical |
| 6.2.10.3.3 | 3 | PDReferencedCMap | A CMap shall not reference any other CMap except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116 | A CMap references another non-standard CMap %1 |
| 6.2.10.4.1 | 1 | PDFont | The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-2:2020, 9.9 | The font program is not embedded |
| 6.2.10.4.1 | 2 | Glyph | Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType... | Not all glyphs referenced for rendering are present in the embedded font program |
| 6.2.10.5 | 1 | Glyph | For every font embedded in a conforming file, the glyph width information in the font dictionary and in the embedded font program shall be consistent for every glyph referenced for rendering. Glyphs that are referenced only with rendering mode 3 a... | Glyph width %1 in the embedded font program is not consistent with the Widths entry of the font dictionary (value %2) |
| 6.2.10.6 | 1 | TrueTypeFontProgram | For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain at least Microsoft Unicode (3,1 – Platform ID=3, Encoding ID=1), or Macintosh Roman (1,0 – Platform ID=1, Encoding ID=0) 'cmap' subtable that ... | The embedded font program for a non-symbolic TrueType font does not contain Microsoft Symbol (3,1 – Platform ID=3, Encoding ID=1) or the Mac Roman ... |
| 6.2.10.6 | 2 | PDTrueTypeFont | All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the... | A non-symbolic TrueType font encoding does not define a correct mapping to the Adobe Glyph List (Encoding = %1, Encoding entry contains a Differenc... |
| 6.2.10.6 | 3 | PDTrueTypeFont | Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary | A symbolic TrueType font specifies an Encoding entry in its dictionary |
| 6.2.10.6 | 4 | TrueTypeFontProgram | The 'cmap' subtable in the embedded font program for a symbolic TrueType font shall contain either the Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0 – Platform ID=1, Encoding ID=0) encoding | The embedded font program for a symbolic TrueType font does not contain Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0... |
| 6.2.10.7 | 1 | Glyph | If a ToUnicode CMap is present, the Unicode values specified there shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE | The glyph has Unicode value 0, U+FEFF or U+FFFE, which is invalid by Unicode standard |
| 6.2.10.8 | 1 | CosActualText | The ActualText entry shall not contain any PUA values | The ActualText entry contains Unicode PUA (Private Use Area) code points |
| 6.2.10.9 | 1 | Glyph | A PDF/A-4 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream | The document contains a reference to the .notdef glyph |
### Clause 6.3

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.3.1 | 1 | PDAnnot | Annotation types not defined in ISO 32000-2:2020, 12.5.6.1, Table 171 shall not be permitted. Additionally, the Sound, Screen, Movie, 3D, RichMedia and FileAttachment types shall not be permitted | Unknown or not permitted Annotation type %1 |
| 6.3.2 | 1 | PDAnnot | Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key | A dictionary of %1 annotation does not contain F key |
| 6.3.2 | 2 | PDAnnot | If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0 | Annotation flags are set the annotation to be hidden/invisible or non-printable (F = %1, Print = %2, Hidden = %3, Invisible = %4, NoView = %5, Togg... |
| 6.3.3 | 1 | PDAnnot | Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: Annotations where the value of the Rect key consists of an array where... | An annotation does not contain an appearance dictionary |
| 6.3.3 | 2 | PDAnnot | For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key | Annotation's appearance dictionary contains entries %1 other than N |
| 6.3.3 | 3 | PDAnnot | If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary | An annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, but the value of the N key is %1 instead of appeara... |
| 6.3.3 | 4 | PDAnnot | If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream | An annotation dictionary's Subtype key has a value %1 and its FT key has a value %2, but the value of the N key is not an appearance stream |
### Clause 6.4

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.4.1 | 1 | PDWidgetAnnot | A Widget annotation dictionary shall not contain the A key | A Widget annotation contains A entry |
| 6.4.1 | 2 | PDAcroForm | The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false | The interactive form dictionary contains the NeedAppearances flag with value true |
| 6.4.2 | 1 | PDAcroForm | The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-4 file, if present, shall not contain the XFA key | The interactive form dictionary contains the XFA key |
| 6.4.2 | 2 | CosDocument | A document's Catalog shall not contain the NeedsRendering key | A document's Catalog contains NeedsRendering flag set to true |
### Clause 6.6

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.6.1 | 1 | PDAction | The Launch, Sound, Movie, ResetForm, ImportData, Hide, Rendition, Trans, SetOCGState and GoTo3DView actions shall not be permitted. Additionally, the obsoleted set-state and no-op actions shall not be permitted | Unknown or not permitted Action type %1 |
| 6.6.1 | 2 | PDNamedAction | Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted | Unknown or not permitted named action %1 |
| 6.6.3 | 1 | PDAdditionalActions | If a document catalog dictionary or page dictionary or an annotation dictionary (other than a Widget annotation dictionary) include an AA entry, its value (which is an additional-actions dictionary) shall only contain keys from the following list:... | Additional-actions dictionary contains key(s) %1 not from the following permitted list: E, X, D, U, Fo, and Bl |
### Clause 6.7

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.7.2.1 | 1 | PDDocument | The document catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-2:2020, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Sub... | The document catalog dictionary doesn't contain metadata key or metadata stream dictionary does not contain either entry Type with value /Metadata ... |
| 6.7.2.1 | 2 | XMPPackage | The bytes attribute shall not be used in the header of an XMP packet | The XMP Package contains bytes attribute |
| 6.7.2.1 | 3 | XMPPackage | The encoding attribute shall not be used in the header of an XMP packet | The XMP Package contains encoding attribute |
| 6.7.2.1 | 4 | XMPPackage | All content of all XMP packets located in any metadata stream present in the PDF shall be well-formed as defined by XMP (ISO 16684-1) | A metadata stream is serialized incorrectly and can not be parsed |
| 6.7.2.1 | 5 | XMPPackage | All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8 | The XMP package uses encoding %1 different from UTF-8 |
| 6.7.3 | 1 | MainXMPPackage | The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema. The PDF/A Identification schema defined in Table 2 uses the namespace URI "http://www.aiim.org/pdfa/ns/id/" | The document metadata stream doesn't contain PDF/A Identification Schema |
| 6.7.3 | 2 | PDFAIdentification | The value of "pdfaid:part" shall be the part number of ISO 19005 to which the file conforms | The "part" property of the PDF/A Identification Schema is %1 instead of 4 for PDF/A-4 conforming file |
| 6.7.3 | 3 | PDFAIdentification | A file that does not conform to either PDF/A-4e or PDF/A-4f shall not provide any "pdfaid:conformance" | The "conformance" property of the PDF/A Identification Schema is %1 instead of null for PDF/A-4 conforming file |
| 6.7.3 | 4 | PDFAIdentification | Property "part" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "part" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.7.3 | 5 | PDFAIdentification | The value of "pdfaid:rev" shall be "2020" | The value of "pdfaid:rev" (%1) is not "2020" |
| 6.7.3 | 6 | PDFAIdentification | Property "rev" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "rev" of the PDF/A Identification Schema has invalid namespace prefix %1 |
### Clause 6.9

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.9 | 1 | EmbeddedFile | The embedded file stream dictionary shall include a valid MIME type value for the Subtype key. If the MIME type is not known, the value "application/octet-stream" shall be used | MIME type %1 of an embedded file is missing or invalid |
| 6.9 | 2 | CosFileSpecification | The file specification dictionary for an embedded file shall contain the F and UF keys | The file specification dictionary for an embedded file does not contain either F or UF key (F = %1, UF = %2) |
| 6.9 | 3 | EmbeddedFile | All of the embedded files shall be compliant with ISO 19005-1, 19005-2 or 19005-4 | An embedded file does not comply to either ISO 19005-1, ISO 19005-2 or ISO 19005-4 |
| 6.9 | 4 | CosFileSpecification | Each embedded file’s file specification dictionary shall contain an AFRelationship key (ISO 32000-2, 7.11.3) that describes how this embedded file relates to the content of the PDF | The file specification dictionary for an embedded file does not contain the AFRelationship key |
### Clause 6.10

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.10 | 1 | PDOCConfig | Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key | Missing Name entry of the optional content configuration dictionary |
| 6.10 | 2 | PDOCConfig | Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-4 file | Optional content configuration dictionary has duplicated name %1 |
| 6.10 | 3 | PDOCConfig | If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file | Optional content group(s) %1 not present in the Order entry of the optional content configuration dictionary |
### Clause 6.11

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.11 | 1 | PDDocument | There shall be no AlternatePresentations entry in the document's name dictionary | The document's name dictionary contains the AlternatePresentations entry |
| 6.11 | 2 | PDPage | There shall be no PresSteps entry in any Page dictionary | A Page dictionary contains the PresSteps entry |
### Clause 6.12

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.12 | 1 | CosDocument | The document catalog shall not contain the Requirements key | The document catalog contains the Requirements key |

## PDF/A-4e

**PDF/A-4E validation profile**
*Validation rules against ISO 19005-4:2020, Level E*
**Total rules: 109**

### Clause 6.1

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.1.2 | 1 | CosDocument | The file header shall begin at byte zero and shall consist of "%PDF-2.n" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 9 (39h) | File header %1 (offset = %2) starts at non-zero offset or does not match the pattern %PDF-2.n, where 'n' is a single digit number between 0 and 9 |
| 6.1.2 | 2 | CosDocument | The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127 | Binary comment in the file header is missing or does not start with 4 bytes with byte values above 127 (first four bytes = %1, %2, %3, %4) |
| 6.1.3 | 1 | CosDocument | File identifiers shall be defined by the ID entry in a PDF file’s trailer dictionary | Missing or empty ID in the document trailer |
| 6.1.3 | 2 | CosTrailer | The Encrypt key shall not be present in the trailer dictionary | The Encrypt key is present in the trailer dictionary |
| 6.1.3 | 3 | CosDocument | No data shall follow the last end-of-file marker as described in ISO 32000-2:2020, 7.5.5 | %1 byte(s) of data is present after the last end-of-file marker |
| 6.1.3 | 4 | CosDocument | The Info key shall not be present in the trailer dictionary of PDF/A-4 conforming files unless there exists a PieceInfo entry in the document catalog dictionary | The Info key present in the trailer dictionary, but PieceInfo entry does not present in the document catalog dictionary |
| 6.1.3 | 5 | CosInfo | If a document information dictionary is present, it shall only contain a ModDate entry | Document information dictionary is present and contains %1 key(s), but allowed only ModDate |
| 6.1.4 | 1 | CosXRef | The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker | Extra spacings or missing EOL characters after the 'xref' keyword in the cross reference table |
| 6.1.5 | 1 | CosString | Hexadecimal strings shall contain an even number of non-white-space characters | A hexadecimal string contains odd number (%1) of non-white-space characters |
| 6.1.5 | 2 | CosString | A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f) | Hexadecimal string contains non-white-space characters outside the range 0 to 9, A to F or a to f |
| 6.1.6.1 | 1 | CosStream | The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword | Actual length of the stream (%1 byte(s)) does not match the value of the Length key in the Stream dictionary (%2 byte(s)) |
| 6.1.6.1 | 2 | CosStream | A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys | A stream object dictionary contains %1 key(s) |
| 6.1.6.1 | 3 | PD3DStream | The Subtype entry in a 3D stream dictionary (ISO 32000-2:2020, 13.6.3) shall have a value which is either U3D or PRC as described in Annex B | The Subtype entry in the 3D stream dictionary (ISO 32000-2:2020, 13.6.3) has value %1 that is neither U3D nor PRC |
| 6.1.6.2 | 1 | CosFilter | All standard stream filters listed in ISO 32000-2:2020, 7.4, Table 6 may be used, with the exception of LZWDecode. Filters that are not listed in ISO 32000-2:2020, 7.4, Table 6 shall not be used. In addition, the Crypt filter shall not be used unl... | Unknown or not permitted Stream filter %1 is used |
| 6.1.7 | 1 | CosUnicodeName | Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences | The name value %1 does not represent a correct UTF-8 character sequence |
| 6.1.8 | 1 | CosIndirect | The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded b... | Extra spacings or missing EOL characters around indirect object/generation number or keywords 'obj' and 'endobj' |
| 6.1.9 | 1 | CosIIFilter | The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-2:2020, 8.9.7, Table 92, or an array containing any such value | Inline image uses not permitted or unknown filter %1 |
| 6.1.11 | 1 | PDPerms | No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-2:2020, 12.8.6, Table 263) | The document permissions dictionary contains key(s) %1 other than UR3 and DocMDP |
| 6.1.12 | 1 | PDDocument | If the Version key is present in the document catalog dictionary, the first character in its value shall be a 2 (32h) and the second character of its value shall be a PERIOD (2Eh) (decimal point). The third character shall be a decimal digit. The ... | The Version key with value %1 does not match the pattern 2.n, where 'n' is a single digit number between 0 and 9 |
### Clause 6.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.2.2 | 1 | Op_Undefined | Content streams shall not contain any operators not defined in ISO 32000-2:2020 even if such operators are bracketed by the BX/EX compatibility operators | A content stream contains operator %1 not defined in ISO 32000-2:2020 |
| 6.2.2 | 2 | PDContentStream | A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-2:2020, 7.8.3. Such resource dict... | A content stream refers to resource(s) %1 not defined in an explicitly associated Resources dictionary |
| 6.2.2 | 3 | PDContentStream | A content stream's named resources shall be defined by a resource dictionary, which shall enumerate the named resources needed by the operators in the content stream and the names by which they can be referred to | A content stream references named resource(s) %1 not defined by a resource dictionary |
| 6.2.3 | 1 | ICCOutputProfile | The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = "prtr") or a monitor profile (Device Class = "mntr"). The profiles shall have a colour space of either "GRAY", "RGB", or "CMYK" | The embedded PDF/A Output Intent colour profile has invalid header (Device Class = %1, color space = %2, version = %3) |
| 6.2.3 | 2 | OutputIntents | If any OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X or PDF/E, then all entries that contain a DestOutputProfile key shall have as the v... | OutputIntents array contains output intent dictionaries with non-matching destination output profiles (indirect keys %1) |
| 6.2.3 | 3 | PDOutputIntent | The DestOutputProfileRef key, as defined in ISO 32000-2:2020, 14.11.5, Table 401, shall not be present in any output intent dictionary | The output intent dictionary contains forbidden entry DestOutputProfileRef |
| 6.2.4.2 | 1 | ICCInputProfile | The profile that forms the stream of an ICCBased colour space shall conform to ISO 32000-2:2020, 8.6.5.5 | The embedded ICC profile (Device Class = %1, color space = %2, version = %3) is either invalid or does not satisfy PDF 2.0 requirements |
| 6.2.4.2 | 2 | PDICCBasedCMYK | Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and over... | Overprint mode (OPM) is set to %1 instead of 0 when an ICCBased CMYK colour space is used with enabled overprinting |
| 6.2.4.2 | 3 | PDICCBasedCMYK | An ICCBased colour space shall not be used where the profile is a CMYK destination profile and is identical to that in the current PDF/A OutputIntent or the current transparency blending colorspace | An ICCBased CMYK color space is identical to the current PDF/A OutputIntent color profile or the current transparency blending color space |
| 6.2.4.3 | 2 | PDDeviceRGB | DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used or if the current transparency blending space, when the DeviceRGB colour space is used, is a device independent RGB-b... | DeviceRGB colour space is used without RGB output intent profile and without current transparency blending space being Device-independent RGB |
| 6.2.4.3 | 3 | PDDeviceCMYK | DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or if the current transparency blending space, when the DeviceCMYK colour space is used, is a device independent C... | DeviceCMYK colour space is used without CMYK output intent profile and without current transparency blending space being Device-independent CMYK |
| 6.2.4.3 | 4 | PDDeviceGray | DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is in effect | DeviceGray colour space is used without output intent profile |
| 6.2.4.4 | 1 | PDDeviceN | For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present | A colorant of the DeviceN or NChannel color space is not defined in the Colorants dictionary |
| 6.2.4.4 | 2 | PDSeparation | All Separation arrays within a single conforming PDF/A-4 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, r... | Several occurrences of a Separation colour space with the same name are not consistent |
| 6.2.5 | 1 | PDExtGState | A graphics state parameter dictionary (ISO 32000-2:2020, 8.4.5) shall not contain the TR key | A graphics state parameter dictionary contains the TR key |
| 6.2.5 | 2 | PDExtGState | A graphics state parameter dictionary shall not contain the TR2 key with a value other than Default | A graphics state parameter dictionary contains the TR2 key with a value other than Default |
| 6.2.5 | 3 | PDExtGState | A graphics state parameter dictionary shall not contain the HTO key | A graphics state parameter dictionary contains the HTO key |
| 6.2.5 | 4 | PDHalftone | All halftones in a conforming PDF/A-4 file shall have the value 1 or 5 for the HalftoneType key | A Halftone has type %1 instead of 1 or 5 |
| 6.2.5 | 5 | PDHalftone | Halftones in a conforming PDF/A-4 file shall not contain a HalftoneName key | A Halftone dictionary contains the HalftoneName key |
| 6.2.5 | 6 | PDHalftone | The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-2:2020 | Custom TransferFunction in a Halftone dictionary is not permitted for primary (CMYK) colorants |
| 6.2.7.1 | 1 | PDXImage | An Image dictionary shall not contain the Alternates key | Alternates key is present in the Image dictionary |
| 6.2.7.1 | 2 | PDXImage | An Image dictionary shall not contain the OPI key | OPI key is present in the Image dictionary |
| 6.2.7.1 | 3 | PDXImage | If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key, if present, shall have a value of false | The value of the Interpolate key in the Image dictionary is true |
| 6.2.7.1 | 4 | PDXImage | If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16 | The value of the BitsPerComponent key in the Image dictionary is %1 |
| 6.2.7.1 | 5 | PDMaskImage | If an image mask dictionary contains the BitsPerComponent key, its value shall be 1 | The value of the BitsPerComponent key in the image mask dictionary is %1 |
| 6.2.7.3 | 1 | JPEG2000 | The number of colour channels in the JPEG2000 data shall be 1, 3 or 4 | JPEG2000 image has %1 colour channels that is neither 1, 3 or 4 |
| 6.2.7.3 | 2 | JPEG2000 | If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field | The JPEG2000 image contains %1 colour specifications with the best colour fidelity (value 0x01 in the APPROX field) |
| 6.2.7.3 | 3 | JPEG2000 | The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming processor shall use only that colour space and shall ignore all other colour space specifications | Invalid JPEG2000 image: the value of the METH entry (%1) in its 'colr' box is different from 0x01, 0x02 or 0x03 |
| 6.2.7.3 | 4 | JPEG2000 | JPEG2000 enumerated colour space 19 (CIEJab) shall not be used | JPEG2000 image uses enumerated colour space 19 (CIEJab), which is not allowed in PDF/A |
| 6.2.7.3 | 5 | JPEG2000 | The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth | JPEG2000 image has different bit-depth parameters in 'bpcc' box, or bit depth is out of range ('bpcc' box present = %1, bit depth = %2) |
| 6.2.8.1 | 1 | PDXForm | A form XObject dictionary shall not contain an OPI key | The form XObject dictionary contains an OPI key |
| 6.2.8.2 | 1 | PDXForm | A conforming file shall not contain any reference XObjects | The document contains a reference XObject (Ref key in the form XObject dictionary) |
| 6.2.9 | 1 | CosBM | Only blend modes that are specified in ISO 32000-2:2020 shall be used for the value of the BM key in a graphic state dictionary or an annotation dictionary | The document uses the blend mode %1 not defined in ISO 32000-2:2020 |
| 6.2.9 | 2 | PDPage | If the document does not contain a PDF/A output intent, then all pages that contain transparency shall either have a page-level PDF/A output intent or the page dictionary shall include the Group key, and the attribute dictionary that forms the val... | The page contains transparent objects with no blending colour space defined |
| 6.2.10.2 | 1 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Requi... | A Font dictionary has value %1 of Type entry instead of Font |
| 6.2.10.2 | 2 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Re... | Invalid Subtype entry in font dictionary with value %1 |
| 6.2.10.2 | 3 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (R... | A BaseFont entry is missing or has invalid type |
| 6.2.10.2 | 4 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer ... | A non-standard simple font dictionary has missing or invalid FirstChar entry |
| 6.2.10.2 | 5 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer -... | A non-standard simple font dictionary has missing or invalid LastChar entry |
| 6.2.10.2 | 6 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Re... | Widths array is missing or has invalid size %1 instead of %2 |
| 6.2.10.2 | 7 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 2.0 ar... | Unsupported font file format %1 of the embedded font |
| 6.2.10.3.1 | 1 | PDType0Font | For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary has a value of... | CIDSystemInfo entries the CIDFont and CMap dictionaries of a Type 0 font are not compatible (CIDSystemInfo Ordering = %1, CMap Ordering = %2, CIDSy... |
| 6.2.10.3.2 | 1 | PDCIDFont | ISO 32000-2:2020, 9.7.4 Table 115 requires that all embedded Type 2 CIDFonts, the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-2:2020... | A Type 2 CIDFont dictionary has missing or invalid CIDToGIDMap entry |
| 6.2.10.3.3 | 1 | PDCMap | All CMaps used within a conforming PDF/A-4 file, except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116, shall be embedded in that file as described in ISO 32000-2:2020, 9.7.5 | A non-standard CMap %1 is not embedded |
| 6.2.10.3.3 | 2 | CMapFile | For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream | WMode entry (value %1) in the embedded CMap and in the CMap dictionary (value %2) are not identical |
| 6.2.10.3.3 | 3 | PDReferencedCMap | A CMap shall not reference any other CMap except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116 | A CMap references another non-standard CMap %1 |
| 6.2.10.4.1 | 1 | PDFont | The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-2:2020, 9.9 | The font program is not embedded |
| 6.2.10.4.1 | 2 | Glyph | Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType... | Not all glyphs referenced for rendering are present in the embedded font program |
| 6.2.10.5 | 1 | Glyph | For every font embedded in a conforming file, the glyph width information in the font dictionary and in the embedded font program shall be consistent for every glyph referenced for rendering. Glyphs that are referenced only with rendering mode 3 a... | Glyph width %1 in the embedded font program is not consistent with the Widths entry of the font dictionary (value %2) |
| 6.2.10.6 | 1 | TrueTypeFontProgram | For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain at least Microsoft Unicode (3,1 – Platform ID=3, Encoding ID=1), or Macintosh Roman (1,0 – Platform ID=1, Encoding ID=0) 'cmap' subtable that ... | The embedded font program for a non-symbolic TrueType font does not contain Microsoft Symbol (3,1 – Platform ID=3, Encoding ID=1) or the Mac Roman ... |
| 6.2.10.6 | 2 | PDTrueTypeFont | All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the... | A non-symbolic TrueType font encoding does not define a correct mapping to the Adobe Glyph List (Encoding = %1, Encoding entry contains a Differenc... |
| 6.2.10.6 | 3 | PDTrueTypeFont | Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary | A symbolic TrueType font specifies an Encoding entry in its dictionary |
| 6.2.10.6 | 4 | TrueTypeFontProgram | The 'cmap' subtable in the embedded font program for a symbolic TrueType font shall contain either the Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0 – Platform ID=1, Encoding ID=0) encoding | The embedded font program for a symbolic TrueType font does not contain Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0... |
| 6.2.10.7 | 1 | Glyph | If a ToUnicode CMap is present, the Unicode values specified there shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE | The glyph has Unicode value 0, U+FEFF or U+FFFE, which is invalid by Unicode standard |
| 6.2.10.8 | 1 | CosActualText | The ActualText entry shall not contain any PUA values | The ActualText entry contains Unicode PUA (Private Use Area) code points |
| 6.2.10.9 | 1 | Glyph | A PDF/A-4 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream | The document contains a reference to the .notdef glyph |
### Clause 6.3

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.3.1 | 1 | PDAnnot | Annotation types not defined in ISO 32000-2:2020, 12.5.6.1, Table 171 shall not be permitted. Additionally, the Sound, Screen, Movie and FileAttachment types shall not be permitted. 3D and RichMedia types shall only be permitted in a PDF/A-4e comp... | Unknown or not permitted Annotation type %1 |
| 6.3.2 | 1 | PDAnnot | Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key | A dictionary of %1 annotation does not contain F key |
| 6.3.2 | 2 | PDAnnot | If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0 | Annotation flags are set the annotation to be hidden/invisible or non-printable (F = %1, Print = %2, Hidden = %3, Invisible = %4, NoView = %5, Togg... |
| 6.3.3 | 1 | PDAnnot | Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: Annotations where the value of the Rect key consists of an array where... | An annotation does not contain an appearance dictionary |
| 6.3.3 | 2 | PDAnnot | For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key | Annotation's appearance dictionary contains entries %1 other than N |
| 6.3.3 | 3 | PDAnnot | If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary | An annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, but the value of the N key is %1 instead of appeara... |
| 6.3.3 | 4 | PDAnnot | If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream | An annotation dictionary's Subtype key has a value %1 and its FT key has a value %2, but the value of the N key is not an appearance stream |
### Clause 6.4

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.4.1 | 1 | PDWidgetAnnot | A Widget annotation dictionary shall not contain the A key | A Widget annotation contains A entry |
| 6.4.1 | 2 | PDAcroForm | The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false | The interactive form dictionary contains the NeedAppearances flag with value true |
| 6.4.2 | 1 | PDAcroForm | The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-4 file, if present, shall not contain the XFA key | The interactive form dictionary contains the XFA key |
| 6.4.2 | 2 | CosDocument | A document's Catalog shall not contain the NeedsRendering key | A document's Catalog contains NeedsRendering flag set to true |
### Clause 6.6

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.6.1 | 1 | PDAction | The Launch, Sound, Movie, ResetForm, ImportData, Hide, Rendition and Trans actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted. The SetOCGState and GoTo3DView actions shall only be permit... | Unknown or not permitted Action type %1 |
| 6.6.1 | 2 | PDNamedAction | Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted | Unknown or not permitted named action %1 |
| 6.6.3 | 1 | PDAdditionalActions | If a document catalog dictionary or page dictionary or an annotation dictionary (other than a Widget annotation dictionary) include an AA entry, its value (which is an additional-actions dictionary) shall only contain keys from the following list:... | Additional-actions dictionary contains key(s) %1 not from the following permitted list: E, X, D, U, Fo, and Bl |
### Clause 6.7

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.7.2.1 | 1 | PDDocument | The document catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-2:2020, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Sub... | The document catalog dictionary doesn't contain metadata key or metadata stream dictionary does not contain either entry Type with value /Metadata ... |
| 6.7.2.1 | 2 | XMPPackage | The bytes attribute shall not be used in the header of an XMP packet | The XMP Package contains bytes attribute |
| 6.7.2.1 | 3 | XMPPackage | The encoding attribute shall not be used in the header of an XMP packet | The XMP Package contains encoding attribute |
| 6.7.2.1 | 4 | XMPPackage | All content of all XMP packets located in any metadata stream present in the PDF shall be well-formed as defined by XMP (ISO 16684-1) | A metadata stream is serialized incorrectly and can not be parsed |
| 6.7.2.1 | 5 | XMPPackage | All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8 | The XMP package uses encoding %1 different from UTF-8 |
| 6.7.3 | 1 | MainXMPPackage | The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema. The PDF/A Identification schema defined in Table 2 uses the namespace URI "http://www.aiim.org/pdfa/ns/id/" | The document metadata stream doesn't contain PDF/A Identification Schema |
| 6.7.3 | 2 | PDFAIdentification | The value of "pdfaid:part" shall be the part number of ISO 19005 to which the file conforms | The "part" property of the PDF/A Identification Schema is %1 instead of 4 for PDF/A-4 conforming file |
| 6.7.3 | 3 | PDFAIdentification | A PDF/A-4e conforming file (as described in Annex B) shall specify the value of "pdfaid:conformance" as E | The "conformance" property of the PDF/A Identification Schema is %1 instead of "E" for PDF/A-4E conforming file |
| 6.7.3 | 4 | PDFAIdentification | Property "part" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "part" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.7.3 | 5 | PDFAIdentification | The value of "pdfaid:rev" shall be "2020" | The value of "pdfaid:rev" (%1) is not "2020" |
| 6.7.3 | 6 | PDFAIdentification | Property "rev" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "rev" of the PDF/A Identification Schema has invalid namespace prefix %1 |
### Clause 6.9

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.9 | 1 | EmbeddedFile | The embedded file stream dictionary shall include a valid MIME type value for the Subtype key. If the MIME type is not known, the value "application/octet-stream" shall be used | MIME type %1 of an embedded file is missing or invalid |
| 6.9 | 2 | CosFileSpecification | The file specification dictionary for an embedded file shall contain the F and UF keys | The file specification dictionary for an embedded file does not contain either F or UF key (F = %1, UF = %2) |
| 6.9 | 4 | CosFileSpecification | Each embedded file’s file specification dictionary shall contain an AFRelationship key (ISO 32000-2, 7.11.3) that describes how this embedded file relates to the content of the PDF | The file specification dictionary for an embedded file does not contain the AFRelationship key |
### Clause 6.10

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.10 | 1 | PDOCConfig | Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key | Missing Name entry of the optional content configuration dictionary |
| 6.10 | 2 | PDOCConfig | Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-4 file | Optional content configuration dictionary has duplicated name %1 |
| 6.10 | 3 | PDOCConfig | If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file | Optional content group(s) %1 not present in the Order entry of the optional content configuration dictionary |
### Clause 6.11

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.11 | 1 | PDDocument | There shall be no AlternatePresentations entry in the document's name dictionary | The document's name dictionary contains the AlternatePresentations entry |
| 6.11 | 2 | PDPage | There shall be no PresSteps entry in any Page dictionary | A Page dictionary contains the PresSteps entry |
### Clause 6.12

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.12 | 1 | CosDocument | The document catalog shall not contain the Requirements key | The document catalog contains the Requirements key |

## PDF/A-4f

**PDF/A-4F validation profile**
*Validation rules against ISO 19005-4:2020, Level F*
**Total rules: 109**

### Clause 6.1

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.1.2 | 1 | CosDocument | The file header shall begin at byte zero and shall consist of "%PDF-2.n" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 9 (39h) | File header %1 (offset = %2) starts at non-zero offset or does not match the pattern %PDF-2.n, where 'n' is a single digit number between 0 and 9 |
| 6.1.2 | 2 | CosDocument | The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127 | Binary comment in the file header is missing or does not start with 4 bytes with byte values above 127 (first four bytes = %1, %2, %3, %4) |
| 6.1.3 | 1 | CosDocument | File identifiers shall be defined by the ID entry in a PDF file’s trailer dictionary | Missing or empty ID in the document trailer |
| 6.1.3 | 2 | CosTrailer | The Encrypt key shall not be present in the trailer dictionary | The Encrypt key is present in the trailer dictionary |
| 6.1.3 | 3 | CosDocument | No data shall follow the last end-of-file marker as described in ISO 32000-2:2020, 7.5.5 | %1 byte(s) of data is present after the last end-of-file marker |
| 6.1.3 | 4 | CosDocument | The Info key shall not be present in the trailer dictionary of PDF/A-4 conforming files unless there exists a PieceInfo entry in the document catalog dictionary | The Info key present in the trailer dictionary, but PieceInfo entry does not present in the document catalog dictionary |
| 6.1.3 | 5 | CosInfo | If a document information dictionary is present, it shall only contain a ModDate entry | Document information dictionary is present and contains %1 key(s), but allowed only ModDate |
| 6.1.4 | 1 | CosXRef | The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker | Extra spacings or missing EOL characters after the 'xref' keyword in the cross reference table |
| 6.1.5 | 1 | CosString | Hexadecimal strings shall contain an even number of non-white-space characters | A hexadecimal string contains odd number (%1) of non-white-space characters |
| 6.1.5 | 2 | CosString | A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f) | Hexadecimal string contains non-white-space characters outside the range 0 to 9, A to F or a to f |
| 6.1.6.1 | 1 | CosStream | The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword | Actual length of the stream (%1 byte(s)) does not match the value of the Length key in the Stream dictionary (%2 byte(s)) |
| 6.1.6.1 | 2 | CosStream | A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys | A stream object dictionary contains %1 key(s) |
| 6.1.6.2 | 1 | CosFilter | All standard stream filters listed in ISO 32000-2:2020, 7.4, Table 6 may be used, with the exception of LZWDecode. Filters that are not listed in ISO 32000-2:2020, 7.4, Table 6 shall not be used. In addition, the Crypt filter shall not be used unl... | Unknown or not permitted Stream filter %1 is used |
| 6.1.7 | 1 | CosUnicodeName | Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences | The name value %1 does not represent a correct UTF-8 character sequence |
| 6.1.8 | 1 | CosIndirect | The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded b... | Extra spacings or missing EOL characters around indirect object/generation number or keywords 'obj' and 'endobj' |
| 6.1.9 | 1 | CosIIFilter | The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-2:2020, 8.9.7, Table 92, or an array containing any such value | Inline image uses not permitted or unknown filter %1 |
| 6.1.11 | 1 | PDPerms | No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-2:2020, 12.8.6, Table 263) | The document permissions dictionary contains key(s) %1 other than UR3 and DocMDP |
| 6.1.12 | 1 | PDDocument | If the Version key is present in the document catalog dictionary, the first character in its value shall be a 2 (32h) and the second character of its value shall be a PERIOD (2Eh) (decimal point). The third character shall be a decimal digit. The ... | The Version key with value %1 does not match the pattern 2.n, where 'n' is a single digit number between 0 and 9 |
### Clause 6.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.2.2 | 1 | Op_Undefined | Content streams shall not contain any operators not defined in ISO 32000-2:2020 even if such operators are bracketed by the BX/EX compatibility operators | A content stream contains operator %1 not defined in ISO 32000-2:2020 |
| 6.2.2 | 2 | PDContentStream | A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-2:2020, 7.8.3. Such resource dict... | A content stream refers to resource(s) %1 not defined in an explicitly associated Resources dictionary |
| 6.2.2 | 3 | PDContentStream | A content stream's named resources shall be defined by a resource dictionary, which shall enumerate the named resources needed by the operators in the content stream and the names by which they can be referred to | A content stream references named resource(s) %1 not defined by a resource dictionary |
| 6.2.3 | 1 | ICCOutputProfile | The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = "prtr") or a monitor profile (Device Class = "mntr"). The profiles shall have a colour space of either "GRAY", "RGB", or "CMYK" | The embedded PDF/A Output Intent colour profile has invalid header (Device Class = %1, color space = %2, version = %3) |
| 6.2.3 | 2 | OutputIntents | If any OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X or PDF/E, then all entries that contain a DestOutputProfile key shall have as the v... | OutputIntents array contains output intent dictionaries with non-matching destination output profiles (indirect keys %1) |
| 6.2.3 | 3 | PDOutputIntent | The DestOutputProfileRef key, as defined in ISO 32000-2:2020, 14.11.5, Table 401, shall not be present in any output intent dictionary | The output intent dictionary contains forbidden entry DestOutputProfileRef |
| 6.2.4.2 | 1 | ICCInputProfile | The profile that forms the stream of an ICCBased colour space shall conform to ISO 32000-2:2020, 8.6.5.5 | The embedded ICC profile (Device Class = %1, color space = %2, version = %3) is either invalid or does not satisfy PDF 2.0 requirements |
| 6.2.4.2 | 2 | PDICCBasedCMYK | Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and over... | Overprint mode (OPM) is set to %1 instead of 0 when an ICCBased CMYK colour space is used with enabled overprinting |
| 6.2.4.2 | 3 | PDICCBasedCMYK | An ICCBased colour space shall not be used where the profile is a CMYK destination profile and is identical to that in the current PDF/A OutputIntent or the current transparency blending colorspace | An ICCBased CMYK color space is identical to the current PDF/A OutputIntent color profile or the current transparency blending color space |
| 6.2.4.3 | 2 | PDDeviceRGB | DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used or if the current transparency blending space, when the DeviceRGB colour space is used, is a device independent RGB-b... | DeviceRGB colour space is used without RGB output intent profile and without current transparency blending space being Device-independent RGB |
| 6.2.4.3 | 3 | PDDeviceCMYK | DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or if the current transparency blending space, when the DeviceCMYK colour space is used, is a device independent C... | DeviceCMYK colour space is used without CMYK output intent profile and without current transparency blending space being Device-independent CMYK |
| 6.2.4.3 | 4 | PDDeviceGray | DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is in effect | DeviceGray colour space is used without output intent profile |
| 6.2.4.4 | 1 | PDDeviceN | For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present | A colorant of the DeviceN or NChannel color space is not defined in the Colorants dictionary |
| 6.2.4.4 | 2 | PDSeparation | All Separation arrays within a single conforming PDF/A-4 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, r... | Several occurrences of a Separation colour space with the same name are not consistent |
| 6.2.5 | 1 | PDExtGState | A graphics state parameter dictionary (ISO 32000-2:2020, 8.4.5) shall not contain the TR key | A graphics state parameter dictionary contains the TR key |
| 6.2.5 | 2 | PDExtGState | A graphics state parameter dictionary shall not contain the TR2 key with a value other than Default | A graphics state parameter dictionary contains the TR2 key with a value other than Default |
| 6.2.5 | 3 | PDExtGState | A graphics state parameter dictionary shall not contain the HTO key | A graphics state parameter dictionary contains the HTO key |
| 6.2.5 | 4 | PDHalftone | All halftones in a conforming PDF/A-4 file shall have the value 1 or 5 for the HalftoneType key | A Halftone has type %1 instead of 1 or 5 |
| 6.2.5 | 5 | PDHalftone | Halftones in a conforming PDF/A-4 file shall not contain a HalftoneName key | A Halftone dictionary contains the HalftoneName key |
| 6.2.5 | 6 | PDHalftone | The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-2:2020 | Custom TransferFunction in a Halftone dictionary is not permitted for primary (CMYK) colorants |
| 6.2.7.1 | 1 | PDXImage | An Image dictionary shall not contain the Alternates key | Alternates key is present in the Image dictionary |
| 6.2.7.1 | 2 | PDXImage | An Image dictionary shall not contain the OPI key | OPI key is present in the Image dictionary |
| 6.2.7.1 | 3 | PDXImage | If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key, if present, shall have a value of false | The value of the Interpolate key in the Image dictionary is true |
| 6.2.7.1 | 4 | PDXImage | If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16 | The value of the BitsPerComponent key in the Image dictionary is %1 |
| 6.2.7.1 | 5 | PDMaskImage | If an image mask dictionary contains the BitsPerComponent key, its value shall be 1 | The value of the BitsPerComponent key in the image mask dictionary is %1 |
| 6.2.7.3 | 1 | JPEG2000 | The number of colour channels in the JPEG2000 data shall be 1, 3 or 4 | JPEG2000 image has %1 colour channels that is neither 1, 3 or 4 |
| 6.2.7.3 | 2 | JPEG2000 | If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field | The JPEG2000 image contains %1 colour specifications with the best colour fidelity (value 0x01 in the APPROX field) |
| 6.2.7.3 | 3 | JPEG2000 | The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming processor shall use only that colour space and shall ignore all other colour space specifications | Invalid JPEG2000 image: the value of the METH entry (%1) in its 'colr' box is different from 0x01, 0x02 or 0x03 |
| 6.2.7.3 | 4 | JPEG2000 | JPEG2000 enumerated colour space 19 (CIEJab) shall not be used | JPEG2000 image uses enumerated colour space 19 (CIEJab), which is not allowed in PDF/A |
| 6.2.7.3 | 5 | JPEG2000 | The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth | JPEG2000 image has different bit-depth parameters in 'bpcc' box, or bit depth is out of range ('bpcc' box present = %1, bit depth = %2) |
| 6.2.8.1 | 1 | PDXForm | A form XObject dictionary shall not contain an OPI key | The form XObject dictionary contains an OPI key |
| 6.2.8.2 | 1 | PDXForm | A conforming file shall not contain any reference XObjects | The document contains a reference XObject (Ref key in the form XObject dictionary) |
| 6.2.9 | 1 | CosBM | Only blend modes that are specified in ISO 32000-2:2020 shall be used for the value of the BM key in a graphic state dictionary or an annotation dictionary | The document uses the blend mode %1 not defined in ISO 32000-2:2020 |
| 6.2.9 | 2 | PDPage | If the document does not contain a PDF/A output intent, then all pages that contain transparency shall either have a page-level PDF/A output intent or the page dictionary shall include the Group key, and the attribute dictionary that forms the val... | The page contains transparent objects with no blending colour space defined |
| 6.2.10.2 | 1 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Requi... | A Font dictionary has value %1 of Type entry instead of Font |
| 6.2.10.2 | 2 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Re... | Invalid Subtype entry in font dictionary with value %1 |
| 6.2.10.2 | 3 | PDFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (R... | A BaseFont entry is missing or has invalid type |
| 6.2.10.2 | 4 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer ... | A non-standard simple font dictionary has missing or invalid FirstChar entry |
| 6.2.10.2 | 5 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer -... | A non-standard simple font dictionary has missing or invalid LastChar entry |
| 6.2.10.2 | 6 | PDSimpleFont | All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Re... | Widths array is missing or has invalid size %1 instead of %2 |
| 6.2.10.2 | 7 | PDFont | All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 2.0 ar... | Unsupported font file format %1 of the embedded font |
| 6.2.10.3.1 | 1 | PDType0Font | For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary has a value of... | CIDSystemInfo entries the CIDFont and CMap dictionaries of a Type 0 font are not compatible (CIDSystemInfo Ordering = %1, CMap Ordering = %2, CIDSy... |
| 6.2.10.3.2 | 1 | PDCIDFont | ISO 32000-2:2020, 9.7.4 Table 115 requires that all embedded Type 2 CIDFonts, the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-2:2020... | A Type 2 CIDFont dictionary has missing or invalid CIDToGIDMap entry |
| 6.2.10.3.3 | 1 | PDCMap | All CMaps used within a conforming PDF/A-4 file, except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116, shall be embedded in that file as described in ISO 32000-2:2020, 9.7.5 | A non-standard CMap %1 is not embedded |
| 6.2.10.3.3 | 2 | CMapFile | For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream | WMode entry (value %1) in the embedded CMap and in the CMap dictionary (value %2) are not identical |
| 6.2.10.3.3 | 3 | PDReferencedCMap | A CMap shall not reference any other CMap except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116 | A CMap references another non-standard CMap %1 |
| 6.2.10.4.1 | 1 | PDFont | The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-2:2020, 9.9 | The font program is not embedded |
| 6.2.10.4.1 | 2 | Glyph | Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType... | Not all glyphs referenced for rendering are present in the embedded font program |
| 6.2.10.5 | 1 | Glyph | For every font embedded in a conforming file, the glyph width information in the font dictionary and in the embedded font program shall be consistent for every glyph referenced for rendering. Glyphs that are referenced only with rendering mode 3 a... | Glyph width %1 in the embedded font program is not consistent with the Widths entry of the font dictionary (value %2) |
| 6.2.10.6 | 1 | TrueTypeFontProgram | For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain at least Microsoft Unicode (3,1 – Platform ID=3, Encoding ID=1), or Macintosh Roman (1,0 – Platform ID=1, Encoding ID=0) 'cmap' subtable that ... | The embedded font program for a non-symbolic TrueType font does not contain Microsoft Symbol (3,1 – Platform ID=3, Encoding ID=1) or the Mac Roman ... |
| 6.2.10.6 | 2 | PDTrueTypeFont | All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the... | A non-symbolic TrueType font encoding does not define a correct mapping to the Adobe Glyph List (Encoding = %1, Encoding entry contains a Differenc... |
| 6.2.10.6 | 3 | PDTrueTypeFont | Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary | A symbolic TrueType font specifies an Encoding entry in its dictionary |
| 6.2.10.6 | 4 | TrueTypeFontProgram | The 'cmap' subtable in the embedded font program for a symbolic TrueType font shall contain either the Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0 – Platform ID=1, Encoding ID=0) encoding | The embedded font program for a symbolic TrueType font does not contain Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0... |
| 6.2.10.7 | 1 | Glyph | If a ToUnicode CMap is present, the Unicode values specified there shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE | The glyph has Unicode value 0, U+FEFF or U+FFFE, which is invalid by Unicode standard |
| 6.2.10.8 | 1 | CosActualText | The ActualText entry shall not contain any PUA values | The ActualText entry contains Unicode PUA (Private Use Area) code points |
| 6.2.10.9 | 1 | Glyph | A PDF/A-4 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream | The document contains a reference to the .notdef glyph |
### Clause 6.3

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.3.1 | 1 | PDAnnot | Annotation types not defined in ISO 32000-2:2020, 12.5.6.1, Table 171 shall not be permitted. Additionally, the Sound, Screen, Movie, 3D and RichMedia types shall not be permitted. The FileAttachment type shall only be permitted in a PDF/A-4f comp... | Unknown or not permitted Annotation type %1 |
| 6.3.2 | 1 | PDAnnot | Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key | A dictionary of %1 annotation does not contain F key |
| 6.3.2 | 2 | PDAnnot | If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0 | Annotation flags are set the annotation to be hidden/invisible or non-printable (F = %1, Print = %2, Hidden = %3, Invisible = %4, NoView = %5, Togg... |
| 6.3.3 | 1 | PDAnnot | Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: Annotations where the value of the Rect key consists of an array where... | An annotation does not contain an appearance dictionary |
| 6.3.3 | 2 | PDAnnot | For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key | Annotation's appearance dictionary contains entries %1 other than N |
| 6.3.3 | 3 | PDAnnot | If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary | An annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, but the value of the N key is %1 instead of appeara... |
| 6.3.3 | 4 | PDAnnot | If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream | An annotation dictionary's Subtype key has a value %1 and its FT key has a value %2, but the value of the N key is not an appearance stream |
### Clause 6.4

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.4.1 | 1 | PDWidgetAnnot | A Widget annotation dictionary shall not contain the A key | A Widget annotation contains A entry |
| 6.4.1 | 2 | PDAcroForm | The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false | The interactive form dictionary contains the NeedAppearances flag with value true |
| 6.4.2 | 1 | PDAcroForm | The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-4 file, if present, shall not contain the XFA key | The interactive form dictionary contains the XFA key |
| 6.4.2 | 2 | CosDocument | A document's Catalog shall not contain the NeedsRendering key | A document's Catalog contains NeedsRendering flag set to true |
### Clause 6.6

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.6.1 | 1 | PDAction | The Launch, Sound, Movie, ResetForm, ImportData, Hide, Rendition, Trans, SetOCGState and GoTo3DView actions shall not be permitted. Additionally, the obsoleted set-state and no-op actions shall not be permitted | Unknown or not permitted Action type %1 |
| 6.6.1 | 2 | PDNamedAction | Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted | Unknown or not permitted named action %1 |
| 6.6.3 | 1 | PDAdditionalActions | If a document catalog dictionary or page dictionary or an annotation dictionary (other than a Widget annotation dictionary) include an AA entry, its value (which is an additional-actions dictionary) shall only contain keys from the following list:... | Additional-actions dictionary contains key(s) %1 not from the following permitted list: E, X, D, U, Fo, and Bl |
### Clause 6.7

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.7.2.1 | 1 | PDDocument | The document catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-2:2020, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Sub... | The document catalog dictionary doesn't contain metadata key or metadata stream dictionary does not contain either entry Type with value /Metadata ... |
| 6.7.2.1 | 2 | XMPPackage | The bytes attribute shall not be used in the header of an XMP packet | The XMP Package contains bytes attribute |
| 6.7.2.1 | 3 | XMPPackage | The encoding attribute shall not be used in the header of an XMP packet | The XMP Package contains encoding attribute |
| 6.7.2.1 | 4 | XMPPackage | All content of all XMP packets located in any metadata stream present in the PDF shall be well-formed as defined by XMP (ISO 16684-1) | A metadata stream is serialized incorrectly and can not be parsed |
| 6.7.2.1 | 5 | XMPPackage | All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8 | The XMP package uses encoding %1 different from UTF-8 |
| 6.7.3 | 1 | MainXMPPackage | The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema. The PDF/A Identification schema defined in Table 2 uses the namespace URI "http://www.aiim.org/pdfa/ns/id/" | The document metadata stream doesn't contain PDF/A Identification Schema |
| 6.7.3 | 2 | PDFAIdentification | The value of "pdfaid:part" shall be the part number of ISO 19005 to which the file conforms | The "part" property of the PDF/A Identification Schema is %1 instead of 4 for PDF/A-4 conforming file |
| 6.7.3 | 3 | PDFAIdentification | A PDF/A-4f conforming file (as described in Annex A) shall specify the value of "pdfaid:conformance" as F | The "conformance" property of the PDF/A Identification Schema is %1 instead of "F" for PDF/A-4F conforming file |
| 6.7.3 | 4 | PDFAIdentification | Property "part" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "part" of the PDF/A Identification Schema has invalid namespace prefix %1 |
| 6.7.3 | 5 | PDFAIdentification | The value of "pdfaid:rev" shall be "2020" | The value of "pdfaid:rev" (%1) is not "2020" |
| 6.7.3 | 6 | PDFAIdentification | Property "rev" of the PDF/A Identification Schema shall have namespace prefix "pdfaid" | Property "rev" of the PDF/A Identification Schema has invalid namespace prefix %1 |
### Clause 6.9

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.9 | 1 | EmbeddedFile | The embedded file stream dictionary shall include a valid MIME type value for the Subtype key. If the MIME type is not known, the value "application/octet-stream" shall be used | MIME type %1 of an embedded file is missing or invalid |
| 6.9 | 2 | CosFileSpecification | The file specification dictionary for an embedded file shall contain the F and UF keys | The file specification dictionary for an embedded file does not contain either F or UF key (F = %1, UF = %2) |
| 6.9 | 4 | CosFileSpecification | Each embedded file’s file specification dictionary shall contain an AFRelationship key (ISO 32000-2, 7.11.3) that describes how this embedded file relates to the content of the PDF | The file specification dictionary for an embedded file does not contain the AFRelationship key |
| 6.9 | 5 | CosDocument | A PDF/A-4f conforming file shall contain an EmbeddedFiles key in the name dictionary of the document catalog dictionary | A PDF/A-4f conforming file does not contain an EmbeddedFiles key in the name dictionary of the document catalog dictionary |
### Clause 6.10

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.10 | 1 | PDOCConfig | Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key | Missing Name entry of the optional content configuration dictionary |
| 6.10 | 2 | PDOCConfig | Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-4 file | Optional content configuration dictionary has duplicated name %1 |
| 6.10 | 3 | PDOCConfig | If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file | Optional content group(s) %1 not present in the Order entry of the optional content configuration dictionary |
### Clause 6.11

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.11 | 1 | PDDocument | There shall be no AlternatePresentations entry in the document's name dictionary | The document's name dictionary contains the AlternatePresentations entry |
| 6.11 | 2 | PDPage | There shall be no PresSteps entry in any Page dictionary | A Page dictionary contains the PresSteps entry |
### Clause 6.12

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.12 | 1 | CosDocument | The document catalog shall not contain the Requirements key | The document catalog contains the Requirements key |

## PDF/UA-1

**PDF/UA-1 validation profile**
*Validation rules against ISO 14289-1:2014*
**Total rules: 106**

### Clause 5

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 5 | 1 | MainXMPPackage | The PDF/UA version and conformance level of a file shall be specified using the PDF/UA Identification extension schema | The document metadata stream doesn't contain PDF/UA Identification Schema |
| 5 | 2 | PDFUAIdentification | The value of "pdfuaid:part" shall be the part number of the International Standard to which the file conforms | The "part" property of the PDF/UA Identification Schema is %1 instead of 1 for PDF/UA-1 conforming file |
| 5 | 3 | PDFUAIdentification | Property "part" of the PDF/UA Identification Schema shall have namespace prefix "pdfuaid" | Property "part" of the PDF/UA Identification Schema has invalid namespace prefix %1 |
| 5 | 4 | PDFUAIdentification | Property "amd" of the PDF/UA Identification Schema shall have namespace prefix "pdfuaid" | Property "amd" of the PDF/UA Identification Schema has invalid namespace prefix %1 |
| 5 | 5 | PDFUAIdentification | Property "corr" of the PDF/UA Identification Schema shall have namespace prefix "pdfuaid" | Property "corr" of the PDF/UA Identification Schema has invalid namespace prefix %1 |
### Clause 6.1

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.1 | 1 | CosDocument | The file header shall consist of "%PDF-1.n" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h) | File header %1 does not match the pattern %PDF-1.n, where 'n' is a single digit number between 0 and 7 |
### Clause 6.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.2 | 1 | CosDocument | The document catalog dictionary shall include a MarkInfo dictionary containing an entry, Marked, whose value shall be true | MarkInfo dictionary is not present in the document catalog, or Marked entry is set to false or is not present in the MarkInfo dictionary (MarkInfo ... |
### Clause 7.1

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.1 | 1 | SEMarkedContent | Content marked as Artifact should not be present inside tagged content | Content marked as Artifact is present inside tagged content (parent struct element %1) |
| 7.1 | 2 | SEMarkedContent | Tagged content should not be present inside content marked as Artifact | Tagged content (parent struct element %1) is present inside content marked as Artifact |
| 7.1 | 3 | SESimpleContentItem | Content shall be marked as Artifact or tagged as real content | Content is neither marked as Artifact nor tagged as real content |
| 7.1 | 4 | CosDocument | Files shall have a Suspects value of false(ISO 32000-1:2008, Table 321) | Suspects entry has a value of true |
| 7.1 | 5 | SENonStandard | All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in ISO 32000-1:2008, 14.8.4, in the role map dictionary of the structure tree root. This mapping may be indirect; within the role map... | Non-standard structure type %1 is not mapped to a standard type |
| 7.1 | 6 | PDStructElem | A circular mapping shall not exist | A circular mapping exists for %1 structure type |
| 7.1 | 7 | PDStructElem | Standard tags defined in ISO 32000-1:2008, 14.8.4, shall not be remapped | Standard type %1 is remapped |
| 7.1 | 8 | PDDocument | The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-1:2008, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with... | The document catalog dictionary doesn't contain metadata key or metadata stream dictionary does not contain either entry Type with value /Metadata ... |
| 7.1 | 9 | MainXMPPackage | The Metadata stream in the document's catalog dictionary shall contain a dc:title entry, where dc is the recommended prefix for the Dublin Core metadata schema as defined in the XMP specification, which clearly identifies the document | Metadata stream does not contain dc:title |
| 7.1 | 10 | CosDocument | The document catalog dictionary shall include a ViewerPreferences dictionary containing a DisplayDocTitle key, whose value shall be true | ViewerPreferences dictionary is not present in the document Catalog, or DisplayDocTitle key is set to false or is not present in the ViewerPreferen... |
| 7.1 | 11 | PDDocument | The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog dictionary, as described in ISO 32000-1:2008, 14.7 | StructTreeRoot entry is not present in the document catalog |
| 7.1 | 12 | PDStructElem | A structure element dictionary shall contain the P (parent) entry according to ISO 32000-1:2008, 14.7.2, Table 355 | A structure element dictionary does not contain the P (parent) entry |
### Clause 7.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.2 | 2 | PDOutline | Natural language in the Outline entries shall be determined | Natural language in the Outline entries cannot be determined |
| 7.2 | 3 | SETable | Table element may contain only TR, THead, TBody, TFoot and Caption elements | Table element contains %1 element(s) instead of TR, THead, TBode, TFoot or Caption |
| 7.2 | 4 | SETR | TR element should be contained in Table, THead, TBody or TFoot element | TR element contained in %1 instead of Table, THead, TBody or TFoot element |
| 7.2 | 5 | SETHead | THead element should be contained in Table element | THead element contained in %1 instead of Table element |
| 7.2 | 6 | SETBody | TBody element should be contained in Table element | TBody element contained in %1 instead of Table element |
| 7.2 | 7 | SETFoot | TFoot element should be contained in Table element | TFoot element contained in %1 instead of Table element |
| 7.2 | 8 | SETH | TH element should be contained in TR element | TH element contained in %1 instead of TR element |
| 7.2 | 9 | SETD | TD element should be contained in TR element | TD element contained in %1 instead of TR element |
| 7.2 | 10 | SETR | TR element may contain only TH and TD elements | TR element contains %1 element(s) instead of TH or TD |
| 7.2 | 11 | SETable | Table element should contain zero or one THead kid | Table element contains more than one THead kid |
| 7.2 | 12 | SETable | Table element should contain zero or one TFoot kid | Table element contains more than one TFoot kid |
| 7.2 | 13 | SETable | If Table element contains TFoot kid, Table element should contain one or more TBody kids | Table element contains TFoot kid, but does not contain TBody kids |
| 7.2 | 14 | SETable | If Table element contains THead kid, Table element should contain one or more TBody kids | Table element contains THead kid, but does not contain TBody kids |
| 7.2 | 15 | SETableCell | A table cell shall not have intersection with other cells | Table cell has intersection with other cells |
| 7.2 | 16 | SETable | Table element may contain a Caption element as its first or last kid | Table element contains Caption as its %1 child instead of first or last one |
| 7.2 | 17 | SELI | LI element should be contained in L element | LI element contained in %1 instead of L element |
| 7.2 | 18 | SELBody | LBody element should be contained in LI element | LBody element contained in %1 instead of LI element |
| 7.2 | 19 | SEL | L element may contain only L, LI and Caption elements | L element contains %1 element(s) instead of L, LI or Caption |
| 7.2 | 20 | SELI | LI element may contain only Lbl and LBody elements | LI element contains %1 element(s) instead of Lbl or LBody |
| 7.2 | 21 | PDStructElem | Natural language for text in ActualText attribute shall be determined | Natural language for text in ActualText attribute cannot be determined |
| 7.2 | 22 | PDStructElem | Natural language for text in Alt attribute shall be determined | Natural language for text in Alt attribute cannot be determined |
| 7.2 | 23 | PDStructElem | Natural language for text in E attribute shall be determined | Natural language for text in E attribute cannot be determined |
| 7.2 | 24 | PDAnnot | Natural language in the Contents entry for annotations shall be determined | Natural language in the Contents entry for annotations cannot be determined |
| 7.2 | 25 | PDFormField | Natural language in the TU key for form fields shall be determined | Natural language in the TU key for form fields cannot be determined |
| 7.2 | 26 | SETOCI | TOCI element should be contained in TOC element | TOCI element contained in %1 instead of TOC element |
| 7.2 | 27 | SETOC | TOC element may contain only TOC, TOCI and Caption elements | TOC element contains %1 element(s) instead of TOC, TOCI or Caption |
| 7.2 | 28 | SETOC | TOC element may contain a Caption element only as its first kid | TOC element contains Caption as its %1 child instead of first one |
| 7.2 | 29 | CosLang | If the Lang entry is present in the document's Catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as described in ISO 32000-1:2008, 14.9.2. A language identifier shall be a Language-T... | Value %1 of the Lang entry is not a Language-Tag |
| 7.2 | 30 | SEMarkedContent | Natural language for text in ActualText attribute in Span Marked Content shall be determined | Natural language for text in ActualText attribute in Span Marked Content cannot be determined |
| 7.2 | 31 | SEMarkedContent | Natural language for text in Alt attribute in Span Marked Content shall be determined | Natural language for text in Alt attribute in Span Marked Content cannot be determined |
| 7.2 | 32 | SEMarkedContent | Natural language for text in E attribute in Span Marked Content shall be determined | Natural language for text in E attribute in Span Marked Content cannot be determined |
| 7.2 | 33 | XMPLangAlt | Natural language for document metadata shall be determined | Natural language for document metadata cannot be determined |
| 7.2 | 34 | SETextItem | Natural language for text in page content shall be determined | Natural language for text in page content cannot be determined |
| 7.2 | 36 | SETHead | THead element may contain only TR elements | THead element contains %1 element(s) instead of TR |
| 7.2 | 37 | SETBody | TBody element may contain only TR elements | TBody element contains %1 element(s) instead of TR |
| 7.2 | 38 | SETFoot | TFoot element may contain only TR elements | TFoot element contains %1 element(s) instead of TR |
| 7.2 | 39 | SETable | Table element may contain only one Caption element | Table element contains %1 Caption elements instead of one |
| 7.2 | 40 | SEL | L element may contain a Caption element only as its first kid | L element contains Caption as its %1 child instead of first one |
| 7.2 | 41 | SETable | Table columns shall have the same number of rows (taking into account row spans) | Table columns 1 and %1 span different number of rows |
| 7.2 | 42 | SETable | Table rows shall have the same number of columns (taking into account column spans) | Table rows 1 and %1 span different number of columns |
| 7.2 | 43 | SETable | Table rows shall have the same number of columns (taking into account column spans) | Table rows 1 and %1 span different number of columns (%2 and %3 respectively) |
### Clause 7.3

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.3 | 1 | SEFigure | Figure tags shall include an alternative representation or replacement text that represents the contents marked with the Figure tag as noted in ISO 32000-1:2008, 14.7.2, Table 323 | Figure structure element neither has an alternate description nor a replacement text |
### Clause 7.4

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.4.2 | 1 | SEHn | For documents that are not strongly structured, as described in ISO 32000-1:2008, 14.8.4.3.5, heading tags shall be used as follows: (*) If any heading tags are used, H1 shall be the first. (*) A document may use more than one instance of any spec... | Heading level %1 is skipped in a descending sequence of header levels |
| 7.4.4 | 1 | PDStructElem | Each node in the tag tree shall contain at most one child H tag | A node contains more than one H tag |
| 7.4.4 | 2 | SEH | All documents shall be either strongly or weakly structured, but not both | Document uses both H and H# tags |
| 7.4.4 | 3 | SEHn | All documents shall be either strongly or weakly structured, but not both | Document uses both H and H# tags |
### Clause 7.5

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.5 | 1 | SETD | If the table's structure is not determinable via Headers and IDs, then structure elements of type TH shall have a Scope attribute | TD does not contain Headers attribute, and Headers for this table cell cannot be determined algorithmically |
| 7.5 | 2 | SETD | If the table's structure is not determinable via Headers and IDs, then structure elements of type TH shall have a Scope attribute | TD references undefined Header(s) %1, and Headers for this table cell cannot be determined algorithmically |
### Clause 7.7

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.7 | 1 | SEFormula | All mathematical expressions shall be enclosed within a Formula tag as detailed in ISO 32000-1:2008, 14.8.4.5 and shall have Alt or ActualText attributes | Formula structure element neither has an alternate description nor a replacement text |
### Clause 7.9

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.9 | 1 | SENote | Note tag shall have ID entry | ID key of the Note tag is not present |
| 7.9 | 2 | SENote | Each Note tag shall have unique ID key | ID key %1 of the Note tag is non-unique |
### Clause 7.10

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.10 | 1 | PDOCConfig | Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key | Missing or empty Name entry of the optional content configuration dictionary |
| 7.10 | 2 | PDOCConfig | The AS key shall not appear in any optional content configuration dictionary | AS key is present in the optional content configuration dictionary |
### Clause 7.11

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.11 | 1 | CosFileSpecification | The file specification dictionary for an embedded file shall contain the non-empty F and UF keys | The file specification dictionary for an embedded file does not contain either F or UF key or at least one of the keys is empty (F = %1, UF = %2) |
### Clause 7.15

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.15 | 1 | PDAcroForm | Dynamic XFA forms shall not be used | Dynamic XFA forms is present |
### Clause 7.16

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.16 | 1 | PDEncryption | An encrypted conforming file shall contain a P key in its encryption dictionary (ISO 32000-1:2008, 7.6.3.2, Table 21). The 10th bit position of the P key shall be true | The file is encrypted but does not contain a P entry in its encryption dictionary or the file is encrypted and does contain a P entry, but the 10th... |
### Clause 7.18

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.18.1 | 1 | PDAnnot | An annotation, excluding annotations of subtype Widget, PrinterMark or Link, shall be nested within an Annot tag | %1 annotation is an Artifact or is nested within %2 tag (standard type = %3) instead of Annot |
| 7.18.1 | 2 | PDAnnot | An annotation (except Widget annotations or hidden annotations, or those having rectangle outside the crop-box) shall have either Contents key or an Alt entry in the enclosing structure element | %1 annotation whose hidden flag is not set and whose rectangle is not outside the crop-box has neither Contents key nor an Alt entry in the enclosi... |
| 7.18.1 | 3 | PDWidgetAnnot | A form field shall have a TU key present or all its Widget annotations shall have alternative descriptions (in the form of an Alt entry in the enclosing structure elements) | A form field neither has TU key nor its Widget annotations have alternative descriptions (in the form of an Alt entry in the enclosing structure el... |
| 7.18.2 | 1 | PDTrapNetAnnot | Annotations of subtype TrapNet shall not be permitted | An annotation of subtype TrapNet exists |
| 7.18.3 | 1 | PDPage | Every page on which there is an annotation shall contain in its page dictionary the key Tabs, and its value shall be S | A page with annotation(s) contains Tabs key with value %1 instead of S |
| 7.18.4 | 1 | PDWidgetAnnot | A Widget annotation shall be nested within a Form tag per ISO 32000-1:2008, 14.8.4.5, Table 340 | A Widget annotation is an Artifact or is nested within %1 tag (standard type = %2) instead of Form |
| 7.18.4 | 2 | SEForm | If the Form element omits a Role attribute (Table 348), it shall have only one child: an object reference (14.7.4.3) identifying the widget annotation per ISO 32000-1:2008, 14.8.4.5, Table 340 | The Form element omits a Role attribute and doesn't have only one child identifying the widget annotation |
| 7.18.5 | 1 | PDLinkAnnot | Links shall be tagged according to ISO 32000-1:2008, 14.8.4.4.2, Link Element | A Link annotation is an Artifact or is nested within %1 tag (standard type = %2) instead of Link |
| 7.18.5 | 2 | PDLinkAnnot | Links shall contain an alternate description via their Contents key as described in ISO 32000-1:2008, 14.9.3 | A link annotation does not include an alternate description in the Contents key |
| 7.18.6.2 | 1 | PDMediaClip | In the media clip data dictionary, the optional CT key (ISO 32000-1:2008, 13.2.4.2, Table 274) is required | CT key is missing from the media clip data dictionary |
| 7.18.6.2 | 2 | PDMediaClip | In the media clip data dictionary, the optional Alt key (ISO 32000-1:2008, 13.2.4.2, Table 274) is required | Alt key (value %1) is missing from the media clip data dictionary or has incorrect value |
| 7.18.8 | 1 | PDPrinterMarkAnnot | PrinterMark annotations, if present, shall be considered Incidental Artifacts, as if they are hidden page elements as defined in ISO 32000-1:2008, 14.8.2.2.3 | A PrinterMark annotation is included in logical structure |
### Clause 7.20

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.20 | 1 | PDXForm | A conforming file shall not contain any reference XObjects | The document contains a reference XObject (Ref key in the form XObject dictionary) |
| 7.20 | 2 | PDXForm | The content of Form XObjects shall be incorporated into structure elements according to ISO 32000-1:2008, 14.7.2 | Form XObject contains MCIDs and is referenced more than once |
### Clause 7.21

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 7.21.3.1 | 1 | PDType0Font | For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary is Identity-H ... | CIDSystemInfo entries the CIDFont and CMap dictionaries of a Type 0 font are not compatible (CIDSystemInfo Ordering = %1, CMap Ordering = %2, CIDSy... |
| 7.21.3.2 | 1 | PDCIDFont | ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2... | A Type 2 CIDFont dictionary has missing or invalid CIDToGIDMap entry |
| 7.21.3.3 | 1 | PDCMap | All CMaps used within a PDF/UA file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5 | A non-standard CMap %1 is not embedded |
| 7.21.3.3 | 2 | CMapFile | For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream | WMode entry (value %1) in the embedded CMap and in the CMap dictionary (value %2) are not identical |
| 7.21.3.3 | 3 | PDReferencedCMap | A CMap shall not reference any other CMap except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118 | A CMap references another non-standard CMap %1 |
| 7.21.4.1 | 1 | PDFont | The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9 | The font program is not embedded |
| 7.21.4.1 | 2 | Glyph | Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced solely in text rendering mode 3 is not rendered and is thus exempt from the requirements that impact the visual representation of the gly... | Not all glyphs referenced for rendering are present in the embedded font program |
| 7.21.4.2 | 1 | PDType1Font | If the FontDescriptor dictionary of an embedded Type 1 font contains a CharSet string, then it shall list the character names of all glyphs present in the font program, regardless of whether a glyph in the font is referenced or used by the PDF or not | A CharSet entry in the Descriptor dictionary of a Type1 font incorrectly lists glyphs present in the font program |
| 7.21.4.2 | 2 | PDCIDFont | If the FontDescriptor dictionary of an embedded CID font contains a CIDSet stream, then it shall identify all CIDs which are present in the font program, regardless of whether a CID in the font is referenced or used by the PDF or not | A CIDSet entry in the Font descriptor does not correctly identify all glyphs present in the embedded font subset |
| 7.21.5 | 1 | Glyph | For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent | Glyph width %1 in the embedded font program is not consistent with the Widths entry of the font dictionary (value %2) |
| 7.21.6 | 1 | TrueTypeFontProgram | For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain one or several non-symbolic cmap entries such that all necessary glyph lookups can be carried out | The embedded font program for a non-symbolic TrueType font does not contain non-symbolic cmap entries |
| 7.21.6 | 2 | PDTrueTypeFont | All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the... | A non-symbolic TrueType font encoding does not define a correct mapping to the Adobe Glyph List (Encoding = %1, Encoding entry contains a Differenc... |
| 7.21.6 | 3 | PDTrueTypeFont | Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary | A symbolic TrueType font specifies an Encoding entry in its dictionary |
| 7.21.6 | 4 | TrueTypeFontProgram | The 'cmap' table in the embedded font program for a symbolic TrueType font shall contain either exactly one encoding or it shall contain, at least, the Microsoft Symbol (3,0 - Platform ID=3, Encoding ID=0) encoding | The embedded font program for a symbolic TrueType font contains %1 cmap subtables and does not contain Microsoft Symbol (3,0 – Platform ID=3, Encod... |
| 7.21.7 | 1 | Glyph | The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 14289-1, 7.21.7 | The glyph can not be mapped to Unicode |
| 7.21.7 | 2 | Glyph | The Unicode values specified in the ToUnicode CMap shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE | The glyph has Unicode value 0, U+FEFF or U+FFFE, which is invalid by Unicode standard |
| 7.21.8 | 1 | Glyph | A PDF/UA compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream | The document contains a reference to the .notdef glyph |

## PDF/UA-2

**PDF/UA-2 validation profile**
*Validation rules against ISO 14289-2:2024*
**Total rules: 91**

### Clause 5

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 5 | 1 | MainXMPPackage | The PDF/UA version of a file shall be specified in the value of the Metadata entry in the document catalog using the PDF/UA identification schema | The document metadata stream doesn't contain PDF/UA Identification Schema |
| 5 | 2 | PDFUAIdentification | The value of "pdfuaid:part" shall be the part number of the International Standard to which the file conforms | The "part" property of the PDF/UA Identification Schema is %1 instead of 2 for PDF/UA-2 conforming file |
| 5 | 3 | PDFUAIdentification | Property "part" of the PDF/UA Identification Schema shall have namespace prefix "pdfuaid" | Property "part" of the PDF/UA Identification Schema has invalid namespace prefix %1 |
| 5 | 4 | PDFUAIdentification | Property "rev" of the PDF/UA Identification Schema shall have namespace prefix "pdfuaid" | Property "rev" of the PDF/UA Identification Schema has invalid namespace prefix %1 |
| 5 | 5 | PDFUAIdentification | The value of "pdfuaid:rev" shall be "2024" | The value of "pdfuaid:rev" (%1) is not "2024" |
### Clause 6.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 6.2 | 1 | CosDocument | The document catalog dictionary shall include a MarkInfo dictionary containing an entry, Marked, whose value shall be true | MarkInfo dictionary is not present in the document catalog, or Marked entry is set to false or is not present in the MarkInfo dictionary (MarkInfo ... |
### Clause 8.2

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 8.2.1 | 1 | PDDocument | The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog dictionary, as described in ISO 32000-2:2020, 14.7 | StructTreeRoot entry is not present in the document catalog |
| 8.2.1 | 2 | PDStructElem | A structure element dictionary shall contain the P (parent) entry according to ISO 32000-2:2020, 14.7.2, Table 323 | A structure element dictionary does not contain the P (parent) entry |
| 8.2.2 | 1 | SESimpleContentItem | Content that is not considered real shall be an artifact | Content is neither marked as Artifact nor tagged as real content |
| 8.2.4 | 1 | SENonStandard | All structure elements shall belong to, or be role mapped to, at least one of the following namespaces specified in ISO 32000-2:2020, 14.8.6: — the PDF 1.7 namespace; — the PDF 2.0 namespace; — the MathML namespace | Non-standard structure type %1 is not mapped to a standard type |
| 8.2.4 | 2 | PDStructElem | A circular mapping shall not exist | A circular mapping exists for %1 structure type |
| 8.2.4 | 3 | PDStructElem | Within a given explicitly provided namespace, structure types shall not be role mapped to other structure types in the same namespace | Structure type %1 is role mapped to other structure type in the same namespace |
| 8.2.4 | 4 | SENonStandard | All structure elements shall belong to, or be role mapped to, at least one of the following namespaces specified in ISO 32000-2:2020, 14.8.6: — the PDF 1.7 namespace; — the PDF 2.0 namespace; — the MathML namespace | The standard structure type %1 is remapped to a non-standard type |
| 8.2.5.2 | 1 | PDStructTreeRoot | The structure tree root shall contain a single Document structure element as its only child, as specified in ISO 32000-2:2020, Annex L and ISO/TS 32005 | The structure tree root contains %1 element(s) instead of a single Document structure element |
| 8.2.5.2 | 2 | PDStructTreeRoot | The structure tree root shall contain a single Document structure element as its only child. The namespace for that element shall be specified as the PDF 2.0 namespace | The structure tree root contains a single Document structure element, but this element is not within the PDF 2.0 namespace (namespace URI = %1) |
| 8.2.5.8 | 1 | SETOCI | Each TOCI in the table of contents shall identify the target of the reference using the Ref entry, either directly on the TOCI structure element itself or on one of its child structure elements | TOCI in the TOC does not contain the Ref entry, neither directly on the TOCI structure element itself nor on its descendant structure elements |
| 8.2.5.12 | 1 | SEH | Conforming files shall not use the H structure type | Document uses H structure type |
| 8.2.5.14 | 1 | SENote | The Note standard structure type shall not be present in conforming documents unless role mapped to a structure element in the PDF 2.0 namespace | Document uses Note structure type |
| 8.2.5.14 | 2 | SEFENote | Real content that refers to footnotes or endnotes (real content enclosed in FENote structure elements) shall use the Ref entry on the referring structure element to reference the FENote. The corresponding FENote shall also use the Ref entry to ide... | Ref entry in the FENote does not reference structure elements with object numbers %1, while these structure elements reference this FENote |
| 8.2.5.14 | 3 | SEFENote | Real content that refers to footnotes or endnotes (real content enclosed in FENote structure elements) shall use the Ref entry on the referring structure element to reference the FENote. The corresponding FENote shall also use the Ref entry to ide... | Ref entry in the FENote reference structure elements with object numbers %1, while these structure elements do not reference this FENote |
| 8.2.5.14 | 4 | SEFENote | The value of the NoteType attribute of the FENote structure element shall be Footnote, Endnote or None | NoteType attribute of the FENote structure element has value %1 instead of Footnote, Endnote or None |
| 8.2.5.20 | 1 | PDLinkAnnot | A link annotation and its associated content shall be enclosed in either a Link or Reference structure element | A Link annotation is an Artifact or is nested within %1 tag (standard type = %2) instead of Link or Reference |
| 8.2.5.20 | 2 | PDLinkAnnot | Link annotations that target different locations shall be in separate Link or Reference structure elements | Struct element %1 contains Link annotations %1 and %2 that target different locations |
| 8.2.5.23 | 1 | SERuby | A Ruby structure element shall contain a single RB structure element and a single RT structure element or a Ruby structure element shall consist of a four-element subsequence: RB, RP, RT, RP | The Ruby structure element has invalid sequence of children: %1 |
| 8.2.5.24 | 1 | SEWarichu | Content typeset as warichu shall be tagged in a three-element sequence consisting of the structure elements WP, WT and WP, grouped inside a Warichu structure element | The Warichu structure element has invalid sequence of children: %1 |
| 8.2.5.25 | 1 | SEL | If Lbl structure elements are present, the ListNumbering attribute shall be present on the respective L structure element; in such cases the value None shall not be used | List items contain Lbl structure elements, but the ListNumbering attribute is not present on the respective L structure element or it has value None |
| 8.2.5.25 | 2 | SELI | Any real content within an LI structure element that is not enclosed in an Lbl structure element shall be enclosed in an LBody structure element | The LI structure element contains real content as its direct child instead of enclosing it into Lbl or LBody structure elements |
| 8.2.5.26 | 1 | SETableCell | Tables shall be regular. A table cell shall not have intersection with other cells | Table cell has intersection with other cells |
| 8.2.5.26 | 2 | SETable | Tables shall be regular. Row groupings formed by THead, TBody and TFoot structure elements shall be regular. Table columns shall have the same number of rows (taking into account row spans) in table and each row groupings formed by THead, TBody an... | Columns 1 and %1 span different number of rows in table, or within one of row groupings formed by THead, TBody and TFoot structure elements |
| 8.2.5.26 | 3 | SETable | Tables shall be regular. Table rows shall have the same number of columns (taking into account column spans) | Table rows 1 and %1 span different number of columns |
| 8.2.5.26 | 4 | SETable | Tables shall be regular. Table rows shall have the same number of columns (taking into account column spans) | Table rows 1 and %1 span different number of columns (%2 and %3 respectively) |
| 8.2.5.26 | 5 | SETD | When a table contains header cells, that table shall provide sufficient semantic information to allow accurate determination of which of its table header cells (structure element TH) pertain to other cells as specified in ISO 32000-2:2020, 14.8.5.7 | TD does not contain Headers attribute, and Headers for this table cell cannot be determined algorithmically |
| 8.2.5.26 | 6 | SETD | When a table contains header cells, that table shall provide sufficient semantic information to allow accurate determination of which of its table header cells (structure element TH) pertain to other cells as specified in ISO 32000-2:2020, 14.8.5.7 | TD references undefined Header(s) %1, and Headers for this table cell cannot be determined algorithmically |
| 8.2.5.27 | 1 | PDStructElem | When present, the Caption element shall be the first or the last child of the structure element | Structure element %1 contains Caption as its %2 child instead of first or last one |
| 8.2.5.28.2 | 1 | SEFigure | A Figure structure element shall have at least one of the following properties: a) an alternate description (Alt property), as specified in ISO 32000-2:2020, 14.9.3; b) a replacement text (ActualText property) that represents the content enclosed ... | Figure structure element neither has an alternate description nor a replacement text |
| 8.2.5.29 | 1 | SEMathMLStructElem | The math structure type shall occur only as a child of a Formula structure element | The math structure type is not nested within Formula tag |
### Clause 8.4

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 8.4.3 | 1 | Glyph | In all cases, where real content maps to Unicode PUA values, an ActualText or Alt entry shall be present | Real content maps to Unicode PUA values, but ActualText and Alt entry are not present |
| 8.4.3 | 2 | CosActualText | The ActualText entry shall not contain any PUA values | The ActualText entry contains Unicode PUA (Private Use Area) code points |
| 8.4.3 | 3 | CosAlt | The Alt entry shall not contain any PUA values | The Alt entry contains Unicode PUA (Private Use Area) code points |
| 8.4.4 | 1 | PDDocument | The default natural language for content and text strings shall be specified using the Lang entry, with a non-empty value, in the catalog dictionary | Catalog dictionary does not contain Lang entry |
| 8.4.4 | 2 | CosLang | If the Lang entry is present in the document's Catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as described in ISO 32000-2:2020, 14.9.2. A language identifier shall be a Language-T... | Value %1 of the Lang entry is not a Language-Tag |
| 8.4.5.3.1 | 1 | PDType0Font | For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary has a value of... | CIDSystemInfo entries the CIDFont and CMap dictionaries of a Type 0 font are not compatible (CIDSystemInfo Ordering = %1, CMap Ordering = %2, CIDSy... |
| 8.4.5.3.2 | 1 | PDCIDFont | All embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-2:2020, Table 115 | A Type 2 CIDFont dictionary has missing or invalid CIDToGIDMap entry |
| 8.4.5.4 | 1 | PDCMap | All CMaps used within a file that conforms to PDF/UA-2, except those listed in ISO 32000-2:2020, Table 116, shall be embedded in that file, as described in ISO 32000-2:2020, 9.7.5 | A non-standard CMap %1 is not embedded |
| 8.4.5.4 | 2 | CMapFile | For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream | WMode entry (value %1) in the embedded CMap and in the CMap dictionary (value %2) are not identical |
| 8.4.5.4 | 3 | PDReferencedCMap | A CMap shall not reference any other CMap except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116 | A CMap references another non-standard CMap %1 |
| 8.4.5.5.1 | 1 | PDFont | The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-2:2020, 9.9 | The font program is not embedded |
| 8.4.5.5.1 | 2 | Glyph | Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced solely in text rendering mode 3 is not rendered and is thus exempt from the requirements that impact the visual representation of the gly... | Not all glyphs referenced for rendering are present in the embedded font program |
| 8.4.5.6 | 1 | Glyph | For every font embedded in a conforming file and referenced for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent for every glyph | Glyph width %1 in the embedded font program is not consistent with the Widths entry of the font dictionary (value %2) |
| 8.4.5.7 | 1 | TrueTypeFontProgram | For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain at least Microsoft Unicode (3,1 – Platform ID=3, Encoding ID=1), or Macintosh Roman (1,0 – Platform ID=1, Encoding ID=0) 'cmap' subtable | The embedded font program for a non-symbolic TrueType font does not contain Microsoft Symbol (3,1 – Platform ID=3, Encoding ID=1) or the Mac Roman ... |
| 8.4.5.7 | 2 | PDTrueTypeFont | All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the... | A non-symbolic TrueType font encoding does not define a correct mapping to the Adobe Glyph List (Encoding = %1, Encoding entry contains a Differenc... |
| 8.4.5.7 | 3 | PDTrueTypeFont | Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary | A symbolic TrueType font specifies an Encoding entry in its dictionary |
| 8.4.5.7 | 4 | TrueTypeFontProgram | The 'cmap' subtable in the embedded font program shall either contain the Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0 – Platform ID=1, Encoding ID=1) encoding | The embedded font program for a symbolic TrueType font does not contain Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0... |
| 8.4.5.8 | 1 | Glyph | The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 14289-2, 8.4.5.8 | The glyph can not be mapped to Unicode |
| 8.4.5.8 | 2 | Glyph | If a ToUnicode CMap is present, the Unicode values it specifies shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE | The glyph has Unicode value 0, U+FEFF or U+FFFE, which is invalid by Unicode standard |
| 8.4.5.9 | 1 | Glyph | A file in conformance with PDF/UA-2 shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream | The document contains a reference to the .notdef glyph |
### Clause 8.6

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 8.6 | 1 | CosTextString | Text strings intended to be human readable shall not use the Unicode PUA | Text string intended to be human readable uses Unicode PUA |
### Clause 8.7

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 8.7 | 1 | PDOCConfig | All optional content configuration dictionaries in the document, including the default optional content configuration dictionary, shall contain a Name entry (see ISO 32000-2:2020, Table 96) whose value is a non-empty text string when: a) a documen... | Missing or empty Name entry of the optional content configuration dictionary |
| 8.7 | 2 | PDOCConfig | The AS key shall not appear in any optional content configuration dictionary | AS key is present in the optional content configuration dictionary |
### Clause 8.8

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 8.8 | 1 | PDDestination | All destinations whose target lies within the current document shall be structure destinations | Destination in Outline item, OpenAction or Link annotation is not a structure destination |
| 8.8 | 2 | PDGoToAction | All destinations whose target lies within the current document shall be structure destinations | Destination in GoTo action is not a structure destination |
### Clause 8.9

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 8.9.2.2 | 1 | PDAnnot | Annotations shall be artifacts, if the Invisible flag is set in accordance with ISO 32000-2:2020, Table 167 | An invisible annotation is included in logical structure with tag %1 (standard type = %2, Invisible = %3) |
| 8.9.2.2 | 2 | PDAnnot | Annotations shall be artifacts if the NoView flag is set and the ToggleNoView flag is not set in accordance with ISO 32000-2:2020, Table 167 | A no-view annotation is included in logical structure with tag %1 (standard type = %2, NoView = %3, ToggleNoView = %4) |
| 8.9.2.3 | 1 | PDMarkupAnnot | Markup annotations shall be enclosed within Annot structure elements | A Markup annotation is an Artifact or is nested within %1 tag (standard type = %2) instead of Annot |
| 8.9.2.3 | 2 | PDMarkupAnnot | When both RC and Contents entries are present for markup annotation, they shall be textually equivalent | A Markup annotation contains Contents and RC entries with non-equivalent textual values (RC = %1, Contents = %2) |
| 8.9.2.4.7 | 1 | PDRubberStampAnnot | If the Name entry is insufficient to describe the intent of the stamp annotation, a Contents entry describing the author’s intent shall be provided | Rubber stamp annotation contains neither Name nor Contents entry |
| 8.9.2.4.8 | 1 | PDInkAnnot | A Contents entry describing the author’s intent shall be provided for Ink annotation | Ink annotation does not contain Contents entry |
| 8.9.2.4.9 | 1 | PDPopupAnnot | Popup annotations shall not be present in the structure tree | A Popup annotation is included in logical structure with tag %1 (standard type = %2) |
| 8.9.2.4.10 | 1 | PDFileAttachmentAnnot | When a file attachment annotation references a file specification dictionary, the file specification dictionary shall include an AFRelationship entry | The file specification dictionary referenced by a file attachment annotation does not include AFRelationship entry |
| 8.9.2.4.11 | 1 | PDSoundAnnot | Sound annotation cannot be used when conforming to PDF/UA-2 | An annotation of subtype Sound exists |
| 8.9.2.4.11 | 2 | PDMovieAnnot | Movie annotation cannot be used when conforming to PDF/UA-2 | An annotation of subtype Movie exists |
| 8.9.2.4.12 | 1 | PDScreenAnnot | Screen annotations shall include a Contents entry | Screen annotation does not contain Contents entry |
| 8.9.2.4.13 | 1 | PDWidgetAnnot | A widget annotation of zero height and width shall be an artifact | A Widget annotation of zero height and width is not marked as an Artifact (structure tag = %1, standard type = %2) |
| 8.9.2.4.14 | 1 | PDPrinterMarkAnnot | A printer's mark annotation shall be an artifact | A PrinterMark annotation is included in logical structure with tag %1 (standard type = %2) |
| 8.9.2.4.15 | 1 | PDTrapNetAnnot | Trap network annotations shall not be used in files conforming to PDF/UA-2 | An annotation of subtype TrapNet exists |
| 8.9.2.4.16 | 1 | PDWatermarkAnnot | When used as real content, Watermark annotations shall be enclosed within Annot structure elements | A Watermark annotation is nested within %1 tag (standard type = %2) instead of Annot |
| 8.9.2.4.19 | 1 | PD3DAnnot | 3D annotation shall include alternate description in respective Contents entry | 3D annotation does not contain Contents entry |
| 8.9.2.4.19 | 2 | PDRichMediaAnnot | RichMedia annotation shall include alternate description in respective Contents entry | RichMedia annotation does not contain Contents entry |
| 8.9.3.3 | 1 | PDPage | Every page that includes an annotation shall contain a Tabs entry in its page dictionary in accordance with ISO 32000-2:2020, Table 31, and its value shall be A, W or S | A page with annotation(s) contains Tabs key with value %1 instead of A, W or S |
| 8.9.4.2 | 1 | PDAnnot | Where an annotation has a Contents entry and the directly enclosing structure element has an Alt entry, the values of Alt and Contents shall be identical | Both Contents and Alt entries are present for the annotation, but they are not identical (Contents = %1, Alt = %2) |
### Clause 8.10

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 8.10.1 | 1 | PDWidgetAnnot | Each widget annotation shall be enclosed by a Form structure element unless the widget annotation is an artifact | A Widget annotation is nested within %1 tag (standard type = %2) instead of Form or Artifact |
| 8.10.1 | 2 | SEForm | A Form structure element shall contain at most one widget annotation | A Form structure element contains %1 widget annotations |
| 8.10.1 | 3 | PDAcroForm | XFA forms shall not be present | XFA form is present |
| 8.10.2.3 | 1 | PDWidgetAnnot | If a label for a widget annotation is not present, a Contents entry shall be provided to supply description and context for the widget | Widget annotation contains neither label nor Contents entry |
| 8.10.2.3 | 2 | PDWidgetAnnot | If an additional action (AA) entry is present in a widget annotation dictionary, the respective widget's Contents entry shall be present | Widget annotation dictionary contains an AA entry, but does not contain the Contents entry |
| 8.10.3.3 | 1 | PDTextField | For text fields, when an RV entry is present a V entry shall also be present, and they shall be textually equivalent | Text field contains RV entry, but no V entry or they are not textually equivalent (RV = %1, V = %2) |
| 8.10.3.5 | 1 | SEGraphicContentItem | If a portion of the appearance of a signature is represented by a graphic, alternative text shall be provided for that graphic | A portion of the appearance of a signature is represented by a graphic, but alternative text is not provided for that graphic |
### Clause 8.11

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 8.11.1 | 1 | MainXMPPackage | The Metadata stream as specified in ISO 32000-2:2020, 14.3 in the document catalog dictionary shall contain a dc:title entry | Metadata stream does not contain dc:title |
| 8.11.1 | 2 | PDDocument | The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-2:2020, 14.3. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with v... | The document catalog dictionary doesn't contain metadata key or metadata stream dictionary does not contain either entry Type with value /Metadata ... |
| 8.11.2 | 1 | CosDocument | The ViewerPreferences dictionary of the document catalog dictionary shall be present and shall contain at least the DisplayDocTitle key with a value of true, as described in ISO 32000-2:2020, Table 147 | ViewerPreferences dictionary is not present in the document Catalog, or DisplayDocTitle key is set to false or is not present in the ViewerPreferen... |
### Clause 8.14

| Clause | Test | Object | Description | Error Message |
|--------|------|--------|-------------|---------------|
| 8.14.1 | 1 | CosFileSpecification | The Desc entry shall be present on all file specification dictionaries present in the EmbeddedFiles name tree of a conforming document | The file specification dictionary for an embedded file does not contain Desc key |
