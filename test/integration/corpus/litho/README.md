# litho corpus

Self-authored conformance fixtures, committed to git rather than downloaded.

Every other corpus comes from a third party and is dominated by files that
exercise one construct at a time. The veraPDF corpus in particular is
single-rule-per-file. That leaves a coverage hole this corpus fills:

> Does a firing rule mask a rule that should also fire?

Each fixture packs many **independent** violation classes into one small file.
If litho short-circuits, latches, or scopes a check too narrowly, the rule set
it reports diverges from veraPDF's and the fixture exposes it.

The fixtures are tiny, self-authored, and carry no third-party licence, so
committing the PDFs is fine. The pikepdf builders in `build/` regenerate them
byte for byte.

## Layout

```
corpus/litho/
├── download.sh     Provisioning step: copies pdfs/ into the output dir
├── manifest.json   Corpus manifest (profile_mode=expand over 8 profiles)
├── build/          pikepdf builders, one per fixture, plus _common.py
├── pdfs/           The committed fixtures
└── README.md       This file
```

`download.sh` downloads nothing. The provisioning image has no Python, so the
Dockerfile bakes `corpus/litho/` in at `/work/litho` and the script copies
`pdfs/*.pdf` into the output directory.

## Regenerating

```bash
cd test/integration/corpus/litho/build
python3 complex_1b.py # rewrites ../pdfs/complex_1b.pdf
```

Builders save with a deterministic `/ID` and `stream_decode_level=none`, so a
rebuild that changes nothing produces an empty diff. The decode level matters:
qpdf's default silently rewrites an `LZWDecode` stream as Flate, which would
delete a planted violation.

`trailer_2b.pdf` is the one exception. It is encrypted, and qpdf derives a
random file encryption key on every save with no way to pin it, so successive
builds differ inside `/O`, `/U` and every encrypted string. Regenerate that
one deliberately rather than diffing it against a fresh build.

## Fixtures

| Fixture                | Home profile | Focus                                                            |
| ---------------------- | ------------ | ---------------------------------------------------------------- |
| `complex_1b.pdf`       | PDF/A-1b     | Part-1 prohibitions: transparency, embedded files, OCG           |
| `complex_2a.pdf`       | PDF/A-2a     | Level-A tagging: role map, structure hierarchy, language         |
| `complex_2b.pdf`       | PDF/A-2b     | Broadest mix; annotations, optional content, filters             |
| `complex_2u.pdf`       | PDF/A-2u     | Level-U Unicode: absent, forbidden and Private Use mappings      |
| `complex_3b.pdf`       | PDF/A-3b     | Embedded and associated files, one 6.8 test per attachment       |
| `complex_4.pdf`        | PDF/A-4      | Part-4 and PDF 2.0: rev 2020, docinfo, Requirements, PresSteps   |
| `complex_ua1.pdf`      | PDF/UA-1     | Structure and accessibility defects in a wired tag tree          |
| `complex_ua2.pdf`      | PDF/UA-2     | Part-2 specifics: namespaces, PUA text, structure destinations   |
| `forbidden_mix.pdf`    | PDF/A-2b     | Forbidden actions, PostScript tint transforms, LZW               |
| `gstate_2b.pdf`        | PDF/A-2b     | Transfer functions and halftones in six graphics states          |
| `limits_2b.pdf`        | PDF/A-2b     | Implementation limits: real, string, name, nesting, DeviceN      |
| `megafont_2b.pdf`      | PDF/A-2b     | Font embedding and glyph-width scoping                           |
| `multipage_2b.pdf`     | PDF/A-2b     | Three pages, a violation class unique to each                    |
| `fontdict_2b.pdf`      | PDF/A-2b     | Font dictionary required keys; page box implementation limits    |
| `structnest_ua1.pdf`   | PDF/UA-1     | Clause 7.2: table, list and TOC nesting; per-element language    |
| `tables_ua1.pdf`       | PDF/UA-1     | Tables and lists: header association, nested list structure      |
| `tablespan_ua2.pdf`    | PDF/UA-2     | Table cell geometry: row and column spans                        |
| `extschema_2b.pdf`     | PDF/A-2b     | XMP extension schema structure across sibling entries            |
| `schemaprefix_1b.pdf`  | PDF/A-1b     | Extension schema RDF container types and absent fields           |
| `trailer_2b.pdf`       | PDF/A-2b     | Encrypted, with data after the last `%%EOF`                      |
| `limits_1b.pdf`        | PDF/A-1b     | Part-1 limits: integer, real, string, array, dictionary, CID     |
| `docinfo_1b.pdf`       | PDF/A-1b     | Document information dictionary against its XMP analogues        |
| `imgxmp_4.pdf`         | PDF/A-4      | Image dictionary keys; one XMP packet per page, one defect each  |
| `idprefix_2b.pdf`      | PDF/A-2b     | Namespace prefixes on the PDF/A Identification Schema            |
| `uaid_ua1.pdf`         | PDF/UA-1     | The PDF/UA Identification Schema: part number, prefixes          |
| `ttencoding_2b.pdf`    | PDF/A-2b     | TrueType encoding keys in the font dictionary                    |
| `ttcmap_2b.pdf`        | PDF/A-2b     | `cmap` subtable sets in embedded TrueType font programs          |
| `fenote_ua2.pdf`       | PDF/UA-2     | Footnote and endnote structure: type, namespace, `/Ref` pairing  |
| `tableregular_ua2.pdf` | PDF/UA-2     | Table regularity: cell intersection and section row groupings    |
| `artifact_ua1.pdf`     | PDF/UA-1     | Artifact and real-content marking; suspect tagging               |
| `annots_1b.pdf`        | PDF/A-1b     | Annotation dictionary constraints and additional actions         |
| `fontkeys_1b.pdf`      | PDF/A-1b     | Part-1 font dictionary required keys and embedded file subtype   |
| `cmaps_2b.pdf`         | PDF/A-2b     | CMap embedding, WMode consistency and `/UseCMap` chains          |
| `outputintent_2b.pdf`  | PDF/A-2b     | OutputIntent array and ICC destination profile headers           |
| `formstruct_ua2.pdf`   | PDF/UA-2     | Widget annotations, Form structure elements and XFA              |
| `xref_1b.pdf`          | PDF/A-1b     | Cross reference table formatting, patched as bytes               |
| `contentstream_4.pdf`  | PDF/A-4      | Content stream operators and the resources it names              |
| `iccbased_4.pdf`       | PDF/A-4      | ICCBased profiles, overprint mode and the destination profile    |
| `headings_ua1.pdf`     | PDF/UA-1     | Heading structure: one H per node, strong or weak but not both   |
| `formcs_2b.pdf`        | PDF/A-2b     | The colour space a Form XObject inherits at its invoking `Do`    |
| `patterncs_2b.pdf`     | PDF/A-2b     | Colour in a tiling pattern and an annotation appearance stream   |
| `usedcs_2b.pdf`        | PDF/A-2b     | Which declared resources count as colour usage                   |
| `filestruct_1b.pdf`    | PDF/A-1b     | File header, trailer identifier, hex strings and AcroForm        |
| `docsync_1b.pdf`       | PDF/A-1b     | Document information entries against their XMP analogues         |
| `syntax_1b.pdf`        | PDF/A-1b     | Stream length and end-of-line, and interactive actions           |
| `annotkinds_ua2.pdf`   | PDF/UA-2     | Annotation subtypes, each judged by its own accessibility test   |
| `annotstruct_ua2.pdf`  | PDF/UA-2     | Annotations judged by the structure element that encloses them   |
| `a4syntax_4.pdf`       | PDF/A-4      | Part-4 file and catalog rules: syntax, actions, optional content |
| `glyphs_2b.pdf`        | PDF/A-2b     | Composite fonts and the glyphs a page actually paints            |
| `structkinds_ua2.pdf`  | PDF/UA-2     | Structure element kinds: Ruby, Warichu, LI, MathML, root         |
| `xobjstate_2b.pdf`     | PDF/A-2b     | Form XObject keys, blend modes, page actions, Separations        |
| `puatext_ua2.pdf`      | PDF/UA-2     | Private Use Area characters in the strings that describe         |
| `formmedia_ua1.pdf`    | PDF/UA-1     | Formulas, notes, shared XObjects and media clips                 |
| `idfields_ua2.pdf`     | PDF/UA-2     | Identification prefixes, destinations and text fields            |
| `linkfont_ua2.pdf`     | PDF/UA-2     | Link target identity and a symbolic font's character map         |
| `xfatable_ua1.pdf`     | PDF/UA-1     | A dynamic XFA configuration beside a conforming table            |
| `extprop_2b.pdf`       | PDF/A-2b     | The container an extension schema's properties live in           |
| `gstateform_1b.pdf`    | PDF/A-1b     | Graphics states, form XObjects and an ICC component count        |
| `xmpsyntax_1b.pdf`     | PDF/A-1b     | A defect in each of the header, cross reference and XMP layers   |
| `metafilter_1b.pdf`    | PDF/A-1b     | A filtered metadata stream, and one that is allowed to be        |
| `xpacket_1b.pdf`       | PDF/A-1b     | The attributes an XMP packet header may not carry                |
| `encperm_ua1.pdf`      | PDF/UA-1     | An encrypted document that forbids accessible extraction         |
| `sigcms_2b.pdf`        | PDF/A-2b     | One signature per digital-signature verdict, and one conforming  |
| `docmdp_2b.pdf`        | PDF/A-2b     | A certifying signature's references, and a real too small to be  |
| `charset_1b.pdf`       | PDF/A-1b     | What a subset font's `/CharSet` and `/CIDSet` have to list       |
| `linearized_1b.pdf`    | PDF/A-1b     | A linearized file whose two trailers disagree on `/ID`           |
| `jpx_2b.pdf`           | PDF/A-2b     | The JPEG 2000 container boxes a conforming image may carry       |
| `threed_4e.pdf`        | PDF/A-4e     | The 3D stream subtypes the engineering conformance permits       |
| `grouping_ua2.pdf`     | PDF/UA-2     | Both halves of a Table 5 row, chosen by how the parent is used   |
| `resinherit_2b.pdf`    | PDF/A-2b     | Resources and a media box a page inherits from an inner node     |
| `inlineimage_2b.pdf`   | PDF/A-2b     | Content repairs on a page that holds inline images               |

### Must-not-fire controls

Recent litho fixes added usage-scoping guards whose failure mode is
over-suppression. Each control below is a construct that looks like a
violation but is exempt, so a firing is a false positive.

| Fixture                                   | Control                                                                | Grounding                    |
| ----------------------------------------- | ---------------------------------------------------------------------- | ---------------------------- |
| `complex_1b`, `complex_2b`, `megafont_2b` | Type3 font `T3B`: skewed `/Widths` on a glyph that is never rendered   | width rule is usage-scoped   |
| `complex_2b`                              | Zero-area annotation with no appearance stream                         | ISO 19005-2, 6.3.3 exemption |
| `complex_1b`                              | Fully conforming annotation: `F=4`, `/AP` present, no `/C`             | ISO 19005-1, 6.5.3           |
| `complex_1b`                              | File header pinned to `%PDF-1.4`                                       | ISO 19005-1, 6.1.2           |
| `complex_ua1`                             | `pdfuaid:part=1` present and correct                                   | ISO 14289-1, 5               |
| `complex_ua2`                             | `/Figure` carrying `/Alt` beside one that carries none                 | ISO 14289-2, 8.2.5.28.2      |
| `complex_ua2`                             | Well-formed `/Lang` beside a malformed one                             | ISO 14289-2, 8.4.4           |
| `complex_ua2`                             | Invisible annotation outside the structure tree                        | ISO 14289-2, 8.9.2.2         |
| `complex_ua2`                             | `/Document` declaring the PDF 2.0 standard namespace                   | ISO 14289-2, 8.2.5.2         |
| `complex_2a`                              | `/Document`, a standard type absent from the `/RoleMap`                | ISO 19005-2, 6.7.3.4         |
| `complex_2a`                              | Markup annotations wrapped in `/Annot` structure elements              | ISO 14289-2, 8.9.2.3         |
| `complex_2u`                              | `/ToUnicode` onto a plain Latin letter                                 | ISO 19005-2, 6.2.11.7.2      |
| `complex_2u`                              | Unrendered code whose glyph name maps to nothing                       | glyph rules are usage-scoped |
| `complex_2u`                              | Private Use glyph inside a marked-content `/ActualText`                | ISO 19005-2, 6.2.11.7.3      |
| `complex_3b`                              | Attachment correct in all four respects, reachable from `/AF`          | ISO 19005-3, 6.8             |
| `complex_4`                               | File header pinned to `%PDF-2.0`                                       | ISO 19005-4, 6.1.2           |
| `complex_4`                               | Page-level `/OutputIntents`, a legal PDF 2.0 construct                 | ISO 32000-2, 14.11.5         |
| `complex_4`                               | `/AFRelationship` on one file specification and not the other          | ISO 19005-4, 6.9             |
| `complex_4`                               | Second image carrying `/Interpolate false`                             | ISO 19005-4, 6.2.7.1         |
| `structnest_ua1`                          | Fourteenth table: Caption first, one `THead`, one `TBody`, one `TFoot` | ISO 14289-1, 7.2             |
| `structnest_ua1`                          | Fourth list: `L` > `LI` > `Lbl` + `LBody`                              | ISO 14289-1, 7.2             |
| `structnest_ua1`                          | Third table of contents: `Caption` first, then a `TOCI`                | ISO 14289-1, 7.2             |
| `structnest_ua1`                          | `/Span` carrying `/ActualText` beside its own `/Lang`                  | ISO 14289-1, 7.2 t21         |
| `structnest_ua1`                          | Marked-content `/Span` declaring `/Lang` inline                        | ISO 14289-1, 7.2 t30         |
| `structnest_ua1`                          | Form field whose `/TU` sits beside a field `/Lang`                     | ISO 14289-1, 7.2 t25         |
| `tables_ua1`                              | Second table: `TH` with `/Scope` and `/ID`, `TD` with `/Headers`       | ISO 14289-1, 7.5             |
| `multipage_2b`                            | Page 1 annotation: `F=4`, `/AP` present, no `/C`                       | ISO 19005-2, 6.3.3           |
| `fontdict_2b`                             | Fifth font conforming in every required key                            | ISO 19005-2, 6.2.11.2        |
| `extschema_2b`                            | Third schema entry conforming in every field                           | ISO 19005-2, 6.6.2.3.3       |
| `schemaprefix_1b`                         | Seventh schema entry: every field present, its value type defined      | ISO 19005-1, 6.7.8 t9        |
| `schemaprefix_1b`                         | Sixth value type of the sixth entry, conforming in every field         | ISO 19005-1, 6.7.8 t12-t16   |
| `schemaprefix_1b`                         | Third field of the fifth value type, beside two defective ones         | ISO 19005-1, 6.7.8 t17-t19   |
| `schemaprefix_1b`                         | Second property of the fifth schema entry, conforming                  | ISO 19005-1, 6.7.8 t8-t11    |
| `gstate_2b`                               | Sixth graphics state: `/TR2 /Default`, unnamed type 1 halftone         | ISO 19005-2, 6.2.5           |
| `tablespan_ua2`                           | Second table spanning two columns with both headers referenced         | ISO 14289-2, 8.2.5.26        |
| `limits_1b`                               | Integers of `2147483647` and `-2147483648`                             | ISO 19005-1, 6.1.12 t1       |
| `limits_1b`                               | Reals of `32767.0` and `-32767.0`                                      | ISO 19005-1, 6.1.12 t2       |
| `limits_1b`                               | An annotation `/Contents` of exactly 65535 bytes                       | ISO 19005-1, 6.1.12 t3       |
| `limits_1b`                               | An array of exactly 8191 elements                                      | ISO 19005-1, 6.1.12 t5       |
| `limits_1b`                               | A dictionary of exactly 4095 entries                                   | ISO 19005-1, 6.1.12 t6       |
| `limits_1b`                               | A CMap whose maximal CID is exactly 65535                              | ISO 19005-1, 6.1.12 t10      |
| `docinfo_1b`                              | `/Trapped` matching `pdf:Trapped`, an entry 6.7.3 does not name        | ISO 19005-1, 6.7.3           |
| `docinfo_1b`                              | `/Company`, a docinfo entry with no XMP analogue at all                | ISO 19005-1, 6.7.3           |
| `docinfo_1b`                              | Extension schema declaring `pdf:Trapped`, conforming in every field    | ISO 19005-1, 6.7.9           |
| `imgxmp_4`                                | `ImOK`, an image conforming in all four respects its siblings break    | ISO 19005-4, 6.2.7.1         |
| `imgxmp_4`                                | `ImMaskOK`, an image mask with `/BitsPerComponent 1`                   | ISO 19005-4, 6.2.7.1 t5      |
| `imgxmp_4`                                | `/Interpolate false` on every image and every image mask               | ISO 19005-4, 6.2.7.1 t3      |
| `imgxmp_4`                                | The alternate inside the `/Alternates` array, itself conforming        | ISO 19005-4, 6.2.7.1 t1      |
| `imgxmp_4`                                | Page 1 `/Metadata`, a conforming page-level packet                     | ISO 19005-4, 6.7.2.1         |
| `imgxmp_4`                                | Catalog packet: `pdfaid:part` 4, `pdfaid:rev` 2020                     | ISO 19005-4, 6.7.3           |
| `idprefix_2b`                             | `dc:title` and `xmp:CreatorTool` under their canonical prefixes        | ISO 19005-2, 6.6.4           |
| `idprefix_2b`                             | An extension schema conforming in every field beside the defects       | ISO 19005-2, 6.6.2.3.3       |
| `uaid_ua1`                                | `pdfuaid:part` keeps its canonical prefix and fires on value only      | ISO 14289-1, 5 t3            |
| `uaid_ua1`                                | `dc:title` present, with `/ViewerPreferences /DisplayDocTitle`         | ISO 14289-1, 7.1             |
| `ttencoding_2b`                           | A symbolic TrueType carrying no `/Encoding`                            | ISO 19005-2, 6.2.11.6 t3     |
| `ttencoding_2b`                           | A non-symbolic TrueType carrying `WinAnsiEncoding`                     | ISO 19005-2, 6.2.11.6 t2     |
| `ttcmap_2b`                               | A non-symbolic program carrying both a (3,1) and a (1,0) subtable      | ISO 19005-2, 6.2.11.6 t1     |
| `ttcmap_2b`                               | A symbolic program whose single subtable is the Microsoft Symbol (3,0) | ISO 19005-2, 6.2.11.6 t4     |
| `fenote_ua2`                              | An `FENote` whose `/Ref` pairing is complete in both directions        | ISO 14289-2, 8.2.5.14 t2, t3 |
| `fenote_ua2`                              | An `FENote` that refers to nothing and that nothing refers to          | ISO 14289-2, 8.2.5.14 t2, t3 |
| `tableregular_ua2`                        | A sectioned table whose `THead` and `TBody` groupings are regular      | ISO 14289-2, 8.2.5.26 t2     |
| `tableregular_ua2`                        | A flat table carrying a real `/ColSpan` that adds up                   | ISO 14289-2, 8.2.5.26 t3     |
| `tableregular_ua2`                        | A table whose every `TH` carries a `/Scope`, settling determination    | ISO 14289-2, 8.2.5.26 t5, t6 |
| `artifact_ua1`                            | A tagged paragraph and a top-level artifact, neither one nested        | ISO 14289-1, 7.1 t1, t2      |
| `annots_1b`                               | A plain square and a plain widget, both with `/F 4` and an `/N` stream | ISO 19005-1, 6.5.3           |
| `annots_1b`                               | An appearance dict carrying `/D` beside an `/N` of the wrong type      | ISO 19005-1, 6.5.3 t5, t6    |
| `fontkeys_1b`                             | A Helvetica with no `/FirstChar`, `/LastChar` or `/Widths`             | ISO 19005-1, 6.3.2 t4-t6     |
| `cmaps_2b`                                | A font naming `/Identity-H`, and one embedding a self-consistent CMap  | ISO 19005-2, 6.2.11.3.3      |
| `outputintent_2b`                         | A well-formed `prtr`/`RGB ` destination profile beside the bad one     | ISO 19005-2, 6.2.3 t1        |
| `formstruct_ua2`                          | A `Form` element enclosing exactly one widget                          | ISO 14289-2, 8.10.1 t2       |
| `formstruct_ua2`                          | A widget with no `/StructParent`, outside the structure tree           | ISO 14289-2, 8.10.1 t1       |
| `xref_1b`                                 | Every object offset and `startxref` value left valid by the patch      | ISO 19005-1, 6.1.4           |
| `contentstream_4`                         | `/F1`, a name the inherited dictionary does define                     | ISO 19005-4, 6.2.2 t3        |
| `iccbased_4`                              | An RGB ICCBased space, valid and unrelated to the OutputIntent         | ISO 19005-4, 6.2.4.2         |
| `headings_ua1`                            | `Sect2`, a node holding exactly one `H` child                          | ISO 14289-1, 7.4.4 t1        |
| `formcs_2b`                               | `Fm0`, a bare `scn` under an inherited ICCBased space                  | ISO 32000-1, 8.10.2          |
| `formcs_2b`                               | `Fm1`, a form selecting its own ICCBased space before painting         | ISO 19005-2, 6.2.4.3 t4      |
| `patterncs_2b`                            | `P2`, a tiling pattern declared in /Resources but never painted        | ISO 19005-2, 6.2.4.3         |
| `annotkinds_ua2`                          | A `Screen` carrying `/Contents`, beside one that carries none          | ISO 14289-2, 8.9.2.4.12 t1   |
| `annotkinds_ua2`                          | An `Ink` carrying `/Contents`, beside one that carries none            | ISO 14289-2, 8.9.2.4.8 t1    |
| `annotkinds_ua2`                          | A `Stamp` carrying `/Name`, the branch `/Contents` alone leaves unmet  | ISO 14289-2, 8.9.2.4.7 t1    |
| `annotstruct_ua2`                         | A `Link` under a `/Link` element, beside one under a `/P`              | ISO 14289-2, 8.2.5.20 t1     |
| `annotstruct_ua2`                         | Two `Link` annotations onto one element, differing only in zoom        | ISO 14289-2, 8.2.5.20 t2     |
| `annotstruct_ua2`                         | A `Square` whose `/RC` rich text carries the words of its `/Contents`  | ISO 14289-2, 8.9.2.3 t2      |
| `annotstruct_ua2`                         | A file spec declaring `/AFRelationship`, beside one that does not      | ISO 14289-2, 8.9.2.4.10 t1   |
| `annotstruct_ua2`                         | A `Popup` left outside the structure tree                              | ISO 14289-2, 8.9.2.4.9 t1    |
| `annotstruct_ua2`                         | `NoView` accompanied by `ToggleNoView`, which the test permits         | ISO 14289-2, 8.9.2.2 t2      |
| `a4syntax_4`                              | An inline image declaring `/AHx`, one of the permitted filters         | ISO 19005-4, 6.1.9 t1        |
| `a4syntax_4`                              | A third optional content configuration whose `/Name` is distinct       | ISO 19005-4, 6.10 t2         |
| `a4syntax_4`                              | A Separation whose colorant name is plain ASCII                        | ISO 19005-4, 6.1.7 t1        |
| `a4syntax_4`                              | A `/NextPage` named action, one of the four permitted                  | ISO 19005-4, 6.6.1 t2        |
| `a4syntax_4`                              | A widget with no `/A`, beside one that carries an action               | ISO 19005-4, 6.4.1 t1        |
| `glyphs_2b`                               | A composite font painting a present, named glyph                       | ISO 19005-2, 6.2.11.4.1 t2   |
| `glyphs_2b`                               | The missing glyph painted in mode 3, through a font of its own         | ISO 19005-2, 6.2.11.4.1 t2   |
| `structkinds_ua2`                         | A `Ruby` of RB then RT, and one of RB, RP, RT, RP                      | ISO 14289-2, 8.2.5.23 t1     |
| `structkinds_ua2`                         | A `Warichu` of WP, WT, WP                                              | ISO 14289-2, 8.2.5.24 t1     |
| `structkinds_ua2`                         | An `LI` holding `Lbl` and `LBody` rather than content directly         | ISO 14289-2, 8.2.5.25 t2     |
| `structkinds_ua2`                         | A `math` element parented by a `Formula`                               | ISO 14289-2, 8.2.5.29 t1     |
| `xobjstate_2b`                            | A Form XObject carrying no `/OPI`                                      | ISO 19005-2, 6.2.9 t1        |
| `xobjstate_2b`                            | An `ExtGState` naming `/Multiply`, a blend mode PDF defines            | ISO 19005-2, 6.2.10 t1       |
| `xobjstate_2b`                            | A third Separation naming a colorant of its own                        | ISO 19005-2, 6.2.4.4 t2      |
| `puatext_ua2`                             | An `/ActualText` carrying the same words without the PUA character     | ISO 14289-2, 8.4.3 t2        |
| `puatext_ua2`                             | An `/Alt` carrying the same words without the PUA character            | ISO 14289-2, 8.4.3 t3        |
| `puatext_ua2`                             | An outline title carrying no PUA character                             | ISO 14289-2, 8.6 t1          |
| `formmedia_ua1`                           | A `Formula` carrying `/Alt`                                            | ISO 14289-1, 7.7 t1          |
| `formmedia_ua1`                           | A third `Note` whose `/ID` is its own                                  | ISO 14289-1, 7.9 t2          |
| `formmedia_ua1`                           | A form carrying `/StructParents` but painted once                      | ISO 14289-1, 7.20 t2         |
| `formmedia_ua1`                           | A media clip carrying both `/CT` and usable `/Alt` text                | ISO 14289-1, 7.18.6.2        |
| `idfields_ua2`                            | An outline destination naming the `Document` element                   | ISO 14289-2, 8.8 t1          |
| `idfields_ua2`                            | A text field whose `/RV` text equals its `/V`                          | ISO 14289-2, 8.10.3.3 t1     |
| `linkfont_ua2`                            | Two links onto the same element, differing only in zoom                | ISO 14289-2, 8.2.5.20 t2     |
| `linkfont_ua2`                            | Two links onto differing page destinations, never compared             | ISO 14289-2, 8.2.5.20 t2     |
| `linkfont_ua2`                            | The same font program declared non-symbolic                            | ISO 14289-2, 8.4.5.7 t4      |
| `xfatable_ua1`                            | A table whose `/RowSpan` covers exactly the rows it has                | ISO 14289-1, 7.2 t41         |
| `extprop_2b`                              | A schema holding one property in a proper `rdf:Seq`                    | ISO 19005-2, 6.6.2.3.3 t5    |
| `extprop_2b`                              | A schema holding two properties in a proper `rdf:Seq`                  | ISO 19005-2, 6.6.2.3.3 t5    |
| `gstateform_1b`                           | An `ExtGState` whose `/SMask` is `/None`                               | ISO 19005-1, 6.4 t1          |
| `gstateform_1b`                           | An `ExtGState` carrying no halftone origin                             | ISO 19005-4, 6.2.5 t3        |
| `gstateform_1b`                           | A form XObject carrying neither `/OPI` nor PostScript                  | ISO 19005-1, 6.2.5 t1        |
| `gstateform_1b`                           | A PostScript form XObject judged at part 4, which has no such rule     | ISO 19005-4, 6.2.8.1 t1      |
| `grouping_ua2`                            | A grouping `Link` holding exactly one `Caption`                        | ISO/TS 32005, Table 5 t1     |
| `grouping_ua2`                            | A grouping `Link` holding a heading                                    | ISO/TS 32005, Table 5 t1     |
| `gstateform_1b`                           | An ICC profile whose `/N` agrees with its header                       | ISO 19005-1, 6.2.3.2 t2      |
| `gstateform_1b`                           | An ICC `/N` mismatch judged at parts 2 to 4, which have no such rule   | ISO 19005-2, 6.2.4.2 t1      |
| `xmpsyntax_1b`                            | A second page carrying no defect of its own                            | ISO 19005-1, 6.1.2 t1        |
| `xmpsyntax_1b`                            | An x-default alternative inside a packet that does not parse           | ISO 14289-1, 7.2 t33         |
| `metafilter_1b`                           | A page's metadata stream carrying the same `/Filter`                   | ISO 19005-1, 6.7.2 t2        |
| `xpacket_1b`                              | A page's packet header carrying the same two attributes                | ISO 19005-1, 6.7.5 t1 and t2 |
| `encperm_ua1`                             | An otherwise conformant encrypted document, so only `/P` answers       | ISO 14289-1, 7.16 t1         |
| `patterncs_2b`                            | `P1` and `A1`, each selecting its own ICCBased space                   | ISO 32000-1, 8.6.8           |
| `usedcs_2b`                               | `Sh0`, a shading declared in /Resources but no `sh` paints             | ISO 19005-2, 6.2.4.3         |
| `usedcs_2b`                               | `T3Unused`, a Type3 font no `Tf` ever selects                          | ISO 19005-2, 6.2.4.3         |
| `usedcs_2b`                               | `PUnc`, an uncoloured pattern writing colour into its cell             | ISO 32000-1, 8.7.3.3         |
| `filestruct_1b`                           | A hexadecimal string with an even count of hex digits only             | ISO 19005-1, 6.1.6           |
| `filestruct_1b`                           | A widget that does carry an appearance dictionary                      | ISO 19005-1, 6.9 t2          |
| `docsync_1b`                              | `/CreationDate` and `xmp:CreateDate`, same instant, other zone         | ISO 19005-1, 6.7.3 t1        |
| `docsync_1b`                              | `/Author` against `dc:creator` in its `rdf:Seq`                        | ISO 19005-1, 6.7.3 t3        |
| `docsync_1b`                              | `/Subject` empty against a declared, empty `dc:description`            | ISO 19005-1, 6.7.3 t4        |
| `syntax_1b`                               | A `/NextPage` named action, one of the four permitted                  | ISO 19005-1, 6.6.1 t2        |
| `syntax_1b`                               | A second form field carrying no action at all                          | ISO 19005-1, 6.6.1 t3        |
| `sigcms_2b`                               | `sig0`, a signature conforming on all three of 6.4.3's tests           | ISO 19005-2, 6.4.3           |
| `sigcms_2b`                               | `sigempty`, a signature field with no `/V` and so no CMS blob          | ISO 19005-2, 6.4.3 t2 and t3 |
| `sigcms_2b`                               | `text`, a text field the signature tests must not read                 | ISO 32000-1, 12.7.4.1        |
| `docmdp_2b`                               | Second `/Reference` entry, carrying no digest key at all               | ISO 19005-2, 6.1.12 t2       |
| `docmdp_2b`                               | A real of `1` beside the denormal, and a conforming signature          | ISO 19005-2, 6.1.13 t5       |
| `charset_1b`                              | `T1Full` and `CIDFull`, subsets whose entry lists every glyph          | ISO 19005-1, 6.3.5           |
| `charset_1b`                              | `T1Whole` and `CIDWhole`, embedded fonts with no subset prefix         | subset-prefix exemption      |
| `linearized_1b`                           | Both trailers hold two different strings inside one `/ID` array        | ISO 32000-1, 14.4            |
| `jpx_2b`                                  | `ImOK`, a JPEG 2000 image conforming on all five tests                 | ISO 19005-2, 6.2.8.3         |
| `jpx_2b`                                  | `ImWaived`, a forbidden METH under an explicit `/ColorSpace`           | ISO 19005-2, 6.2.8.3         |
| `threed_4e`                               | A `/U3D` and a `/PRC` 3D stream, both permitted values                 | ISO 19005-4, 6.1.6.1 t3      |
| `resinherit_2b`                           | `FR`, the root's font, behind a nearer ancestor's `/Resources`         | ISO 32000-1, 7.7.3.4         |
| `resinherit_2b`                           | `F6` and `CS4`, an ancestor's, behind the page's own `/Resources`      | ISO 32000-1, 7.7.3.4         |
| `resinherit_2b`                           | `F4` and `CS3`, an ancestor's, behind a `/Resources` naming no object  | veraPDF `knownKey` reading   |

