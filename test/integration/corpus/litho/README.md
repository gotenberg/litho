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

| Fixture                | Home profile | Focus                                                           |
| ---------------------- | ------------ | --------------------------------------------------------------- | --- | --- | --- | --- | --- | --- |
| `complex_1b.pdf`       | PDF/A-1b     | Part-1 prohibitions: transparency, embedded files, OCG          |
| `complex_2a.pdf`       | PDF/A-2a     | Level-A tagging: role map, structure hierarchy, language        |
| `complex_2b.pdf`       | PDF/A-2b     | Broadest mix; annotations, optional content, filters            |
| `complex_2u.pdf`       | PDF/A-2u     | Level-U Unicode: absent, forbidden and Private Use mappings     |
| `complex_3b.pdf`       | PDF/A-3b     | Embedded and associated files, one 6.8 test per attachment      |
| `complex_4.pdf`        | PDF/A-4      | Part-4 and PDF 2.0: rev 2020, docinfo, Requirements, PresSteps  |
| `complex_ua1.pdf`      | PDF/UA-1     | Structure and accessibility defects in a wired tag tree         |
| `complex_ua2.pdf`      | PDF/UA-2     | Part-2 specifics: namespaces, PUA text, structure destinations  |
| `forbidden_mix.pdf`    | PDF/A-2b     | Forbidden actions, PostScript tint transforms, LZW              |
| `formstruct_ua2.pdf`   | 4            | 6                                                               | 6   | 6   | 6   | 5   | 7   | 7   |
| `gstate_2b.pdf`        | PDF/A-2b     | Transfer functions and halftones in six graphics states         |
| `limits_2b.pdf`        | PDF/A-2b     | Implementation limits: real, string, name, nesting, DeviceN     |
| `megafont_2b.pdf`      | PDF/A-2b     | Font embedding and glyph-width scoping                          |
| `multipage_2b.pdf`     | PDF/A-2b     | Three pages, a violation class unique to each                   |
| `fontdict_2b.pdf`      | PDF/A-2b     | Font dictionary required keys; page box implementation limits   |
| `structnest_ua1.pdf`   | PDF/UA-1     | Clause 7.2: table, list and TOC nesting; per-element language   |
| `tables_ua1.pdf`       | PDF/UA-1     | Tables and lists: header association, nested list structure     |
| `tablespan_ua2.pdf`    | PDF/UA-2     | Table cell geometry: row and column spans                       |
| `extschema_2b.pdf`     | PDF/A-2b     | XMP extension schema structure across sibling entries           |
| `outputintent_2b.pdf`  | 4            | 7                                                               | 5   | 5   | 5   | 6   | 7   | 7   |
| `schemaprefix_1b.pdf`  | PDF/A-1b     | Extension schema RDF container types and absent fields          |
| `trailer_2b.pdf`       | PDF/A-2b     | Encrypted, with data after the last `%%EOF`                     |
| `limits_1b.pdf`        | PDF/A-1b     | Part-1 limits: integer, real, string, array, dictionary, CID    |
| `docinfo_1b.pdf`       | PDF/A-1b     | Document information dictionary against its XMP analogues       |
| `imgxmp_4.pdf`         | PDF/A-4      | Image dictionary keys; one XMP packet per page, one defect each |
| `idprefix_2b.pdf`      | PDF/A-2b     | Namespace prefixes on the PDF/A Identification Schema           |
| `uaid_ua1.pdf`         | PDF/UA-1     | The PDF/UA Identification Schema: part number, prefixes         |
| `ttencoding_2b.pdf`    | PDF/A-2b     | TrueType encoding keys in the font dictionary                   |
| `ttcmap_2b.pdf`        | PDF/A-2b     | `cmap` subtable sets in embedded TrueType font programs         |
| `fenote_ua2.pdf`       | PDF/UA-2     | Footnote and endnote structure: type, namespace, `/Ref` pairing |
| `tableregular_ua2.pdf` | PDF/UA-2     | Table regularity: cell intersection and section row groupings   |
| `artifact_ua1.pdf`     | PDF/UA-1     | Artifact and real-content marking; suspect tagging              |
| `annots_1b.pdf`        | PDF/A-1b     | Annotation dictionary constraints and additional actions        |
| `fontkeys_1b.pdf`      | PDF/A-1b     | Part-1 font dictionary required keys and embedded file subtype  |
| `cmaps_2b.pdf`         | PDF/A-2b     | CMap embedding, WMode consistency and `/UseCMap` chains         |
| `outputintent_2b.pdf`  | PDF/A-2b     | OutputIntent array and ICC destination profile headers          |
| `formstruct_ua2.pdf`   | PDF/UA-2     | Widget annotations, Form structure elements and XFA             |

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
| `fontkeys_1b`                             | A Helvetica with no `/FirstChar`, `/LastChar` or `/Widths`             | ISO 19005-1, 6.3.2 t4-t6     |
| `cmaps_2b`                                | A font naming `/Identity-H`, and one embedding a self-consistent CMap  | ISO 19005-2, 6.2.11.3.3      |
| `outputintent_2b`                         | A well-formed `prtr`/`RGB ` destination profile beside the bad one     | ISO 19005-2, 6.2.3 t1        |
| `formstruct_ua2`                          | A `Form` element enclosing exactly one widget                          | ISO 14289-2, 8.10.1 t2       |
| `formstruct_ua2`                          | A widget with no `/StructParent`, outside the structure tree           | ISO 14289-2, 8.10.1 t1       |

