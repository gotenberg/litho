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

| Fixture              | Home profile | Focus                                                          |
| -------------------- | ------------ | -------------------------------------------------------------- |
| -------------------- | ------------ | -------------------------------------------------------------- |
| `complex_1b.pdf`     | PDF/A-1b     | Part-1 prohibitions: transparency, embedded files, OCG         |
| `complex_2a.pdf`     | PDF/A-2a     | Level-A tagging: role map, structure hierarchy, language       |
| `complex_2b.pdf`     | PDF/A-2b     | Broadest mix; annotations, optional content, filters           |
| `complex_2u.pdf`     | PDF/A-2u     | Level-U Unicode: absent, forbidden and Private Use mappings    |
| `complex_3b.pdf`     | PDF/A-3b     | Embedded and associated files, one 6.8 test per attachment     |
| `complex_4.pdf`      | PDF/A-4      | Part-4 and PDF 2.0: rev 2020, docinfo, Requirements, PresSteps |
| `complex_ua1.pdf`    | PDF/UA-1     | Structure and accessibility defects in a wired tag tree        |
| `complex_ua2.pdf`    | PDF/UA-2     | Part-2 specifics: namespaces, PUA text, structure destinations |
| `forbidden_mix.pdf`  | PDF/A-2b     | Forbidden actions, PostScript tint transforms, LZW             |
| `gstate_2b.pdf`      | PDF/A-2b     | Transfer functions and halftones in six graphics states        |
| `limits_2b.pdf`      | PDF/A-2b     | Implementation limits: string, name, nesting, DeviceN          |
| `megafont_2b.pdf`    | PDF/A-2b     | Font embedding and glyph-width scoping                         |
| `multipage_2b.pdf`   | PDF/A-2b     | Three pages, a violation class unique to each                  |
| `fontdict_2b.pdf`    | PDF/A-2b     | Font dictionary required keys; page box implementation limits  |
| `structnest_ua1.pdf` | PDF/UA-1     | Clause 7.2: table, list and TOC nesting; per-element language  |
| `tables_ua1.pdf`     | PDF/UA-1     | Tables and lists: header association, nested list structure    |
| `tablespan_ua2.pdf`  | PDF/UA-2     | Table cell geometry: row and column spans                      |
| `extschema_2b.pdf`   | PDF/A-2b     | XMP extension schema structure across sibling entries          |
| `trailer_2b.pdf`     | PDF/A-2b     | Encrypted, with data after the last `%%EOF`                    |

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
| `gstate_2b`                               | Sixth graphics state: `/TR2 /Default`, unnamed type 1 halftone         | ISO 19005-2, 6.2.5           |
| `tablespan_ua2`                           | Second table spanning two columns with both headers referenced         | ISO 14289-2, 8.2.5.26        |

Verified counts at the home profile: `6.2.11.5` fires exactly once on
`complex_2b` (the rendered `T3A` glyph, not the unrendered `T3B` one) on both
litho and veraPDF, and `6.3.3` fires exactly three times, skipping the
zero-area annotation.

## Expected firing sets