Verified counts at the home profile: `6.2.11.5` fires exactly once on
`complex_2b` (the rendered `T3A` glyph, not the unrendered `T3B` one) on both
litho and veraPDF, and `6.3.3` fires exactly three times, skipping the
zero-area annotation.

## Expected firing sets

Counts are distinct rule classes (`clause/test`) reported by litho. litho and
veraPDF agree exactly on the set in every cell except the five marked `*`,
which are covered under [Known divergences](#known-divergences).

| Fixture                | A-1a | A-1b | A-2a | A-2b | A-2u | A-3a | A-3b | A-3u | A-4 | A-4e | A-4f | UA-1 | UA-2 |
| ---------------------- | ---- | ---- | ---- | ---- | ---- | ---- | ---- | ---- | --- | ---- | ---- | ---- | ---- |
| `a4syntax_4.pdf`       | 17   | 15   | 21   | 19   | 19   | 21   | 19   | 19   | 16  | 17   | 18   | 13   | 8    |
| `annotkinds_ua2.pdf`   | 7    | 7    | 8    | 8    | 8    | 8    | 8    | 8    | 5   | 5    | 6    | 8    | 10   |
| `annots_1b.pdf`        | 16   | 14   | 14   | 12   | 12   | 14   | 12   | 12   | 10  | 10   | 11   | 12   | 8    |
| `annotstruct_ua2.pdf`  | 13   | 12   | 12   | 12   | 12   | 14   | 14   | 14   | 10  | 9    | 9    | 8    | 13   |
| `artifact_ua1.pdf`     | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 4   | 4    | 5    | 5    | 3    |
| `charset_1b.pdf`       | 7    | 4    | 7    | 4    | 5    | 7    | 4    | 5    | 3   | 3    | 4    | 9    | 7    |
| `cmaps_2b.pdf`         | 9    | 6    | 10   | 7    | 8    | 10   | 7    | 8    | 8   | 8    | 9    | 12   | 12   |
| `complex_1b.pdf`       | 26   | 23   | 22   | 19   | 20   | 24   | 21   | 22   | 19  | 18   | 18   | 14   | 11   |
| `complex_2a.pdf`       | 18   | 15   | 21   | 17   | 17   | 22   | 18   | 18   | 17  | 17   | 18   | 12   | 11   |
| `complex_2b.pdf`       | 21   | 18   | 23   | 20   | 21   | 25   | 22   | 23   | 19  | 18   | 18   | 14   | 12   |
| `complex_2u.pdf`       | 18   | 15   | 23   | 18   | 20   | 23   | 18   | 20   | 16  | 16   | 17   | 14   | 13   |
| `complex_3b.pdf`       | 20   | 17   | 23   | 20   | 21   | 24   | 21   | 22   | 21  | 20   | 20   | 14   | 10   |
| `complex_4.pdf`        | 20   | 18   | 25   | 23   | 23   | 27   | 25   | 25   | 23  | 23   | 23   | 14   | 9    |
| `complex_ua1.pdf`      | 7    | 6    | 7    | 6    | 6    | 7    | 6    | 6    | 5   | 5    | 6    | 11   | 10   |
| `complex_ua2.pdf`      | 11   | 8    | 16   | 12   | 12   | 16   | 12   | 12   | 8   | 8    | 9    | 18\* | 18\* |
| `contentstream_4.pdf`  | 6    | 4    | 8    | 6    | 6    | 8    | 6    | 6    | 6   | 6    | 7    | 8    | 7    |
| `docinfo_1b.pdf`       | 11   | 8    | 4    | 1    | 2    | 4    | 1    | 2    | 6   | 6    | 7    | 5    | 5    |
| `docmdp_2b.pdf`        | 6    | 4    | 8    | 6    | 6    | 8    | 6    | 6    | 5   | 5    | 6    | 10   | 8    |
| `docsync_1b.pdf`       | 11   | 9    | 6    | 4    | 4    | 6    | 4    | 4    | 6   | 6    | 7    | 9    | 8    |
| `encperm_ua1.pdf`      | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 6    | 2    | 4    |
| `extprop_2b.pdf`       | 7    | 4    | 6    | 3    | 4    | 7    | 4    | 5    | 6   | 6    | 7    | 8    | 7    |
| `extschema_2b.pdf`     | 12   | 9    | 11   | 8    | 9    | 12   | 9    | 10   | 6   | 6    | 7    | 8    | 8    |
| `fenote_ua2.pdf`       | 4    | 3    | 5    | 4    | 4    | 5    | 4    | 4    | 3   | 3    | 4    | 5    | 6    |
| `filestruct_1b.pdf`    | 15   | 13   | 12   | 10   | 10   | 12   | 10   | 10   | 13  | 13   | 14   | 10   | 8    |
| `fontdict_2b.pdf`      | 9    | 7    | 10   | 8    | 8    | 10   | 8    | 8    | 8   | 8    | 9    | 7    | 7    |
| `fontkeys_1b.pdf`      | 13   | 10   | 13   | 10   | 11   | 13   | 10   | 11   | 11  | 11   | 12   | 8    | 8    |
| `forbidden_mix.pdf`    | 14   | 12   | 14   | 12   | 12   | 16   | 14   | 14   | 14  | 13   | 13   | 11   | 9    |
| `formcs_2b.pdf`        | 6    | 4    | 6    | 4    | 4    | 6    | 4    | 4    | 5   | 5    | 6    | 7    | 7    |
| `formmedia_ua1.pdf`    | 8    | 8    | 8    | 8    | 8    | 8    | 8    | 8    | 7   | 7    | 8    | 7    | 5    |
| `formstruct_ua2.pdf`   | 4    | 4    | 6    | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 6    | 7    | 7    |
| `glyphs_2b.pdf`        | 7    | 4    | 8    | 5    | 6    | 8    | 5    | 6    | 6   | 6    | 7    | 10   | 10   |
| `grouping_ua2.pdf`     | 3    | 3    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 5    | 8    |
| `gstate_2b.pdf`        | 8    | 6    | 12   | 10   | 10   | 12   | 10   | 10   | 10  | 10   | 11   | 7    | 7    |
| `gstateform_1b.pdf`    | 9    | 6    | 8    | 5    | 6    | 8    | 5    | 6    | 9   | 9    | 10   | 8    | 7    |
| `headings_ua1.pdf`     | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 4   | 4    | 5    | 5    | 5    |
| `iccbased_4.pdf`       | 5    | 3    | 7    | 5    | 5    | 7    | 5    | 5    | 5   | 5    | 6    | 8    | 7    |
| `idfields_ua2.pdf`     | 6    | 6    | 7    | 7    | 7    | 7    | 7    | 7    | 4   | 4    | 5    | 4    | 6    |
| `idprefix_2b.pdf`      | 13   | 10   | 12   | 9    | 10   | 13   | 10   | 11   | 10  | 10   | 11   | 8    | 7    |
| `imgxmp_4.pdf`         | 10   | 8    | 15   | 13   | 13   | 15   | 13   | 13   | 8   | 9    | 10   | 7    | 6    |
| `inlineimage_2b.pdf`   | 6\*  | 4    | 9\*  | 6    | 7    | 9\*  | 6    | 7    | 6   | 6    | 7    | 10   | 10   |
| `jpx_2b.pdf`           | 4    | 2    | 9    | 7    | 7    | 9    | 7    | 7    | 8   | 8    | 9    | 5    | 6    |
| `limits_1b.pdf`        | 12   | 9    | 9    | 6    | 7    | 9    | 6    | 7    | 4   | 4    | 5    | 11   | 9    |
| `limits_2b.pdf`        | 10   | 8    | 12   | 10   | 10   | 12   | 10   | 10   | 6   | 6    | 7    | 10   | 8    |
| `linearized_1b.pdf`    | 6    | 4    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 7    | 7    |
| `linkfont_ua2.pdf`     | 7    | 6    | 8    | 7    | 8    | 8    | 7    | 8    | 5   | 5    | 6    | 4    | 5    |
| `megafont_2b.pdf`      | 12   | 9    | 13   | 10   | 11   | 13   | 10   | 11   | 10  | 10   | 11   | 12   | 11   |
| `metafilter_1b.pdf`    | 6    | 3    | 6    | 3    | 4    | 6    | 3    | 4    | 6   | 6    | 7    | 8    | 7    |
| `multipage_2b.pdf`     | 12   | 10   | 13   | 11   | 11   | 13   | 11   | 11   | 10  | 10   | 11   | 10   | 9    |
| `outputintent_2b.pdf`  | 6    | 4    | 7    | 5    | 5    | 7    | 5    | 5    | 6   | 6    | 7    | 7    | 7    |
| `patterncs_2b.pdf`     | 5    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 10   | 8    |
| `puatext_ua2.pdf`      | 5    | 5    | 6    | 6    | 6    | 6    | 6    | 6    | 4   | 4    | 5    | 3    | 4    |
| `resinherit_2b.pdf`    | 9    | 6    | 11   | 8    | 9    | 11   | 8    | 9    | 9   | 9    | 10   | 9    | 9    |
| `schemaprefix_1b.pdf`  | 17   | 14   | 18   | 15   | 16   | 18   | 15   | 16   | 6   | 6    | 7    | 8    | 8    |
| `sigcms_2b.pdf`        | 6    | 4    | 9    | 7    | 7    | 9    | 7    | 7    | 5   | 5    | 6    | 10   | 8    |
| `structkinds_ua2.pdf`  | 6    | 5    | 7    | 6    | 6    | 7    | 6    | 6    | 3   | 3    | 4    | 5    | 7    |
| `structnest_ua1.pdf`   | 5    | 4    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 36   | 30   |
| `syntax_1b.pdf`        | 11   | 9    | 11   | 9    | 9    | 11   | 9    | 9    | 9   | 9    | 10   | 12   | 8    |
| `tableregular_ua2.pdf` | 4    | 3    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 8    | 7    |
| `tables_ua1.pdf`       | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 4   | 4    | 5    | 2    | 5    |
| `tablespan_ua2.pdf`    | 3    | 3    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 4    | 4    |
| `threed_4e.pdf`        | 7    | 5    | 8    | 6    | 6    | 8    | 6    | 6    | 5   | 5    | 6    | 11   | 8    |
| `trailer_2b.pdf`       | 7    | 5    | 7    | 5    | 5    | 7    | 5    | 5    | 6   | 6    | 7    | 7    | 7    |
| `ttcmap_2b.pdf`        | 8    | 5    | 9    | 6    | 7    | 9    | 6    | 7    | 6   | 6    | 7    | 9    | 8    |
| `ttencoding_2b.pdf`    | 10   | 7    | 10   | 7    | 8    | 10   | 7    | 8    | 8   | 8    | 9    | 10   | 10   |
| `uaid_ua1.pdf`         | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 6    | 5    | 4    |
| `usedcs_2b.pdf`        | 6    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 7    | 7    |
| `xfatable_ua1.pdf`     | 5    | 5    | 6    | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 6    | 4    | 7    |
| `xmpsyntax_1b.pdf`     | 9    | 7    | 9    | 7    | 7    | 9    | 7    | 7    | 7   | 7    | 8    | 9    | 8    |
| `xobjstate_2b.pdf`     | 8    | 6    | 11   | 9    | 9    | 11   | 9    | 9    | 10  | 10   | 11   | 7    | 7    |
| `xpacket_1b.pdf`       | 8    | 5    | 9    | 6    | 7    | 9    | 6    | 7    | 9   | 9    | 10   | 8    | 7    |
| `xref_1b.pdf`          | 7    | 5    | 6    | 4    | 4    | 6    | 4    | 4    | 5   | 5    | 6    | 7    | 7    |

### complex_1b.pdf at PDF/A-1b

`6.1.10/1` LZWDecode forbidden. `6.1.11/1` filespec `/EF`. `6.1.11/2`
`/EmbeddedFiles` name tree. `6.1.13/1` `/OCProperties`. `6.2.3.3/1` DeviceRGB
with no OutputIntent. `6.2.3.3/2` DeviceCMYK. `6.2.3.3/3` uncalibrated colour.
`6.2.4/3` `/Interpolate true`. `6.2.6/1` reference XObject. `6.2.7/1`
PostScript XObject. `6.2.9/1` invalid `ri`. `6.3.4/1` font not embedded.
`6.3.6/1` glyph-width mismatch. `6.4/2` image `/SMask`. `6.4/3` transparency
group. `6.4/4` `/BM` not Normal. `6.4/5` `/CA` not 1.0. `6.4/6` `/ca` not 1.0.
`6.5.2/1` `/Movie` annotation. `6.5.3/2` annotation flags. `6.5.3/3`
annotation `/C`. `6.6.1/1` JavaScript action. `6.7.2/1` no XMP.

### complex_2b.pdf at PDF/A-2b

`6.1.7.2/1` LZWDecode. `6.2.4.3/2` DeviceRGB. `6.2.4.3/3` DeviceCMYK.
`6.2.4.3/4` DeviceGray. `6.2.6/1` invalid `ri`. `6.2.8/3` `/Interpolate true`.
`6.2.9/3` PostScript XObject. `6.2.10/2` transparency group with no blending
colour space. `6.2.11.4.1/1` font not embedded. `6.2.11.5/1` glyph-width
mismatch. `6.3.1/1` `/Movie` annotation. `6.3.2/2` annotation flags. `6.3.3/1`
no appearance stream. `6.4.2/2` `/NeedsRendering`. `6.5.1/1` JavaScript action.
`6.6.2.1/1` no XMP. `6.8/5` embedded file. `6.9/1` optional content
configuration. `6.9/3` `/Order`. `6.9/4` `/AS`.

### complex_2a.pdf at PDF/A-2a

Level-A additions on top of the part-2 layer. `6.7.3.4/1` `/Unmapped` is in no
role mapping. `6.7.3.4/2` `/MyType` loops through `/Foo` and `/Bar`.
`6.7.3.4/3` the standard type `/Div` is remapped to `/Custom`. `6.7.4/1` a
structure element `/Lang` of `english!`. Part-2 layer: `6.1.7.2/1` LZWDecode.
`6.2.4.3/2` DeviceRGB. `6.2.4.3/3` DeviceCMYK. `6.2.4.3/4` DeviceGray.
`6.2.6/1` invalid `ri`. `6.2.8/3` `/Interpolate true`. `6.2.9/3` PostScript
XObject. `6.2.10/2` transparency group with no blending colour space.
`6.2.11.4.1/1` font not embedded. `6.3.1/1` `/Movie` annotation. `6.3.2/2`
annotation flags. `6.3.3/1` no appearance stream. `6.4.2/2` `/NeedsRendering`.
`6.5.1/1` JavaScript action. `6.9/1` optional content configuration. `6.9/3`
`/Order`. `6.9/4` `/AS`.

### complex_2u.pdf at PDF/A-2u

`6.2.11.7.2/1` a rendered glyph with no Unicode mapping. `6.2.11.7.2/2` a
`/ToUnicode` entry mapping onto U+0000. The rest is the part-2 layer:
`6.1.7.2/1` LZWDecode. `6.2.4.3/2` DeviceRGB. `6.2.4.3/3` DeviceCMYK.
`6.2.4.3/4` DeviceGray. `6.2.6/1` invalid `ri`. `6.2.8/3` `/Interpolate true`.
`6.2.9/3` PostScript XObject. `6.2.10/2` transparency group. `6.2.11.4.1/1`
font not embedded. `6.3.1/1` `/Movie` annotation. `6.3.2/2` annotation flags.
`6.3.3/1` no appearance stream. `6.4.2/2` `/NeedsRendering`. `6.5.1/1`
JavaScript action. `6.6.2.1/1` no XMP. `6.9/1` optional content configuration.
`6.9/3` `/Order`. `6.9/4` `/AS`. The Private Use mapping is silent here and
fires `6.2.11.7.3/1` at PDF/A-2a, which is where that rule lives.

### complex_3b.pdf at PDF/A-3b

One attachment per test, each correct in every other respect: `6.8/1` no MIME
type on the embedded file stream. `6.8/2` no `/UF`. `6.8/3` no
`/AFRelationship`. `6.8/4` an attachment reachable only through the
`/EmbeddedFiles` name tree. Part-2 layer: `6.1.7.2/1` LZWDecode. `6.2.4.3/2`
DeviceRGB. `6.2.4.3/3` DeviceCMYK. `6.2.4.3/4` DeviceGray. `6.2.6/1` invalid
`ri`. `6.2.8/3` `/Interpolate true`. `6.2.9/3` PostScript XObject. `6.2.10/2`
transparency group. `6.2.11.4.1/1` font not embedded. `6.3.1/1` `/Movie`
annotation. `6.3.2/2` annotation flags. `6.3.3/1` no appearance stream.
`6.4.2/2` `/NeedsRendering`. `6.5.1/1` JavaScript action. `6.9/1` optional
content configuration. `6.9/3` `/Order`. `6.9/4` `/AS`.

### complex_4.pdf at PDF/A-4

`6.1.3/4` a document information dictionary with no `/PieceInfo` to license
it. `6.1.3/5` that dictionary holds `/Title` rather than `/ModDate`. `6.2.4.3/2`
DeviceRGB. `6.2.4.3/3` DeviceCMYK. `6.2.4.3/4` DeviceGray. `6.2.5/1` `/TR` in
a graphics state dictionary. `6.2.5/2` `/TR2` other than `/Default`.
`6.2.7.1/3` `/Interpolate true`. `6.2.8.2/1` reference XObject. `6.2.9/2` page
transparency with no output intent and no group colour space. `6.2.10.4.1/1`
font not embedded. `6.3.1/1` `/Movie` is not in the ISO 32000-2 annotation
table. `6.3.3/1` no appearance stream. `6.4.2/2` `/NeedsRendering`. `6.6.1/1`
`/Launch` action, still banned where JavaScript is not. `6.7.3/5` `pdfaid:rev`
is 2019. `6.9/1` no MIME type on an embedded file stream. `6.9/3` an
attachment that is not itself a conforming file. `6.9/4` no
`/AFRelationship`. `6.10/1` optional content configuration with no `/Name`.
`6.10/3` `/Order` missing an optional content group. `6.11/2` `/PresSteps` on
a page. `6.12/1` `/Requirements` in the catalog.

### complex_ua1.pdf at PDF/UA-1

`7.1/3` untagged real content. `7.1/6` circular `/RoleMap`. `7.1/10` no
`/DisplayDocTitle`. `7.2/33` no document `/Lang` for metadata. `7.2/34` no
`/Lang` for page content. `7.3/1` `/Figure` with no `/Alt`. `7.4.2/1` heading
level skip (H1 to H3). `7.18.1/2` annotation with no `/Contents`. `7.18.3/1`
page with annotations and no `/Tabs`. `7.18.5/2` link with no alternate
description. `7.21.4.1/1` font not embedded.

### complex_ua2.pdf at PDF/UA-2

`5/2` `pdfuaid:part` is 1. `5/5` `pdfuaid:rev` is 2023. `8.2.1/2` a structure
element with no `/P`. `8.2.2/1` untagged real content. `8.2.4/1` `/Gamma` is
in a non-standard namespace and in no role mapping. `8.2.4/2` `/MyType` loops
through `/Foo` and `/Bar`. `8.2.4/3` `/Alpha` is role mapped to another type
in its own namespace. `8.2.5.28.2/1` a `/Figure` with no `/Alt`. `8.4.3/1`
Private Use text with no `/ActualText`. `8.4.4/1` no catalog `/Lang`.
`8.4.4/2` a structure element `/Lang` of `english!`. `8.4.5.5.1/1` font not
embedded. `8.7/1` an optional content configuration with no `/Name`. `8.7/2`
`/AS` in the default configuration. `8.8/2` a `/GoTo` action onto a page
destination rather than a structure destination. `8.9.2.2/1` an invisible
annotation that is not an artifact. `8.9.3.3/1` a page with annotations and no
`/Tabs`.

### forbidden_mix.pdf at PDF/A-2b

`6.1.7.2/1` LZWDecode. `6.2.4.3/2` DeviceRGB. `6.2.4.3/4` DeviceGray.
`6.2.4.4/1` DeviceN colorant missing from `/Colorants`. `6.2.9/3` PostScript
XObject. `6.2.11.4.1/1` font not embedded. `6.3.3/1` no appearance stream.
`6.4.2/2` `/NeedsRendering`. `6.5.1/1` forbidden action. `6.6.2.1/1` no XMP.
`6.8/5` embedded file. `6.9/1` optional content configuration.

### megafont_2b.pdf at PDF/A-2b

`6.2.4.3/2` DeviceRGB. `6.2.4.3/3` DeviceCMYK. `6.2.6/1` invalid `ri`.
`6.2.8/3` `/Interpolate true`. `6.2.10/2` transparency group. `6.2.11.4.1/1`
font not embedded. `6.2.11.5/1` glyph-width mismatch on the rendered Type3
glyph. `6.3.1/1` `/Movie` annotation. `6.3.3/1` no appearance stream.
`6.6.2.1/1` no XMP.

## First bug found: PDF/UA-2 clause-5 masking

On its first sweep this corpus found a litho false negative, and it is exactly
the masking class the fixtures exist to detect.

**`complex_ua1.pdf` at PDF/UA-2.** veraPDF fires `5/2` (`pdfuaid:part` is 1,
not 2) and `5/5` (`pdfuaid:rev` is absent, must be `2024`). litho fired only
`5/2`: `checkPDFUAIdentificationIn` in
`pkg/internal/compliance/accessibility_identification.go` returned on the first
clause-5 failure, so the `pdfuaid:rev` check below it never ran when
`pdfuaid:part` was also wrong. veraPDF evaluates both tests independently
against the same XMP package object. Fixed by accumulating clause-5 violations
instead of returning the first; all 40 cells now agree.

Why no downloaded corpus could catch it: every veraPDF conformance fixture
breaks exactly one rule, so none carries both a wrong `pdfuaid:part` and a
missing `pdfuaid:rev`. Re-running the fix against all 138 PDF/UA-2 corpus files
changes nothing. A single-rule corpus is structurally unable to expose an
early return, which is the whole argument for these fixtures.

## The multi-page axis

Every other fixture is a single page, so none of them can catch a page-scoped
check that stops after the first page, or one that answers from page one and
never revisits. `multipage_2b.pdf` isolates that: each page carries a
violation class that appears on no other page.

| Page | Unique to that page                                                                                                                      |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| 1    | `6.2.4.3/3` DeviceCMYK with no output intent                                                                                             |
| 2    | `6.2.6/1` invalid `ri`, `6.2.8/3` `/Interpolate true`                                                                                    |
| 3    | `6.2.9/3` PostScript XObject, `6.2.10/2` transparency group, `6.2.11.4.1/1` font not embedded, `6.3.1/1` + `6.3.3/1` `/Movie` annotation |

Page 1 also carries a conforming counterpart of what fails later: an
annotation with `F=4`, an appearance stream and no `/C`. A check that reads
only page 1 therefore goes quiet rather than firing, so the failure mode is a
missing rule, which the rule-set comparison catches, rather than a wrong one.

litho reports all three pages' classes and agrees with veraPDF on all thirteen
profiles, so no page-scoped check short-circuits today. Remediation takes it
from 11 veraPDF violations to 1, introduces nothing, and preserves all three
pages.

## Tables and lists

No other fixture contains a `/Table` or an `/L`, so ISO 14289-1, 7.5 and the
table and list rows of the ISO/TS 32005 Table 5 matrix are unreached by the
rest of the corpus. Both are nested structures, which is where a check that
stops at the first defect, or one that never descends past the outer element,
hides a second finding.

`tables_ua1.pdf` holds two tables. The first is defective: its `TH` carries
neither `/Scope` nor `/ID` and its `TD` carries no `/Headers`, so the header
association is not determinable and `7.5/1` fires. The second conforms in all
three respects. Its silence is the load-bearing part: a check that latched on
the first table, or reported once per document, would either miss it or
double-count, and the rule-set comparison shows the difference.

The list is `L > LI > Lbl + LBody`, with the second `LI` omitting the `/Lbl`
its sibling carries, so the nesting the Table 5 matrix governs is present
alongside a real asymmetry between siblings.

## Structure nesting and per-element language

ISO 14289-1, 7.2 was the largest unreached cell in the corpus: 37 of its 41
tests fired on no fixture. Despite the clause title it is a mixed clause. It
governs where a table, list and table-of-contents element may sit in the
structure tree, where a `Caption` may sit among its siblings, and whose
natural language covers text that is not page content.
`structnest_ua1.pdf` reaches 35 of the 37.

Nine of the defects are siblings under one `Div`, so a check that reports the
first misplaced element and stops reports one rule where nine are due:

| Element under the `Div` | Rule     |
| ----------------------- | -------- |
| `TR` outside a table    | `7.2/4`  |
| `THead` outside a table | `7.2/5`  |
| `TBody` outside a table | `7.2/6`  |
| `TFoot` outside a table | `7.2/7`  |
| `TH` outside a `TR`     | `7.2/8`  |
| `TD` outside a `TR`     | `7.2/9`  |
| `LI` outside an `L`     | `7.2/17` |
| `LBody` outside an `LI` | `7.2/18` |
| `TOCI` outside a `TOC`  | `7.2/26` |

Thirteen further tables carry one defect each, because the section-count and
child-type tests interact: a table with a `THead` and no `TBody` fails `7.2/14`
whatever else is wrong with it, so a fixture that piled the defects into one
table could not tell a missing report from an absorbed one. `7.2/3` is a
non-table kid, `7.2/10` a non-cell `TR` kid, `7.2/11` and `7.2/12` a duplicate
`THead` and `TFoot`, `7.2/13` and `7.2/14` a `TFoot` and a `THead` with no
`TBody`, `7.2/16` a mid-position `Caption`, `7.2/39` a second one, `7.2/36`,
`7.2/37` and `7.2/38` a section holding a `P`, and `7.2/42` and `7.2/43` the
two directions of an irregular grid. Three lists cover `7.2/19`, `7.2/20` and
`7.2/40`, and two tables of contents cover `7.2/27` and `7.2/28`.

The language half needs the opposite setup: no catalog `/Lang`, so nothing
determines a language document-wide. `7.2/2` fires on an outline entry,
`7.2/25` on a form field `/TU`, `7.2/21`, `7.2/22` and `7.2/23` on structure
elements carrying `/ActualText`, `/Alt` and `/E`, and `7.2/30`, `7.2/31` and
`7.2/32` on the same three properties set on a `/Span` marked-content
sequence. The page shows no glyphs, only artifact and tagged vector content,
so the document-wide `7.2/34` never competes with the per-element tests.

Six controls conform where the defects break: the fourteenth table, the fourth
list and the third table of contents are correct in every respect their
siblings are not, a structure `/Span` carries `/ActualText` beside its own
`/Lang`, a marked-content `/Span` declares `/Lang` inline, and a second form
field carries `/Lang` beside its `/TU`. Every rule above fires exactly once on
both litho and veraPDF, which is what proves the controls silent.

Two tests stay unreached. One cannot be reached at all, the other is a
coverage opportunity:

- `7.2/41` is unreachable under PDF/UA-1. veraPDF sets
  `numberOfColumnWithWrongRowSpan` in two places. The first fires when a cell's
  `/RowSpan` runs past the last row, but the row count is itself computed as
  the maximum of `row index + /RowSpan` over every cell, so the condition can
  never hold. The second is a `/RowSpan` crossing a `THead`, `TBody` or
  `TFoot` boundary, and it is guarded by `isPDFUA2RelatedFlavour`, so a
  PDF/UA-1 run never evaluates it.
- `7.2/15` is reachable, and litho did not implement it when this fixture
  landed. `19c31ab` implemented it, and the two tools now agree on the
  intersecting grid that reaches it. See
  [Closed since, not planted](#closed-since-not-planted).

The other four tests of the clause were already covered: `7.2/24` by
`complex_ua2.pdf` and `limits_2b.pdf`, `7.2/29` by `complex_2a.pdf` and
`complex_ua2.pdf`, `7.2/33` and `7.2/34` by `complex_ua1.pdf`. `7.2/1` and
`7.2/35` do not exist in the profile, which is why the clause holds 41 tests
numbered up to 43.

## Font dictionary keys

A coverage scan against the veraPDF profile showed this corpus reached only a
third of the PDF/A-2b rule set, with the font dictionary requirements of
ISO 19005-2, 6.2.11.2 entirely unvisited. They need no font program, so
`fontdict_2b.pdf` plants them directly.

Its shape is the point. Five fonts are drawn on one page and four carry a
different defect, so a check that reports the first offending font and stops
reports one rule where four are due:

| Font | Defect                                    | Rule         |
| ---- | ----------------------------------------- | ------------ |
| F1   | no `/FirstChar`                           | `6.2.11.2/4` |
| F2   | no `/LastChar`                            | `6.2.11.2/5` |
| F3   | three codes declared, two widths supplied | `6.2.11.2/6` |
| F4   | no `/BaseFont`                            | `6.2.11.2/3` |
| F5   | none                                      | silent       |

`6.2.11.2/3` is why the fixture exists. litho wired that test for part 1 only,
and a unit test asserted the requirement was "PDF/A-1 only". All of
`PDFA-2B.xml`, `PDFA-3B.xml` and `PDFA-4.xml` define it, and the rule
constants were already present but unreferenced, so the gap was a mapping
omission rather than a missing check.

Page 2 is two units square, below the three-unit minimum of ISO 19005-2,
6.1.13, so both edges fail its implementation limit.

## TrueType cmap subtable sets

`ttencoding_2b.pdf` reaches ISO 19005-2, 6.2.11.6 t2 and t3, which read the font
dictionary. Tests t1 and t4 of the same clause are scoped to
`TrueTypeFontProgram` instead, so they need a real embedded program whose 'cmap'
table carries particular (platformID, encodingID) subtables, and no fixture
embedded a TrueType program at all. `build/_truetype.py` emits them, so the
builders still depend on nothing but pikepdf.

t1 judges non-symbolic programs and t4 symbolic ones, so no single font reaches
both. `ttcmap_2b.pdf` draws four:

| Font | Flags        | `cmap` subtables | Rule         |
| ---- | ------------ | ---------------- | ------------ |
| T1   | non-symbolic | (3,0) only       | `6.2.11.6/1` |
| T2   | symbolic     | (1,0) and (3,1)  | `6.2.11.6/4` |
| T3   | non-symbolic | (3,1) and (1,0)  | silent       |
| T4   | symbolic     | (3,0) only       | silent       |

Both defects are `TrueTypeFontProgram` rules, so a check that reports the first
offending program and stops reports one rule where two are due, and the two
conforming programs catch a check that condemns every embedded program once it
sees one.

The part split is real, and the same file exercises it at its other profiles.
Parts 2 and 3 and PDF/UA-1 ask a non-symbolic program for at least one subtable,
and for more than one when a Microsoft Symbol (3,0) is present, since a symbol
subtable supplies no non-symbolic lookup. PDF/A-4 and PDF/UA-2 instead ask for a
Microsoft Unicode (3,1) or a Macintosh Roman (1,0), whatever the count. PDF/A-1
states no non-symbolic requirement at all, and reports T2 under its own symbolic
rule `6.3.7/3` instead, since that part permits exactly one subtable.

Two details are load-bearing and were measured rather than assumed. The programs
carry their glyph names in a `post` table in format 2.0: with format 3.0 a
non-symbolic font whose 'cmap' offers no usable subtable fails the glyph lookup
as well and drags in `6.2.11.4.1/2`, which would mask the 'cmap' verdict. And
the two defective fonts are drawn inside a `q`/`Q` pair, because ISO 32000-1,
8.4.2 and Table 104 make the text font part of the graphics state: a validator
that ignores the restore attributes the codes after it to the wrong font.

litho reported t4 correctly at every part when the fixture was written, and t1
at none of them. The rule constants existed but nothing referenced them, so the
non-symbolic half of the check was absent while the symbolic half was complete.

## Footnotes and endnotes

PDF/UA-2 was the corpus's weakest profile, and ISO 14289-2, 8.2.5.14 was wholly
unreached. It governs how a document marks up notes: PDF 2.0 retires the PDF 1.7
`Note` type in favour of `FENote`, requires the referring content and the note
to point at each other through `/Ref`, and constrains the note's `/NoteType`.
All four tests are independent, so `fenote_ua2.pdf` plants them together:

| Element | Defect                                             | Rule         |
| ------- | -------------------------------------------------- | ------------ |
| N1      | legacy `Note`, bound to the PDF 1.7 namespace      | `8.2.5.14/1` |
| N2      | the paragraph points at the note, note silent      | `8.2.5.14/2` |
| N3      | the note points at the paragraph, paragraph silent | `8.2.5.14/3` |
| N4      | `/NoteType /Sidenote`, `/Ref` complete both ways   | `8.2.5.14/4` |
| N5      | none                                               | silent       |
| N6      | none: refers to nothing, nothing refers to it      | silent       |

The two `/Ref` directions are measured, not read out of the clause, which names
neither. veraPDF calls them `orphanRefs` and `ghostRefs`, and an isolated probe
per direction settles which is which: a referrer pointing at a silent note fires
t2, a note pointing at a silent referrer fires t3, and wiring both directions or
neither conforms. N5 and N6 are those last two cases, kept as controls, because
"absent" and "half-built" are exactly what a naive check would conflate.

Test 1 turns on the namespace rather than the type name. The same `Note`
element claimed in the PDF 2.0 namespace resolves elsewhere and reports 8.2.4 t1
instead, so N1 binds it to the PDF 1.7 namespace deliberately.

litho reported t1 and t4 and neither `/Ref` test when the fixture was written.
`Rule_14289_2_8_2_5_14_t02` and `t03` existed but nothing referenced them, so
both were dead rules; `checkPDFUA2FENoteRefs` wires them.

## Table regularity and section row groupings

`tablespan_ua2.pdf` reaches ISO 14289-2, 8.2.5.26 t3 and records t1 and t2 as
constructs it could not produce. `tableregular_ua2.pdf` produces both.

The intersection t1 needs is not the naive one. A `/RowSpan` of 2 does not
collide with the row below it: the table model shifts that row's cells past the
occupied slot rather than overlapping them, a mistake made three times in this
project. A real intersection needs a row whose own spans reach into a slot a
previous row already carries down into. Table A is
TH(RowSpan 2), TH, TH(RowSpan 2) over a single TH(ColSpan 2), whose second
column lands on the slot the last cell of row 1 occupies.

Test 2 is decided by a section boundary. Table B's `/RowSpan` starts in the
`THead` and reaches into the `TBody`, and the two profiles judge that
differently, which is measured on both tools rather than assumed:

| profile  | verdict                                   |
| -------- | ----------------------------------------- |
| PDF/UA-2 | `8.2.5.26/2`, the grouping is not regular |
| PDF/UA-1 | `7.2/42`, its column rule                 |

PDF/UA-1 draws no section boundary, so it carries the span into the next row,
sees a wider row and reports the column count. litho reported the PDF/UA-2
column rule `8.2.5.26/3` for this table, which was an FP and an FN in one
construct: the wrong test of the right clause. `rowSpanCrossingSection` scopes
the section model to PDF/UA-2 so both profiles now match.

Table E reaches test 5, and getting there needed an order the clause does not
state. Eight arrangements probed blind all stayed silent on both tools; the
bytecode gave the answer. `GFSETable.checkTable` settles header determination in
sequence, and only the last step reports:

1. `checkRegular` fails, or the table has no rows: determination stops.
2. `hasScope` succeeds: determination stops. It succeeds when **every** `TH`
   carries a `/Scope`, so one scoped `TH` silences the whole clause.
3. Otherwise `hasHeaders` runs, and it alone sets `hasConnectedHeader` and fills
   `unknownHeaders`. It skips the cell at row 0 column 0 and stops at the first
   cell it cannot resolve, so one table yields one verdict.

Table E is that shape: regular, its single `TH` unscoped and parked in the
exempt corner, and the cell at row 1 column 1 sharing its row and column with no
`TH`. litho matched none of this. It fired t5 for tables whose `TH` cells were
all scoped, where veraPDF is silent, and stayed silent on the undeterminable
cell, where veraPDF fires: a false positive and a false negative in one clause.

Table F is the other half of the split: the same cell naming a header id no
`TH` declares (t6). It needs one more thing than table E, and the reason is the
last piece of the model. `GFSETH.getDefaultScope` gives an unscoped `TH` an
implicit scope from its position under PDF/UA-2, and returns null under
PDF/UA-1. So a table whose earlier cells rely on that implicit scope resolves
them under PDF/UA-2 and fails an earlier cell under PDF/UA-1, and since the
fallback stops at the first failure the same file reports opposite halves of
the split on the two profiles. Table F therefore gives every cell before the
target an explicit valid `/Headers`, which makes the target the first failure
under both. The file reports t6 at PDF/UA-2 and its PDF/UA-1 spelling 7.5 t2,
and litho matches both.

## Artifacts and real content

ISO 14289-1, clause 7.1 was the largest unreached cluster in PDF/UA-1. It draws
the line between real content and artifacts, and `artifact_ua1.pdf` plants three
independent defects across it:

| Construct                                    | Rule    |
| -------------------------------------------- | ------- |
| an `/Artifact` sequence nested inside a `/P` | `7.1/1` |
| a `/P` sequence nested inside an `/Artifact` | `7.1/2` |
| catalog `/MarkInfo << /Suspects true >>`     | `7.1/4` |
| a plain tagged paragraph                     | silent  |
| a plain top-level artifact                   | silent  |

The two controls matter because the clause is about nesting, not about tags or
artifacts as such: a check that condemns every artifact, or every tagged
sequence, fires on them.

The fixture caught a profile-mapping bug. litho reported `7.1/4`, an
ISO 14289-1 clause, while validating against PDF/UA-2, whose profile carries no
rule against `/Suspects` at all. A comment in `accessibility_helpers.go`
asserted the two parts shared the requirement; they do not.

## Annotations and additional actions

PDF/A-1b was the weakest PDF/A profile in the corpus, with ISO 19005-1, 6.5.3
and 6.6.2 unreached between them. Both are annotation clauses and both are
cheap, so `annots_1b.pdf` takes all nine of their tests at once:

| Object                         | Defect                                    | Rule           |
| ------------------------------ | ----------------------------------------- | -------------- |
| Square                         | `/CA 0.5`                                 | `6.5.3/1`      |
| Square                         | no `/F`, so Print cannot be set           | `6.5.3/2`      |
| Square                         | `/C` array with no RGB OutputIntent       | `6.5.3/3`      |
| Square                         | `/AP` carrying `/D` beside `/N`           | `6.5.3/4`      |
| Widget, `/FT /Btn`             | `/N` is a stream, not a subdictionary     | `6.5.3/5`      |
| Square                         | `/N` is a subdictionary, not a stream     | `6.5.3/6`      |
| Widget                         | `/AA`                                     | `6.6.2/1`      |
| parent form field              | `/AA`                                     | `6.6.2/2`      |
| catalog                        | `/AA`                                     | `6.6.2/3`      |
| Square                         | `/AP` with `/D` beside a wrong-typed `/N` | `6.5.3/4` only |
| a plain Square, a plain Widget | none                                      | silent         |

The clauses split by object, which decides where each defect has to live.
6.5.3 is scoped to `PDAnnot`, so its six tests are six sibling annotations on
one page. 6.6.2 splits across `PDWidgetAnnot`, `PDFormField` and `PDDocument`,
so the widget carries its own `/AA`, a separate parent field carries another,
and the catalog carries the third. Tests 5 and 6 are complementary halves of
one rule about `/N`, so reaching both needs a Btn widget and a non-widget
annotation with their `/N` values swapped.

Test 3 is reachable only because the file declares no OutputIntent: the test
permits `/C` when the destination profile is RGB, and with no output intent
there is nothing to permit it.

The last defective annotation is a must-not-fire control as much as a defect.
Tests 5 and 6 both open with the same escape, `AP != "N"`, which holds for any
appearance dictionary carrying more than `/N`: such a dictionary is already
reported by test 4, and neither `/N`-type test judges it. litho applied that
escape to test 5 alone, so a non-widget with extra keys and a dictionary `/N`
reported test 6 where veraPDF is silent. Neither of the two defects that
compose this case shows it on its own.

At its home profile the file fires 14 rules, the densest in the corpus, and
remediation takes it to 4 without introducing anything.

## Part-1 font dictionary keys

`fontdict_2b.pdf` covers this requirement as ISO 19005-2, 6.2.11.2, which has
four tests. Part 1 states it as ISO 19005-1, 6.3.2 and splits it into seven, so
two of them are reachable from no other fixture: the `/Type` key of the font
dictionary itself and the subtype of an embedded font file.

| Font | Defect                                    | Rule      |
| ---- | ----------------------------------------- | --------- |
| F1   | no `/Type`                                | `6.3.2/1` |
| F4   | no `/FirstChar`                           | `6.3.2/4` |
| F5   | no `/LastChar`                            | `6.3.2/5` |
| F6   | two codes declared, three widths supplied | `6.3.2/6` |
| F7   | `/FontFile3` whose `/Subtype` is `/Type1` | `6.3.2/7` |
| F8   | none: a standard font with no width keys  | silent    |
| F9   | none                                      | silent    |

Tests 4, 5 and 6 exempt a standard font, so the fonts carrying them name a face
outside the standard 14, and F8 plants that exemption as a control: a Helvetica
with none of the three keys conforms, and a check that ignores `isStandard`
fires on it.

Two tests of the clause cannot be reached, and both were measured rather than
assumed:

- **Test 2**, a `/Subtype` outside the five the clause permits. veraPDF builds a
  font object only for those five, so a font naming anything else reaches no
  test at all. Probed with `/Litho`, `/CIDFontType0`, `/CIDFontType2`, `/Type1C`
  and `/MMType1`: all silent.
- **Test 3**, a font with no `/BaseFont`. veraPDF 1.30.2 throws on it at
  PDF/A-1b and abandons the whole run, while the identical file validates at
  PDF/A-2b and reports the part-2 spelling of the same requirement. The
  reduction is a single Type1 font with `/BaseFont` removed.

litho reports both. Neither is planted, because a fixture carrying them would
carry a divergence, and in the second case no verdict at all.

The file also plants a form XObject with an `/F` key, which ISO 19005-1, 6.1.7
forbids: it belongs to no font clause, so a font verdict is visibly separate
from it.

## CMap embedding and UseCMap chains

ISO 19005-2, 6.2.11.3.3 was the largest cluster shared by all four part-2 and
part-3 profiles. It governs the CMaps a Type 0 font uses, and its three tests
are three different questions:

| Font | Construct                                                       | Rule           |
| ---- | --------------------------------------------------------------- | -------------- |
| T1   | names a CMap that is neither predefined nor embedded            | `6.2.11.3.3/1` |
| T2   | embeds a CMap whose dictionary `/WMode` contradicts its program | `6.2.11.3.3/2` |
| T3   | embeds a CMap pulling in another embedded CMap, not predefined  | `6.2.11.3.3/3` |
| T4   | names `/Identity-H`                                             | silent         |
| T5   | embeds a self-consistent CMap pulling in `/Identity-H`          | silent         |

T3's `/UseCMap` references an embedded CMap **stream** rather than naming one,
and that is forced rather than chosen: a `/UseCMap` naming a CMap veraPDF
cannot resolve makes it throw and abandon the run. Only a reference to a real
stream reaches the test.

litho reported nothing for T3. `findNonPredefinedUseCMap` discarded any name
without a hyphen, which is a reasonable guard for a name scraped out of the
CMap program, where a stray token can be picked up, but wrong for a name read
from an embedded CMap's own `/CMapName`: that name is authoritative and usually
carries no hyphen at all. The guard now applies only to scraped names.

The part-1 spelling 6.3.3.3 has no third test, so at PDF/A-1b the file reports
t1 and t2 only, and PDF/A-4 states all three as 6.2.10.3.3.

## Output intents and ICC destination profiles

Every other fixture omits `/OutputIntents`, which is what keeps the colour
clauses reachable elsewhere. This one is the opposite case, and the only fixture
that declares an output intent at all, so it exercises the paths that run only
when a destination profile is present. ISO 19005-2, 6.2.3 was unreached across
all four part-2 and part-3 profiles.

| Intent | Construct                                                   | Rule      |
| ------ | ----------------------------------------------------------- | --------- |
| A      | ICC profile whose device class is `scnr`                    | `6.2.3/1` |
| B      | a second intent whose destination profile is another object | `6.2.3/2` |
| B      | `/S /GTS_PDFX` carrying `/DestOutputProfileRef`             | `6.2.3/3` |

Intent B's own profile is well formed, a `prtr` device class over `RGB `, which
makes it the control for test 1: the verdict lands on the offending profile
rather than on every profile in the array.

The ICC profiles are built by the fixture rather than shipped. These tests read
the profile header, so each is a 128-byte header plus an empty tag table, and
the header carries a real size, an `acsp` signature and a PCS illuminant because
veraPDF parses profile bytes eagerly and throws on a placeholder.

Test 1 is reached through the device class rather than the data colour space.
Both routes fire it, but a profile declaring `Lab ` makes litho report
ISO 19005-2, 6.2.4.3 t4 against the page's device colour operators while veraPDF
does not, so the `scnr` route is the one planted. That divergence is recorded
below rather than built into a fixture.

At PDF/A-1b the same file reports the part-1 spelling 6.2.2 t1 and t2.

## Widget annotations and Form structure

ISO 14289-2, 8.10.1 ties interactive form fields to the structure tree, and it
was the last unreached PDF/UA-2 cluster outside the ones deliberately skipped.

| Construct                                      | Rule       |
| ---------------------------------------------- | ---------- |
| a field widget enclosed by a `P`, not a `Form` | `8.10.1/1` |
| one `Form` element enclosing two widgets       | `8.10.1/2` |
| `/XFA` on the AcroForm                         | `8.10.1/3` |
| a `Form` enclosing exactly one widget          | silent     |
| a widget with no `/StructParent`               | silent     |

The second control is the interesting one. Test 1's predicate passes when the
structure parent type is null, so a widget outside the structure tree entirely
conforms, and the natural reading of the clause, "every widget must sit under a
Form", fires on it.

Widgets reach the tree through `/StructParent` and an `/OBJR` entry in the
element's `/K`, with the parent tree mapping each widget's number back to its
element. Getting that wiring wrong makes every widget look unparented, which
fires test 1 everywhere and masks the rest.

The fixture found a profile split litho had collapsed. Every PDF/A part and
PDF/UA-2 forbid the `/XFA` key outright, but ISO 14289-1, 7.15 forbids only
**dynamic** XFA: its test reads `dynamicRender != 'required'`. litho fired on
the mere presence of the key, so the static XFA that real forms carry reported
7.15 at PDF/UA-1. Eight pdfjs files stopped firing it once the two were
separated.

## Cross reference table formatting

ISO 19005-1, 6.1.4 constrains the bytes of the cross reference table itself: the
`xref` keyword and the subsection header are separated by exactly one EOL
marker, and the header's starting object number and count by exactly one SPACE.
No writer emits either defect, so `xref_1b.pdf` plants both as bytes after the
save, turning `xref\n0 6\n` into `xref\n\n0  6\n`.

The patch is safe because of where it lands. `startxref` records where the table
begins, and inserting bytes inside the table does not move that offset; every
object offset is earlier in the file and unaffected. This is the same technique
`limits_2b.pdf` uses for a value pikepdf will not emit, and both fixtures rebuild
byte for byte.

**Both defects sit on the same header line on purpose, and that is the point of
the fixture.** They are separate tests of one clause, so a scanner that stops at
the first reports one rule where two are due. litho did exactly that: the
surplus EOL left its parser standing on the extra byte, the subsection header
scan then read no digits and gave up, and the spacing defect went unreported.
Either defect alone agreed with veraPDF; only the pair revealed it. The scanner
now steps over surplus EOL markers and whitespace so the header is still read.

Test 3 forbids a cross reference stream, which cannot coexist with a table, so
it needs a file of its own and is not planted here.

## Content stream operators and resources

ISO 19005-4, 6.2.2 is the only clause that judges a content stream as a whole
rather than the objects it references: its operators must be defined, and the
resources it names must be enumerated by a dictionary the stream itself
carries.

| Construct                                                      | Rule      |
| -------------------------------------------------------------- | --------- |
| an undefined operator inside a `BX`/`EX` pair                  | `6.2.2/1` |
| the page carries no `/Resources` and inherits them             | `6.2.2/2` |
| the stream names `/F99`, which no dictionary defines           | `6.2.2/3` |
| the stream also draws with `/F1`, which the dictionary defines | silent    |

Tests 2 and 3 both concern resources and are easy to conflate, so each is
planted separately: one page that inherits its dictionary, and one name that
resolves nowhere. The undefined operator sits inside a compatibility section
because the clause forbids it "even if such operators are bracketed by the
BX/EX compatibility operators", so a check that treats the bracket as an escape
misses it.

The fixture found a content-destroying remediation bug. Adding a `/DefaultCMYK`
colour space to a page that carries no `/Resources` wrote a fresh dictionary
holding only `/ColorSpace`, which **shadowed** the inherited one and made every
inherited font and XObject unreachable: the remediated page lost its `/Font`
entirely. The fixer now starts from the inherited dictionary, which litho
already had a helper to find, and only builds a new one for a page that truly
inherits nothing.

## ICCBased colour spaces

ISO 19005-4, 6.2.4.2 judges the ICC profile an ICCBased colour space carries and
the two ways such a space may conflict with the file's destination profile.
Four spaces are drawn on one page:

| Space | Construct                                               | Rule        |
| ----- | ------------------------------------------------------- | ----------- |
| CS0   | profile version 5.0                                     | `6.2.4.2/1` |
| CS1   | CMYK, stroked with `/OP true` and `/OPM 1`              | `6.2.4.2/2` |
| CS2   | the OutputIntent's own destination profile              | `6.2.4.2/3` |
| CS3   | RGB, `mntr`, version 2.1, unrelated to the OutputIntent | silent      |

Two of the three are easy to trigger by accident, which decides the design.
Test 3 compares the ICCBased profile against the destination one by object
**and by MD5**, so any CMYK space carrying the same bytes fires it. A first
attempt at a control did exactly that: its profile happened to be byte-identical
to the destination, so the "conforming" space reported test 3. CS1 therefore
uses a profile that differs from the destination, or its overprint verdict would
be masked, and CS3 is RGB rather than CMYK so test 3 does not reach it at all.

The profiles are built by the fixture, as in `outputintent_2b.pdf`: a 128-byte
header plus an empty tag table, with a real size, an `acsp` signature and a PCS
illuminant, because veraPDF parses profile bytes eagerly. The preferred-CMM
field carries no meaning for these tests and is used only to make otherwise
identical profiles differ byte for byte.

At PDF/A-2b the same file reports tests 1 and 2, and at PDF/A-1b the part-1
spelling 6.2.3.2 t1.

## Heading structure

ISO 14289-1, 7.4.4 asks two things of a document's headings: no node may hold
more than one `H` child, and a document is either weakly structured with `H` or
strongly structured with `H1` to `H6`, never both.

| Element          | Construct                              | Rule      |
| ---------------- | -------------------------------------- | --------- |
| `Sect1`          | two `H` children under one node        | `7.4.4/1` |
| the `H` elements | a generic `H` beside numbered ones     | `7.4.4/2` |
| `H1`             | a numbered heading beside generic ones | `7.4.4/3` |
| `Sect2`          | exactly one `H` child                  | silent    |

The second requirement is stated **twice, once per object**: test 2 against
`SEH` and test 3 against `SEHn`. A document carrying both kinds therefore fails
both, because each kind offends from the other's point of view.

litho reported only one of them. It picked a structure "mode" from whichever
heading came first and blamed the other kind, with a comment explaining that
firing both would double-report a single defect, and a unit test had pinned
that. Either kind alone agreed with veraPDF; only a document mixing them
revealed it. Six real-world files gained the missing half once the two were
separated, four of them veraPDF's own designed-to-fail files for these tests:
its `7.4.4-t02-fail` files also fail t03, and its `t03-fail` files also fail
t02.

## Extension schemas and the trailer

Two more fixtures come from the same coverage scan. `6.6.2.3.3` was the
largest unreached cluster at 18 rules, and the whole `6.1` file-structure
family was unvisited.

`extschema_2b.pdf` puts three schema entries in one bag. The first is
defective in its own fields (no `pdfaSchema:prefix`), the second has three
sibling properties each defective differently (a category outside
`{external, internal}`, a missing description, a missing name), and the third
carries value type definitions: one `pdfaType` without its description, one
of its `pdfaField` entries without a name, and a second type conforming
throughout. Sibling entries are the point: a check that reports the first bad
schema and stops never reaches the second, and one that condemns the bag on
first sight wrongly reports the conforming parts of the third.

The category is the one field of the clause whose value is constrained rather
than merely required. litho used to check only that `pdfaProperty:category` was
present, so the `sideways` category above was a divergence until `009d8fa`
compared the value the way veraPDF does. Both tools now report it once, as
`6.6.2.3.3/9` at PDF/A-2b and `6.7.8/10` at PDF/A-1b.

The value types matter separately. Half of 6.6.2.3.3 describes `pdfaType` and
`pdfaField`, which no property entry reaches, so a fixture built only from
properties leaves that half unvisited.

`trailer_2b.pdf` is encrypted and carries junk after its last `%%EOF`, so it
reaches `6.1.3/2` and `6.1.3/3`. Encryption is worth having in the corpus for
its own sake: it changes how every string and stream is read, so a reader that
gives up on it reports nothing rather than reporting the encryption.

Both were built after the coverage scan showed the corpus firing 33 of the
144 PDF/A-2b rules; the fixtures added since raised that to 40 before these
two landed.

## Extension schema containers

`extschema_2b.pdf` reaches six tests of the clause and reaches every one of
them the same way, by omitting a required field from a schema, a property or a
field. Twelve more stayed unreached, and they are unreached because they need
the other halves of the same conjunctions. Every test of the clause reads
`isXValid... == true && ...Prefix == "pdfaN"`, so a wrong RDF container type or
an absent field fires it exactly as a wrong namespace prefix does.
`schemaprefix_1b.pdf` takes the container route and the absence route. The
clause is 6.7.8 in ISO 19005-1 and 6.6.2.3.3 in ISO 19005-2/3, so one fixture
closes both cells.

| Construct                                   | A-1b       | A-2b, A-2u, A-3b |
| ------------------------------------------- | ---------- | ---------------- |
| `pdfaExtension:schemas` is an `rdf:Seq`     | `6.7.8/2`  | `6.6.2.3.3/1`    |
| schema entry carrying a `litho:extra` field | `6.7.8/1`  | `6.6.2.3.2/1`    |
| no `pdfaSchema:schema`                      | `6.7.8/3`  | `6.6.2.3.3/2`    |
| no `pdfaSchema:namespaceURI`                | `6.7.8/4`  | `6.6.2.3.3/3`    |
| `pdfaSchema:valueType` holds an `rdf:Bag`   | `6.7.8/7`  | `6.6.2.3.3/6`    |
| property with no `pdfaProperty:valueType`   | `6.7.8/9`  | `6.6.2.3.3/8`    |
| value type with no `pdfaType:type`          | `6.7.8/12` | `6.6.2.3.3/11`   |
| value type with no `pdfaType:namespaceURI`  | `6.7.8/13` | `6.6.2.3.3/12`   |
| value type with no `pdfaType:prefix`        | `6.7.8/14` | `6.6.2.3.3/13`   |
| `pdfaType:field` holds an `rdf:Bag`         | `6.7.8/16` | `6.6.2.3.3/15`   |
| field with no `pdfaField:valueType`         | `6.7.8/18` | `6.6.2.3.3/17`   |
| field with no `pdfaField:description`       | `6.7.8/19` | `6.6.2.3.3/18`   |

The undefined-field test moves clause between the parts. ISO 19005-1 numbers it
`6.7.8/1` alongside the rest; ISO 19005-2/3 split it out as `6.6.2.3.2/1`.

The grouping is the masking probe. Seven schema entries share the one
container, and the sixth entry holds six sibling value types in a single
`rdf:Seq`: four break a different field or container of their own, the fifth
carries the two defective fields, and the sixth conforms. A check that reports
the first offending value type and stops reports one rule where four are due,
and a check that condemns a container once a member fails wrongly reports the
sixth. The `rdf:Seq` around the schemas is load-bearing in the same way:
veraPDF reports it and still descends, so the container defect and the twelve
entry defects appear side by side.

Four controls conform where their siblings break. The seventh schema entry is
complete in every field, and its property names a value type the same entry
defines, so `isValueTypeDefined` is exercised in the passing direction beside
the entries that break everything else. The sixth value type and the third
field of the fifth are conforming siblings of defective ones, and the second
property of the fifth schema entry is a conforming sibling of the property that
declares no value type. Every rule above fires exactly once on both litho and
veraPDF, which is what proves the four controls silent.

The page shows one line of text rather than nothing. An empty content stream
presents no text item, and ISO 14289-1, 7.2 t34 takes a text item as its
subject, so a blank page used to make litho and veraPDF disagree for a reason
that has nothing to do with extension schemas. `6f22ce2` closed that, and a
text-free page now agrees too. The line stays because it is what makes both
tools report `7.2/34` once at PDF/UA-1: without it both go quiet, and the
fixture covers one cell less.

`6.7.8/6` stays unreached, and so does the wrong-prefix half of every test.
The first is a litho gap, described under
[Known divergences](#known-divergences). The second is not: `591cb06` brought
the two tools into agreement on it, so it is a coverage opportunity, described
under [Closed since, not planted](#closed-since-not-planted).

## Graphics states

ISO 19005-2, 6.2.5 forbids transfer functions and constrains halftones, and
every one of its tests lives in an `ExtGState`, so the clause is authorable
without unusual content. `gstate_2b.pdf` applies six states on one page:

| State | Defect                                    | Rule      |
| ----- | ----------------------------------------- | --------- |
| GS1   | `/TR` present                             | `6.2.5/1` |
| GS2   | `/TR2` with a value other than `/Default` | `6.2.5/2` |
| GS3   | `/HTP` present                            | `6.2.5/3` |
| GS4   | halftone of type 6                        | `6.2.5/4` |
| GS5   | type 1 halftone carrying `/HalftoneName`  | `6.2.5/5` |
| GS6   | none                                      | silent    |

All six tests of the clause fire, including `6.2.5/6` on the colorant and
transfer function pairing, and the conforming state stays silent. Five
different states carrying five different defects is the point: a check that
reported the first and stopped would report one rule where five are due.

## Implementation limits

ISO 19005-2, 6.1.13 scatters its tests across object types rather than
gathering them in one dictionary, so `limits_2b.pdf` plants each in a
different part of the file:

| Defect                                  | Rule       |
| --------------------------------------- | ---------- |
| a real beyond +/-3.403e38               | `6.1.13/2` |
| annotation `/Contents` of 40000 bytes   | `6.1.13/3` |
| resource key name longer than 127 bytes | `6.1.13/4` |
| content nesting `q` 30 deep             | `6.1.13/8` |
| DeviceN naming 33 colorants             | `6.1.13/9` |

Test 2 needs a byte patch. No writer emits a real that large: pikepdf renders
`1e45` as a 46 digit integer, and veraPDF then judges it under the integer
limit t1 rather than the real one. The builder therefore rewrites the token
after saving, replacing its last two digits with `.0`. The token keeps its
length, so every cross reference offset stays valid, and the value is still far
above the limit. Both tools report t2 and neither reports t1.

Three tests stay uncovered on purpose. `t7` needs more than 8388607 indirect
objects and `t10` a CMap with an out-of-range CID, neither of which belongs in
a small fixture. `t5` needs a real closer to zero than `1.175e-38`, and pikepdf
normalises such a denormal to plain `0`, which the test explicitly permits; the
byte patch does not help there, because a denormal written long enough to patch
is a different value.

## Table cell geometry

ISO 14289-2, 8.2.5.26 constrains the grid a table's cells describe through
`/RowSpan` and `/ColSpan`. `tables_ua1.pdf` covers header association but
declares no spans, so none of this clause is reachable from it.

`tablespan_ua2.pdf` holds two tables. The first declares a two column grid
and then gives its second row a cell spanning three, so the row's spans no
longer add up and `8.2.5.26/3` fires. The second is a genuine spanning
layout whose spans do add up and whose cells all reference a header; it
carries real spans rather than a trivial grid, so its silence shows the check
accepts a legitimate span rather than ignoring the attribute.

Only `t3` of the clause is reached. `t1` needs two cells claiming one grid
position, which a `/RowSpan` alone cannot produce: under the table model a
spanning cell shifts the following row's cells rather than colliding with
them, and veraPDF agrees the result is not an intersection. A `/RowSpan` and a
`/ColSpan` together do produce one, but litho does not implement the PDF/UA-2
test, so planting it would add a disagreeing cell. See
[Known divergences](#known-divergences). `t2` and `t4` need span geometries
these builders could not produce, and `t5`/`t6` are the header association
`tables_ua1.pdf` already covers.

## Part-1 implementation limits

Part 1 and part 2 cap different things, so `limits_2b.pdf` reaches none of
ISO 19005-1, 6.1.12 beyond the three tests the two parts share. Its
40000-byte string breaks the part-2 cap of 32767 and sits well inside the
part-1 cap of 65535, and the array and dictionary caps exist in part 1 only.
`limits_1b.pdf` plants the rest of the cluster:

| Defect                                 | Rule        |
| -------------------------------------- | ----------- |
| integers of 2147483648 and -2147483649 | `6.1.12/1`  |
| reals of 32767.5 and -32767.5          | `6.1.12/2`  |
| annotation `/Contents` of 65536 bytes  | `6.1.12/3`  |
| an array of 8192 elements              | `6.1.12/5`  |
| a dictionary of 4096 entries           | `6.1.12/6`  |
| a CMap whose maximal CID is 65536      | `6.1.12/10` |

Every breach sits one step past its limit and every one is paired with a
control sitting exactly on it, in its own object, so an off-by-one in either
direction shows as a wrong firing count rather than as silence. veraPDF
reports two checks under `6.1.12/1` and two under `6.1.12/2`, one under each
of the rest, and litho reports the same, so all six controls are silent on
both tools.

The four numeric and container pairs are sibling entries of one page-piece
dictionary, and that bag is the masking probe: a check that reports the first
breach in it and stops reports one rule where four are due. PDF defines no
standard dictionary that carries an arbitrary number, and ISO 32000-1, 14.5
reserves `/PieceInfo` for private application data, which is why the probes
live there rather than under invented page keys. The two strings are sibling
annotations, conforming in every other respect so 6.5.3 stays out of the way,
and the two CMaps are sibling fonts.

The overflowing CMap range is one the content never renders. Both fonts draw
codes the CMap maps to low CIDs, and the range that overshoots is `<F000>` to
`<F0FF>`, which nothing selects. A rendered CID above 65535 additionally
trips litho's part-1 glyph-presence check on a font that embeds no font
program, which veraPDF exempts, and this fixture is about implementation
limits rather than that divergence.

`t7` stays unreached on purpose: it needs more than 8388607 indirect objects,
whose cross-reference table alone runs to 168 MB at 20 bytes an entry. `t4`,
`t8` and `t9` are the name, nesting and DeviceN tests `limits_2b.pdf` already
reaches at PDF/A-1b, which leaves `t7` as the only test of the clause no
fixture covers.

Two part-2 tests come free. ISO 19005-2, 6.1.13 t1 shares part 1's integer
range and its t10 shares the CID cap, so both fire here at PDF/A-2b, where
`limits_2b.pdf` reaches neither. Part 2 caps a real at 3.403e38 and a string
at 32767, so `6.1.13/3` fires on both strings at that profile and the reals
stay silent.

### Document information dictionary and XMP

ISO 19005-1, 6.7.3 requires every entry of the document information
dictionary that is present to equal its XMP analogue. The clause holds eight
tests, one per entry, all scoped to the information dictionary itself. No
other fixture carries an information dictionary beside an XMP packet, so all
eight were unreached. `docinfo_1b.pdf` reaches all eight.

Every entry is mismatched at once, so a check that reports the first
offending entry and stops reports one rule where eight are due. Each
mismatch is a different kind, so the fixture probes how the two values are
compared and not only whether they are compared:

| Entry           | Mismatch                                      | Rule      |
| --------------- | --------------------------------------------- | --------- |
| `/CreationDate` | same wall clock, `+01:00` against `Z`         | `6.7.3/1` |
| `/Title`        | the same letters in a different case          | `6.7.3/2` |
| `/Author`       | value matches, `dc:creator` holds two entries | `6.7.3/3` |
| `/Subject`      | an unrelated value                            | `6.7.3/4` |
| `/Keywords`     | the same three keywords in a different order  | `6.7.3/5` |
| `/Creator`      | present here, absent from the packet          | `6.7.3/6` |
| `/Producer`     | one trailing space                            | `6.7.3/7` |
| `/ModDate`      | same wall clock, `+02:00` against `-05:00`    | `6.7.3/8` |

`t3` is the one test that is not a plain inequality. Its condition is
`Author == XMPCreator && XMPCreatorSize == 1`, so `dc:creator` carries
`/Author` verbatim as its first entry and a second entry behind it. Only the
size condition fails, which a check that compares values alone cannot see.

The page is empty on purpose. Colour, font and content rules would add
classes that say nothing about metadata synchronisation. At the home profile
the fixture fires the eight tests of the clause and nothing else, on both
litho and veraPDF, one check each.

Reaching all eight tests needs all eight entries mismatched, so the controls
come from a second dimension. `/Trapped` matches `pdf:Trapped` exactly and no
test of 6.7.3 names it, so a firing means the check generalised past the
entries the clause lists. `/Company` has no XMP analogue at all, so a firing
means the check condemns every unmatched entry. `pdf:Trapped` is not one of
the three properties the predefined Adobe PDF schema carries, so 6.7.9
requires a declaration for it: the packet ends with an extension schema that
conforms in every field, and its silence is the third control. Without that
declaration veraPDF and litho both report `6.7.9/2` and `6.7.9/3` at
PDF/A-1b, and the part-2 and part-3 profiles split on the same property, so
the declaration keeps an XMP schema clause out of a fixture about
synchronisation.

Remediation takes the file from 8 veraPDF violations to 0 at PDF/A-1b,
introduces no class at any profile, and preserves the page.

Two cells disagreed when the fixture landed. Both were litho bugs it found,
and both are fixed.

**`6.7.3/7` and `6.7.3/8` at PDF/A-4.** litho reported them, veraPDF reported
nothing. `PDFA-4.xml` defines clause 6.7.3 with six tests, `t1` to `t6`, all
of them PDF/A identification checks on `PDFAIdentification` and
`MainXMPPackage`. ISO 19005-4 states no docinfo-versus-XMP consistency
requirement at all: 6.1.3 already reduces the information dictionary to
`/ModDate` behind a `/PieceInfo`. litho carried
`Rule_19005_4_6_7_3_t07` and `t08` in `rules_extra.go`, described there as
"litho-specific rules with no standalone profile entry", and
`xmpDocInfoSyncRulePart4` in `pkg/internal/compliance/xmp_docinfo.go` routed
every part-4 sync mismatch to them. The two rule numbers collided with a real
clause that means something else, and their `Object` read `CIDSystemInfo`.
`da2cec0` dropped them. The fixture now reports `6.7.3/2`, `/3` and `/5` at
PDF/A-4, the three identification tests, on both tools.

**`7.2/34` at PDF/UA-1.** litho reported it, veraPDF reported nothing. The
rule's object is `SETextItem` and its test is
`gContainsCatalogLang == true || Lang != null`, so its subject is a
structure element that owns a text-showing operator. This fixture has no
structure tree and its page shows no text, so veraPDF has no subject.
`checkStructureTreeLangCoverage` in
`pkg/internal/compliance/accessibility_lang.go` returned
`fail(langRule, "catalog")` as soon as the catalog carried no
`/StructTreeRoot`, without asking whether the document showed any text.
`6f22ce2` made a text item a precondition. Both tools now stay quiet here,
and two files differing only in their content stream confirm the boundary:
with one `Tj` both report `7.2/34`, with an empty stream neither does.

### Image dictionaries and XMP packets

A coverage scan put PDF/A-4 second from the bottom: 43 of the 109 rules in
`PDFA-4.xml` fired on the corpus. Two clusters explain most of the gap.
ISO 19005-4, 6.2.7.1 governs the image XObject dictionary and only its
`/Interpolate` test was reached, by `complex_4.pdf`. ISO 19005-4, 6.7.2.1
governs the XMP packet and only its "the catalog has a metadata stream" test was
reached. `imgxmp_4.pdf` closes the other eight. It takes litho from 43 rules to
47 and veraPDF from 43 to 51; the difference between those two numbers is the
divergence below.

Four sibling images carry one defect each, so a check that reports the first
offending image and stops reports one rule where four are due:

| XObject    | Defect                            | Rule        |
| ---------- | --------------------------------- | ----------- |
| `ImAlt`    | `/Alternates`                     | `6.2.7.1/1` |
| `ImOPI`    | `/OPI`                            | `6.2.7.1/2` |
| `ImBPC`    | `/BitsPerComponent 3`             | `6.2.7.1/4` |
| `ImMask`   | image mask, `/BitsPerComponent 2` | `6.2.7.1/5` |
| `ImBase`   | none, carries `/Mask ImMask`      | silent      |
| `ImOK`     | none                              | silent      |
| `ImMaskOK` | none                              | silent      |

`ImMask` is one object reached two ways. The two validators disagree about what
an image mask is: litho reads `/BitsPerComponent` on any image XObject whose
`/ImageMask` is true, and veraPDF reads it only on an image it reaches through
another image's `/Mask`, because `getisMask` returns false for every plain
`PDXImage` and true only for a `PDMaskImage`. `ImMask` is drawn from the page
resources and named as the `/Mask` of `ImBase`, so one object with one defect
sits in both models and both tools report it once. 2 is a legal
`/BitsPerComponent` for a regular image, so the defect stays inside t5 rather
than also failing t4 on the drawn instance.

Every image uses CalRGB, every image mask is painted through an explicit CalGray
fill colour, and the file declares no fonts, so ISO 19005-4, 6.2.4.3 and the
font clauses stay out of the way. At PDF/A-4 the fixture fires four rules and
nothing else.

The XMP half needs the opposite of a single loaded object. All four tests scope
to veraPDF's `XMPPackage`, and one packet carrying all four defects would
cascade: a packet that is not valid UTF-8 does not parse either, so the
header-attribute tests would have nothing to read and the identification schema
would go down with it. A PDF may carry a `/Metadata` stream on the catalog and
on every page, and veraPDF models each one as its own `XMPPackage`, so each
defect gets a packet of its own:

| Page | Packet                                     | Rule                     |
| ---- | ------------------------------------------ | ------------------------ |
| 1    | conforming                                 | silent                   |
| 2    | `bytes="1024"` in the `<?xpacket?>` header | `6.7.2.1/2`              |
| 3    | `encoding="UTF-8"` in the header           | `6.7.2.1/3`              |
| 4    | end tag differing in case                  | `6.7.2.1/4`, `6.7.2.1/5` |
| 5    | UTF-16BE                                   | `6.7.2.1/5`              |

Page 4 fires two rules, and that is both tools agreeing rather than one of them
over-reporting: a packet that does not parse has no valid serialization and no
determinable encoding. `6.7.2.1/5` therefore fires twice, once with an encoding
of `null` and once with `UTF-16BE`. The catalog packet stays conforming, because
the document has to keep declaring PDF/A-4.

Page-level packets are also the only packets these builders can plant a header
defect in. `pikepdf.Pdf.save` reserialises the catalog packet through its own
XMP writer, which repairs a `bytes=` attribute, an `encoding=` attribute, broken
markup and a UTF-16 encoding alike. Page-level metadata streams pass through
untouched.

At PDF/A-1b the cluster is unreachable and both tools agree. `GFPDMetadata`
builds a non-main `XMPPackage` only when the flavour is not part 1, so under a
PDF/A-1 flavour veraPDF models the catalog packet and nothing else. The part-1
spellings of these tests, `6.7.5/1`, `6.7.5/2` and `6.7.9/1`, stay silent on
both sides.

### For the Known divergences section

**`6.7.2.1/2`, `/3`, `/4` and `/5` at PDF/A-4, and their part-2 and part-3
spelling `6.6.2.1/2` to `/5` at PDF/A-2a, PDF/A-2b, PDF/A-2u and PDF/A-3b.**
veraPDF reports all four on `imgxmp_4.pdf`; litho reports none. ISO 19005-4,
6.7.2.1 t4 reads "all content of all XMP packets located in any metadata stream
present in the PDF", and veraPDF matches that: `GFCosDict` builds a `PDMetadata`
for every dictionary carrying a `/Metadata` key whose `/Type` is not `/Catalog`,
so a page-level packet is an `XMPPackage` like any other. litho reads
`catalog["Metadata"]` and nothing else: `xmpLoadMetadata` in
`pkg/internal/compliance/xmp.go` is the only entry point, and every packet check
hangs off its return value. The implementation is right and its scope is wrong,
which an A/B pins down: the same `bytes=` attribute in the catalog packet fires
`6.7.2.1/2` on both tools, and in a page packet on veraPDF alone. litho has the
gap.

**`7.2/34` at PDF/UA-1.** litho reports it on `imgxmp_4.pdf`, veraPDF does not.
The rule's subject is veraPDF's `SETextItem` and this fixture shows no glyphs,
only image XObjects, so there is no text whose natural language could be
undetermined. `checkStructureTreeLangCoverage` in
`pkg/internal/compliance/accessibility_lang.go` returns the rule unconditionally
when the catalog has no `/StructTreeRoot`, without first asking whether the
document draws any text. Adding one `Tj` to the fixture makes veraPDF report it
too, which is the whole difference between the two files. Every other fixture in
this corpus draws text, so none of them could show this. litho has the gap.

## Known divergences

**`6.2.4.3/4` on a `Lab ` destination profile.** litho reports the device
colour-space rule against a page whose output intent declares an ICC profile
with a `Lab ` data colour space; veraPDF does not. The reduction is a single
page painting `0 0 0 rg` under one output intent whose profile header carries
`prtr` and `Lab `. Both tools agree once the profile declares `RGB `, `CMYK` or
`GRAY`, so the disagreement is about whether a Lab destination profile
establishes a device colour space at all. Not planted in any fixture.

**`6.2.4.3/2` when an uncoloured pattern is painted.** litho reports DeviceRGB
used inside a coloured (`/PaintType 1`) tiling pattern; veraPDF does not, but
only when the same page also paints an uncoloured (`/PaintType 2`) pattern.
Painting one appears to abandon veraPDF's pattern colour traversal for the whole
page: no `PDTilingPattern` context appears anywhere in its report, so the device
colour inside every sibling coloured pattern goes unexamined.

The minimal pair is two files differing by one content line. Both declare a
coloured pattern whose cell is `0.2 0.4 0.8 rg 5 5 40 40 re f`, painted through
`[/Pattern]`, with no OutputIntent and no `/DefaultRGB`:

| file                                     | veraPDF     | litho       |
| ---------------------------------------- | ----------- | ----------- |
| coloured pattern painted alone           | `6.2.4.3/2` | `6.2.4.3/2` |
| the same, plus an uncoloured one painted | silent      | `6.2.4.3/2` |

Confirmed on a real file as well: `pdfjs/tiling_patterns_variations.pdf` paints
four coloured patterns carrying `rg` and one uncoloured pattern. Deleting the
uncoloured pattern makes veraPDF report the rule; reducing the page to a single
coloured fill while leaving the uncoloured pattern declared but unpainted also
makes it report. Only painting the uncoloured one suppresses it.

litho is right on the spec: ISO 19005-2, 6.2.4.3 constrains DeviceRGB wherever
content paints with it, and a coloured pattern's cell supplies its own colour
(ISO 32000-1, 8.7.3.3). No code changed and no fixture plants it. Same treatment
as the `7.1/12` entry below.

**`7.1/12` at PDF/UA-1.** litho reports a structure element missing its `/P`
entry; veraPDF cannot. `GFPDStructElem.getcontainsParent` returns
`getParent() != null`, and `PDStructElem.getParent` builds its result from
`getKey(ASAtom.P)`, which yields an empty but non-null `COSObject` for an absent
key, so `containsParent` is true whatever the dictionary says. Three probes
confirm it: an element with no `/P` at all, one whose `/P`-less parent breaks
its subtree, and a content-free `/P`-less sibling all leave the test silent,
while the first two report `7.1/3` instead because the missing link untags their
content. litho is right on the spec, ISO 32000-1, 14.7.2, Table 355 requires
`/P`, so no code changed and no fixture plants it. Same shape as the `6.1.9`
entry below.

Two cells in the table above disagree, both on `complex_ua2.pdf` and both
rooted in the same construct: the structure element that carries no `/P`.

**`8.2.1/2` at PDF/UA-2 and `7.1/12` at PDF/UA-1.** Both rules read
`containsParent == true`. litho reports the element; veraPDF reports nothing.
veraPDF drops a `/P`-less element from the structure tree instead of
validating it, which its own output confirms: it reports that element's
marked content under `8.2.2/1` as untagged real content. ISO 14289-2, 8.2.1
requires the `/P` entry, so litho is right and veraPDF has the gap. The
construct stays because it is the only thing in the corpus that reaches
either rule.

**`7.2/34` at PDF/UA-1.** veraPDF reports it, litho does not. This one is a
litho gap, not a veraPDF one. The masking bug behind it is fixed; what
remains is a separate facet of the same rule, described under
[Second bug found](#second-bug-found-pdfua-1-language-coverage-fixed-one-facet-remains).

Three more cells disagree, all on `inlineimage_2b.pdf` and all on the same
construct: an unusable `/Lang` on a marked-content property list in a document
with no structure tree.

**`6.8.4/1` at PDF/A-1a, `6.7.4/1` at PDF/A-2a and PDF/A-3a.** veraPDF reports
the inline `/Span <</Lang (-pt)>> BDC`, litho does not. ISO 19005-2, 6.7.4
holds a `/Lang` "in the document's Catalog dictionary or in a structure element
dictionary or property list" to a language identifier, and veraPDF judges every
such entry as a `CosLang`, the one in a content stream's property list included.
litho's check scans page property lists too, inline and named, but
`checkPDFALangValid` in `pkg/internal/compliance/structure.go` returns as soon
as the catalog has no `/StructTreeRoot`, before it reaches them. The clause
does not depend on the structure tree, so litho has the gap. A remediation hides it: the
first round adds a structure tree, the revalidation then reports the entry, and
the marked-content repair removes it, so both tools agree on every remediated
output.

One litho gap and one veraPDF gap sit outside the corpus, because planting
either would add a disagreeing cell rather than coverage.

**`6.1.9/1`, an `endobj` with no preceding EOL, where litho is right.** The
clause reads "The object number and endobj keyword shall each be preceded by an
EOL marker". An indirect object holding a bare integer, written
`56 0 obj` then `20184endobj`, breaks it: `endobj` follows the digit `4`
directly. litho reports it. veraPDF reports nothing, and the difference is the
object's shape rather than the defect: the same missing EOL on a dictionary
object draws `6.1.9/1` from both tools, while on an indirect integer only litho
reports. veraPDF builds no `CosIndirect` for the scalar, so its
`spacingCompliesPDFA` never runs. The clause names no object shape, so litho is
right and veraPDF under-reports, the same way it drops a `/P`-less structure
element under `8.2.1/2`. `pdfjs/issue1045.pdf`, `bug1011159.pdf` and
`issue3566.pdf` all carry the construct.

### Instance counts, which the corpus does not pin

Everything above compares rule _sets_. litho and veraPDF also disagree on how
many times a rule fires, systematically, because veraPDF's object model is
finer-grained. The counts table records rule classes rather than instances, so
none of this shows there; the detector is a per-rule instance-count
differential run beside the set comparison.

Four families account for nearly all of it. Each was read off veraPDF's own
contexts rather than inferred:

| Rules                 | veraPDF object         | veraPDF counts     | litho counts  |
| --------------------- | ---------------------- | ------------------ | ------------- |
| `6.2.3.3`, `6.2.4.3`  | `PDDeviceRGB` etc.     | per operator       | per page      |
| `6.2.11.7.2`, `6.3.8` | `Glyph`                | per rendered glyph | per font      |
| `6.8`, `6.9`          | `CosFileSpecification` | per route          | per file spec |
| `6.1.7`, `6.1.8`      | `CosUnicodeName`       | per occurrence     | per name      |

`annotstruct_ua2.pdf` at PDF/A-3a draws 38 `6.2.4.3/2` violations from veraPDF,
one per `operators[n]/colorSpace[0]` or `fillCS[0]` across the page and every
annotation appearance stream, against litho's one. `charset_1b.pdf` at PDF/A-2a
draws 8 `6.2.11.7.2/1`, one per `usedGlyphs[n]`, against litho's 4, one per
font. `complex_1b.pdf` at PDF/A-3a draws `6.8/3` three times for one file
specification, reached as `root/EmbeddedFiles[0]`, as an indirect object and
through a `/AF` values array. `a4syntax_4.pdf` draws `6.1.8/1` twice for one
name, once as a Separation colorant and once as a resource key.

A count divergence is not automatically benign. `6.8.3.4/1` looked like one of
these and was a defect: veraPDF named nine distinct structure elements sharing
an unmapped `/Annot` type, litho deduplicated by type name, and the rule is
scoped to the element. It now counts per element. The four families above stay
because litho's granularity is the one its violation contexts promise, not
because a count is unimportant.

## Closed since, not planted

Six constructs were divergences when the fixtures around them were built. Each
now agrees, so each is a coverage opportunity rather than a gap. None is in a
committed fixture yet.

**`6.7.8/6`, the property container.** `pdfaSchema:property` holding an
`rdf:Bag` or a bare value instead of an `rdf:Seq`. `15607c4` wired the rule,
which the container check had named as validated elsewhere while nothing
validated it. An `rdf:Alt` there is accepted by both tools; until that commit it
also made litho report the entry's schema, namespaceURI and prefix as missing,
because an unrecognised container left its children attributed to the enclosing
schemas container. The part-2 spelling is `6.6.2.3.3/5`.

**`6.7.8/9` and `6.7.8/18`, a value type nothing defines.** `24ebd6b`
implemented `isValueTypeDefined` against the vocabulary veraPDF predefines: 26
types for part 1 and 36 for parts 2 and 3, so `Colorant` and `Timecode` are
undefined under part 1 and defined under the later parts. A type the same schema
entry declares resolves in either order; one declared by a different entry
resolves for neither. The part-2 spellings are `6.6.2.3.3/8` and `6.6.2.3.3/17`.

**`8.2.5.26/1`, a table cell intersection at PDF/UA-2.** `82b6bb0` widened the
PDF/UA-1 scan to PDF/UA-2. Both tests scope to `SETableCell` and read
`hasIntersection != true`, so one scan serves both; the `THead`/`TBody`/`TFoot`
row-span limit that had been given as the reason for keeping them apart belongs
to `8.2.5.26/2`, which scopes to `SETable`.

**The wrong-prefix half of ISO 19005-1, 6.7.8.** Every test of the clause
requires a specific namespace prefix, and a schema field carried under a second
prefix bound to the same URI (`xSchema:schema` where `xmlns:xSchema` is
`http://www.aiim.org/pdfa/ns/schema#`) is exactly that defect.
`xmpCheckExtensionSchemas` in `pkg/internal/compliance/xmp.go` used to emit
every rule of the offending level and then return before the walk that finds
everything else, so one prefix defect both over-reported and masked. It was
also nondeterministic: the scan ranged the `xmpExpectedPrefix` map and returned
on its first hit, so a packet misprefixed at two levels reported one level's
set on eleven of twelve runs and the other's on the twelfth. `591cb06` narrowed
the report to the fields that actually carry the wrong prefix, dropped the
early return, and fixed the visit order. A single misprefixed
`pdfaSchema:schema` now draws `6.7.8/3` from both tools and nothing else; the
same packet with a property missing its description draws `6.7.8/3` and
`6.7.8/11` from both, which is the masking gone; a packet misprefixed at the
schema and the property level at once draws the same eight rules from both,
identically on twelve consecutive runs. The part-2 spellings behave the same
way, starting at `6.6.2.3.3/2`.

**`7.2/15`, a table cell intersection at PDF/UA-1.** veraPDF sizes the grid,
then places every cell and marks an intersection when two cells claim the same
slot. A first row of `TH(RowSpan 2)`, `TH`, `TH(RowSpan 2)` over a second row
holding one `TH(ColSpan 2)` puts that cell's second column on a slot the first
row's last cell already carries down. `19c31ab` implemented the check, and both
tools now report `7.2/15` twice on that grid and nothing on two conforming span
layouts beside it.

**`7.2/2`, an outline entry with its own `/Lang`.** veraPDF reads
`gContainsCatalogLang == true` on `PDOutline` and offers no per-entry escape,
which matches ISO 32000-1 Table 153: an outline item dictionary defines no
`/Lang` entry. `walkOutlineItemsForLang` in
`pkg/internal/compliance/accessibility_lang.go` exempted an item carrying one
anyway. `902aa09` removed the exemption. Two files differing only in that entry
now report `7.2/2` on both tools, and a catalog `/Lang` silences both.
`structnest_ua1.pdf` uses a plain outline item, which reaches the same test.

## Second bug found: PDF/UA-1 language coverage (fixed, one facet remains)

`checkStructureTreeLangCoverage` in
`pkg/internal/compliance/accessibility_lang.go` computed a document-wide
`anyLangFound` latch: true as soon as any one structure element carried a
`/Lang`. The per-element report was gated on `effectiveLang == "" &&
!anyLangFound`, so a single `/Lang` anywhere in the tree silenced ISO 14289-1
7.2 t34 for every element that had none. veraPDF evaluates each text item on
its own (`gContainsCatalogLang == true || Lang != null`).

The A/B that proved it masking rather than an absent implementation: two
structure elements, both fully tagged, no catalog `/Lang`. Put `/Lang en-US`
on the first and litho reported nothing; remove that one entry and litho
reported t34 on both. veraPDF reports the uncovered element either way. The
only difference between the two files is a `/Lang` on an unrelated sibling.

Fixed by dropping the latch, inheriting language by `/Lang` **presence** up
the `/P` chain, and restricting the subject to elements that own text, via
page `/StructParents` to the `ParentTree`. Scoping the subject is required,
not incidental: a `/Figure` whose marked content draws only an image XObject
is an `SEImageXObjectItem` to veraPDF and never reaches t34, which no
structural test can tell without reading the content stream.

The remaining `7.2/34` divergence on `complex_ua2.pdf` is a **different
facet** of the same rule, not the latch: that file's real content sits
outside the structure tree entirely, and litho's t34 walk visits structure
elements only. Closing it means reaching content the tree never references,
which is a larger change and is deliberately not attempted here.

## Inherited colour spaces

`formcs_2b.pdf` covers ISO 19005-2, 6.2.4.3 test 4 for content that does not
choose its own colour space. Per ISO 32000-1 §8.6.8 Table 74 `sc`/`scn` set
components within the space `cs`/`CS` already selected and never select one, and
per §8.10.2 a Form XObject begins in the graphics state in effect at the
invoking `Do`. A bare `scn` inside a form therefore paints in whatever the
invoker had selected, which decides the verdict without appearing anywhere in
the form.

Four forms are invoked from one page and differ only in the state at their `Do`:

| Form  | State at its `Do`                    | Form content         | Rule        |
| ----- | ------------------------------------ | -------------------- | ----------- |
| `Fm0` | ICCBased selected, still in scope    | bare `scn`           | silent      |
| `Fm1` | nothing selected                     | `/CS0 cs` then `scn` | silent      |
| `Fm2` | ICCBased selected, then `Q`-restored | bare `scn`           | `6.2.4.3/4` |
| `Fm3` | ICCBased selected, still in scope    | `0 0 0 1 k`          | `6.2.4.3/3` |

`Fm2` is the case the fixture exists for. Its `Do` sits after a `Q` that pops
the scope where the ICCBased space was selected, so the initial DeviceGray is
back in effect even though a `cs` operator appears earlier in the stream.
Reading the page without honouring `q`/`Q` puts `Fm2` in the same class as
`Fm0`.

Two decisions keep the verdict attributable to `Fm2` alone, and both were
forced by measurement rather than chosen. `Fm3` names DeviceCMYK rather than
DeviceGray, so it answers test 3: an earlier draft used `0 g` and both litho and
veraPDF reported test 4 from `Fm3`, which let an implementation that misses the
inherited-state case still report the rule. The page also paints its own text
inside the ICCBased scope, because text drawn with no colour space selected is
itself a DeviceGray use and reported the clause from the page.

The file declares no OutputIntent. Test 4 is `gOutputCS != null`, so a PDF/A
OutputIntent of any destination colour space permits DeviceGray: an RGB one
silences the clause as thoroughly as a grayscale one.

litho previously read a bare `sc`/`scn` in any form as evidence that some space
was already selected, on the grounds that it did not model the inherited state.
Measurement contradicted the premise: veraPDF does model it, `Q` restore
included. litho now carries the state observed at each `Do` into the form, and
falls back to the old heuristic only for a tiling pattern or an annotation
appearance stream, which no `Do` invokes and whose entry state it therefore
never observes.

## Pattern and appearance-stream colour

`patterncs_2b.pdf` covers the two content scopes `formcs_2b` cannot reach. A
tiling pattern and an annotation appearance stream each carry a content stream
that no `Do` invokes, so neither the invoking state nor the object itself decides
what colour space they begin in.

Measured against veraPDF, both begin in the **default** graphics state, and a
space the page selected before painting the pattern does not carry into it:

| probe                                                 | veraPDF |
| ----------------------------------------------------- | ------- |
| pattern with a bare `f`, page selected nothing        | `t4`    |
| pattern with a bare `f`, page selected ICCBased first | `t4`    |
| pattern selecting its own ICCBased space              | silent  |
| appearance stream with a bare `f`                     | `t4`    |
| appearance stream selecting its own space             | silent  |

That is the opposite of a Form XObject, which does inherit. So the heuristic that
read a bare `sc`/`scn` in these scopes as evidence a space was already selected
was wrong for both, and it is now gone: every scope no `Do` invokes starts from
the default state.

The same measurement exposed a second, opposite defect. litho's pattern walk had
no usage gate, so it scanned every entry in `/Resources/Pattern` whether or not a
`scn` named it. veraPDF is usage-driven, so a declared-but-unpainted pattern was a
false positive. Pattern names are now collected from `scn`/`SCN`: per ISO 32000-1
Table 74 the only form of those operators taking a name operand is the pattern
form, so a trailing name identifies the pattern without tracking whether a
`/Pattern` space is current.

The fixture had to be built so the two defects cannot cancel. An earlier draft
gave the unpainted pattern a DeviceGray fill, and litho before the fix still
reported test 4 on the file: it missed `P0` and `A0` but scanned `P2`, agreeing
with veraPDF for the wrong reasons. Giving `P2` a DeviceCMYK fill separates them,
and the file now shows litho wrong in both directions at once: it reported test 3
that veraPDF does not, and missed test 4 that veraPDF does.

## Declared versus painted resources

`usedcs_2b.pdf` covers the usage gate on the three resource categories that carry
their own colour. ISO 19005-2, 6.2.4.3 constrains a colour space when content
paints with it, not when a resource dictionary declares it, and each category is
reached by its own operator:

| Resource            | Painted by              | veraPDF on declared-only |
| ------------------- | ----------------------- | ------------------------ |
| `/Pattern` entry    | a name operand of `scn` | silent                   |
| `/Shading` entry    | `sh`                    | silent                   |
| `/Font` Type3 entry | selected by `Tf`        | silent                   |

litho gated none of the three. The pattern gate landed with `patterncs_2b`; this
fixture adds the other two and a fourth construct that is not about usage at all.

An uncoloured tiling pattern (`/PaintType 2`) shall not specify any colour in its
content stream: the colour comes from the operands of the `scn` that selects it,
interpreted in the underlying space of `[/Pattern base]` (ISO 32000-1 §8.7.3.3,
Table 75). The initial DeviceGray of §8.6.8 therefore never applies inside the
cell. Measured, veraPDF reports nothing for such a cell, not even for an explicit
device colour written into it, and litho was applying its implicit-DeviceGray
inference there.

That last one was the root cause of two long-standing corpus divergences,
`govdocs/thread_000_000324` and `pdfjs/quadpoints`. Both draw with an uncoloured
pattern whose cell ends in a bare `S`, and both now agree with veraPDF.

Building the file exposed the same cancellation trap as `patterncs_2b`, in a
sharper form. `Sh1` and `T3Used` legitimately report test 4, so on the first
build litho reported test 4 whether or not it scanned the four ungated objects,
and both binaries agreed. Every declared-only object now paints DeviceCMYK and
the two painted ones paint DeviceGray, which puts the two failure directions on
different tests: before the fix litho reported an extra test 3, after it agrees.

## File-level syntax and forms

`filestruct_1b.pdf` covers four PDF/A-1b clauses that judge the file rather than
any page, all of which the coverage scan showed unreached: the header comment,
the trailer identifier, the lexical form of hexadecimal strings, and the
interactive form dictionary.

| Construct                                         | Rule      |
| ------------------------------------------------- | --------- |
| header comment bytes below 128                    | `6.1.2/2` |
| no `/ID` in the trailer                           | `6.1.3/1` |
| hexadecimal string with an odd digit count        | `6.1.6/1` |
| hexadecimal string containing a non-hex character | `6.1.6/2` |
| `/NeedAppearances true` on the AcroForm           | `6.9/1`   |
| a form field with no appearance dictionary        | `6.9/2`   |

The two `6.1.6` defects are kept on separate strings deliberately. veraPDF tests
them independently, `hexCount % 2 == 0` against `containsOnlyHex == true`, so one
string breaking both would report a single rule and leave the other unproven. A
third, well-formed string sits beside them as the control.

Three of the six cannot be written by pikepdf, which emits a conforming header
comment, always writes an `/ID`, and normalises every string it serialises. They
are patched into the saved bytes, each replacement exactly as long as what it
replaces, so no cross-reference offset moves. The builder refuses any patch that
would change a length rather than silently corrupting the file.

The fixture found a bug on its first build, which is the argument for writing
them. veraPDF reported `6.7.3` tests 2, 6 and 7 that litho did not: those compare
`/Title`, `/Creator` and `/Producer` against their XMP counterparts, and this
file sets all three while carrying no XMP packet at all. Each test reads
`Key == null || Key == XMPKey`, so a present Info entry cannot match an absent
XMP property. litho compared the two only when a packet existed, returning early
otherwise because the surrounding packet checks have nothing to examine. It now
runs the comparison against an empty XMP result instead.

## Document information against XMP

`docsync_1b.pdf` covers ISO 19005-1, 6.7.3, which compares eight document
information entries with their XMP analogues. Every test reads
`Key == null || Key == XMPKey`, so an entry that is present must have a matching
XMP property. The interesting question is what "matching" means, and four
different wrong answers were four real litho defects.

| Info entry      | XMP side                                   | Rule      |
| --------------- | ------------------------------------------ | --------- |
| `/Title`        | `dc:title` written as an attribute         | `6.7.3/2` |
| `/Keywords`     | empty, `pdf:Keywords` undeclared           | `6.7.3/5` |
| `/Creator`      | `xmp:CreatorTool` with another value       | `6.7.3/6` |
| `/Producer`     | `pdf:Producer` absent from the packet      | `6.7.3/7` |
| `/ModDate`      | an hour away from `xmp:ModifyDate`         | `6.7.3/8` |
| `/CreationDate` | `xmp:CreateDate`, same instant, other zone | silent    |
| `/Author`       | `dc:creator` in an `rdf:Seq`, equal        | silent    |
| `/Subject`      | empty, `dc:description` declared empty     | silent    |

The three controls are the point of the file, because each is the exact shape a
naive comparison reports:

- `D:20220406201541+02'00'` against `2022-04-06T13:15:41-05:00` is the same
  instant, 18:15:41 UTC. The clause asks whether the two are equivalent, not
  whether they are spelled alike, so comparing normalised local wall-clock
  strings reports a mismatch veraPDF does not.
- `/Subject` is present but empty and `dc:description` is declared empty. An
  empty string is not null, so the entry is still compared, and it matches
  because the comparison turns on whether the property is DECLARED rather than
  on whether it carries text. `/Keywords` beside it is the same empty entry
  whose property is _not_ declared, and that one fails.
- `/Author` uses `dc:creator` in an `rdf:Seq` and `/Title` uses `dc:title` as an
  attribute. Both look equal to a comparison that reads a property's text, but
  these are container-typed in XMP and veraPDF resolves them through the
  container, so an attribute answers nothing.

The file is a regression lock for the whole sequence of fixes, not just the last
one. Run against the binaries from each stage it reports, in order: an extra
`t1` with `t2` and `t5` missing, then `t2` missing, then agreement. The fourth
defect in that sequence, skipping the clause entirely when a document carries no
XMP packet, is locked by [filestruct_1b](#file-level-syntax-and-forms) instead,
which has no `/Metadata` at all.

## Stream syntax and interactive actions

`syntax_1b.pdf` covers ISO 19005-1 6.1.7 and 6.6.1, two independent axes the
coverage scan showed unreached.

| Construct                                           | Rule      |
| --------------------------------------------------- | --------- |
| a stream whose `/Length` is not its byte count      | `6.1.7/1` |
| a stream keyword followed by a lone CARRIAGE RETURN | `6.1.7/2` |
| a named action other than the four permitted        | `6.6.1/2` |
| a form field carrying an `/A` action                | `6.6.1/3` |

The two `6.1.7` defects sit on separate Form XObjects deliberately. A single
stream that both mis-declares its length and breaks its end-of-line leaves the
second condition unproven, because a parser that gives up on the first never
reaches it. The page content stream beside them is well formed and is the control
for both.

`6.6.1` splits by object: test 2 judges a named action, so it rides on an outline
item, and test 3 judges a form field, so it needs an AcroForm field carrying
`/A`. Each has a conforming neighbour: a `/NextPage` action, which is one of the
four the clause permits, and a second field with no action at all.

Both `6.1.7` defects are beyond pikepdf, which computes `/Length` from the data
and emits a conforming end-of-line, so they are patched into the saved bytes at
identical length. Finding the declared length needs care: searching backwards for
the nearest `<<` lands on the empty `/Resources` dictionary nested in the same
object, not on the stream dictionary, so the builder scans forward for the
`/Length ... >> stream` that introduces the marked payload instead.

`6.7.5`, which forbids `bytes` and `encoding` in the XMP packet header, is
deliberately absent. pikepdf reserialises any packet assigned to the catalog,
reordering attributes and rewriting the header, so those two attributes survive
neither authoring nor a length-preserving patch. Reaching that clause needs a
hand-assembled file.

## Annotation subtypes

`annotkinds_ua2.pdf` covers ISO 14289-2, 8.9.2.4, which the coverage scan showed
almost wholly unreached. The clause judges annotations subtype by subtype, and
each of its tests is scoped to one veraPDF object, so a file reaches a test only
by carrying that exact annotation kind. Eleven annotations sit on one page.

| Annotation                                   | Rule           |
| -------------------------------------------- | -------------- |
| `Sound`, any                                 | `8.9.2.4.11/1` |
| `TrapNet`, any                               | `8.9.2.4.15/1` |
| `Screen` with no `/Contents`                 | `8.9.2.4.12/1` |
| `3D` with no `/Contents`                     | `8.9.2.4.19/1` |
| `RichMedia` with no `/Contents`              | `8.9.2.4.19/2` |
| `Ink` with no `/Contents`                    | `8.9.2.4.8/1`  |
| `Stamp` with neither `/Name` nor `/Contents` | `8.9.2.4.7/1`  |
| `PrinterMark` inside the structure tree      | `8.9.2.4.14/1` |

The clause splits one idea, "an annotation must be describable or be an
artifact", across nine independent tests, so a check that reports the first
offending annotation and stops reports one rule where eight are due.

Two of the eleven need no defect at all. `Sound` and `TrapNet` are forbidden
outright, so their tests read simply `false`: the annotation's presence is the
violation. They are the reason the file cannot be reduced to "annotations
missing a description".

`PrinterMark` is the opposite shape. Its rule is satisfied when the annotation is
outside the structure tree or marked as an artifact, so reaching it means putting
the annotation into the tree, wired the way a real one is: a `/StructParent` on
the annotation, an `/OBJR` in the enclosing element's `/K`, and a `ParentTree`
entry mapping the number back. Wire it wrong and the annotation looks unparented,
which silences the test rather than firing it.

Three conforming twins sit beside the defects, one per subtype where the test
turns on an entry rather than on the subtype itself. Rule-set agreement alone
cannot see them, because veraPDF reports a rule once however many annotations
break it, so a check condemning every `Screen` would produce the same rule set as
a correct one. The per-rule instance counts are what prove them: `8.9.2.4.12/1`,
`8.9.2.4.8/1` and `8.9.2.4.7/1` each report one failed check, not two, in both
implementations. The `Stamp` twin uses `/Name` rather than `/Contents`
deliberately: the test accepts either, and only the `/Name` branch is otherwise
unexercised.

## Annotations in the structure tree

`annotstruct_ua2.pdf` covers the rest of ISO 14289-2 clause 8.9, the rules that
turn on where an annotation sits rather than on what subtype it is. Almost every
one of them is satisfied by `structParentType == null`, so an annotation reaches
the test only by being wired into the structure tree, and reaching a test is the
hard part rather than failing it.

Wiring means three things agreeing: a `/StructParent` on the annotation, an
`/OBJR` in the enclosing element's `/K`, and a `ParentTree` entry mapping the
number back to that element. Miss one and the annotation reads as unparented,
which silences the test rather than firing it.

| Annotation                                         | Rule           |
| -------------------------------------------------- | -------------- |
| `Link` under a `/P`                                | `8.2.5.20/1`   |
| `NoView` set without `ToggleNoView`                | `8.9.2.2/2`    |
| `Square` under a `/P`                              | `8.9.2.3/1`    |
| `Square` whose `/RC` differs from `/Contents`      | `8.9.2.3/2`    |
| `Popup` inside the tree                            | `8.9.2.4.9/1`  |
| `FileAttachment` with `/FS`, no `/AFRelationship`  | `8.9.2.4.10/1` |
| zero-area `Widget` inside the tree                 | `8.9.2.4.13/1` |
| `Watermark` under a `/P`                           | `8.9.2.4.16/1` |
| `/Contents` against the element's differing `/Alt` | `8.9.4.2/1`    |
| a field widget with `/AA` and no `/Contents`       | `8.10.2.3/2`   |

The enclosing element's type is the discriminator for four of these, so the file
carries `/Link`, `/Annot`, `/Form` and plain `/P` elements. Moving an annotation
between them changes the verdict without changing one byte of the annotation, so
a check that reads only the annotation dictionary cannot pass this file.

Two entries are placed where measurement put them rather than where their names
suggest. `/AFRelationship` is a file specification entry, so an
annotation-level one leaves `8.9.2.4.10/1` firing and makes no control at all.
`8.9.4.2/1` compares the annotation's `/Contents` with the `/Alt` of the element
enclosing it, so an annotation-level `/Alt` never reaches the test.

The file found three litho defects on its first build, which is the argument for
writing them. `8.9.2.3/2` was a dead rule, declared and registered but never
fired. `8.2.5.20/1` was approximated by a document-level count that fired only
when a file held no `Link` element at all, so a single well-placed Link masked
every misplaced one: exactly the masking this corpus exists to expose.
`8.2.5.20/2` compared whole destination arrays, so it reported two links onto
the same structure element with different zoom as disagreeing, and reported page
destinations and URI actions that veraPDF never examines.

## Part-4 file and catalog rules

`a4syntax_4.pdf` covers eleven ISO 19005-4 rules the coverage scan showed
unreached. All eleven judge the file or the catalog rather than any page, and
they sit on eight independent axes, so a check that abandons a document after
the first syntax complaint reports one rule where eight are due.

| Construct                                             | Rule        |
| ----------------------------------------------------- | ----------- |
| a stream whose `/Length` is not its byte count        | `6.1.6.1/1` |
| a name object that is not valid UTF-8                 | `6.1.7/1`   |
| an object header with non-conforming spacing          | `6.1.8/1`   |
| an inline image declaring the `/LZW` filter           | `6.1.9/1`   |
| a `/Perms` entry other than `UR3` or `DocMDP`         | `6.1.11/1`  |
| a catalog `/Version` that is not 2.x                  | `6.1.12/1`  |
| a widget annotation carrying `/A`                     | `6.4.1/1`   |
| a named action other than the four permitted          | `6.6.1/2`   |
| `pdfaid:rev` bound to a prefix other than `pdfaid`    | `6.7.3/6`   |
| two optional content configurations sharing a `/Name` | `6.10/2`    |
| `/AlternatePresentations` in the name dictionary      | `6.11/1`    |

Three of these are narrower than their clause text suggests, and each was
settled by measurement rather than by reading. `6.1.9` judges an INLINE image
filter: an `LZWDecode` stream sits in the same file and does not reach it.
`6.10/2` judges optional content CONFIGURATIONS, so the two groups carry
distinct names on purpose and the duplicate is between the default
configuration and one entry of `/Configs`. `6.1.7` visits names in only a few
modelled positions; a Separation colorant name works, while an unknown catalog
key, a resource key, a `/Differences` glyph name and an annotation `/Name` were
each measured silent.

Four constructs are beyond pikepdf, which computes `/Length` from the data,
normalises object headers, and refuses to emit a name that is not valid UTF-8.
They are patched into the saved bytes at identical length so no cross-reference
offset moves, and the builder refuses any patch that would change a length.

## Structure element kinds

`structkinds_ua2.pdf` covers five ISO 14289-2 rules that judge what a structure
element may contain, or what may contain it. Each is scoped to one veraPDF
object, so a file reaches a test only by carrying that exact element kind.

| Construct                              | Rule         |
| -------------------------------------- | ------------ |
| a root holding two `Document` elements | `8.2.5.2/1`  |
| `Ruby` whose kids are RB, RP, RT       | `8.2.5.23/1` |
| `Warichu` whose kids are WP, WT        | `8.2.5.24/1` |
| `LI` holding a marked-content item     | `8.2.5.25/2` |
| `math` whose parent is a `P`           | `8.2.5.29/1` |

The root defect is the one that interacts, since `8.2.5.2/1` judges the very
kids the other four hang from. Two `Document` elements break it while leaving
both subtrees walkable, and the `Warichu` pair lives under the SECOND `Document`
on purpose, so a walk that reads only the first root kid loses `8.2.5.24/1`
outright. The construct also fires ISO/TS 32005 `Table 5. StructTreeRoot-Document`,
which counts `Document` kids rather than comparing the whole kid sequence.

Controls sit beside every defect, because each of these tests judges a shape
rather than a kind: a `Ruby` of RB, RT and another of RB, RP, RT, RP, a
`Warichu` of WP, WT, WP, an `LI` holding `Lbl` and `LBody` beside one holding
content directly, and a `math` element parented by a `Formula`.

The MathML namespace is load-bearing. veraPDF decides `SEMathMLStructElem` by
namespace rather than by type name, so the same element declared in the PDF 2.0
namespace, or declaring none, is not a math element to it and reports `8.2.4/1`
instead. The `math` elements here bind to the MathML namespace deliberately, and
it is declared in the structure tree root's `/Namespaces`.

## Form XObject keys, blend modes and page actions

`xobjstate_2b.pdf` covers four ISO 19005-2 rules that judge different parts of
one page: what a Form XObject dictionary may carry, what a graphics state may
name as a blend mode, whether a page may act on being opened, and whether two
Separations agreeing on a colorant name agree on how to render it.

| Construct                                       | Rule        |
| ----------------------------------------------- | ----------- |
| a Form XObject carrying `/OPI`                  | `6.2.9/1`   |
| an `ExtGState` naming a blend mode outside PDF  | `6.2.10/1`  |
| a page carrying `/AA`                           | `6.5.2/2`   |
| two Separations sharing a colorant, disagreeing | `6.2.4.4/2` |

Four independent axes in one file, so a check that abandons a page after its
first complaint reports one rule where four are due. Each defect has a
conforming neighbour, because every one of these tests turns on a value rather
than on a kind.

`6.2.9/1` reads `(Subtype2 == null || Subtype2 != "PS") && containsPS == false
&& containsOPI == false`, so three different keys break one test. Only `/OPI` is
planted: the other two collapse onto the same rule and would prove nothing
extra, while `/OPI` is the one a real producer emits.

`6.2.4.4/2` is reported once per offending Separation by veraPDF and once per
disagreeing pair by litho, so their instance counts differ by one. Both leave
the third Separation silent, which is what the control exists to show.

## Private Use Area characters

`puatext_ua2.pdf` covers the ISO 14289-2 rules that forbid a Private Use Area
code point in text a consumer reads out, because nothing outside the producing
system knows what one means. The requirement is split across three veraPDF
objects, so a file reaches each only by putting the character in that position.

| Construct                            | Rule      |
| ------------------------------------ | --------- |
| `/ActualText` on a structure element | `8.4.3/2` |
| `/Alt` on a structure element        | `8.4.3/3` |
| an outline item `/Title`             | `8.6/1`   |

The same `/ActualText` answers ISO 19005-4 `6.2.10.8/1`, the PDF/A-4 spelling of
the first row. That clause covers `CosActualText` ALONE: the PDF/A-4 profile
defines no `CosAlt` rule, so an `/Alt` carrying a PUA character is out of scope
there. litho reported it anyway, a false positive this file's PDF/A-4 column
would have hidden, because the conforming `/ActualText` beside it fires the same
rule. Only the instance counts separated them.

Where the third character goes was measured rather than read off the clause.
`CosTextString` sounds like any string, but a document information `/Title` and
an annotation `/Contents` were both silent; an outline item's `/Title` reaches
it. Each defect sits beside a twin carrying the same words without the PUA
character, so a check condemning every `/ActualText`, `/Alt` or title fires on
the control.

## Formulas, notes, shared XObjects and media clips

`formmedia_ua1.pdf` covers five ISO 14289-1 rules, each scoped to a different
veraPDF object, so a file reaches each only by carrying that exact construct.

| Construct                                         | Rule         |
| ------------------------------------------------- | ------------ |
| a `Formula` with neither `/Alt` nor `/ActualText` | `7.7/1`      |
| two `Note` elements sharing an `/ID`              | `7.9/2`      |
| a Form XObject painted under two elements         | `7.20/2`     |
| a media clip with no `/CT`                        | `7.18.6.2/1` |
| a media clip whose `/Alt` carries no text         | `7.18.6.2/2` |

The two media clip tests sit on separate clips deliberately. One clip missing
both `/CT` and usable `/Alt` text would report one rule and leave the other
unproven, so one clip carries a valid `/Alt` and no `/CT`, the next carries
`/CT` and an `/Alt` whose only text is empty, and a third carries both.

`7.20/2` turns on something the clause text does not say. veraPDF exits the
check early for a Form XObject carrying no `/StructParents`, so such a form can
be painted any number of times and answer nothing; reaching the rule needs the
key AND the same indirect object encountered twice. Painting a form twice under
two `P` elements without the key was measured silent, as were the same construct
spread across two pages and one whose own content is marked. The control beside
it therefore carries `/StructParents` too and is painted once, which is what
shows the discriminator is the repetition rather than the key.

## Identification prefixes, destinations and text fields

`idfields_ua2.pdf` covers four ISO 14289-2 rules on four unrelated parts of the
document, so a check that stops after the first complaint visibly under-reports.

| Construct                                        | Rule         |
| ------------------------------------------------ | ------------ |
| `part` bound to a prefix other than `pdfuaid`    | `5/3`        |
| `rev` bound to a prefix other than `pdfuaid`     | `5/4`        |
| an outline destination naming a page             | `8.8/1`      |
| a text field whose `/RV` disagrees with its `/V` | `8.10.3.3/1` |

The two identification defects are load-bearing. XMP identifies a schema by
NAMESPACE, not by prefix, so declaring a second prefix on the PDF/UA
identification namespace and hanging `part` and `rev` off it keeps the file
identifiable as PDF/UA-2 while making both prefixes wrong. Had it stopped being
identifiable, the other rules would go unevaluated and the file would report
almost nothing, so those two firing alongside the rest is the proof it worked.

`8.8` splits by object: test 1 judges a `PDDestination` and test 2 a
`PDGoToAction`. Only test 1 is planted here, on an outline item's `/Dest`;
`annotstruct_ua2.pdf` covers test 2 through link annotations. The outline item
beside it names the `Document` element and is the control.

`8.10.3.3/1` found a dead rule. veraPDF reads `/RV` through the same rich text
helper as a markup annotation's `/RC`, parsing it as XML and reducing it to its
text nodes before comparing with the plain `/V`, and litho read `/RV` nowhere at
all.

## Link target identity and a symbolic character map

`linkfont_ua2.pdf` covers two ISO 14289-2 rules on unrelated parts of the file.

| Construct                                                   | Rule         |
| ----------------------------------------------------------- | ------------ |
| two links under one `Link` element onto different targets   | `8.2.5.20/2` |
| a symbolic TrueType with neither a (3,0) nor a (1,0) `cmap` | `8.4.5.7/4`  |

`8.2.5.20/2` is the reason for the file. veraPDF compares only STRUCTURE
destinations, and compares them by object identity alone: a destination array's
first element must be a dictionary carrying `/S`, and the view parameters after
it are ignored. A page destination, a URI action, or a destination naming no
structure element leaves nothing to compare and is never evaluated.

Three groups of links sit side by side and only the first offends. Two name
different structure elements and fire. Two name the SAME element with different
view parameters and stay silent, which is the control that matters: comparing
whole destination arrays rather than target identity reports that pair as
disagreeing, and litho once did. Two more carry ordinary page destinations that
differ from each other, and are silent because a page destination never reaches
the comparison at all.

The symbolic font is painted in rendering mode 3, which is a recorded divergence
rather than a preference. A symbolic TrueType with neither subtable cannot map
its codes, so veraPDF reports every code it paints as a glyph that is not
present; litho leaves a code it cannot map unchecked, on the ground that presence
is then unresolvable. Mode 3 exempts the glyphs on both sides, keeping this file
about the character map. Reaching that false negative needs the font painted
visibly, and closing it means turning an unresolvable code into a firing one,
which has produced false positives on real-world corpora before.

## A dynamic XFA configuration and row spans past the last row

`xfatable_ua1.pdf` reaches three ISO 14289-1 rules.

| Construct                                           | Rule     |
| --------------------------------------------------- | -------- |
| an XFA configuration asking for dynamic rendering   | `7.15/1` |
| a `/RowSpan` past the last row, in a later column   | `7.2/41` |
| a `/RowSpan` past the last row, in the first column | `7.2/43` |

`7.15/1` reads `dynamicRender != 'required'`, which is not a PDF key at all: it
lives inside the XFA packet the `/AcroForm` carries, so reaching the rule means
writing XFA XML rather than setting a dictionary entry.

The two table rules are one defect placed a column apart. A `/RowSpan` reaching
past the last declared row puts cells in rows no `TR` declares, and which rule
answers for it turns on where that span starts: the first column answers
`7.2/43`, any other column answers `7.2/41`. That split follows veraPDF's cell
placement rather than anything the clauses say, and was measured across two,
three and four column tables before being relied on.

Neither is a span in an EARLIER row, which is a different defect: such a span
SHIFTS the following row's cells rather than colliding with them, so it reads as
a column count mismatch and answers `7.2/42`, which `tablespan_ua2.pdf` covers.

Both rules were unreachable in litho before this fixture. Rows a span reaches
past the last `TR` exist in the grid with nothing to declare them, and litho
compared only declared rows, so every row still agreed and nothing fired;
`7.2/41` had no firing site at all. A third table conforms and stays silent, its
span covering exactly the rows the table declares, which is what shows the
geometry check accepts a legitimate span rather than ignoring the attribute.

## The container an extension schema's properties live in

`extprop_2b.pdf` reaches ISO 19005-2 `6.6.2.3.3/5`, which reads

    isPropertyValidSeq == true && (propertyPrefix == null || propertyPrefix == "pdfaSchema")

so it judges the CONTAINER holding a schema's property list rather than any
property inside it. `extschema_2b.pdf` covers the tests judging a schema's own
fields and its individual properties, and none of those reach the container,
because a well-formed `rdf:Seq` is what carries them there.

Three schemas sit side by side and only the first offends, holding its property
list in an `rdf:Bag`. The second holds one property in a proper `rdf:Seq`; the
third holds two, because a container check that accepted a one-entry list and
rejected a longer one would otherwise pass unnoticed.

Every property inside all three schemas is complete and well formed, and the
three carry the same property shape deliberately. If a check condemned a schema
for something a property carries, all three would fire and the file would prove
nothing about the container.

### `gstateform_1b.pdf`

`gstateform_1b.pdf` reaches four rules the coverage scan showed unreached and
separates two prohibitions that different parts spell differently.

ISO 19005-1 splits the form XObject prohibitions: 6.2.4 test 2 answers for
`/OPI` on any XObject, 6.2.5 test 1 for a PostScript one. ISO 19005-2 and -3
fold `/OPI`, `/Subtype2 /PS` and `/PS` into 6.2.9 test 1. ISO 19005-4, 6.2.8.1
covers `/OPI` alone, because ISO 32000-2 removed PostScript XObjects from the
format and left part 4 nothing to prohibit. The file carries one form of each
kind, so a checker that answered with the wrong rule of that set is visible as a
count rather than hidden behind a firing neighbour.

The ICC constructs work the same way. Only ISO 19005-1, 6.2.3.2 test 2 measures
`/N` against the component count the profile header implies; parts 2 to 4 spell
6.2.4.2 test 1 as a requirement on the profile's own conformance instead. The
file carries one profile whose `/N` disagrees with its header and one whose `/N`
agrees, both CMYK, so the mismatch answers only where a rule states it.

Every graphics state is invoked with `gs` and every form is drawn with `Do`. An
`ExtGState` no content stream selects is not part of the page's state, and the
same dictionaries left unused fire nothing in veraPDF. That was measured before
the fixture relied on it.

### `xmpsyntax_1b.pdf`

`xmpsyntax_1b.pdf` carries one defect in each of three layers of the file: the
first eight bytes, the format of the cross-reference section, and the XML inside
the metadata stream. `6.1.2/1` answers for the header, `6.1.4/3` for the
cross-reference stream, `6.7.9/1` for a raw `<` in the XMP character data.

The layering is the point. A reader that gave up on the malformed header never
reaches the cross-reference section, and one that gave up there never reaches
the metadata, so each rule firing is evidence that the reader kept going past
the layer above it.

`6.1.2/1` reads `headerOffset == 0 && /%PDF-\d\.\d/.test(header)`. Rewriting
`%PDF-1.7` as `%PDF-17.` breaks the pattern in place: the header stays at offset
zero and no byte after it moves, so the cross-reference offsets stay valid.
Prepending junk instead shifts every offset in the file, which makes veraPDF
fail to parse rather than report, and a report with zero rules is
indistinguishable from a pass.

The raw `<` also costs the file its PDF/A identification and its `dc:title`,
because the packet carrying them cannot be parsed: `6.7.11/1` fires alongside at
part 1, `7.1/9` and `8.11.1/1` at the accessibility profiles. That is one
construct, not three defects. It is also what the file protects: litho used to
read properties out of a packet it had already reported as malformed, resolving
a title veraPDF cannot see and qualifying a language alternative veraPDF never
builds.

pikepdf regenerates the XMP packet on save unless both `preserve_pdfa` and
`fix_metadata_version` are off, silently repairing the planted `<`. The builder
asserts the byte survived rather than trusting the flags.

### `metafilter_1b.pdf`

`metafilter_1b.pdf` reaches ISO 19005-1 `6.7.2/2`, which reads

    isCatalogMetadata == false || Filter == null

so the prohibition is on the CATALOG's packet alone. A metadata stream hanging
off anything else may be filtered.

The file carries both, encoded identically: the catalog's packet offends, and a
page's packet with the same `/Filter /FlateDecode` over the same kind of
compressed payload is the control. A checker matching on the filter rather than
on whose packet it is condemns both; a checker that skipped metadata streams
condemns neither. One stream cannot tell those apart.

Both packets are well-formed XML, and only the catalog's carries a PDF/A claim,
so the filter is the only thing either can answer for.

qpdf strips `/Filter` off a metadata stream on every save path tried, including
with `preserve_pdfa` and `fix_metadata_version` off. Each stream is therefore
planted after the save, its dictionary padded with spaces so the object keeps
its byte length and no cross-reference offset moves. Padding after `endobj`
instead would cost a `6.1.8/1`.

### `xpacket_1b.pdf`

`xpacket_1b.pdf` reaches ISO 19005-1 `6.7.5/1` and `6.7.5/2`, which read
`bytes == null` and `encoding == null`. Both attributes ride in the
`<?xpacket ...?>` processing instruction rather than in any PDF dictionary, so
reaching the clause means writing the packet header.

The catalog's packet carries both, and they are answered separately, so a
checker recognising one attribute and not the other shows up as a missing rule
rather than hidden behind its neighbour.

A second packet hangs off the page carrying the same two attributes. It is the
control: measured against veraPDF, a packet outside the catalog answers for
neither test, so a checker matching any packet header rather than the
document's own shows up as an extra firing. That scope was measured before the
fixture relied on it.

pikepdf regenerates the packet on save unless both `preserve_pdfa` and
`fix_metadata_version` are off, and the regenerated header carries neither
attribute. The builder asserts both survived rather than trusting the flags.

### `encperm_ua1.pdf`

`encperm_ua1.pdf` reaches ISO 14289-1 `7.16/1`, which reads
`P != null && (P & 512) == 512`. Bit 10 is "extract for accessibility", and a
document withholding it denies assistive technology the text.

It is the one fixture written byte by byte rather than through pikepdf. qpdf
never clears bit 10, whatever `pikepdf.Permissions(accessibility=False)` asks
for: measured at revision 4 and revision 6, `/P` comes out `-1028` either way,
and `extract=False` clears bit 5 instead. So the builder writes the standard
security handler directly, ISO 32000-1 Algorithms 2, 3 and 5, with V=4, R=4 and
a V2 crypt filter.

Writing it by hand buys back determinism the qpdf path cannot offer: qpdf
refuses a deterministic `/ID` together with encryption, which is why
`trailer_2b.pdf` is not reproducible. Here the `/ID` is fixed, so the whole
file, keys included, rebuilds byte for byte.

The metadata stream is encrypted like every other stream. Leaving it in the
clear made the packet unreadable to both engines, which cost the file its title
and its `pdfuaid:part` and buried the permissions rule under three unrelated
ones. That was measured before the fixture settled on encrypting it.

Apart from its unembedded font the document conforms, so at PDF/UA-1 exactly two
rules answer: the permissions and `7.21.4.1`. A checker reading the permissions
wrongly shows up immediately rather than behind a neighbour.

### `sigcms_2b.pdf`

ISO 19005-2, 6.4.3 splits into three tests over two objects. Test 1 asks the
PDSignature whether its `/ByteRange` covers the whole document. Tests 2 and 3
ask the CMS SignedData in `/Contents` whether a signing certificate rides along
and whether there is exactly one SignerInfo. Nothing about the first is visible
from the second, so a checker reading only `/ByteRange` reports one third of the
clause and passes the rest.

Four signature fields carry one verdict each:

| Field  | `/Contents`                | `/ByteRange`    | Answers   |
| ------ | -------------------------- | --------------- | --------- |
| `sig0` | one signer, certificate    | covers the file | nothing   |
| `sig1` | one signer, certificate    | 64 bytes short  | `6.4.3/1` |
| `sig2` | one signer, no certificate | covers the file | `6.4.3/2` |
| `sig3` | two signers, certificate   | covers the file | `6.4.3/3` |

Each defect sits on a field that conforms on the other two, and `sig0` conforms
on all three, so a verdict attached to the wrong field answers on `sig0` too.

The blobs are genuine `openssl cms -sign` output, embedded in the builder rather
than generated at build time: openssl stamps a signingTime, so its output is not
reproducible across runs. They are the same bytes as
`pkg/internal/compliance/testdata/cms_{real,nocert,two}.der`, which is what
veraPDF 1.30.2 was measured against. An unparseable blob answers tests 2 and 3
together and would mask the split, so every blob here is real CMS.

`/ByteRange` is written as a ten-digit zero-padded placeholder and patched after
the save against the offset the signature's own `/Contents` landed at. A
signature cannot know its offset before the file is laid out, and padding to a
fixed width is what keeps the patch from moving an xref offset.

`sigempty` and `text` carry no `/AP`, which answers `6.3.3/1` twice. That is the
fixture's second purpose. A widget annotation is normally merged with its form
field, per ISO 32000-1, 12.7.4.1, so it is reached by the page `/Annots` walk
and the `/AcroForm` `/Fields` walk alike, and a checker running both states the
same missing appearance twice. The four signature widgets carry an `/AP`, so
this lands on two objects rather than six and the instance count is meaningful.

### `docmdp_2b.pdf`

Two clauses answer here, and nothing connects them. ISO 19005-2, 6.1.12 t2 reads
the `/Reference` entries of the signature the catalog `/Perms /DocMDP` names.
ISO 19005-2, 6.1.13 t5 reads a real in a page content stream. A checker that
abandons the document after its first structural verdict reports one of the two.

6.1.12 t2 is `permsContainDocMDP == false || entries.split('&').filter(elem =>
elem == 'DigestLocation' || elem == 'DigestMethod' || elem == 'DigestValue')
.length == 0`, so it only bites once the catalog certifies the document. Two
`/Reference` entries sit under that signature: the first names a
`/DigestMethod`, the second carries only `/TransformMethod` and
`/TransformParams`. A check that condemns the array rather than the entry fires
twice.

Nothing else about the signature is wrong, which is the point. Its `/Contents`
is the same genuine `openssl cms -sign` blob `sigcms_2b.pdf` uses, one signer
with its certificate, and its `/ByteRange` is patched after the save to cover
the file exactly. All three tests of 6.4.3 therefore stay silent and the 6.1.12
verdict is attributable to the `/Reference` entry alone.

6.1.13 t5 is `realValue == 0.0 || (realValue <= -1.175e-38) || (realValue >=
1.175e-38)`, the smallest normal single-precision magnitude. The page paints a
rectangle whose width is `0.000000000000000000000000000000000000000001`, a
denormal, beside one of `1`. Writing it as a literal rather than in exponent
form matters: ISO 32000-1, 7.3.3 gives PDF reals no exponent syntax at all.

The clause is stated by parts 2 and 3 only. ISO 19005-1, 6.1.12 t2 caps the
magnitude and says nothing about the floor, and ISO 19005-4 has no CosReal rule
at all, so the row reads 4 at A-1b and 5 at A-4 where the other profiles read 6.

A third construct, a font `/Subtype` naming no font type, was built and then
dropped: veraPDF builds no font object for an unrecognised subtype, so it
reaches neither 6.2.11.2 t2 nor the embedding rules behind it. Carrying it would
have baked a permanent rule-set divergence into the corpus.

### `charset_1b.pdf`

A subset font has to declare which glyphs it kept. Two entries carry that
declaration, `/CharSet` for a Type 1 subset and `/CIDSet` for a CID subset, and
four rules read them at two different strictnesses:

| Rule                         | Reads                                    |
| ---------------------------- | ---------------------------------------- |
| ISO 19005-1, 6.3.5 t2/t3     | the entry has to be there                |
| ISO 19005-2/3, 6.2.11.4.2 t1 | a `/CharSet` present lists every glyph   |
| ISO 19005-2/3, 6.2.11.4.2 t2 | a `/CIDSet` present identifies every CID |
| ISO 14289-1, 7.21.4.2 t1/t2  | the PDF/UA-1 spelling of the same two    |

Part 1 requires the entry to exist. Parts 2 and 3 permit it to be absent and
judge it only once present, so an absent entry and an incomplete one are
different defects and no single font can carry both. Eight fonts carry one
status each, four per family:

| Font       | Entry             | Answers                |
| ---------- | ----------------- | ---------------------- |
| `T1None`   | no `/CharSet`     | 6.3.5 t2, part 1 only  |
| `T1Short`  | `/CharSet (/A)`   | the completeness tests |
| `T1Full`   | `/CharSet (/A/B)` | nothing                |
| `T1Whole`  | no `/CharSet`     | nothing: not a subset  |
| `CIDNone`  | no `/CIDSet`      | 6.3.5 t3, part 1 only  |
| `CIDShort` | `/CIDSet` less 2  | the completeness tests |
| `CIDFull`  | full `/CIDSet`    | nothing                |
| `CIDWhole` | no `/CIDSet`      | nothing: not a subset  |

Both families answer at every profile, so the file is a masking probe twice
over: a checker that reports the first offending font and stops reports one
rule where two are due. The `Full` fonts are the conforming case of the entry
the others break, and the `Whole` fonts are the subset-prefix exemption planted
as a control.

Every subset prefix differs. veraPDF keys a font object on its name, so two
fonts sharing `ABCDEF+LithoCFF` would collapse into one and the control would
never be judged apart from the defect.

`T1Short` paints `B`, which is what makes it answer at part 1 too. ISO 19005-1,
6.3.5 t2 is one-directional and scoped to the glyphs the content paints, while
ISO 19005-2, 6.2.11.4.2 t1 compares against the whole program. A `/CharSet` of
`(/A)` on a font that paints `B` fails both readings.

The font programs come from `_cff.py` and `_truetype.py`. A validator has to
parse a real program to know which glyphs it defines, so a placeholder stream
leaves every completeness test silent. `_cff.py` emits a bare CFF, the `Type1C`
form of `/FontFile3`, the same way `_truetype.py` emits a TrueType: writing
them keeps the builders' one dependency, pikepdf, the only one.

### `linearized_1b.pdf`

ISO 19005-1, 6.1.3 test 4 is the only rule in the corpus that reads more than
one trailer: `(isLinearized != true) || lastID == null || (firstPageID ==
lastID)`. A linearized file states its `/ID` twice, once in the first-page
trailer at the top and once in the last trailer at the end, and the two shall
be identical. A checker that keeps one trailer and discards the other cannot
answer the test at all, which is how litho failed it before this fixture
existed.

| Trailer    | `/ID`                   |
| ---------- | ----------------------- |
| first page | `[<a11c...> <c0ff...>]` |
| last       | `[<a11c...> <b0bb...>]` |

Element 0 matches and element 1 does not, so only a check comparing the whole
array answers. Both arrays also hold two different strings, and that is the
control: ISO 32000-1, 14.4 has element 1 change whenever a file is updated, so
a pair differing within one trailer is conformant. A check reading the
requirement as "the two entries of the `/ID` array shall be identical" fires on
the last trailer, which is the one nothing is wrong with. Test 1 of the same
clause stays silent, both trailers carrying the key with two non-empty strings,
so the 6.1.3 verdict is attributable to the comparison alone.

Three pages carry an unembedded Helvetica and paint in DeviceGray, and there is
neither `/Metadata` nor an output intent, so three further clauses answer from
elsewhere in the specification.

qpdf lays the file out. Linearization is a whole-file property, not something a
builder plants object by object. The `/ID` values are patched afterwards
against the bytes qpdf wrote: they are hex strings of a fixed width, so nothing
moves and the layout survives.

### `jpx_2b.pdf`

ISO 19005-2, 6.2.8.3 states five tests over the JPEG 2000 data of a `JPXDecode`
image, and every one reads a box of the JP2 container rather than the
entropy-coded data: the channel count and bit depth from `ihdr` and `bpcc`, the
colour specification from `colr`. So a fixture plants each defect by naming a
value, which is what `_jp2.py` writes.

| Image      | Container                         | Answers     |
| ---------- | --------------------------------- | ----------- |
| `ImOK`     | 3 channels, one sRGB `colr`       | nothing     |
| `ImChan`   | 2 channels                        | `6.2.8.3/1` |
| `ImSpecs`  | two `colr` boxes, both APPROX 1   | `6.2.8.3/2` |
| `ImMeth`   | one `colr` box, METH 4            | `6.2.8.3/3` |
| `ImJab`    | one `colr` box, enumerated CIEJab | `6.2.8.3/4` |
| `ImDepth`  | a `bpcc` box declaring 40 bits    | `6.2.8.3/5` |
| `ImWaived` | METH 4, and a `/ColorSpace` entry | nothing     |

The five defects are independent, so a checker that reports the first offending
image and stops reports one test where five are due. All seven images are
painted.

`ImSpecs` carries two specifications that both set APPROX to 1. Test 2 reads
"there shall be exactly one colour space specification that has the value 0x01
in the APPROX field", so two of them fail it, while test 3 still finds an
effective METH of 1 and stays silent. Two specifications with no APPROX 1 at
all answer both tests at once and the split disappears, which was measured on
both engines before the fixture settled on this shape.

`ImWaived` is the exemption planted as a control. The clause hands the colour
tests to the image dictionary's `/ColorSpace` when it has one, so an image
whose `colr` box states a forbidden METH conforms as long as the PDF names the
colour space itself. A check reading the container unconditionally fires on it.
`ImOK` conforms on all five tests, so a verdict attached to the wrong image
answers on it too.

The codestreams are embedded rather than written. A JPEG 2000 codestream is
entropy coded, which no builder writes by hand, and the corpus builders take no
dependency beyond pikepdf. `_jp2.py` records the OpenJPEG command line that
produced them and strips the `COM` marker carrying the encoder version, so the
bytes do not move when the encoder is upgraded.

### `threed_4e.pdf`

PDF/A-4e is the only conformance level that admits a 3D annotation, and
ISO 19005-4, 6.1.6.1 test 3 is the only rule that reads the stream one points
at: `Subtype == 'U3D' || Subtype == 'PRC'`.

| Annotation | `/3DD` `/Subtype` | Answers     |
| ---------- | ----------------- | ----------- |
| first      | `/U3D`            | nothing     |
| second     | `/PRC`            | nothing     |
| third      | `/LithoNot3D`     | `6.1.6.1/3` |

Both permitted values are planted, not just one. The test is a two-way
disjunction, so a check hardcoding `U3D` alone passes the file unless a `PRC`
sits beside it. The defective stream is otherwise identical to the two
conforming ones, so a verdict attached to the wrong stream answers on a control.

Every annotation carries an appearance stream and a `/Contents` string, which
keeps ISO 19005-4, 6.3.3 silent and leaves the 6.1.6.1 verdict standing alone.
At every profile other than PDF/A-4e a 3D annotation is a forbidden subtype,
which is a different clause on a different object, so this is the one fixture
whose home profile is a conformance level rather than a part.

### `grouping_ua2.pdf`

Most ISO/TS 32005 Table 5 rows are stated twice, as two rules sharing a clause
and differing only by test number: one governs the parent "when used as a
grouping element", the other "when used as a non-grouping element". Which half
applies is not a property of the type name. ISO 32000-2:2020, 14.8.4.2 lets
`Link` sit at block or inline level, so the same type groups in one document and
carries content in another.

A coverage scan over the 5170 corpus files found the two grouping-dependent rule
kinds unreached in every one of them: no file held a `Link`, `Annot`, `Reference`
or `FENote` used as a container. litho decided the question from a static set of
type names, which answers both halves wrongly, and nothing in the corpus said so.

Five `Link` elements sit side by side, none carrying a content item of its own,
so the only thing that varies is what each contains:

| `Link` kids        | Grouping | Answers                   |
| ------------------ | -------- | ------------------------- |
| `Caption` x2       | yes      | `Table 5. Link-Caption/1` |
| `Span` + `H1`      | no       | `Table 5. Link-Hn/1`      |
| `Span` + `Caption` | no       | `Table 5. Link-Caption/2` |
| `Caption`          | yes      | nothing                   |
| `H1`               | yes      | nothing                   |

The `Span` is what makes two of them non-grouping, and planting them beside the
first is the whole point: the same `Caption` kid is legal under a grouping `Link`
and forbidden under a non-grouping one, so a checker keying on the type name
cannot pass both rows at once. The last two are controls, and each is a
construct the wrong half would condemn: a single `Caption` satisfies "at most
one", and the `Hn` row governs non-grouping use only.

Every firing was measured on veraPDF 1.30.2 and litho one construct per file
before the five were planted together, and the two tools return the same rule
set in all thirteen cells.

### `resinherit_2b.pdf`

ISO 32000-1, 7.7.3.4 and Table 30 make `/Resources` and `/MediaBox`
inheritable: a page that does not state one takes it from its nearest ancestor
in the page tree that does, whole. A census of every corpus found 35 pages with
no `/Resources` of their own, 14 inheriting from the root and none from an
intermediate `/Pages` node. litho fell back to the root's dictionary, which no
corpus file could contradict, while missing every font and colour space an
intermediate node lends, and it judged a media box the root writes once for the
document where veraPDF judges it once for every page that takes it.

Seven pages sit under six intermediate nodes, one per construct, and every
font and colour space is a distinct object, so each verdict names its page:

| Page           | Its `/Resources`      | Ancestor lends               | Answers                              |
| -------------- | --------------------- | ---------------------------- | ------------------------------------ |
| `Inter`        | none                  | `F1`, `CS0`, two levels up   | `6.2.11.6/2`, `6.2.4.3/2`, `6.2.2/2` |
| `Own`          | `F2` alone            | `F6`, `CS4`                  | `6.2.11.4.1/1` for `F2` alone        |
| `Null`         | `null`                | `F3`, `CS2`                  | `6.2.4.3/3`, `6.2.2/2`               |
| `Dangle`       | `9999 0 R`, no object | `F4`, `CS3`                  | nothing the ancestor lends           |
| `Parent`       | none, `/Parent` wrong | `F5`, on the node listing it | `6.2.11.4.1/1`, `6.2.2/2`            |
| `Box1`, `Box2` | none                  | `/MediaBox [0 0 2 2]`        | `6.1.13/11`, once per page           |

`F1`, `F2`, `F3` and `F5` are unembedded, so each also answers
`6.2.11.4.1/1`: four instances, one per font. `F1` is the only TrueType font
reached with no `/Encoding`, which makes the intermediate node a rule class of
its own rather than one instance among four.

Every object a page must not reach carries a defect no reached object does, so
reaching one adds a rule class rather than an instance. `FR`, the root's font,
`F6` and `F4` are TrueType fonts outside the standard 14 with no `/FirstChar`
(`6.2.11.2/4`). `CS4` and `CS3` are Separations naming `SpotRGB`, the colorant
`CS0` names, over DeviceCMYK instead of DeviceRGB (`6.2.4.4/2`). And every page
draws with the names its ancestors define: `Inter` with `FR`, which the nearer
ancestor's dictionary replaces rather than merges with, `Own` with `F6` and
`CS4` past the dictionary it states, `Dangle` with `F4` and `CS3`.

Three readings are veraPDF's, measured one construct per file before they were
packed. A `null` entry is absent (ISO 32000-1, 7.3.7), so `Null` inherits. A
reference to a missing object is kept as the page's own value, as veraPDF's
`knownKey` keeps any indirect reference, although 7.3.10 reads it as the null
object; litho follows the entry veraPDF reads, as it does for `/AA` and the
other keys `knownKey` covers. And `Parent` inherits through the node whose
`/Kids` lists it, which veraPDF's tree walk records as the parent, not through
the `/Parent` entry naming the root.

qpdf drops a null dictionary value and cannot write a reference to a missing
object, so the builder plants both as placeholder names of the same length and
patches the bytes after the save, which moves no offset. It also builds the page
tree by hand: pikepdf's page list flattens the tree and pushes inherited
attributes onto each page, which would delete every construct here.

litho before the fix disagrees with veraPDF on the rule set in all thirteen
cells. At PDF/A-2b it misses `6.1.13/11` and `6.2.11.6/2`, and reports
`6.2.11.2/4`, `6.2.11.2/6` and `6.2.4.4/2` from the objects it should not
reach. After it, the two tools return
the same rule set in all thirteen cells, and the same count for every rule at
every PDF/A level. The PDF/UA levels differ on instance counts only, the
per-content-item untagged-content rules the corpus does not pin (see
[Instance counts](#instance-counts-which-the-corpus-does-not-pin)).

### `inlineimage_2b.pdf`

Four remediation fixers rewrite a page's content stream from its parsed
operations, and the content parser reads an inline image's data off-token (ISO
32000-1, 8.9.7). Writing the operations back dropped every image on the page
and left `<< ... >> BI` with no `ID` or `EI`, a stream no reader can parse.
Remediating the 105 corpus files that draw inline images in page content, at
PDF/A-1b, PDF/A-2b, PDF/A-4 and PDF/UA-1, default and aggressive, none of the
four repaired a page holding one, so the gate never saw it.

One page carries all four defects beside two inline images, whose data holds a
NUL, a high byte, a delimiter and an end-of-line marker:

| Construct                                               | A-2b         | UA-1       |
| ------------------------------------------------------- | ------------ | ---------- |
| image 1 drawn at the deepest of 29 nested `q`           | `6.1.13/8`   | silent     |
| `(A\x01B)`, whose 0x01 names no glyph under WinAnsi     | `6.2.11.8/1` | `7.21.8/1` |
| a 40000-byte string, shown in rendering mode 3          | `6.1.13/3`   | silent     |
| that string's `/Span <</Lang (-pt)>> BDC` property list | silent       | `7.2/29`   |
| image 2 at the top level, after the text                | none         | none       |

Packing them is the point: string-limit runs in the resource tier and the
other three in the content tier, so one aggressive remediation rewrites the
same stream up to four times, and each rewrite has to build on the one before
it and carry both images across. The control byte also draws `6.2.11.4.1/2`
(`7.21.4.1/2` at UA-1), and at UA-1 `7.21.7/2`, since code 1 names no glyph and
so maps to no Unicode value. The font is the conforming non-symbolic TrueType of
`ttcmap_2b.pdf`, embedded, with `/Widths` from code 1 so the `.notdef` width
agrees with the dictionary.

Measured on veraPDF 1.30.2 and litho, instance counts per rule:

| PDF/A-2b       | before | default | aggressive |
| -------------- | ------ | ------- | ---------- |
| `6.1.13/3`     | 1 / 1  | 1 / 1   | none       |
| `6.1.13/8`     | 1 / 1  | 1 / 1   | none       |
| `6.2.11.4.1/2` | 1 / 1  | 1 / 1   | none       |
| `6.2.11.8/1`   | 1 / 1  | 1 / 1   | none       |
| `6.2.4.3/4`    | 3 / 3  | none    | none       |
| `6.6.2.1/1`    | 1 / 1  | none    | none       |

Each cell reads litho / veraPDF. The three content repairs are aggressive, so
the default remediation leaves their findings as residuals, and the aggressive
one clears everything on both tools. At PDF/UA-1 the default remediation removes
the `/Lang`, which clears `7.2/29` on both, and the aggressive one strips the
control byte as well, leaving `7.1/3` alone, reported once by litho and four
times by veraPDF. Every output of the eight profiles `manifest.json` lists,
default and aggressive, carries both images byte for byte, and litho and
veraPDF agree on each one's rule set.

Before the fix both cells failed. At PDF/A-2b aggressive the string-limit
rewrite, in the resource tier, dropped the images and the text with them: the
extracted text fell from 2 bytes to 0, the rollback of the content tier could
not restore it, and the writer refused the whole remediation. At PDF/UA-1 the
`/Lang` repair never reached the output, and `7.2/29` stayed a residual.

## Scope

The runner's `compliance` feature compares verdicts, not rule sets, and
`manifest.json` expands every fixture across the eight profiles it lists. The
hand-measured matrix is wider: all thirteen conformance levels veraPDF
validates, so 70 x 13 = 910 cells. litho and veraPDF return the same verdict in
all of them: in each cell either both report nothing or both report something.

Verdict agreement is the weaker property. Rule-set agreement is what these
fixtures exist to protect, and it holds in 905 of the 910 cells. The five
exceptions, two on `complex_ua2` and three on `inlineimage_2b`, are marked `*`
in the counts table and covered under
[Known divergences](#known-divergences); veraPDF reports a non-empty set in
each, which is why the verdict still matches.

Both figures are measured by hand against a live veraPDF rather than taken from
the runner. The thirteen flavours are `1a 1b 2a 2b 2u 3a 3b 3u 4 4e 4f ua1 ua2`
on the veraPDF side and the matching `PDF/A-*` and `PDF/UA-*` names on litho's:

```bash
go build -o /tmp/litho ./cmd/litho
/tmp/litho validate pdfs/complex_2b.pdf -profile "PDF/A-2b" -f json
verapdf -f 2b --format json --maxfailuresdisplayed -1 pdfs/complex_2b.pdf
```

Rerun that comparison whenever a compliance rule changes. A new class appearing
on one side only is either a fix or a regression, and this corpus is where it
shows up first.