Verified counts at the home profile: `6.2.11.5` fires exactly once on
`complex_2b` (the rendered `T3A` glyph, not the unrendered `T3B` one) on both
litho and veraPDF, and `6.3.3` fires exactly three times, skipping the
zero-area annotation.

## Expected firing sets

Counts are distinct rule classes (`clause/test`) reported by litho. litho and
veraPDF agree exactly on the set in every cell except the two marked `*`,
which are covered under [Known divergences](#known-divergences).

| Fixture                | A-1b | A-2a | A-2b | A-2u | A-3b | A-4 | UA-1 | UA-2 |
| ---------------------- | ---- | ---- | ---- | ---- | ---- | --- | ---- | ---- |
| `annots_1b.pdf`        | 14   | 14   | 12   | 12   | 12   | 10  | 12   | 8    |
| `artifact_ua1.pdf`     | 3    | 3    | 3    | 3    | 3    | 4   | 5    | 3    |
| `cmaps_2b.pdf`         | 6    | 10   | 7    | 8    | 7    | 8   | 12   | 12   |
| `complex_1b.pdf`       | 23   | 22   | 19   | 20   | 21   | 19  | 14   | 11   |
| `complex_2a.pdf`       | 15   | 21   | 17   | 17   | 18   | 17  | 12   | 11   |
| `complex_2b.pdf`       | 18   | 23   | 20   | 21   | 22   | 19  | 14   | 12   |
| `complex_2u.pdf`       | 15   | 23   | 18   | 20   | 18   | 16  | 14   | 13   |
| `complex_3b.pdf`       | 17   | 23   | 20   | 21   | 21   | 21  | 14   | 10   |
| `complex_4.pdf`        | 18   | 25   | 23   | 23   | 25   | 23  | 14   | 9    |
| `complex_ua1.pdf`      | 6    | 7    | 6    | 6    | 6    | 5   | 11   | 10   |
| `complex_ua2.pdf`      | 8    | 16   | 12   | 12   | 12   | 8   | 17\* | 18\* |
| `docinfo_1b.pdf`       | 8    | 4    | 1    | 2    | 1    | 6   | 5    | 5    |
| `extschema_2b.pdf`     | 9    | 11   | 8    | 9    | 9    | 6   | 8    | 8    |
| `fontdict_2b.pdf`      | 7    | 10   | 8    | 8    | 8    | 8   | 7    | 7    |
| `fenote_ua2.pdf`       | 3    | 5    | 4    | 4    | 4    | 3   | 5    | 6    |
| `fontkeys_1b.pdf`      | 10   | 13   | 10   | 11   | 10   | 11  | 8    | 8    |
| `forbidden_mix.pdf`    | 12   | 14   | 12   | 12   | 14   | 14  | 11   | 9    |
| `gstate_2b.pdf`        | 6    | 12   | 10   | 10   | 10   | 10  | 7    | 7    |
| `idprefix_2b.pdf`      | 10   | 12   | 9    | 10   | 10   | 10  | 8    | 7    |
| `imgxmp_4.pdf`         | 8    | 15   | 13   | 13   | 13   | 8   | 7    | 6    |
| `limits_1b.pdf`        | 9    | 9    | 6    | 7    | 6    | 4   | 11   | 9    |
| `limits_2b.pdf`        | 8    | 12   | 10   | 10   | 10   | 6   | 10   | 8    |
| `megafont_2b.pdf`      | 9    | 13   | 10   | 11   | 10   | 10  | 12   | 11   |
| `multipage_2b.pdf`     | 10   | 13   | 11   | 11   | 11   | 10  | 10   | 9    |
| `schemaprefix_1b.pdf`  | 14   | 18   | 15   | 16   | 15   | 6   | 8    | 8    |
| `structnest_ua1.pdf`   | 4    | 4    | 4    | 4    | 4    | 3   | 36   | 30   |
| `tables_ua1.pdf`       | 5    | 5    | 5    | 5    | 5    | 4   | 2    | 5    |
| `tableregular_ua2.pdf` | 3    | 4    | 4    | 4    | 4    | 3   | 8    | 7    |
| `tablespan_ua2.pdf`    | 3    | 4    | 4    | 4    | 4    | 3   | 4    | 4    |
| `trailer_2b.pdf`       | 5    | 7    | 5    | 5    | 5    | 6   | 7    | 7    |
| `ttcmap_2b.pdf`        | 5    | 9    | 6    | 7    | 6    | 6   | 9    | 8    |
| `ttencoding_2b.pdf`    | 7    | 10   | 7    | 8    | 7    | 8   | 10   | 10   |
| `uaid_ua1.pdf`         | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 4    |

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

litho reports all three pages' classes and agrees with veraPDF on all eight
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
  landed. `d61d0ba` implemented it, and the two tools now agree on the
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

| Object                         | Defect                                | Rule      |
| ------------------------------ | ------------------------------------- | --------- |
| Square                         | `/CA 0.5`                             | `6.5.3/1` |
| Square                         | no `/F`, so Print cannot be set       | `6.5.3/2` |
| Square                         | `/C` array with no RGB OutputIntent   | `6.5.3/3` |
| Square                         | `/AP` carrying `/D` beside `/N`       | `6.5.3/4` |
| Widget, `/FT /Btn`             | `/N` is a stream, not a subdictionary | `6.5.3/5` |
| Square                         | `/N` is a subdictionary, not a stream | `6.5.3/6` |
| Widget                         | `/AA`                                 | `6.6.2/1` |
| parent form field              | `/AA`                                 | `6.6.2/2` |
| catalog                        | `/AA`                                 | `6.6.2/3` |
| a plain Square, a plain Widget | none                                  | silent    |

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
present, so the `sideways` category above was a divergence until `da8cf69`
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
that has nothing to do with extension schemas. `188946e` closed that, and a
text-free page now agrees too. The line stays because it is what makes both
tools report `7.2/34` once at PDF/UA-1: without it both go quiet, and the
fixture covers one cell less.

`6.7.8/6` stays unreached, and so does the wrong-prefix half of every test.
The first is a litho gap, described under
[Known divergences](#known-divergences). The second is not: `c104d24` brought
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
`0bb0f82` dropped them. The fixture now reports `6.7.3/2`, `/3` and `/5` at
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
`188946e` made a text item a precondition. Both tools now stay quiet here,
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

## Closed since, not planted

Six constructs were divergences when the fixtures around them were built. Each
now agrees, so each is a coverage opportunity rather than a gap. None is in a
committed fixture yet.

**`6.7.8/6`, the property container.** `pdfaSchema:property` holding an
`rdf:Bag` or a bare value instead of an `rdf:Seq`. `817080d` wired the rule,
which the container check had named as validated elsewhere while nothing
validated it. An `rdf:Alt` there is accepted by both tools; until that commit it
also made litho report the entry's schema, namespaceURI and prefix as missing,
because an unrecognised container left its children attributed to the enclosing
schemas container. The part-2 spelling is `6.6.2.3.3/5`.

**`6.7.8/9` and `6.7.8/18`, a value type nothing defines.** `fa8e408`
implemented `isValueTypeDefined` against the vocabulary veraPDF predefines: 26
types for part 1 and 36 for parts 2 and 3, so `Colorant` and `Timecode` are
undefined under part 1 and defined under the later parts. A type the same schema
entry declares resolves in either order; one declared by a different entry
resolves for neither. The part-2 spellings are `6.6.2.3.3/8` and `6.6.2.3.3/17`.

**`8.2.5.26/1`, a table cell intersection at PDF/UA-2.** `8d6c3ab` widened the
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
set on eleven of twelve runs and the other's on the twelfth. `c104d24` narrowed
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
row's last cell already carries down. `d61d0ba` implemented the check, and both
tools now report `7.2/15` twice on that grid and nothing on two conforming span
layouts beside it.

**`7.2/2`, an outline entry with its own `/Lang`.** veraPDF reads
`gContainsCatalogLang == true` on `PDOutline` and offers no per-entry escape,
which matches ISO 32000-1 Table 153: an outline item dictionary defines no
`/Lang` entry. `walkOutlineItemsForLang` in
`pkg/internal/compliance/accessibility_lang.go` exempted an item carrying one
anyway. `2c3be3f` removed the exemption. Two files differing only in that entry
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

## Scope

The runner's `compliance` feature compares verdicts, not rule sets, so this
corpus scores 176/176 matched on verdict alone. The rule-set agreement above is
the property the fixtures actually protect, and it is verified by hand against
a live veraPDF:

```bash
go build -o /tmp/litho ./cmd/litho
/tmp/litho validate pdfs/complex_2b.pdf -profile "PDF/A-2b" -f json
verapdf -f 2b --format json --maxfailuresdisplayed -1 pdfs/complex_2b.pdf
```

Rerun that comparison whenever a compliance rule changes. A new class appearing
on one side only is either a fix or a regression, and this corpus is where it
shows up first.
