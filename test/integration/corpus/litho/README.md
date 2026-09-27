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
| `gstate_2b.pdf`        | PDF/A-2b     | Transfer functions and halftones, judged at the `gs` setting one |
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
| `pagegroup_2b.pdf`     | PDF/A-2b     | Page transparency groups, and an XMP packet a page carries       |
| `fontleft_2b.pdf`      | PDF/A-2b     | Widths no program answers, and the order fonts are read in       |
| `namereads_1b.pdf`     | PDF/A-1b     | Names read again at every colour link, every `gs`, every image   |
| `colouroff_2b.pdf`     | PDF/A-2b     | Resource names a stream with its colour operators off never uses |
| `blendcs_4.pdf`        | PDF/A-4      | Which transparency blending spaces cover device colour           |
| `threedcs_4.pdf`       | PDF/A-4      | The colour space of a 3D stream, read as veraPDF builds it       |
| `undecodable_2b.pdf`   | PDF/A-2b     | Content streams veraPDF reads no operator from                   |
| `fontclaims_2b.pdf`    | PDF/A-2b     | Name-keyed CFF gaps, pattern and Type 3 program reads, `.notdef` |
| `imagereach_2b.pdf`    | PDF/A-2b     | Images a form lists or a shared content stream never draws       |
| `repairleft_4.pdf`     | PDF/A-4      | Inherited colour spaces, emptied `'` and `"`, a covered group    |
| `defaultcs_4.pdf`      | PDF/A-4      | Default colour spaces judged in place of the family they replace |
| `csnames_2b.pdf`       | PDF/A-2b     | Colour-space names inside colour space arrays, read as the entry |
| `offlimits_1b.pdf`     | PDF/A-1b     | Implementation limits in streams drawn with colour operators off |
| `xrefstm_2b.pdf`       | PDF/A-2b     | The trailer entries of a cross-reference stream, and an update's |
| `typedreads_1b.pdf`    | PDF/A-1b     | Values veraPDF reads again through typed objects of its model    |
| `fontcopies_2b.pdf`    | PDF/A-2b     | Fonts written directly, judged per copy, and CIDs past a program |
| `usecmap_2u.pdf`       | PDF/A-2u     | `/ToUnicode` CMaps built on usecmap and `/UseCMap` bases         |
| `mclang_2a.pdf`        | PDF/A-2a     | Marked-content `/Lang` entries no content walk judges            |
| `mcform_2a.pdf`        | PDF/A-2a     | Marked-content `/Lang` in forms and annotation appearances       |
| `xmpempty_1b.pdf`      | PDF/A-1b     | Empty XMP containers and control bytes against `/Info`           |
| `extvalue_2b.pdf`      | PDF/A-2b     | Extension property values against their declared types           |
| `afref_3b.pdf`         | PDF/A-3b     | File specifications something refers to but no `/AF` lists       |
| `langpua_ua2.pdf`      | PDF/UA-2     | Private Use Area characters in structure and content `/Lang`     |
| `repairroom_2b.pdf`    | PDF/A-2b     | Moves a stripped `.notdef` made, and DeviceN spaces in shadings  |
| `paintop_2b.pdf`       | PDF/A-2b     | Transparency no operator paints under, halftones judged once     |
| `imagedraws_2b.pdf`    | PDF/A-2b     | Images appearances and Type 3 glyphs draw; appearances as forms  |
| `unembednames_2u.pdf`  | PDF/A-2u     | Glyph names of unembedded fonts; one reading of Type 3 glyphs    |
| `exttypes_2b.pdf`      | PDF/A-2b     | Extension value types resolved per schema entry, not per packet  |
| `extpredef_2b.pdf`     | PDF/A-2b     | Predefined properties, structures and XPath through declarations |
| `extguards_2b.pdf`     | PDF/A-2b     | Extension-declared values veraPDF accepts, false-positive guards |
| `formcycle_ua1.pdf`    | PDF/UA-1     | Forms whose resources list each other, under auto-tagging        |
| `formflat_ua1.pdf`     | PDF/UA-1     | Tagged widgets with no `/AcroForm`, under the form mutations     |
| `remreach_2b.pdf`      | PDF/A-2b     | Defects below a page's resources the repairs did not reach       |
| `annotlang_ua1.pdf`    | PDF/UA-1     | Languages annotations name; symbolic TrueType encoding dicts     |
| `xmppage_2b.pdf`       | PDF/A-2b     | Extension schema tests on the XMP packets pages carry            |
| `parentreach_2b.pdf`   | PDF/A-2b     | Repairs of the resources a page inherits past its `/Parent`      |
| `inlineinterp_2b.pdf`  | PDF/A-2b     | Inline images asking for interpolation in every stream parsed    |
| `formhidden_ua1.pdf`   | PDF/UA-1     | Hidden and NoView widgets under the form mutations; conforming   |
| `annothidden_2b.pdf`   | PDF/A-2b     | Annotations that show nowhere, under PDF/A remediation           |
| `kidsrepair_1b.pdf`    | PDF/A-1b     | Repairs of inherited resources and of pages written in `/Kids`   |
| `inlinefs_4.pdf`       | PDF/A-4      | File specifications written inline outside any annotation `/FS`  |
| `sigtagged_ua1.pdf`    | PDF/UA-1     | A certified signature on a tagged page, under the rewrites       |
| `kidsarray_2b.pdf`     | PDF/A-2b     | Pages written directly in a `/Kids` array named by reference     |
| `formmark_ua1.pdf`     | PDF/UA-1     | Forms judged by what they draw, and the sequences they open      |
| `laterreads_2u.pdf`    | PDF/A-2u     | What a later repair reads: removed annotations, a font's uses    |
| `baretree_2b.pdf`      | PDF/A-2b     | A page tree short of the entries it requires, under the rewrites |
| `namedmcid_ua1.pdf`    | PDF/UA-1     | Marked content naming its property list; Links judged per page   |
| `directstream_2b.pdf`  | PDF/A-2b     | Streams written inside other objects, under every writer path    |

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
| `gstate_2b`                               | `GS8` and `GSi`, `/TR` states no painted stream's `gs` sets            | ISO 19005-2, 6.2.5 t1        |
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
| `paintop_2b`                              | Pages 1 to 7: a transparent state no painting operator paints under    | ISO 19005-2, 6.2.10 t2       |
| `paintop_2b`                              | `H1`, `H2`: a component a process colorant reaches before a spot one   | ISO 19005-2, 6.2.5 t6        |
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
| `formmark_ua1`                            | Five forms painting nothing untagged, two of them nothing at all       | ISO 14289-1, 7.1 t3          |
| `namedmcid_ua1`                           | Page and form content tagged through a named property list's `/MCID`   | ISO 14289-1, 7.1 t3          |
| `namedmcid_ua1`                           | Unmapped `/MCID`s inside a mapped one, around nothing, around artifact | ISO 14289-1, 7.1 t3          |
| `namedmcid_ua1`                           | A Private Use glyph covered by `/ActualText` in the page's named list  | ISO 14289-2, 8.4.3 t1        |
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
| `pagegroup_2b`                            | A page transparency group with an ICCBased blending space              | ISO 19005-2, 6.2.10 t2       |
| `pagegroup_2b`                            | A page transparency group with no `/CS` over opaque content            | ISO 19005-2, 6.2.10 t2       |
| `pagegroup_2b`                            | `xmp:CreatorTool` in the XMP packet a page carries                     | ISO 19005-2, 6.6.2.3.1       |
| `fontleft_2b`                             | `CK`, a CID its CFF charset lacks, declared at the default width       | ISO 19005-2, 6.2.11.5        |
| `fontleft_2b`                             | `TS`, Type 3 widths half a unit apart under a `/FontMatrix` of 0.01    | ISO 19005-2, 6.2.11.5        |
| `fontleft_2b`                             | `CN`, a code named `.notdef`, whose glyph the CFF charset holds        | ISO 19005-2, 6.2.11.4.1 t2   |
| `fontleft_2b`                             | `TM`, a missing CID in a form drawn while rendering mode 3 is in force | ISO 19005-2, 6.2.11.4.1 t2   |
| `fontleft_2b`                             | `TN`, a Type0 font with no `/BaseFont` over a descendant naming one    | ISO 19005-2, 6.2.11.2 t3     |
| `namereads_1b`                            | A Separation a Type 3 glyph selects past its `d1`, read by no link     | ISO 32000-1, 9.6.5           |
| `colouroff_2b`                            | Three glyphs past `d1` naming a colour resource only the page defines  | ISO 19005-2, 6.2.2 t2        |
| `colouroff_2b`                            | `PUnc`, an uncoloured cell selecting a space only the page defines     | ISO 32000-1, 8.7.3.3         |
| `blendcs_4`                               | `FmRGB` and `FmCalRGB`, isolated groups covering their DeviceRGB       | ISO 19005-4, 6.2.4.3 t2      |
| `threedcs_4`                              | `A7` to `A9`: a stream's resources, a view's stream, an untyped stream | ISO 32000-2, 13.6.3          |
| `defaultcs_4`                             | Page 16: a DefaultRGB over a monitor GRAY profile, no component count  | ISO 32000-1, 8.6.5.6         |
| `defaultcs_4`                             | Page 23: a glyph past `d1` selecting `rg` under a DefaultRGB of `5`    | ISO 32000-1, 9.6.5           |
| `defaultcs_4`                             | Page 24: a form's DefaultGray of `5` under the page's inherited fill   | ISO 19005-4, 6.2.2 t3        |
| `csnames_2b`                              | Page 17: a form's shading naming `/CS0`, which only the page defines   | ISO 32000-1, 7.8.3           |
| `csnames_2b`                              | Page 20: two Separations of one colorant over `/CS0` and `/DeviceCMYK` | ISO 19005-2, 6.2.4.4 t2      |
| `offlimits_1b`                            | Page 2: a coloured cell selecting `DN` beside an uncoloured one        | ISO 32000-1, 8.7.3.3         |
| `offlimits_1b`                            | Pages 6, 8, 9: a pattern selected with colour operators off            | ISO 32000-1, 9.6.5           |
| `offlimits_1b`                            | Pages 11 to 13: `99999999999 g` drawn with colour operators off        | ISO 19005-1, 6.1.12 t1       |
| `typedreads_1b`                           | Every `/Lang` an odd hex string decoding to a well-formed tag          | ISO 19005-1, 6.8.4           |
| `typedreads_1b`                           | The outline dictionary's `/Title`, never read again as an item's       | ISO 32000-1, 12.3.3          |
| `fontcopies_2b`                           | Two pages sharing an indirect resource or `/Font` dictionary           | one font per dictionary      |
| `fontcopies_2b`                           | `M1`: a missing CID whose `/W` entry is glyph 0's advance              | ISO 19005-2, 6.2.11.5 t1     |
| `usecmap_2u`                              | `T2`: a `/UseCMap /Adobe-Korea1-UCS2` base mapping all four codes      | ISO 19005-2, 6.2.11.7.2 t1   |
| `afref_3b`                                | `FA3`: a FileAttachment `/FS` the catalog `/AF` also lists             | ISO 19005-3, 6.8 t4          |
| `afref_3b`                                | A `/Names /JavaScript` tree written as an empty dictionary             | ISO 32000-1, 7.7.4           |
| `langpua_ua2`                             | An annotation's own `/Lang` carrying a Private Use Area character      | no CosLang reads it          |
| `imagedraws_2b`                           | `ImRef`, a drawn image carrying `/Ref`, which only a form answers for  | ISO 19005-2, 6.2.9 t2        |
| `imagedraws_2b`                           | `Stencil`, a drawn image mask with `/BitsPerComponent 2`               | ISO 19005-2, 6.2.8 t5        |
| `unembednames_2u`                         | `ZD`, a ZapfDingbats code named `a71`                                  | ISO 19005-2, 6.2.11.7.2 t1   |
| `unembednames_2u`                         | `H2`, a Helvetica only Type 3 procedures read in mode 3 show           | ISO 19005-2, 6.2.11.4.1 t1   |
| `extguards_2b`                            | GPSCoordinate, Marker and Dimensions values an extension declares      | ISO 19005-2, 6.6.2.3.1 t2    |
| `extguards_2b`                            | Thumbnail and Colorant values outside a closed choice                  | ISO 19005-2, 6.6.2.3.1 t2    |
| `extguards_2b`                            | XPath values `a:b`, `$x` and `x:f(1)/a`, which the JDK compiles        | ISO 19005-2, 6.6.2.3.1 t2    |
| `extguards_2b`                            | `xmpMM:DerivedFrom` declared a type holding `stRef:filePath`           | ISO 19005-2, 6.6.2.3.1 t2    |
| `extguards_2b`                            | `xmp:CreateDate` declared an unknown type, holding a date              | ISO 19005-2, 6.6.2.3.1 t2    |
| `annotlang_ua1`                           | An annotation whose element outside the tree carries `/Lang (de-DE)`   | ISO 14289-1, 7.2 t29         |
| `annotlang_ua1`                           | An annotation's own malformed `/Lang`, beside an element with none     | ISO 14289-1, 7.2 t29         |
| `annotlang_ua1`                           | A symbolic TrueType whose `/Encoding` lists `/Differences` only        | ISO 14289-1, 7.21.6 t3       |
| `annotlang_ua1`                           | A symbolic TrueType whose `/Encoding` is an empty dictionary           | ISO 14289-1, 7.21.6 t3       |
| `xmppage_2b`                              | `ex:pt` in a page packet, declared by the catalog packet alone         | ISO 19005-2, 6.6.2.3.1 t1    |
| `xmppage_2b`                              | A page entry typing `ex:pa` `MainT`, which the catalog's entry defines | ISO 19005-2, 6.6.2.3.3 t8    |

Verified counts at the home profile: `6.2.11.5` fires exactly once on
`complex_2b` (the rendered `T3A` glyph, not the unrendered `T3B` one) on both
litho and veraPDF, and `6.3.3` fires exactly three times, skipping the
zero-area annotation.

## Expected firing sets