Counts are distinct rule classes (`clause/test`) reported by litho. litho and
veraPDF agree exactly on the set in every cell except the two marked `*`,
which are covered under [Known divergences](#known-divergences).

| Fixture              | A-1b | A-2a | A-2b | A-2u | A-3b | A-4 | UA-1 | UA-2 |
| -------------------- | ---- | ---- | ---- | ---- | ---- | --- | ---- | ---- |
| `complex_1b.pdf`     | 23   | 22   | 19   | 20   | 21   | 19  | 14   | 11   |
| `complex_2a.pdf`     | 15   | 21   | 17   | 17   | 18   | 17  | 12   | 11   |
| `complex_2b.pdf`     | 18   | 23   | 20   | 21   | 22   | 19  | 14   | 12   |
| `complex_2u.pdf`     | 15   | 23   | 18   | 20   | 18   | 16  | 14   | 13   |
| `complex_3b.pdf`     | 17   | 23   | 20   | 21   | 21   | 21  | 14   | 10   |
| `complex_4.pdf`      | 18   | 25   | 23   | 23   | 25   | 23  | 14   | 9    |
| `complex_ua1.pdf`    | 6    | 7    | 6    | 6    | 6    | 5   | 11   | 10   |
| `complex_ua2.pdf`    | 8    | 16   | 12   | 12   | 12   | 8   | 17\* | 18\* |
| `extschema_2b.pdf`   | 9    | 11   | 8    | 9    | 9    | 6   | 8    | 8    |
| `fontdict_2b.pdf`    | 7    | 10   | 8    | 8    | 8    | 8   | 7    | 7    |
| `forbidden_mix.pdf`  | 12   | 14   | 12   | 12   | 14   | 14  | 11   | 9    |
| `gstate_2b.pdf`      | 6    | 12   | 10   | 10   | 10   | 10  | 7    | 7    |
| `limits_2b.pdf`      | 7    | 11   | 9    | 9    | 9    | 6   | 10   | 8    |
| `megafont_2b.pdf`    | 9    | 13   | 10   | 11   | 10   | 10  | 12   | 11   |
| `multipage_2b.pdf`   | 10   | 13   | 11   | 11   | 11   | 10  | 10   | 9    |
| `structnest_ua1.pdf` | 4    | 4    | 4    | 4    | 4    | 3   | 36   | 30   |
| `tables_ua1.pdf`     | 5    | 5    | 5    | 5    | 5    | 4   | 2    | 5    |
| `tablespan_ua2.pdf`  | 3    | 4    | 4    | 4    | 4    | 3   | 4    | 4    |
| `trailer_2b.pdf`     | 5    | 7    | 5    | 5    | 5    | 6   | 7    | 7    |

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

Two tests stay unreached, and neither is a fixture shortcoming:

- `7.2/41` is unreachable under PDF/UA-1. veraPDF sets
  `numberOfColumnWithWrongRowSpan` in two places. The first fires when a cell's
  `/RowSpan` runs past the last row, but the row count is itself computed as
  the maximum of `row index + /RowSpan` over every cell, so the condition can
  never hold. The second is a `/RowSpan` crossing a `THead`, `TBody` or
  `TFoot` boundary, and it is guarded by `isPDFUA2RelatedFlavour`, so a
  PDF/UA-1 run never evaluates it.
- `7.2/15` is reachable but litho does not implement it, so planting it would
  add a disagreeing cell rather than coverage. See
  [Known divergences](#known-divergences).

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
| annotation `/Contents` of 40000 bytes   | `6.1.13/3` |
| resource key name longer than 127 bytes | `6.1.13/4` |
| content nesting `q` 30 deep             | `6.1.13/8` |
| DeviceN naming 33 colorants             | `6.1.13/9` |

Four tests of the clause stay uncovered on purpose. `t7` needs more than
8388607 indirect objects and `t10` a CMap with an out-of-range CID, neither of
which belongs in a small fixture. `t1`, `t2` and `t5` need numbers a writer
will not emit: pikepdf normalises a denormal such as `1e-40` to plain `0`,
which `t5` explicitly permits, so it cannot be planted through these builders
at all.

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
position, which a `/RowSpan` cannot produce: under the table model a spanning
cell shifts the following row's cells rather than colliding with them, and
veraPDF agrees the result is not an intersection. `t2` and `t4` need span
geometries these builders could not produce, and `t5`/`t6` are the header
association `tables_ua1.pdf` already covers.

## Known divergences

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
remains is a separate facet of the same rule, described below.

**`6.6.2.3.3/9` on `extschema_2b.pdf`**, and its part-1 spelling `6.7.8/10`.
veraPDF reports a property whose `pdfaProperty:category` is neither
`external` nor `internal`; litho checks only that the field is present, never
its value. A litho gap, under repair.

Two more litho gaps surfaced while building `structnest_ua1.pdf`. Neither is
in a committed fixture, because planting either would add a disagreeing cell
rather than coverage. Both are worth a fixture the moment litho implements
them.

**`7.2/15`, a table cell intersection.** veraPDF sizes the grid, then places
every cell and marks an intersection when two cells claim the same slot. A
first row of `TH(RowSpan 2)`, `TH`, `TH(RowSpan 2)` over a second row holding
one `TH(ColSpan 2)` puts that cell's second column on a slot the first row's
last cell already carries down. veraPDF reports `7.2/15`; litho reports
nothing, and `Rule_14289_1_7_2_t15` is referenced nowhere outside `rules.go`.

**`7.2/2`, an outline entry with its own `/Lang`.** veraPDF reads
`gContainsCatalogLang == true` on `PDOutline` and offers no per-entry escape,
which matches ISO 32000-1 Table 153: an outline item dictionary defines no
`/Lang` entry. `walkOutlineItemsForLang` in
`pkg/internal/compliance/accessibility_lang.go` exempts an item carrying one
anyway. Two files differing only in that entry make it visible: with it,
veraPDF reports `7.2/2` and litho reports nothing; without it, both report it.
`structnest_ua1.pdf` therefore uses a plain outline item, which both tools
agree on.

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
corpus scores 40/40 matched on verdict alone. The rule-set agreement above is
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