Counts are distinct rule classes (`clause/test`) reported by litho. litho and
veraPDF agree exactly on the set in every cell except the two marked `*`,
which are covered under [Known divergences](#known-divergences).

| Fixture                | A-1a | A-1b | A-2a | A-2b | A-2u | A-3a | A-3b | A-3u | A-4 | A-4e | A-4f | UA-1 | UA-2 |
| ---------------------- | ---- | ---- | ---- | ---- | ---- | ---- | ---- | ---- | --- | ---- | ---- | ---- | ---- |
| `a4syntax_4.pdf`       | 17   | 15   | 21   | 19   | 19   | 21   | 19   | 19   | 16  | 17   | 18   | 13   | 8    |
| `afref_3b.pdf`         | 5    | 2    | 5    | 2    | 3    | 5    | 2    | 3    | 5   | 5    | 6    | 10   | 7    |
| `annothidden_2b.pdf`   | 5    | 2    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 11   | 6    |
| `annotkinds_ua2.pdf`   | 7    | 7    | 8    | 8    | 8    | 8    | 8    | 8    | 5   | 5    | 6    | 8    | 10   |
| `annotlang_ua1.pdf`    | 6    | 5    | 6    | 5    | 5    | 6    | 5    | 5    | 4   | 4    | 5    | 1    | 4    |
| `annots_1b.pdf`        | 16   | 14   | 14   | 12   | 12   | 14   | 12   | 12   | 10  | 10   | 11   | 12   | 8    |
| `annotstruct_ua2.pdf`  | 13   | 12   | 12   | 12   | 12   | 14   | 14   | 14   | 10  | 9    | 9    | 8    | 13   |
| `artifact_ua1.pdf`     | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 4   | 4    | 5    | 5    | 3    |
| `baretree_2b.pdf`      | 4    | 1    | 3    | 0    | 1    | 4    | 1    | 2    | 4   | 4    | 5    | 7    | 6    |
| `blendcs_4.pdf`        | 7    | 5    | 7    | 5    | 5    | 7    | 5    | 5    | 4   | 4    | 5    | 6    | 6    |
| `charset_1b.pdf`       | 7    | 4    | 7    | 4    | 5    | 7    | 4    | 5    | 3   | 3    | 4    | 9    | 7    |
| `cmaps_2b.pdf`         | 9    | 6    | 10   | 7    | 8    | 10   | 7    | 8    | 8   | 8    | 9    | 12   | 12   |
| `colouroff_2b.pdf`     | 4    | 1    | 4    | 2    | 2    | 4    | 2    | 2    | 3   | 3    | 4    | 6    | 6    |
| `complex_1b.pdf`       | 26   | 23   | 22   | 19   | 20   | 24   | 21   | 22   | 19  | 18   | 18   | 14   | 11   |
| `complex_2a.pdf`       | 18   | 15   | 21   | 17   | 17   | 22   | 18   | 18   | 17  | 17   | 18   | 12   | 11   |
| `complex_2b.pdf`       | 21   | 18   | 23   | 20   | 21   | 25   | 22   | 23   | 19  | 18   | 18   | 14   | 12   |
| `complex_2u.pdf`       | 18   | 15   | 23   | 18   | 20   | 23   | 18   | 20   | 16  | 16   | 17   | 14   | 13   |
| `complex_3b.pdf`       | 20   | 17   | 23   | 20   | 21   | 24   | 21   | 22   | 21  | 20   | 20   | 14   | 10   |
| `complex_4.pdf`        | 20   | 18   | 25   | 23   | 23   | 27   | 25   | 25   | 23  | 23   | 23   | 14   | 9    |
| `complex_ua1.pdf`      | 7    | 6    | 7    | 6    | 6    | 7    | 6    | 6    | 5   | 5    | 6    | 11   | 10   |
| `complex_ua2.pdf`      | 11   | 8    | 16   | 12   | 12   | 16   | 12   | 12   | 8   | 8    | 9    | 18\* | 18\* |
| `contentstream_4.pdf`  | 6    | 4    | 8    | 6    | 6    | 8    | 6    | 6    | 6   | 6    | 7    | 8    | 7    |
| `csnames_2b.pdf`       | 8    | 6    | 9    | 7    | 7    | 9    | 7    | 7    | 7   | 7    | 8    | 5    | 6    |
| `defaultcs_4.pdf`      | 9    | 6    | 9    | 7    | 7    | 9    | 7    | 7    | 7   | 7    | 8    | 10   | 7    |
| `directstream_2b.pdf`  | 5    | 2    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 7    | 6    |
| `docinfo_1b.pdf`       | 11   | 8    | 4    | 1    | 2    | 4    | 1    | 2    | 6   | 6    | 7    | 5    | 5    |
| `docmdp_2b.pdf`        | 6    | 4    | 8    | 6    | 6    | 8    | 6    | 6    | 5   | 5    | 6    | 10   | 8    |
| `docsync_1b.pdf`       | 11   | 9    | 6    | 4    | 4    | 6    | 4    | 4    | 6   | 6    | 7    | 9    | 8    |
| `encperm_ua1.pdf`      | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 6    | 2    | 4    |
| `extguards_2b.pdf`     | 9    | 6    | 6    | 3    | 4    | 7    | 4    | 5    | 5   | 5    | 6    | 5    | 5    |
| `extpredef_2b.pdf`     | 7    | 4    | 6    | 3    | 4    | 7    | 4    | 5    | 5   | 5    | 6    | 5    | 5    |
| `extprop_2b.pdf`       | 7    | 4    | 6    | 3    | 4    | 7    | 4    | 5    | 6   | 6    | 7    | 8    | 7    |
| `extschema_2b.pdf`     | 12   | 9    | 11   | 8    | 9    | 12   | 9    | 10   | 6   | 6    | 7    | 8    | 8    |
| `exttypes_2b.pdf`      | 10   | 7    | 9    | 6    | 7    | 10   | 7    | 8    | 5   | 5    | 6    | 5    | 6    |
| `extvalue_2b.pdf`      | 7    | 4    | 6    | 3    | 4    | 7    | 4    | 5    | 5   | 5    | 6    | 5    | 6    |
| `fenote_ua2.pdf`       | 4    | 3    | 5    | 4    | 4    | 5    | 4    | 4    | 3   | 3    | 4    | 5    | 6    |
| `filestruct_1b.pdf`    | 15   | 13   | 12   | 10   | 10   | 12   | 10   | 10   | 13  | 13   | 14   | 10   | 8    |
| `fontclaims_2b.pdf`    | 9    | 6    | 9    | 6    | 7    | 9    | 6    | 7    | 7   | 7    | 8    | 11   | 11   |
| `fontcopies_2b.pdf`    | 14   | 11   | 13   | 10   | 11   | 13   | 10   | 11   | 11  | 11   | 12   | 13   | 13   |
| `fontdict_2b.pdf`      | 9    | 7    | 10   | 8    | 8    | 10   | 8    | 8    | 8   | 8    | 9    | 7    | 7    |
| `fontkeys_1b.pdf`      | 13   | 10   | 13   | 10   | 11   | 13   | 10   | 11   | 11  | 11   | 12   | 8    | 8    |
| `fontleft_2b.pdf`      | 7    | 4    | 9    | 5    | 7    | 9    | 5    | 7    | 7   | 7    | 8    | 14   | 13   |
| `forbidden_mix.pdf`    | 14   | 12   | 14   | 12   | 12   | 16   | 14   | 14   | 14  | 13   | 13   | 11   | 9    |
| `formcs_2b.pdf`        | 6    | 4    | 6    | 4    | 4    | 6    | 4    | 4    | 5   | 5    | 6    | 7    | 7    |
| `formcycle_ua1.pdf`    | 4    | 2    | 4    | 2    | 2    | 4    | 2    | 2    | 3   | 3    | 4    | 6    | 6    |
| `formflat_ua1.pdf`     | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 6   | 6    | 7    | 1    | 3    |
| `formhidden_ua1.pdf`   | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 4   | 4    | 5    | 0    | 5    |
| `formmark_ua1.pdf`     | 4    | 4    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 2    | 3    |
| `formmedia_ua1.pdf`    | 8    | 8    | 8    | 8    | 8    | 8    | 8    | 8    | 7   | 7    | 8    | 7    | 5    |
| `formstruct_ua2.pdf`   | 4    | 4    | 6    | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 6    | 7    | 7    |
| `glyphs_2b.pdf`        | 7    | 4    | 8    | 5    | 6    | 8    | 5    | 6    | 6   | 6    | 7    | 10   | 10   |
| `grouping_ua2.pdf`     | 3    | 3    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 5    | 8    |
| `gstate_2b.pdf`        | 9    | 7    | 13   | 11   | 11   | 13   | 11   | 11   | 10  | 10   | 11   | 7    | 7    |
| `gstateform_1b.pdf`    | 9    | 6    | 8    | 5    | 6    | 8    | 5    | 6    | 9   | 9    | 10   | 8    | 7    |
| `headings_ua1.pdf`     | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 4   | 4    | 5    | 5    | 5    |
| `iccbased_4.pdf`       | 5    | 3    | 7    | 5    | 5    | 7    | 5    | 5    | 5   | 5    | 6    | 8    | 7    |
| `idfields_ua2.pdf`     | 6    | 6    | 7    | 7    | 7    | 7    | 7    | 7    | 4   | 4    | 5    | 4    | 6    |
| `idprefix_2b.pdf`      | 13   | 10   | 12   | 9    | 10   | 13   | 10   | 11   | 10  | 10   | 11   | 8    | 7    |
| `imagedraws_2b.pdf`    | 9    | 6    | 9    | 7    | 7    | 9    | 7    | 7    | 8   | 8    | 9    | 10   | 7    |
| `imagereach_2b.pdf`    | 4    | 2    | 4    | 2    | 2    | 4    | 2    | 2    | 3   | 3    | 4    | 5    | 6    |
| `imgxmp_4.pdf`         | 10   | 8    | 15   | 13   | 13   | 15   | 13   | 13   | 8   | 9    | 10   | 7    | 6    |
| `inlinefs_4.pdf`       | 7    | 5    | 9    | 7    | 7    | 11   | 9    | 9    | 4   | 4    | 5    | 10   | 6    |
| `inlineimage_2b.pdf`   | 7    | 4    | 10   | 6    | 7    | 10   | 6    | 7    | 6   | 6    | 7    | 10   | 10   |
| `inlineinterp_2b.pdf`  | 7    | 4    | 6    | 4    | 4    | 6    | 4    | 4    | 5   | 5    | 6    | 10   | 8    |
| `jpx_2b.pdf`           | 4    | 2    | 9    | 7    | 7    | 9    | 7    | 7    | 8   | 8    | 9    | 5    | 6    |
| `kidsarray_2b.pdf`     | 6    | 3    | 6    | 3    | 4    | 7    | 4    | 5    | 7   | 7    | 8    | 12   | 8    |
| `kidsrepair_1b.pdf`    | 6    | 3    | 6    | 3    | 4    | 6    | 3    | 4    | 6   | 6    | 7    | 5    | 5    |
| `langpua_ua2.pdf`      | 7    | 5    | 8    | 7    | 7    | 8    | 7    | 7    | 4   | 4    | 5    | 4    | 3    |
| `laterreads_2u.pdf`    | 12   | 9    | 12   | 8    | 9    | 15   | 11   | 12   | 14  | 13   | 14   | 14   | 11   |
| `limits_1b.pdf`        | 12   | 9    | 9    | 6    | 7    | 9    | 6    | 7    | 4   | 4    | 5    | 11   | 9    |
| `limits_2b.pdf`        | 10   | 8    | 12   | 10   | 10   | 12   | 10   | 10   | 6   | 6    | 7    | 10   | 8    |
| `linearized_1b.pdf`    | 6    | 4    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 7    | 7    |
| `linkfont_ua2.pdf`     | 7    | 6    | 8    | 7    | 8    | 8    | 7    | 8    | 5   | 5    | 6    | 4    | 5    |
| `mcform_2a.pdf`        | 3    | 2    | 3    | 2    | 2    | 4    | 3    | 3    | 6   | 6    | 7    | 7    | 6    |
| `mclang_2a.pdf`        | 2    | 2    | 1    | 1    | 1    | 2    | 2    | 2    | 5   | 5    | 6    | 4    | 4    |
| `megafont_2b.pdf`      | 12   | 9    | 13   | 10   | 11   | 13   | 10   | 11   | 10  | 10   | 11   | 12   | 11   |
| `metafilter_1b.pdf`    | 6    | 3    | 6    | 3    | 4    | 6    | 3    | 4    | 6   | 6    | 7    | 8    | 7    |
| `multipage_2b.pdf`     | 12   | 10   | 13   | 11   | 11   | 13   | 11   | 11   | 10  | 10   | 11   | 10   | 9    |
| `namedmcid_ua1.pdf`    | 6    | 6    | 7    | 7    | 7    | 7    | 7    | 7    | 6   | 6    | 7    | 7    | 5    |
| `namereads_1b.pdf`     | 10   | 7    | 10   | 8    | 8    | 10   | 8    | 8    | 7   | 7    | 8    | 6    | 6    |
| `offlimits_1b.pdf`     | 9    | 6    | 9    | 7    | 7    | 9    | 7    | 7    | 6   | 6    | 7    | 7    | 7    |
| `outputintent_2b.pdf`  | 6    | 4    | 7    | 5    | 5    | 7    | 5    | 5    | 6   | 6    | 7    | 7    | 7    |
| `pagegroup_2b.pdf`     | 7    | 4    | 7    | 4    | 5    | 8    | 5    | 6    | 6   | 6    | 7    | 6    | 6    |
| `paintop_2b.pdf`       | 7    | 4    | 5    | 2    | 3    | 6    | 3    | 4    | 6   | 6    | 7    | 9    | 7    |
| `parentreach_2b.pdf`   | 7    | 3    | 7    | 3    | 4    | 8    | 4    | 5    | 7   | 7    | 8    | 7    | 7    |
| `patterncs_2b.pdf`     | 5    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 10   | 8    |
| `puatext_ua2.pdf`      | 5    | 5    | 6    | 6    | 6    | 6    | 6    | 6    | 4   | 4    | 5    | 3    | 4    |
| `remreach_2b.pdf`      | 8    | 4    | 6    | 3    | 4    | 7    | 4    | 5    | 7   | 6    | 6    | 10   | 8    |
| `repairleft_4.pdf`     | 10   | 8    | 16   | 13   | 14   | 16   | 13   | 14   | 9   | 10   | 11   | 11   | 9    |
| `repairroom_2b.pdf`    | 5    | 2    | 7    | 3    | 5    | 8    | 4    | 6    | 8   | 8    | 9    | 10   | 9    |
| `resinherit_2b.pdf`    | 9    | 6    | 11   | 8    | 9    | 11   | 8    | 9    | 9   | 9    | 10   | 9    | 9    |
| `schemaprefix_1b.pdf`  | 17   | 14   | 18   | 15   | 16   | 18   | 15   | 16   | 6   | 6    | 7    | 8    | 8    |
| `sigcms_2b.pdf`        | 6    | 4    | 9    | 7    | 7    | 9    | 7    | 7    | 5   | 5    | 6    | 10   | 8    |
| `sigtagged_ua1.pdf`    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 4   | 4    | 5    | 0    | 4    |
| `structkinds_ua2.pdf`  | 6    | 5    | 7    | 6    | 6    | 7    | 6    | 6    | 3   | 3    | 4    | 5    | 7    |
| `structnest_ua1.pdf`   | 5    | 4    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 36   | 30   |
| `syntax_1b.pdf`        | 11   | 9    | 11   | 9    | 9    | 11   | 9    | 9    | 9   | 9    | 10   | 12   | 8    |
| `tableregular_ua2.pdf` | 4    | 3    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 8    | 7    |
| `tables_ua1.pdf`       | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 4   | 4    | 5    | 2    | 5    |
| `tablespan_ua2.pdf`    | 3    | 3    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 4    | 4    |
| `threed_4e.pdf`        | 7    | 5    | 8    | 6    | 6    | 8    | 6    | 6    | 5   | 5    | 6    | 11   | 8    |
| `threedcs_4.pdf`       | 8    | 6    | 9    | 7    | 7    | 9    | 7    | 7    | 6   | 5    | 7    | 8    | 6    |
| `trailer_2b.pdf`       | 7    | 5    | 7    | 5    | 5    | 7    | 5    | 5    | 6   | 6    | 7    | 7    | 7    |
| `ttcmap_2b.pdf`        | 8    | 5    | 9    | 6    | 7    | 9    | 6    | 7    | 6   | 6    | 7    | 9    | 8    |
| `ttencoding_2b.pdf`    | 10   | 7    | 10   | 7    | 8    | 10   | 7    | 8    | 8   | 8    | 9    | 10   | 10   |
| `typedreads_1b.pdf`    | 10   | 8    | 9    | 8    | 8    | 11   | 10   | 10   | 9   | 8    | 8    | 7    | 11   |
| `uaid_ua1.pdf`         | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 6    | 5    | 4    |
| `undecodable_2b.pdf`   | 9    | 6    | 9    | 7    | 7    | 9    | 7    | 7    | 8   | 8    | 9    | 11   | 9    |
| `usecmap_2u.pdf`       | 5    | 2    | 8    | 3    | 5    | 8    | 3    | 5    | 5   | 5    | 6    | 9    | 10   |
| `unembednames_2u.pdf`  | 7    | 4    | 9    | 5    | 7    | 9    | 5    | 7    | 7   | 7    | 8    | 11   | 11   |
| `usedcs_2b.pdf`        | 6    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 7    | 7    |
| `xfatable_ua1.pdf`     | 5    | 5    | 6    | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 6    | 4    | 7    |
| `xmpempty_1b.pdf`      | 8    | 5    | 6    | 3    | 4    | 6    | 3    | 4    | 7   | 7    | 8    | 5    | 6    |
| `xmppage_2b.pdf`       | 6    | 3    | 14   | 11   | 12   | 15   | 12   | 13   | 5   | 5    | 6    | 5    | 5    |
| `xmpsyntax_1b.pdf`     | 9    | 7    | 9    | 7    | 7    | 9    | 7    | 7    | 7   | 7    | 8    | 9    | 8    |
| `xobjstate_2b.pdf`     | 8    | 6    | 11   | 9    | 9    | 11   | 9    | 9    | 10  | 10   | 11   | 7    | 7    |
| `xpacket_1b.pdf`       | 8    | 5    | 9    | 6    | 7    | 9    | 6    | 7    | 9   | 9    | 10   | 8    | 7    |
| `xref_1b.pdf`          | 7    | 5    | 6    | 4    | 4    | 6    | 4    | 4    | 5   | 5    | 6    | 7    | 7    |
| `xrefstm_2b.pdf`       | 12   | 10   | 8    | 6    | 6    | 8    | 6    | 6    | 6   | 6    | 7    | 4    | 5    |

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

veraPDF builds a graphics state only from a `gs` operator, so a state no `gs`
sets is judged nowhere. It judges a state written as an indirect object once
for the whole document, one written directly once for every `gs` that sets it,
and a halftone written as an indirect object once, whatever states name it.
The six states above are indirect and set once each. The same page also sets
these, all written directly in a resource dictionary:

| State  | Set by                                        | Answers at PDF/A-2b |
| ------ | --------------------------------------------- | ------------------- |
| GS7    | two `gs`, holding `/TR`                       | `6.2.5/1` x2        |
| GS8    | no `gs`, holding `/TR`                        | nothing             |
| GS9    | two `gs`, holding `/RI /Foo`                  | `6.2.6/1` x2        |
| H1, H2 | one `gs` each, naming one indirect type 6 HT  | `6.2.5/4` once      |
| GSn    | one `gs` in `FmInner`, which `FmOuter` paints | `6.2.5/3`           |
| GSi    | one `gs` in `FmIdle`, which no `Do` paints    | nothing             |

litho judged every state the page's resources, the resources of the forms the
page's resources list and the annotation appearances' resources declared, once
each, whatever set it: GS8 and GSi failed, GS7 and GS9 failed once, the shared
halftone once per state, and `GSn`, one form further down, not at all.

| Rule      | Profile  | veraPDF | litho | litho before |
| --------- | -------- | ------- | ----- | ------------ |
| `6.2.5/1` | PDF/A-2b | 3       | 3     | 4            |
| `6.2.5/3` | PDF/A-2b | 2       | 2     | 1            |
| `6.2.5/4` | PDF/A-2b | 2       | 2     | 3            |
| `6.2.6/1` | PDF/A-2b | 2       | 2     | 1            |
| `6.2.8/1` | PDF/A-1b | 3       | 3     | 4            |
| `6.2.9/1` | PDF/A-1b | 2       | 2     | 1            |
| `6.2.5/1` | PDF/A-4  | 3       | 3     | 4            |
| `6.2.5/4` | PDF/A-4  | 2       | 2     | 3            |

The rule sets match veraPDF 1.30.2 in all thirteen cells, and the counts at
every PDF/A level. In the counts table, GS9 adds `6.2.9/1` at the part-1
levels and `6.2.6/1` at the part-2 and part-3 levels. Remediation leaves the direct states as they
are, so `6.2.5/1`, `/3`, `/4` and `6.2.6/1` stay after a PDF/A-2b repair, and
`6.2.8/1` and `6.2.9/1` after a PDF/A-1b one; both tools report the same
residual set on every output.

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
reached. `imgxmp_4.pdf` closes the other eight, on both tools.

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

Both tools report the same rule set on this fixture at every profile the gate
runs, and the gate's `ruleset_mismatch` holds it there. Two classes depend on
litho reading the file the way veraPDF does.

**`6.7.2.1/2` to `/5` at PDF/A-4, and the part-2 and part-3 spelling
`6.6.2.1/2` to `/5`.** ISO 19005-4, 6.7.2.1 t4 reads "all content of all XMP
packets located in any metadata stream present in the PDF", and veraPDF builds
a `PDMetadata` for every dictionary carrying a `/Metadata` key whose `/Type` is
not `/Catalog`, so a page-level packet is an `XMPPackage` like any other.
`f01dc81` made litho check the packet of every metadata stream, not only the
catalog's, so both tools report all four.

**`7.2/34` at PDF/UA-1.** The rule's subject is veraPDF's `SETextItem`, and
this fixture shows no glyphs, only image XObjects. `6f22ce2` made a text item a
precondition of the rule, as under `docinfo_1b.pdf` above, so neither tool
reports it here.

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

**`7.2/34` at PDF/UA-1 no longer disagrees.** The facet described under
[Second bug found](#second-bug-found-pdfua-1-language-coverage-fixed) was text
outside the structure tree. `c1c11d3` judges the language of text no structure
element owns, and both tools now report the rule on this file, so `7.1/12` is
the cell's only difference.

One veraPDF gap sits outside the corpus, because planting it would add a
disagreeing cell rather than coverage.

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

## Second bug found: PDF/UA-1 language coverage (fixed)

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

A `7.2/34` divergence on `complex_ua2.pdf` outlived that fix. It was a
**different facet** of the same rule, not the latch: that file's real content
sits outside the structure tree entirely, and litho's t34 walk visited
structure elements only. `c1c11d3` closed it by judging the language of text no
structure element owns, and both tools now report the rule on that file.

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
it, and so does every `/Lang` veraPDF reads as a CosLang, which
`langpua_ua2.pdf` covers. Each defect sits beside a twin carrying the same words
without the PUA character, so a check condemning every `/ActualText`, `/Alt` or
title fires on the control.

## Private Use Area characters in language entries

`langpua_ua2.pdf` covers the rest of ISO 14289-2, 8.6 t1. veraPDF's
`CosTextString` has one subtype, `CosLang`, built for the catalog `/Lang`, a
structure element's `/Lang`, the `/Lang` of the element an annotation or a form
field names by `/StructParent`, and the `/Lang` of a marked-content property
list. The verapdf corpus reaches the catalog position (`PDF_UA-2__8.6-t01-fail-a`)
and `puatext_ua2.pdf` the outline title; this file carries the two a document
states most often.

| Construct                      | Rule    |
| ------------------------------ | ------- |
| `/Lang` on a structure element | `8.6/1` |
| `/Lang` on a BDC property list | `8.6/1` |

A Private Use Area character is no letter, so each defect also breaks the
Language-Tag grammar: ISO 14289-2 `8.4.4/2`, ISO 14289-1 `7.2/29`, `6.8.4/1`
at PDF/A-1a and `6.7.4/1` at PDF/A-2a and 3a. Each sits beside a twin whose `/Lang` is a plain
tag, and the control is an annotation whose own `/Lang` carries the same
character: veraPDF reads an annotation's language from the element its
`/StructParent` names (GFPDAnnot.getLang), never from the annotation, so
neither rule may fire on it.

The rule sets equal veraPDF 1.30.2's in all thirteen cells and at both WTPDF
levels, and so do the counts of the rules the defects reach:

| Profile  | Rule      | litho | veraPDF 1.30.2 | litho before |
| -------- | --------- | ----- | -------------- | ------------ |
| PDF/UA-2 | `8.6/1`   | 2     | 2              | 0            |
| PDF/UA-2 | `8.4.4/2` | 2     | 2              | 2            |
| PDF/UA-1 | `7.2/29`  | 2     | 2              | 2            |
| PDF/A-1a | `6.8.4/1` | 2     | 2              | 2            |
| PDF/A-2a | `6.7.4/1` | 2     | 2              | 2            |
| PDF/A-3a | `6.7.4/1` | 2     | 2              | 2            |

The gate compares rule sets, so it sees the two positions as one `8.6/1`: a
change that loses one of them and keeps the other passes it. The unit test
`TestTextStringPUA` pins each position on its own, the enclosing element of an
annotation outside the tree among them.

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

Each cross-reference entry is 20 bytes, its end-of-line marker included, as ISO
32000-1, 7.5.4 requires. The builder used to write a space before CR LF, 21
bytes, which qpdf reports as an invalid entry and repairs by reconstructing the
table. Neither litho's nor veraPDF's firing set moved on any of the 13 profiles
when the entries shrank.

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

### `parentreach_2b.pdf`

`resinherit_2b.pdf` has a page whose `/Parent` names the root while an
intermediate node lists it, but that node lends a font, which no repair
touches. Here four pages sit under four intermediate nodes, each `/Parent`
naming the root, and each node lends its page a defect one repair owns:

| Page | Its node lends                                         | Repair              |
| ---- | ------------------------------------------------------ | ------------------- |
| `Gs` | `GS0`, a graphics state with a transfer function       | graphics            |
| `Dn` | `CSD`, a DeviceN of nine colorants and no `/Colorants` | devicen, aggressive |
| `Fm` | `Fm0`, a form stating no `/Resources`, and `GS1`       | nested-resources    |
| `Lg` | `P1`, a property list whose `/Lang` is no language tag | marked-content-lang |

The root lends `GSR`, a conforming graphics state no page draws with, and a
CMYK OutputIntent covers the DeviceCMYK alternate. Every page names what it
draws only through its node, so each also answers `6.2.2/2` from PDF/A-2 on.

| Rule at PDF/A-2b               | veraPDF | litho |
| ------------------------------ | ------- | ----- |
| `6.2.2/2` names only inherited | 5       | 5     |
| `6.2.4.4/1` DeviceN colorants  | 3       | 3     |
| `6.2.5/1` transfer function    | 1       | 1     |

At PDF/A-1b the DeviceN answers `6.1.12/9`, past eight colorants, and the
graphics state `6.2.8/1`. The two tools return the same rule set and the same
counts in all thirteen cells.

The repairs of the resources a page inherits climbed `/Parent`, so they
repaired the root's dictionary, and page-resources pinned the root's
`/Resources` on every page. The PDF/A-2 and PDF/A-3 outputs passed both tools
with every name the pages draw undefined: the form no longer drew, and the fill
named no colour space. PDF/A-4 caught it (`6.2.2/3`). At PDF/A-1b, where no
repair pins `/Resources` on a page, the defects stayed where no repair reached
them, `6.2.8/1` and `6.1.12/9` in both modes, and PDF/UA-1 and PDF/UA-2 kept
the property list's `/Lang` (`7.2/29`, `8.4.4/2`). Resolved through the node
listing each page, every output passes both tools but the default PDF/A-1b
one, which keeps `6.1.12/9`, a trim only the aggressive remediation makes.

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

### `pagegroup_2b.pdf`

ISO 19005-2, 6.2.10 test 2 asks every page that contains transparency, in a
document without a PDF/A OutputIntent, for a `/Group` whose `/CS` names the
default blending colour space. The file declares no OutputIntent, and every
page paints in an ICCBased space, so no page uses a device colour space of its
own. Pages 1 to 4 and 6 paint through constant alpha, page 5 paints opaquely,
and page 7 paints a form with an isolated transparency group:

| Page | `/Group`                              | Answers at PDF/A-2b |
| ---- | ------------------------------------- | ------------------- |
| 1    | none                                  | `6.2.10/2`          |
| 2    | `/S /Transparency`, an ICCBased `/CS` | nothing             |
| 3    | `/S /Transparency`, no `/CS`          | `6.2.10/2`          |
| 4    | `/S /Transparency`, `/CS /DeviceRGB`  | `6.2.4.3/2`         |
| 5    | `/S /Transparency`, no `/CS`          | nothing             |
| 6    | `/S /Transparency`, `/CS /CS0`        | `6.2.10/2`          |
| 7    | `/S /Transparency`, `/CS /CS0`        | `6.2.10/2`          |

Page 2 is the control. Its ICCBased blending space answers the rule, and a
repair of pages 1 and 3 has to leave it in place: litho's group repair dropped
every `/CS` that was not a device space, which left page 2, and on a later
round the space it had just given page 1, failing a rule the source passed.
Page 3 is the group a check reading "has a `/Group`" as enough lets through.
Page 4 blends in DeviceRGB, which test 2 of 6.2.4.3 allows only under an RGB
OutputIntent.

Pages 5 to 7 are how veraPDF reads the test's two halves. It decides that a
page contains transparency from what its content and annotations draw, never
from the page's own `/Group`, so page 5, a group with no `/CS` over opaque
content, needs no blending space. It reads a group's `/CS` with no resources
(`PDGroup.getColorSpace`), so the `/CS0` pages 6 and 7 name, the ICCBased entry
of their own `/ColorSpace`, is no blending space. litho counted the page group
as transparency and any `/CS` that is not null as a blending space, so it
failed page 5 and passed pages 6 and 7: three findings at PDF/A-2b where
veraPDF reports four, one of them on a page veraPDF passes. The group repair
now replaces a named `/CS` with an ICCBased sRGB space, as it does a device
one, and every remediated output passes 6.2.10 test 2 on both tools.

Page 1 also carries its own XMP packet. ISO 19005-2, 6.6.2.3.1 constrains the
properties of every metadata stream in the file, not only the catalog's:
`photoshop:History` is no property of the XMP 2005 Photoshop schema and fails
tests 1 and 2, `dc:format` is a language alternative where the schema defines
MIMEType text and fails test 2, and `xmp:CreatorTool` is a predefined text
property that stays silent. litho prefixes a finding in that packet with the
object that holds it, `object N: `, which its schema repair read as part of the
namespace URI, so it claimed the finding and stripped nothing. Part 1 judges
the catalog packet alone, so the page packet is silent at PDF/A-1b.

At PDF/A-1b the six page groups and the form group on page 7 are what ISO
19005-1, 6.4 test 3 forbids, and page 4's blending space fails 6.2.3.3 test 1. That is the profile where a
repair removing the groups and another synthesizing them again showed: the
remediation took a second round to converge.

| Profile  | Rule          | litho | veraPDF 1.30.2 | litho before |
| -------- | ------------- | ----- | -------------- | ------------ |
| PDF/A-2b | `6.2.10/2`    | 4     | 4              | 3            |
| PDF/A-2b | `6.2.4.3/2`   | 1     | 1              | 1            |
| PDF/A-2b | `6.6.2.3.1/1` | 1     | 1              | 1            |
| PDF/A-2b | `6.6.2.3.1/2` | 2     | 2              | 2            |
| PDF/A-4  | `6.2.9/2`     | 4     | 4              | 3            |
| PDF/A-1b | `6.4/3`       | 7     | 7              | 7            |
| PDF/A-1b | `6.4/6`       | 5     | 5              | 5            |
| PDF/A-1b | `6.2.3.3/1`   | 1     | 1              | 1            |
| PDF/A-1b | `6.7.11/2`    | 1     | 1              | 1            |

The two tools return the same rule set in all thirteen cells, and the same
instance count for every rule at every PDF/A level.

### `fontleft_2b.pdf`

`fontleft_2b.pdf` covers two things no other corpus file reached: glyph widths
the font program cannot answer for, and the order in which veraPDF reads a
font's program, which decides whether the presence and width rules judge a
glyph at all. veraPDF builds a font's model, and reads its program, once per
font object and rendering mode, for the stream it parses first; a later stream
has its glyphs judged by neither rule unless it is the first to show the font
in some other mode.

Every construct rides on a font of its own, so each count has one source:

| Font | Construct                                                               | Answers at PDF/A-2b                        |
| ---- | ----------------------------------------------------------------------- | ------------------------------------------ |
| `CC` | CID 3, absent from a CID-keyed CFF charset, `/W` 500 against `/DW` 1000 | `6.2.11.4.1/2`, `6.2.11.5/1`, `6.2.11.8/1` |
| `CK` | the same CID with no `/W` entry, so declared at the default width       | `6.2.11.4.1/2`, `6.2.11.8/1`               |
| `CN` | a bare CFF whose `/Differences` name code 65 `.notdef`, `/Widths` 500   | `6.2.11.5/1`, `6.2.11.8/1`                 |
| `T3` | a Type 3 code named `/b` with no glyph procedure, `/Widths` 500         | `6.2.11.4.1/2`, `6.2.11.5/1`               |
| `TS` | a Type 3 procedure declaring 50 against 50.5, `/FontMatrix` 0.01        | nothing                                    |
| `TF` | a form drawn before the page text: CIDs 5, 6 and 8 in it, 7 after it    | `6.2.11.4.1/2` x3, `6.2.11.8/1` x4         |
| `TA` | CID 5 in the page text, CIDs 6 and 7 in an annotation appearance        | `6.2.11.4.1/2`, `6.2.11.8/1` x3            |
| `TM` | CID 5 in a form drawn while rendering mode 3 is in force                | `6.2.11.8/1`                               |
| `TN` | a Type0 font with no `/BaseFont` over a descendant that names one       | nothing                                    |

The composite TrueType fonts embed a three-glyph program, so CID 5 and above
name no glyph, and their `/DW` equals the advance of glyph 0: those glyphs
answer the presence and `.notdef` rules and never the width rule.

`CC` and `CK` differ by one `/W` entry. veraPDF weighs a CID its CFF charset
lacks at the CIDFont's default width, so the glyph fails the width rule only
where `/W` declares something else. `T3` is weighed the same way, at a missing
width of 0, and `TS` holds the control for a comparison veraPDF makes in glyph
space, where the `/FontMatrix` plays no part.

`TF`, `TA` and `TM` are the reading order. At PDF/A-2 a form is parsed where
the page draws it, so the form's three glyphs are judged and the page text's
CID 7 is not; the page text is parsed before the annotation, so `TA` answers
once. At PDF/A-1 veraPDF parses a page's annotations before its content and
`TA` answers twice, which is why `6.3.5/1` counts 8 at PDF/A-1b where
`6.2.11.4.1/2` counts 7. At PDF/UA a form claims its modes where drawn but none
of its glyphs is judged, and annotations come first as at PDF/A-1, so the
presence rule counts 5. `TM` inherits rendering mode 3 from the page, which
exempts it from the presence rule everywhere.

The per-rule instance counts equal veraPDF 1.30.2's in all thirteen cells,
except ISO 14289-1, 7.1 t3 and 7.2 t34 and ISO 14289-2, 8.2.2 t1, which veraPDF
states once per untagged operator (10) and litho once per page (1), the family
described under [Instance counts](#instance-counts-which-the-corpus-does-not-pin).

### `namereads_1b.pdf`

ISO 19005-1, 6.1.12 test 4 bounds a name to 127 bytes, and veraPDF judges a
name once as a COS object and again each time a typed object of its model
wraps it: the colorant name of a Separation or a DeviceN at every link an
operator makes to the colour space, the `/RI` and `/BM` of a graphics state at
every `gs` that builds it, and the `/Intent` of an image, of its `/SMask` and of
an inline image. A space or a graphics state written directly carries no
identity, so it is read again at every link, and one written as an indirect
object once. No file of any corpus puts a long name in one of these places.

Fourteen distinct 130-byte names each sit in one place, so the file fails the
test 42 times: once per name in the object sweep, once more where a resource
dictionary reads a Separation or DeviceN colorant itself, and once per re-read.
The builder's table lists every name with its count. The control is a
Separation a Type 3 glyph selects past its `d1`, which ignores its colour
operators (ISO 32000-1, 9.6.5), so no link reads it again. The direct graphics
state carrying an `/RI` is set by one `gs`; `gstate_2b.pdf` holds the direct
states two `gs` set, which fail the rendering-intent test at each of them.

| Rule at PDF/A-1b                      | veraPDF | litho | litho before |
| ------------------------------------- | ------- | ----- | ------------ |
| `6.1.12/4` name past 127 bytes        | 42      | 42    | 19           |
| `6.2.3.3/2` DeviceCMYK                | 19      | 19    | 19           |
| `6.2.3.3/3` DeviceGray                | 3       | 3     | 3            |
| `6.2.9/1` rendering intent            | 5       | 5     | 5            |
| `6.4/2` soft mask, `6.4/4` blend mode | 1, 2    | 1, 2  | 1, 2         |
| `6.7.2/1` no metadata                 | 1       | 1     | 1            |

At the part 2, 3 and 4 levels the glyph's `cs` also stops failing 6.2.2 t2, as
in `colouroff_2b.pdf`.

### `colouroff_2b.pdf`

ISO 19005-2, 6.2.2 test 2 fails a content stream that resolves a resource name
only through the page's resources, once per stream. A Type 3 glyph past its
`d1` ignores its colour operators (ISO 32000-1, 9.6.5, Table 113), and so does
the cell of an uncoloured tiling pattern (8.7.3.3). veraPDF resolves the name
of no `cs`, `CS`, `sh`, `scn` or `SCN` there, so none of them is an inherited
name. Four glyphs, each in a Type 3 font of its own, and two tiling cells use
names only the page defines:

| Stream              | Names through the page | `6.2.2/2` |
| ------------------- | ---------------------- | --------- |
| `T1` glyph past d1  | `/CS0 cs`              | silent    |
| `T2` glyph past d1  | `/Sh0 sh`              | silent    |
| `T3` glyph past d1  | `/Pattern cs /P0 scn`  | silent    |
| `T4` glyph after d0 | `/CS0 cs`              | fires     |
| `PUnc` cell         | `/CS0 cs`              | silent    |
| `PCol` cell         | `/CS0 cs`              | fires     |

A form paints `PUnc`, not the page: an uncoloured pattern turns the colour
operators off for every tiling cell the same stream paints, which would silence
the `PCol` control too. Every glyph has a font of its own because veraPDF gives
the glyphs of one Type 3 font a single set of inherited names.

| Rule at PDF/A-2b   | veraPDF | litho | litho before |
| ------------------ | ------- | ----- | ------------ |
| `6.2.2/2`          | 2       | 2     | 6            |
| `6.6.2.1/1` no XMP | 1       | 1     | 1            |

At PDF/A-4 the same streams answer ISO 19005-4, 6.2.2 t2 the same way.

### `blendcs_4.pdf`

ISO 19005-4, 6.2.4.3 lets a device-independent transparency blending space of
the same model cover DeviceRGB and DeviceCMYK. veraPDF tests the blending
space's type, an ICC profile naming `RGB ` or a CalRGB space for DeviceRGB, an
ICC profile naming `CMYK` for DeviceCMYK, and its DeviceGray test reads the
output intent alone, so nothing covers DeviceGray. It reads a group's `/CS` with
no resources, so a name of a `/ColorSpace` entry is no blending space at all.
litho let a gray ICC, CalGray or Lab isolated group cover DeviceGray.

| Stream      | Group `/CS`                          | `6.2.4.3` |
| ----------- | ------------------------------------ | --------- |
| page 1      | `/CS0`, a name                       | `/2` x2   |
| `FmName`    | `/CS1`, a name of its own entry      | `/2` x2   |
| `FmGray`    | `[/ICCBased]` over a `GRAY` profile  | `/4` x2   |
| `FmCalGray` | `[/CalGray]`                         | `/4` x2   |
| `FmLab`     | `[/Lab]`                             | `/4` x2   |
| `FmRGB`     | `[/ICCBased]` over an `RGB ` profile | silent    |
| `FmCalRGB`  | `[/CalRGB]`                          | silent    |

Each stream paints `rg` or `g` and one fill. The forms sit on a second page,
painted in this order, the two controls last; the named page group sits alone
on the first, which paints nothing transparent, so the page needs no blending
space under ISO 19005-4, 6.2.9 t2 and the rule stays out of its count.
`pagegroup_2b.pdf` holds named page groups over transparent content.

| Rule at PDF/A-4          | veraPDF | litho | litho before |
| ------------------------ | ------- | ----- | ------------ |
| `6.2.4.3/2` DeviceRGB    | 4       | 4     | 4            |
| `6.2.4.3/4` DeviceGray   | 6       | 6     | 0            |
| `6.2.9/2` blending space | 1       | 1     | 1            |
| `6.7.2.1/1` no metadata  | 1       | 1     | 1            |

### `threedcs_4.pdf`

veraPDF reads the colour space of a 3D stream, one whose `/Type` is `/3D`, from
its `/ColorSpace` alone (`GFPD3DStream`), following a 3D reference dictionary
to its stream. A name, alone or in a one-element array, reads as `cs` would
select it in the page's resources, so the page's Default colour space replaces a
device family and a `/ColorSpace` entry is read in its place. Any other array is
read with no resources: no Default replaces a family inside it, and a name
inside it reads as no colour space. `threed_4e.pdf` gives no 3D stream a colour
space, and no file of any corpus does. The page's DefaultRGB is an ICCBased
space over an abstract profile, which fails ISO 19005-4, 6.2.4.2 wherever a
link reaches it.

| Annotation | 3D stream `/ColorSpace`                       | Answers     |
| ---------- | --------------------------------------------- | ----------- |
| `A1`       | `/DeviceRGB`, which DefaultRGB replaces       | `6.2.4.2/1` |
| `A2`       | `[/ICCBased]` over an abstract profile        | `6.2.4.2/1` |
| `A3`       | `[/Indexed /DeviceRGB ...]`, not replaced     | `6.2.4.3/2` |
| `A4`       | `/CS0`, a Separation over DeviceCMYK          | `6.2.4.3/3` |
| `A5`       | `[/Indexed /CS1 ...]`, `/CS1` read as nothing | `6.2.4.3/4` |
| `A6`       | `/DeviceGray`, through a 3D reference         | `6.2.4.3/4` |
| `A7`       | none; `/DeviceRGB` in its `/Resources`        | nothing     |
| `A8`       | none; `/DeviceRGB` in a `/3DV` view's stream  | nothing     |
| `A9`       | `/DeviceRGB`, on a stream with no `/Type /3D` | nothing     |

The three controls each carry DeviceRGB, so reading any of them raises the
DeviceRGB count above the one `A3` owes. A 3D annotation is a forbidden subtype
at PDF/A-4, which answers on each annotation's own object.

| Rule at PDF/A-4              | veraPDF | litho | litho before |
| ---------------------------- | ------- | ----- | ------------ |
| `6.2.4.2/1` ICC profile      | 2       | 2     | 4            |
| `6.2.4.3/2` DeviceRGB        | 1       | 1     | 0            |
| `6.2.4.3/3` DeviceCMYK       | 1       | 1     | 0            |
| `6.2.4.3/4` DeviceGray       | 2       | 2     | 0            |
| `6.3.1/1` annotation subtype | 9       | 9     | 9            |
| `6.7.2.1/1` no metadata      | 1       | 1     | 1            |

The four files return the same rule set as veraPDF 1.30.2 in all thirteen
cells each. Their counts agree at every level but the two PDF/UA ones, where
veraPDF counts `7.1/3` and `8.2.2/1` once per untagged content item, and `7.2/34`
once per text show, and litho once each, as for every untagged fixture,
`usedcs_2b.pdf` among them.

### `undecodable_2b.pdf`

veraPDF 1.30 decodes a stream through FlateDecode, ASCIIHexDecode,
ASCII85Decode, LZWDecode and RunLengthDecode alone, and reads no operator from
a content stream whose chain ends with any other name: the abbreviations ISO
32000-1, 8.9.7 gives inline images, the image codecs, BrotliDecode, an unknown
name. A name it does not decode earlier in the chain passes its input on
unchanged. Nor does it read a `/Contents` that is neither a stream nor an
array. litho read the operators of the streams it decodes itself, judged every
font of a page whose content it could not read, and read a Type 3 glyph's width
from a procedure veraPDF cannot read either. The corpus files that reach the
abbreviations, BrotliDecode, an image codec or a `/Contents` that is no stream
are pdfjs files, which carry no compliance golden, and no file of any corpus
writes a filter veraPDF does not decode before the last of a chain, or content
stream parameters that do not fit their chain.

Each page paints the same probe wherever a reader takes its operators, with a
font of its own: text in an unembedded Helvetica, a DeviceRGB fill with no
OutputIntent, and an undefined operator.

| Page | Where the probe sits                                     | veraPDF reads it |
| ---- | -------------------------------------------------------- | ---------------- |
| 1    | the page, no filter                                      | yes              |
| 2    | the page, `[/Foo /FlateDecode]` over Flate data          | yes              |
| 3    | the page, `[/FlateDecode /Foo]` over Flate data          | no               |
| 4    | the page, `/Fl` over Flate data                          | no               |
| 5    | the page, `/DCTDecode` over the probe itself             | no               |
| 6    | the page, `/BrotliDecode` over Brotli data               | no               |
| 7    | none: `/Contents` is a dictionary                        | no               |
| 8    | a form XObject, `/Fl` over Flate data                    | no               |
| 9    | a tiling pattern cell, `/AHx` over hex digits            | no               |
| 10   | an annotation appearance, `/A85` over base-85            | no               |
| 12   | the page, `/FlateDecode` with two parameter dictionaries | no               |
| 13   | the page, `/FlateDecode` with the parameters `[5]`       | no               |

Page 2 gives `/Foo` a PNG predictor and `/FlateDecode` null, so the chain is
read without `/Foo` only if the parameters leave with it. veraPDF fails to
decode a stream whose parameters count other than its filters, or hold an entry
that is neither a dictionary nor null. Page 11 shows the glyph of a Type 3 font
whose procedure is `/AHx` over `500 0 d0`, under a `/Widths` of 500. veraPDF
cannot read the procedure, takes the missing width of 0 and fails the glyph
width, where litho read 500.

| Rule at PDF/A-2b                  | veraPDF | litho | litho before |
| --------------------------------- | ------- | ----- | ------------ |
| `6.1.7.2/1` non-standard filter   | 8       | 8     | 8            |
| `6.2.2/1` undefined operator      | 2       | 2     | 8            |
| `6.2.4.3/2` DeviceRGB             | 4       | 4     | 16           |
| `6.2.4.3/4` DeviceGray            | 3       | 3     | 10           |
| `6.2.11.4.1/1` font not embedded  | 2       | 2     | 10           |
| `6.2.11.5/1` glyph width mismatch | 1       | 1     | 0            |
| `6.6.2.1/1` no XMP                | 1       | 1     | 1            |

The file returns the same rule set as veraPDF 1.30.2 in all thirteen cells,
and the same counts in the eleven PDF/A ones. At PDF/UA-1 veraPDF counts `7.1/3`
and `7.2/34`, and at PDF/UA-2 `8.2.2/1`, once per untagged content item, and
litho once each.

### `fontclaims_2b.pdf`

`fontclaims_2b.pdf` covers glyphs no other corpus file reached: CIDs a
name-keyed CFF program lacks, the streams whose glyphs claim a font's program
before the page text does, and a control code an unembedded standard font names
`.notdef`. Every construct rides on a font of its own, so each count has one
source:

| Font | Construct                                                                 | Answers at PDF/A-2b                        |
| ---- | ------------------------------------------------------------------------- | ------------------------------------------ |
| `NK` | CID 3 past a three-glyph name-keyed CFF, a subset, `/W` 500 against 1000  | `6.2.11.4.1/2`, `6.2.11.5/1`, `6.2.11.8/1` |
| `NF` | the same CID in the same program, no subset tag, `/W` 600                 | `6.2.11.4.1/2`, `6.2.11.8/1`               |
| `PT` | CID 5 in a tiling cell the page fills with, then CID 6 in the page text   | `6.2.11.8/1` x2                            |
| `TC` | CID 5 in a Type 3 procedure the page shows, then CIDs 6 and 7 in its text | `6.2.11.4.1/2`, `6.2.11.8/1` x3            |
| `TF` | CID 5 in the procedure of a Type 3 glyph a form shows                     | `6.2.11.4.1/2`, `6.2.11.8/1`               |
| `HV` | code 1 through an unembedded Helvetica with no `/Encoding`                | `6.2.11.4.1/1`, `6.2.11.8/1`               |

A name-keyed CFF program, one with no `ROS` operator, takes a CID as its glyph
index (ISO 32000-1, 9.7.4.2). veraPDF weighs the glyph a subset lacks at the
CIDFont's `/DW`, and the glyph a full font lacks at the program's
`defaultWidthX`, 600 here, so `NF`'s `/W` of 600 passes the width rule.

`PT` and `TC` are the reading order. veraPDF parses a tiling cell where an
operator paints with the pattern and judges none of the cell's glyphs by the
program reads that parse claims, so neither of `PT`'s glyphs answers the
presence rule. It parses a Type 3 glyph procedure where the page shows the
glyph, and judges the procedure's glyphs, so of `TC`'s three CIDs only the
procedure's answers it. `TF` is reached through a form, where litho judged no
Type 3 procedure before. `HV` reads StandardEncoding from the metrics of the
standard 14 font, which names nothing at code 1; its `/ToUnicode` keeps the
Unicode rules off the glyph.

| Rule at PDF/A-2b                  | veraPDF | litho | litho before |
| --------------------------------- | ------- | ----- | ------------ |
| `6.2.11.4.1/1` font not embedded  | 1       | 1     | 1            |
| `6.2.11.4.1/2` glyph missing      | 4       | 4     | 3            |
| `6.2.11.5/1` glyph width          | 1       | 1     | 0            |
| `6.2.11.8/1` `.notdef` referenced | 9       | 9     | 5            |
| `6.2.4.3/4` DeviceGray            | 10      | 10    | 10           |
| `6.6.2.1/1` no metadata           | 1       | 1     | 1            |

The rule sets equal veraPDF 1.30.2's in all thirteen cells, and so do the
counts, but at PDF/UA, where veraPDF counts `7.1/3`, `7.2/34` and `8.2.2/1`
once per untagged content item and litho once, the family described under
[Instance counts](#instance-counts-which-the-corpus-does-not-pin). At PDF/UA the
form claims its fonts without its glyphs being judged, so `TF` does not answer
the presence rule there, which counts 3.

### `imagereach_2b.pdf`

`imagereach_2b.pdf` holds three images, each with an `/Intent` outside the four
ISO 32000-1 names and an `/Interpolate` of true, and no operator draws any of
them. veraPDF reaches an image through the `Do` that draws it, once for the
whole document, and a content stream two pages share through the resources of
the first page:

| Image | Where it sits                                                        |
| ----- | -------------------------------------------------------------------- |
| `I1`  | in the resources of a form the first page draws, whose content fills |
| `I2`  | in the resources of a form nested in a form the first page draws     |
| `I3`  | drawn by the form the second page names for the shared stream's `Do` |

So neither image rule fails. litho judged every entry of a drawn form's
resources and every page's resources for a shared stream, which failed both
three times.

| Rule at PDF/A-2b              | veraPDF | litho | litho before |
| ----------------------------- | ------- | ----- | ------------ |
| `6.2.6/1` rendering intent    | 0       | 0     | 3            |
| `6.2.8/3` `/Interpolate` true | 0       | 0     | 3            |
| `6.2.4.3/4` DeviceGray        | 2       | 2     | 2            |
| `6.6.2.1/1` no metadata       | 1       | 1     | 1            |

The rule sets equal veraPDF 1.30.2's in all thirteen cells, and so do the
counts but for `7.1/3` and `8.2.2/1` at PDF/UA, the same family.

### `csnames_2b.pdf`

A colour space array holds its `/Indexed` base, its Separation or DeviceN
alternate and its `/Colorants` spaces as colour spaces (ISO 32000-1, 8.6.6.3 to
8.6.6.5), and veraPDF reads a name there that is the key of another entry of the
same `/ColorSpace` resource dictionary as that entry, resolved in the one
dictionary the space was read from and never in the page's. No corpus file names an entry inside a colour space array, in a `/Colorants`
dictionary, as a shading's colour space or as an image's or inline image's
`/Indexed` base, and none holds an `/Indexed` base as an indirect family name,
so no count reached the reading; litho read the name as a space it cannot read,
DeviceGray. Two neighbours are reached already and stay out: a page group
naming an entry, in `blendcs_4.pdf`, and an entry holding an indirect family
name, in 73 files, govdocs thread_300_300760.pdf among them.

Twenty pages each carry resources of their own, and each Separation a colorant
of its own:

| Pages   | Name                                                                   | Answers at PDF/A-2b           |
| ------- | ---------------------------------------------------------------------- | ----------------------------- |
| 1, 2, 3 | an `/Indexed` base, the same over `[/DeviceRGB]`, an entry naming one  | DeviceRGB x2 each             |
| 4, 5    | a Separation's and a DeviceN's alternate, `/CS0` being DeviceCMYK      | DeviceCMYK x3 each            |
| 6       | a `/Colorants` entry naming a Separation over DeviceRGB                | DeviceRGB x3, DeviceGray x3   |
| 7, 8    | a DefaultRGB's `/Indexed` base, an inline image's                      | DeviceCMYK x2, DeviceRGB x1   |
| 9, 10   | an `/Indexed` over an abstract ICCBased entry, direct, then indirect   | ICC x2, then x1               |
| 11, 12  | `/CSp` naming a Pattern space, coloured cell, then uncoloured over RGB | DeviceRGB x2 + `6.1.13/1`, x1 |
| 13      | an `/Indexed` base that is an indirect `/DeviceRGB`                    | DeviceRGB x2                  |
| 14      | a form's `/Indexed` over `/CS0`, which only the page defines           | DeviceGray x2                 |
| 15, 16  | the page's `/Indexed` space and image, in a form redefining `/CS0`     | DeviceRGB x2, x1              |
| 17      | a form's shading naming `/CS0`, which only the page defines            | nothing                       |
| 18, 19  | the page's shading and `/Indexed` image a form paints, page DefaultRGB | ICC x1 each                   |
| 20      | two Separations of one colorant over `/CS0` and `/DeviceCMYK`          | DeviceCMYK x6                 |

Page 11's cell gives `w` the integer 2147483648, which veraPDF reads only
because the name reaches the Pattern space and builds the pattern. Pages 15,
16, 18 and 19 also fail `6.2.2/2`, the form selecting a space, an image or a
shading only the page defines. Page 20 is the 6.2.4.4 t2 control: the two
alternates compare as the spaces they name, the same DeviceCMYK. A name that
reaches itself again and a fan-out of names stay out, because veraPDF runs out
of time or stack on both.

| Rule at PDF/A-2b         | veraPDF | litho |
| ------------------------ | ------- | ----- |
| `6.1.13/1` integer       | 1       | 1     |
| `6.2.2/2` inherited name | 4       | 4     |
| `6.2.4.2/1` ICC profile  | 5       | 5     |
| `6.2.4.3/2` DeviceRGB    | 18      | 18    |
| `6.2.4.3/3` DeviceCMYK   | 14      | 14    |
| `6.2.4.3/4` DeviceGray   | 5       | 5     |
| `6.6.2.1/1` no metadata  | 1       | 1     |

The two tools return the same rule set in all thirteen cells, and the same count
for every rule at every PDF/A level; at the PDF/UA levels veraPDF counts `7.1/3`
and `8.2.2/1` once per untagged content item.

### `offlimits_1b.pdf`

An uncoloured tiling pattern's cell ignores its colour operators (ISO 32000-1,
8.7.3.3), and so does a Type 3 glyph past its `d1` (9.6.5). veraPDF builds no
colour space and no pattern an operator in such a stream selects, and neither
does it in a form or a glyph such a stream draws, so the DeviceN colourant
limit, ISO 19005-1, 6.1.12 t9, and the integer limit of a number given to a
colour operator, t1, go unjudged there; an image such a stream paints is still
built. A census found 17 corpus files drawing a stream with its colour
operators off and none holding a number past a limit in one, so no count
reached the reading and no remediation had to leave such a number as written.

Fourteen pages each hold one construct. `DN` is a DeviceN of 33 colourants,
past the 8 of ISO 19005-1 and the 32 of ISO 19005-2:

| Pages      | Construct                                                               | 6.1.12 at PDF/A-1b |
| ---------- | ----------------------------------------------------------------------- | ------------------ |
| 1, 3       | an uncoloured cell paints an image in `DN`, or a form painting one      | t9 x1 each         |
| 2          | a coloured cell selects `DN` beside an uncoloured cell                  | nothing            |
| 4, 5       | an `/Indexed` over the `DN` entry, selected and filled, then an image   | t9 x2, x1          |
| 6, 7       | a glyph selects a pattern past its `d1`, then past the `Q` closing it   | nothing, t1 x1     |
| 8, 9       | a pattern an uncoloured cell, or a form it paints, selects              | nothing            |
| 10         | a bare `scn` in a form the page paints in the Pattern space             | t1 x1              |
| 11, 12, 13 | `99999999999 g` in a form, a glyph and a coloured cell drawn colour-off | nothing            |
| 14         | `99999999999 g` in a form the page paints                               | t1 x1              |

The patterns of pages 6 to 10 are coloured cells of their own giving `w` the
integer 2147483648, which counts only where veraPDF builds the pattern. Page 2
is the uncoloured pattern turning the colour operators off for every tiling
cell the same stream paints, the coloured one beside it included. Pages 11 to
14 are what the content-limits repair reads: at PDF/A-1b it clamps the number on
page 14, which compliance judges, and leaves the three it does not, in both
modes. It also clamps the `w` of pages 6, 8 and 9, in cells compliance never
builds, which changes content no rule reads: the repair walks every tiling
pattern the resources name, where compliance reaches a cell only through an
operator that builds it. Page 7's glyph opens with `q`, so its `d1` is no first operator and its
width reads as 0 against `/Widths` 1000, `6.3.6/1`.

| Rule at PDF/A-1b       | veraPDF | litho |
| ---------------------- | ------- | ----- |
| `6.1.12/1` integer     | 3       | 3     |
| `6.1.12/9` DeviceN     | 5       | 5     |
| `6.2.3.3/2` DeviceCMYK | 5       | 5     |
| `6.2.3.3/3` DeviceGray | 5       | 5     |
| `6.3.6/1` glyph width  | 1       | 1     |
| `6.7.2/1` no metadata  | 1       | 1     |

The two tools return the same rule set in all thirteen cells. Their counts agree
at every PDF/A level but PDF/A-1a, where veraPDF states `6.3.8/1` once per
rendered glyph and litho once per font, and at the PDF/UA levels, where
veraPDF counts `7.1/3`, `7.2/34` and `8.2.2/1` once per untagged item (see
[Instance counts](#instance-counts-which-the-corpus-does-not-pin)).

### `xrefstm_2b.pdf`

A cross-reference stream's dictionary holds the file's trailer beside the
entries that describe the stream (ISO 32000-1, 7.5.8.2), and veraPDF builds the
trailer from the entries Table 15 defines, `/Size`, `/Prev`, `/Root`,
`/Encrypt`, `/Info` and `/ID`. The dictionary is an indirect object the object
sweep judges whole, so a value in a trailer entry is judged twice, a document
information dictionary written directly there three times, and an entry Table
15 does not define once. An older section's stream is judged once, as the
indirect object it is. 922 corpus files end in a cross-reference stream and
none holds a defect in its trailer entries, so no count reached the reading.

qpdf writes the first section as a cross-reference stream whose trailer holds a
direct `/Info`, and the builder appends an incremental update rewriting the
page's content stream, indexed by a stream of its own:

| Where                                  | Value       | Rule at PDF/A-2b   | Count |
| -------------------------------------- | ----------- | ------------------ | ----- |
| newest `/ID`, first element            | 31 digits   | `6.1.6/1`          | 2     |
| newest direct `/Info`, `/Title`        | `<414>`     | `6.1.6/1`          | 3     |
| newest direct `/Info`, `/Producer`     | `<41G1>`    | `6.1.6/2`          | 3     |
| newest `/Foo`, no Table 15 entry       | `<414>`     | `6.1.6/1`          | 1     |
| older direct `/Info`, `/Producer`      | `<414>`     | `6.1.6/1`          | 1     |
| newest direct `/Info`, `/Count`        | 2147483648  | `6.1.13/1`         | 3     |
| newest `/Big`, no Table 15 entry       | 2147483648  | `6.1.13/1`         | 1     |
| older direct `/Info`, `/Count`         | 2147483648  | `6.1.13/1`         | 1     |
| newest direct `/Info`, `/Subject`      | 32768 bytes | `6.1.13/3`         | 3     |
| newest direct `/Info`, a key           | 200 bytes   | `6.1.13/4`         | 3     |
| newest, a key of the stream dictionary | 200 bytes   | `6.1.13/4`         | 1     |
| newest direct `/Info`, `/Width`        | 40000.5     | PDF/A-1 `6.1.12/2` | 3     |

The newest `/Size` stays in range. veraPDF judges an out-of-range `/Size` twice
like any trailer entry, but qpdf then refuses the file outright.

| Rule at PDF/A-2b         | veraPDF | litho |
| ------------------------ | ------- | ----- |
| `6.1.6/1` odd hex digits | 7       | 7     |
| `6.1.6/2` non-hex digit  | 3       | 3     |
| `6.1.13/1` integer       | 5       | 5     |
| `6.1.13/3` string        | 3       | 3     |
| `6.1.13/4` name          | 4       | 4     |
| `6.6.2.1/1` no metadata  | 1       | 1     |

The two tools return the same rule set, and the same count for every rule, in
all thirteen cells. At PDF/A-1b the real answers `6.1.12/2` three times on
both, and the cross-reference stream `6.1.4/3`.

### `typedreads_1b.pdf`

veraPDF judges a value once as a COS object and again wherever a typed object
of its model wraps it in a new one with no identity of its own: an outline
item's `/Title`, a structure element's `/S`, `/Lang`, `/Alt` and
`/ActualText`, the catalog's `/Lang`, the `/Lang` of the element an
annotation's or a form field's `/StructParent` names, each page box, a stream's
`/Filter` names, the file specifications of the `/EmbeddedFiles` name tree.
Every rule of the wrapper's type judges it again: the hex string tests of ISO
19005-1, 6.1.6, and the limits of 6.1.12. No corpus file holds a hex string
defect in one of these places, or a page box, a filter name, a structure type
or a role map key past a limit; two verapdf files hold an outline `/Title` past
the string limit, which is where the reading showed.

The builder's table lists every defective string, each distinct. The reach is
veraPDF's, measured one construct per file: the outline walk runs from
`/First` along `/First` and `/Next`, each item once, and never reads the outline
dictionary's own `/Title` or an item only `/Last` names; the structure walk
reads an indirect element once however often `/K` lists it, and never its
`/E`; an annotation reads its element's `/Lang`, a form field too, down every
`/Kids` entry holding a `/T`, so the widget that is also the field reads it
twice more; a media box is judged for every page it applies to, and an indirect
element of a box once. Every `/Lang` ends on the digit 7, which an odd hex
string reads as `70`, so it decodes to a well-formed tag (`nlp`, `enp`, `dp`)
and the language rules of PDF/A-1a, PDF/A-2a and PDF/UA stay silent: veraPDF
counts those per read. A structure element whose `/K` loops back to itself
stays out, because veraPDF's PDF/A-3 file specification walk follows it until
its stack overflows.

| Rule at PDF/A-1b             | veraPDF | litho |
| ---------------------------- | ------- | ----- |
| `6.1.6/1` odd hex digits     | 34      | 34    |
| `6.1.6/2` non-hex digit      | 2       | 2     |
| `6.1.11/1` EF key            | 2       | 1     |
| `6.1.11/2` EmbeddedFiles key | 1       | 1     |
| `6.1.12/2` real              | 14      | 14    |
| `6.1.12/4` name              | 6       | 6     |
| `6.7.2/1` no metadata        | 1       | 1     |
| `6.9/2` field appearance     | 2       | 2     |

The two tools return the same rule set in all thirteen cells. The one count
that differs is a file specification's: veraPDF judges it once as an indirect
object and once through the name tree, `6.1.11/1` here and `6.8`, `6.9` at the
later parts, where litho judges it once (see
[Instance counts](#instance-counts-which-the-corpus-does-not-pin)).

### `fontcopies_2b.pdf`

A font dictionary written directly in a `/Font` resource dictionary has no
object number, and veraPDF tells its copies apart by the dictionary that writes
each: every such dictionary is one font, judged once however many pages or
forms reach it. Twelve corpus files write a font directly, and judging each
copy moves no count on any of them. Three fonts are written directly, an
unembedded Helvetica (`U`), a Type 1 subset whose `/Widths` say 100 where its
CFF says 600 (`W`), and a Type0 over a shared three-glyph CIDFontType2 showing
CID 5 (`C`), in nine layouts over 13 copies: a copy on each of two pages, two
pages sharing an indirect resource dictionary, two sharing an indirect `/Font`
dictionary, two inheriting from their node, an indirect font and then a copy
under the same name, two names on one page, one form two pages paint, two
forms with a copy each, and two forms sharing resources.

A CID a TrueType CIDFont's program holds no glyph for is weighed at glyph 0's
advance, so it fails the width rule wherever the declared width differs, beside
the presence and `.notdef` rules. `fontleft_2b.pdf` shows such CIDs with `/DW`
at that advance, where the width rule stays silent, and pdfjs
Brotli-Prototype-FileA.pdf shows one veraPDF cannot open. Seven indirect Type0
fonts, `M1` to `M7`, move the declared width and the map: a `/W` at glyph 0's
advance (the control), a `/W` off it, no `/DW`, no `/CIDToGIDMap`, the first CID
past the program, a map stream sending CID 1 past it, and a map stream ending
before the CID. Last, two unembedded Type0 fonts share one descendant object
declaring Adobe-Korea1 under UniJIS-UCS2-H, and two pages write one Type0 font
with its descendant written in it: the embedding rule counts descendant
objects, the `/CIDSystemInfo` rule composite fonts.

| Rule at PDF/A-2b                | veraPDF | litho |
| ------------------------------- | ------- | ----- |
| `6.2.2/2` inherited name        | 2       | 2     |
| `6.2.4.3/4` DeviceGray          | 62      | 62    |
| `6.2.11.2/1` font `/Type`       | 7       | 7     |
| `6.2.11.3.1/1` `/CIDSystemInfo` | 4       | 4     |
| `6.2.11.3.2/1` `/CIDToGIDMap`   | 1       | 1     |
| `6.2.11.4.1/1` not embedded     | 16      | 16    |
| `6.2.11.4.1/2` glyph missing    | 20      | 20    |
| `6.2.11.5/1` glyph width        | 45      | 45    |
| `6.2.11.8/1` `.notdef`          | 20      | 20    |
| `6.6.2.1/1` no metadata         | 1       | 1     |

`U` answers 13 times and the Korean descendants 3, `C` answers presence,
`.notdef` and width 13 times each, `W` width 26 times, and `M1` to `M7` add 7,
7 and 6. The Korean descendants carry no `/Type`, as in the measurement that
fixed their counts, which `6.2.11.2/1` answers once per descendant and once
more per composite font read through it.

The two tools return the same rule set in all thirteen cells, and the same count
for every rule at every PDF/A level; at the PDF/UA levels veraPDF counts the
untagged-content rules once per text show.

Remediated at PDF/A-1b, in both modes, the output gives `C`'s descendant a
`/CIDSet` naming the CIDs the three-glyph program holds, while `C` renders CID 5,
which the program does not hold. ISO 19005-1, 6.3.5 has the set identify the
CIDs present in the program, and veraPDF asks it at PDF/A-1 only for a rendered
CID the program holds: one the `/CIDToGIDMap` covers whose glyph lies below
`maxp.numGlyphs`, with or without an outline. Neither tool reports `6.3.5/3` on
the output, and the two agree on its rule set.

### `usecmap_2u.pdf`

A `/ToUnicode` CMap builds on another two ways (ISO 32000-1, 9.7.5): the
usecmap operator in its program names a predefined CMap, and a `/UseCMap` entry
in its stream dictionary (Table 120) names a predefined CMap or an embedded CMap
stream, which may name another in turn. veraPDF merges only the single-code
mappings of a usecmap base, a base replacing the mappings defined before the
operator and yielding to those after it, and supplies every mapping of a
`/UseCMap` base to the codes the program leaves out. The Adobe UCS2 CMaps it
reads are Adobe's current resources, which map codes the 2009 copies PDF.js
ships leave out and drop others; PDF.js ships no Adobe-KR-UCS2 at all. pdfjs
issue5010.pdf is the one corpus file whose `/ToUnicode` builds on another CMap,
through usecmap and Adobe-Korea1-UCS2, where the two sources agree, and no file
names a base with `/UseCMap`, so no count reached any of this.

Five Type 3 fonts show codes 41, 60, 61 and 65 hexadecimal through glyph names
of no Unicode value, and six Type0 fonts, each over a CIDFontType2 whose
`/CIDToGIDMap` sends every CID it shows but 0 to a glyph, show CIDs where the
two Adobe sources disagree:

| Font | `/ToUnicode`                                             | `6.2.11.7.2` t1, t2 |
| ---- | -------------------------------------------------------- | ------------------- |
| `T1` | runs `/No-Such-CMap usecmap`                             | 4, 0                |
| `T2` | `/UseCMap /Adobe-Korea1-UCS2`                            | 0, 0                |
| `T3` | `/UseCMap` a stream mapping 41 and 65                    | 2, 0                |
| `T4` | `/UseCMap` a stream mapping 65, whose `/UseCMap` maps 41 | 2, 0                |
| `T5` | 60 to U+0000, `/Adobe-Korea1-UCS2 usecmap`, 61 to U+0000 | 2, 1                |
| `C1` | `/Adobe-GB1-UCS2 usecmap`                                | 1, 0                |
| `C2` | `/Adobe-CNS1-UCS2 usecmap`                               | 1, 0                |
| `C3` | `/Adobe-Japan1-UCS2 usecmap`                             | 4, 0                |
| `C4` | `/Adobe-KR-UCS2 usecmap`                                 | 1, 0                |
| `C5` | `/UseCMap /Adobe-CNS1-UCS2`                              | 1, 0                |
| `C6` | `/UseCMap /Adobe-Japan1-UCS2`                            | 1, 0                |

Adobe-Korea1-UCS2 maps 60 and 61 through bfchar and 41 and 65 through bfrange,
which usecmap does not merge, so `T5`'s base replaces its U+0000 for 60 and
yields to it for 61. Each Type0 font also shows CID 0, the `.notdef` glyph,
which `6.2.11.8/1` answers once per font. Two readings stay out: a `/UseCMap`
chain that loops, on which veraPDF raises "Loop inside CMap" and abandons the
validation, and Japan1 CIDs 5A0D and 5A0E, which Adobe's resource maps through
the last bfrange of a block it counts as 42 entries and veraPDF's bundled copy
as 41, so its reader drops them.

| Rule at PDF/A-2u          | veraPDF | litho |
| ------------------------- | ------- | ----- |
| `6.2.11.7.2/1` no Unicode | 19      | 19    |
| `6.2.11.7.2/2` U+0000     | 1       | 1     |
| `6.2.11.8/1` `.notdef`    | 6       | 6     |
| `6.2.4.3/4` DeviceGray    | 11      | 11    |
| `6.6.2.1/1` no metadata   | 1       | 1     |

`C2` shows CID 44DF, which Adobe's current CNS1 resource maps to U+F538, a
Private Use value only the base supplies, since the program maps no code
itself. ISO 19005-2, 6.2.11.7.3 and ISO 14289-2, 8.4.3 ask for an `/ActualText`
around such a glyph, so both tools report `6.2.11.7.3/1` at PDF/A-2a and
PDF/A-3a and `8.4.3/1` at PDF/UA-2, once each. The thirteen cells return the
same rule set, and at the PDF/A levels the same count for every rule.

### `afref_3b.pdf`

ISO 19005-3, 6.8 test 4 asks every file specification dictionary of a PDF/A-3
file to be an associated file, and only an `/AF` array associates one (ISO
32000-2, 14.13): on the catalog, a page, an annotation, an XObject or a
structure element. A reference from anywhere else names the file without
stating how it relates to the document, which is what `/AFRelationship` on an
associated file records. veraPDF's `isAssociatedFile` holds only the objects
`/AF` arrays list, whether the file is embedded or external and whether the
dictionary is indirect or written inline. litho used to accept an annotation's
reference as an association for an external file, and a dictionary written
inline outright; the verapdf corpus reaches the first route on two files, and
no corpus file the others.

Five annotations each refer to an external file specification carrying `/F`,
`/UF` and `/AFRelationship`, so no other 6.8 test applies:

| Annotation | Route to the file specification                     | `6.8/4` |
| ---------- | --------------------------------------------------- | ------- |
| `FA1`      | a FileAttachment `/FS` naming an indirect filespec  | fires   |
| `FA2`      | a FileAttachment `/FS` written inline               | fires   |
| `LK`       | a Link's `/GoToR` action whose `/F` is inline       | fires   |
| `RM`       | a RichMedia `/Assets` tree naming an indirect one   | fires   |
| `FA3`      | a FileAttachment `/FS` the catalog `/AF` also lists | silent  |

The catalog's `/Names /JavaScript` tree is an empty dictionary. ISO 32000-1,
7.7.4 makes the tree a map of document-level JavaScript actions, and litho
judges each action it holds; an empty tree holds none, so neither ISO 19005-1,
6.6.1 t1 nor ISO 19005-2/3, 6.5.1 t1 fires. litho used to fire them on the
presence of the key. Three pdfjs files write the tree empty, but each carries
other JavaScript actions, so no corpus file showed it.

| Rule at PDF/A-3b               | veraPDF | litho |
| ------------------------------ | ------- | ----- |
| `6.3.1/1` RichMedia annotation | 1       | 1     |
| `6.8/4` no associated file     | 4       | 4     |

The two tools return the same rule set and the same count for every rule in
all thirteen cells.

### `inlinefs_4.pdf`

ISO 32000-1, 7.11.3 does not require a file specification to be an indirect
object. veraPDF judges a `/Type /Filespec` dictionary wherever it meets one, as
it walks every value of every object, and names an inline one by the object
holding it. litho judged an inline one only inside an annotation that is an
object of its own, and its repairs reached an annotation's `/FS` alone. Four
routes each carry a different set of defects, so a check or a repair that
misses one reports a count other than the one due:

| Route     | Where the specification sits                    | Embeds              | Keys                      |
| --------- | ----------------------------------------------- | ------------------- | ------------------------- |
| `LK1`     | the `/AF` of a Link that is an object           | text, no `/Subtype` | `/F` only                 |
| `LK2`     | the `/AF` of a Link written inline in `/Annots` | text, `/plain`      | `/F`, `/UF`, relationship |
| `Fm`      | the `/AF` of a form XObject the page lists      | nothing             | `/F`, `/UF`               |
| `catalog` | the catalog's `/AF`                             | nothing             | `/F`, `/UF`, relationship |

Neither embedded file is a PDF. An `/AF` array associates only the indirect
objects it lists, and veraPDF counts an inline specification as associated
with nothing, even one an `/AF` array lists inline, so `6.8/4` fires on all
four at PDF/A-3b.

| Rule                                | veraPDF | litho | litho before |
| ----------------------------------- | ------- | ----- | ------------ |
| PDF/A-1b `6.1.11/1` `/EF` present   | 2       | 2     | 1            |
| PDF/A-2b `6.8/5` not PDF/A          | 2       | 2     | 1            |
| PDF/A-3b `6.8/1` media type         | 2       | 2     | 1            |
| PDF/A-3b `6.8/2` `/F` without `/UF` | 1       | 1     | 1            |
| PDF/A-3b `6.8/3` no relationship    | 2       | 2     | 1            |
| PDF/A-3b `6.8/4` not associated     | 4       | 4     | 1            |
| PDF/A-4 `6.9/1` media type          | 2       | 2     | 1            |
| PDF/A-4 `6.9/2` `/F` without `/UF`  | 1       | 1     | 1            |
| PDF/A-4 `6.9/3` not PDF/A           | 2       | 2     | 1            |
| PDF/A-4 `6.9/4` no relationship     | 1       | 1     | 1            |

The two tools return the same rule set and the same count for every rule in
all thirteen cells. The Links carry `/Contents` and the page draws nothing, so
the PDF/UA levels judge the annotations and not the content.

Remediation at the eight manifest profiles leaves the same rule set on both
tools in every cell. The aggressive mode leaves nothing but `7.18.5/1` at
PDF/UA-1, the Links the auto-tagger nests in an Artifact. The normal mode
also keeps the embedded files, which the part 1 prohibition (`6.1.11/1`) and
the PDF/A conformance of an attachment (`6.8/5` at part 2, `6.9/3` at part 4)
refuse, and repairs everything else. Before, both modes left `6.9/1` to
`6.9/4` at PDF/A-4, `6.8/2` and `6.8/5` at part 2 and `6.1.11/1` at part 1,
and at PDF/A-3b left veraPDF reporting `6.8/1`, `6.8/3` and `6.8/4` on the
routes litho neither judged nor repaired, where litho reported nothing.

### `repairroom_2b.pdf`

Two repairs reached less than the rule they answer or removed more than the
finding. The `.notdef` repair strips a code the font gives no glyph from the
strings a page shows (ISO 19005-2, 6.2.11.8), and the `/Colorants` repair
completes a DeviceN whose spot colorants have no entry (6.2.4.4 t1). The page
carries three constructs, each with something after it that shows what the
repair did:

| Construct                                        | What the repair must keep or reach   |
| ------------------------------------------------ | ------------------------------------ |
| `(AB) Tj [(\x01) -3000] TJ (BA) Tj`              | the 3000-unit move of the emptied TJ |
| `(A\x01B) Tj (AB) Tj`                            | the 600-unit width of the `.notdef`  |
| a DeviceN in a shading pattern and in `/Shading` | both, reusing the page's SpotX       |

A number in a TJ array moves the text position (ISO 32000-1, 9.4.3), and a
glyph moves it by its width (9.4.4). The repair dropped a TJ it emptied whole
and removed a glyph with its width, so the text after either started further
left. It now keeps each as a TJ number. The font is `repairleft_4.pdf`'s,
whose `/Widths` give code 1 the 600 units of the `.notdef` glyph it shows.
poppler's `pdftotext -bbox` places the words after an aggressive PDF/A-2b
remediation at:

| Word after the stripped code | Source x | Before the fix | Now   |
| ---------------------------- | -------- | -------------- | ----- |
| `BA`, line 1                 | 129.6    | 86.4           | 129.6 |
| `BAB`, line 2                | 86.4     | 79.2           | 86.4  |

The `/Colorants` repair walked the page's `/ColorSpace` resources alone, and
compliance judges the DeviceN a shading paints with as any other, so both
spaces kept 6.2.4.4 t1 through remediation on litho and veraPDF alike.
`CSX`, a Separation of SpotX the page paints with, is the definition both
completed `/Colorants` reuse (6.2.4.4 t2).

| Rule at PDF/A-2b                 | veraPDF | litho |
| -------------------------------- | ------- | ----- |
| `6.2.4.4/1` DeviceN colorants    | 2       | 2     |
| `6.2.11.4.1/2` glyph not in font | 1       | 1     |
| `6.2.11.8/1` `.notdef`           | 1       | 1     |

The aggressive remediation at PDF/A-2a, 2b, 2u, 3b and 4 now leaves nothing,
where it left both 6.2.4.4 t1 findings, and the default one leaves the glyph
findings only the aggressive mode repairs; veraPDF agrees on every output. The
runner scores rule sets, so it guards the `/Colorants` reach, and the text
positions above are measured by hand. Of the five corpus files whose shadings
use a DeviceN short of colorants, pdfjs issue17065.pdf is the one a
remediation left short, and the remediation corpus runs none of them. No
corpus page the `.notdef` repair runs on shows text after an emptied TJ whose
numbers move.

### `paintop_2b.pdf`

ISO 19005-2, 6.2.10 t2 and ISO 19005-4, 6.2.9 t2 ask every page that contains
transparency, in a document without a PDF/A OutputIntent, for a blending
colour space. veraPDF decides that a page contains transparency at the
operators that paint, under the graphics state in force there
(`TransparencyBehaviour`, veraPDF-validation 1.30): a fill tests `/ca`, a
stroke `/CA`, both a soft mask and a blend mode, an alpha counts only below 1,
and a gs replaces the soft mask and the blend mode, absent ones included. The
file declares no OutputIntent and no page group, and paints in an ICCBased
space throughout. Each of pages 1 to 7 sets a transparent graphics state that
no operator paints under:

| Page | Construct                                                    |
| ---- | ------------------------------------------------------------ |
| 1    | `/ca 0.5` set inside `q ... Q`, the fill after `Q`           |
| 2    | `/ca 0.5` before a stroke alone                              |
| 3    | `/CA 0.5` before a fill alone                                |
| 4    | `/ca 1.5` before a fill                                      |
| 5    | a soft mask, cleared by a graphics state that names none     |
| 6    | a tiling pattern filling at `/ca 0.5`, selected, never used  |
| 7    | an annotation appearance setting `/ca 0.5` and painting none |

No page contains transparency, so neither rule fires. litho counted every
graphics state a gs set, every tiling pattern the resources listed and every
state an appearance set, and failed all eight pages, the halftone page among
them through the pattern its resources list.

Page 8 sets four graphics states whose halftones ISO 19005-2, 6.2.5 judges.
veraPDF builds a halftone from an `/HT` that is a name, a dictionary or a
stream, and one more for every component of a type 5 halftone, judged under
its colorant name. It lists the components in the iteration order of the Java
HashMap its dictionary is, walks them from the last, and judges a component
written as an indirect object where it first reaches it:

| State | `/HT`                                              | Answers at PDF/A-2b  |
| ----- | -------------------------------------------------- | -------------------- |
| `H1`  | type 5, `/Red` and `/Cyan` naming one component    | nothing              |
| `H2`  | type 5, `/Blue` and `/Yellow` naming one component | nothing              |
| `H3`  | the name `/Foo`                                    | `6.2.5/4`, `6.2.5/5` |
| `H4`  | a type 6 halftone, a stream                        | `6.2.5/4`            |

`/Cyan` and `/Yellow`, process colorants that need no transfer function,
reach the shared components before `/Red` and `/Blue`, which would, so `H1`
and `H2` pass. `H3` has no type and names itself as its `/HalftoneName`. litho
failed `6.2.5/6` for `/Red` and `/Blue` and read neither `H3` nor `H4`.

| Rule       | Profile  | veraPDF | litho | litho before |
| ---------- | -------- | ------- | ----- | ------------ |
| `6.2.10/2` | PDF/A-2b | 0       | 0     | 8            |
| `6.2.9/2`  | PDF/A-4  | 0       | 0     | 8            |
| `6.2.5/4`  | PDF/A-2b | 2       | 2     | 0            |
| `6.2.5/5`  | PDF/A-2b | 1       | 1     | 0            |
| `6.2.5/6`  | PDF/A-2b | 0       | 0     | 2            |
| `6.2.5/4`  | PDF/A-4  | 2       | 2     | 0            |
| `6.2.5/5`  | PDF/A-4  | 1       | 1     | 0            |
| `6.2.5/6`  | PDF/A-4  | 0       | 0     | 2            |
| `6.4/1`    | PDF/A-1b | 1       | 1     | 1            |
| `6.4/5`    | PDF/A-1b | 1       | 1     | 1            |
| `6.4/6`    | PDF/A-1b | 2       | 2     | 2            |

At PDF/A-1b, ISO 19005-1, 6.4 judges every graphics state a gs builds, painted
under or not, and the halftones are silent. The two tools return the same rule
set in all thirteen cells, and the same count for every rule at every PDF/A
level. The repair replaces `H3` and `H4` with `/Default`, and every remediated
output passes on both tools at PDF/A-1b, PDF/A-2b and PDF/A-4.

### `imagedraws_2b.pdf`

`imagedraws_2b.pdf` draws images where only the streams veraPDF parses beside a
page's content reach them. veraPDF judges an image wherever a content stream it
parses draws it: an annotation's appearance, every glyph procedure of a Type 3
font text is shown with, and a form naming the image through the page's
resources. It builds a form model object for each appearance, so the form rules
judge an appearance's own dictionary too. Every image carries `/Interpolate`
true:

| Object    | Where it stands                                                           | Answers at PDF/A-2b |
| --------- | ------------------------------------------------------------------------- | ------------------- |
| `IAP`     | drawn by the normal appearance of a Square annotation                     | `6.2.8/3`           |
| `IT3`     | drawn by glyph `b` of `T3`, the page showing only glyph `a`, in mode 3    | `6.2.8/3`           |
| `IPF`     | drawn by `FmBare`, a form with no `/Resources`, through the page's name   | `6.2.8/3`           |
| `BI`      | two inline images with `/I true` in the page's content                    | `6.2.8/3` x2        |
| `ApOPI`   | an appearance with no `/Subtype`, carrying `/OPI`                         | `6.2.9/1`           |
| `ApRef`   | an appearance carrying `/Ref`                                             | `6.2.9/2`           |
| `ImRef`   | a drawn image carrying `/Ref`                                             | silent              |
| `Stencil` | a drawn image mask with `/BitsPerComponent 2`                             | silent              |
| `Base`    | an image whose `/Mask` declares `/BitsPerComponent 8` and no `/ImageMask` | `6.2.8/5`           |

The last three are the roles veraPDF reads an image in: it states the
reference XObject rule on forms alone, `t4` on every drawn image, an image mask
included, and `t5` on an image's `/Mask` alone. litho judged the images of a
page's content, its forms and its patterns, one inline image per stream, the
`/Ref` of any XObject, and `t5` on any image mask, so its rule set matched
veraPDF's but for `6.2.9/1`, two of its classes by coincidence: `ImRef` stood
in for `ApRef`, and `Stencil` for `Base`.

| Rule at PDF/A-2b               | veraPDF | litho | litho before |
| ------------------------------ | ------- | ----- | ------------ |
| `6.2.8/3` `/Interpolate` true  | 5       | 5     | 1            |
| `6.2.8/5` mask bits            | 1       | 1     | 1            |
| `6.2.9/1` form `/OPI` or `/PS` | 1       | 1     | 0            |
| `6.2.9/2` reference XObject    | 1       | 1     | 1            |
| `6.2.2/2` inherited resource   | 1       | 1     | 1            |
| `6.2.4.3/4` DeviceGray         | 13      | 13    | 13           |
| `6.6.2.1/1` no metadata        | 1       | 1     | 1            |

The rule sets equal veraPDF 1.30.2's in all thirteen cells, and so do the
counts but for `7.1/3` at PDF/UA-1 and `8.2.2/1` at PDF/UA-2, the family
described under
[Instance counts](#instance-counts-which-the-corpus-does-not-pin).

### `unembednames_2u.pdf`

A simple font that embeds no program has no glyph names but its encoding's,
and veraPDF takes each shown glyph's Unicode value from that name alone: no
name fails `6.2.11.7.2/1`, the name `.notdef` has the value U+0000 in its glyph
list and fails `6.2.11.7.2/2`, and a name the Adobe Glyph List, the Symbol font
and ZapfDingbats all leave out fails `/1`. It holds a table for
MacRomanEncoding, MacExpertEncoding and WinAnsiEncoding alone, and a standard
14 Latin font with no table reads StandardEncoding from its metrics. The
`.notdef` rule reads the same names, but for a symbolic TrueType, whose
encoding it ignores.

| Font | Construct                                                           | Answers at PDF/A-2u                  |
| ---- | ------------------------------------------------------------------- | ------------------------------------ |
| `HV` | Helvetica, no `/Encoding`, shows `01 80 41`                         | `6.2.11.7.2/2`, `6.2.11.8/1` x2 each |
| `AS` | an Arial Type1 under `/StandardEncoding` shows `41`                 | `6.2.11.7.2/1`                       |
| `TS` | a symbolic Arial TrueType under `/WinAnsiEncoding` shows `00 01 41` | `6.2.11.7.2/2` x2, `6.2.11.8/1`      |
| `ZD` | ZapfDingbats, `/Differences` naming `41` `a71`, shows `41`          | silent; `6.3.8/1` at PDF/A-1a        |
| `T3` | a Type 3 font shown in modes 3 and 0, then 1 on page 2              | `6.2.11.7.2/2`, `6.2.11.8/1` x2 each |

`T3`'s glyph `a` shows code `01`, and its glyph `b`, which nothing shows, code
`02`, both through the unembedded Helvetica `H2`. veraPDF parses every
procedure of a Type 3 font once, in the rendering mode of the operator that
first shows the font, mode 3 included: each code fails the Unicode value and
`.notdef` rules once, and `H2`, built in mode 3, passes the embedding rule.
litho judged the procedure of each shown glyph, per page, in the first visible
mode that showed it.

| Rule at PDF/A-2u               | veraPDF | litho | litho before |
| ------------------------------ | ------- | ----- | ------------ |
| `6.2.11.4.1/1` not embedded    | 4       | 4     | 5            |
| `6.2.11.6/3` symbolic encoding | 1       | 1     | 1            |
| `6.2.11.7.2/1` no Unicode      | 1       | 1     | 0            |
| `6.2.11.7.2/2` U+0000          | 6       | 6     | 1            |
| `6.2.11.8/1` `.notdef`         | 5       | 5     | 5            |
| `6.2.4.3/4` DeviceGray         | 6       | 6     | 6            |
| `6.6.2.1/1` no metadata        | 1       | 1     | 1            |

The rule sets equal veraPDF 1.30.2's in all thirteen cells, and so do the
counts but for `7.1/3` and `7.2/34` at PDF/UA-1 and `8.2.2/1` at PDF/UA-2, the
same family.

### `exttypes_2b.pdf`

veraPDF builds one validators container per extension schema entry: the
part's predefined types plus the types that entry defines, where a type with
fields and no `pdfaType:namespaceURI` registers nothing
(`ValidatorsContainerCreator.registerTypeNode`). An entry counts only with a
namespace URI and a property array, the last one per URI replacing the
others, and it declares a property only with a type it knows. Each entry's
`pdfaProperty:valueType` and `pdfaField:valueType` are judged against the
container of the entry that counts for its URI, and against the basic types
alone when none counts (`AXLExtensionSchemasContainer`). litho pooled the
types of every entry and resolved the predefined ones for any entry. Only
verapdf `6-6-2-3-3-t03-fail-c.pdf` reached this among the corpora, through a
structured type with no namespace URI.

| Construct                                                                    | Rule at PDF/A-2b           |
| ---------------------------------------------------------------------------- | -------------------------- |
| `ex:old` of type `Pair`, defined by an entry a later one of its URI replaces | `6.6.2.3.3/8`              |
| an entry with no property array, fields of type Dimensions, Locale and Box   | `6.6.2.3.3/17` three times |
| `et:a` written at top level, where `et` names value types only               | `6.6.2.3.1/1` and `/2`     |

| Rule at PDF/A-2b | veraPDF | litho |
| ---------------- | ------- | ----- |
| `6.1.7.1/1`      | 1       | 1     |
| `6.1.7.1/2`      | 2       | 2     |
| `6.6.2.3.1/1`    | 1       | 1     |
| `6.6.2.3.1/2`    | 1       | 1     |
| `6.6.2.3.3/8`    | 1       | 1     |
| `6.6.2.3.3/17`   | 3       | 3     |

At PDF/A-1b the same constructs fail `6.7.8/9`, `6.7.8/18` three times,
`6.7.9/2` and `6.7.9/3`. All thirteen cells return the same rule set on both.

Remediation drops the value types of the entry with no property array, which
no property can use, so its output keeps `6.6.2.3.3/8` alone at PDF/A-2b and
PDF/A-3b and `6.7.8/9` alone at PDF/A-1b, on both tools.

### `extpredef_2b.pdf`

veraPDF resolves a property through the extension schema entries of its packet
first, then those of the main packet, and only then through the predefined
schemas (`AXLXMPProperty.getSchemasDefinition`). A predefined property an entry
declares is judged against the declared type, and one no entry declares
against its predefined type, even when an entry extends its namespace. A
structure type the part predefines is judged field by field
(`StructuredTypeValidator`, with the fields of `XMPConstants`), and an XPath
value is compiled by the JDK's XPath compiler (`XPathTypeValidator`). litho
judged a predefined property of an extended namespace against nothing, a
predefined structure for being a structure only, and an XPath for being a
simple value only. No corpus file reaches any of the seven:

| Property           | Declaration                        | Value                            |
| ------------------ | ---------------------------------- | -------------------------------- |
| `dc:title`         | `Text`                             | a Lang Alt                       |
| `xmp:CreateDate`   | `Integer`                          | a date                           |
| `xmpRights:Marked` | none, xmpRights extended otherwise | `yes`, not a Boolean             |
| `ex:dim`           | `Dimensions`                       | `stDim:w` holding `abc`          |
| `ex:ref`           | `ResourceRef`                      | `stRef:filePath`, a later member |
| `ex:evt`           | `seq ResourceEvent`                | `stEvt:when` holding `notadate`  |
| `ex:path`          | `XPath`                            | `///`                            |

| Rule at PDF/A-2b | veraPDF | litho |
| ---------------- | ------- | ----- |
| `6.1.7.1/1`      | 1       | 1     |
| `6.1.7.1/2`      | 2       | 2     |
| `6.6.2.3.1/2`    | 7       | 7     |

Every property fails the same test, so the rule set cannot tell one of them
regressing from the rest: the count can. At PDF/A-1b each fails `6.7.9/3`
instead. All thirteen cells return the same rule set on both.

### `extguards_2b.pdf`

The must-not-fire counterpart of `extpredef_2b.pdf`: every property passes ISO
19005-2, 6.6.2.3.1 on veraPDF, and each is a shape litho reported at some
point, so a firing of that clause at PDF/A-2b is a false positive. The one
rule of 6.6.2.3 the fixture fires is `6.6.2.3.3/8`, for the unknown type `Nope`
declared for `xmp:CreateDate`.

| Property            | Declaration                      | Value                         | Why it passes                                |
| ------------------- | -------------------------------- | ----------------------------- | -------------------------------------------- |
| `ex:gps`            | `GPSCoordinate`                  | `10,20.5N`                    | a type of parts 2 and 3, its pattern matched |
| `ex:mark`           | `Marker`                         | `xmpDM:startTime` a structure | the field is of type any                     |
| `ex:dim`            | `Dimensions`                     | `stDim:w`, `h` and `unit`     | every field of the type, of its type         |
| `ex:thumb`          | `Thumbnail`                      | `xmpGImg:format` of `PNG`     | a closed choice is read as Text              |
| `ex:col`            | `Colorant`                       | `xmpG:mode` of `XYZ`          | likewise                                     |
| `ex:xp1` to `xp3`   | `XPath`                          | `a:b`, `$x`, `x:f(1)/a`       | the JDK's XPath compiler takes all three     |
| `xmp:CreateDate`    | `Nope`, which nothing defines    | a date                        | undeclared, so judged as the predefined Date |
| `xmpMM:DerivedFrom` | `RefX`, holding `stRef:filePath` | a RefX structure              | judged against its declaration first         |
| `dc:title`          | none, dc extended with `dc:foo`  | a Lang Alt                    | judged against its predefined type           |

| Rule at PDF/A-2b | veraPDF | litho |
| ---------------- | ------- | ----- |
| `6.1.7.1/1`      | 1       | 1     |
| `6.1.7.1/2`      | 2       | 2     |
| `6.6.2.3.3/8`    | 1       | 1     |

At PDF/A-1b the part-1 container knows no Marker nor Colorant and a stricter
GPSCoordinate, so the fixture fires `6.7.8/9` three times, `6.7.9/2` twice and
`6.7.9/3` three times there, on both. All thirteen cells return the same rule
set on both.

### `formcycle_ua1.pdf`

Remediation at PDF/UA-1, PDF/UA-2 and the level-A PDF/A profiles tags an
untagged file, and the tagging marks every region of page content that is not a
structure leaf as an `/Artifact`, except a region drawing a Form XObject that
paints text (ISO 14289-1, 7.1). It judges a form by the XObjects its resources
list, and resources can list each other back: ISO 32000-1, 8.10.1 forbids a form
that invokes itself, not one that lists a form listing it. The scan walked the
forms depth first and cached a "no text" for a form whose search had stopped at
a form still on its path, so which forms it judged decorative followed Go's map
order. One page in the seven corpora lists forms that list each other back, page
1 of pdfjs issue19800.pdf, and it names the same text-painting forms either way,
so no count reached this.

One untagged page draws three forms between three lines of its own text, in the
conforming embedded TrueType of `ttcmap_2b.pdf`:

| Page name | Form                                              | Region after tagging |
| --------- | ------------------------------------------------- | -------------------- |
| `/X1`     | A: lists B and C and draws C; listed, never drawn | none                 |
| `/X2`     | B: lists and draws A, so it draws C's text        | left unmarked        |
| `/X3`     | D: draws E, which shows text                      | left unmarked        |
| `/X4`     | F: a rectangle                                    | `/Artifact`          |

When the page's map order put `/X1` before `/X2`, and A's put B before C, the
scan met B from A, cached "no text" for it and swept `/X2` into an `/Artifact`.
The name C shows then stopped extracting, and the integrity probe refused the
remediation. Measured on veraPDF 1.30.2 and litho, eight runs of each of the
eight profiles `manifest.json` lists, default and aggressive:

| Profile  | Runs refused before the fix       | Rules left after it, litho / veraPDF |
| -------- | --------------------------------- | ------------------------------------ |
| PDF/A-2a | 6 of 8, both modes                | none / none                          |
| PDF/UA-1 | 6 of 8, both modes                | `7.1/3`: 1 / 2                       |
| PDF/UA-2 | 6 of 8 default, 8 of 8 aggressive | `8.2.2/1`: 1 / 2                     |

After the fix every run of every profile writes the same bytes. PDF/A-1b, 2b,
2u, 3b and 4 do not tag, write the same bytes before and after, and leave no
rule on either tool. The rule left at PDF/UA is honest: `/X2` and `/X3` draw
real text the classifier does not tag, and leaving their regions unmarked keeps
it readable rather than hiding it inside an `/Artifact`. litho reports the page
once, and veraPDF each of the two regions.

Before remediation the file agrees with veraPDF in all thirteen cells:

| Rule at PDF/UA-1               | veraPDF | litho |
| ------------------------------ | ------- | ----- |
| `6.2/1` no `/MarkInfo`         | 1       | 1     |
| `7.1/3` unmarked content       | 6       | 1     |
| `7.1/8` no metadata stream     | 1       | 1     |
| `7.1/10` no `/DisplayDocTitle` | 1       | 1     |
| `7.1/11` no structure tree     | 1       | 1     |
| `7.2/34` no natural language   | 5       | 1     |

### `formflat_ua1.pdf`

Flattening a tagged form turns each widget's appearance into page content,
which ISO 14289-1, 7.1 requires to be tagged or an artifact. litho hands the
content to the structure element that enclosed the widget, and a Form element
that then holds content rather than its one widget reference takes a PrintField
`/Role` (ISO 32000-1, Table 348), which is what ISO 14289-1, 7.18.4 test 2 asks
of it. A widget with nothing to draw leaves with the form, and an element left
holding nothing leaves with it. `annotstruct_ua2`, `formstruct_ua2` and
`idfields_ua2` reach the tagging; none reaches a Form element emptied by the
flatten, an element that names no `/Pg`, or a radio or push-button role.

One tagged page presents three widgets, each the only kid of its own Form
element:

| Widget                                       | Form element   | After `FlattenForm`                                    |
| -------------------------------------------- | -------------- | ------------------------------------------------------ |
| `empty`: a text field, no value, no `/AP`    | `/Pg` the page | widget dropped, element pruned from the tree           |
| a kid of `choice`, a radio button that is on | no `/Pg`       | content tagged through an `/MCR` naming the page; `rb` |
| `go`: a push button                          | `/Pg` the page | content tagged as MCID 2; `pb`                         |

The catalog has no `/AcroForm`, a shape real files carry (pdfjs issue17794.pdf),
so `mutate-pages`, which copies the page onto its own document, gives it a
form. That form has to list the fields the page already presents as well as
the copies, or qpdf reports the page's own widgets as not reachable from it.

Measured on veraPDF 1.30.2 and litho before the fix, flattening added `7.1/3`
at PDF/UA-1 and `8.2.2/1` at PDF/UA-2 on both tools and left the `empty` field
behind. After it, the flattened file fails a subset of the input's rules at
every level, on both tools, and qpdf reports nothing on either output.

The file agrees with veraPDF in all thirteen cells, rule for rule and count for
count. At the home profile:

| Rule at PDF/UA-1           | veraPDF | litho |
| -------------------------- | ------- | ----- |
| `7.1/8` no metadata stream | 1       | 1     |

The PDF/A rules are the appearances painting in DeviceRGB and DeviceGray with
no output intent (`6.2.3.3/1` and `/3` at part 1, `6.2.4.3/2` and `/4` from part
2), the `empty` field with no appearance (`6.9/2` at part 1, `6.3.3/1` from part
2), the push button whose `/N` is a stream where a button's is a state
dictionary (`6.5.3/5` at part 1, `6.3.3/3` from part 2), the missing XMP packet
(`6.7.2/1`, `6.6.2.1/1`, `6.7.2.1/1`), and at PDF/A-4 the `%PDF-1.7` header and,
at 4f, the missing `/EmbeddedFiles`.

### `remreach_2b.pdf`

`remreach_2b.pdf` holds defects compliance judges below a page's own
resources, where the repairs of their rules did not reach. Six DeviceN colour
spaces over one spot colorant each carry no `/Colorants`, which ISO 19005-2,
6.2.4.4 t1 fails at every colour link that reaches the space:

| Spot    | Where the DeviceN stands                                    | `6.2.4.4/1` |
| ------- | ----------------------------------------------------------- | ----------- |
| `SpotA` | the `/ColorSpace` resources of a form the page draws        | 3           |
| `SpotB` | the colour space of an image the page draws                 | 1           |
| `SpotC` | the resources of a coloured tiling pattern the page fills   | 3           |
| `SpotD` | the base of an `/Indexed` space the page selects            | 2           |
| `SpotE` | the resources of the Type 3 font whose glyph the page shows | 3           |
| `SpotF` | the resources of an annotation's normal appearance          | 3           |

The pattern's cell also draws an image with `/Interpolate` true (6.2.8 t3), and
the catalog's `/Names /EmbeddedFiles` tree names a file specification typed
`/F`, which ISO 32000-1, 7.11.4 makes one by position and no `/AF` lists (ISO
19005-3, 6.8 t4). The `/Colorants` repair walked the page's `/ColorSpace`
entries and shadings, the XObject repair the page's `/XObject` resources and
the forms they name, and the association repair the dictionaries typed
`/Filespec`, so a remediation left every one of these in place, on litho and
veraPDF alike. A CMYK OutputIntent covers the DeviceCMYK alternates.

| Rule at PDF/A-2b                | veraPDF | litho |
| ------------------------------- | ------- | ----- |
| `6.2.4.4/1` DeviceN colorants   | 15      | 15    |
| `6.2.8/3` `/Interpolate` true   | 1       | 1     |
| `6.8/5` embedded file not PDF/A | 1       | 1     |

At PDF/A-3b the file fails `6.8/4` once in place of `6.8/5`. The rule sets
equal veraPDF 1.30.2's in all thirteen cells, and so do the counts but for
`7.1/3` at PDF/UA-1 and `8.2.2/1` at PDF/UA-2, the family described under
[Instance counts](#instance-counts-which-the-corpus-does-not-pin).

The remediation at PDF/A-2a, 2b and 2u now leaves `6.8/5` alone, at PDF/A-4
`6.9/3` alone, the embedded file no repair can make conforming, and at
PDF/A-3b nothing, where each left the 15 DeviceN findings and the image, and
PDF/A-3b the association. The aggressive remediation, which drops the
attachment, leaves nothing at any PDF/A level. veraPDF agrees on every output
in both modes.

### `annotlang_ua1.pdf`

Two constructs no file of the seven corpora reaches, each planted so that it is
the only source of the rules it answers: a regression moves the rule set, not
just a count.

An annotation or a form field names a structure element by its `/StructParent`
(ISO 32000-1, 14.7.4.4), and veraPDF reads that element's `/Lang` as a
`CosLang` (GFPDAnnot.getLang, GFPDFormField.getLang) whether or not the
structure tree reaches it. Every rule scoped to `CosLang` judges it: ISO
14289-1 `7.2/29`, ISO 14289-2 `8.4.4/2`, `6.8.4/1` at PDF/A-1a and `6.7.4/1`
at PDF/A-2a and 3a. litho judged only the elements its tree walk reached. The
tree here holds two paragraphs and reaches no annotation:

| Square | Its element, outside the tree  | Answers                           |
| ------ | ------------------------------ | --------------------------------- |
| A1     | `/Lang (en_US)`                | every `CosLang` rule              |
| A2     | `/Lang ()`                     | the PDF/UA rules; PDF/A admits it |
| A3     | `/Lang (de-DE)`                | silent                            |
| A4     | none; the square's own `/Lang` | silent                            |

A form field names its element the same way, and a widget that is also a
field is read twice. No widget is planted, since the `mutate-pages` and
`mutate-flatten` cells treat widgets apart; `TestStructParentLangValidated`
pins that route.

ISO 14289-1 `7.21.6/3` and its restatements, `6.3.7/2` at PDF/A-1,
`6.2.11.6/3` at PDF/A-2 and 3, `6.2.10.6/3` at PDF/A-4 and `8.4.5.7/3` at
PDF/UA-2, forbid a symbolic TrueType font an `/Encoding` entry. veraPDF tests
`Encoding == null`, where `Encoding` is the name `/Encoding` names or the
`/BaseEncoding` of the dictionary (GFPDSimpleFont.getEncoding), so a dictionary
naming no base encoding passes. `S1`'s lists `/Differences` only and `S2`'s is
empty but for its `/Type`; litho reported both. Both embed the symbolic program
of `ttcmap_2b.pdf`'s T4 and carry a `/ToUnicode`, so no other font rule fires.
An `/Encoding` that is neither a name, a dictionary nor null stops veraPDF
1.30.2 with a NullPointerException, so it is not planted.

| Profile         | Rule         | litho | veraPDF 1.30.2 | litho before |
| --------------- | ------------ | ----- | -------------- | ------------ |
| PDF/UA-1        | `7.2/29`     | 2     | 2              | 0            |
| PDF/UA-2        | `8.4.4/2`    | 2     | 2              | 0            |
| PDF/A-1a        | `6.8.4/1`    | 1     | 1              | 0            |
| PDF/A-2a, 3a    | `6.7.4/1`    | 1     | 1              | 0            |
| PDF/UA-1        | `7.21.6/3`   | 0     | 0              | 2            |
| PDF/UA-2        | `8.4.5.7/3`  | 0     | 0              | 2            |
| PDF/A-1a, 1b    | `6.3.7/2`    | 0     | 0              | 2            |
| PDF/A-2a to 3u  | `6.2.11.6/3` | 0     | 0              | 2            |
| PDF/A-4, 4e, 4f | `6.2.10.6/3` | 0     | 0              | 2            |

Before the fix every one of the thirteen cells disagreed. After it the rule
sets and the counts of every rule equal veraPDF 1.30.2's in all thirteen, and
in the remediated output of the eight profiles `manifest.json` lists, default
and aggressive. Remediation used to leave the language rules at PDF/UA-1,
PDF/UA-2, PDF/A-1a and PDF/A-2a on both tools, since the `/Lang` repair walked
the structure tree alone. It now strips the tags of the elements the
`/ParentTree` maps as well, and the remediated output of those cells and of
WTPDF-1.0-Accessibility, default and aggressive, fails no language rule on
either tool.

### `xmppage_2b.pdf`

ISO 19005-2, 6.6.2.3 constrains the properties and extension schemas of every
metadata stream in the file. veraPDF models the packet a page carries as an
XMPPackage like the catalog's (`GFPDMetadata.getXMPPackage`): its properties
resolve through its own extension schemas and the catalog packet's, and its
schemas answer the structure tests. litho judged a page packet's properties by
name alone and never judged its schemas, so a namespace no packet declares
passed, and every structure defect did. No corpus file reaches a rule set this
changes: the 16 files whose counts rise already fail the same rules elsewhere.
Each page of the fixture plants one kind of defect, so each rule it fires comes
from one page:

| Page | Packet                                                          | Rule at PDF/A-2b              |
| ---- | --------------------------------------------------------------- | ----------------------------- |
| 1    | `eu:foo`, a namespace no packet declares                        | `6.6.2.3.1/1` and `/2`        |
| 1    | `ex:pt`, declared by the catalog packet alone                   | silent                        |
| 2    | a property with no description, one of category `sideways`      | `6.6.2.3.3/10`, `6.6.2.3.3/9` |
| 2    | an entry with no prefix, a value type with no description       | `6.6.2.3.3/4`, `6.6.2.3.3/14` |
| 2    | a `dc:stray` field inside a schema entry                        | `6.6.2.3.2/1`                 |
| 3    | `pdfaExtension:schemas` written as an `rdf:Seq`                 | `6.6.2.3.3/1`                 |
| 4    | `ex:pa` of type `MainT`, which the catalog's `ex` entry defines | silent                        |
| 4    | an entry with no property array, a field of type `Dimensions`   | `6.6.2.3.3/17`                |

Page 4's entry resolves `MainT` through the catalog's entry for the same
namespace, as veraPDF builds a page entry's validators on a copy of the catalog
entry's (`AXLXMPPackage.getCurrentSchemasDefinitionPDFA_2_3`). No property name
repeats across packets: veraPDF checks one property per namespace, prefix and
name in a document, the first its traversal meets, and litho judges every
packet's, so a repeated name would tie the verdict to that order.

| Rule at PDF/A-2b | veraPDF | litho | litho before |
| ---------------- | ------- | ----- | ------------ |
| `6.1.7.1/1`      | 4       | 4     | 4            |
| `6.1.7.1/2`      | 10      | 10    | 10           |
| `6.6.2.3.1/1`    | 1       | 1     | 0            |
| `6.6.2.3.1/2`    | 1       | 1     | 0            |
| `6.6.2.3.2/1`    | 1       | 1     | 0            |
| `6.6.2.3.3/1`    | 1       | 1     | 0            |
| `6.6.2.3.3/4`    | 1       | 1     | 0            |
| `6.6.2.3.3/9`    | 1       | 1     | 0            |
| `6.6.2.3.3/10`   | 1       | 1     | 0            |
| `6.6.2.3.3/14`   | 1       | 1     | 0            |
| `6.6.2.3.3/17`   | 1       | 1     | 0            |

ISO 19005-1 builds no package for a page's metadata stream, so PDF/A-1b and
PDF/A-1a are silent on the page packets, on both tools. All thirteen cells
return the same rule set and the same counts on both.

The remediation of every PDF/A-2 and PDF/A-3 cell used to leave `6.6.2.3.3/1`
and `/17` on both tools, as it did when the catalog packet carries the same two
defects. litho used to report nothing there, where veraPDF reported those two
and, at PDF/A-2b, where nothing else ran the metadata repair, all nine. The
repair now renames page 3's `rdf:Seq` an `rdf:Bag` and drops the value types of
page 4's entry with no property array, which no property can use, so the
PDF/A-2b and PDF/A-3b outputs, default and aggressive, pass on both tools.

### `inlineinterp_2b.pdf`

ISO 19005-2, 6.2.8 t3 holds every image dictionary to an `/Interpolate` of
false, and an inline image writes the key `/I` (ISO 32000-1, 8.9.7, Table 93).
veraPDF judges each inline image as an image of its own, wherever a content
stream it parses holds it. The images repair set `/Interpolate` on image
XObjects alone, so every inline image kept its finding through a remediation.
The corpus holds such images in page content and tiling pattern cells only;
this fixture puts one in each other stream veraPDF parses:

| Stream                                          | Key written                 | A-2b           |
| ----------------------------------------------- | --------------------------- | -------------- |
| a form XObject the page draws                   | `/I true`                   | `6.2.8/3`      |
| glyph `a` of a Type 3 font the page shows       | `/Interpolate true`         | `6.2.8/3`      |
| the normal appearance of a Square annotation    | `/I true`                   | `6.2.8/3`      |
| the page's content, an indirect array of two    | `/Interpolate true /I true` | `6.2.8/3`      |
| the same content, text in Helvetica, unembedded | none                        | `6.2.11.4.1/1` |

Each image's data holds a NUL, a high byte, a delimiter and an end-of-line
marker. The repair respells each `true` as `false` in place and carries every
other byte across, so all four streams are rewritten, the page's through the
array its `/Contents` names. That array is why the unembedded font is there: no
repair answers it, so it stays in every output, and a rewrite of the page a
validator can no longer read shows as a finding one tool reports and the other
does not.

| Rule at PDF/A-2b                 | veraPDF | litho | litho remediated |
| -------------------------------- | ------- | ----- | ---------------- |
| `6.2.8/3` `/Interpolate` true    | 4       | 4     | 0                |
| `6.2.11.4.1/1` font not embedded | 1       | 1     | 1                |
| `6.2.4.3/4` DeviceGray           | 6       | 6     | 0                |
| `6.6.2.1/1` no metadata          | 1       | 1     | 0                |

The rule sets equal veraPDF 1.30.2's in all thirteen cells, and so do the
counts but for `7.1/3` and `7.2/34` at PDF/UA-1 and `8.2.2/1` at PDF/UA-2, the
family described under
[Instance counts](#instance-counts-which-the-corpus-does-not-pin). Remediated
at the eight profiles `manifest.json` lists, default and aggressive, every
output leaves the font alone at the PDF/A levels, `6.3.4/1` at PDF/A-1b, and
`7.18.1/2` and `7.21.4.1/1` at PDF/UA-1 and `8.4.5.5.1/1` at PDF/UA-2, where no
interpolation rule applies, and veraPDF agrees on each one's rule set and
counts.

Before the repair, every PDF/A output kept the four `6.2.8/3` findings on both
tools. With the repair but not the write of the page's array, the rewrite wrote
the reference to its new stream as the value of the array's object, which
qpdf reads as an integer and litho's text extraction as no text: the writer
rolled the content repairs back and the four findings came back.

### `formhidden_ua1.pdf`

ISO 32000-1, Table 165 keeps a Hidden annotation off the screen and off paper,
and a NoView one off the screen, printing it only when its Print flag is set.
Flattening a form used to keep such widgets, and with them a form field in a
document meant to have none. It now drops each one with its field, drawing
nothing, and prunes the Form element it leaves holding nothing. A printed
NoView widget goes too. Content that shows only in print needs an optional
content group whose `/AS` applies a print usage (ISO 32000-1, 8.11.4.4), and
veraPDF reports `6.1.13/1` at PDF/A-1b, `6.9/4` at PDF/A-2b and PDF/A-3b,
`7.10/2` at PDF/UA-1 and `8.7/2` at PDF/UA-2 on a flattened page holding the
appearance in such a group, where the same page without it passes. Without
`/AS` the group passes but keeps its `/D` state in print too, so it never
prints. No fixture carried a hidden widget, so `mutate-flatten` never reached
one: pdfjs `annotation_hidden_noview.pdf`, `bug1737260.pdf`,
`widget_hidden_print.pdf` and verapdf `PDF_UA-1__7.18.1-t03-pass-c.pdf` did,
outside the cell.

One tagged page presents six text fields, each with an appearance painting a
box:

| Field    | `/F`                             | Structure element | After `FlattenForm`            |
| -------- | -------------------------------- | ----------------- | ------------------------------ |
| `secret` | 6: Hidden, Print                 | its own Form      | dropped, its element pruned    |
| `draft`  | 2: Hidden                        | none              | dropped                        |
| `stamp`  | 36: NoView, Print                | its own Form      | dropped, its element pruned    |
| `note`   | 32: NoView                       | its own Form      | dropped, its element pruned    |
| `tip`    | 292: NoView, ToggleNoView, Print | its own Form      | dropped, its element pruned    |
| `name`   | 4: Print                         | its own Form      | baked, tagged, PrintField `tv` |

ISO 14289-1, 7.18.4 test 1 exempts a hidden widget from the Form tag, so
`draft` needs none. ISO 14289-2, 8.9.2.2 test 2 fires on `stamp` and `note` and
not on `tip`, whose ToggleNoView it permits. With an XMP packet claiming
`pdfuaid:part` 1, the file conforms to PDF/UA-1 on both tools, one of the two
cells of this corpus that report nothing, and its flattened output conforms too.

Measured on veraPDF 1.30.2 and litho, flattening before the fix kept all five
widgets and their fields, which `mutate-flatten` counts as `promise_broken`.
After it, the flattened file fails a subset of the input's rules at every
level, on both tools: the flags rule, `8.9.2.2/2` and `8.10.2.3/1` go, and
nothing comes. qpdf reports nothing on either file.

The file agrees with veraPDF in all thirteen cells, rule for rule and count for
count. PDF/UA-1 reports nothing on either tool. The other cells report the
flags of the five widgets (`6.5.3/2` at part 1, `6.3.2/2` from part 2, 5 each),
the appearances painting in DeviceGray with no output intent (`6.2.3.3/3` at
part 1, `6.2.4.3/4` from part 2, 13 each), the `pdfuaid` properties no
extension schema declares and the missing PDF/A identification (`6.7.9/2`,
`/3` and `6.7.11/1` at part 1, `6.6.2.3.1/1`, `/2` and `6.6.4/1` at parts 2
and 3, `6.7.3/1` at part 4), the `%PDF-1.7` header and, at 4f, the missing
`/EmbeddedFiles` at part 4. At PDF/UA-2 they are the part-1 claim (`5/2`,
`5/5`), the Document element outside the PDF 2.0 namespace (`8.2.5.2/2`), the
two NoView widgets (`8.9.2.2/2`) and the five tagged widgets with neither a
`Lbl` element nor `/Contents` (`8.10.2.3/1`).

Remediation at the eight manifest profiles, normal and aggressive, leaves
nothing on either tool except `8.10.2.3/1` three times at PDF/UA-2, on both.
The PDF/A remediations remove `secret`, `draft` and `note`, which show neither
on screen nor in print, with their fields and the Form elements and
`/ParentTree` keys of `secret` and `note`. `stamp` and `tip` print, so they
stay, their NoView flag cleared: they show on screen too.

### `annothidden_2b.pdf`

ISO 19005-2, 6.3.2 requires every annotation to print and to show on screen,
and ISO 32000-1, Table 165 keeps a Hidden annotation off the screen and off
paper, and a NoView one off the screen, printing it only with Print set. PDF/A
remediation removes an annotation that shows nowhere rather than make it
visible, with what goes with it, and repairs the flags of every other one. The
census found 49 Hidden or NoView annotations in 22 corpus files and none of the
constructs below, so no gate cell reached them:

| Annotation                           | `/F`                      | After PDF/A remediation                     |
| ------------------------------------ | ------------------------- | ------------------------------------------- |
| page 1 `note`, a Text                | 6: Hidden, Print          | removed                                     |
| its pop-up                           | none                      | removed with `note`                         |
| a Text whose `/IRT` names `note`     | 4: Print                  | removed with `note`, the reply it is        |
| `toggle`, a Square                   | 288: NoView, ToggleNoView | kept, `/F 4`: it shows on rollover          |
| `shown`, a Square                    | 4: Print                  | kept, the control                           |
| `note2`, a Text                      | 4: Print                  | kept, its `/Popup` dropped                  |
| its pop-up                           | 2: Hidden                 | removed                                     |
| page 2, a Square inline in `/Annots` | 2: Hidden                 | removed                                     |
| first widget of field `pair`         | 2: Hidden                 | removed, `pair` keeps its second widget     |
| only widget of field `lone`          | 32: NoView                | removed with `lone`, which leaves `/CO`     |
| `item`, only child of field `group`  | 6: Hidden, Print          | removed with `group`                        |
| widget of the signed field `sig`     | 2: Hidden                 | kept, `/F 4`: the signature needs its field |

Page 1's `/Annots` is an indirect array, which the removal writes back at its
own object number, where every later fixer reads it (see
`laterreads_2u.pdf`). The signature carries the CMS blob of
`sigcms_2b.pdf` with a `/ByteRange` patched to cover the file, so `6.4.3` is
silent. Every annotation but the pop-ups has an empty appearance, and the pages
draw nothing.

Measured on veraPDF 1.30.2 and litho, the file agrees with veraPDF in all
thirteen cells, rule for rule and count for count. From part 2 it fails
`6.3.2/2` eight times, once for each flagged annotation, and part 1 fails
`6.5.3/2` nine times, the pop-up with no `/F` among them. The rest is the
PDF/A-2b claim read at the other levels and the absent structure tree at the
accessible ones.

Remediation at the eight manifest profiles, normal and aggressive, leaves
nothing on either tool at the six PDF/A profiles: 13 annotations become 5 and 4
fields become 2. Before the removal each of those cells kept `6.3.2/2`, or
`6.5.3/2` twice at PDF/A-1b, for the hidden pop-up the flag repair skips, and
every hidden annotation came out visible. PDF/UA-1 leaves `7.18.1/2` three times
and PDF/UA-2 nothing, on both tools and as before: neither removes anything.

### `kidsrepair_1b.pdf`

ISO 32000-1, 7.7.3.4 hands a page stating no `/Resources` the dictionary of its
nearest ancestor in the page tree, whole. `parentreach_2b.pdf` made the colour,
graphics and language repairs find that ancestor through the node listing the
page, but seven more still read the dictionary the parser merges key by key
from every ancestor.
The transparency repair wrote an inline graphics state repair through the
page's own `/Resources` entry, so it dropped every repair of a dictionary a
page inherits. And a page written directly in its node's `/Kids`, against ISO
32000-1, Table 29, has no object number: page-resources skipped it, and every
repair of the `/Resources` it states was refused. A census of every corpus
found two such pages, in pdfjs `issue9105_other.pdf` and `issue9540.pdf`,
neither carrying a defect a repair owns.

Four pages each draw one graphics state with `/CA 0.5`, written directly in its
`/ExtGState`, itself written directly in its `/Resources`, so a repair can only
be written through the page tree node holding them:

| Page  | Where it sits                        | Its `/Resources` | Draws | Lent by |
| ----- | ------------------------------------ | ---------------- | ----- | ------- |
| `Rt`  | the root's `/Kids`                   | none             | `GSR` | root    |
| `Mis` | node M's `/Kids`, `/Parent` the root | none             | `GSM` | node M  |
| `Kid` | written directly in node K's `/Kids` | none             | `GSK` | node K  |
| `Own` | written directly in node K's `/Kids` | its own          | `GSO` | itself  |

`GSO` also carries a transfer function, and `Own` a transparency page group.
At PDF/A-1b the file answers `6.4/5` four times, one per graphics state,
`6.4/3` for the group and `6.2.8/1` for the transfer function. From PDF/A-2 on
it answers `6.2.2/2` for the three pages naming only inherited resources and
`6.2.5/1` for the transfer function, beside the identification and tagging
rules a part-1 claim draws at the other levels. The two tools return the same
rule set and the same count for every rule in all thirteen cells.

Measured on veraPDF 1.30.2 and litho alike, the PDF/A-1b outputs kept all six
findings in both modes before the fix, and the PDF/A-2a, 2b, 2u, 3b and 4
outputs kept `6.2.2/2` for `Kid` and `6.2.5/1` for `Own`. Every output of the
eight manifest profiles now passes both tools in both modes; the PDF/UA ones
passed before too.

The mutations missed the two direct pages as well. `mutate-stamp` left `Kid`
and `Own` unstamped, and `mutate-pages` rebuilt the tree from the pages'
object numbers, listing both as object 0: litho read one page of four, qpdf
warned that the tree holds a non-dictionary object, veraPDF could not parse the
output, and litho's rule set lost `6.2.5/1` from PDF/A-2 on and `6.2.8/1` and
`6.4/3` at PDF/A-1b. A Writer now gives each page written directly in `/Kids`
an object number before any edit or repair, and node K names both by
reference. Every mutation or remediation output that changes the file lists
four pages and reads in qpdf without a warning. On the stamped and moved
outputs veraPDF reports the fixture's rule set less `6.2.2/2`, which a page
stating its own `/Resources` passes, plus the font and colour rules the stamp
draws.

### `sigtagged_ua1.pdf`

A signature covers the bytes its `/ByteRange` names (ISO 32000-1, 12.8.1). An
incremental update appends after them and keeps it valid; a full rewrite moves
every one of them, so litho draws each signed widget's appearance into its page
and drops the signature, its field and the `/Perms` entry naming it (ISO
32000-1, 12.8.4). No fixture was both tagged and signed, so the mutation cells
never saw a signature drawn into a tagged page, nor a certification a rewrite
drops.

One tagged page shows `AB` as a paragraph and presents three fields, each merged
with its widget, carrying a `/TU` and the only kid of its own Form element:

| Field      | Kind                                           | After a full rewrite                                |
| ---------- | ---------------------------------------------- | --------------------------------------------------- |
| `approval` | signed; `/Perms /DocMDP` names it, with `/P 2` | drawn into the page under its Form element, as `tv` |
| `name`     | a text field                                   | kept                                                |
| `cosign`   | an unsigned signature field                    | kept, to be signed                                  |

The signature is genuine: the builder signs the file's bytes itself, a detached
CMS SignedData with one RSA signer, its self-signed certificate and no signed
attributes, so a rebuild is byte for byte and poppler's pdfsig reports it valid,
its certificate untrusted. No appearance draws text, so drawing one into the
page changes no page text, which `mutate-pages` and `mutate-encrypt` compare.

Measured on veraPDF 1.30.2 and litho before the fix, the full rewrites of
`mutate-pages` and `mutate-encrypt` copied `approval` with its `/ByteRange`,
which pdfsig reported invalid or not verified, and both tools failed `6.4.3/1`
on the page move at PDF/A-2b; `mutate-flatten` kept `/Perms /DocMDP` naming a
signature no field held. After it, pdfsig finds only the unsigned `cosign` on
the page move and no signature on the flattened output, each passes PDF/UA-1 on
both tools, and `mutate-sign` keeps `approval` valid beside the signature it
adds. The `pages` and `encrypt` cells no longer admit `6.4.3/1`, so a rewrite
that copied the signature again fails 8 cells here.

The file agrees with veraPDF in all thirteen cells, rule for rule and count for
count, and so does each of the page move, the flattened, the signed and the
rewritten output. PDF/UA-1 reports nothing on either tool. The other cells
report the appearances painting in DeviceRGB and DeviceGray with no output
intent (`6.2.3.3/1` and `/3` at part 1, `6.2.4.3/2` and `/4` from part 2), the
`pdfuaid` properties no extension schema declares and the missing PDF/A
identification (`6.7.9/2`, `/3` and `6.7.11/1` at part 1, `6.6.2.3.1/1`, `/2`
and `6.6.4/1` at parts 2 and 3, `6.7.3/1` at part 4), the `%PDF-1.7` header and,
at 4f, the missing `/EmbeddedFiles` at part 4. At PDF/UA-2 they are the part-1
claim (`5/2`, `5/5`), the Document element outside the PDF 2.0 namespace
(`8.2.5.2/2`) and the three widgets with neither a `Lbl` element nor `/Contents`
(`8.10.2.3/1`).

Remediation at the eight manifest profiles, default and aggressive, leaves
nothing on either tool except `8.10.2.3/1` at PDF/UA-2, on both. The PDF/A-4
remediation writes `%PDF-2.0` in a full rewrite, which drops the signature; the
others update the file incrementally and keep it.

### `kidsarray_2b.pdf`

ISO 32000-1, 7.7.3.2, Table 29 makes `/Kids` an array of indirect references. A
Writer gives each page written directly in one an object number before any edit
or repair, and has the array name it by reference. `kidsrepair_1b.pdf` writes
its two direct pages in a `/Kids` array its node holds inline. Here node L names
its `/Kids` array by reference, so the numbering rewrites the array, not the
node, and no corpus file had that shape. veraPDF 1.30.2 refuses a file whose
page tree holds an intermediate node written directly in `/Kids` ("doesn't
appear to be a valid PDF"), so the other shape the numbering handles has no
fixture.

| Page | Where it sits                                   | Shows | Carries                    |
| ---- | ----------------------------------------------- | ----- | -------------------------- |
| 1    | the root's `/Kids`, by reference                | `A`   | nothing                    |
| 2    | the array node L names as its `/Kids`, directly | `B`   | a text field, a page `/AA` |
| 3    | the same array, directly, after page 2          | `AB`  | a Text annotation, no `/F` |

Each page states its own `/Resources` and shows its text in the conforming
embedded TrueType of `ttcmap_2b.pdf`, painted in the initial DeviceGray with no
output intent (`6.2.3.3/3` at part 1, `6.2.4.3/4` from part 2, once per page).
Page 2's `/AA` holds a permitted GoTo action and fails `6.5.2/2` at parts 2 and
3 and `6.6.3/1` at part 4. Page 3's annotation fails `6.5.3/2` at part 1 and
`6.3.2/1` from part 2. Every appearance paints nothing. The other cells report
the identification and tagging rules a part-2 claim draws at the other levels.

The two tools return the same rule set in all thirteen cells, and the same
counts in the eleven PDF/A ones. At PDF/UA-1 veraPDF counts `7.1/3` and
`7.2/34`, and at PDF/UA-2 `8.2.2/1`, once per untagged content item, and litho
once each.

Measured on litho and veraPDF 1.30.2 before the numbering, the Writer missed
both direct pages. `mutate-stamp` left them unstamped, `mutate-pages` wrote a
tree listing them as object 0, which litho read as no page at all and qpdf
refused, `mutate-flatten` left the field's widget on page 2, and
`mutate-autotag` wrote the structure elements of pages 2 and 3 with
`/Pg 0 0 R`. Remediation left page 2's `/AA` in place (`6.5.2/2` at PDF/A-2a,
2b, 2u and 3b, `6.6.3/1` at PDF/A-4) and, at PDF/UA-1 and PDF/UA-2, the content
of the two pages untagged (`7.1/3`, `8.2.2/1`) and their `/Tabs` unset
(`7.18.3/1`, `8.9.3.3/1`), on both tools. Now every `mutate-*` cell passes but
`mutate-redact`, which refuses by design, and every remediation output of the
eight manifest profiles, default and aggressive, passes both tools but the
PDF/UA-2 ones, where both report `8.8/2` for the destination of page 2's GoTo
action. qpdf reads each output without a warning.

### `formmark_ua1.pdf`

ISO 14289-1, 7.1 asks that real content be tagged or marked as an artifact (test
3), and that neither sit inside the other (tests 1 and 2); ISO 14289-2, 8.2.2
asks the first again. veraPDF builds its content items from what a form XObject
draws, in the marked-content state around the `Do` (ISO 32000-1, 14.6): a form
that paints nothing is no content, and the sequences a form opens are judged as
the page's are. litho counted every `Do` of a form as one untagged content item
and read no marked content inside a form. Flattening `annothidden_2b.pdf` drew
its empty appearances onto an untagged page, and litho reported `7.1/3` and
`8.2.2/1` on the output where veraPDF reports neither. No corpus file opened a
sequence inside a form, or drew a form that paints nothing into a tagged
document.

One tagged page paints a square as a `/P` and draws six forms:

| Form       | Draws                                                                  | Verdict          |
| ---------- | ---------------------------------------------------------------------- | ---------------- |
| `Empty`    | nothing                                                                | silent           |
| `ArtForm`  | a square in its own `/Artifact`                                        | silent           |
| `TagForm`  | a square under `/MCID 3`, which only its `/StructParents 1` entry maps | silent           |
| `ObjrForm` | an unmarked square; its `/StructParent 2` names a `/Figure` over it    | silent           |
| `Chain`    | a form whose content stream is empty                                   | silent           |
| `NestForm` | in the page's `/MCID 1`, an unresolved `/MCID 0`, then an `/Artifact`  | `7.1/1`, `7.1/2` |

Measured on veraPDF 1.30.2 and litho, the file agrees with veraPDF in all
thirteen cells, rule for rule and count for count. PDF/UA-1 reports `7.1/1` and
`7.1/2`, both from `NestForm`'s artifact, which opens inside the page's tagged
sequence, and PDF/UA-2 the part-1 claim (`5/2`, `5/5`) and the Document element
outside the PDF 2.0 namespace (`8.2.5.2/2`). Before the fix litho reported
`7.1/3` in place of the two at PDF/UA-1, and `8.2.2/1` beside the three at
PDF/UA-2. The PDF/A cells report the six squares painting in DeviceGray with no
output intent (`6.2.3.3/3` at part 1, `6.2.4.3/4` from part 2), the `pdfuaid`
properties no extension schema declares and the missing PDF/A identification
(`6.7.9/2`, `/3` and `6.7.11/1` at part 1, `6.6.2.3.1/1`, `/2` and `6.6.4/1` at
parts 2 and 3, `6.7.3/1` at part 4), the `%PDF-1.7` header at part 4 and, at 4f,
the missing `/EmbeddedFiles`.

Remediation at the eight manifest profiles, default and aggressive, leaves
nothing on either tool but `7.1/1` and `7.1/2` at PDF/UA-1, on both: no repair
reaches into a form. The flattened output reports the input's rule set at every
manifest profile.

### `laterreads_2u.pdf`

Remediation runs its fixers over one allocator, and a fixer reads an object
where an earlier one queued it. Four repairs read past that. The annotations
fixer writes the `/Annots` it filtered at the array's own object number and
queues each annotation it removes as the null object: annot-opacity,
annot-appearance, annot-artifact, af-relationship and forbidden-annots read the
array as the file wrote it, and wrote the removed annotation back with their
own repair. annot-flags wrote the array it filtered into the page instead, so
the array object stayed as the file wrote it, still listing what it removed.
The fonts repair decided a font's `/ToUnicode` on the first page whose
`/Resources` named it, from that page's content alone. And the graphics repair
skipped the appearances of a page that resolves no `/Resources`, though an
appearance draws from its own (ISO 32000-1, 12.5.5). No gated corpus file
reaches any of them.

| Page | Carries                                                                                                                               | Read by                         |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------- |
| 1    | no `/Resources` above it, no `/Contents`; a Square whose appearance has `/OPI`, drawing an image with `/Interpolate true`             | graphics                        |
| 2    | an indirect `/Annots`: a Screen and a Square, each with `/CA 0.5` and no appearance                                                   | annot-opacity, annot-appearance |
| 3    | an indirect `/Annots`: a Hidden FileAttachment written inline, whose file specification has no `/UF` and embeds a text file; a Square | annot-flags                     |
| 4    | `/Resources` naming the unembedded ZapfDingbats `Zp`, which the page shows nothing with                                               | fonts                           |
| 5    | `Zp` showing two codes, a form naming `Zf` showing two, an appearance naming `Za` showing two                                         | fonts                           |

The image is in an ICCBased space and the file carries a CMYK PDF/A output
intent. An image in a device space makes veraPDF look up a `/Default` space in
the `/Resources` page 1 lacks and report the device family as an undefined name
(`6.2.2/3` at part 4), and a repair of device colour gives the page `/Resources`
before the graphics repair runs.

Measured on veraPDF 1.30.2 and litho, the file returns the same rule set in all
thirteen cells, and the same counts in the eleven PDF/A ones. At PDF/UA-1
veraPDF counts `7.1/3` and `7.2/34`, and at PDF/UA-2 `8.2.2/1`, once per
untagged content item, and litho once each. The PDF/A cells report the three
unembedded fonts (`6.3.4/1` at part 1, `6.2.11.4.1/1` at parts 2 and 3,
`6.2.10.4.1/1` at part 4), their six codes with no Unicode mapping (`6.3.8/1`
at PDF/A-1a, `6.2.11.7.2/1` at the A and U levels of parts 2 and 3), the
appearance's `/OPI` and the image's `/Interpolate` (`6.2.4/2` and `/3` at part
1, `6.2.9/1` and `6.2.8/3` at parts 2 and 3, `6.2.8.1/1` and `6.2.7.1/3` at
part 4), the annotations (`6.5.2/1`, `6.5.3/1` and `/2` at part 1, `6.3.1/1`,
`6.3.2/2` and `6.3.3/1` from part 2) and the attachment (`6.1.11/1` at part 1,
`6.8/2` and `/5` at part 2, `6.8/1` to `/4` at part 3, and the `6.9` tests at
part 4), beside the identification and tagging rules a part-2 claim draws at the
other levels. veraPDF judges the attachment's file specification in the array
object, which it sweeps as an indirect object.

Remediation at the eight manifest profiles, default and aggressive, leaves, on
both tools and count for count, the unembedded fonts at the PDF/A profiles
(`6.3.4/1`, `6.2.11.4.1/1` or `6.2.10.4.1/1`), `7.1/3`, `7.18.1/2` and
`7.21.4.1/1` at PDF/UA-1, and `8.2.2/1`, `8.4.5.5.1/1` and `8.9.2.4.12/1` at
PDF/UA-2. Before the fixes, on both tools, every PDF/A output kept the
appearance and image findings, the PDF/A-2a and 2u outputs kept `6.2.11.7.2/1`,
PDF/UA-1 `7.21.7/1` and PDF/UA-2 `8.4.5.8/1`, six times each, and the default
PDF/A-1b output kept `6.1.11/1` and the default PDF/A-2a, 2b and 2u outputs
`6.8/5`, from the attachment's embedded file in the array object annot-flags
left as the file wrote it. The PDF/A-1b to 3b outputs also held the removed
Screen again, as an object no page lists, which no rule judges; the full rewrite
of a PDF/A-4 output drops it either way.

### `baretree_2b.pdf`

ISO 32000-1, 7.7.2, Table 28 makes the catalog's `/Pages` an indirect
reference; 7.7.3.2, Table 29 requires a page tree node's `/Type /Pages` and a
`/Count` of the pages under it; 7.7.3.3, Table 30 requires a page's `/Parent`,
and its `/Resources` and `/MediaBox`, both inheritable. A rewrite copied each
entry as the source had it, and page insertion and deletion refused a root with
no object number. The corpus files that lack `/Type` on a node or state a wrong
`/Count` are damaged past what veraPDF parses, and none writes the root
directly in the catalog.

The catalog writes the root directly, so no page names a `/Parent`. The root
has no `/Type`, lends nothing, and counts four of its five pages:

| Page | Its own box              | Its own `/Resources` | Shows   |
| ---- | ------------------------ | -------------------- | ------- |
| 1    | 200 by 200               | the font             | `A`     |
| 2    | 200 by 200, by reference | the font             | `B`     |
| 3    | 200 by 200               | none                 | nothing |
| 4    | 200 by 200               | `5`, no dictionary   | nothing |
| 5    | none                     | the font             | `AB`    |

Page 2 is the control: its box is a rectangle written by reference, and stays
as it is. Pages 3 and 4 have no `/Contents`: veraPDF reports a page with no
`/Resources` whose content paints anything, DeviceGray included, under
`6.2.2/3` at PDF/A-4, which litho does not. The text is in the conforming
embedded TrueType of `ttcmap_2b.pdf`, in DeviceGray under a CMYK OutputIntent.
veraPDF 1.30.2 refuses a file holding
a node or a page with no `/Type`, a root with no `/Count` or one larger than its
pages. It reads no XMP from a catalog with no `/Type`, and it reads a
`/MediaBox` of three numbers with a 0 for the missing one and reports
`6.1.13/11`, which litho does not judge on a box short of four numbers. None of
those shapes has a fixture: pdfjs `issue5334.pdf` has a catalog with no
`/Type`, and pdfjs `issue15150.pdf` a `/Resources` naming no dictionary.

The two tools return the same rule set in all thirteen cells, and the same
counts in the eleven PDF/A ones: nothing at PDF/A-2b, and at the other levels
the identification and tagging rules a part-2 claim draws. At PDF/UA-1 veraPDF
counts `7.1/3` and `7.2/34`, and at PDF/UA-2 `8.2.2/1`, twice, once per
untagged text of the four pages its `/Count` lets it read, and litho once each.

Measured on litho and veraPDF 1.30.2 before the fix, `mutate-pages` refused the
file (`/Pages is not an indirect reference`), and the stamp, flattening,
encryption, auto-tagging, split, merge and every remediation that wrote the
file wrote the root with no `/Type` and pages 3, 4 and 5 as the source has them:
qpdf warned four times on each. Now a rewrite numbers the root and writes it
with `/Type /Pages` and the `/Count 5` its walk reads, every page with the root
as its `/Parent`, pages 3 and 4 with an empty `/Resources` and page 5 with the
US Letter box every reader assumes, and qpdf reads it without a warning. An
incremental update writes only what it edits: a remediation that repairs no
page leaves pages 3, 4 and 5 as the source has them. The outputs report the
input's rule set on both tools, and the stamp adds only its unembedded font and
DeviceRGB. Remediation at the eight manifest profiles, default and aggressive,
leaves nothing on either tool, before the fix and after.

### `namedmcid_ua1.pdf`

A `BDC` operator writes its property list inline or names it from the
`/Properties` of the current resources (ISO 32000-1, 14.6.2), and the `/MCID`
that ties the content to its structure element sits in that list either way.
veraPDF resolves the name through the stream's own resources and then through
the page's (`PDResourcesHandler.getProperties`): a form's own definition wins,
even one that is no dictionary, and only a missing or null one falls to the
page's. litho read the `/MCID` and `/Lang` of an inline list alone, and resolved
a form's names through the form's resources alone. No corpus file names a list
carrying an `/MCID` under a structure tree.

The catalog carries no `/Lang`, and every structure element but two carries one,
so each language finding comes from the construct planted for it. Page 1 shows:

| Construct                                                                 | Verdict        |
| ------------------------------------------------------------------------- | -------------- |
| text under `/MC0`, which the page's `/Properties` maps to `/MCID 0`       | silent         |
| form `/Fm1`, whose text sits under `/MC1` from its own `/Properties`      | silent         |
| form `/Fm2`, whose text sits under `/MC2`, which only the page defines    | silent         |
| text under `/MC4`, `/MCID 2`, owned by an element with no `/Lang`         | `7.2/34`       |
| text under `/MC5`, whose list carries the `/Lang` its element lacks       | silent         |
| form `/Fm3`, a Private Use glyph under `/MC6` with `/ActualText`, page's  | silent         |
| form `/Fm5`, a square in a `/Span` under the page's `/MC8`, `/ActualText` | `7.2/30`       |
| `/MCID 9` unmapped inside the mapped `/MCID 1`, over text                 | silent         |
| `/MCID 8` unmapped, wrapping nothing                                      | silent         |
| `/MCID 7` unmapped, wrapping only an artifact                             | silent         |
| a Link written inline in an `/Annots` array page 2 shares, no `/Contents` | `7.18.5/2` x 2 |

`/Fm5`'s `/Span` has no `/Lang`, and the `/MC9` around it names an element with
none, so the language of its `/ActualText` is determined nowhere. The three
unmapped identifiers are the shapes the retired orphan-MCID check reported as
`7.1/3` and veraPDF does not: content under an unmapped `/MCID` is tagged by the
innermost mapped one around it, and a sequence painting nothing, or only an
artifact, is no untagged content. veraPDF builds the inline Link once for each
page listing it, and skips a Link that is an object of its own when it meets it
again, so `7.18.5/2`, `7.18.5/1` and `7.18.1/2` each count 2.

Measured on veraPDF 1.30.2 and litho, the file agrees with veraPDF in all
thirteen cells, rule for rule and count for count. PDF/UA-1 reports `7.2/30`,
`7.2/34`, `7.2/33` for the title with no language, the Link's three rules and
the unembedded font (`7.21.4.1/1`), and PDF/UA-2 the part-1 claim (`5/2`,
`5/5`), the Document element outside the PDF 2.0 namespace (`8.2.5.2/2`), the
missing default language (`8.4.4/1`) and the font (`8.4.5.5.1/1`). Before the fix
litho reported `7.1/3` twice at PDF/UA-1 and missed `7.2/30`, reported
`8.2.2/1` twice and `8.4.3/1` at PDF/UA-2, reported `6.2.11.7.3/1` at PDF/A-2a
and PDF/A-3a, and counted `7.18.5/2` once. The PDF/A cells report the
unembedded font, the nine device gray paints with no output intent
(`6.2.3.3/3` at part 1, `6.2.4.3/4` from part 2), the Link's missing print flag
(`6.5.3/2` at part 1, `6.3.2/1` from part 2), the three forms naming a list only
the page defines (`6.2.2/2` from part 2), the `pdfuaid` properties no extension
schema declares and the missing PDF/A identification, the `%PDF-1.7` header at
part 4 and, at 4f, the missing `/EmbeddedFiles`.

Remediation at the eight manifest profiles, default and aggressive, leaves the
same rule set and counts on both tools in every cell: the unembedded font
everywhere, and at PDF/UA-1 the inline Link's three rules besides. Before the
fix litho also left `7.1/3` at PDF/UA-1 and `8.2.2/1` and `8.4.3/1` at
PDF/UA-2. Deleting either page and flattening leave outputs on which both tools
report the same rule set and counts at both PDF/UA levels.

### `directstream_2b.pdf`

ISO 32000-1, 7.3.8.1 makes every stream an indirect object, and 7.5.7 keeps
streams out of object streams. litho's parser reads a stream written inside a
dictionary or an array, as veraPDF's does, and every writer path wrote it back
where it found it: qpdf then reads the object holding it as damaged, and a write
with `--compact` packed it into an object stream. The writers now give each
such stream an indirect object of its own, with a reference in its place. No
corpus file holds one. Split, merge and insert wrote one for two pdfjs files,
`poppler-937-0-fuzzed.pdf` and `issue13783.pdf`, whose page box runs on into the
page's `/Contents` reference: the page copy resolved the reference into the
`/CropBox` it wrote.

The page holds a stream in each place a copy meets one, each showing its own
text in the conforming embedded TrueType of `ttcmap_2b.pdf`, in DeviceGray with
no output intent:

| Where the stream sits                                    | Shows |
| -------------------------------------------------------- | ----- |
| the page's indirect content stream, first in `/Contents` | `A`   |
| the `/Contents` array, directly, second                  | `B`   |
| the page's `/XObject` resources, directly, as `/Fm0`     | `AB`  |
| `/Fm0`'s own `/XObject` resources, directly, as `/Fm1`   | `BA`  |

The direct content stream draws `/Fm0`, which draws `/Fm1`, and the page's
`/MediaBox` runs on past its four coordinates into a reference to the indirect
content stream, the shape of the two pdfjs files. pikepdf makes every stream
indirect, so the page is appended by hand in an incremental update.

Measured on veraPDF 1.30.2 and litho, the file returns the same rule set in all
thirteen cells. Both tools read every stream. veraPDF counts the four text
paints in DeviceGray (`6.2.3.3/3` at part 1, `6.2.4.3/4` from part 2) and litho
two; at PDF/UA-1 veraPDF counts `7.1/3` and `7.2/34`, and at PDF/UA-2
`8.2.2/1`, once per stream, and litho once each. The other cells report the
identification and tagging rules a part-2 claim draws.

Before the fix, every write of the file held the direct streams inline: split,
merge, insert, rotation, stamping, flattening, auto-tagging, encryption, and
remediation at the eight manifest profiles, default and aggressive. qpdf failed
every output, and a split with `--compact` held them inside an object stream.
Now each output holds every stream as an object of its own, qpdf reads it
without a warning, and its pages show the four texts. The gate's qpdf check
tells the two apart in `mutate-encrypt` alone: the input already carries the
warning classes an output writing the streams inline draws, so the other cells
read the same either way, and the Go tests of each writer path pin the rest.
Remediation at the eight manifest profiles, default and aggressive, leaves
nothing at the PDF/A profiles on either tool, and `7.1/3` at PDF/UA-1 and
`8.2.2/1` at PDF/UA-2 on both, veraPDF counting two and litho one, before the
fix and after.

## Scope

`manifest.json` expands every fixture across the eight profiles it lists, so the
runner scores 119 x 8 = 952 cells. The hand-measured matrix is wider: all
thirteen conformance levels veraPDF validates, so 119 x 13 = 1547 cells. litho
and veraPDF return the same verdict in all of them: in each cell either both
report nothing or both report something.

Verdict agreement is the weaker property. Rule-set agreement is what these
fixtures exist to protect, and it holds in 1545 of the 1547 cells. The two
exceptions, both on `complex_ua2`, are marked `*` in the counts table and
covered under [Known divergences](#known-divergences); veraPDF reports a
non-empty set in each, which is why the verdict still matches.

The runner's `compliance` feature scores both. `matched` counts the cells whose
verdicts agree, and `ruleset_mismatch`, a cost metric the baseline gates, counts
the cells whose sets of failed rules (clause and test) differ. Every cell but
three is non-compliant on both sides (`formhidden_ua1` and `sigtagged_ua1` at
PDF/UA-1 and `baretree_2b` at PDF/A-2b are the corpus's conforming cells), so a
change that drops or invents a rule class leaves `matched` at 952 and moves only
`ruleset_mismatch`, and on those three cells it moves both. Both exceptions sit
in the manifest's profiles, `complex_ua2` at PDF/UA-1 and PDF/UA-2, so the
metric reads 2 on this corpus, and the text scoreboard names the rules only one
side reports. A third cell in the runner is a regression, or a divergence to
cover under Known divergences and mark `*`.

The `remediation` and `remediation-aggressive` features validate each
remediated output with both tools, and their `disagreement` metric sums, over
the cells, the difference between the numbers of rules each reports. It reads 0
on this corpus in both modes, so any cell that moves it is a regression or a
divergence to cover under Known divergences.

The thirteen-level figures are measured by hand against a live veraPDF. The
thirteen flavours are `1a 1b 2a 2b 2u 3a 3b 3u 4 4e 4f ua1 ua2` on the veraPDF
side and the matching `PDF/A-*` and `PDF/UA-*` names on litho's:

```bash
go build -o /tmp/litho ./cmd/litho
/tmp/litho validate pdfs/complex_2b.pdf -profile "PDF/A-2b" -f json
verapdf -f 2b --format json --maxfailuresdisplayed -1 pdfs/complex_2b.pdf
```

Rerun that comparison whenever a compliance rule changes. A new class appearing
on one side only is either a fix or a regression, and this corpus is where it
shows up first.
