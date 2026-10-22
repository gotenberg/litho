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
├── manifest.json   Corpus manifest (profile_mode=expand over 13 profiles)
├── text-overrides.json  Corrections to the PDF.js text goldens, with reasons
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
| `pagelabels_2b.pdf`    | PDF/A-2b     | Page labels in every numbering style, under the page edits       |
| `annotshare_ua1.pdf`   | PDF/UA-1     | Annotation objects on two pages; XMP scoping; short page boxes   |
| `structcopy_ua2.pdf`   | PDF/UA-2     | Every part of a structure tree a copy of its pages carries       |
| `noresources_4.pdf`    | PDF/A-4      | Page content with no resource dictionary, its own or inherited   |
| `outlinedest_ua2.pdf`  | PDF/UA-2     | Outline structure destinations in every form; named OC configs   |
| `pageboxes_2b.pdf`     | PDF/A-2b     | Page boxes that are not rectangles, under inherited crop boxes   |
| `rolemapscope_ua2.pdf` | PDF/UA-2     | Private types the root `/RoleMap` maps, per declared namespace   |
| `destforms_ua2.pdf`    | PDF/UA-2     | Named destinations, `/Dest` dictionaries, additional actions     |
| `rolemapns_ua2.pdf`    | PDF/UA-2     | Mapped private types in namespaces the root `/RoleMap` misses    |
| `viewerboxes_2b.pdf`   | PDF/A-2b     | Boxes veraPDF passes and no viewer uses, under the rewrites      |
| `gotochain_ua2.pdf`    | PDF/UA-2     | GoTo actions run through `/Next`: arrays, a loop, a shared one   |
| `emptyfig_ua2.pdf`     | PDF/UA-2     | Figures that enclose nothing, and every reference to them        |
| `sharedk_ua1.pdf`      | PDF/UA-1     | A structure `/K` array two elements share, direct kids in it     |
| `fieldkids_2b.pdf`     | PDF/A-2b     | Form fields whose `/Kids` arrays are objects of their own        |
| `boxorigin_2b.pdf`     | PDF/A-2b     | Crop boxes whose lower-left corner is not the user-space origin  |
| `treekids_ua1.pdf`     | PDF/UA-1     | Structure trees whose `/Kids` arrays are objects of their own    |
| `directfig_ua2.pdf`    | PDF/UA-2     | Figures written directly in a structure `/K`, under remediation  |
| `inlinekids_ua2.pdf`   | PDF/UA-2     | Figures of their own that only inline elements list              |
| `fontembed_2b.pdf`     | PDF/A-2b     | Unembedded fonts a supplied program embeds, wherever they sit    |
| `defaultbox_2b.pdf`    | PDF/A-2b     | Pages missing their media box in an A4 document, under rewrites  |
| `siglock_2b.pdf`       | PDF/A-2b     | Unsigned signature fields carrying a `/Lock` or seed values      |
| `cmapusecmap_2b.pdf`   | PDF/A-2b     | Encoding CMaps whose usecmap base maps the codes they show       |
| `cmapusestream_2b.pdf` | PDF/A-2b     | An encoding CMap built on an embedded `/UseCMap` base            |
| `cmapnotdef_2b.pdf`    | PDF/A-2b     | Encoding CMaps whose notdef ranges map the codes they show       |
| `cmappredef_2b.pdf`    | PDF/A-2b     | Fonts naming predefined CMaps, through their bases and notdefs   |
| `cmapusevert_1b.pdf`   | PDF/A-1b     | An encoding CMap whose `/UseCMap` names the vertical CMap V      |
| `cmapverabase_2b.pdf`  | PDF/A-2b     | Encoding CMaps only veraPDF's bundle maps, as bases and by name  |
| `cmapcollect_1b.pdf`   | PDF/A-1b     | A font whose `/Encoding` names a collection CMap, Adobe-Korea1-2 |
| `cmapcidtext_2b.pdf`   | PDF/A-2b     | Text of codes an encoding CMap sends to other CIDs, no ToUnicode |
| `stdremap_ua2.pdf`     | PDF/UA-2     | A standard type the root `/RoleMap` sends to a private dead end  |
| `remapchain_ua2.pdf`   | PDF/UA-2     | Standard types mapped through a private type to a standard one   |
| `imagecolour_2b.pdf`   | PDF/A-2b     | Images over the colorant None; JPX CIELab, ROMM-RGB, e-sRGB      |
| `redactrefs_ua1.pdf`   | PDF/UA-1     | Copies of page text behind objects a redaction cannot read       |
| `redactcopies_2b.pdf`  | PDF/A-2b     | Copies of page text no decoded value spells                      |
| `redactvectors_4.pdf`  | PDF/A-4      | Copies of page text no comparison of bytes finds                 |
| `rolecycle_ua1.pdf`    | PDF/UA-1     | A standard type in a `/RoleMap` cycle, and a type mapped into it |
| `rolecycle_ua2.pdf`    | PDF/UA-2     | Role-map cycles through the root `/RoleMap` and namespaces       |
| `type0nodesc_2b.pdf`   | PDF/A-2b     | Type 0 fonts whose `/DescendantFonts` holds no dictionary        |
| `fnlimits_1b.pdf`      | PDF/A-1b     | Type 4 functions past the limits, reached and unreached          |
| `oirgb_2b.pdf`         | PDF/A-2b     | Device colour images under a non-sRGB RGB output intent          |
| `ocgconfigs_2b.pdf`    | PDF/A-2b     | An optional content `/Order` incomplete in `/Configs` alone      |
| `ttmaccopy_2b.pdf`     | PDF/A-2b     | Symbolic TrueType whose (1,0) `cmap` copies (3,1), and a control |
| `nestedpattern_2b.pdf` | PDF/A-2b     | Names a pattern cell or a form's `scn` finds only on the page    |
| `symbolname_2u.pdf`    | PDF/A-2u     | A font whose name holds "Symbol", and a Symbol-only `/CharSet`   |
| `fontunread_2b.pdf`    | PDF/A-2b     | Font programs in their container that no parser can finish       |
| `type1notdef_2u.pdf`   | PDF/A-2u     | Codes a Type 1 encoding leaves to a program naming them .notdef  |
| `fontnohead_2b.pdf`    | PDF/A-2b     | TrueType programs with no `head` table, weighed at 2048 units    |
| `fontemptycs_2b.pdf`   | PDF/A-2b     | CFF programs whose CharStrings INDEX holds no glyph              |
| `fontcff2_2b.pdf`      | PDF/A-2b     | OpenType programs, `CFF2` among them, read by the embedding font |
| `fontotread_2b.pdf`    | PDF/A-2b     | OpenType programs the glyph rules read by the embedding font     |
| `aakeys_4.pdf`         | PDF/A-4      | Catalog and page `/AA` holding a permitted and a forbidden key   |
| `patternshade_2b.pdf`  | PDF/A-2b     | Shadings a tiling cell or a Type 3 glyph reaches                 |
| `patternglyph_2b.pdf`  | PDF/A-2b     | Type 3 glyphs a tiling cell shows, nested once                   |
| `shadedefault_2b.pdf`  | PDF/A-2b     | Shading patterns over DeviceCMYK beside DeviceRGB, CMYK intent   |
| `type3names_2u.pdf`    | PDF/A-2u     | Type 3 fonts with no `/ToUnicode`, glyph names their one meaning |
| `realglyphs_ua1.pdf`   | PDF/UA-1     | One Type 3 glyph shown in real content and beside it, per page   |
| `type3cycle_2u.pdf`    | PDF/A-2u     | A Type 3 font shown again in a cell its own glyph selects        |
| `sizetie_2b.pdf`       | PDF/A-2b     | Two font sizes of equal weight, under bookmark generation        |
| `iccv4intent_1b.pdf`   | PDF/A-1b     | An output intent's ICC v4 destination profile at PDF/A-1         |
| `iccv4gray_1b.pdf`     | PDF/A-1b     | A grey ICC v4 destination profile whose tags need v2 re-encoding |
| `iccv4space_1b.pdf`    | PDF/A-1b     | ICCBased spaces of a page and an image sharing an ICC v4 profile |
| `drivetitle_1b.pdf`    | PDF/A-1b     | Info text entries that begin like a PDF date, against XMP        |
| `unclosedmc_ua1.pdf`   | PDF/UA-1     | An artifact sequence the content stream never closes             |
| `danglingstr_ua1.pdf`  | PDF/UA-1     | Auto-tagging a page that ends inside an unterminated string      |
| `readorder_ua1.pdf`    | PDF/UA-1     | Untagged two-column page whose stream interleaves the columns    |
| `readcolumns_ua1.pdf`  | PDF/UA-1     | Columns set line by line; a running head in the title's object   |
| `prosecolumns_ua1.pdf` | PDF/UA-1     | Prose columns on shared baselines; a borderless table control    |
| `filledrules_ua1.pdf`  | PDF/UA-1     | A table ruled by filled hairlines; prose under a barred title    |
| `sharedicc_2b.pdf`     | PDF/A-2b     | One content stream four pages share, each over its own ICC space |
| `texlig_2b.pdf`        | PDF/A-2b     | A TeX ligature code and a code 32 whose names map nothing        |
| `qnestreach_2b.pdf`    | PDF/A-2b     | q/Q nesting past 28 in a form, a cell, a glyph and an appearance |
| `patternreach_2b.pdf`  | PDF/A-2b     | DeviceRGB a pattern reaches, one path, cycle or sharing per page |
| `iccbadgray_1b.pdf`    | PDF/A-1b     | A grey output intent whose ICC profile states version 5.0        |
| `iccbadgray_2b.pdf`    | PDF/A-2b     | A direct grey output intent over a scanner-class ICC profile     |
| `iccbadcmyk_1b.pdf`    | PDF/A-1b     | A CMYK output intent whose ICC profile states version 5.0        |
| `iccbadcmyk_2b.pdf`    | PDF/A-2b     | Two output intents sharing one scanner-class CMYK ICC profile    |
| `refreach_ua1.pdf`     | PDF/UA-1     | Reference XObjects a cell, glyph, page or appearance draws       |
| `inlinelzw_2b.pdf`     | PDF/A-2b     | Inline images with a forbidden filter in every stream parsed     |
| `headingnest_ua2.pdf`  | PDF/UA-2     | Generic `H` headings nested three `Sect` elements deep           |
| `twoprofile_1b.pdf`    | PDF/A-1b     | A PDF/X intent before the PDF/A one, each over its own profile   |
| `formreuse_ua1.pdf`    | PDF/UA-1     | Marked forms a cell, glyph, form or appearance references again  |
| `formshare_ua1.pdf`    | PDF/UA-1     | Marked forms a content stream two pages share draws once         |
| `inlinejpx_2b.pdf`     | PDF/A-2b     | Inline images applying JPXDecode or JBIG2Decode                  |
| `headingart_ua2.pdf`   | PDF/UA-2     | Generic `H` headings nested in `Art` and `Sect` elements         |
| `sharedtag_ua1.pdf`    | PDF/UA-1     | Untagged text and a marked form two pages' shared content draws  |
| `sharedclose_ua1.pdf`  | PDF/UA-1     | A marked form and an open artifact two pages' shared content has |
| `pdfxonly_2b.pdf`      | PDF/A-2b     | A PDF/X output intent and no PDF/A one, over CMYK and RGB paint  |
| `pdfxnoprofile_2b.pdf` | PDF/A-2b     | A PDF/A intent naming no profile beside a PDF/X CMYK intent      |
| `qshyphen_2b.pdf`      | PDF/A-2b     | Soft hyphens shown inside `q`...`Q` within a text object         |
| `sharedarray_2b.pdf`   | PDF/A-2b     | Direct /Contents arrays sharing a stream, beside an invalid `ri` |
| `sharedstream_2b.pdf`  | PDF/A-2b     | One stream two pages read through a simple and a Type0 font      |
| `ttnoenc_2b.pdf`       | PDF/A-2b     | Embedded TrueType fonts carrying no `/Encoding`, by `cmap` set   |
| `sharednested_2b.pdf`  | PDF/A-2b     | Forms and a pattern two pages share and bind alike or apart      |
| `transparency_2b.pdf`  | PDF/A-2b     | Knockout groups, luminosity masks with /BC and /TR, blend modes  |
| `annotsynth_2b.pdf`    | PDF/A-2b     | Polygon, Underline and Squiggly annotations with no `/AP`        |
| `recolorimg_2b.pdf`    | PDF/A-2b     | 16-bit, Lab, colour-key, matted and /Decode images to recolour   |
| `tounicodepair_2u.pdf` | PDF/A-2u     | A simple font's two-byte /ToUnicode mapping a shown byte pair    |
| `flattentext_2b.pdf`   | PDF/A-2b     | Transparency over, under and on text, which flattening keeps     |
| `blendspace_2b.pdf`    | PDF/A-2b     | Blend modes, knockout and a soft mask in CMYK and grey groups    |
| `flattenannots_2b.pdf` | PDF/A-2b     | Transparent annotations, which flattening draws into the page    |
| `flattenrefuse_1b.pdf` | PDF/A-1b     | Transparency on a page flattening refuses, beside one it takes   |
| `iccalternate_2b.pdf`  | PDF/A-2b     | ICCBased spaces no engine can use, read as their CIE /Alternate  |
| `formplacement_2b.pdf` | PDF/A-2b     | Widget appearances a viewer places by Algorithm 8.1, not a shift |
| `formuntagged_ua1.pdf` | PDF/UA-1     | Painting widgets in a document with no structure tree            |
| `smaskindata_2b.pdf`   | PDF/A-2b     | JPEG 2000 images whose data holds their soft mask, both kinds    |
| `flattentiles_1b.pdf`  | PDF/A-1b     | Transparency over a photograph and a chart, composited in tiles  |
| `iccgroupalt_2b.pdf`   | PDF/A-2b     | Groups and transparent text in unusable ICC spaces' /Alternate   |
| `flatuntagged_ua1.pdf` | PDF/UA-1     | Transparent annotations in a document with no structure tree     |

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
| `annotshare_ua1`                          | `pdfuaid:part` 1, its namespace declared on the property element       | ISO 14289-1, 5 t2            |
| `annotshare_ua1`                          | A Link beyond the media box its page inherits from the page tree       | ISO 14289-1, 7.18.5          |
| `annotshare_ua1`                          | A Link above a crop box of three numbers, which has no height          | ISO 14289-1, 7.18.5          |
| `annotshare_ua1`                          | An extension schema whose every element declares its namespace         | ISO 19005-2, 6.6.2.3.1       |
| `annots_1b`                               | A plain square and a plain widget, both with `/F 4` and an `/N` stream | ISO 19005-1, 6.5.3           |
| `annots_1b`                               | An appearance dict carrying `/D` beside an `/N` of the wrong type      | ISO 19005-1, 6.5.3 t5, t6    |
| `fontkeys_1b`                             | A Helvetica with no `/FirstChar`, `/LastChar` or `/Widths`             | ISO 19005-1, 6.3.2 t4-t6     |
| `cmaps_2b`                                | A font naming `/Identity-H`, and one embedding a self-consistent CMap  | ISO 19005-2, 6.2.11.3.3      |
| `outputintent_2b`                         | A well-formed `prtr`/`RGB ` destination profile beside the bad one     | ISO 19005-2, 6.2.3 t1        |
| `formstruct_ua2`                          | A `Form` element enclosing exactly one widget                          | ISO 14289-2, 8.10.1 t2       |
| `formstruct_ua2`                          | A widget with no `/StructParent`, outside the structure tree           | ISO 14289-2, 8.10.1 t1       |
| `structcopy_ua2`                          | A root `/RoleMap`, `/ClassMap`, `/IDTree` and two `/Namespaces`        | ISO 14289-2, 8.2             |
| `outlinedest_ua2`                         | Outline items opening structure destinations in four forms             | ISO 14289-2, 8.8             |
| `outlinedest_ua2`                         | Two optional content configurations, each named and ordering both      | ISO 14289-2, 8.7             |
| `rolemapscope_ua2`                        | A `Para` in the PDF 2.0 namespace, which the PDF 1.7 reading maps      | ISO 14289-1, 7.1 t5          |
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
| `aakeys_4`                                | `/E` and `/X`, permitted keys beside a forbidden one in the same `/AA` | ISO 19005-4, 6.6.3 t1        |
| `aakeys_4`                                | A page `/AA` holding `/D` alone                                        | ISO 19005-4, 6.6.3 t1        |
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
| `noresources_4`                           | Page 5, showing the content stream page 4 has judged already           | ISO 19005-4, 6.2.2 t3        |
| `noresources_4`                           | Page 6, whose content paints nothing under an empty `/Resources`       | ISO 32000-2, Table 31        |
| `noresources_4`                           | Page 2's annotation appearance, which states no `/Resources` either    | ISO 19005-4, 6.2.2 t3        |
| `gotochain_ua2`                           | A GoTo action run through `/Next` whose `/SD` names page 1's heading   | ISO 14289-2, 8.8 t2          |
| `gotochain_ua2`                           | The GoTo action two chains share, a second time                        | one action per object        |
| `sharedk_ua1`                             | The Figure the two Sects' shared `/K` array holds, a second time       | one element per array read   |
| `cmapusecmap_2b`                          | `U2`: codes an `/Identity-H usecmap` base maps to glyphs present       | ISO 32000-1, 9.7.5.4         |
| `cmapusestream_2b`                        | `S1`: a code only the embedded `/UseCMap` base maps, to a glyph        | ISO 32000-1, 9.7.5.3         |
| `cmapnotdef_2b`                           | `N2`: a code a notdef range maps to a CID the program holds            | ISO 32000-1, 9.7.6.3         |
| `cmappredef_2b`                           | `P2`: codes `/UniJIS-UCS2-V` takes from its base, to glyphs present    | ISO 32000-1, 9.7.5.2         |
| `cmappredef_2b`                           | `P5`: a code `/90ms-RKSJ-H`'s notdef range maps to a CID present       | ISO 32000-1, 9.7.6.3         |
| `cmapverabase_2b`                         | `K2`: codes an `/Adobe-KR-9 usecmap` base maps to glyphs present       | ISO 32000-1, 9.7.5.4         |
| `cmapverabase_2b`                         | `K6`: U+2002, which veraPDF's `/UniGB-UTF16-H` maps to a CID present   | ISO 32000-1, 9.7.5.2         |
| `recolorimg_2b`                           | `Im16`, an ICCBased image at 16 bits a component                       | ISO 19005-2, 6.2.8           |
| `recolorimg_2b`                           | `ImKey`, an image with a colour-key `/Mask` array                      | ISO 19005-2, 6.2.8           |
| `remapchain_ua2`                          | `Div`: a `/RoleMap` chain through `Section` that ends on `Sect`        | ISO 14289-2, 8.2.4 t4        |
| `remapchain_ua2`                          | PDF 1.7 `Sect`: a `/RoleMapNS` chain through a custom `Section`        | ISO 14289-2, 8.2.4 t4        |
| `stdremap_ua2`                            | PDF 2.0 `Sect`: an element the `/RoleMap` entry for `Sect` misses      | ISO 32000-2, 14.8.6          |
| `rolecycle_ua1`                           | `Item` at PDF/A-2a: mapped into a cycle, its chain stops on `Sect`     | ISO 19005-2, 6.7.3.4 t2      |
| `rolecycle_ua2`                           | A PDF 2.0 `/RoleMapNS` self-map of `Note`, which no element uses       | ISO 14289-2, 8.2.4 t2        |
| `rolecycle_ua2`                           | `Panel`: a `/RoleMapNS` cycle that never leaves its namespace          | ISO 14289-2, 8.2.4 t2        |
| `type0nodesc_2b`                          | `C1`: a Type 0 font over a CIDFontType2 dictionary                     | ISO 32000-1, Table 121       |
| `fnlimits_1b`                             | `C`: a type 4 function past both limits as a shading's `/Function`     | GFPDShading, veraPDF 1.30    |
| `fnlimits_1b`                             | `F`: a type 4 function past both limits behind an unused Separation    | ISO 19005-1, 6.1.12          |
| `fnlimits_1b`                             | `/Two`: a direct stitching tint transform compared with itself         | ISO 19005-2, 6.2.4.4 t2      |
| `ocgconfigs_2b`                           | `/D` and `/Configs[1]`: an `/Order` naming both OCGs, one in a group   | ISO 19005-2, 6.9 t3          |
| `nestedpattern_2b`                        | `P2`: a tiling pattern holding the `/GS0` its cell sets                | ISO 19005-2, 6.2.2 t2        |
| `nestedpattern_2b`                        | `X1`: a form XObject holding the `/P3` its `scn` selects               | ISO 19005-2, 6.2.2 t2        |
| `fontunread_2b`                           | `CN`: a CFF whose Name INDEX is empty, which still reads               | ISO 19005-2, 6.2.11.4.1 t1   |
| `fontunread_2b`                           | `OK`, `TK`: the whole programs `U1`, `U2`, `T2` and `T3` are cut from  | ISO 19005-2, 6.2.11.4.1 t1   |
| `type1notdef_2u`                          | `NC`: codes the program's encoding vector names `A` and `B`            | ISO 19005-2, 6.2.11.7.2      |
| `fontnohead_2b`                           | `HK`: `H1`'s program with a `head` stating 2048 units                  | ISO 19005-2, 6.2.11.5        |
| `fontnohead_2b`                           | `HO`: an `OTTO` with `CFF ` outlines and no `head`, at the CFF's scale | ISO 19005-2, 6.2.11.5        |
| `fontemptycs_2b`                          | `EK`, `CK`: the whole programs the empty ones are cut from             | ISO 19005-2, 6.2.11.4.1 t2   |
| `fontcff2_2b`                             | `F3`, `F4`: a `CFF2` program a TrueType font reads through `hmtx`      | ISO 19005-2, 6.2.11.4.1 t1   |
| `fontcff2_2b`                             | `OK`: a Type 1 font over an `OTTO` with `CFF ` outlines                | ISO 19005-2, 6.2.11.4.1 t1   |
| `fontotread_2b`                           | `W1`, `W3`, `W4`: CFF fonts declaring the `CFF ` widths, not `hmtx`'s  | ISO 19005-2, 6.2.11.5        |
| `fontotread_2b`                           | `W5`, `H1`: TrueType fonts over an `OTTO` declaring `hmtx`'s widths    | ISO 19005-2, 6.2.11.5        |
| `patternshade_2b`                         | every shading's `/Function`: a type 4 program past both number limits  | ISO 19005-2, 6.1.13 t1       |
| `patternshade_2b`                         | `SH1`'s own `/ExtGState`, setting `/TR /Identity` and `/BM /Foo`       | ISO 19005-2, 6.2.5, 6.2.10   |
| `patternglyph_2b`                         | page 4: a Type 3 font a cell selects with `Tf` and shows nothing in    | ISO 19005-2, 6.2.6 t1        |
| `qnestreach_2b`                           | page 5: page 1's 29-level form painted again                           | one judgement per stream     |
| `qnestreach_2b`                           | page 6: a page 20 levels deep painting a form 15 levels deep           | ISO 19005-2, 6.1.13 t8       |
| `sharedicc_2b`                            | page 3: an ICC version 4 display profile, at PDF/A-2b                  | ISO 19005-2, 6.2.4.2         |
| `sharedicc_2b`                            | page 4: the shared stream under page 1's resources, a second time      | one reading per resource set |
| `refreach_ua1`                            | page 6: a reference XObject a page, a cell and a Type 3 font list      | ISO 14289-1, 7.20 t1         |
| `formreuse_ua1`                           | page 7: a form, a cell and a glyph drawn twice, each drawing one form  | ISO 14289-1, 7.20 t2         |
| `formshare_ua1`                           | every page: a marked form the content two pages share draws once       | ISO 14289-1, 7.20 t2         |
| `sharedtag_ua1`                           | pages 1, 2: a marked form shared content draws once, before and after  | ISO 14289-1, 7.20 t2         |
| `sharedclose_ua1`                         | pages 1, 2: a marked form shared content draws once, before and after  | ISO 14289-1, 7.20 t2         |
| `ttnoenc_2b`                              | `N3110`, `N10`, `N30`: no `/Encoding`, a (1,0) or (3,0) subtable       | ISO 19005-2, 6.2.11.4.1 t2   |

Verified counts at the home profile: `6.2.11.5` fires exactly once on
`complex_2b` (the rendered `T3A` glyph, not the unrendered `T3B` one) on both
litho and veraPDF, and `6.3.3` fires exactly three times, skipping the
zero-area annotation.

## Expected firing sets

Counts are distinct rule classes (`clause/test`) reported by litho. litho and
veraPDF agree exactly on the set in every cell except those marked `*`, which
are covered under [Known divergences](#known-divergences).

| Fixture                | A-1a | A-1b | A-2a | A-2b | A-2u | A-3a | A-3b | A-3u | A-4 | A-4e | A-4f | UA-1 | UA-2 |
| ---------------------- | ---- | ---- | ---- | ---- | ---- | ---- | ---- | ---- | --- | ---- | ---- | ---- | ---- |
| `a4syntax_4.pdf`       | 17   | 15   | 21   | 19   | 19   | 21   | 19   | 19   | 16  | 17   | 18   | 13   | 8    |
| `aakeys_4.pdf`         | 4    | 2    | 6    | 4    | 4    | 6    | 4    | 4    | 2   | 2    | 3    | 5    | 6    |
| `afref_3b.pdf`         | 5    | 2    | 5    | 2    | 3    | 5    | 2    | 3    | 5   | 5    | 6    | 10   | 7    |
| `annothidden_2b.pdf`   | 5    | 2    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 11   | 6    |
| `annotkinds_ua2.pdf`   | 7    | 7    | 8    | 8    | 8    | 8    | 8    | 8    | 5   | 5    | 6    | 8    | 10   |
| `annotlang_ua1.pdf`    | 6    | 5    | 6    | 5    | 5    | 6    | 5    | 5    | 4   | 4    | 5    | 1    | 4    |
| `annots_1b.pdf`        | 16   | 14   | 14   | 12   | 12   | 14   | 12   | 12   | 10  | 10   | 11   | 12   | 8    |
| `annotshare_ua1.pdf`   | 6    | 4    | 5    | 4    | 5    | 6    | 5    | 6    | 7   | 7    | 8    | 6    | 4    |
| `annotstruct_ua2.pdf`  | 13   | 12   | 12   | 12   | 12   | 14   | 14   | 14   | 10  | 9    | 9    | 8    | 13   |
| `annotsynth_2b.pdf`    | 8    | 5    | 5    | 2    | 3    | 6    | 3    | 4    | 6   | 6    | 7    | 9    | 7    |
| `artifact_ua1.pdf`     | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 4   | 4    | 5    | 5    | 3    |
| `baretree_2b.pdf`      | 4    | 1    | 3    | 0    | 1    | 4    | 1    | 2    | 4   | 4    | 5    | 7    | 6    |
| `blendcs_4.pdf`        | 7    | 5    | 7    | 5    | 5    | 7    | 5    | 5    | 4   | 4    | 5    | 6    | 6    |
| `blendspace_2b.pdf`    | 9    | 6    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 6    | 6    |
| `boxorigin_2b.pdf`     | 4    | 1    | 3    | 0    | 1    | 4    | 1    | 2    | 4   | 4    | 5    | 5    | 5    |
| `charset_1b.pdf`       | 7    | 4    | 7    | 4    | 5    | 7    | 4    | 5    | 3   | 3    | 4    | 9    | 7    |
| `cmapcidtext_2b.pdf`   | 6    | 3    | 6    | 3    | 4    | 6    | 3    | 4    | 4   | 4    | 5    | 8    | 8    |
| `cmapcollect_1b.pdf`   | 7    | 4    | 7    | 4    | 5    | 7    | 4    | 5    | 5   | 5    | 6    | 9    | 9    |
| `cmapnotdef_2b.pdf`    | 6    | 3    | 7    | 4    | 5    | 7    | 4    | 5    | 5   | 5    | 6    | 9    | 9    |
| `cmappredef_2b.pdf`    | 6    | 4    | 6    | 4    | 4    | 6    | 4    | 4    | 5   | 5    | 6    | 8    | 8    |
| `cmaps_2b.pdf`         | 9    | 6    | 10   | 7    | 8    | 10   | 7    | 8    | 8   | 8    | 9    | 12   | 12   |
| `cmapusecmap_2b.pdf`   | 6    | 3    | 7    | 4    | 5    | 7    | 4    | 5    | 5   | 5    | 6    | 9    | 9    |
| `cmapusestream_2b.pdf` | 5    | 2    | 6    | 3    | 4    | 6    | 3    | 4    | 4   | 4    | 5    | 8    | 8    |
| `cmapusevert_1b.pdf`   | 5    | 3    | 4    | 2    | 2    | 4    | 2    | 2    | 3   | 3    | 4    | 6    | 6    |
| `cmapverabase_2b.pdf`  | 8    | 5    | 10   | 7    | 8    | 10   | 7    | 8    | 8   | 8    | 9    | 12   | 12   |
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
| `danglingstr_ua1.pdf`  | 6    | 4    | 6    | 4    | 4    | 6    | 4    | 4    | 3   | 3    | 4    | 3    | 5    |
| `defaultbox_2b.pdf`    | 4    | 1    | 4    | 1    | 2    | 5    | 2    | 3    | 4   | 4    | 5    | 7    | 6    |
| `defaultcs_4.pdf`      | 9    | 6    | 9    | 7    | 7    | 9    | 7    | 7    | 7   | 7    | 8    | 10   | 7    |
| `destforms_ua2.pdf`    | 5    | 5    | 7    | 7    | 7    | 7    | 7    | 7    | 4   | 4    | 5    | 2    | 2    |
| `directfig_ua2.pdf`    | 4    | 4    | 5    | 5    | 5    | 5    | 5    | 5    | 2   | 2    | 3    | 3    | 1    |
| `directstream_2b.pdf`  | 5    | 2    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 7    | 6    |
| `docinfo_1b.pdf`       | 11   | 8    | 4    | 1    | 2    | 4    | 1    | 2    | 6   | 6    | 7    | 5    | 5    |
| `docmdp_2b.pdf`        | 6    | 4    | 8    | 6    | 6    | 8    | 6    | 6    | 5   | 5    | 6    | 10   | 8    |
| `docsync_1b.pdf`       | 11   | 9    | 6    | 4    | 4    | 6    | 4    | 4    | 6   | 6    | 7    | 9    | 8    |
| `drivetitle_1b.pdf`    | 3    | 0    | 4    | 1    | 2    | 4    | 1    | 2    | 6   | 6    | 7    | 7    | 6    |
| `emptyfig_ua2.pdf`     | 4    | 4    | 5    | 5    | 5    | 5    | 5    | 5    | 2   | 2    | 3    | 3    | 1    |
| `encperm_ua1.pdf`      | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 6    | 2    | 4    |
| `extguards_2b.pdf`     | 9    | 6    | 6    | 3    | 4    | 7    | 4    | 5    | 5   | 5    | 6    | 5    | 5    |
| `extpredef_2b.pdf`     | 7    | 4    | 6    | 3    | 4    | 7    | 4    | 5    | 5   | 5    | 6    | 5    | 5    |
| `extprop_2b.pdf`       | 7    | 4    | 6    | 3    | 4    | 7    | 4    | 5    | 6   | 6    | 7    | 8    | 7    |
| `extschema_2b.pdf`     | 12   | 9    | 11   | 8    | 9    | 12   | 9    | 10   | 6   | 6    | 7    | 8    | 8    |
| `exttypes_2b.pdf`      | 10   | 7    | 9    | 6    | 7    | 10   | 7    | 8    | 5   | 5    | 6    | 5    | 6    |
| `extvalue_2b.pdf`      | 7    | 4    | 6    | 3    | 4    | 7    | 4    | 5    | 5   | 5    | 6    | 5    | 6    |
| `fenote_ua2.pdf`       | 4    | 3    | 5    | 4    | 4    | 5    | 4    | 4    | 3   | 3    | 4    | 5    | 6    |
| `fieldkids_2b.pdf`     | 6    | 3    | 5    | 2    | 3    | 6    | 3    | 4    | 5   | 5    | 6    | 8    | 6    |
| `filestruct_1b.pdf`    | 15   | 13   | 12   | 10   | 10   | 12   | 10   | 10   | 13  | 13   | 14   | 10   | 8    |
| `filledrules_ua1.pdf`  | 7    | 5    | 7    | 5    | 5    | 7    | 5    | 5    | 4   | 4    | 5    | 4    | 6    |
| `flattenannots_2b.pdf` | 12   | 9    | 8    | 5    | 6    | 9    | 6    | 7    | 9   | 9    | 10   | 11   | 8    |
| `flattenrefuse_1b.pdf` | 6    | 3    | 6    | 3    | 4    | 6    | 3    | 4    | 6   | 6    | 7    | 6    | 6    |
| `flattentiles_1b.pdf`  | 6    | 3    | 7    | 4    | 5    | 7    | 4    | 5    | 7   | 7    | 8    | 6    | 6    |
| `flattentext_2b.pdf`   | 11   | 8    | 6    | 3    | 4    | 7    | 4    | 5    | 7   | 7    | 8    | 7    | 6    |
| `flatuntagged_ua1.pdf` | 7    | 5    | 5    | 3    | 3    | 5    | 3    | 3    | 2   | 2    | 3    | 4    | 5    |
| `fnlimits_1b.pdf`      | 6    | 3    | 6    | 3    | 4    | 6    | 3    | 4    | 5   | 5    | 6    | 6    | 6    |
| `fontcff2_2b.pdf`      | 8    | 5    | 7    | 4    | 5    | 7    | 4    | 5    | 5   | 5    | 6    | 9    | 9    |
| `fontclaims_2b.pdf`    | 9    | 6    | 9    | 6    | 7    | 9    | 6    | 7    | 7   | 7    | 8    | 11   | 11   |
| `fontcopies_2b.pdf`    | 14   | 11   | 13   | 10   | 11   | 13   | 10   | 11   | 11  | 11   | 12   | 13   | 13   |
| `fontdict_2b.pdf`      | 9    | 7    | 10   | 8    | 8    | 10   | 8    | 8    | 8   | 8    | 9    | 7    | 7    |
| `fontembed_2b.pdf`     | 6    | 4    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 7    | 7    |
| `fontemptycs_2b.pdf`   | 8    | 5    | 9    | 5    | 7    | 9    | 5    | 7    | 7   | 7    | 8    | 11   | 11   |
| `fontkeys_1b.pdf`      | 13   | 10   | 13   | 10   | 11   | 13   | 10   | 11   | 11  | 11   | 12   | 8    | 8    |
| `fontleft_2b.pdf`      | 7    | 4    | 9    | 5    | 7    | 9    | 5    | 7    | 7   | 7    | 8    | 14   | 13   |
| `fontnohead_2b.pdf`    | 6    | 3    | 5    | 2    | 3    | 5    | 2    | 3    | 3   | 3    | 4    | 7    | 7    |
| `fontotread_2b.pdf`    | 8    | 5    | 8    | 5    | 6    | 8    | 5    | 6    | 6   | 6    | 7    | 10   | 10   |
| `fontunread_2b.pdf`    | 6    | 4    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 7    | 7    |
| `forbidden_mix.pdf`    | 14   | 12   | 14   | 12   | 12   | 16   | 14   | 14   | 14  | 13   | 13   | 11   | 9    |
| `formcs_2b.pdf`        | 6    | 4    | 6    | 4    | 4    | 6    | 4    | 4    | 5   | 5    | 6    | 7    | 7    |
| `formcycle_ua1.pdf`    | 4    | 2    | 4    | 2    | 2    | 4    | 2    | 2    | 3   | 3    | 4    | 6    | 6    |
| `formflat_ua1.pdf`     | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 6   | 6    | 7    | 1    | 3    |
| `formhidden_ua1.pdf`   | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 4   | 4    | 5    | 0    | 5    |
| `formmark_ua1.pdf`     | 4    | 4    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 2    | 3    |
| `formmedia_ua1.pdf`    | 8    | 8    | 8    | 8    | 8    | 8    | 8    | 8    | 7   | 7    | 8    | 7    | 5    |
| `formplacement_2b.pdf` | 2    | 1    | 1    | 0    | 1    | 2    | 1    | 2    | 4   | 4    | 5    | 5    | 7    |
| `formreuse_ua1.pdf`    | 7    | 4    | 6    | 4    | 4    | 6    | 4    | 4    | 3   | 3    | 4    | 10   | 8    |
| `formshare_ua1.pdf`    | 5    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 2   | 2    | 3    | 5    | 7    |
| `formstruct_ua2.pdf`   | 4    | 4    | 6    | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 6    | 7    | 7    |
| `formuntagged_ua1.pdf` | 5    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 2   | 2    | 3    | 4    | 5    |
| `glyphs_2b.pdf`        | 7    | 4    | 8    | 5    | 6    | 8    | 5    | 6    | 6   | 6    | 7    | 10   | 10   |
| `gotochain_ua2.pdf`    | 5    | 5    | 7    | 7    | 7    | 7    | 7    | 7    | 4   | 4    | 5    | 2    | 1    |
| `grouping_ua2.pdf`     | 3    | 3    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 5    | 8    |
| `gstate_2b.pdf`        | 9    | 7    | 13   | 11   | 11   | 13   | 11   | 11   | 10  | 10   | 11   | 7    | 7    |
| `gstateform_1b.pdf`    | 9    | 6    | 8    | 5    | 6    | 8    | 5    | 6    | 9   | 9    | 10   | 8    | 7    |
| `headingart_ua2.pdf`   | 4    | 4    | 5    | 5    | 5    | 5    | 5    | 5    | 2   | 2    | 3    | 2    | 1    |
| `headingnest_ua2.pdf`  | 4    | 4    | 5    | 5    | 5    | 5    | 5    | 5    | 2   | 2    | 3    | 2    | 1    |
| `headings_ua1.pdf`     | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 4   | 4    | 5    | 5    | 5    |
| `iccalternate_2b.pdf`  | 4    | 1    | 3    | 0    | 1    | 4    | 1    | 2    | 4   | 4    | 5    | 6    | 6    |
| `iccbadcmyk_1b.pdf`    | 4    | 1    | 5    | 2    | 3    | 5    | 2    | 3    | 5   | 5    | 6    | 7    | 6    |
| `iccbadcmyk_2b.pdf`    | 5    | 2    | 4    | 1    | 2    | 5    | 2    | 3    | 6   | 6    | 7    | 7    | 6    |
| `iccbadgray_1b.pdf`    | 4    | 1    | 5    | 2    | 3    | 5    | 2    | 3    | 5   | 5    | 6    | 7    | 6    |
| `iccbadgray_2b.pdf`    | 5    | 2    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 7    | 6    |
| `iccbased_4.pdf`       | 5    | 3    | 7    | 5    | 5    | 7    | 5    | 5    | 5   | 5    | 6    | 8    | 7    |
| `iccgroupalt_2b.pdf`   | 10   | 7    | 6    | 3    | 4    | 7    | 4    | 5    | 6   | 6    | 7    | 7    | 6    |
| `iccv4gray_1b.pdf`     | 4    | 1    | 4    | 1    | 2    | 4    | 1    | 2    | 4   | 4    | 5    | 7    | 6    |
| `iccv4intent_1b.pdf`   | 4    | 1    | 4    | 1    | 2    | 4    | 1    | 2    | 4   | 4    | 5    | 7    | 6    |
| `iccv4space_1b.pdf`    | 4    | 1    | 4    | 1    | 2    | 4    | 1    | 2    | 4   | 4    | 5    | 7    | 6    |
| `idfields_ua2.pdf`     | 6    | 6    | 7    | 7    | 7    | 7    | 7    | 7    | 4   | 4    | 5    | 4    | 6    |
| `idprefix_2b.pdf`      | 13   | 10   | 12   | 9    | 10   | 13   | 10   | 11   | 10  | 10   | 11   | 8    | 7    |
| `imagecolour_2b.pdf`   | 4    | 2    | 4    | 2    | 2    | 4    | 2    | 2    | 3   | 3    | 4    | 5    | 6    |
| `imagedraws_2b.pdf`    | 9    | 6    | 9    | 7    | 7    | 9    | 7    | 7    | 8   | 8    | 9    | 10   | 7    |
| `imagereach_2b.pdf`    | 6\*  | 4\*  | 6\*  | 4\*  | 4\*  | 6\*  | 4\*  | 4\*  | 4\* | 4\*  | 5\*  | 5    | 6    |
| `imgxmp_4.pdf`         | 10   | 8    | 15   | 13   | 13   | 15   | 13   | 13   | 8   | 9    | 10   | 7    | 6    |
| `inlinefs_4.pdf`       | 7    | 5    | 9    | 7    | 7    | 11   | 9    | 9    | 4   | 4    | 5    | 10   | 6    |
| `inlineimage_2b.pdf`   | 7    | 4    | 10   | 6    | 7    | 10   | 6    | 7    | 6   | 6    | 7    | 10   | 10   |
| `inlineinterp_2b.pdf`  | 7    | 4    | 6    | 4    | 4    | 6    | 4    | 4    | 5   | 5    | 6    | 10   | 8    |
| `inlinejpx_2b.pdf`     | 5    | 3    | 6    | 4    | 4    | 6    | 4    | 4    | 5   | 5    | 6    | 5    | 6    |
| `inlinekids_ua2.pdf`   | 4    | 4    | 5    | 5    | 5    | 5    | 5    | 5    | 2   | 2    | 3    | 3    | 1    |
| `inlinelzw_2b.pdf`     | 6    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 9    | 7    |
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
| `nestedpattern_2b.pdf` | 4    | 1    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 6    | 6    |
| `noresources_4.pdf`    | 7    | 5    | 8    | 6    | 6    | 8    | 6    | 6    | 2   | 3    | 4    | 10   | 7    |
| `ocgconfigs_2b.pdf`    | 5    | 2    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 5    | 5    |
| `offlimits_1b.pdf`     | 9    | 6    | 9    | 7    | 7    | 9    | 7    | 7    | 6   | 6    | 7    | 7    | 7    |
| `oirgb_2b.pdf`         | 3    | 1    | 3    | 1    | 1    | 3    | 1    | 1    | 2   | 2    | 3    | 5    | 6    |
| `outlinedest_ua2.pdf`  | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 2   | 2    | 3    | 2    | 0    |
| `outputintent_2b.pdf`  | 6    | 4    | 7    | 5    | 5    | 7    | 5    | 5    | 6   | 6    | 7    | 7    | 7    |
| `pageboxes_2b.pdf`     | 4    | 1    | 4    | 1    | 2    | 5    | 2    | 3    | 4   | 4    | 5    | 5    | 5    |
| `pagegroup_2b.pdf`     | 7    | 4    | 7    | 4    | 5    | 8    | 5    | 6    | 6   | 6    | 7    | 6    | 6    |
| `pagelabels_2b.pdf`    | 4    | 1    | 3    | 0    | 1    | 4    | 1    | 2    | 4   | 4    | 5    | 7    | 6    |
| `paintop_2b.pdf`       | 7    | 4    | 5    | 2    | 3    | 6    | 3    | 4    | 6   | 6    | 7    | 9    | 7    |
| `parentreach_2b.pdf`   | 7    | 3    | 7    | 3    | 4    | 8    | 4    | 5    | 7   | 7    | 8    | 7    | 7    |
| `patterncs_2b.pdf`     | 5    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 10   | 8    |
| `patternglyph_2b.pdf`  | 8    | 4    | 6    | 3    | 4    | 7    | 4    | 5    | 7   | 7    | 8    | 7    | 7    |
| `patternreach_2b.pdf`  | 6    | 2    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 10   | 7    |
| `patternshade_2b.pdf`  | 8    | 4    | 7    | 4    | 5    | 8    | 5    | 6    | 6   | 6    | 7    | 7    | 6    |
| `pdfxnoprofile_2b.pdf` | 5    | 2    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 6    | 6    |
| `pdfxonly_2b.pdf`      | 6    | 3    | 5    | 2    | 3    | 6    | 3    | 4    | 6   | 6    | 7    | 6    | 6    |
| `prosecolumns_ua1.pdf` | 7    | 5    | 7    | 5    | 5    | 7    | 5    | 5    | 4   | 4    | 5    | 4    | 6    |
| `puatext_ua2.pdf`      | 5    | 5    | 6    | 6    | 6    | 6    | 6    | 6    | 4   | 4    | 5    | 3    | 4    |
| `qnestreach_2b.pdf`    | 6    | 2    | 4    | 1    | 2    | 5    | 2    | 3    | 4   | 4    | 5    | 10   | 7    |
| `qshyphen_2b.pdf`      | 4    | 1    | 3    | 0    | 1    | 4    | 1    | 2    | 4   | 4    | 5    | 7    | 6    |
| `remapchain_ua2.pdf`   | 5    | 4    | 6    | 5    | 5    | 6    | 5    | 5    | 2   | 2    | 3    | 4    | 0    |
| `readcolumns_ua1.pdf`  | 7    | 5    | 7    | 5    | 5    | 7    | 5    | 5    | 4   | 4    | 5    | 4    | 6    |
| `readorder_ua1.pdf`    | 7    | 5    | 7    | 5    | 5    | 7    | 5    | 5    | 4   | 4    | 5    | 4    | 6    |
| `recolorimg_2b.pdf`    | 7    | 4    | 3    | 0    | 1    | 4    | 1    | 2    | 4   | 4    | 5    | 6    | 6    |
| `redactcopies_2b.pdf`  | 5    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 7    | 7    |
| `redactrefs_ua1.pdf`   | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 4   | 4    | 5    | 4    | 5    |
| `redactvectors_4.pdf`  | 8    | 6    | 11   | 9    | 9    | 10   | 8    | 8    | 5   | 4    | 4    | 8    | 8    |
| `refreach_ua1.pdf`     | 7    | 4    | 6    | 4    | 4    | 6    | 4    | 4    | 3   | 3    | 4    | 10   | 8    |
| `remreach_2b.pdf`      | 8    | 4    | 6    | 3    | 4    | 7    | 4    | 5    | 7   | 6    | 6    | 10   | 8    |
| `repairleft_4.pdf`     | 10   | 8    | 16   | 13   | 14   | 16   | 13   | 14   | 9   | 10   | 11   | 11   | 9    |
| `repairroom_2b.pdf`    | 5    | 2    | 7    | 3    | 5    | 8    | 4    | 6    | 8   | 8    | 9    | 10   | 9    |
| `resinherit_2b.pdf`    | 9    | 6    | 11   | 8    | 9    | 11   | 8    | 9    | 9   | 9    | 10   | 9    | 9    |
| `rolemapns_ua2.pdf`    | 4    | 4    | 5    | 5    | 5    | 5    | 5    | 5    | 2   | 2    | 3    | 2    | 1    |
| `rolemapscope_ua2.pdf` | 4    | 4    | 5    | 5    | 5    | 5    | 5    | 5    | 2   | 2    | 3    | 2    | 2    |
| `realglyphs_ua1.pdf`   | 6    | 3    | 6    | 3    | 4    | 6    | 3    | 4    | 2   | 2    | 3    | 10   | 9    |
| `rolecycle_ua1.pdf`    | 4    | 4    | 6    | 4    | 4    | 6    | 4    | 4    | 3   | 3    | 4    | 2    | 5    |
| `rolecycle_ua2.pdf`    | 5    | 4    | 8    | 5    | 5    | 8    | 5    | 5    | 2   | 2    | 3    | 5    | 4    |
| `schemaprefix_1b.pdf`  | 17   | 14   | 18   | 15   | 16   | 18   | 15   | 16   | 6   | 6    | 7    | 8    | 8    |
| `shadedefault_2b.pdf`  | 5    | 2    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 6    | 6    |
| `sharedclose_ua1.pdf`  | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 3    | 2   | 2    | 3    | 1    | 4    |
| `sharedarray_2b.pdf`   | 5    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 3   | 3    | 4    | 5    | 6    |
| `sharedstream_2b.pdf`  | 5    | 3    | 7    | 4    | 5    | 7    | 4    | 5    | 6   | 6    | 7    | 9    | 9    |
| `sharednested_2b.pdf`  | 4    | 1    | 4    | 1    | 2    | 5    | 2    | 3    | 5   | 5    | 6    | 6    | 6    |
| `sharedicc_2b.pdf`     | 4\*  | 2\*  | 4\*  | 2\*  | 2\*  | 4\*  | 2\*  | 2\*  | 3\* | 3\*  | 4\*  | 5    | 6    |
| `sharedk_ua1.pdf`      | 4    | 4    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 1    | 5    |
| `sharedtag_ua1.pdf`    | 5    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 2   | 2    | 3    | 6    | 7    |
| `sigcms_2b.pdf`        | 6    | 4    | 9    | 7    | 7    | 9    | 7    | 7    | 5   | 5    | 6    | 10   | 8    |
| `siglock_2b.pdf`       | 5    | 2    | 3    | 0    | 1    | 4    | 1    | 2    | 4   | 4    | 5    | 8    | 6    |
| `sigtagged_ua1.pdf`    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 4   | 4    | 5    | 0    | 4    |
| `sizetie_2b.pdf`       | 4    | 1    | 3    | 0    | 1    | 4    | 1    | 2    | 4   | 4    | 5    | 7    | 6    |
| `smaskindata_2b.pdf`   | 5    | 2    | 5    | 2    | 3    | 6    | 3    | 4    | 6   | 6    | 7    | 6    | 6    |
| `stdremap_ua2.pdf`     | 4    | 4    | 6    | 5    | 5    | 6    | 5    | 5    | 2   | 2    | 3    | 3    | 1    |
| `structcopy_ua2.pdf`   | 5    | 4    | 6    | 5    | 5    | 6    | 5    | 5    | 2   | 2    | 3    | 3    | 0    |
| `structkinds_ua2.pdf`  | 6    | 5    | 7    | 6    | 6    | 7    | 6    | 6    | 3   | 3    | 4    | 5    | 7    |
| `structnest_ua1.pdf`   | 5    | 4    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 36   | 30   |
| `symbolname_2u.pdf`    | 6    | 3    | 6    | 3    | 4    | 6    | 3    | 4    | 4   | 4    | 5    | 8    | 8    |
| `syntax_1b.pdf`        | 11   | 9    | 11   | 9    | 9    | 11   | 9    | 9    | 9   | 9    | 10   | 12   | 8    |
| `tableregular_ua2.pdf` | 4    | 3    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 8    | 7    |
| `tables_ua1.pdf`       | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 4   | 4    | 5    | 2    | 5    |
| `tablespan_ua2.pdf`    | 3    | 3    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 4    | 4    |
| `texlig_2b.pdf`        | 6    | 2    | 5    | 1    | 3    | 6    | 2    | 4    | 5   | 5    | 6    | 9    | 8    |
| `threed_4e.pdf`        | 7    | 5    | 8    | 6    | 6    | 8    | 6    | 6    | 5   | 5    | 6    | 11   | 8    |
| `threedcs_4.pdf`       | 8    | 6    | 9    | 7    | 7    | 9    | 7    | 7    | 6   | 5    | 7    | 8    | 6    |
| `tounicodepair_2u.pdf` | 4    | 2    | 3    | 0    | 0    | 4    | 1    | 1    | 4   | 4    | 5    | 7    | 6    |
| `trailer_2b.pdf`       | 7    | 5    | 7    | 5    | 5    | 7    | 5    | 5    | 6   | 6    | 7    | 7    | 7    |
| `transparency_2b.pdf`  | 10   | 7    | 6    | 3    | 4    | 7    | 4    | 5    | 7   | 7    | 8    | 6    | 6    |
| `treekids_ua1.pdf`     | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 5    | 4   | 4    | 5    | 1    | 4    |
| `ttcmap_2b.pdf`        | 8    | 5    | 9    | 6    | 7    | 9    | 6    | 7    | 6   | 6    | 7    | 9    | 8    |
| `ttencoding_2b.pdf`    | 10   | 7    | 10   | 7    | 8    | 10   | 7    | 8    | 8   | 8    | 9    | 10   | 10   |
| `ttnoenc_2b.pdf`       | 7    | 4    | 8    | 5    | 6    | 8    | 5    | 6    | 7   | 7    | 8    | 10   | 11   |
| `ttmaccopy_2b.pdf`     | 6    | 3    | 6    | 3    | 4    | 6    | 3    | 4    | 3   | 3    | 4    | 8    | 7    |
| `twoprofile_1b.pdf`    | 4    | 1    | 5    | 2    | 3    | 5    | 2    | 3    | 5   | 5    | 6    | 6    | 6    |
| `type0nodesc_2b.pdf`   | 7    | 4    | 7    | 4    | 5    | 7    | 4    | 5    | 5   | 5    | 6    | 7    | 7    |
| `type1notdef_2u.pdf`   | 7    | 4    | 8    | 4    | 6    | 8    | 4    | 6    | 6   | 6    | 7    | 10   | 10   |
| `type3names_2u.pdf`    | 5    | 2    | 5    | 2    | 3    | 5    | 2    | 3    | 3   | 3    | 4    | 7    | 7    |
| `type3cycle_2u.pdf`    | 5    | 2    | 4    | 0    | 1    | 5    | 1    | 2    | 4   | 4    | 5    | 8    | 7    |
| `typedreads_1b.pdf`    | 10   | 8    | 9    | 8    | 8    | 11   | 10   | 10   | 9   | 8    | 8    | 7    | 11   |
| `uaid_ua1.pdf`         | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 6    | 5   | 5    | 6    | 5    | 4    |
| `unclosedmc_ua1.pdf`   | 4    | 4    | 4    | 4    | 4    | 4    | 4    | 4    | 3   | 3    | 4    | 1    | 4    |
| `undecodable_2b.pdf`   | 9    | 6    | 9    | 7    | 7    | 9    | 7    | 7    | 8   | 8    | 9    | 11   | 9    |
| `usecmap_2u.pdf`       | 5    | 2    | 8    | 3    | 5    | 8    | 3    | 5    | 5   | 5    | 6    | 9    | 10   |
| `unembednames_2u.pdf`  | 7    | 4    | 9    | 5    | 7    | 9    | 5    | 7    | 7   | 7    | 8    | 11   | 11   |
| `usedcs_2b.pdf`        | 6    | 3    | 5    | 3    | 3    | 5    | 3    | 3    | 4   | 4    | 5    | 7    | 7    |
| `viewerboxes_2b.pdf`   | 4    | 1    | 3    | 0    | 1    | 4    | 1    | 2    | 4   | 4    | 5    | 5    | 5    |
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

**A content stream pages share under resources of their own.** ISO 32000-1,
7.7.3.3 lets pages share a content stream, and the names it uses resolve in
each page's `/Resources` (7.8.3). veraPDF judges the stream once for the
document, through the first page that draws it, so what a later page's names
select goes unjudged; litho judges each page whose resources differ, and keeps
veraPDF's single reading for pages whose resources are equal. The marked cells
of `sharedicc_2b.pdf` (ICCBased spaces, ISO 19005-2, 6.2.4.2 and ISO 19005-1,
6.2.3.2) and `imagereach_2b.pdf` (an image the second page draws, 6.2.6 and
6.2.8) are this divergence, eleven each, at every PDF/A level. Given a copy of
the stream for each page, veraPDF fails what litho fails. Outside the litho
corpus it reaches pdfjs `pypdf2332.pdf` alone, whose rule sets agree and whose
DeviceGray count is litho's 13 to veraPDF's 10: three pages share a stream
under scanned images of their own. Remediation cannot clear the abstract
profile of `sharedicc_2b.pdf`'s second page, since replacing it changes its
colours, so the remediated outputs keep `6.2.4.2/1` (`6.2.3.2/1` at PDF/A-1) on
litho and report nothing on veraPDF: the remediation features' `disagreement`
reads 11 in each mode, one per PDF/A level.

**DeviceCMYK painted through the output intent's profile.** The renderer
converts colour as the image decoder does: an uncalibrated DeviceCMYK colour
goes through the destination profile of the page's or the document's output
intent (ISO 32000-2, 14.11.5), else litho's built-in CMYK printer profile.
poppler 26.09.0 converts DeviceCMYK with its own fixed formula whatever the
intents say, so the `render` cell mismatches `pdfxonly_2b.pdf` and
`iccbadcmyk_2b.pdf`: their intents' profiles map every CMYK colour to a
neutral grey (the fixtures' `A2B0` tables keep a and b at 128), which litho
paints (144 and 184 grey) and poppler does not (244 114 22 and 97 155 205 for
the squares' `0 0.6 1 0` and `0.6 0.2 0 0.1`). `iccbadcmyk_1b.pdf`'s profile
states version 5.0, an ICC.2 profile ISO 32000-1, 8.6.5.5, Table 67 and ISO
32000-2 do not list, so litho reads no colour through it and paints
DeviceCMYK through its built-in profile, and the page matches poppler.

**The pages the `render` cell leaves mismatched.** Rendered at 72 dpi, 31 pages
of 27 fixtures differ from the poppler 26.09.0 goldens beyond the cell's
tolerance. 16 pages of 13 fixtures differ by more than 1% of the page. Each of
those differences is adjudicated, and none is a litho defect left open:

| File                                           | Page       | Differing pixels     | Why                                       |
| ---------------------------------------------- | ---------- | -------------------- | ----------------------------------------- |
| `pdfxonly_2b`, `iccbadcmyk_2b`                 | 1          | 3600, 3933           | DeviceCMYK through the output intent      |
| `inlineinterp_2b`, `complex_2b`, `megafont_2b` | 1          | 148178, 14873, 14874 | interpolation half a sample off (poppler) |
| `multipage_2b`                                 | 2          | 14873                | interpolation half a sample off (poppler) |
| `inlinejpx_2b`                                 | 1          | 5600                 | poppler stops after an inline JPX image   |
| `imagecolour_2b`                               | 1          | 15375                | colorant None and JPX colour (poppler)    |
| `pageboxes_2b`                                 | 1, 2, 3    | page size            | page boxes read as pdf.js reads them      |
| `kidsarray_2b`, `kidsrepair_1b`                | 2 and 3, 3 | 1 by 1 golden        | poppler draws no page written in `/Kids`  |
| `patternglyph_2b`                              | 3          | 2500                 | poppler paints sub-pixel cells solid      |
| `smaskindata_2b`                               | 1          | 15768                | JPX opacity from the data (poppler)       |

- **Interpolation.** Each fixture enlarges a 2 by 2 or 4 by 1 image with
  `/Interpolate true` fifty times or more. poppler's bilinear filter places a
  sample's value at the low corner of its cell, litho at its centre: ISO
  32000-1, 8.9.4 maps the image onto the unit square with each sample filling a
  cell, so enlarging must not move it. `pkg/internal/render/imgpaint` records
  the measurement. The 14873 pixels of `complex_2b` page 1 and `multipage_2b`
  page 2 are one cause: the same 2 by 2 image (`_common.py`, `rgb_image`)
  drawn into the same 200 by 100 point rectangle, the whole difference inside
  it; `megafont_2b` builds that image itself and adds one pixel of a glyph
  edge. poppler's Splash backend is alone: PDFium and poppler's own Cairo
  backend (`pdftocairo`) paint all four pages the way litho does, with 0
  differing pixels on `complex_2b`, `multipage_2b` and `inlineinterp_2b`, and 9
  on `megafont_2b`, an edge they anti-alias and litho and Splash do not. The
  Square annotations of `complex_2b` and `forbidden_mix`, which have no `/AP`,
  match: litho draws them from `/C`, `/IC`, the border and
  `/CA` as poppler does (`pkg/internal/render/annot`, rendering only; ISO
  19005-2, 6.3.3 is still the rule both files break).
- **`inlinejpx_2b`.** poppler paints the first inline JPXDecode image, warns
  `Problem with skipping JPEG2000 box, stream error`, and paints nothing after
  it: not the second JPX image, the JBIG2, CCITT and Flate images, nor the form.
  ISO 32000-1, 7.4.9 applies JPXDecode to image XObjects alone, so the file is
  wrong, but the data of each inline image runs to its `EI` (8.9.7), and the
  CCITT and Flate controls after it are images every reader paints.
- **`imagecolour_2b`.** poppler paints the two images over the colorant None,
  which produce no visible output (8.6.6.4, 8.6.6.5), as grey ramps, and the
  JPX CIELab and ROMM-RGB images in their raw samples (it rejects the EP fields
  with `Bad COLR header box (bad size: 35)`), where 7.4.9 takes their colour from
  the JPEG 2000 data. litho paints the None images as nothing, and the JPX ones
  in the colours the image decoder reads (see the section on this file). Before
  the renderer skipped them, litho painted the None images black, from the
  decoder's transparent pixels.
- **PDFium, the third reading.** pypdfium2 4.30.0 at 72 dpi, scored by the
  cell's comparison, reads these four fixtures:
  - `pdfxonly_2b`, `iccbadcmyk_2b`: PDFium matches poppler pixel for pixel
    (3600 and 3916 from litho). Neither reads DeviceCMYK
    through an output intent, which litho does by decision (14.11.5).
  - `inlinejpx_2b`: PDFium paints none of the four JPXDecode or JBIG2Decode
    inline images, the form's included, and paints the CCF and Flate
    controls (5600 from litho, 3281 from poppler). It drops the codecs ISO
    32000-1, 7.4.9 and 8.9.7, Table 94 leave to image XObjects; litho paints
    every image whose data runs to its `EI`, as remediation keeps them by
    moving each to an image XObject (`fixer_inlinemove.go`).
  - `imagecolour_2b`: PDFium paints the None images as poppler does, black
    and grey, and none of the four JPX images (19200 from litho, 15714 from
    poppler). The three renderers differ, and litho's reading is the
    specification's: the None images paint nothing (8.6.6.4, 8.6.6.5), the
    EP-field CIELab image converts to sRGB 0 223 255 from the samples 200 80
    40 under its stated ranges and D50 illuminant, as a by-hand ISO/IEC
    15444-2, M.11.7.4 conversion gives, and the e-sRGB fallback matches
    poppler.
- **`pageboxes_2b`.** litho renders the crop box as pdf.js reads it, 612 by
  792, and poppler 500 by 700 (see the section on this file).
- **`patternglyph_2b` page 3.** A Type 3 glyph fills its 10 by 10 pixel box
  with a nested tiling pattern whose cells, in glyph space, are 0.2 pixels
  wide, each holding a red square 0.1 pixels wide. The squares cover a quarter
  of every pixel: litho paints the box 255 191 191, as PDFium (pypdfium2
  4.30.0) does within one unit, and poppler paints it solid red. litho matched
  poppler only while it adjusted the cell's rectangles, which grew each 0.1
  pixel square to a whole pixel; a tiling cell paints without stroke
  adjustment, from the initial `/SA` false (ISO 32000-1, 10.6.5).
- **`smaskindata_2b`.** poppler applies neither image's `/SMaskInData` (ISO
  32000-1, Table 89): it paints both squares opaque, their four channels read
  out of step into stripes. PDFium (pypdfium2 4.30.0) composites `ImStraight`,
  whose colour comes from its data, as litho does, within 47 levels where it
  smooths the 32 pixels over 90 points, and paints `ImPremul`, which names
  `/DeviceRGB`, opaque, black outside its disc (7750 pixels from litho, 10328
  between PDFium and poppler). litho's reading is the specification's and the
  codec's: its decoding of both images matches OpenJPEG 2.5's, opacity and
  colour, premultiplication undone (T.800, Table I.16), and the two discs
  composite to the same picture. The pdfjs files `issue11306`, `issue16782`
  and `isssue18194`, the only `/SMaskInData` images of the other corpora, read
  the same way: litho's decoding matches OpenJPEG's to within 1 level of
  opacity and 2 of colour; poppler paints `issue11306`'s stroke over an opaque
  black box and `issue16782`'s cloud in grey stripes, and PDFium composites
  `issue16782`'s cloud as litho does.
- **Pages written directly in `/Kids`.** ISO 32000-1, 7.7.3.2, Table 29 makes
  `/Kids` an array of references. poppler renders such a page as an empty 1 by
  1 image, and litho draws the page dictionary where the array holds it.

The other 15 pages differ by at most 1% of the page and by more than half of
the pixels either renderer inks: on each, one renderer draws a glyph, a fill or
an annotation the other does not. PDFium (pypdfium2 4.30.0, at 72 dpi) is the
third reading. The pages where litho differs from both are litho divergences
and stay open:

| File                    | Page | Differing / inked   | Who differs | Why                                                                                    |
| ----------------------- | ---- | ------------------- | ----------- | -------------------------------------------------------------------------------------- |
| `filestruct_1b`         | 1    | 1200 / 1422         | litho       | draws a widget's `/AP` under `/NeedAppearances true`, which the others rebuild empty   |
| `afref_3b`              | 1    | 80 / 80             | poppler     | strokes a Link's `/Border` over an empty appearance stream (12.5.5)                    |
| `linkfont_ua2`          | 1    | 720 / 1345          | poppler     | the same `/Border` stroke over six links                                               |
| `annotshare_ua1`        | 1, 2 | 202, 202 / 358, 202 | poppler     | draws its own icon for a Text annotation with no `/AP`                                 |
| `formmedia_ua1`         | 1    | 2400 / 4495         | poppler     | draws no Screen annotation; litho and PDFium paint their appearance                    |
| `sigcms_2b`             | 1    | 677 / 964           | poppler     | builds a text field's appearance from `/V` with no `/NeedAppearances`                  |
| `cmapusecmap_2b`        | 1    | 176 / 176           | poppler     | litho maps codes through the `usecmap` base (9.7.5.4); poppler and PDFium draw nothing |
| `undecodable_2b`        | 3    | 60 / 60             | poppler     | draws nothing for `[/FlateDecode /Foo]`, which litho and PDFium read                   |
| `offlimits_1b`          | 3    | 100 / 100           | poppler     | paints no image while no colour space is selected; litho and PDFium paint it grey      |
| `fontclaims_2b`         | 1    | 35 / 65             | poppler     | draws no glyph for the last show, which litho and PDFium draw as A                     |
| `cmaps_2b`, `limits_1b` | 1    | 314, 110 / 350, 156 | undefined   | an unembedded Adobe-Identity CIDFont: poppler takes the CID as Unicode, litho a box    |
| `type0nodesc_2b`        | 1    | 176 / 237           | undefined   | Type 0 fonts with no descendant: litho boxes, poppler nothing, PDFium a substitute     |
| `parentreach_2b`        | 2    | 25 / 25             | undefined   | a one-input tint transform under nine colorants: litho black, poppler orange           |

The cell's ink bar counts them; the 1% page bar alone passed every one.

Three more were litho's alone and now match. `fontemptycs_2b` draws nothing
for a CFF program with no glyph, where litho drew a substitute or a box: the
program is embedded, and 9.6.6.3 and 9.7.6.3 send a code it lacks to its own
`.notdef`, which it does not hold. `unembednames_2u` draws nothing for the
codes its encodings leave unnamed, where litho drew boxes, and advances them by
the `/MissingWidth`, 0, as poppler and PDF.js do (ISO 32000-1, 9.6.2.1): 8 of
its 184 inked pixels differ, where litho's advance of one em left 78 of 219.
`ttencoding_2b` draws A and B for its symbolic TrueType fonts with the
stand-in the flags give, as poppler and PDFium do, where litho asked for no
substitute and drew boxes.

Three causes of these rows were litho's and are closed. A form whose `/Resources` does not define a
name now resolves it in the page's resources, as poppler and PDFium do (ISO
32000-1, 7.8.3 grants it only to a form with no `/Resources`), so the
`sharednested_2b` forms stroke at the page's `/GS0` line width; a tiling cell
keeps its own resources, which both renderers draw it with. Its cell's
hairline, which litho snapped to whole pixels, now paints at half strength
across two rows, its bottom edge on the cell's `/BBox` clipped to the half
inside, as PDFium paints it and poppler, which draws a cell on a Splash of its
own that never turns stroke adjustment on: a cell runs from the initial
graphics state, whose `/SA` is false (10.6.5). Both pages match, 35 pixels
apart where poppler shifts its resampled cells by a row. And litho reads
nothing from a content stream whose chain decodes past a filter it cannot
decode, so `undecodable_2b` page 2 now matches. A `cs` or `CS` naming no
colour space, and colour operands that do not fit the space in force, now leave
the colour as it was, as poppler draws them; with `/Resources` read whole
(7.7.3.4), the `/CS4` square of `resinherit_2b` page 2 now fills white, as
poppler and PDF.js draw it.

The mechanism is veraPDF's object identity, not a rule. `BaseValidator` (core
1.30.2, `checkRequired`, line 328) pushes a model object onto its work stack
only when its `getID()` is not yet in `idSet`, and `GFPDObject` (validation
1.30.2, lines 57 to 68) builds a content stream's ID from the object key of
the stream it wraps, `"<num> <gen> obj PDContentStream"`. `GFPDPage` (lines 239
to 254) wraps each page's `/Contents`, as `PDPage` (parser 1.30.2, line 85)
holds it, together with that page's resources handler, so the second page's
`GFPDContentStream` carries the first page's ID and is never pushed: its
operators are never parsed, and nothing its resources name is reached. The
contexts veraPDF prints on `imagereach_2b.pdf` show the key:
`contentStream[0](5 0 obj PDContentStream)`. A form XObject's stream takes its
ID the same way, so a form two pages draw is also read once; that changes
nothing while the form names only its own `/Resources`.

Re-measured with live veraPDF 1.30.2 at all thirteen levels, the 22 cells and
what each side counts (litho / veraPDF):

| Levels              | `sharedicc_2b`    | `imagereach_2b`                                     |
| ------------------- | ----------------- | --------------------------------------------------- |
| PDF/A-1a, 1b        | `6.2.3.2/1` 2 / 0 | `6.2.4/3` 1 / 0, `6.2.9/1` 1 / 0, `6.2.3.3/3` 3 / 2 |
| PDF/A-2a to 3u, six | `6.2.4.2/1` 1 / 0 | `6.2.6/1` 1 / 0, `6.2.8/3` 1 / 0, `6.2.4.3/4` 3 / 2 |
| PDF/A-4, 4e, 4f     | `6.2.4.2/1` 1 / 0 | `6.2.7.1/3` 1 / 0, `6.2.4.3/4` 3 / 2                |

Each extra report is an object veraPDF never reaches, not a second count of
one it does. The ICC tests (ISO 19005-1, 6.2.3.2; ISO 19005-2, 6.2.4.2 t1)
judge the profile of each ICCBased space, and the abstract and version 4
spaces are page 2's and page 3's own. The intent and interpolation tests (ISO
19005-1, 6.2.4 and 6.2.9; ISO 19005-2, 6.2.6 and 6.2.8 t3; ISO 19005-4,
6.2.7.1 t3) judge each image dictionary, and `I3` is drawn by page 2 alone.
The device colour tests (ISO 19005-1, 6.2.3.3; ISO 19005-2, 6.2.4.3) judge a
use, and the third DeviceGray is `I3`'s. No clause here counts a shared object
once, so litho keeps its per-reading judgement. Rewriting each fixture with a
copy of the stream per page makes veraPDF's rule sets and counts equal litho's
in all 26 cells.

**`6.2.4.3/4` on a `Lab ` destination profile.** litho reports the device
colour-space rule against a page whose output intent declares an ICC profile
with a `Lab ` data colour space; veraPDF does not. The reduction is a single
page painting `0 0 0 rg` under one output intent whose profile header carries
`prtr` and `Lab `. Both tools agree once the profile declares `RGB `, `CMYK` or
`GRAY`, so the disagreement is about whether a Lab destination profile
establishes a device colour space at all. Not planted in any fixture.

**Counts on `cmapverabase_2b.pdf`.** The rule sets agree in all thirteen
cells, and one count does not. `6.2.11.3.3/3` (`6.2.10.3.3/3`, `7.21.3.3/3`,
`8.4.5.4/3`) reads 4 on litho and 1 on veraPDF at the eleven levels past part
1: veraPDF builds its `PDReferencedCMap` from the `/UseCMap` entry alone
(`GFPDCmap.getUseCMap`), so `K4` counts and the usecmap operators of `K1`, `K2`
and `K3` do not, where litho reads the clause's "a CMap shall not reference any
other CMap" as covering the operator too (ISO 32000-1, 9.7.5.4). Remediation
inlines the operators' bases, after which both tools agree on every output,
default and aggressive, rule by rule and count by count. A `/UseCMap` chain
that loops still makes veraPDF raise "Loop inside CMap" and abandon the run,
where litho leaves the codes unknown; that is not planted in any fixture.

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

Four veraPDF gaps sit outside the corpus, because planting any of them would
add a disagreeing cell rather than coverage.

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

**`6.1.13/11` on a page boundary whose corners are indirect references, where
litho is right.** veraPDF measures a box through `GFCosBBox.getRequiredValue`,
which tests each array entry as written for a number, so a corner written as a
reference to one reads as `null`, which the rule's arithmetic takes for 0.
`/MediaBox [0 0 612 5 0 R]`, object 5 being `792`, fails the size limit there,
measured on veraPDF 1.30.2 at PDF/A-2b and PDF/A-3b. ISO 32000-1, 7.3.10 lets
any object be indirect, so the box is 612 by 792 and litho passes it, while it
reads a missing corner and one that is no number as 0 as veraPDF does.
`pdfjs/issue7872.pdf` writes every corner of its boxes that way, and veraPDF
fails it twice.

**The PDF/UA-1 off-page exemption on a rectangle written reversed, where litho
is right.** ISO 14289-1, 7.18.1 exempts an annotation whose rectangle lies
outside the crop box, and veraPDF's profile carries the exemption into every
7.18 annotation test. Its `isOutsideCropBox` compares the corners as written,
`crop[1] >= rect[3] || crop[0] >= rect[2] || crop[3] <= rect[1] || crop[2] <=
rect[0]`, over a crop box it does not normalize either.
ISO 32000-1, 7.9.5 lets a rectangle be written from any two diagonally
opposite corners, so litho normalizes both. A Link with no `/Contents` written
`[100 100 -50 50]`, across the left edge of its page, or lying squarely on a
page whose media or crop box is written `[612 792 0 0]`, fails `7.18.1/2`,
`7.18.5/1` and `7.18.5/2` on litho and nothing on veraPDF 1.30.2, which
reports all three once the corners are written in order. `pdfjs/issue13447.pdf`,
`issue16384.pdf` and `issue20062.pdf` each carry one such annotation, a
PolyLine, a widget and a Polygon, and draw one `7.18.1` or `7.18.4` finding
more from litho. The decision site is `isAnnotOutsideCropBox`,
`pkg/internal/compliance/accessibility_helpers.go`.

**A device colour space selected by name on a page with no resources, where
litho is right.** `/DeviceRGB cs` selects DeviceRGB directly (ISO 32000-1,
8.6.8, Table 74), with no resource dictionary. veraPDF resolves the name through
the page's resources, finds none on a page that states and inherits none,
leaves the fill colour space unset and judges the paint that follows in the
initial DeviceGray. With no output intent, `/DeviceRGB cs 1 0 0 sc` and a fill
fail `6.2.4.3/2` twice on litho and `6.2.4.3/4` twice on veraPDF 1.30.2 at
PDF/A-2b, 3b and 4, and `6.2.3.3/1` against `6.2.3.3/3` at PDF/A-1b; the
operators `rg`, `k` and `g` fall back to their own family and agree. At PDF/A-4
both tools also report the page's missing dictionary (`6.2.2/3`, see
`noresources_4.pdf`). No corpus file selects a device space that way on such a
page.

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

The same rule at PDF/A-1a was a second defect. `structkinds_ua2.pdf` drew 21
checks from veraPDF and 2 from litho, which took the Ruby and Warichu types for
standard. PDF 1.5 added them, so the PDF Reference 1.4 table the clause names
(9.7.4) does not list them, and litho now reports each of the 19 elements
beside the two `math` ones. From PDF/A-2a both tools take them for standard.

A fifth family is closed. ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1 take
`SESimpleContentItem` as their object, one per painting operator or image, and
7.2 t34 takes `SETextItem`, one per text show; litho stated each once per
document. It now counts one check per item, and reads each content stream once,
as veraPDF validates each object once: two pages sharing a content stream
(`imagereach_2b.pdf`), or a form two pages draw (`fontcopies_2b.pdf`), count
their items on the first page only, and a PostScript XObject is no item
(`multipage_2b.pdf`). Across the litho corpus at its thirteen levels and the
veraPDF corpus at its fifteen, the counts equal veraPDF's in 7500 of the 7502
cells where `7.1/3` or `8.2.2/1` fires (5814 before) and in 350 of the 352
where `7.2/34` does (238 before), and no rule set moves. What is left is
`baretree_2b.pdf`, whose root `/Count` lets veraPDF read four of its five pages,
and `7.2/34` on `complex_ua2.pdf`, whose element with no `/P` veraPDF drops
(see [Known divergences](#known-divergences)). ISO 19005-1, 6.3.2 t7 joins the
descendant rules: veraPDF states it on a Type 0 font and again on its
descendant, which `fontcff2_2b.pdf` and `fontotread_2b.pdf` show.

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
control byte as well, leaving `7.1/3` alone, reported four times by both. Every output of the first eight profiles `manifest.json` lists,
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

The per-rule instance counts equal veraPDF 1.30.2's in all thirteen cells.
ISO 14289-1, 7.1 t3 and 7.2 t34 and ISO 14289-2, 8.2.2 t1 count once per
untagged operator (10) on both tools.

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

The four files return the same rule set and the same counts as veraPDF 1.30.2
in all thirteen cells each. At the PDF/UA levels both tools count `7.1/3` and
`8.2.2/1` once per untagged content item and `7.2/34` once per text show.

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

The file returns the same rule set and the same counts as veraPDF 1.30.2 in
all thirteen cells. At PDF/UA-1 both tools count `7.1/3` and `7.2/34`, and at
PDF/UA-2 `8.2.2/1`, once per untagged content item.

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
counts: at PDF/UA both tools count `7.1/3`, `7.2/34` and `8.2.2/1` once per
untagged content item. At PDF/UA the
form claims its fonts without its glyphs being judged, so `TF` does not answer
the presence rule there, which counts 3.

### `imagereach_2b.pdf`

`imagereach_2b.pdf` holds three images, each with an `/Intent` outside the four
ISO 32000-1 names and an `/Interpolate` of true. veraPDF reaches an image
through the `Do` that draws it, once for the whole document, and a content
stream two pages share through the resources of the first page:

| Image | Where it sits                                                        |
| ----- | -------------------------------------------------------------------- |
| `I1`  | in the resources of a form the first page draws, whose content fills |
| `I2`  | in the resources of a form nested in a form the first page draws     |
| `I3`  | drawn by the form the second page names for the shared stream's `Do` |

No operator draws `I1` or `I2`, so neither image rule fails for them. `I3` is
drawn: the second page's `/FmList` resolves in the second page's resources
(ISO 32000-1, 7.8.3) and paints it, so both rules fail once. veraPDF fails
neither, since it reads the shared stream through the first page alone (see
[Known divergences](#known-divergences)). litho once judged every entry of a
drawn form's resources too, which failed both three times.

| Rule at PDF/A-2b              | veraPDF | litho | litho before |
| ----------------------------- | ------- | ----- | ------------ |
| `6.2.6/1` rendering intent    | 0       | 1     | 0            |
| `6.2.8/3` `/Interpolate` true | 0       | 1     | 0            |
| `6.2.4.3/4` DeviceGray        | 2       | 3     | 2            |
| `6.6.2.1/1` no metadata       | 1       | 1     | 1            |

The rule sets equal veraPDF 1.30.2's in the two PDF/UA cells, and litho adds
the two image rules in the eleven PDF/A cells (`6.2.4/3` and `6.2.9/1` at
PDF/A-1, and the interpolation test `6.2.7.1/3` alone at PDF/A-4).
`6.2.4.3/4` counts the second page's initial DeviceGray fill once more.

### `sharedicc_2b.pdf`

Four pages share one content stream, `/CS0 cs 0.5 0.5 0.5 scn 0 0 10 10 re f`,
and each page's `/Resources` maps `/CS0` to an ICCBased space of its own. The
name resolves in each page's resources (ISO 32000-1, 7.8.3), so the one stream
paints four spaces. Merging duplicate streams on a write makes such files:
`csnames_2b.pdf`, rewritten with `--compact`, shares one content stream
between pages 1, 2, 9, 10 and 13 and another between pages 14 to 19, and
veraPDF stops failing the abstract profiles of pages 9, 10, 18 and 19, which
the rewrite leaves in place; litho stopped too, until it judged each page's
resources. No corpus file shares a content stream under ICCBased spaces of its
own: `pypdf2332.pdf` (pdfjs) shares three under images of their own, and
`imagereach_2b.pdf` one under a form.

| Page | `/CS0`                                         | Answers                     |
| ---- | ---------------------------------------------- | --------------------------- |
| 1    | ICCBased over a version 2 display profile      | nothing                     |
| 2    | ICCBased over a version 2 abstract profile     | `6.2.4.2/1`, `6.2.3.2/1`    |
| 3    | ICCBased over a version 4 display profile      | `6.2.3.2/1` at PDF/A-1 only |
| 4    | page 1's space, in resources equal to page 1's | nothing                     |

ISO 19005-2, 6.2.4.2 takes a profile of the input, output, display or colour
space class, so page 2 fails at every PDF/A level; ISO 19005-1, 6.2.3.2 reads
the PDF 1.4 profile, version 2, so page 3 fails at PDF/A-1 alone. Page 4 reads
the stream under the same resources as page 1, and is judged once with it.

| Rule                    | veraPDF | litho | each page its own stream, veraPDF |
| ----------------------- | ------- | ----- | --------------------------------- |
| `6.2.4.2/1` at PDF/A-2b | 0       | 1     | 1                                 |
| `6.2.3.2/1` at PDF/A-1b | 0       | 2     | 2                                 |
| `6.6.2.1/1` at PDF/A-2b | 1       | 1     | 1                                 |

veraPDF judges the shared stream through page 1 alone and fails no ICC test;
given a copy of the stream for each page, it fails what litho fails on the
shared one. The two PDF/UA cells agree, rule by rule and count by count.

### `sharedarray_2b.pdf`

ISO 32000-1, 7.7.3.3 lets a page's `/Contents` be an array of streams, and
nothing keeps two pages from naming the same stream in theirs. Aggressive
remediation replaces an invalid rendering intent (ISO 19005-1, 6.2.9 and ISO
19005-2, 6.2.6) by rewriting the page's content. Written in place, into the
array's first stream with the others emptied, the rewrite took a later shared
stream off the other page and, through a shared first stream, painted this
page's content on it. No corpus file reaches it. Each page paints a square of
its own and the shared square:

| Page | `/Contents` | Streams                                           | Answers at PDF/A-2b |
| ---- | ----------- | ------------------------------------------------- | ------------------- |
| 1    | `[A S]`     | A: `/Bogus ri` and a square; S: the shared square | `6.2.6/1`           |
| 2    | `[B S]`     | B: a square of its own                            | nothing             |
| 3    | `[T C]`     | T: the shared square; C: `/Bogus ri` and a square | `6.2.6/1`           |
| 4    | `[T D]`     | D: a square of its own                            | nothing             |
| 5    | `[E]`       | E: `/Bogus ri` and a square, shared by no page    | `6.2.6/1`           |

A page whose array shares a stream gets a stream of its own, and the page
dictionary points at it; page 5, which shares nothing, is rewritten in place
as before. veraPDF passes the aggressive output either way at PDF/A-1b and
PDF/A-2b, since the lost or doubled square breaks no rule: only the page
content tells the two writes apart. Both tools agree on every cell, rule by
rule.

### `sharedstream_2b.pdf`

ISO 32000-1, 7.7.3.3 lets two pages name the same content stream, and each page
reads it through its own `/Resources` (7.8.3). Both pages name one stream as
their whole `/Contents`, `BT /F1 24 Tf 50 700 Td (A\x01) Tj ET`:

| Page | `/F1`                                                   | Reads `(A\x01)` as                   | Answers at PDF/A-2b          |
| ---- | ------------------------------------------------------- | ------------------------------------ | ---------------------------- |
| 1    | a simple TrueType under `WinAnsiEncoding`               | `A`, then 0x01, which names no glyph | `6.2.11.4.1/2`, `6.2.11.8/1` |
| 2    | a Type0 font under `Identity-H`, a `CIDToGIDMap` stream | one code, CID 0x4101, the glyph `A`  | nothing                      |

Aggressive remediation strips the control byte page 1 shows (ISO 19005-2,
6.2.11.8). Written in place, the rewrite took the byte off page 2 too, which
then showed a lone `A`, half a two-byte code: litho reported `.notdef` there,
and veraPDF, which judges a shared stream once through page 1's resources, did
not. No corpus file has a page naming through a reference a stream another page
names, and a repair rewriting it. Both fonts embed the TrueType program of
`ttcmap_2b.pdf`; the Type0 font carries a ToUnicode CMap mapping its code to
U+0041. Both pages also draw `6.2.4.3/4`, litho once per page and veraPDF once.

Measured on veraPDF 1.30.2, litho and veraPDF agree on the rule set at all
thirteen levels on the input and on every output, default and aggressive. Before
the fix the aggressive outputs of the eleven levels where the strip runs, every
level but PDF/A-1a and 1b, failed `.notdef` and glyph rules on page 2 in litho
alone, and the second page's stream read `[(A) -600] TJ`; now page 1 gets a
stream of its own and page 2 keeps `(A\x01)`.

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
for every rule at every level; at the PDF/UA levels both count `7.1/3` and
`8.2.2/1` once per untagged content item.

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
at every level but PDF/A-1a, where veraPDF states `6.3.8/1` once per rendered
glyph and litho once per font (see
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
for every rule at every level; at the PDF/UA levels both count the
untagged-content rules once per text show.

Remediated at PDF/A-1b, in both modes, the output gives `C`'s descendant a
`/CIDSet` naming the CIDs the three-glyph program holds, while `C` renders CID 5,
which the program does not hold. ISO 19005-1, 6.3.5 has the set identify the
CIDs present in the program, and veraPDF asks it at PDF/A-1 only for a rendered
CID the program holds: one the `/CIDToGIDMap` covers whose glyph lies below
`maxp.numGlyphs`, with or without an outline. Neither tool reports `6.3.5/3` on
the output, and the two agree on its rule set. At PDF/A-1a and 1b the output
also carries UniJIS-UCS2-H embedded for the Korean fonts, the two written
directly moving to objects of their own, so `6.3.3.3/1` (4) clears on both
tools. `6.3.3.1/1` (4) stays on both: the descendant declares Adobe-Korea1
under a Japan1 CMap, which no faithful copy of the CMap changes.

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

Remediation at the first eight manifest profiles leaves the same rule set on both
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
counts.

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
counts.

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
first eight profiles `manifest.json` lists, default and aggressive:

| Profile  | Runs refused before the fix       | Rules left after it, litho / veraPDF |
| -------- | --------------------------------- | ------------------------------------ |
| PDF/A-2a | 6 of 8, both modes                | none / none                          |
| PDF/UA-1 | 6 of 8, both modes                | `7.1/3`: 2 / 2                       |
| PDF/UA-2 | 6 of 8 default, 8 of 8 aggressive | `8.2.2/1`: 2 / 2                     |

After the fix every run of every profile writes the same bytes. PDF/A-1b, 2b,
2u, 3b and 4 do not tag, write the same bytes before and after, and leave no
rule on either tool. The rule left at PDF/UA is honest: `/X2` and `/X3` draw
real text the classifier does not tag, and leaving their regions unmarked keeps
it readable rather than hiding it inside an `/Artifact`. Both tools report each
of the two regions.

Before remediation the file agrees with veraPDF in all thirteen cells:

| Rule at PDF/UA-1               | veraPDF | litho |
| ------------------------------ | ------- | ----- |
| `6.2/1` no `/MarkInfo`         | 1       | 1     |
| `7.1/3` unmarked content       | 6       | 6     |
| `7.1/8` no metadata stream     | 1       | 1     |
| `7.1/10` no `/DisplayDocTitle` | 1       | 1     |
| `7.1/11` no structure tree     | 1       | 1     |
| `7.2/34` no natural language   | 5       | 5     |

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
equal veraPDF 1.30.2's in all thirteen cells, and so do the counts.

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
in the remediated output of the first eight profiles `manifest.json` lists, default
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
counts. Remediated
at the first eight profiles `manifest.json` lists, default and aggressive, every
output leaves the font alone at the PDF/A levels, `6.3.4/1` at PDF/A-1b, and
`7.18.1/2` and `7.21.4.1/1` at PDF/UA-1 and `8.4.5.5.1/1` at PDF/UA-2, where no
interpolation rule applies, and veraPDF agrees on each one's rule set and
counts.

Before the repair, every PDF/A output kept the four `6.2.8/3` findings on both
tools. With the repair but not the write of the page's array, the rewrite wrote
the reference to its new stream as the value of the array's object, which
qpdf reads as an integer and litho's text extraction as no text: the writer
rolled the content repairs back and the four findings came back.

### `inlinelzw_2b.pdf`

ISO 19005-1, 6.1.10 forbids LZWDecode in an inline image's filters, and ISO
19005-2/3, 6.1.10 and ISO 19005-4, 6.1.9 forbid LZWDecode, Crypt and every value
outside ISO 32000-1, Table 6. veraPDF judges each inline image as an image of
its own. The LZW repair re-encodes stream objects, whose dictionary and data
are apart, so every inline image kept its finding through a remediation. The
corpus holds such images in page content alone; this fixture puts one in each
stream veraPDF parses, and spells the filters every way the repair reads them:

| Stream                                       | Filters written                              | A-1b       | A-2b       |
| -------------------------------------------- | -------------------------------------------- | ---------- | ---------- |
| the page's content                           | `/F /LZW /DP << /EarlyChange 0 >>`, and `/L` | `6.1.10/2` | `6.1.10/1` |
| the page's content                           | `/F [/AHx /LZW /RL]`                         | `6.1.10/2` | `6.1.10/1` |
| the page's content                           | `/F /AHx`, the control                       | none       | none       |
| a form XObject the page draws                | `/Filter /LZWDecode`                         | `6.1.10/2` | `6.1.10/1` |
| glyph `a` of a Type 3 font the page shows    | `/F [/LZW]`                                  | `6.1.10/2` | `6.1.10/1` |
| the cell of a tiling pattern the page fills  | `/F /LZW`                                    | `6.1.10/2` | `6.1.10/1` |
| the normal appearance of a Square annotation | `/F /Lzw`, a name outside Table 6            | none       | `6.1.10/1` |

Every image draws the same eight grey samples from real LZW data. The repair
decodes each image through its last forbidden filter, writes it again with
FlateDecode, keeps the RunLength after the LZW of the second, and states the
new length in the first's `/L`; the control keeps its bytes. At PDF/A-1 the
`/Lzw` image keeps its bytes too, since ISO 19005-1 names LZWDecode alone.

| Rule at PDF/A-2b                   | veraPDF | litho | litho remediated |
| ---------------------------------- | ------- | ----- | ---------------- |
| `6.1.10/1` forbidden inline filter | 6       | 6     | 0                |
| `6.2.4.3/4` DeviceGray             | 8       | 8     | 0                |
| `6.6.2.1/1` no metadata            | 1       | 1     | 0                |

The rule sets equal veraPDF 1.30.2's in all thirteen cells, and so do the
counts. Remediated at the eleven PDF/A levels, default and aggressive, every
output passes on both tools but PDF/A-4f's, which keeps `6.9/5`, the embedded
file that level requires, and each draws the samples the source draws. Before
the repair, every output kept the five or six inline filter findings on both
tools.

### `inlinejpx_2b.pdf`

JPXDecode and JBIG2Decode are standard filters (ISO 32000-1, 7.4, Table 6), but
no inline image's: ISO 32000-2, 8.9.7, Table 92 gives neither an abbreviation,
and ISO 32000-1, 7.4.9 applies JPXDecode to image XObjects alone. veraPDF
1.30.2's PDF/A-2, 3 and 4 profiles state ISO 19005-2/3, 6.1.10 t1 and ISO
19005-4, 6.1.9 t1 as that abbreviation list, while litho let both filters
through. No other corpus file holds such an image. ISO 19005-1, 6.1.10 names
LZWDecode alone, so part 1 judges none of them by the rule:

| Stream                        | Filters written                         | A-1b | A-2b       |
| ----------------------------- | --------------------------------------- | ---- | ---------- |
| the page's content            | `/F /JPXDecode`, `/CS /RGB /BPC 8`      | none | `6.1.10/1` |
| the page's content            | `/F /JPXDecode`, colour from the data   | none | `6.1.10/1` |
| the page's content            | `/F /JBIG2Decode`, 1-bit DeviceGray     | none | `6.1.10/1` |
| the page's content            | `/F [/AHx /JBIG2Decode]`, an image mask | none | `6.1.10/1` |
| the page's content            | `/F /CCF`, a control                    | none | none       |
| the page's content            | `/F /Fl`, a control                     | none | none       |
| a form XObject the page draws | `/Filter /JPXDecode`, the full key      | none | `6.1.10/1` |

The JPEG 2000 data is the codestream `_jp2.py` holds in a JP2 container with
one sRGB colour specification; the JBIG2 data is an embedded stream of one
generic region coded with MMR, the band the CCF control codes with Group 4.

| Rule at PDF/A-2b                   | veraPDF | litho | litho remediated |
| ---------------------------------- | ------- | ----- | ---------------- |
| `6.1.10/1` forbidden inline filter | 5       | 5     | 0                |

The rule sets equal veraPDF 1.30.2's in all thirteen cells, and so do the
counts. The repair moves each image to an image XObject the stream's resources
name, its data byte for byte under its filters spelled in full, so every viewer
decodes the samples it decoded. Decoding the images and writing them inline
with Flate passed veraPDF too, at PDF/A-2b and PDF/A-4, default and aggressive,
but writes one decoder's reading of the JPEG 2000 data into the file.
Remediated at the eleven PDF/A levels, default and aggressive, every output
passes on both tools but PDF/A-4f's, which keeps `6.9/5`, the embedded file
that level requires.

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

Remediation at the first eight manifest profiles, normal and aggressive, leaves
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

Remediation at the first eight manifest profiles, normal and aggressive, leaves
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
first eight manifest profiles now passes both tools in both modes; the PDF/UA ones
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

Remediation at the first eight manifest profiles, default and aggressive, leaves
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

The two tools return the same rule set and the same counts in all thirteen
cells. At PDF/UA-1 both count `7.1/3` and `7.2/34`, and at PDF/UA-2 `8.2.2/1`,
once per untagged content item.

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
first eight manifest profiles, default and aggressive, passes both tools but the
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

Remediation at the first eight manifest profiles, default and aggressive, leaves
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

Measured on veraPDF 1.30.2 and litho, the file returns the same rule set and
the same counts in all thirteen cells. At PDF/UA-1 both count `7.1/3` and
`7.2/34`, and at PDF/UA-2 `8.2.2/1`, once per untagged content item. The PDF/A
cells report the three
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

Remediation at the first eight manifest profiles, default and aggressive, leaves, on
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
untagged text of the four pages its `/Count` lets it read, and litho three
times, once per untagged text of the five pages its walk reads. veraPDF's
`PDDocument.getPages` asks for as many pages as the root's `/Count` states and
finds each by descending the `/Count` of every node (`PDPageTreeBranch`), so
every page-scoped rule sees four pages, while its COS-level rules still reach
the fifth page's objects through the document's indirect objects
(`GFCosDocument.getIndirectObjects`). ISO 32000-1, 7.7.3.2 makes `/Count` the
number of leaves, so the tree contradicts itself, and litho keeps reading the
leaves, as its rewrites do: the reading veraPDF applies is not one every rule
shares, and no corpus file but this one states a `/Count` short of its leaves.

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
DeviceRGB. Remediation at the first eight manifest profiles, default and aggressive,
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

Remediation at the first eight manifest profiles, default and aggressive, leaves the
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
two; at PDF/UA-1 both count `7.1/3` and `7.2/34`, and at PDF/UA-2 `8.2.2/1`,
once per stream. The other cells report the
identification and tagging rules a part-2 claim draws.

Before the fix, every write of the file held the direct streams inline: split,
merge, insert, rotation, stamping, flattening, auto-tagging, encryption, and
remediation at the first eight manifest profiles, default and aggressive. qpdf failed
every output, and a split with `--compact` held them inside an object stream.
Now each output holds every stream as an object of its own, qpdf reads it
without a warning, and its pages show the four texts. The gate's qpdf check
tells the two apart in `mutate-encrypt` alone: the input already carries the
warning classes an output writing the streams inline draws, so the other cells
read the same either way, and the Go tests of each writer path pin the rest.
Remediation at the first eight manifest profiles, default and aggressive, leaves
nothing at the PDF/A profiles on either tool, and `7.1/3` at PDF/UA-1 and
`8.2.2/1` at PDF/UA-2 on both, both counting two, before the fix and after.

### `pagelabels_2b.pdf`

ISO 32000-1, 12.4.2 makes `/PageLabels` a number tree keyed by page index, each
range giving its pages a numbering style, a prefix and a start (Table 159), so
an edit that moves, removes or adds a page changes what every later page
displays. A split and a merge dropped the tree, and page insertion and deletion
kept it as it was, keyed to pages that had moved. No litho fixture carried
labels; 520 files of the other corpora do.

Eight pages, each showing its own text, carry one range per style. The tree is
a root with `/Kids` over two leaves, each with its `/Limits` (ISO 32000-1,
7.9.7, Table 37), and the range of page 4 is a page label dictionary of its own
with the optional `/Type`:

| Page | Shows | Label   | Range                                               |
| ---- | ----- | ------- | --------------------------------------------------- |
| 1    | `A`   | `i`     | lowercase roman                                     |
| 2    | `B`   | `ii`    |                                                     |
| 3    | `AA`  | `Cover` | a prefix with no numbering style                    |
| 4    | `AB`  | `A-7`   | decimal, prefix `A-`, start 7, an object of its own |
| 5    | `BA`  | `A-8`   |                                                     |
| 6    | `BB`  | `a`     | lowercase letters                                   |
| 7    | `AAA` | `b`     |                                                     |
| 8    | `AAB` | `Ü-IV`  | uppercase roman, a UTF-16BE prefix `Ü-`, start 4    |

The file conforms to PDF/A-2b: the text is in the conforming embedded TrueType
of `ttcmap_2b.pdf`, in DeviceGray under a CMYK OutputIntent, as in
`baretree_2b.pdf`. No PDF/A or PDF/UA rule reads page labels. The two tools
return the same rule set in all thirteen cells, and the same counts in the
eleven PDF/A ones: nothing at PDF/A-2b, and at the other levels the
identification and tagging rules a part-2 claim draws. At PDF/UA-1 both count
`7.1/3` and `7.2/34`, and at PDF/UA-2 `8.2.2/1`, once per untagged text of the
eight pages.

`mutate-pages` moves page 1 to the end and repeats it there, and its label
promise, read with qpdf, wants the output to show
`ii Cover A-7 A-8 a b Ü-IV i i`. Before the fix the move kept the source's
tree, so every page showed the label of the page that used to stand there.
Measured on litho, pdf.js and veraPDF 1.30.2 after it: the move, a split of
pages 3 to 6, a merge with an unlabelled document, an insertion of one at page
5 and a deletion of pages 2 and 5 show in pdf.js the labels their pages showed
in their sources, the unlabelled pages their page numbers, and report the
input's rule set on both tools, the move in all thirteen cells. The move that
repeats page 1 does too on veraPDF 1.30.2, in all thirteen cells. Remediation
at the first eight manifest profiles, default and aggressive, leaves nothing on
either tool and keeps every label.

### `annotshare_ua1.pdf`

veraPDF judges an annotation object once, on the first page listing it, however
many pages list it: its validator skips an object key it has met already. litho
judged it once for every listing, so each rule an annotation failed counted
again for every page listing it. No corpus file lists an annotation object
twice. Page 1 lists four annotation objects, the Square twice, and page 2 lists
the first three again:

| Construct                                                                  | Verdict at PDF/UA-1                |
| -------------------------------------------------------------------------- | ---------------------------------- |
| a Link without `/Contents` outside the structure tree, listed by two pages | `7.18.5/1`, `7.18.5/2`, `7.18.1/2` |
| a Text annotation with no flags, `/Contents` or appearance, on two pages   | `7.18.1/1`, `7.18.1/2`             |
| a text field Widget with no `/TU` and no appearance, on two pages          | `7.18.1/3`, `7.18.4/1`             |
| a Square under an Annot element, listed twice by page 1                    | silent                             |
| a Link beyond the media box page 2 inherits from the page tree             | silent                             |
| a Link on page 3 above its crop box of three numbers, `[0 0 300]`          | silent                             |

Every rule counts once, and `7.18.1/2` twice, for the Link and the Text. At
PDF/UA-2 the Square's `/Contents` differs from its element's `/Alt`, and
`8.9.4.2/1` counts once. From part 2 the Text fails the flag test once
(`6.3.2/1`), and the Text, the Widget and the Square, which carry no
appearance, fail the appearance test once each (`6.3.3/1`); at part 1 the Text
fails `6.5.3/2` and the Widget `6.9/2` once.

Both exempt Links lie off the crop box veraPDF reads (`PDPage.getCropBox`): the
one the page inherits, and one whose missing corner reads as 0, which covers no
height. litho read the page dictionary alone, found no box on page 2 and a
malformed one on page 3, and held both Links to the rules.

Page 3 also states an art box of `[0 0 400 null]`. veraPDF reads a missing
corner and one that is no number as 0 (`GFCosBBox.getRequiredValue`), so the
crop and art boxes fail `6.1.13/11` at every part-2 and part-3 level, twice;
litho skipped any box that was not four numbers. No corpus file carries a box
that short except one a damaged page object runs into. Every rewrite drops
both boxes, which enclose no area (see `pageboxes_2b.pdf`), so its output
passes `6.1.13/11`, and the Link on page 3 lies on the page: both tools count
one more `7.18.1/2`, `7.18.5/1` and `7.18.5/2` check for each copy of the page,
the rule sets unchanged.

The XMP packet declares every namespace on the property element using it, as
pikepdf's metadata API writes it: `pdfuaid:part` 1, `pdfaid:part` 2 and
`pdfaid:conformance` B, `dc:title`, and an extension schema declaring the
`pdfuaid` namespace whose every element declares its own. litho read
`pdfuaid:part` through the literal `pdfuaid:part>1<`, which met only the closing
tag, and reported `5/2` on a packet claiming part 1. PDF/A-2b sees a conforming
identification and extension schema.

Measured on veraPDF 1.30.2 and litho, the file agrees with veraPDF in all
thirteen cells, rule for rule and count for count. Before the fix litho reported
`5/2` at PDF/UA-1 and missed `6.1.13/11` at the part-2 and part-3 levels, and at
PDF/UA-1 counted `7.18.5/1` 4 times, `7.18.5/2` 3, `7.18.1/2` 6, `7.18.1/1`,
`7.18.1/3` and `7.18.4/1` twice, at PDF/UA-2 `8.9.4.2/1` twice, and at PDF/A-2b
`6.3.2/1` twice and `6.3.3/1` 6 times. The other rules the cells report are
the stream lengths pikepdf writes (`6.1.7/1`, `6.1.7.1/1`, `6.1.6.1/1`), the
part-2 claim at the other levels (`6.7.11`, `6.6.4`, `6.7.3`), the `Annot`
element at PDF/A-1a, no PDF 1.4 standard type (`6.8.3.4/1`), the `%PDF-1.7`
header at part 4 (`6.1.2/1`), the Document element outside the PDF 2.0
namespace (`8.2.5.2/2`) and the part-1 claim (`5/2`, `5/5`) at PDF/UA-2, and,
at PDF/A-4f, the missing `/EmbeddedFiles`.

Remediation at the first eight manifest profiles, default and aggressive, leaves the
same rule set and counts on both tools in every cell: nothing at the PDF/A
levels, where the page box repair now drops the short crop and art boxes it
used to leave alone, the Link's and the Text's missing descriptions at
PDF/UA-1, and the contradicting `/Alt` at PDF/UA-2. The stamp, page, autotag,
flatten, encrypt and redact mutations move the rule sets only as their
allowances let them, and the one qpdf warning their outputs carry, the Square
listed twice, is the input's own.

### `structcopy_ua2.pdf`

Split, merge and page insertion carry the structure elements that describe the
pages they copy (ISO 32000-1, 14.7), and every entry of the structure tree root
that names them: the `/ParentTree` keyed by the pages' `/StructParents` and the
annotations' `/StructParent`, the `/IDTree`, the root `/RoleMap` and
`/ClassMap`, and the PDF 2.0 `/Namespaces`. No corpus file packs all of those
into one tree over several pages, so this file does, and conforms at PDF/UA-2 on
both tools, so any part a copy mishandles shows as a rule the output adds.

| Page | Content, in reading order                                                         |
| ---- | --------------------------------------------------------------------------------- |
| 1    | `H1` `title-1`, a `Para` `intro`, a `Para` of class `Centered`, a `Link` + `OBJR` |
| 2    | a `Para` `cont`, then a 2 x 2 `Table`: `TH` `h1` and `h2`, `TD` naming them       |
| 3    | `H1`, a `Para` in `fr-FR`, a `Callout` in a custom namespace, a `Link` + `OBJR`   |

The Document element names the PDF 2.0 namespace, the other elements the default
one, whose `Para` the root `/RoleMap` maps to `P`. The custom namespace's
`/RoleMapNS` maps `Callout` to the PDF 2.0 `Aside` (ISO 32000-2, 14.7.4.2). The
paragraphs of pages 1 and 2 sit under one `Sect` naming no page, so a copy of
either page keeps part of it, and the two data cells name the header cells'
identifiers in `/Headers`, so a merge renaming a clashing identifier has to
rename both sides. Each link annotation's `/StructParent` keys the
`/ParentTree` after the three pages' keys. The font is embedded and draws only
`A` and `B`, which is all the text uses, and no word of page 1 occurs anywhere
else, so a redaction of the page removes every copy of it.

The file agrees with veraPDF 1.30.2 in all thirteen cells, rule for rule and
count for count. PDF/UA-2 reports nothing. PDF/UA-1 reports the part-2 claim
(`5/2`), the `%PDF-2.0` header (`6.1/1`) and the `Callout`, which the PDF 1.7
reading leaves unmapped (`7.1/5`). The PDF/A cells report the thirteen text
paints in device gray with no output intent (`6.2.3.3/3` at part 1,
`6.2.4.3/4` from part 2), the missing PDF/A identification and the `pdfuaid`
properties no extension schema declares, the header at parts 2 and 3
(`6.1.2/1`), and the `Callout` again at level A (`6.8.3.4/1`, `6.7.3.4/1`);
PDF/A-4f adds the missing `/EmbeddedFiles`.

Measured on litho and veraPDF 1.30.2 before the structure was carried, a split
of any page or range and a merge of the file with itself wrote no structure
tree, failing `7.1/11`, `7.1/3` and, with a link on the page, `7.18.5/1` at
PDF/UA-1 and `8.2.1/1` and `8.2.2/1` at PDF/UA-2, and `mutate-pages` added
`7.1/3` and `7.18.5/1` at PDF/UA-1 and `8.2.2/1` at PDF/UA-2. Now every page
alone, pages 2 to 3, the merge with itself, page 1 inserted in front of the file
and after it, and the page move all pass PDF/UA-2 on both tools. At PDF/UA-1
each reports the input's rules, or, with no `Callout` on the pages it keeps,
the input's without `7.1/5`, which the merge counts once per `Callout`. Every
`mutate-*` cell passes, the page move under the allowance that lets no rule go.
Remediation at the first eight manifest profiles, default and aggressive, leaves the
same rule set on both tools in every cell: `6.7.3.4/1` at PDF/A-2a and `7.1/5`
at PDF/UA-1, the `Callout` a PDF 1.7 reading cannot map.

### `noresources_4.pdf`

ISO 32000-2, 7.7.3.3, Table 31 makes `/Resources` a required, inheritable entry
of a page, and a page that needs no resources states an empty dictionary. A page
that states none, under a page tree that states none either, leaves its content
stream with no resource dictionary at all, where ISO 19005-4, 6.2.2 requires
every content stream to have one. veraPDF fails `6.2.2/3` once for such a
stream, whatever it paints, an empty one included: it resolves the stream's
initial DeviceGray through the missing dictionary and reports that name as
undefined. litho stated the test only for a name the stream uses, so a stream
that names none drew nothing. No file the gate validates at PDF/A-4 has such a
page; three pdfcpu and five pdfjs files do, and veraPDF agrees with litho on
the six of them it can parse.

| Page | `/Contents`                      | `/Resources`  | `6.2.2/3` |
| ---- | -------------------------------- | ------------- | --------- |
| 1    | paints a rectangle in DeviceGray | none          | once      |
| 2    | an empty stream                  | none          | once      |
| 3    | `q Q`                            | `null`        | once      |
| 4    | `q Q`, one stream shared with 5  | none          | once      |
| 5    | the stream page 4 shows          | none          | none      |
| 6    | `q Q`                            | an empty dict | none      |
| 7    | none                             | none          | none      |

Page 2 carries a Square annotation whose appearance stream states no
`/Resources` either, and fails nothing through it: veraPDF gives a form XObject
resources of its own even when it states none. No stream names a resource, so
`6.2.2/3` reaches the rule set through the missing dictionaries alone.

| Rule                                  | veraPDF | litho | litho before |
| ------------------------------------- | ------- | ----- | ------------ |
| PDF/A-4 `6.2.2/3` undefined names     | 5       | 5     | 0            |
| PDF/A-4 `6.2.4.3/4` DeviceGray, no OI | 2       | 2     | 2            |

The two tools return the same rule set and the same count for every rule in all
thirteen cells. Parts 1 to 3 have no test 3, and report page 1's DeviceGray
(`6.2.3.3/3` at part 1, `6.2.4.3/4` from part 2), the part-4 identification,
the `%PDF-2.0` header at parts 2 and 3 and, at level A, the missing structure.
PDF/A-4e and 4f add their own identification, 4f the missing `/EmbeddedFiles`,
and the PDF/UA cells report the untagged document and its annotation.

Remediation at the first eight manifest profiles, default and aggressive, leaves
nothing on either tool in any cell: every rewrite writes the `/Resources`
Table 31 requires, an empty dictionary on a page that inherits none. So does a
stamp, a page move and an encryption, measured through the litho commands they
call, and `6.2.2/3` passes on their outputs at PDF/A-4, which the mutation
allowances admit (`pageResourcesRules`); flattening and redaction may pass any
rule, and auto-tagging leaves the file, which holds no text, alone.

### `outlinedest_ua2.pdf`

Split and merge rebuild the outline of the pages they copy, and a page insertion
copies the optional content its pages draw into another document. An outline
item names a structure destination (ISO 32000-2, 12.3.2.3) in several forms,
and ISO 14289-2, 8.8 asks every destination inside the document to be one. No
corpus file packs those forms into one conforming file, and none names one
through a named destination's dictionary, so this file does, over three pages
that each open with a heading or a paragraph of their own:

| Item               | Destination                                                              |
| ------------------ | ------------------------------------------------------------------------ |
| `Opening heading`  | A GoTo action: `/D` page 1 and `/SD` its `H1`, both `/XYZ 50 752 null`   |
| `Second page`      | A child item whose `/Dest` is `[P /Fit]`, the first paragraph of page 2  |
| `Third page`       | `/Dest (third)`, a `/Dests` entry `<< /D [page 3 /Fit] /SD [H1 /Fit] >>` |
| `Third page again` | A GoTo action whose `/D` is `(third)`                                    |

Page 1 draws one paragraph in a layer the default configuration shows and one
in a layer it hides. `/OCProperties` lists both groups in the default
configuration and in one `/Configs` entry, each named and each ordering both
groups (ISO 14289-2, 8.7; ISO 19005-2, 6.9).

The file agrees with veraPDF 1.30.2 in all thirteen cells, rule for rule.
PDF/UA-2 reports nothing. PDF/UA-1 reports the part-2 claim (`5/2`) and the
`%PDF-2.0` header (`6.1/1`). The PDF/A cells report the text painted in device
gray with no output intent (`6.2.3.3/3` at part 1, `6.2.4.3/4` from part 2),
the missing PDF/A identification and the `pdfuaid` properties no extension
schema declares, the header at parts 2 and 3 (`6.1.2/1`), and at part 1 the
optional content it forbids (`6.1.13/1`); PDF/A-4f adds the missing
`/EmbeddedFiles`.

Measured on litho and veraPDF 1.30.2 before these constructs were carried, a
split of page 1, of page 3 and of pages 2 to 3, and a merge of the file with
itself, failed `8.8/1` at PDF/UA-2, each rebuilt item naming its page alone; a
split of page 2 dropped the item naming a paragraph and no page; the merge
added `7.10/1` at PDF/UA-1 and `6.9/1` at PDF/A-2b, its default optional content
configuration unnamed; and page 1 inserted in front of the file or after it left
its copied groups out of `/OCProperties`, so a viewer drew the hidden layer. Now
every page alone, pages 2 to 3, the merge with itself, page 1 inserted in front
and after, and the page move pass PDF/UA-2 on both tools, and report the input's
rules at PDF/UA-1 and PDF/A-2b. Remediation at the first eight manifest profiles,
default and aggressive, leaves nothing on either tool.

### `pageboxes_2b.pdf`

ISO 32000-1, 7.7.3.3, Table 30 makes each page boundary a rectangle, four
numbers (7.9.5), and gives each but the media box a default: the media box for
the crop box, the crop box for the others. veraPDF, and litho's compliance,
measure a box on its first four entries, a missing one or one that is no number
as 0. pdf.js and poppler read a box that is not four numbers enclosing an area
as no box, and draw the page on its default, US Letter for the media box. A
rewrite copied a crop, bleed, trim or art box as its source stated it, and
wrote US Letter for any media box that is not four numbers. The corpus holds a
media box of more than four entries only where its first four are Letter
(`directstream_2b.pdf`), and no such box on a page tree node, nor one beside a
crop box a node above states.

The root states `/MediaBox [0 0 612 792]` and `/CropBox [0 0 500 700]`. A node
under it states `/MediaBox [0 0 400 500 /X]` and `/CropBox [0 0 0 0]`, and lists
pages 2 and 3. No page draws anything.

| Page | Box         | As written         | veraPDF measures | A viewer reads | Written as    |
| ---- | ----------- | ------------------ | ---------------- | -------------- | ------------- |
| 1    | `/MediaBox` | `[0 0 600 800 7]`  | `[0 0 600 800]`  | US Letter      | US Letter     |
| 1    | `/CropBox`  | `[0 0 300]`        | no area          | the media box  | the media box |
| 1    | `/TrimBox`  | `[0 /Foo 200 300]` | `[0 0 200 300]`  | the crop box   | none          |
| 1    | `/BleedBox` | `[0 0 300 400 9]`  | `[0 0 300 400]`  | the crop box   | none          |
| 2    | `/MediaBox` | the node's         | `[0 0 400 500]`  | US Letter      | US Letter     |
| 2    | `/CropBox`  | the node's         | no area          | the media box  | the media box |
| 3    | `/MediaBox` | `[0 0 612]`        | no area          | US Letter      | US Letter     |
| 3    | `/CropBox`  | the node's         | no area          | the media box  | the media box |

Every rewrite writes a box as a viewer reads it. A crop, bleed, trim or art box
a viewer does not use reads as its default, and the output drops it. Dropping
page 1's crop box, or the node's, would let the root's show through, so each
page states its media box as its crop box and shows the region it showed. A
media box a viewer does not use reads as US Letter, and the output writes
Letter. The pinned pdf.js shows all three pages at 612 by 792, and so does every
output.

The file agrees with veraPDF 1.30.2 in all thirteen cells, rule for rule and
count for count. From part 2 the crop box of page 1, the one pages 2 and 3
inherit and the media box of page 3 fail `6.1.13/11`, four checks; every other
box passes it. Every other rule is one the file draws at a level it does not
claim: the identification its part-2, level-B claim does not match, the
tagging, title and language the a levels and PDF/UA ask for, and at part 4 the
`%PDF-1.7` header and, at PDF/A-4f, the missing `/EmbeddedFiles`.

Measured on litho and veraPDF 1.30.2 before the fix, the full rewrite, split,
merge, insertion, page move and PDF/A-1b remediation each wrote pages 1 and 2
as US Letter, where litho read 600 by 800 and 400 by 500, kept page 1's crop,
trim and bleed boxes as written, and failed `6.1.13/11`. The PDF/A-2b
remediation, which repairs the boxes itself, dropped page 1's crop box and the
node's, and the root's then cropped pages 1 and 3 to 500 by 700. Now every one
of those outputs passes `6.1.13/11` on both tools, and each page shows the
region the table gives. Remediation at the first eight manifest profiles, default and
aggressive, leaves nothing on either tool.

The fix wrote each box veraPDF measures as the rectangle it measures, where the
table's last column once read "the same". Measured on the pinned pdf.js, the
full rewrite, split and PDF/A-2b remediation then showed page 1 at 600 by 800
and page 2 at 400 by 500, and kept page 1's trim and bleed boxes as the
rectangles of their first four entries, boxes no viewer had drawn the page by.
Now every output shows each page at US Letter and states no trim or bleed box,
as the table gives; `6.1.13/11` still passes on both tools, and remediation at
the first eight manifest profiles, default and aggressive, still leaves nothing on
either tool.

### `rolemapscope_ua2.pdf`

The structure tree root's `/RoleMap` maps a private structure type to a
standard one for an element in no namespace, or in the PDF 1.7 namespace when
that namespace declares no `/RoleMapNS` (ISO 32000-2, 14.8.6). A type an
element declares in the PDF 2.0 namespace resolves through that namespace
alone, which defines no private type, and a `/RoleMap` target is a PDF 1.7
type. No corpus file declares a namespace on an element whose type only the
`/RoleMap` maps, so this file does, beside the two placements the map reaches,
under a root element whose private type the map sends to `Document`:

| Element                         | Namespace | `/RoleMap` | Read at PDF/UA-2                    |
| ------------------------------- | --------- | ---------- | ----------------------------------- |
| `Workbook`, the root's only kid | none      | `Document` | the PDF 1.7 `Document`: `8.2.5.2/2` |
| `H1`                            | PDF 2.0   |            | `H1`                                |
| `Para`                          | PDF 2.0   | `P`        | no standard type: `8.2.4/1`         |
| `Para`                          | none      | `P`        | `P`                                 |
| `Para`                          | PDF 1.7   | `P`        | `P`                                 |

The file agrees with veraPDF 1.30.2 in all thirteen cells, rule for rule and
count for count. PDF/UA-2 reports the root's `Document` outside the PDF 2.0
namespace (`8.2.5.2/2`) and the `Para` in it (`8.2.4/1`), veraPDF one check
each. PDF/UA-1 and the level-A cells read the file as PDF 1.7, where the
`/RoleMap` maps all three `Para`, and report nothing of the structure:
PDF/UA-1 reports the part-2 claim (`5/2`) and the `%PDF-2.0` header (`6.1/1`).
The PDF/A cells report the text painted in device gray with no output intent
(`6.2.3.3/3` at part 1, `6.2.4.3/4` from part 2), the missing PDF/A
identification and the `pdfuaid` properties no extension schema declares, and
the header at parts 2 and 3 (`6.1.2/1`); PDF/A-4f adds the missing
`/EmbeddedFiles`.

Measured on litho and veraPDF 1.30.2 before the fix, litho let the `/RoleMap`
map the `Para` in the PDF 2.0 namespace and missed `8.2.4/1` at PDF/UA-2.
PDF/UA-2 remediation declared the PDF 2.0 namespace on `Workbook`, which cut it
off from the map, and the single-Document repair then wrapped it in a second
`Document`: veraPDF reported `8.2.4/1` on the output, for `Workbook` and the
`Para`, and litho nothing. Now the repair names the root element a PDF 2.0
`Document` and leaves the `/RoleMap` as written, and renames the `Para` in the
PDF 2.0 namespace `P`, the type the map gives every other `Para` (see
`rolemapns_ua2.pdf`): both tools report nothing, default and aggressive, and
before that rename both reported `8.2.4/1` for the `Para` alone. Remediation at
the seven other profiles of the first eight in the manifest leaves nothing on either tool, and every
`mutate-*` cell passes.

### `destforms_ua2.pdf`

ISO 14289-2, 8.8 asks every destination inside the document to be a structure
destination (ISO 32000-2, 12.3.2.3). It judges a named destination by the value
the name maps to, and a GoTo action wherever it hangs. PDF/UA-2 remediation
gives a destination one by writing a GoTo action with the page destination
under `/D` and the structure destination under `/SD` (12.6.4.2, Table 202). The
corpus files the gate remediates reach a GoTo action under a page's or an
annotation's `/AA` only in documents of one page holding no structure content,
and never name a destination through the catalog `/Dests` or write a
dictionary under `/Dest`, so this file packs those forms into a tagged document
of three pages, each opening with a heading or a paragraph of its own:

| Owner                      | Destination                                                                           | Test    |
| -------------------------- | ------------------------------------------------------------------------------------- | ------- |
| Outline item `Second page` | `/Dest (second)`, a `/Names /Dests` entry for page 2                                  | `8.8/1` |
| Outline item `Third page`  | A GoTo action whose `/D` is `/third`, a catalog `/Dests` entry for page 3             | `8.8/2` |
| First link on page 1       | `/Dest /third`                                                                        | `8.8/1` |
| First link on page 1       | A direct GoTo action for page 1 under `/E` of its direct `/AA`                        | `8.8/2` |
| Second link on page 1      | `/Dest` holding a GoTo action for page 2, which ISO 32000-1, Table 173 does not allow | `8.8/1` |
| Second link on page 1      | A GoTo action to `(second)` under `/X` of an `/AA` written as an object of its own    | `8.8/2` |
| Page 2                     | A direct GoTo action for page 2 under `/AA /O`                                        | `8.8/2` |
| Page 2                     | A GoTo action for page 3, an object of its own, under `/AA /C`                        | `8.8/2` |

The file agrees with veraPDF 1.30.2 in all thirteen cells, rule for rule and
count for count. PDF/UA-2 reports `8.8/1` three times and `8.8/2` five times.
PDF/UA-1 reports the part-2 claim (`5/2`) and the `%PDF-2.0` header (`6.1/1`).
The PDF/A cells report the links' appearances painted in device RGB and the
text in device gray with no output intent (`6.2.3.3/1` and `6.2.3.3/3` at part
1, `6.2.4.3/2` and `6.2.4.3/4` from part 2) and the missing PDF/A
identification. Parts 1 to 3 add the `pdfuaid` properties no extension schema
declares, and parts 2 and 3 the header (`6.1.2/1`) and page 2's `/AA`
(`6.5.2/2`), which part 4 reports as `6.6.3/1`; PDF/A-4f adds the missing
`/EmbeddedFiles`.

Measured on litho and veraPDF 1.30.2 before the fix, PDF/UA-2 remediation,
default and aggressive, left all eight destinations as they were, and both
tools reported `8.8/1` three times and `8.8/2` five times on the output. Now
each is a GoTo action carrying `/SD`, a name kept under `/D` and the dictionary
under `/Dest` moved to `/A`, and PDF.js opens the page each opened before.
Remediation at the first eight manifest profiles, default and aggressive, leaves
nothing on either tool, and every `mutate-*` cell passes.

### `rolemapns_ua2.pdf`

`rolemapscope_ua2.pdf` shows which placements the structure tree root's
`/RoleMap` governs. This file holds, under a root `Document` in the PDF 2.0
namespace, one element per repair PDF/UA-2 remediation makes when an element
declares a namespace the map does not govern, while the map sends its private
type to a standard one (ISO 32000-2, 14.8.6). No corpus file declares such a
namespace, before or after remediation.

| Element   | Namespace                                   | `/RoleMap`  | Repair                                   |
| --------- | ------------------------------------------- | ----------- | ---------------------------------------- |
| `Heading` | custom, no `/RoleMapNS`                     | `H1`        | the namespace maps it to the 2.0 `H1`    |
| `Para`    | the same custom namespace                   | `P`         | the namespace maps it to the 2.0 `P`     |
| `Para`    | custom, an indirect `/RoleMapNS` of `Other` | `P`         | the entry joins that indirect dictionary |
| `Para`    | PDF 1.7, with a `/RoleMapNS` of `Chapter`   | `P`         | renamed `P`                              |
| `Block`   | PDF 2.0                                     | `Body`, `P` | renamed `P`                              |

A custom namespace defines no standard type, so its elements keep their names
and the namespace's `/RoleMapNS` maps the type into the PDF 2.0 namespace; a
standard namespace defines the target, so its elements are renamed. The file
agrees with veraPDF 1.30.2 in all thirteen cells and both WTPDF levels, rule
for rule and count for count. PDF/UA-2 reports each of the five elements
(`8.2.4/1`, five checks on each tool); the WTPDF levels add their missing
declaration. PDF/UA-1 and the level-A cells read the file as PDF 1.7, where the
`/RoleMap` maps every element, and the other cells report what
`rolemapscope_ua2.pdf` reports.

Measured on litho and veraPDF 1.30.2 before the repair, PDF/UA-2 remediation,
default and aggressive, left `8.2.4/1` on both tools, five checks each, and so
did remediation to either WTPDF level. Now each of those outputs passes on both
tools. Remediation at the seven other profiles of the first eight in the manifest leaves nothing on
either tool.

### `viewerboxes_2b.pdf`

ISO 32000-1, 7.9.5 writes a rectangle as four numbers. veraPDF measures a page
boundary on its first four entries, a missing one or one that is no number as
0, and ISO 19005-2, 6.1.13 t11 passes every box below. pdf.js and poppler read
each as no box, and draw the page on the box's default (ISO 32000-1, Table 30):
US Letter for the media box, the media box for the crop box, the crop box for
the others. `pageboxes_2b.pdf` holds boxes of those kinds that enclose no area
once measured. No corpus file holds one that encloses an area, a media box
holding a name, or a crop box a viewer uses on a media box it does not.

Pages 2 and 4 state `/MediaBox [0 0 600 800]` and page 5 `[0 0 400 500]`.
Page 5 sits under two nodes: the outer states `/CropBox [0 0 200 300]`, the
inner `/CropBox [0 0 300 400 9]`. No page draws anything.

| Page | Box         | As written         | veraPDF measures | A viewer reads   | Written as    |
| ---- | ----------- | ------------------ | ---------------- | ---------------- | ------------- |
| 1    | `/MediaBox` | `[10 10 300]`      | `[10 10 300 0]`  | US Letter        | US Letter     |
| 2    | `/CropBox`  | `[0 0 300 400 9]`  | `[0 0 300 400]`  | the media box    | none          |
| 2    | `/ArtBox`   | `[10 10 320]`      | `[10 10 320 0]`  | the crop box     | none          |
| 3    | `/MediaBox` | `[0 /Foo 600 800]` | `[0 0 600 800]`  | US Letter        | US Letter     |
| 3    | `/CropBox`  | `[0 0 700 900]`    | the same         | cut to US Letter | the same      |
| 4    | `/CropBox`  | `[0 /Foo 300 400]` | `[0 0 300 400]`  | the media box    | none          |
| 4    | `/TrimBox`  | `[0 0 310 410 9]`  | `[0 0 310 410]`  | the crop box     | none          |
| 4    | `/BleedBox` | `[5 5 320]`        | `[5 5 320 0]`    | the crop box     | none          |
| 5    | `/CropBox`  | the inner node's   | `[0 0 300 400]`  | the media box    | the media box |

pdf.js takes a page's nearest crop box, the inner node's for page 5, and reads
no box. Dropping it would let the outer node's show through, so the page states
its media box as its crop box. poppler 26.09.0 passes over the inner box and
shows the outer one, 200 by 300: the two viewers part there, and litho reads
the page as pdf.js does. qpdf reads the media boxes of pages 1 and 3 as
undefined and sets them to Letter.

The file agrees with veraPDF 1.30.2 in all thirteen cells, rule for rule and
count for count. Every box passes `6.1.13/11`, and PDF/A-2b reports nothing.
Every other rule is one the file draws at a level it does not claim: the
identification its part-2, level-B claim does not match, the tagging, title and
language the a levels and PDF/UA ask for, and at part 4 the `%PDF-1.7` header
and, at PDF/A-4f, the missing `/EmbeddedFiles`.

Measured on the pinned pdf.js before the fix, the full rewrite wrote every box
as the rectangle veraPDF measures: page 1 became a page 290 by 10 points, pages
2, 4 and 5 showed 300 by 400 and page 3 600 by 800, where the source shows 612
by 792, 600 by 800, 612 by 792, 600 by 800 and 400 by 500. The split wrote the
same but for page 5, which it copied without the node above it. litho read page
3 at 600 by 800 and page 4 at 300 by 400. Now litho reads the pages as pdf.js
shows them, and the full rewrite, the split and remediation at the first eight
manifest profiles, default and aggressive, show each page as the source shows
it. The rewrites pass PDF/A-2b on both tools, and the remediations leave nothing
on either.

### `gotochain_ua2.pdf`

ISO 32000-1, 12.6.2, Table 193 lets an action's `/Next` hold one action or an
array of actions, run after it, so the actions an owner triggers form a tree.
ISO 14289-2, 8.8 asks every destination inside the document to be a structure
destination (ISO 32000-2, 12.3.2.3), a GoTo action's among them wherever it
runs. veraPDF judges every GoTo action of the tree, each action object once
whichever owner reaches it first. Two corpus files run a GoTo action after
another action, neither tagged, and none writes `/Next` as an array, loops it
back on itself or shares a chained action between two owners, so this file
does, over three pages that each open with a heading of their own:

| Owner                     | Action tree                                                                                                                                     | GoTo actions without `/SD` |
| ------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- |
| Outline item `First page` | A direct GoTo action for page 1, whose direct `/Next` array holds a Named action, then a GoTo action for page 2 written as an object of its own | 2                          |
| `/OpenAction`             | A direct GoTo action for page 1 whose `/Next` is that same GoTo action for page 2                                                               | 1                          |
| First link on page 1      | A GoTo action for page 3, a URI action, then a GoTo action for page 2 looping back to the first, all objects of their own                       | 2                          |
| Second link on page 1     | A Named action whose `/Next` is an array object of its own: a direct GoTo action for page 3, and one whose `/SD` names page 1's heading         | 1                          |
| Page 2                    | `/AA /O`, a Named action object whose `/Next` is a direct GoTo action to `(second)`, a `/Names /Dests` entry for page 2                         | 1                          |

The GoTo action for page 2 that the outline item and the `/OpenAction` share is
one object, judged once.

The file agrees with veraPDF 1.30.2 in all thirteen cells, rule for rule and
count for count. PDF/UA-2 reports `8.8/2` seven times. PDF/UA-1 reports the
part-2 claim (`5/2`) and the `%PDF-2.0` header (`6.1/1`). The PDF/A cells report
the links' appearances painted in device RGB and the text in device gray with no
output intent (`6.2.3.3/1` and `6.2.3.3/3` at part 1, `6.2.4.3/2` and
`6.2.4.3/4` from part 2) and the missing PDF/A identification. Parts 1 to 3 add
the `pdfuaid` properties no extension schema declares, and parts 2 and 3 the
header (`6.1.2/1`) and page 2's `/AA` (`6.5.2/2`), which part 4 reports as
`6.6.3/1`; PDF/A-4f adds the missing `/EmbeddedFiles`.

Measured on litho and veraPDF 1.30.2 before the fix, litho reported `8.8/2`
three times, on the GoTo actions an owner holds itself, against veraPDF's seven.
PDF/UA-2 remediation, default and aggressive, gave those three a structure
destination and left the four run through `/Next`, which veraPDF reported and
litho did not. Now litho judges every GoTo action of the tree, each object once,
and remediation walks the tree: each of the seven carries `/SD`, the Named and
URI actions keep their entries, and PDF.js opens the page the outline item, the
links and the `/OpenAction` opened before. Remediation at the first eight manifest
profiles, default and aggressive, leaves nothing on either tool, and every
`mutate-*` cell passes.

### `emptyfig_ua2.pdf`

ISO 14289-2, 8.2.5.28.2 and ISO 14289-1, 7.3 ask every Figure for alternate
text. Aggressive PDF/UA remediation removes a Figure that lacks it and encloses
nothing, since no text could describe it, and every reference to it goes with
it; the default mode leaves the Figure, and alternate text the caller supplies
keeps it. The corpus reaches two of the forms such a Figure takes, no kid and
a marked-content identifier its page does not mark, and no reference the
removal cleans but a `/ParentTree` slot, so this file packs the other forms and
references into a tagged document of two pages:

| Figure          | Encloses                                                | Named by                                            | After aggressive remediation                    |
| --------------- | ------------------------------------------------------- | --------------------------------------------------- | ----------------------------------------------- |
| F1              | the null object                                         |                                                     | removed                                         |
| F2              | MCID 7, which page 1 does not mark                      | a `/ParentTree` slot                                | removed, the slot null                          |
| F3              | an object reference to object 999, which is not defined | a `/ParentTree` entry of its own, under key 5       | removed, the entry gone                         |
| F4              | F5 alone                                                |                                                     | removed                                         |
| F5              | a marked-content reference to MCID 8, unmarked          | a `/ParentTree` slot                                | removed, the slot null                          |
| F6, with `/Alt` | F7 alone                                                |                                                     | stays, with no kid                              |
| F7              | nothing                                                 |                                                     | removed                                         |
| F8, in a `Div`  | MCID 9, unmarked                                        | a `/ParentTree` slot                                | removed; the `Div` stays, with no kid           |
| F9              | MCID 10, unmarked                                       | the `/IDTree`, a `/Ref`, an outline `/SE` and `/SD` | removed; the `/SD` names the `Document` instead |
| F10, on page 2  | MCID 0 of page 2, unmarked                              | an outline item's `/SD`                             | stays, with the placeholder `/Alt`              |
| K2              | MCID 2, a drawing page 1 marks                          |                                                     | stays, with the placeholder `/Alt`              |
| K3              | nothing                                                 | an outline item's `/Dest`, a destination array      | stays, with the placeholder `/Alt`              |

The `Document` is the element page 1 opens on once F9 is gone, the one
structure-destination remediation picks for a destination to page 1. Page 2
holds no other element, so without F10 the action to it would keep its page
destination alone, which ISO 14289-2, 8.8 rejects: F10 stays as the only
element that `/SD` can name. K3 stays because a destination array names it
with no page destination beside it to fall back on.

The file agrees with veraPDF 1.30.2 in all thirteen cells, rule for rule and
count for count. PDF/UA-2 reports `8.2.5.28.2/1` for the eleven Figures lacking
alternate text, and PDF/UA-1 `7.3/1` for the same eleven beside the part-2
claim (`5/2`) and the `%PDF-2.0` header (`6.1/1`). The PDF/A cells report the
text and drawings painted in device gray with no output intent (`6.2.3.3/3` at
part 1, `6.2.4.3/4` from part 2) and the missing PDF/A identification. Parts 1
to 3 add the `pdfuaid` properties no extension schema declares, and parts 2
and 3 the header (`6.1.2/1`); PDF/A-4f adds the missing `/EmbeddedFiles`.

Measured on litho and veraPDF 1.30.2 before the removal, aggressive remediation
to PDF/UA-1, PDF/UA-2 and WTPDF-1.0 Accessibility gave all eleven Figures the
placeholder `/Alt`, and both tools reported nothing on the outputs. With the
removal, nine went and K2 and K3 kept the placeholder, and the PDF/UA-2 and
WTPDF-1.0 Accessibility outputs failed `8.8/2` once on both, the action to
page 2 left without its `/SD`. Now eight go and F10, K2 and K3 keep the
placeholder: the PDF/UA-1, PDF/UA-2 and WTPDF-1.0 Accessibility outputs pass
both tools. The default mode leaves the eleven findings on both tools, as
before. Remediation at the six PDF/A profiles among the first eight manifest profiles and at WTPDF-1.0 Reuse,
default and aggressive, leaves nothing on either tool.

### `sharedk_ua1.pdf`

ISO 32000-1, 14.7.2 makes the structure tree a strict tree, and Table 323 lets
an element's `/K` be an array, which a file may write as an object of its own
(7.3.10). A walk down `/K` that remembers the elements it met, and not the
arrays it read, reads such an array again wherever it is reached, and so reads
the elements written directly in it again: no object number marks them. Were
those direct elements to name the array again as their own `/K`, the work
would double at every level, and a four-object file ran every validation,
remediation and page copy until the caller's timeout. No corpus file names a
`/K` or `/Kids` array from two places, so this file does, once, in a tagged
page whose `Document` holds a paragraph and two `Sect` elements:

| Element       | `/K`                                                                        |
| ------------- | --------------------------------------------------------------------------- |
| `P`           | MCID 0, a filled rectangle on the page                                      |
| First `Sect`  | the array object both `Sect` elements name: a `Figure` and a `Span`, direct |
| Second `Sect` | the same array object                                                       |

The `Figure` has no alternate text. litho reads the shared array once and
reports it once, as veraPDF 1.30.2 does: the file agrees with veraPDF in all
thirteen cells, rule for rule and count for count. PDF/UA-1 reports `7.3/1`.
PDF/UA-2 reports `8.2.5.28.2/1`, the `Span` under a `Sect` (`Table 5.
Sect-Span/1`), the part-1 claim and the missing `pdfuaid:rev` (`5/2`, `5/5`)
and the `Document` outside the PDF 2.0 namespace (`8.2.5.2/2`). The PDF/A cells
report the rectangle painted in device gray with no output intent (`6.2.3.3/3`
at part 1, `6.2.4.3/4` from part 2) and the missing PDF/A identification;
parts 1 to 3 add the `pdfuaid` property no extension schema declares, part 4
the `%PDF-1.7` header (`6.1.2/1`), and PDF/A-4f the missing `/EmbeddedFiles`.

Measured on litho before the fix, the walks read the array under each `Sect`
and reported the `Figure` twice, `7.3/1`, `8.2.5.28.2/1` and `Table 5.
Sect-Span/1` each twice, against veraPDF's once. Remediation at the first eight
manifest profiles, default and aggressive, leaves nothing at the six PDF/A
profiles on either tool. At PDF/UA-1 and PDF/UA-2 the default mode leaves the
`Figure` on both, as it leaves every `Figure` lacking alternate text. Aggressive
remediation took nothing out of the tree and gave no placeholder to a `Figure`
written directly in an array, so it left the `Figure` too; now it takes the
`Figure` out of the shared array, since it encloses nothing, and alternate text
the caller supplies goes into the array in its place. The aggressive PDF/UA-1
output passes both tools, and the aggressive PDF/UA-2 output keeps `Table 5.
Sect-Span/1` alone on both. A split naming the page twice, a
merge of the file with itself and an insertion of it into itself copy the tree
twice, and each copy holds a `Figure` written directly in an array: litho
reports each, twice in all, and veraPDF reports one, as it reports one for two
`Figure` elements written directly in two arrays of a single tree. The rule
sets agree.

### `fieldkids_2b.pdf`

ISO 32000-1, 12.7.3.1, Table 220 makes a field's `/Kids` an array of indirect
references, and nothing stops the array being written as an object of its own
and named by reference. Every walk over the field tree reads such an array
once: the direct fields it holds could each name it again, and read anew
wherever it is reached it fans out 2^depth, a denial of service that a 4-object
file drives to the caller's timeout. No corpus file names a field's `/Kids` by
reference, so this file gives the gate the shape:

| Field         | Kind         | `/Kids`                | Widgets                           |
| ------------- | ------------ | ---------------------- | --------------------------------- |
| `group`       | non-terminal | an array, by reference | none of its own                   |
| `group.first` | text, merged | none                   | itself, with an appearance        |
| `group.inner` | text         | an array, by reference | one, whose `/AA` holds JavaScript |
| `group.note`  | text, direct | none                   | none: written in `group`'s array  |
| `check`       | check box    | an array, by reference | one, with no appearance           |

The page shows nothing, and every appearance paints nothing.

The file agrees with veraPDF 1.30.2 in all thirteen cells, rule for rule and
count for count. PDF/A-2b reports `6.4.1/1` for the widget of `group.inner` and
`6.3.3/1` for the widget of `check`; every PDF/A cell of parts 2 and 3 reports
both, and part 1 reports them as `6.6.2/1` and `6.9/2`, the latter reached
through the field walk. PDF/UA-1 reports `7.18.1/3` and `7.18.4/1` once per
widget. The other rules are the ones the part-2, level-B claim draws at the
levels it does not match: identification, tagging, and at part 4 the
`%PDF-1.7` header.

`forms` lists the four terminal fields, and `set-form` sets `group.inner` and
`check` through the arrays. Measured on litho and veraPDF 1.30.2, `set-form`,
`split`, `merge` and `autotag` outputs report on both tools what the file does,
`set-form` adding `6.4.1/3` for the `/NeedAppearances` it sets, and the
`flatten-form` output passes both. Remediation at the first eight manifest profiles,
default and aggressive, leaves nothing on either tool; at PDF/UA-1 it adds a
`/TU` to the four fields written as objects of their own, `group.note` being
written directly. Every output litho wrote before the walks read each array
once is byte-identical to the one it writes now, but the encrypted one, whose
key is random.

### `boxorigin_2b.pdf`

A page draws in user space, where `(0, 0)` is the coordinate origin, and a
viewer shows the crop box (ISO 32000-1, 14.11.2), which nothing makes start
there. `pdf.TextStamp` measures X and Y from the page's bottom-left corner, the
visible one, so litho translates a stamp by the lower-left point of the region
a viewer shows. A rectangle may name either pair of opposite corners (7.9.5),
so that point is the lesser of each pair, and a crop box reaching past the media
box counts once reduced to their intersection (14.11.2.1). No page elsewhere in
this corpus has a crop box that does not start at the origin, so the
`mutate-stamp` cell could not tell a stamp at the corner from one at the origin,
or from one moved by the box origin twice, as the cell itself placed it:

| Page | Boundaries as written                              | Visible lower-left |
| ---- | -------------------------------------------------- | ------------------ |
| 1    | `/MediaBox [0 0 612 792] /CropBox [50 60 500 700]` | `(50, 60)`         |
| 2    | `/MediaBox [-306 -396 306 396]`                    | `(-306, -396)`     |
| 3    | `/MediaBox [306 396 -306 -396]`                    | `(-100, -150)`     |
|      | `/CropBox [200 300 -100 -150]`                     |                    |
| 4    | `/MediaBox [0 0 612 792] /CropBox [30 40 700 900]` | `(30, 40)`         |
| 5    | `/CropBox [100 120 500 700] /Rotate 90`            | `(100, 120)`       |
| 6    | `/CropBox [70 80 400 500]`, from its parent node   | `(70, 80)`         |

Pages 5 and 6 state `/MediaBox [0 0 612 792]`. No page draws anything.

The file agrees with veraPDF 1.30.2 in all thirteen cells, rule for rule and
count for count, and PDF/A-2b reports nothing: every rule is one the file draws
at a level it does not claim, as on `viewerboxes_2b.pdf`. Remediation at the
first eight manifest profiles, default and aggressive, leaves nothing on either tool.

`mutate-stamp` reads where the stamp's text lands on each page and requires it
10 points right of and above the visible corner. Its stamp reads there on every
page, and poppler's `pdftotext -bbox` places it at the same points. Planted in
turn, each misplacement fails the cell on this file: the cell adding the box
origin to X and Y (page 1 reads at `(110, 130)`), `stampOriginShift` reading the
crop box's first two entries (page 3 at `(210, 310)`), and no shift at all (page
1 at `(10, 10)`). The six other `mutate-*` cells pass the file at all ten
manifest profiles.

### `treekids_ua1.pdf`

ISO 32000-1, 7.9.6 and 7.9.7 make a name or number tree's `/Kids` an array of
indirect references to its nodes, and a file may write that array as an object
of its own (7.3.10). A walk that marks the nodes it met, and not the arrays it
read, reads such an array again wherever a direct node names it: were the
direct nodes to name their own array again, the work would double at every
level. The structure tree's `/ParentTree` and `/IDTree` readers the page copy
and form flattening use were such walks. No corpus file names a structure
tree's `/Kids` by reference, so this file does, once each:

| Tree          | Root                   | `/Kids`                | Leaf, written directly in `/Kids` |
| ------------- | ---------------------- | ---------------------- | --------------------------------- |
| `/ParentTree` | an object, no `/Nums`  | an array, by reference | `/Nums` for keys 0 and 1          |
| `/IDTree`     | an object, no `/Names` | an array, by reference | `/Names` for `p1`, `p2` and `p3`  |

Page 1 inherits its `/MediaBox` from the page tree node and states `/CropBox
[0 0 300 792]`; its second paragraph sits at x = 400, past the crop box, where
a viewer hides it and a reader ignoring the crop box shows it. A redaction
searches the widest region any reading of the page shows, so the `redact`
mutation clears it with the rest of page 1.

litho and veraPDF 1.30.2 agree in all thirteen cells, rule for rule and count
for count. PDF/UA-1 reports only the Helvetica left unembedded
(`7.21.4.1/1`). PDF/UA-2 reports the part-1 claim and the missing
`pdfuaid:rev` (`5/2`, `5/5`), the `Document` outside the PDF 2.0 namespace
(`8.2.5.2/2`) and the unembedded font (`8.4.5.5.1/1`). The PDF/A cells report
the font, the text painted in device gray with no output intent (`6.2.3.3/3`
at part 1, `6.2.4.3/4` from part 2) and the missing PDF/A identification;
parts 1 to 3 add the `pdfuaid` property no extension schema declares, part 4
the `%PDF-1.7` header (`6.1.2/1`), and PDF/A-4f the missing `/EmbeddedFiles`.

Measured on litho, `split`, `merge` and `flatten-form` outputs report at
PDF/UA-1 what the file does, the structure carried whole, and `redact` over
page 1 leaves no text on it. Every output of `redact`, `flatten-form`, `split`
and `merge` over the 5235 corpus files litho wrote before the tree readers
read each array once is byte-identical to the one it writes now.

### `directfig_ua2.pdf`

ISO 32000-1, 14.7.2, Table 323 lets an element's `/K` hold a structure element
directly, with no object number of its own, and ISO 14289-2, 8.2.5.28.2 and
ISO 14289-1, 7.3 ask such a `Figure` for alternate text like any other.
Remediation writes the text into the object that holds the `Figure`, the
element whose `/K` lists it, through inline elements however deep, or the `/K`
array written as an object of its own, and aggressive remediation takes out a
`Figure` that encloses nothing from that object. No corpus file writes a
`Figure` directly but `sharedk_ua1`, whose one encloses nothing, so this file
packs the places such a `Figure` sits into a tagged page:

| Figure | Written in                                                 | Encloses                             | After aggressive remediation       |
| ------ | ---------------------------------------------------------- | ------------------------------------ | ---------------------------------- |
| D1     | the `Document`'s `/K` array                                | MCID 1, a drawing                    | stays, with the placeholder `/Alt` |
| D2     | the `/K` array of a `Div` written inline in the same array | MCID 2, a drawing                    | stays, with the placeholder `/Alt` |
| D3     | a `/K` array written as an object of its own, a `Sect`'s   | MCID 3, a drawing                    | stays, with the placeholder `/Alt` |
| D4     | the `Document`'s `/K` array                                | MCID 9, which the page does not mark | removed                            |
| D5     | the `Sect`'s array, beside D3                              | D6 alone                             | removed, with D6                   |
| D6     | D5's `/K` array                                            | the null object                      | removed                            |

The page's `/ParentTree` array names, for each identifier, the nearest element
written as an object of its own. The file agrees with veraPDF 1.30.2 in all
thirteen cells, rule for rule. PDF/UA-2 reports `8.2.5.28.2/1` and PDF/UA-1
`7.3/1`, beside the part-2 claim (`5/2`) and the `%PDF-2.0` header (`6.1/1`).
litho reports each of the six `Figure` elements, and veraPDF one, D4, whose
context names it `0 0 obj`, the number every element written directly has. The
PDF/A cells report the drawings painted in device gray with no output intent
(`6.2.3.3/3` at part 1, `6.2.4.3/4` from part 2) and the missing PDF/A
identification. Parts 1 to 3 add the `pdfuaid` properties no extension schema
declares, and parts 2 and 3 the header (`6.1.2/1`); PDF/A-4f adds the missing
`/EmbeddedFiles`.

Measured on litho and veraPDF 1.30.2 before the fix, remediation to PDF/UA-1,
PDF/UA-2 and WTPDF-1.0 Accessibility, default and aggressive, left all six
findings on litho and the one on veraPDF: the placeholder and the removal
reached only a `Figure` written as an object of its own. Now the aggressive
outputs give D1 to D3 the placeholder, take out D4 to D6, and pass both tools.
The default mode leaves the six findings, as before. Remediation at the six
PDF/A manifest profiles, default and aggressive, leaves nothing on either tool.

### `inlinekids_ua2.pdf`

ISO 32000-1, 14.7.2, Table 323 lets an element's `/K` hold a structure element
directly, with no object number of its own, and lets that inline element's `/K`
list elements that do have one. A `Figure` only an inline element lists takes
alternate text at its own number like any other, and aggressive remediation
takes out one that encloses nothing: its reference leaves the inline element's
`/K` in the object holding that element, or the `/K` array written as an object
of its own the inline element names. No corpus file lists an element of its own
from an inline one, so this file packs the places such a `Figure` sits into a
tagged page:

| Figure | Listed by                                                 | Encloses                             | After aggressive remediation       |
| ------ | --------------------------------------------------------- | ------------------------------------ | ---------------------------------- |
| F1     | the `/K` of an inline `Div`                               | MCID 1, a drawing                    | stays, with the placeholder `/Alt` |
| F2     | the `/K` array of an inline `Part`                        | MCID 9, which the page does not mark | removed                            |
| F3     | a `/K` array of its own an inline `Sect` names            | nothing                              | removed, the array left empty      |
| F4     | the `/K` of an inline `Figure`, itself lacking `/Alt`     | the null object                      | removed, with the inline `Figure`  |
| F5     | the `/K` of an inline `NonStruct` that names it in `/Ref` | nothing                              | stays, with the placeholder `/Alt` |

F5 stays because the inline element naming it in `/Ref` has no object number to
write that entry back at. The page's `/ParentTree` array names the `P` holding
MCID 0 and F1. Each inline holder has a structure type of its own: veraPDF gives
every element written inline the identity `0 0 obj` and reads two inline
elements of one type as one, so with three inline `Div` elements it reported
only the last one's `Figure`. The file agrees with veraPDF 1.30.2 in all
thirteen cells, rule for rule and count for count. PDF/UA-2 reports
`8.2.5.28.2/1` six times and PDF/UA-1 `7.3/1` six times, beside the part-2
claim (`5/2`) and the `%PDF-2.0` header (`6.1/1`). The PDF/A cells report the
rules `directfig_ua2`'s do.

Measured on litho and veraPDF 1.30.2 before the fix, aggressive remediation to
PDF/UA-1 and PDF/UA-2 gave all six `Figure` elements the placeholder: the
removal kept every `Figure` an inline element lists. Now those outputs give F1
and F5 the placeholder, take out F2 to F4 and the inline `Figure`, and pass
both tools, as the placeholder-only outputs did. The default mode leaves the six
findings.

### `fontembed_2b.pdf`

Remediation embeds a font program only when the caller supplies one
(`pdf.WithFontProvider`, `pdf.WithFontFiles`), and the corpus commits none, so
the gate remediates this file without one and every font stays unembedded. The
fonts are the ones a synthetic program built at test time serves, in each place
a font dictionary sits: `TestFontEmbedFixture` in `pkg/pdf` remediates the file
at PDF/A-2b with such programs and pins what each font becomes.

| Font | Written                                             | Kind                                | With the test's programs      |
| ---- | --------------------------------------------------- | ----------------------------------- | ----------------------------- |
| HV   | as an object, named by the page                     | Helvetica, no `/Widths`             | embedded, TrueType            |
| TT   | as an object, named by the page                     | TrueType under WinAnsiEncoding      | embedded, TrueType            |
| CF   | as an object, named by the page                     | non-symbolic Type 1                 | embedded, CFF as `/Type1C`    |
| IN   | inline in the page's direct `/Font` dictionary      | Helvetica-Bold                      | embedded at a new object      |
| FI   | inline in the direct `/Resources` of form `X1`      | TrueType, TT's twin                 | embedded at a new object      |
| SH   | inline in a `/Resources` object pages 2 and 3 share | Helvetica-Oblique                   | embedded once, for both pages |
| JP   | as an object, named by the page                     | Type0 over Adobe-Japan1, `<3042>`   | embedded, CIDFontType2        |
| NO   | as an object, named by the page                     | TrueType no program is supplied for | refused, `ErrFontNotSupplied` |
| WD   | as an object, named by the page                     | TrueType declaring 900, program 667 | refused, `ErrFontWidths`      |

| Rule at PDF/A-2b            | veraPDF | litho |
| --------------------------- | ------- | ----- |
| `6.2.4.3/4` DeviceGray      | 10      | 10    |
| `6.2.11.4.1/1` not embedded | 9       | 9     |
| `6.6.2.1/1` no metadata     | 1       | 1     |

The two tools return the same rule set in all thirteen cells, and the same
count for every rule at every level; at the PDF/UA levels both count the
untagged-content rules once per text show. Remediated without a program at
the first eight manifest profiles, default and aggressive, each output fails the
embedding rule nine times on both tools, beside PDF/UA-1's `7.1/3` and PDF/UA-2's
`8.2.2/1`, the same on both. At PDF/A-1b remediation embeds JP's UniJIS-UCS2-H,
which part 1 requires embedded (ISO 19005-1, 6.3.3.3), so `6.3.3.3/1` no
longer stays beside them. Remediated with the test's
programs at PDF/A-1b, PDF/A-2b (default and aggressive), PDF/A-4 and PDF/UA-1,
the embedding rule falls to two, NO and WD, on both tools, and no rule appears.

### `defaultbox_2b.pdf`

ISO 32000-1, 7.7.3.3, Table 30 makes `/MediaBox` required and inheritable, and
gives it no default. A viewer draws a page that neither states nor inherits a
box it uses on US Letter, and `pdf.Reader.Page` reads it so. A write that gives
such a page a media box, as every rewrite, split, merge, insertion and
remediation does for a page it writes, gives it the first size of the
document's pages, the one most pages have first, each in both orientations,
then the size `pdf.WithDefaultPageSize` names, that holds everything the page
paints, at origin 0 0, and US Letter, what a viewer showed, when none does. No
corpus file holds a page missing its media box among pages a majority size
shares: pdfjs `boundingBox_invalid.pdf`'s two sized pages tie, and the others
are US Letter or smaller than it.

| Page | Its box, or its node's           | A viewer shows | A rewrite writes       |
| ---- | -------------------------------- | -------------- | ---------------------- |
| 1    | A4, 595.28 by 841.89             | A4             | A4                     |
| 2    | A4                               | A4             | A4                     |
| 3    | none                             | US Letter      | A4                     |
| 4    | US Letter                        | US Letter      | US Letter, the control |
| 5    | none, `/CropBox [0 0 590 830]`   | 590 by 792     | A4, showing 590 by 830 |
| 6    | its node's `/MediaBox [0 0 0 0]` | US Letter      | A4, on the node        |
| 7    | A4                               | A4             | A4                     |

Page 5's crop box reaches past US Letter, which a viewer cuts it to, and fits
A4, which the viewer cuts it against once the page has one. Each page draws its
own word at (20, 100), inside every size, in the conforming embedded TrueType
of `ttcmap_2b.pdf`, in DeviceGray under a CMYK OutputIntent, and states its
own `/Resources`.

| Rule at PDF/A-2b               | veraPDF | litho |
| ------------------------------ | ------- | ----- |
| `6.1.13/11` page 6's media box | 1       | 1     |

The two tools return the same rule set in all thirteen cells: `6.1.13/11` at
every part-2 and part-3 level, and the identification, tagging, title and
language rules the part-2, level-B claim draws elsewhere. Remediated at the
first eight manifest profiles, default and aggressive, every output conforms on both
tools. Every remediation but PDF/A-1b's shows every page at A4 but pages 4
and 5: the PDF/A-2 and PDF/A-3 ones repair the node's box to US Letter in an
incremental update and give pages 3, 5 and 6 each a box of its own, and the
PDF/A-2a, PDF/A-4 and PDF/UA ones rewrite the file. PDF/A-1b reports no page
box, so its incremental update writes none of them, and qpdf reads pages 3 and
5 as US Letter, as it reads the source.
Measured on the pinned pdf.js, the full rewrite, the split, the merge and the
insertion show pages 3 and 6 at 595.28 by 841.89 and page 5 at 590 by 830,
where they showed 612 by 792 and 590 by 792 before, and qpdf finds nothing in
any of them.

### `siglock_2b.pdf`

ISO 32000-1, 12.7.4.5 lets an unsigned signature field state what signing it
does and asks: its `/Lock` (Table 233) names the fields the signature turns
read-only, and its seed values (`/SV`, Table 234) choose or require the
subfilter, the reason and the digest. `pdf.WithSignatureField` signs such a
field in place and honours both. No corpus file holds an unsigned signature
field with either, so the `mutate-sign` cell, which signs each unsigned
signature field in a write of its own, reaches every path here.

| Field          | Kind         | Entry                                                       | Signing it                             |
| -------------- | ------------ | ----------------------------------------------------------- | -------------------------------------- |
| `name`         | text         | none                                                        |                                        |
| `group`        | non-terminal | none                                                        |                                        |
| `group.first`  | text         | none                                                        |                                        |
| `group.second` | text         | none                                                        |                                        |
| `lockinclude`  | signature    | `/Lock /Include [(name) (group)]`                           | locks `name` and both kids of `group`  |
| `lockexclude`  | signature    | `/Lock /Exclude [(group.first)]`                            | locks every field but `group.first`    |
| `lockall`      | signature    | `/Lock /All`                                                | locks every field                      |
| `seedmet`      | signature    | `/SV`: a subfilter list, a required reason and digest       | `ETSI.CAdES.detached`, the seed reason |
| `seedunmet`    | signature    | `/SV` requiring `adbe.x509.rsa_sha1`, which litho cannot do | refused, `ErrSeedValueUnmet`           |

`seedmet` lists `adbe.x509.rsa_sha1` first, which litho does not write, so the
subfilter Table 234 picks is the next, `ETSI.CAdES.detached`; it requires the
reason `I approve this document` and a SHA-256 or SHA-512 digest. A locked
field turns read-only through the ReadOnly bit of its `/Ff` (Table 221), which
kids inherit (12.7.3.1): `lockexclude` sets it on `group.second` alone, since
setting it on `group` would reach the excluded `group.first`. Every field is
merged with its widget on the one page; the signature widgets are of zero size,
the text widgets' appearances paint nothing, and the page shows nothing.

The two tools return the same rule set in all thirteen cells: none at PDF/A-2b,
and elsewhere the identification, tagging, title and language rules the
part-2, level-B claim draws, which no signature field changes. Remediated at the
first eight manifest profiles, default and aggressive, every output conforms on both
tools. Under `mutate-sign` the four fields signed in place keep their promises,
read with qpdf, and `seedunmet` is the cell's one documented refusal. Each of the three locked signatures is followed by a signature in every other
unsigned field and in a new one, refused with `ErrSignatureFieldLocked` where
the lock reaches the field, and by a document timestamp that rewrites no field.

### `cmapusecmap_2b.pdf`, `cmapusestream_2b.pdf` and `cmapnotdef_2b.pdf`

An embedded encoding CMap of a Type 0 font builds on a base CMap two ways (ISO
32000-1, 9.7.5): the usecmap operator in its program names a predefined CMap,
and a `/UseCMap` entry in its stream dictionary (Table 120) names a predefined
CMap or an embedded stream. A notdefrange sends a range of codes to one CID
(Adobe Technical Note #5014). veraPDF's `CMap.toCID` reads the program's own
cidchar and cidrange mappings first, then those of its bases in the order they
were added, a `/UseCMap` base last, then the notdef mappings, and sends a code
still unmapped to CID 0 (9.7.6.3). litho resolved none of it: a CMap naming a
base or holding a notdef range left every unmapped code unknown, and a
`/UseCMap` base was never read, so its codes went to CID 0. No corpus file
reaches a base or a notdef range that maps a shown code.

Every font is a CIDFontType2 over the three-glyph TrueType program whose
`/CIDToGIDMap` sends CIDs 1 to 6 to a glyph and stops there. A CID past it,
and CID 0, which `CIDFontType2Program.containsCID` refuses, select no glyph,
which veraPDF reads as absent and as `.notdef`; `GFGlyph` exempts only code 0.

| Fixture            | Font | Encoding CMap                                        | Shows     | CIDs        |
| ------------------ | ---- | ---------------------------------------------------- | --------- | ----------- |
| `cmapusecmap_2b`   | `U1` | `/Identity-H usecmap`, bfchar alone                  | 0005 0009 | 5, 9 absent |
| `cmapusecmap_2b`   | `U2` | `/Identity-H usecmap`, bfchar alone                  | 0001 0006 | 1, 6        |
| `cmapusestream_2b` | `S1` | cidchar 0041 to 5, `/UseCMap` a stream: 0042 to 4    | 0041 0042 | 5, 4        |
| `cmapnotdef_2b`    | `N1` | cidchar 0041 to 5, notdefrange 0000 to 00FF to 0     | 0041 0043 | 5, 0        |
| `cmapnotdef_2b`    | `N2` | cidrange 0000 to 0006, notdefrange 0100 to 01FF to 3 | 0002 0150 | 2, 3        |

`S1`'s base is a stream whose name is no predefined CMap, which 6.2.11.3.3 t3
fails on both tools.

| Rule at PDF/A-2b            | `cmapusecmap_2b` | `cmapusestream_2b` | `cmapnotdef_2b` |
| --------------------------- | ---------------- | ------------------ | --------------- |
| `6.2.11.3.3/3` UseCMap      | 0                | 1                  | 0               |
| `6.2.11.4.1/2` glyph absent | 1                | 0                  | 1               |
| `6.2.11.8/1` `.notdef`      | 1                | 0                  | 1               |

veraPDF 1.30.2 and litho report these counts alike, and the same rule set and
counts in all thirteen cells of each file; at the PDF/UA levels both count the
untagged-content rules once per text show. Before the bases were resolved,
litho missed the two glyph rules on `cmapusecmap_2b` and `cmapnotdef_2b` and
reported both on `cmapusestream_2b`, in every cell.

### `cmappredef_2b.pdf` and `cmapusevert_1b.pdf`

A Type 0 font's `/Encoding` can name a predefined CMap, and a predefined CMap
can build on another through usecmap and declare notdef ranges (ISO 32000-1,
9.7.5.2). veraPDF's `PDCMap` parses the bundled CMap a name denotes with the
parser it runs on an embedded stream, so it follows the chain, applies the
ranges, and sends a code nothing maps to CID 0 (9.7.6.3); PDF.js follows the
chain too (`extendCMap`). Every -V CMap holds only its vertical forms and takes
the rest of its mappings, and its code space, from its -H base. litho read a
named CMap from its own records alone: a -V CMap split no code, so no glyph
rule saw the font, a notdef-range code and a code nothing maps were left
unknown, and remediation declined to embed `/UseCMap /V` because V's own
records map 53 codes. No corpus file embeds the program of a font that names a
predefined CMap, and none names a base through `/UseCMap`.

The fonts are CIDFontType2 over the three-glyph TrueType program, declaring the
Adobe-Japan1 supplement their CMap declares. The `/CIDToGIDMap` sends CIDs 1 to
6 to a glyph, and `P5`'s CID 231 too.

| Fixture          | Font | Encoding                                    | Shows     | CIDs              |
| ---------------- | ---- | ------------------------------------------- | --------- | ----------------- |
| `cmappredef_2b`  | `P1` | `/UniJIS-UCS2-V`                            | 0021 0041 | 2, 34 absent      |
| `cmappredef_2b`  | `P2` | `/UniJIS-UCS2-V`                            | 0021 0022 | 2, 3              |
| `cmappredef_2b`  | `P3` | `/90ms-RKSJ-H`, notdefrange 00 to 1F to 231 | 01        | 231 absent        |
| `cmappredef_2b`  | `P4` | `/UniJIS-UCS2-H`                            | E000      | 0, mapped nowhere |
| `cmappredef_2b`  | `P5` | `/90ms-RKSJ-H`, notdefrange 00 to 1F to 231 | 01        | 231               |
| `cmappredef_2b`  | `P6` | `/EUC-V`                                    | 41 A4A2   | 34, 843 absent    |
| `cmapusevert_1b` | `V1` | embedded, cidchar 2121 to 1, `/UseCMap /V`  | 2121      | 1                 |

| Rule                        | `cmappredef_2b` at PDF/A-2b | `cmapusevert_1b` at PDF/A-1b |
| --------------------------- | --------------------------- | ---------------------------- |
| `6.2.11.4.1/2` glyph absent | 5                           | not in part 1                |
| `6.2.11.8/1` `.notdef`      | 5                           | not in part 1                |
| `6.3.3.3/1` CMap embedded   | not at this level           | 1                            |

veraPDF 1.30.2 and litho report these counts alike, and the same rule set in
all thirteen cells of each file. Before, litho reported neither glyph rule on
`cmappredef_2b` in any cell, and its remediation called the PDF/A-2b output
conforming where veraPDF reports both rules on it. `P6` also extracts as `A`
and U+3042 through EUC-H, where litho read the codes as UTF-16 and returned
two unrelated characters. Remediation of `cmapusevert_1b` at PDF/A-1b now
embeds V flattened over H, with `/WMode 1` in its dictionary and program, and
both tools find the output conforming; before, 6.3.3.3 stayed. At PDF/A-1a and
1b, remediation of `cmappredef_2b` embeds each font's named CMap, rendered from
the bundled data: the codespaces as declared, so 90ms-RKSJ-H keeps its one-
and two-byte codes, the notdefrange, and the -V CMaps flattened over their -H
base. `6.3.3.3/1` falls from 6 to 0 on both tools, default and aggressive, and
no rule appears; `6.3.5/1` (5) stays on both.

### `cmapverabase_2b.pdf`

veraPDF's `PDCMap` loads the CMap file it bundles under a name, whether a
font's `/Encoding`, a usecmap operand or a `/UseCMap` entry names it, and
decodes the font's codes through it (ISO 32000-1, 9.7.5.2 and 9.7.5.4). Its
bundle (veraPDF-parser `font/cmap`) holds 118 CMaps. Fifteen are missing from
the PDF.js copies litho reads for text: the character-collection CMaps of the
newest supplements (Adobe-CNS1-7, Adobe-GB1-6, Adobe-Japan1-7, Adobe-Japan2-0,
Adobe-KR-0 to Adobe-KR-9, each an identity over the collection's CIDs) and
UniAKR-UTF16-H. Eight more map other codes than PDF.js's: GBK2K-H, the UniGB
UTF-8, UTF-16 and UTF-32 CMaps, UniCNS-UTF16-H, UniJIS-UTF16-H and -V add
mappings, and UniKS-UTF16-H holds three fewer. litho left a base it did not
bundle unresolved and its codes unknown, did not decode an `/Encoding` naming
such a CMap at all, and read the eight through PDF.js's mappings. No corpus
file names a base only veraPDF bundles, and none shows a code where the copies
differ.

Every font is a CIDFontType2 over the three-glyph TrueType program, whose
`/CIDToGIDMap` sends CIDs 1 to 6 to a glyph; `K6`'s also sends 7716 and `K7`'s 4419.

| Font | Encoding                          | Shows     | CIDs              |
| ---- | --------------------------------- | --------- | ----------------- |
| `K1` | `/Adobe-KR-9 usecmap`, bfchar     | 0005 0009 | 5, 9 absent       |
| `K2` | `/Adobe-KR-9 usecmap`, bfchar     | 0001 0006 | 1, 6              |
| `K3` | `/Adobe-Japan1-7 usecmap`, bfchar | 5A20      | 0, past CIDCount  |
| `K4` | `/UseCMap /Adobe-GB1-6`, bfchar   | 0007      | 7 absent          |
| `K5` | `/UniAKR-UTF16-H`                 | 0041      | 34 absent         |
| `K6` | `/UniGB-UTF16-H`                  | 2002      | 7716              |
| `K7` | `/UniKS-UTF16-H`                  | FA2E      | 0, mapped nowhere |

| Rule at PDF/A-2b            | litho before | litho now and veraPDF |
| --------------------------- | ------------ | --------------------- |
| `6.2.11.4.1/2` glyph absent | 1, on `K6`   | 5                     |
| `6.2.11.8/1` `.notdef`      | 1, on `K6`   | 5                     |

Before, litho read `K6`'s U+2002 as unmapped through PDF.js's UniGB-UTF16-H, a
false positive, and missed the other five, so the rule sets agreed by chance.
Now both tools report the same rule set in all thirteen cells; the one count
that differs is under [Known divergences](#known-divergences). `K4`'s and
`K5`'s names are no CMap of Table 118, which `6.2.11.3.3/3` and `/1` report, and
veraPDF knows no CIDSystemInfo for UniAKR-UTF16-H, which `6.2.11.3.1/1`
reports. Remediation used to drop `K4`'s `/UseCMap` as naming nothing, which
sent code 0007 to CID 0, and left the operators of `K1` to `K3` in place, so
litho kept `6.2.11.3.3/3` after it at eleven levels where veraPDF did not. It
now inlines every such base from veraPDF's copy, each code keeping its CID.
It also embeds the file veraPDF reads for `K5`'s `/UniAKR-UTF16-H`, byte for
byte with the Adobe-KR-9 CIDSystemInfo the file declares, since PDF/A requires
a CMap outside Table 118 embedded and PDF.js ships none. That settles `K5`'s
embedding and CIDSystemInfo rules (`6.2.11.3.3/1` and `6.2.11.3.1/1` and their
restatements) on both tools with no rule added, default and aggressive, at all
thirteen levels, and both tools then report the same rules and counts on every
output. Part 1 requires even a Table 118 CMap embedded (ISO 19005-1,
6.3.3.3), so at PDF/A-1a and 1b remediation also embeds veraPDF's files for
`K6`'s and `K7`'s names, byte for byte. `6.3.3.3/1` and `6.3.3.1/1` (2 each)
and, at PDF/A-1a, `6.3.8/1` (2) then clear on both tools, default and
aggressive, and no rule appears.

The Unicode rules (`6.2.11.7.2/1` at A-2a, A-2u, A-3a, A-3u, `7.21.7/1`,
`8.4.5.8/1`, `6.3.8/1` at A-1a) once counted `K1` and `K2` on litho only, and
after the embedding `K5` too: the clauses exempt the Adobe-GB1, Adobe-CNS1,
Adobe-Japan1 and Adobe-Korea1 collections by name, and veraPDF maps the
glyphs of Adobe-KR, Adobe's successor Korean collection, through its
Adobe-KR-UCS2 CMap as well. litho now takes the collection where veraPDF's
PDType0Font does, from the CIDFont under Identity-H and from the encoding CMap
otherwise, and exempts every collection whose Adobe-<Ordering>-UCS2 CMap
veraPDF bundles, so the counts agree.

### `cmapcollect_1b.pdf`

A character collection has a CMap of its own, named for it: Adobe-Korea1-2 is
an identity over the Adobe-Korea1 CIDs of supplement 2, declaring that
CIDSystemInfo. The name lies outside ISO 32000-1, 9.7.5.2, Table 118, but
veraPDF 1.30.2 bundles the file, and so do PDF.js's bcmaps, so both decode a
font's codes through it. veraPDF takes a named CMap's CIDSystemInfo from its
CharacterCollections table, which lists only the Table 118 names, so the name
fails the CIDSystemInfo rule as well as the embedding rule at every level. The
verapdf corpus holds the shape in four files (`6-2-11-3-3-t01-fail-a`,
`6-2-10-3-3-t01-fail-a`, `7.21.3.3-t01-fail-a`, `8.4.5.4-t01-fail-a`), at
PDF/A-2b, PDF/A-4 and PDF/UA only, which the gate never remediates at PDF/A-1.

The font is a CIDFontType2 over Adobe-Korea1-2 and the three-glyph TrueType
program, whose `/CIDToGIDMap` sends CIDs 1 to 6 to a glyph.

| Font | Encoding          | Shows     | CIDs |
| ---- | ----------------- | --------- | ---- |
| `C1` | `/Adobe-Korea1-2` | 0001 0002 | 1, 2 |

| Level      | CMap embedded  | CIDSystemInfo compatible |
| ---------- | -------------- | ------------------------ |
| A-1a, A-1b | `6.3.3.3/1`    | `6.3.3.1/1`              |
| A-2, A-3   | `6.2.11.3.3/1` | `6.2.11.3.1/1`           |
| A-4        | `6.2.10.3.3/1` | `6.2.10.3.1/1`           |
| UA-1       | `7.21.3.3/1`   | `7.21.3.1/1`             |
| UA-2       | `8.4.5.4/1`    | `8.4.5.3.1/1`            |

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells.
Remediation used to leave the name as written, judging that it denoted no
CMap, so both rules stayed on both tools at every level, default and
aggressive, and no output was fully fixed. It now embeds the CMap rendered
from the bundled data, with the CIDSystemInfo Adobe Korea1 2 the file
declares, every code keeping its CID. Both rules clear on both tools, with the
Unicode rule (`6.3.8/1`, `6.2.11.7.2/1`, `7.21.7/1`, `8.4.5.8/1`) where the
level has one, and both tools find 24 of the 26 outputs conforming; at
PDF/A-4f the two keep `6.9/5`, the embedded file that level requires.

### `cmapcidtext_2b.pdf`

A Type 0 font's encoding CMap sends a character code to a CID (ISO 32000-1,
9.7.6.2), and a font with no `/ToUnicode` CMap reads its text through tables
keyed by CID: the character collection's UCS2 CMap (9.10.2), or the
`/CIDToGIDMap` stream with the embedded program's `cmap` behind it. litho read
both tables at the code, which is the CID only under Identity-H and Identity-V
(9.7.5.2), and left a reference-only Adobe font under an embedded CMap stream
without the collection's table at all. No corpus file shows text this way
without a `/ToUnicode` CMap answering first. Both fonts embed a one-byte code
space and show <61> <62>:

| Font | Encoding CMap              | CIDs   | Route                                      | Text |
| ---- | -------------------------- | ------ | ------------------------------------------ | ---- |
| `G1` | cidchar 61 to 2, 62 to 1   | 2, 1   | `/CIDToGIDMap` 1 to A, 2 to B, then `cmap` | BA   |
| `G2` | cidchar 61 to 35, 62 to 34 | 35, 34 | Adobe-Japan1 UCS2, reference-only font     | BA   |

Before, litho extracted U+6162 for `G1`, the two bytes read as one CID, and
"ab" for `G2`. `G1` embeds the three-glyph program, so its glyph rules stay
silent; `G2` fails the embedding rules (6.3.4 t1, 6.2.11.4.1 t1, 6.2.10.4.1
t1). veraPDF 1.30.2 and litho report the same rule set in all thirteen cells,
with the same counts at every level; at the PDF/UA levels both count the
untagged-content rules once per text show. The text change moves no
validation or remediation result: litho reports and remediates the file alike
before and after.

### `ttmaccopy_2b.pdf`

ISO 32000-1, 9.6.6.4 has a symbolic TrueType font with no `/Encoding` and no
(3,0) subtable select its glyphs through the (1,0) Macintosh subtable, whose
codes are Mac OS Roman characters. Some producers write the (3,1) mapping under
the (1,0) record instead, so the "Mac" table names nothing of its own. litho
read such a copy as Mac OS Roman; it now reads a symbolic font whose (1,0)
subtable maps every code it covers in 0x80-0xFF to the glyph (3,1) maps the
same value to through the glyphs its codes select, and keeps Mac OS Roman for
a genuine table. pdfjs `issue15910` is the one corpus file that reaches the
construct. Both fonts embed the three-glyph program with the glyphs named
Adieresis and adieresis:

| Font | (1,0) subtable             | (3,1) subtable             | Shows | Text |
| ---- | -------------------------- | -------------------------- | ----- | ---- |
| `M1` | C4 Adieresis, E4 adieresis | C4 Adieresis, E4 adieresis | C4 E4 | Ää   |
| `M2` | 80 Adieresis, 8A adieresis | C4 Adieresis, E4 adieresis | 80 8A | Ää   |

Before, litho extracted "ƒ‰" for `M1`. `M2` is the control: any reading but
Mac OS Roman gives "€Š". Both programs declare two subtables and no (3,0), so
6.2.11.6 t4 fires once per font, and neither declares `/Encoding`, so t3 stays
silent. veraPDF 1.30.2 and litho report the same rule set in all thirteen
cells, with the same counts at every level; at the PDF/UA levels both count the
untagged-content rules once per text show. The text change moves no
validation result. Remediation writes a `/ToUnicode` for both fonts: `M1`'s
from the glyphs its codes select, `M2`'s from the Mac OS Roman reading its post
names confirm. At the seven levels that require a Unicode mapping, veraPDF
1.30.2 and litho then both drop the rule for `M2` as they do for `M1`.

### `stdremap_ua2.pdf` and `remapchain_ua2.pdf`

ISO 14289-2, 8.2.4 test 4 reports a structure element whose type is standard
in the namespace it belongs to and whose role mapping leads to no standard type.
A mapping is followed to its end (ISO 32000-2, 14.7.4 and 14.8.6), through the
structure tree root's `/RoleMap` for an element in no namespace or in a PDF 1.7
namespace without a `/RoleMapNS`, and through the `/RoleMapNS` of the element's
namespace otherwise. No corpus file maps a standard type to a private one that a
PDF/UA-2 reading reaches, so these two files do, each under a root `Document` in
the PDF 2.0 namespace:

| File             | Element   | Namespace | Mapping                                               | Read at PDF/UA-2 |
| ---------------- | --------- | --------- | ----------------------------------------------------- | ---------------- |
| `stdremap_ua2`   | `Sect`    | PDF 1.7   | `/RoleMap` `Sect` to `Book`, which nothing maps       | `8.2.4/4`        |
| `stdremap_ua2`   | `Sect`    | PDF 2.0   | none: the `/RoleMap` does not govern it               | `Sect`           |
| `remapchain_ua2` | `Div`     | none      | `/RoleMap` `Div` to `Section`, `Section` to `Sect`    | `Sect`           |
| `remapchain_ua2` | `Sect`    | PDF 1.7   | `/RoleMapNS` to a custom `Section`, which maps `Sect` | the 2.0 `Sect`   |
| `remapchain_ua2` | `Callout` | custom    | `/RoleMapNS` to the PDF 2.0 `Aside`                   | `Aside`          |

Both files agree with veraPDF 1.30.2 on the rule set in all thirteen cells and
both WTPDF levels. PDF/UA-2 reports the PDF 1.7 `Sect` of `stdremap_ua2` once
on each tool and nothing on `remapchain_ua2`; the WTPDF levels add their missing
declaration (`6.1.3/1`). PDF/UA-1 and the level-A cells read both files as PDF
1.7, where no namespace counts: the `/RoleMap` reaches both `Sect` elements of
`stdremap_ua2`, which PDF/UA-1 reports as `7.1/7` and parts 2 and 3 at level A
as `6.7.3.4/3`, and it does not map `Callout`, which PDF/UA-1 reports as
`7.1/5`, PDF/A-1a as `6.8.3.4/1` and parts 2 and 3 at level A as `6.7.3.4/1`.
PDF/UA-1 also reports the `Div` of `remapchain_ua2` as `7.1/7`, since ISO
14289-1 forbids remapping a standard type at all. The other cells report what
`rolemapscope_ua2.pdf` reports. The counts differ in one respect: litho reports
`7.1/7` and `6.7.3.4/3` once per `/RoleMap` entry, veraPDF once per element, so
on `stdremap_ua2` veraPDF counts two checks where litho reports one.

Measured on litho and veraPDF 1.30.2 before the fix, litho judged 8.2.4 test 4
on the first hop of each mapping and only for an element in no namespace: it
missed the PDF 1.7 `Sect` of `stdremap_ua2` and reported both the `Div` and the
PDF 1.7 `Sect` of `remapchain_ua2`, and at PDF/A-2a and PDF/A-3a it reported
the `Div` as `6.7.3.4/3`. Remediation at the manifest profiles and both WTPDF
levels, default and aggressive, leaves the same rule set on both tools. Before
the repair that set was `8.2.4/4` on `stdremap_ua2` at PDF/UA-2 and both WTPDF
levels; now remediation drops the `/RoleMap` entry that leads `Sect` nowhere,
the PDF 1.7 `Sect` reads as a `Sect`, and both tools report nothing there.

### `imagecolour_2b.pdf`

Two image colours no corpus file states. A Separation space over the colorant
None, and a DeviceN space naming only None, never produce visible output (ISO
32000-1, 8.6.6.4 and 8.6.6.5): litho decodes such an image fully transparent,
and a redaction that clears part of it keeps the space and its tints, where
writing the transparent decode back as DeviceRGB painted the placement black.
A JPXDecode image without `/ColorSpace` takes its colour from the JPEG 2000
data (ISO 32000-1, 7.4.9): litho reads enumerated CIELab, with and without EP
fields (ISO/IEC 15444-2, M.11.7.4.1), and ROMM-RGB (ISO 22028-2) as colour,
and e-sRGB, whose integer encoding the JPEG 2000 text leaves to PIMA 7667, in
DeviceRGB by its channel count.

| Image     | Colour                                    | Decodes as              |
| --------- | ----------------------------------------- | ----------------------- |
| `SepNone` | `[/Separation /None /DeviceGray f]`       | transparent             |
| `DevNone` | `[/DeviceN [/None /None] /DeviceGray f2]` | transparent             |
| `JLabDef` | JPX, enumerated CIELab, no EP fields      | sRGB (0, 213, 255)      |
| `JLabEP`  | JPX, enumerated CIELab, EP fields, D50    | sRGB (0, 223, 255)      |
| `JROMM`   | JPX, enumerated ROMM-RGB                  | sRGB (255, 15, 35)      |
| `JESRGB`  | JPX, enumerated e-sRGB                    | DeviceRGB (200, 80, 40) |

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells. No
rule reads the colorant name or the enumerated JPX space beyond CIEJab (ISO
19005-2, 6.2.8.3 test 4); the sets are the DeviceGray alternates without an
output intent (`6.2.4.3/4`), the missing metadata and the tagging rules.

### `redactrefs_ua1.pdf` and `redactcopies_2b.pdf`

Redaction clears the copies of removed text a file holds outside the page, then
scans what a full rewrite writes for every removed string and refuses when one
survives. Neither half had a fixture: the `mutate-redact` cell redacts all of
page 1, and no file in the corpus quoted its page 1 text behind an object the
scrub cannot read, or where no decoded value spells it. Both fixtures draw
`the secret plan` on page 1 and `page two` on page 2.

`redactrefs_ua1` holds two copies behind objects the scrub cannot read, and a
rewrite copies both. The structure element of page 1's paragraph holds the words
in `/ActualText`, and the `/ParentTree` slot for its MCID is null, so the entry
names no element; the element is still in the tree, reached from the root's `/K`
with `/Pg` naming page 1 (ISO 32000-1, 14.7.4). The outline's `/First` is a
number rather than an outline item, and its `/Last` is the item whose title
quotes the paragraph (Table 153). The redaction now finds the element through
`/K` and the item through `/Last` and `/Prev`, and the output holds neither
copy. Before, it refused, reading the null slot as an element written directly
into the array.

`redactcopies_2b` holds two copies a rewrite copies and no decoded value spells.
The catalog's `/PieceInfo` holds a FlateDecode stream that decodes to other
words, with the paragraph stored after its zlib checksum (RFC 1950, 2.2), and
the catalog's `/Dests` names a destination after the paragraph (12.3.2.3).
Before, the redaction succeeded and wrote both out, which pikepdf reads back as
the stored stream data and a name key. Now it refuses with
`ErrRedactionIncomplete` and names the `/Dests` survivor. The `/PieceInfo` copy
goes before the scan: redaction clears the private data of the catalog's
`/PieceInfo`, keeping its `/LastModified` (see `redactvectors_4`).

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells of
each file.

### `redactvectors_4.pdf`

Redaction proves its output clean by comparing what a rewrite writes with every
removed string. Four copies of page 1's paragraph, `the secret plan`, sit where
that comparison cannot see them, and before, redacting page 1 wrote every one
out and reported success:

| Copy                                                                                  | Clause                       | Now                |
| ------------------------------------------------------------------------------------- | ---------------------------- | ------------------ |
| `/OpenAction` and page 1's `/AA /O` build the paragraph from `'the sec' + 'ret plan'` | ISO 32000-1, 12.6.4.16       | script cut         |
| `/Names /JavaScript` runs a script held in a UTF-16BE text stream                     | ISO 32000-1, 7.9.3           | tree removed       |
| Page 1's `/Metadata` carries a page thumbnail as base64 (`xmpGImg:image`)             | ISO 32000-1, 14.3.2          | entry removed      |
| The catalog's `/PieceInfo` holds a stream with no filter whose bytes are zlib data    | ISO 32000-1, 14.5, Table 368 | `/Private` removed |

Two attachments hold no copy and stay: a zip archive and a PDF whose content
stream is Flate-compressed, both about page two. The scan reads into each, a
zip's members and an attached PDF's objects, and finds nothing. pikepdf reads
none of the four copies back from the output, and both attachments. No other
fixture puts a copy of page 1's text in any of these homes. Page 2 draws
`page two`.

PDF/A-4 permits JavaScript actions (ISO 19005-4, 6.6.1), so at PDF/A-4 only the
page's `/AA /O` trigger fires (6.6.3 test 1), and 6.9 test 3 fires for the
attachments, neither a PDF/A file. veraPDF 1.30.2 and litho report the same
rule set in all thirteen cells.

### `rolecycle_ua1.pdf` and `rolecycle_ua2.pdf`

A circular role mapping is forbidden by ISO 14289-1, 7.1 test 6, ISO 14289-2,
8.2.4 test 2 and ISO 19005-1/2/3, 6.8.3.4/6.7.3.4 test 2. veraPDF 1.30.2 judges
it per structure element. It follows the root `/RoleMap` from the element's
type name, whatever namespace the element declares, without stopping on a
standard type, and reports a chain that revisits a name. In a PDF 2.0 document
it also follows the `/RoleMapNS` chain from the element's type and reports one
that returns to the type it left a namespace from; a chain that comes back to a
different type of a namespace is ISO 14289-2, 8.2.4 test 3 instead. PDF/UA
judges every element. PDF/A judges only an element that reaches no standard
type, and PDF/A-1 judges a standard type by its name alone. No corpus file puts
a standard type in a cycle, so these two files do:

| File            | Element   | Namespace | Mapping                                       | Read at PDF/UA |
| --------------- | --------- | --------- | --------------------------------------------- | -------------- |
| `rolecycle_ua1` | `Sect`    | none      | `/RoleMap` `Sect` to `Book`, `Book` to `Sect` | circular       |
| `rolecycle_ua1` | `Item`    | none      | `/RoleMap` `Item` to `Sect`, into that cycle  | circular       |
| `rolecycle_ua1` | `Div`     | none      | none                                          | `Div`          |
| `rolecycle_ua2` | `Sect`    | none      | `/RoleMap` `Sect` to `Book`, `Book` to `Sect` | circular       |
| `rolecycle_ua2` | `Callout` | custom    | `/RoleMapNS` to a second namespace and back   | circular       |
| `rolecycle_ua2` | `Panel`   | custom    | `/RoleMapNS` `Panel` to `Frame` and back      | `8.2.4/3`      |
| `rolecycle_ua2` | none      | PDF 2.0   | `/RoleMapNS` `Note` to itself, no element     | nothing        |

Both files agree with veraPDF 1.30.2 on the rule set in all thirteen cells and
both WTPDF levels, and on the count of every circular-mapping check. PDF/UA-1
reports `7.1/6` on `Sect` and `Item` of `rolecycle_ua1`, PDF/UA-2 and both WTPDF
levels `8.2.4/2` on the same two; parts 2 and 3 at level A report `6.7.3.4/2` on
the `Sect` alone, whose chain closes on itself before meeting a standard type,
while the chain of `Item` stops on `Sect`. At PDF/UA-2 `rolecycle_ua2` reports
`8.2.4/2` on `Sect` and `Callout`, `8.2.4/3` and `8.2.4/1` on `Panel`, which
the same-namespace cycle leaves mapped to no standard type, and nothing on the
unused `Note` entry. The counts differ only where `stdremap_ua2` says: litho
reports `7.1/7` once per `/RoleMap` entry.

Measured before the fix, litho judged the circular rules on `/RoleMap` entries
whose chain met no standard type and on `/RoleMapNS` self-maps: it reported no
circular mapping on either file's `Sect` or `Item`, `8.2.4/2` on the unused
`Note` entry, and `8.2.4/3` on all four entries of the two namespace cycles, and
it missed `8.2.4/1` on `Panel`. Over every corpus, the tagged profiles change
no rule set on any other file, and the circular-mapping counts change on 28
files, one per element where litho counted entries; on the eight of them
measured against veraPDF, `pdfjs` `issue16759` (1043 elements) among them, the
counts now match.
Remediation breaks the root `/RoleMap` cycle at its standard type: it drops the
`Sect` entry, so `Book` leads to `Sect` and a `Sect` stays one. Before, it sent
`Book` to `P`, so at PDF/UA-2 the `Sect` of `rolecycle_ua2` read as a `P`. Both
tools find every remediated `rolecycle_ua1` clean. Remediation also breaks the
`/RoleMapNS` loops an element closes, by the same rule: a loop through a
standard type loses that type's entry, and a loop of private types has its
closing entry sent to a standard namespace the chain has not left. `Box` and
`Panel` of `rolecycle_ua2` now map to the PDF 2.0 `Div`, since neither element
holds content directly, and the unused `Note` entry stays. Both tools find every
remediated `rolecycle_ua2` clean at PDF/UA-2 and both WTPDF levels, default and
aggressive; before, all six outputs kept `8.2.4/1`, `8.2.4/2` and `8.2.4/3` on
both tools.

### `type0nodesc_2b.pdf`

veraPDF builds a Type 0 font over its descendant's dictionary and reads the
font's `/Type` and `/BaseFont` there (PDType0Font.getDescendantCOSDictionary,
veraPDF 1.30.2). When the first entry of `/DescendantFonts` is no dictionary, or
the entry is absent, empty or not an array, it substitutes an empty dictionary,
so the font fails the `/Type` test (`6.3.2/1`, `6.2.11.2/1`, `6.2.10.2/1`) as
well as the `/BaseFont` test (`t3`), although its own dictionary writes both.
litho stated `t3` alone. The file shows one code through each of five fonts:

| Font | `/DescendantFonts`                    | Verdict             |
| ---- | ------------------------------------- | ------------------- |
| `C1` | an array of a CIDFontType2 dictionary | silent, the control |
| `N1` | `[ null ]`                            | `t1` and `t3`       |
| `N2` | absent                                | `t1` and `t3`       |
| `N3` | `[ ]`                                 | `t1` and `t3`       |
| `N4` | a CIDFontType2 dictionary, no array   | `t1` and `t3`       |

litho and veraPDF 1.30.2 report the same rule set in all thirteen cells, and
both fail `t1` and `t3` four times at every PDF/A part. Over every corpus only
`pdfjs` `issue12823`, outside the gated corpora, writes such a font.

### `fnlimits_1b.pdf`

A type 4 function writes its program as a stream of numbers (ISO 32000-1,
7.10.5), which the implementation limits of ISO 19005-1, 6.1.12 and ISO
19005-2, 6.1.13 constrain like every other number. veraPDF parses the program
of every type 4 function its model reaches, one CosInteger or CosReal per
number, and reaches a function through a Separation or DeviceN tint transform,
a graphics state's `/TR` and `/TR2`, a halftone's `/TransferFunction` and a
stitching function's `/Functions`, but never through a shading's `/Function`.
litho judged no function program at all.

| Function | Reached as                                                     | Verdict at PDF/A-1b |
| -------- | -------------------------------------------------------------- | ------------------- |
| `A`      | tint transform of the Separation `/Spot` that `cs` selects     | `t1` and `t2`       |
| `B`      | `/TransferFunction` of the `/Spot` component of a type 5 `/HT` | `t2`                |
| `E`      | inside the direct stitching tint transform of `/Two`           | `t1`                |
| `C`      | `/Function` of the shading `sh` paints                         | silent              |
| `F`      | tint transform of a Separation nothing selects                 | silent              |

Each counts once for the whole document. At PDF/A-2b and PDF/A-3b only `A` and
`E` fail, the integer limit; part 4 states no limit. The direct stitching tint
transform of `/Two` also exposed a false `6.2.4.4/2`: litho compared it with
itself at every link and found it different, since the comparison read no
stream, so the one Separation failed three times. litho and veraPDF 1.30.2 now
report the same rule set in all thirteen cells, and the same `6.1.12` and
`6.1.13` counts. No corpus file holds a type 4 function past a limit, nor a
direct tint transform naming another object.

### `oirgb_2b.pdf`

The document's output intent holds an RGB monitor profile of the sRGB colorants
over a 1.8 gamma, and the page draws a DeviceRGB image, an Indexed image over
DeviceRGB and a DeviceGray image. An output intent's destination profile
characterises the device a document was prepared for (ISO 32000-1, 14.11.5),
and ISO 19005-2, 6.2.4.3 admits DeviceRGB on the strength of an RGB destination
profile, so litho reads DeviceRGB, the Indexed base included, through it, as it
reads DeviceCMYK through a CMYK one. DeviceGray stays an sRGB grey: the profile
is not a Gray profile. Every RGB destination profile in the other corpora is
sRGB, which gives each colour back as it came, so no other file shows the
reading.

| Image  | Colour                    | Decodes as                     |
| ------ | ------------------------- | ------------------------------ |
| `RGB`  | `/DeviceRGB`, blue 128    | blue 146, through the intent   |
| `Idx`  | Indexed over `/DeviceRGB` | (200, 80, 40) as (210, 99, 53) |
| `Gray` | `/DeviceGray`             | the grey levels as they are    |

MuPDF 1.26.7 decodes every pixel of the three images as litho does, and as it
does the samples themselves with ICC off. PDF.js reads no output intent.

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells: the
missing metadata and the tagging rules. No colour rule fires, since every
device space the page uses has an output intent to stand on.

### `ocgconfigs_2b.pdf`

ISO 19005-2, 6.9 t3 and ISO 19005-4, 6.10 t3 require the `/Order` of any
optional content configuration dictionary to reference every OCG in the file,
and veraPDF tests the default `/D` and every `/Configs` entry (ISO 32000-1,
Table 100). litho read `/D` alone. The only other incomplete `/Configs`
`/Order`, in `complex_ua2`, sits beside a `/D` that fails too, so the rule
fired there anyway and nothing isolated the alternate configurations.

| Configuration      | `/Order`                    | Verdict |
| ------------------ | --------------------------- | ------- |
| `/D` `Default`     | both OCGs                   | silent  |
| `/Configs[0]`      | `Shown` alone               | `6.9/3` |
| `/Configs[1]`      | both, in a `(Layers)` group | silent  |
| `/Configs[2]`, ref | empty                       | `6.9/3` |

veraPDF 1.30.2 reports `6.9/3` twice from part 2 and `6.10/3` twice at part 4,
one check per failing configuration, and litho now does the same: the two
tools report the same rule set and the same counts in all thirteen cells. The
rest of each cell is the profile the PDF/A-2b claim does not match: the
identification of another part or level, the missing tagging, the PDF/A-4
identification and `%PDF-1.7` header, PDF/A-4f's missing `/EmbeddedFiles`,
and at part 1 the optional content it forbids (`6.1.13/1`).

PDF/A remediation appends the OCGs each `/Order` leaves out. veraPDF 1.30.2
finds the output fully conforming at PDF/A-2b and PDF/A-4 in both modes, and at
PDF/A-3b in the default mode.

The repair also names an OCG that carries no `/Name`, which Table 98 requires.
veraPDF matches `/Order` to the OCGs by `/Name`, and a nameless OCG enters its
set as an empty name no reference removes, which is why `complex_2b`'s
completed `/Order` stayed reported until its OCG was named.

### `nestedpattern_2b.pdf`

ISO 19005-2, 6.2.2 test 2 fails a content stream that resolves a name only
through the page's resources. A tiling pattern's cell is a content stream with
a `/Resources` entry of its own (ISO 32000-1, 8.7.3.1, Table 75), and an `scn`
whose last operand is a name selects a pattern (8.6.8, Table 74). veraPDF
1.30.2 fails test 2 on a cell that finds `/GS0` only on the page, with its
`/Resources` empty or absent alike, and on a form XObject whose `/P3 scn` does.
No corpus file reaches either construct, and litho's remediation gave form
XObjects and Type 3 fonts the names they borrow, never a pattern cell, nor the
pattern a form selects:

| Object | Construct                                          | Answers   |
| ------ | -------------------------------------------------- | --------- |
| `P0`   | tiling pattern, `/Resources << >>`, `/GS0 gs`      | `6.2.2/2` |
| `P1`   | tiling pattern, no `/Resources`, `/GS1 gs`         | `6.2.2/2` |
| `P2`   | tiling pattern holding its own `/GS0`              | silent    |
| `X0`   | form XObject, `/Resources << >>`, `/P3 scn`        | `6.2.2/2` |
| `X1`   | form XObject holding its own `/P3`                 | silent    |
| `P3`   | tiling pattern the two forms paint, naming nothing | silent    |

`P2` and `X1` are the over-repair controls. Test 2 fails three times, once for
each of `P0`, `P1` and `X0`. A CMYK OutputIntent covers the initial DeviceGray
every cell paints in and the file draws no text, so at PDF/A-2b test 2 is the
whole firing set. veraPDF 1.30.2 and litho report the same rule set in all
thirteen cells, with the same counts at every PDF/A level; at the PDF/UA levels
veraPDF counts the untagged-content rule once per painted path.

PDF/A remediation copies each borrowed name into the stream that uses it and
leaves `P2` and `X1` as they are. veraPDF 1.30.2 finds the output fully
conforming at PDF/A-2a, PDF/A-2b, PDF/A-2u, PDF/A-3b and PDF/A-4 in both
modes, where it reported test 2 three times before.

### `ttnoenc_2b.pdf`

ISO 32000-1, 9.6.6.4 reads a TrueType font "when the font has no Encoding
entry, or the font descriptor's Symbolic flag is set" through its (3,0)
subtable, else its (1,0) one, and leaves any other mapping to the processor.
veraPDF 1.30.2 takes no other mapping for glyph presence
(`TrueTypeFontProgram.containsCode`), so a program offering neither subtable
holds no glyph for any code, even when its (3,1) subtable maps the code. Its
widths still fall back to any subtable, and the glyph has no name, so its
Unicode value comes from `/ToUnicode` alone. litho read a font with no
`/Encoding` through its (3,1) subtable and a symbolic one without a symbol
subtable through whatever its `cmap` offered, so it found every glyph:

| Font    | Construct                                     | Answers at PDF/A-2b          |
| ------- | --------------------------------------------- | ---------------------------- |
| `N31`   | non-symbolic, no `/Encoding`, (3,1) only      | `6.2.11.4.1/2`, `6.2.11.6/2` |
| `N3110` | non-symbolic, no `/Encoding`, (3,1) and (1,0) | `6.2.11.6/2`                 |
| `N10`   | non-symbolic, no `/Encoding`, (1,0) only      | `6.2.11.6/2`                 |
| `N30`   | non-symbolic, no `/Encoding`, (3,0) only      | `6.2.11.6/1`, `6.2.11.6/2`   |
| `S31`   | symbolic, no `/Encoding`, (3,1) only          | `6.2.11.4.1/2`               |

Each font draws codes 65 and 66 and carries matching `/Widths`, so the width
rule stays silent. No font carries `/ToUnicode`, so at the Unicode levels every
code of all five fails `6.2.11.7.2/1` and its restatements on both tools: the
name the program gives none of them holds a Unicode value. veraPDF 1.30.2 and
litho report the same rule set in all thirteen cells, with the same counts.

`builtinProgramEncoding` reads no encoding out of a `/FontFile2` on purpose:
the program's built-in encoding is a `cmap` to glyphs, not to names.

### `sharednested_2b.pdf`

ISO 19005-2, 6.2.2 test 2 fails a form XObject or a tiling pattern cell that
resolves a name only through the page's resources. When two pages draw the
same stream, each may bind that name to a different object, so a repair that
copies one page's binding into the shared stream repaints the other page with
it. litho's remediation left every shared form and pattern alone, and no corpus
file draws one from two pages:

| Object | Construct                                     | The two pages bind its name |
| ------ | --------------------------------------------- | --------------------------- |
| `X0`   | form XObject, `/Resources << >>`, `/GS0 gs`   | apart                       |
| `X1`   | form XObject, `/Resources << >>`, `/GS1 gs`   | alike                       |
| `P0`   | tiling pattern, `/Resources << >>`, `/GS0 gs` | apart                       |

Page 1 binds `/GS0` to a line width of 2, page 2 to 4, and both bind `/GS1` to
one object. The pages share one indirect `/XObject` dictionary. Test 2 fails
three times, once for each stream, judged under page 1's resources. A CMYK
OutputIntent covers the DeviceGray every stream paints in and the file draws
no text, so at PDF/A-2b test 2 is the whole firing set. veraPDF 1.30.2 and
litho report the same rule set in all thirteen cells, with the same counts.

PDF/A remediation gives `X1` the name in place, since both pages bind it
alike. `X0` and `P0` keep their own dictionaries: each page draws a copy
carrying its own `/GS0`, and each page gets its own copy of the shared
`/XObject` dictionary. veraPDF 1.30.2 finds the output fully conforming at
PDF/A-2b, PDF/A-3b and PDF/A-4 in the default mode and at PDF/A-2b and PDF/A-4
in the aggressive one, where it reported test 2 three times before.

### `symbolname_2u.pdf`

ISO 19005-2, 6.2.11.7.2 asks a Unicode value of every shown glyph, which ISO
32000-1, 9.10.2 derives from the glyph name the encoding gives the code, and
veraPDF asks it of every Glyph (`toUnicode != null`) whatever the font is
called. litho exempted a font whose `/BaseFont` holds "Symbol" twice over: when
its `/CharSet` lists only Symbol-font names, and when the shown codes its
encoding names all resolve, skipping the codes it leaves unnamed. No corpus
file reaches either exemption at PDF/A-1a, 2u, 3u, 4 or its own profile, so
this fixture plants the first:

| Font | Construct                                                                         | Answers at PDF/A-2u          |
| ---- | --------------------------------------------------------------------------------- | ---------------------------- |
| `MS` | nonsymbolic TrueType `MdSymbolA-Regular`, `/CharSet (/alpha/beta)`, shows `41 42` | `6.2.11.7.2/1`, `6.2.11.6/2` |

Its `/Differences` name `41 A 42 H20850` over no `/BaseEncoding`, and the
program holds the glyphs `A` and `H20850`, so both codes select a glyph.
`H20850` has no Unicode value in the Adobe Glyph List, the Symbol font or
ZapfDingbats, so code `42` fails `6.2.11.7.2/1`. A `/CharSet` is meaningful for
Type 1 fonts alone (ISO 32000-1, Table 122), and veraPDF reads none for a
TrueType. The `/Encoding` dictionary names no `/BaseEncoding`, which a
nonsymbolic TrueType fails under `6.2.11.6/2` on both tools.

| Rule at PDF/A-2u                  | veraPDF | litho | litho before |
| --------------------------------- | ------- | ----- | ------------ |
| `6.2.11.6/2` nonsymbolic encoding | 1       | 1     | 1            |
| `6.2.11.7.2/1` no Unicode         | 1       | 1     | 0            |
| `6.2.4.3/4` DeviceGray            | 1       | 1     | 1            |
| `6.6.2.1/1` no metadata           | 1       | 1     | 1            |

litho left the Unicode rule out at PDF/A-1a (`6.3.8/1`), PDF/A-2a, 2u, 3a and
3u, PDF/UA-1 (`7.21.7/1`) and PDF/UA-2 (`8.4.5.8/1`). veraPDF 1.30.2 and litho
now report the same rule set and the same counts in all thirteen cells.
Remediation writes the font a `/ToUnicode` CMap and leaves the encoding rule,
which both tools report on the output at every manifest profile in both modes.

### `fontunread_2b.pdf`

ISO 19005-2, 6.2.11.4.1 t1 asks that every font program be embedded (ISO
32000-1, 9.9), and veraPDF answers it with its own parse of the program
(`GFPDFont.getcontainsFontFile`: a program, and one that parsed). Bytes in the
container their key declares that its parser cannot finish embed nothing: the
font is reported as not embedded, and no rule weighs its glyphs, its widths or
its cmap. litho judged the container alone. Each font shows `41 42` through
`/WinAnsiEncoding`:

| Font | Program                                                        | veraPDF-parser 1.30 stops at         |
| ---- | -------------------------------------------------------------- | ------------------------------------ |
| `U1` | Type 1 `/FontFile`, the clear-text portion alone, no eexec     | no `/CharStrings` entry              |
| `U2` | Type 1 `/FontFile` whose Private dictionary has no CharStrings | no `/CharStrings` entry              |
| `U3` | `/FontFile3 /Type1C` whose Top DICT INDEX is empty             | the empty Top DICT INDEX             |
| `U4` | `/FontFile3 /Type1C` whose Top DICT states no CharStrings      | a CharStrings INDEX read at offset 0 |
| `T1` | TrueType `/FontFile2`, an sfnt with no table                   | no `hhea` table                      |
| `T2` | TrueType `/FontFile2` with every table but `hmtx`              | no `hmtx` table                      |
| `T3` | TrueType `/FontFile2` with every table but `hhea`              | no `hhea` table                      |
| `O1` | `/FontFile3 /OpenType`, an `OTTO` with no table                | neither a `CFF ` nor an `hhea` table |

`OK` and `TK` embed the whole programs the Type 1 and TrueType fonts are cut
from, and `CN` a CFF whose Name INDEX is empty, which veraPDF reads. All three
are embedded on both tools.

| Rule at PDF/A-2b                      | veraPDF | litho | litho before |
| ------------------------------------- | ------- | ----- | ------------ |
| `6.2.11.4.1/1` font not embedded      | 8       | 8     | 0            |
| `6.2.11.6/1` non-symbolic cmap (`T1`) | 0       | 0     | 1            |
| `6.2.4.3/4` DeviceGray                | 11      | 11    | 11           |
| `6.6.2.1/1` no metadata               | 1       | 1     | 1            |

The embedding rule moves the same way at PDF/A-1a and 1b (`6.3.4/1`), PDF/A-4,
4e and 4f (`6.2.10.4.1/1`), PDF/UA-1 (`7.21.4.1/1`) and PDF/UA-2
(`8.4.5.5.1/1`), and the cmap rule with it (`6.2.10.6/1`, `7.21.6/1`,
`8.4.5.7/1`). veraPDF 1.30.2 and litho report the same rule set in all thirteen
cells. Remediation cannot embed a program the fixture does not supply, so the
embedding rule stays on the output at every manifest profile in both modes, on
both tools, and no rule appears that the input did not fail.

Two shapes veraPDF reads but weighs differently stay out of the fixture, and
have one each: a TrueType with no `head` table
([`fontnohead_2b.pdf`](#fontnohead_2bpdf)), and a CFF whose CharStrings INDEX
is empty ([`fontemptycs_2b.pdf`](#fontemptycs_2bpdf)).

### `type1notdef_2u.pdf`

ISO 32000-1, 9.10.2 derives a simple font's Unicode value from the glyph name
a code selects, and Table 114 makes the program's built-in encoding the base
of an `/Encoding` that names no table. veraPDF reads it that way
(`PDSimpleFont.toUnicode`): the name the encoding gives the code, or, when it
gives none, the name the program gives it. A program that names the code
`.notdef` gives it U+0000, its value in veraPDF's glyph list, which fails the
value rule, ISO 19005-2, 6.2.11.7.2 t2, and passes the presence rule, t1. litho
read the code as unnamed and reported t1. Each font embeds a readable program,
carries no `/ToUnicode`, and shows `41` and one code its encoding leaves
unnamed:

| Font | Program and `/Encoding`                                            | Shows   | The program names the code |
| ---- | ------------------------------------------------------------------ | ------- | -------------------------- |
| `N1` | Type 1, vector filled with `.notdef`, naming `A` and `B`; none     | `41 43` | `.notdef`                  |
| `N2` | `N1` with the vector left unfilled; none                           | `41 43` | nothing                    |
| `N3` | Type 1 on `StandardEncoding`; `/Differences [65 /A]`, no base      | `41 80` | `.notdef`                  |
| `N4` | `/FontFile3 /Type1C` on the Standard encoding; none                | `41 80` | `.notdef`                  |
| `N5` | `N1`'s program; `/StandardEncoding`, a table veraPDF holds none of | `41 43` | `.notdef`                  |
| `N6` | `N4`'s program; `/StandardEncoding`                                | `41 80` | `.notdef`                  |

`NC` shows `41 42` through `N1`'s program, both named, and passes.

| Rule at PDF/A-2u                    | veraPDF | litho | litho before |
| ----------------------------------- | ------- | ----- | ------------ |
| `6.2.11.7.2/2` U+0000               | 5       | 5     | 0            |
| `6.2.11.7.2/1` no Unicode value     | 1       | 1     | 5            |
| `6.2.11.4.1/2` glyph not in program | 4       | 4     | 4            |
| `6.2.11.5/1` width (`N2`)           | 1       | 1     | 1            |
| `6.2.4.3/4` DeviceGray              | 7       | 7     | 7            |
| `6.6.2.1/1` no metadata             | 1       | 1     | 1            |

The glyph rule fires for `N1`, `N2`, `N3` and `N5` alone: a Type 1 program holds
no glyph its encoding names `.notdef` (`Type1FontProgram.containsGlyph`), where
a CFF holds `.notdef` at GID 0. The value rule moves the same way at PDF/A-2a,
3a and 3u, at PDF/A-4, 4e and 4f (`6.2.10.7/1`, the one Unicode rule part 4
states), at PDF/UA-1 (`7.21.7/2`) and at PDF/UA-2 (`8.4.5.8/2`). At PDF/A-1a
`.notdef` passes `6.3.8/1`, since veraPDF only asks there whether its glyph list
holds the name, so `N2` alone fails it, where litho reported five fonts. veraPDF
1.30.2 and litho report the same rule set in all thirteen cells. Remediation
leaves the value rule on the output at every manifest profile in both modes, on
both tools: no `/ToUnicode` entry gives `.notdef` a value other than its own.
Nor does one give `N2`'s `43`, which no encoding names, a value, so the presence
rule stays on the output too, `6.3.8/1` at PDF/A-1a, `6.2.11.7.2/1` at PDF/A-2a,
2u, 3a and 3u, `7.21.7/1` at PDF/UA-1 and `8.4.5.8/1` at PDF/UA-2, on both tools.
Remediation mapped it to `C` before, the StandardEncoding reading litho's text
extraction took.

### `type3names_2u.pdf`

A Type 3 font names its glyphs only through its `/Encoding` `/Differences`
array (ISO 32000-1, 9.6.5), so with no `/ToUnicode` a glyph name is the one
statement of what a glyph means. veraPDF's PDF/A-1 flavour reads a Type 3
glyph's Unicode value from the CMap alone, so every shown code fails ISO
19005-1, 6.3.8 t1 at PDF/A-1a. At PDF/A-2a, 2u, 3a and 3u, PDF/UA-1 and
PDF/UA-2 it accepts a name the Adobe Glyph List, the Symbol font or
ZapfDingbats lists, and refuses a `uniXXXX` spelling and a producer's own name.
Neither font carries a `/ToUnicode`:

| Font | `/Differences` from `41`, all shown | Answers at PDF/A-1a | Answers at PDF/A-2u |
| ---- | ----------------------------------- | ------------------- | ------------------- |
| `TN` | `A`, `alpha`, `a12`, `uni00E9`      | `6.3.8/1` x4        | `6.2.11.7.2/1` (44) |
| `TP` | `A`, `g12`                          | `6.3.8/1` x2        | `6.2.11.7.2/1` (42) |

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells.
Remediation writes `TN` a `/ToUnicode` from its names (U+0041, U+03B1, U+261E,
U+00E9) at PDF/A-1a, 2a, 2u, 3a, 3u, PDF/UA-1 and PDF/UA-2, in both modes, and
writes `TP` none: `g12` states no value, and a map leaving `42` out would claim
a repair veraPDF does not see. The outputs keep `TP`'s findings alone, the same
rule set on both tools at every profile: `6.3.8/1` x2 at PDF/A-1a,
`6.2.11.7.2/1` x1 at PDF/A-2a, 2u, 3a and 3u, `7.21.7/1` x1 at PDF/UA-1 and
`8.4.5.8/1` x1 at PDF/UA-2. The other six profiles carry no Unicode rule and
fully fix, but PDF/A-4f, which keeps `6.9/5`.

### `sizetie_2b.pdf`

Bookmark generation takes the body font size to be the size that sets the most
characters, and calls a line set larger a heading. One page shows `AABB` at 48
points over `ABAB` at 12, four characters each, so the two sizes tie. The tie
broke in the iteration order of a Go map until `5688d5c6`, and the outline came
out with `AABB` or without it from run to run. The tie now breaks to the smaller
size, and both generations find the one heading `AABB`.

The `determinism` feature runs `generate-bookmarks` (a Writer generation with
the CLI's options, replacing the outline) at PDF/UA-2 and `split-reconstruct` (a
split of every page that reconstructs the outline) at PDF/A-4e, eight times
each. With the tie broken in map order again, the feature counted one or both
cells nondeterministic in 10 of 10 runs.

The file conforms to PDF/A-2b: the text is in the conforming embedded TrueType
of `ttcmap_2b.pdf`, in DeviceGray under a CMYK OutputIntent, as in
`pagelabels_2b.pdf`. veraPDF 1.30.2 and litho report the same rule set in all
thirteen cells: nothing at PDF/A-2b, and at the other levels the
identification and tagging rules a part-2 claim draws. Remediated at all
thirteen, default and aggressive, every output carries the same rule set on both
tools and fully fixes, but PDF/A-4f, which keeps `6.9/5` on both.

### `iccv4intent_1b.pdf`

PDF/A-1 rests on PDF 1.4, which predates ICC v4, so ISO 19005-1, 6.2.2 takes
the destination profile of PDF Reference 1.4, 9.10.4, a v2 profile, and
veraPDF's test reads "version < 3.0". ISO 19005-2, -3 and -4, 6.2.3 admit v4.
The file's one output intent carries a CMYK printer profile whose header states
version 4.2, and nothing else in it fails PDF/A-1b. litho accepted major
versions 2 and 4 in every part until `9e4f95c7`, so the PDF/A-1b cell read
conforming where veraPDF reports `6.2.2/1`; with that fix reverted the PDF/A-1a
and 1b cells lose `6.2.2/1` and the remediated PDF/A-1a and 1b outputs read
fully fixed on litho alone.

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells:
`6.2.2/1` alone at PDF/A-1b, and at the other levels the tagging and
identification rules a part-1 claim draws, with no ICC rule. Remediated at all
thirteen, default and aggressive, every output carries the same rule set on both
tools. Remediation at PDF/A-1a and 1b re-encodes the profile as version 2 in
place (`5e8bf48c`): a profile with no tags loses nothing, so every level fully
fixes, but PDF/A-4f, which keeps `6.9/5`. Before that commit litho wrote no
replacement destination profile, and the PDF/A-1a and 1b outputs kept
`6.2.2/1` on both tools.

### `iccv4gray_1b.pdf`

The tagged twin of `iccv4intent_1b.pdf`. Its one output intent carries a v4.2
grey display profile of the shape pdfjs `issue19532.pdf` has: `desc` and `cprt`
as `mluc`, the sRGB tone curve as a type 3 `para` curve, and a D50 white point
with the Bradford adaptation from D65 in `chad`, every value computed from IEC
61966-2-1 and ICC.1:2010, Annex E.3. None of those tags has its version 4 type
in version 2 (ICC.1:2001-04, 6.5), so a v2 form must re-encode each one, where
`iccv4intent_1b.pdf` only changes a header field.

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells:
`6.2.2/1` alone at PDF/A-1b, and at the other levels the tagging and
identification rules a part-1 claim draws, with no ICC rule. Remediated at all
thirteen, default and aggressive, every output carries the same rule set on both
tools and fully fixes, but PDF/A-4f, which keeps `6.9/5`. At PDF/A-1a and 1b the
profile is re-encoded as version 2.4 (`5e8bf48c`): `mluc` becomes
`textDescriptionType` and `textType`, the curve a 1024-entry `curv` within one
16-bit step of the function, and the white point the D65 white the adaptation
maps from; the output converts every 8-bit grey to the same sRGB colour on
litho and lcms 2.14. Before that commit the PDF/A-1a and 1b outputs kept `6.2.2/1` on both
tools.

### `iccv4space_1b.pdf`

The colour-space twin of `iccv4gray_1b.pdf`. Its v4.2 grey display profile, of
the same shape, sits where the content paints rather than in an output intent:
`CS0` fills a square and colours the text, and the image `Im0` names the same
profile stream in its own `/ColorSpace`. `CS1`, the control, fills a second
square over a v2.4 profile of the same transfer function. The file declares no
output intent. ISO 19005-1, 6.2.3.2 asks of an ICCBased space the profile of
PDF Reference 1.4, 4.5.4, and veraPDF's `ICCInputProfile` test reads "version
< 3.0"; ISO 19005-2, -3 and -4, 6.2.4.2 admit v4.

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells:
`6.2.3.2/1` alone at PDF/A-1b, five failed checks on both tools (the two
selections of `CS0`, the fill and the text it colours, and the image, none for
`CS1`), and at the
other levels the tagging and identification rules a part-1 claim draws, with no
ICC rule. Remediated at all thirteen, default and aggressive, every output
carries the same rule set on both tools and fully fixes, but PDF/A-4f, which
keeps `6.9/5`. At PDF/A-1a and 1b the profile is re-encoded as version 2.4 in
place, so the page and the image still share one profile object, and lcms 2.17
converts every 8-bit grey to the same sRGB colour through it as through the
source; the other levels keep the v4 profile. Before the `icc-space-v2` fixer
the PDF/A-1a and 1b outputs kept `6.2.3.2/1` on both tools.

### `iccbadgray_1b.pdf` and `iccbadgray_2b.pdf`

A grey destination profile that cannot be kept or re-encoded is replaced with
litho's generated v2.4 grey profile, `sgray-v2.icc` (`4afa50fb`), whose `cprt`
dedicates it to the public domain under CC0 1.0. A grey intent must keep a grey
profile: sRGB under a grey output condition would license DeviceRGB content the
source never had (ISO 19005-1, 6.2.3.3; ISO 19005-2, 6.2.4.3). No fixture
reached that replacement before these two; `outputintent_2b.pdf` reaches the
sRGB one, and `iccbadcmyk_1b.pdf` and `iccbadcmyk_2b.pdf` the CMYK one.

`iccbadgray_1b.pdf` has one indirect intent whose grey display profile states
version 5.0, which no part admits. `iccbadgray_2b.pdf` has one intent written
directly in `/OutputIntents` over a version 2.1 grey profile of the input
device class `scnr`. Both profiles carry v2 `desc`, `cprt`, `wtpt` and `kTRC`
tags, and each page paints a DeviceGray square and text in an embedded
TrueType.

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells:
`6.2.2/1` at PDF/A-1a and 1b, `6.2.3/1` at the other PDF/A levels, beside the
tagging and identification rules each claim draws, and no ICC rule at PDF/UA-1
and UA-2. Remediated at all thirteen, default and aggressive, every output
carries the same rule set on both tools and fully fixes, but PDF/A-4f, which
keeps `6.9/5`. At every PDF/A level the output intent reads the CC0 grey
profile with `/N 1` and keeps its `/OutputConditionIdentifier`; PDF/UA checks
no output intent, so those outputs keep the source profile.

The two files take the two branches of the replacement. A version no part
admits fails an ICCBased space sharing the profile as well (ISO 19005-2,
6.2.4.2 t1), so `iccbadgray_1b.pdf` has its profile replaced at its own object
number. A scanner profile is admitted in an ICCBased space, so
`iccbadgray_2b.pdf` keeps it and the intent names a new profile object; the
intent has no object number of its own, so remediation rewrites the catalog's
array. Before the fix in the same change, that branch overwrote the profile
stream in place after allocating the replacement once more under a number
nothing referenced, so every output carried an orphan 2.7 KB copy of the grey
profile, and two intents sharing one profile came back with two copies, which
a later fixer had to unify.

### `iccbadcmyk_1b.pdf` and `iccbadcmyk_2b.pdf`

A CMYK destination profile that cannot be kept or re-encoded is replaced with
litho's stock CMYK printer profile,
`pkg/internal/graphics/icc/profiles/cgats001-cmyk-v2.icc`: version 2.4, class
`prtr`, PCS `Lab `, generated by `cmd/gen-iccv2` from the CC0
CGATS001Compat characterisation, written FlateDecode-compressed. A CMYK intent must keep a CMYK profile: sRGB under a CMYK output
condition would stop covering the DeviceCMYK content the page paints (ISO
19005-1, 6.2.3.3; ISO 19005-2, 6.2.4.3). No fixture reached that replacement
before these two.

`iccbadcmyk_1b.pdf` has one indirect intent whose CMYK output profile states
version 5.0, which no part admits. `iccbadcmyk_2b.pdf` has a `GTS_PDFA1` and a
`GTS_PDFX` intent, both indirect, naming one version 2.1 CMYK profile of the
input device class `scnr`, and its page paints an ICCBased space over the same
profile stream. Both profiles carry v2 `desc`, `cprt`, `wtpt`, and lut8 `A2B0`
and `B2A0` tags over a Lab connection space, and each page paints DeviceCMYK
and text in an embedded TrueType.

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells:
`6.2.2/1` at PDF/A-1a and 1b, `6.2.3/1` at the other PDF/A levels, beside the
tagging and identification rules each claim draws, and no ICC rule at PDF/UA-1
and UA-2. `iccbadcmyk_2b.pdf` also fails `6.2.4.2/3` at PDF/A-4, 4e and 4f:
its ICCBased space reads the CMYK profile of the current output intent (ISO
19005-4, 6.2.4.2). Remediated at all thirteen, default and aggressive, every
output carries the same rule set on both tools and fully fixes, but PDF/A-4f,
which keeps `6.9/5`. At every PDF/A level the output intents read the bundled
CMYK profile with `/N 4` and keep their `/OutputConditionIdentifier`; PDF/UA
checks no output intent, so those outputs keep the source profile.

The two files take the two branches of the replacement.
`iccbadcmyk_1b.pdf` has its profile replaced at its own object number, so the
output holds one ICC profile. `iccbadcmyk_2b.pdf` keeps the scanner profile for
its ICCBased space, which admits one (ISO 19005-2, 6.2.4.2), and both intents
name one new profile object, as ISO 19005-2, 6.2.3 t2 asks: the output holds
two ICC profiles and no orphan. Pointing the intents away from the space's
profile also clears `6.2.4.2/3` at PDF/A-4.

The stock profile's `AToB` tables are CGATS001Compat's, so an image decoded
from a remediated output reads DeviceCMYK through the new intent exactly as it
reads a document with no CMYK intent. The profile it replaced, `cmyk.icc`, was
an 8-bit table into XYZ whose every entry was 128: LittleCMS read every CMYK
colour as one pale grey, poppler refused it, and litho's engine could not
build a transform from it.

### `twoprofile_1b.pdf`

ISO 19005-1, 6.2.2 asks every output intent carrying `/DestOutputProfile` to
name the same indirect object, and ISO 19005-2/3/4, 6.2.3 t2 ask the same. The
corpus fails the rule in three files, `outputintent_2b.pdf` and the veraPDF
`6-2-3-t02-fail-a` pair, each with the `GTS_PDFA1` intent first. This fixture
lists a `GTS_PDFX` intent over a version 2.1 CMYK printer profile before a
`GTS_PDFA1` intent over a version 2.1 RGB display profile, and its page paints
DeviceRGB, which only the RGB intent covers (ISO 19005-1, 6.2.3.3). The order
is the probe: a repair that keeps the first entry's profile takes the PDF/A
intent's colour away.

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells. At
PDF/A-1b the file fails `6.2.2/2` alone; at the other PDF/A levels `6.2.3/2`,
beside the identification and tagging rules each claim draws.

Remediated at the eleven PDF/A levels, default and aggressive, every output
reports the same rule set on both tools and passes but PDF/A-4f's, which keeps
`6.9/5`. The cleanup fixer keeps the `GTS_PDFA1` intent's profile and points
the `GTS_PDFX` intent at it, so the PDF/A reading keeps its colour and the
PDF/X intent keeps its `/OutputConditionIdentifier` over an RGB profile; the
fix it reports names the CMYK profile it replaced. The CMYK profile stays in
the file unreferenced.

Before that change the outputs at PDF/A-1a and 1b kept `6.2.2/2`, since the
fixer claimed only clause 6.2.3, which is 6.2.2 in part 1. At the other levels
it kept the first entry's profile, and two rounds replaced the document's
colour: the first pointed the PDF/A intent at the CMYK profile, which left the
DeviceRGB square uncovered, and the second replaced the whole `/OutputIntents`
array with one stock sRGB `GTS_PDFA1` intent, dropping the PDF/X intent and
both source profiles.

### `pdfxonly_2b.pdf`

ISO 32000-1, 14.11.5 admits several output intents, one per output condition.
The catalog's sole intent is a `GTS_PDFX` one over a version 2.1 CMYK printer
profile, and the page paints a DeviceCMYK square and a DeviceRGB one. Only a
`GTS_PDFA1` intent covers device colour (ISO 19005-1, 6.2.3.3; ISO 19005-2,
6.2.4.3), so every PDF/A level fails the device colour rules, `6.2.3.3/1` and
`/2` at part 1 and `6.2.4.3/2` and `/3` after it, beside the identification
and tagging rules each claim draws. veraPDF 1.30.2 and litho report the same
rule set in all thirteen cells.

Remediated at the eleven PDF/A levels, default and aggressive, every output
reports the same rule set on both tools and passes but PDF/A-4f's, which keeps
`6.9/5`. The new `GTS_PDFA1` intent goes first and names the PDF/X intent's
profile object, with its `/OutputConditionIdentifier` and `/RegistryName`, and
the PDF/X intent stays as it was: the profile is one PDF/A admits as a CMYK
destination profile (ISO 19005-2, 6.2.3 t1), and both intents naming it meets
6.2.3 t2. A DefaultRGB covers the DeviceRGB square. Before, remediation
replaced the whole `/OutputIntents` array with a stock `CGATS TR 001` intent,
which dropped the PDF/X intent and its profile.

### `pdfxnoprofile_2b.pdf`

The catalog lists a `GTS_PDFA1` intent carrying an `/OutputConditionIdentifier`
and no `/DestOutputProfile`, then a `GTS_PDFX` intent over a version 2.1 CMYK
printer profile. The page paints DeviceRGB. The PDF/A intent covers nothing
without a profile, so every PDF/A level fails `6.2.3.3/1` or `6.2.4.3/2`.
veraPDF 1.30.2 and litho report the same rule set in all thirteen cells.

Remediated at the eleven PDF/A levels, default and aggressive, every output
reports the same rule set on both tools and passes but PDF/A-4f's, which keeps
`6.9/5`. The profile-less `GTS_PDFA1` intent is replaced by a stock sRGB one,
since a second `GTS_PDFA1` intent would name a second PDF/A output condition.
The CMYK profile does not cover DeviceRGB, so the PDF/X intent cannot lend it,
and ISO 19005-2, 6.2.3 t2 points the PDF/X intent at the sRGB profile: it
keeps its `/OutputConditionIdentifier` over an RGB profile, as the cleanup
fixer leaves `twoprofile_1b.pdf`'s, and the fix it reports names the CMYK
profile it replaced. The `/DefaultCMYK` remediation stages beside the sRGB
intent names that CMYK profile rather than litho's stock one: PDF/A admits it
as a CMYK profile (ISO 19005-2, 6.2.4.2 t1), so the producer's
characterisation stays and every output is about 28 KB smaller, with the same
rule set on both tools. Before, remediation dropped the PDF/X intent, and then
left its CMYK profile in the file unreferenced.

### `drivetitle_1b.pdf`

ISO 19005-1, 6.7.3 makes each document information entry equal to its XMP
analogue, and veraPDF compares a text entry as a string. Only `/CreationDate`
and `/ModDate` are dates (ISO 32000-1, Table 317), and litho read every value
starting `D:` as a date on the Info side alone until `bf06c896`. Two text
entries begin that way, each equal to its XMP copy, on independent tests:

| Info entry      | Value                      | XMP property     | Before `bf06c896` |
| --------------- | -------------------------- | ---------------- | ----------------- |
| `/Title`        | `D:\openkm\temp\okm25.pdf` | `dc:title`       | `6.7.3/2`         |
| `/Subject`      | `D:20200101`               | `dc:description` | `6.7.3/4`         |
| `/CreationDate` | `D:20200101120000Z`        | `xmp:CreateDate` | silent, a date    |

`/CreationDate` is the control: a real date still compares as an instant. With
the fix reverted, litho reports `6.7.3/2` and `6.7.3/4` at PDF/A-1a and 1b, on
the input and on every remediated output. veraPDF 1.30.2 and litho report the
same rule set in all thirteen cells: nothing at PDF/A-1b, and at the other
levels the rules a part-1 claim draws. Remediated at all thirteen, default and
aggressive, every output carries the same rule set on both tools and fully
fixes, but PDF/A-4f, which keeps `6.9/5`.

### `unclosedmc_ua1.pdf`

ISO 32000-1, 14.6 delimits a marked-content sequence by its BMC or BDC and the
matching EMC, so content after a BMC no EMC closes lies in no sequence. The page
holds a closed `/P` sequence and then `/Artifact BMC` and a stroked rule with no
EMC. ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1 judge the rule unmarked, as
veraPDF does: its semantic content stream makes everything after the last
balanced EMC one unmarked group. litho counted the open artifact as marking what
followed until `3e903ac2`; with that fix reverted the PDF/UA-1 cell reads
conforming and the PDF/UA-2 cell loses `8.2.2/1`.

veraPDF 1.30.2 and litho report the same rule set in all thirteen cells:
`7.1/3` alone at PDF/UA-1, and at PDF/UA-2 `8.2.2/1` beside the `5/2`, `5/5`
and `8.2.5.2/2` a part-1 claim and a Document element outside the PDF 2.0
namespace draw.
Remediated at all thirteen, default and aggressive, every output carries the
same rule set on both tools and fully fixes, but PDF/A-4f, which keeps `6.9/5`.
The content-tag fixer leaves a page that already holds marked content, so until
the marked-content-close fixer appended the missing EMC in a stream of its own,
the PDF/UA-1 output kept `7.1/3` and the PDF/UA-2 output `8.2.2/1`.

### `danglingstr_ua1.pdf`

A literal string runs until its balancing right parenthesis (ISO 32000-1,
7.3.4.2), so one a stream never closes swallows every byte after it. The page is
untagged: a line of text, a stroked rule, then `(dangling ( string` to the end
of the stream. Auto-tagging wraps the trailing content in an `/Artifact`
sequence, and until `dc543dfd` it appended the closing EMC after the last byte,
inside the string, which left the sequence open. With that fix reverted the
remediated PDF/UA-1 and PDF/UA-2 outputs keep `7.1/3` and `8.2.2/1` on both
tools; with it, the EMC goes before the string.

The rest of the document is ready for PDF/UA-1: `/Lang`, DisplayDocTitle, the
pdfuaid claim and dc:title, the conforming embedded TrueType of
`ttcmap_2b.pdf`. veraPDF 1.30.2 and litho report the same rule set in all
thirteen cells, the untagged page's `6.2/1`, `7.1/11` and `7.1/3` at PDF/UA-1,
and neither reports the string. Remediated at all thirteen, default and
aggressive, every output carries the same rule set on both tools and fully
fixes, but PDF/A-4f, which keeps `6.9/5`.

The same string swallowed what writers append after a page's content: the
`mutate-stamp` cell's restoring `Q` and stamp were read inside it, and qpdf
reported the stamped page as not carrying the stamp. Stamping and flattening
now end the page's content at its last operator before appending, as
auto-tagging does, and the cell matches.

### `fontnohead_2b.pdf`

ISO 19005-2, 6.2.11.5 weighs a program's advances in units of its em, which an
sfnt states in its `head` table alone. A TrueType program with no `head` states
none, and veraPDF still reads it: its `TrueTypeHeadTable` starts unitsPerEm at
2048 and keeps that with no table to read. litho took 1000.

| Font | Program                                         | Declared      | At 2048 | At 1000 |
| ---- | ----------------------------------------------- | ------------- | ------- | ------- |
| `H1` | TrueType, advances 1229, no `head`              | `/Widths` 600 | passes  | fails   |
| `H2` | CIDFontType2, advances 600, no `head`           | `/W` 293      | passes  | fails   |
| `HK` | `H1`'s program, `head` stating 2048             | `/Widths` 600 | passes  | passes  |
| `HO` | Type 1 over an `OTTO` with `CFF ` and no `head` | `/Widths` 600 | passes  | passes  |

`HO` keeps the scale of its CFF: a Type 1 font reads an OpenType program
through its `CFF ` table. The converse of `H1`, advances 600 under `/Widths`
600, fails veraPDF and passed litho; the fixture leaves it out because a font of
each kind leaves both tools on the same rule set for different glyphs, which
the runner cannot tell apart. It is measured instead, with 1000-unit advances
passing 488 and failing 1000 and 490, and 2048-unit ones passing 1000.

| Rule at PDF/A-2b        | veraPDF | litho | litho before |
| ----------------------- | ------- | ----- | ------------ |
| `6.2.11.5/1` width      | 0       | 0     | 4            |
| `6.2.4.3/4` DeviceGray  | 4       | 4     | 4            |
| `6.6.2.1/1` no metadata | 1       | 1     | 1            |

The width rule moves the same way at PDF/A-1a and 1b (`6.3.6/1`), PDF/A-4, 4e
and 4f (`6.2.10.5/1`), PDF/UA-1 (`7.21.5/1`) and PDF/UA-2 (`8.4.5.6/1`).
veraPDF 1.30.2 and litho report the same rule set in all thirteen cells.
Remediation scales the widths it writes by the same 2048: before, it rewrote
`H1` and `H2` at 1000, so in all thirteen cells and both modes veraPDF failed
the output for widths the input passed, where litho reported none. Each output now
carries the same rule set on both tools, and no rule appears that the input
did not fail.

### `fontemptycs_2b.pdf`

A bare CFF whose CharStrings INDEX holds no charstring still reads, so ISO
19005-2, 6.2.11.4.1 t1 counts the font as embedded, but it defines no glyph,
`.notdef` included: every code shown fails 6.2.11.4.1 t2. litho read no
glyph table and judged nothing. veraPDF names the glyph of a code its encoding
names `.notdef` present, and reports it under 6.2.11.8 t1 instead, and it
reports every CID of a Type 0 font under both. The width rule, 6.2.11.5, weighs
each glyph against the width veraPDF reads for a glyph the program lacks: the
`defaultWidthX` of the Private DICT for a name-keyed program, the
`/MissingWidth` for a subset, and the CIDFont's default width (`/DW`, else 1000)
for a CID-keyed one. Each simple font shows `41 42` through `/WinAnsiEncoding`,
each Type 0 font CIDs 1 and 2 under Identity-H:

| Font | Program                                            | Declared                        | Width rule |
| ---- | -------------------------------------------------- | ------------------------------- | ---------- |
| `E1` | name-keyed, `defaultWidthX` 700                    | `/Widths` 700                   | passes     |
| `E2` | name-keyed, `defaultWidthX` 700                    | `/Widths` 0                     | fails      |
| `E3` | `E1`'s, the font named `ABCDEF+E3`                 | `/Widths` 700, no MissingWidth  | fails      |
| `E4` | `E1`'s, the font named `ABCDEF+E4`                 | `/Widths` 300, MissingWidth 300 | passes     |
| `E5` | `E1`'s, `/Differences` naming `41` `.notdef`       | `/Widths` 700                   | passes     |
| `C1` | CID-keyed, CIDFontType0                            | `/W` 1000, no `/DW`             | passes     |
| `C2` | CID-keyed, CIDFontType0                            | `/W` 500                        | fails      |
| `K1` | name-keyed under CIDFontType0, `defaultWidthX` 700 | `/W` 700                        | passes     |
| `K2` | name-keyed under CIDFontType0, `defaultWidthX` 700 | `/W` 0                          | fails      |

`EK` and `CK` embed the whole programs the empty ones are cut from, and pass.

| Rule at PDF/A-2b                    | veraPDF | litho | litho before |
| ----------------------------------- | ------- | ----- | ------------ |
| `6.2.11.4.1/2` glyph not in program | 17      | 17    | 0            |
| `6.2.11.5/1` width                  | 8       | 8     | 0            |
| `6.2.11.8/1` .notdef                | 9       | 9     | 1            |
| `6.2.4.3/4` DeviceGray              | 11      | 11    | 11           |
| `6.6.2.1/1` no metadata             | 1       | 1     | 1            |

The rules move the same way at PDF/A-1a and 1b (`6.3.5/1`, `6.3.6/1`; part 1
states no .notdef rule), PDF/A-4, 4e and 4f (`6.2.10.4.1/2`, `6.2.10.5/1`,
`6.2.10.9/1`), PDF/UA-1 (`7.21.4.1/2`, `7.21.5/1`, `7.21.8/1`) and PDF/UA-2
(`8.4.5.5.1/2`, `8.4.5.6/1`, `8.4.5.9/1`). veraPDF 1.30.2 and litho report the
same rule set in all thirteen cells. Remediation cannot give a program a glyph
it lacks and writes no width for a program with none, so the three rules stay
on the output at every manifest profile in both modes, on both tools, and no
rule appears that the input did not fail.

### `fontcff2_2b.pdf`

ISO 32000-1, Table 126 lets `/FontFile3 /OpenType` hold an OpenType program.
veraPDF reads it by the font that embeds it: a Type 1 or CIDFontType0 font as a
CFF font, through its `CFF ` table, a TrueType or CIDFontType2 font as a
TrueType font, through its `hhea` and `hmtx` tables. A program lacking the
tables its reading needs is not embedded (6.2.11.4.1 t1), and a `CFF2` table
(OpenType 1.8) is not a `CFF ` table: veraPDF stops at "Can't locate "CFF "
table". litho counted any such program as embedded when either reading would
finish.

| Font | Font type    | Program                                    | Embedded |
| ---- | ------------ | ------------------------------------------ | -------- |
| `F1` | Type1        | `OTTO`, `CFF2` outlines, `hhea` and `hmtx` | no       |
| `F2` | CIDFontType0 | `F1`'s                                     | no       |
| `F3` | TrueType     | `F1`'s, `/Widths` 600 as `hmtx` states     | yes      |
| `F4` | CIDFontType2 | `F1`'s, `/W` 600                           | yes      |
| `F5` | TrueType     | `F1`'s, `/Widths` 500                      | yes      |
| `M1` | Type1        | a version-1.0 sfnt with no `CFF ` table    | no       |
| `M2` | CIDFontType0 | `M1`'s                                     | no       |
| `M3` | TrueType     | `OTTO`, `CFF ` outlines, no `hhea`/`hmtx`  | no       |
| `M4` | CIDFontType2 | `M3`'s                                     | no       |
| `OK` | Type1        | `OTTO`, `CFF ` outlines, `hhea` and `hmtx` | yes      |

`F5` fails the width rule alone on both tools, so the TrueType reading of a
`CFF2` program weighs `hmtx` as litho already did.

| Rule at PDF/A-2b                 | veraPDF | litho | litho before |
| -------------------------------- | ------- | ----- | ------------ |
| `6.2.11.4.1/1` font not embedded | 6       | 6     | 0            |
| `6.2.11.5/1` width (`F5`)        | 2       | 2     | 2            |
| `6.2.4.3/4` DeviceGray           | 10      | 10    | 10           |
| `6.6.2.1/1` no metadata          | 1       | 1     | 1            |

The embedding rule moves the same way at PDF/A-1a and 1b (`6.3.4/1`), PDF/A-4,
4e and 4f (`6.2.10.4.1/1`), PDF/UA-1 (`7.21.4.1/1`) and PDF/UA-2
(`8.4.5.5.1/1`). veraPDF 1.30.2 and litho report the same rule set in all
thirteen cells. At PDF/A-1a and 1b both report `6.3.2/7`, the `/OpenType`
subtype PDF 1.4 does not define, for every font, and both count each Type 0
font and its descendant, 14 checks (litho counted 10 before). The glyph rules
take the
same reading, which [`fontotread_2b.pdf`](#fontotread_2bpdf) makes visible.
Remediation cannot embed a program the fixture does not supply, so the
embedding rule stays on the output at every manifest profile in both modes, on
both tools, and no rule appears that the input did not fail. Since the glyph
rules read `F4`'s program through `hmtx`, remediation also writes `F4` a `/DW`
of 600, which changes no rule on either tool.

### `fontotread_2b.pdf`

veraPDF reads an embedded sfnt by the font that embeds it for every rule, not
only for the embedding verdict `fontcff2_2b.pdf` plants: a Type 1 or
CIDFontType0 font reads its `CFF ` table and nothing else of the sfnt, a
TrueType or CIDFontType2 font reads any sfnt, `OTTO` included, as a TrueType
program. litho's glyph rules read the program by the outlines it carries, so a
Type 1 font over an `OTTO` weighed `hmtx` and a CIDFontType2 font over one
weighed the CFF charstrings. Every program is a `/FontFile3 /OpenType` whose
CFF charstrings advance 600 and whose `hmtx` states 500. No corpus file holds
a program whose two readings disagree, so this fixture plants them:

| Font | Font type    | Program                                                   | Declares        |
| ---- | ------------ | --------------------------------------------------------- | --------------- |
| `W1` | Type1        | `OTTO`, `CFF ` outlines                                   | `/Widths` 600   |
| `W2` | Type1        | `W1`'s                                                    | `/Widths` 500   |
| `W3` | Type1        | a version-1.0 sfnt carrying `glyf` and a `CFF ` table     | `/Widths` 600   |
| `W4` | CIDFontType0 | a version-1.0 sfnt carrying `glyf` and a CID-keyed `CFF ` | `/W` 600        |
| `W5` | CIDFontType2 | `OTTO`, CID-keyed `CFF ` outlines                         | `/W` 500        |
| `W6` | CIDFontType2 | `W5`'s                                                    | `/W` 600        |
| `H1` | TrueType     | `OTTO`, `CFF ` outlines, no `head`, `hmtx` advancing 1000 | `/Widths` 488   |
| `P1` | CIDFontType2 | `OTTO` whose `maxp` counts 3 glyphs, `CFF ` CIDs 1 to 3   | `/W` 500, CID 3 |

`H1` weighs its advances against the 2048 units a TrueType program with no
`head` takes ([`fontnohead_2b.pdf`](#fontnohead_2bpdf)). `P1` shows CID 3,
which its `CFF ` charset holds and its `maxp` does not count, so the TrueType
reading finds no glyph for it and draws `.notdef`.

| Rule at PDF/A-2b                | veraPDF | litho | litho before |
| ------------------------------- | ------- | ----- | ------------ |
| `6.2.11.5/1` width (`W2`, `W6`) | 4       | 4     | 12           |
| `6.2.11.4.1/2` glyph (`P1`)     | 1       | 1     | 0            |
| `6.2.11.8/1` `.notdef` (`P1`)   | 1       | 1     | 0            |
| `6.2.4.3/4` DeviceGray          | 8       | 8     | 8            |
| `6.6.2.1/1` no metadata         | 1       | 1     | 1            |

The same rules move at PDF/A-1a and 1b (`6.3.6/1`, `6.3.5/1`), PDF/A-2a, 2u,
3a, 3b and 3u, PDF/A-4, 4e and 4f (`6.2.10.5/1`, `6.2.10.4.1/2`,
`6.2.10.9/1`), PDF/UA-1 (`7.21.5/1`, `7.21.4.1/2`, `7.21.8/1`) and PDF/UA-2
(`8.4.5.6/1`, `8.4.5.5.1/2`, `8.4.5.9/1`). veraPDF 1.30.2 and litho report the
same rule set and the same counts in all thirteen cells. At PDF/A-1a and 1b
both count `6.3.2/7` for each Type 0 font and its descendant, 12 checks, as in
`fontcff2_2b.pdf`; for the untagged content, both count PDF/UA-1 `7.1/3` and
`7.2/34` and PDF/UA-2 `8.2.2/1` 8 times each. Before, litho counted 8 checks
and 1 each.

Remediation repairs the widths from the same reading: it writes `W2` 600 and
`W6` 500 and leaves the other fonts alone. Before, it repaired from the
outlines too, and its output failed the width rule for all eight fonts when
read as the fonts read it. At every manifest profile in both modes the width
rule leaves the output on both tools, the presence and `.notdef` rules for
`P1` stay, since remediation cannot give a program a glyph it lacks, and no
rule appears that the input did not fail.

### `aakeys_4.pdf`

ISO 19005-4, 6.6.3 does not forbid the `/AA` entry of a document catalog or a
page, as ISO 19005-1, 6.6.2 t3 and ISO 19005-2/3, 6.5.2 t1 and t2 do. It
restricts the keys of the additional-actions dictionary to `E`, `X`, `D`, `U`,
`Fo` and `Bl`, and veraPDF judges each such dictionary by the keys it holds.
litho's remediation dropped the catalog and page `/AA` whole at every part, so
at part 4 it lost the permitted entries with the forbidden ones. No corpus file
holds a permitted key beside a forbidden one in a catalog or page `/AA`, so
this fixture plants it. Every action is a GoTo to page 1, which no PDF/A part
forbids:

| Owner   | `/AA` keys                           | Answers at PDF/A-4 |
| ------- | ------------------------------------ | ------------------ |
| catalog | `/WC`, `/E`, in an object of its own | `6.6.3/1`          |
| page 1  | `/O`, `/X`                           | `6.6.3/1`          |
| page 2  | `/D` alone, the control              | nothing            |

| Rule                                    | veraPDF | litho | litho before |
| --------------------------------------- | ------- | ----- | ------------ |
| `6.6.3/1` at PDF/A-4, 4e, 4f            | 2       | 2     | 2            |
| `6.6.2/3` catalog `/AA` at PDF/A-1a, 1b | 1       | 1     | 1            |
| `6.5.2/1` catalog `/AA` at parts 2, 3   | 1       | 1     | 1            |
| `6.5.2/2` page `/AA` at parts 2, 3      | 2       | 2     | 2            |
| `8.8/2` GoTo at PDF/UA-2                | 2       | 2     | 1            |

The UA-2 row is a second finding: litho judged the GoTo actions of a page's
`/O` and `/C` under ISO 14289-2, 8.8 t2 and never those of the catalog's
document-level triggers (ISO 32000-1, Table 197), which veraPDF builds and
judges. Neither tool counts the GoTo under the catalog's `/E`, which names no
catalog action. veraPDF 1.30.2 and litho report the same rule set and the
same counts in all thirteen cells.

Remediated at PDF/A-4, 4e and 4f, in both modes, the output keeps the catalog
`/AA` holding `/E` alone, page 1's holding `/X` alone and page 2's untouched,
and both tools find it conforming (PDF/A-4f keeps `6.9/5`, the embedded file
it requires). At parts 1 to 3 every `/AA` goes, as before. At PDF/UA-2 the
output keeps both GoTo actions, which point at a page with no structure
element to name, and both tools report `8.8/2` twice. Every other output is
conforming on both tools.

### `patternshade_2b.pdf`

A shading reaches a page through a shading pattern an `scn` selects (ISO
32000-1, 8.7.4.1) or through `sh` (8.7.4.2), and either can sit in a tiling
pattern's cell (8.7.3.1) or in a Type 3 glyph description (9.6.5). veraPDF
1.30.2 judges the colour space of every such shading, a Separation or DeviceN
tint transform included, and judges neither the shading's `/Function` nor a
shading pattern's own `/ExtGState`. Three pdfjs files reach a shading through a cell (`issue6737` paints one with
`sh`, `issue16127` and `issue8565` select shading patterns), and litho matches
veraPDF on all three. No corpus file reaches a shading through a glyph, and
none isolates each path, so this fixture plants one path per page, each
answering a rule nothing else in the file fires. A CMYK OutputIntent covers DeviceCMYK and
the DeviceGray a glyph shows in:

| Page | Path                                 | Shading colour space               | Answers at PDF/A-2b     |
| ---- | ------------------------------------ | ---------------------------------- | ----------------------- |
| 1    | tiling cell, `/SH1 scn`              | DeviceRGB                          | `6.2.4.3/2`             |
| 2    | Type 3 glyph on the page, `/SH2 scn` | Separation, type 4 tint 2147483648 | `6.1.13/1`              |
| 3    | tiling cell, Type 3 glyph, `/Sh3 sh` | DeviceN of 33 colorants            | `6.1.13/9`, `6.2.4.4/1` |

Every shading's `/Function` is a type 4 program holding 2147483649 and
40000.5, and `SH1` carries an `/ExtGState` setting `/TR /Identity` and `/BM
/Foo`. Neither tool judges any of them, so `6.1.13/1` counts the tint transform
alone, `6.2.5` and `6.2.10` stay silent, and at PDF/A-1b `6.1.12/2` stays
silent. The same graphics state set by a page's `gs` fails `6.2.5/1`, `6.2.5/2`,
`6.2.10/1` and `6.2.10/2` on both tools, so the silence is the oracle's reading
and not a planting mistake. Page 3's DeviceN has no `/Colorants`, which
`6.2.4.4/1` answers.

| Rule at PDF/A-2b                  | veraPDF | litho |
| --------------------------------- | ------- | ----- |
| `6.1.13/1` integer, page 2's tint | 1       | 1     |
| `6.1.13/9` 33 colorants           | 1       | 1     |
| `6.2.4.3/2` DeviceRGB             | 1       | 1     |
| `6.2.4.4/1` no `/Colorants`       | 1       | 1     |

litho already reached every path, so the fixture pins the reading rather than
a fix. veraPDF 1.30.2 and litho report the same rule set and the same counts in
all thirteen cells; at PDF/UA both count `7.1/3` and `8.2.2/1` three times, once
per page's untagged item.

Remediated at every level in both modes, both tools report the same rule set
on every output, and both PDF/UA outputs are conforming. At the PDF/A levels
the type 4 tint's integer stays (no fixer rewrites a program), the default mode
keeps the 33-colorant DeviceN where the aggressive mode clears it, and the
PDF/A-4 and 4e outputs are conforming. Remediation replaces the CMYK
OutputIntent with an sRGB one to cover page 1's DeviceRGB and gives page 2's
Separation the `/DefaultCMYK` it injects as its alternate, since no Default
reaches the shading of a shading pattern (see
[`shadedefault_2b.pdf`](#shadedefault_2bpdf)). Before that, one DeviceCMYK
finding stayed on both tools at every PDF/A level, `6.2.4.3/3` (`6.2.3.3/2` at
PDF/A-1).

### `patternglyph_2b.pdf`

A tiling pattern's cell can show text in a Type 3 font (ISO 32000-1, 8.7.3.1,
9.6.5), and the glyph description paints images, shows text in other fonts,
selects colours and selects patterns of its own. veraPDF 1.30.2 judges what
such a glyph draws once an `scn` selects the pattern and text is shown with the
font, and never through a `Tf` alone. No corpus file shows Type 3 text in a
pattern cell, so this fixture plants one path per page, each answering a rule
nothing else in the file fires. A CMYK OutputIntent covers the DeviceGray every
cell and glyph shows text in:

| Page | Path                                                 | Glyph draws                         | Answers at PDF/A-2b |
| ---- | ---------------------------------------------------- | ----------------------------------- | ------------------- |
| 1    | tiling cell, Type 3 glyph                            | an image with `/Interpolate true`   | `6.2.8/3`           |
| 2    | tiling cell, Type 3 glyph                            | text in an unembedded TrueType font | `6.2.11.4.1/1`      |
| 3    | tiling cell, Type 3 glyph, tiling cell, Type 3 glyph | a DeviceRGB fill                    | `6.2.4.3/2`         |
| 4    | tiling cell selecting the font with `Tf` alone       | an image with `/Intent /Bogus`      | nothing             |

| Rule at PDF/A-2b                 | veraPDF | litho |
| -------------------------------- | ------- | ----- |
| `6.2.11.4.1/1` font not embedded | 1       | 1     |
| `6.2.4.3/2` DeviceRGB            | 2       | 2     |
| `6.2.8/3` `/Interpolate` true    | 1       | 1     |

litho already reached every path, so the fixture pins the reading rather than
a fix. The paths ride different walks: with the drawn-stream walk's pattern
descent cut, litho loses `6.2.8/3` and keeps the other two, which the font and
colour walks reach on their own. veraPDF 1.30.2 and litho report the same rule
set and the same counts in all thirteen cells; at PDF/UA both count `7.1/3` and
`8.2.2/1` four times, once per page's untagged item.

Remediated at every level in both modes, both tools report the same rule set
on every output. What stays is the unembedded TrueType font, which litho ships
no program for (`6.2.11.4.1/1`, `6.3.4/1` at PDF/A-1, `6.2.10.4.1/1` at part 4,
`7.21.4.1/1` and `8.4.5.5.1/1` at PDF/UA), with `6.3.8/1` at PDF/A-1a and
`6.9/5` at PDF/A-4f beside it.

### `patternreach_2b.pdf`

A pattern reaches a page through an `scn` or `SCN` that selects it (ISO
32000-1, 8.7.3.1 and 8.7.4.1), from the page's content, a form XObject, an
annotation appearance, a Type 3 glyph or another pattern's cell, and a shading
reaches it through a shading pattern or through `sh` (8.7.4.2). veraPDF 1.30.2
judges the colour space of each such shading and of each colour operator in a
cell, and judges a stream once however many paths reach it. The veraPDF corpus
reaches a cell only from a page's content and reaches no shading pattern, and
the litho fixtures before this one reach a shading pattern only from a page, a
cell or a glyph, so this fixture plants one path per page, each painting
DeviceRGB, which `6.2.4.3/2` answers under the CMYK OutputIntent:

| Page | Path                                                     | `6.2.4.3/2` |
| ---- | -------------------------------------------------------- | ----------- |
| 1    | `SCN` strokes with a shading pattern                     | 1           |
| 2    | a form XObject selects a shading pattern                 | 1           |
| 3    | an annotation appearance selects a shading pattern       | 1           |
| 4    | text in a `d1` Type 3 font filled with a shading pattern | 1           |
| 5    | a tiling cell paints a form XObject that runs `sh`       | 1           |
| 6    | a shading pattern over a type 4 mesh shading             | 1           |
| 7    | a shading pattern over an `/Indexed` space on DeviceRGB  | 1           |
| 8    | a direct shading pattern holding a direct shading        | 2           |
| 9    | a tiling cell that selects itself                        | 2           |
| 10   | two tiling cells that select each other                  | 4           |
| 11   | a Type 3 glyph whose pattern cell shows the same font    | 2           |
| 12   | one shading pattern on the page and in a cell it paints  | 1           |
| 13   | one /Pattern dictionary two cells share                  | 1           |

A cycle ends where it reaches a stream already judged (pages 9 to 11), and a
shading pattern or a /Pattern dictionary reached twice counts once (pages 12
and 13). Page 8's pattern and shading are direct objects, so the `scn` and the
fill after it each link to the shading's colour space.

litho already reached every path, so the fixture pins the reading rather than
a fix. veraPDF 1.30.2 and litho report the same rule set and the same counts in
all thirteen cells, 19 for `6.2.4.3/2` (`6.2.3.3/1` at PDF/A-1); at PDF/UA both
count `7.1/3` and `8.2.2/1` fifteen times. A chain of twelve tiling cells, each
selecting the next, is left out: litho counts a cell as a level of the form
budget (`pdf.Limits.MaxFormDepth`, 10 by default) and stops there, where
veraPDF has no budget. Raised to 40, litho reports the innermost cell's
DeviceRGB as veraPDF does.

Remediated at every level in both modes, both tools report the same rule set
on every output: the PDF/A-1 to PDF/A-3, PDF/A-4, PDF/A-4e and PDF/UA-2
outputs are conforming, and `6.9/5` stays at PDF/A-4f and `7.18.1/2` at
PDF/UA-1.

### `qnestreach_2b.pdf`

ISO 19005-1, 6.1.12 and ISO 19005-2, 6.1.13 cap q/Q nesting at 28 levels (ISO
32000-1, Annex C, Table C.1). veraPDF 1.30.2 counts the levels of every content
stream it parses on its own, from zero, and fails the test once for each `q`
past the 28th: in the page's content and in each form XObject, tiling pattern
cell, Type 3 glyph description and annotation appearance the page draws. litho
judged the page's content alone. No corpus file nests that deep outside a
page's content, so this fixture plants one stream per page, each at its own
depth so the count names the stream:

| Page | Stream                                 | Levels | `6.1.13/8` |
| ---- | -------------------------------------- | ------ | ---------- |
| 1    | a form XObject                         | 29     | 1          |
| 2    | a tiling pattern cell                  | 30     | 2          |
| 3    | a Type 3 glyph description             | 31     | 3          |
| 4    | an annotation's normal appearance      | 32     | 4          |
| 5    | page 1's form, painted again           | 29     | 0          |
| 6    | a page 20 deep painting a form 15 deep | 20, 15 | 0          |

| Rule at PDF/A-2b               | veraPDF | litho before | litho |
| ------------------------------ | ------- | ------------ | ----- |
| `6.1.13/8` q/Q nesting past 28 | 10      | 0            | 10    |

veraPDF 1.30.2 and litho report the same rule set and the same counts in all
thirteen cells: `6.1.12/8` counts 10 at PDF/A-1, and PDF/A-4 states no nesting
limit. A CMYK OutputIntent covers the DeviceGray every stream paints in.

No fixer reduces nesting, so remediation keeps `6.1.13/8` (`6.1.12/8` at
PDF/A-1) at every PDF/A-1 to PDF/A-3 level in both modes, on both tools; the
PDF/A-4, PDF/A-4e and PDF/UA-2 outputs are conforming, and `6.9/5` stays at
PDF/A-4f and `7.18.1/2` at PDF/UA-1.

### `shadedefault_2b.pdf`

A Default colour space replaces a device family the content stream selects
through its own resources (ISO 32000-1, 8.6.5.6), and a shading pattern has no
resources of its own (8.7.4.1, Table 76). veraPDF 1.30.2 builds the shading of
a shading pattern with no resources to read a Default from, so it judges the
shading's device family against the output intent alone. The sole intent here
is a CMYK one, which covers every DeviceCMYK, and page 1 paints DeviceRGB,
which it does not. Remediation covers DeviceRGB by replacing the intent with an
sRGB one and covers DeviceCMYK with a `/DefaultCMYK`, which reached none of
these shadings. No corpus file paints such a shading beside DeviceRGB under a
CMYK intent, so this fixture plants one per shape:

| Page | Path                                  | Shading colour space                               |
| ---- | ------------------------------------- | -------------------------------------------------- |
| 1    | `rg` fill, `/P1 scn`                  | Separation `/Spot` over DeviceCMYK, both indirect  |
| 2    | `/P2 scn`, pattern and shading direct | DeviceCMYK                                         |
| 3    | `/P3 scn`                             | DeviceN `/Spot2` over DeviceCMYK, `/Colorants` too |
| 4    | `/CS0 cs`, `k` fill, `/P1 scn`        | the page's `/CS0` is page 1's Separation `/Spot`   |
| 5    | a form whose own resources hold `/P5` | Separation `/Spot` over DeviceCMYK                 |

The source fails `6.2.4.3/2` twice at PDF/A-2b (`6.2.3.3/1` at PDF/A-1) on
both tools, and nothing else in the colour rules. veraPDF 1.30.2 and litho
report the same rule set in all thirteen cells, with the same counts but for
`7.1/3` and `8.2.2/1` at PDF/UA, the family described under
[Instance counts](#instance-counts-which-the-corpus-does-not-pin).

Remediated before the fix, every PDF/A output in both modes failed `6.2.4.3/3`
six times (`6.2.3.3/2` at PDF/A-1) on both tools, for the DeviceCMYK the
shadings hold. veraPDF also failed `6.2.4.4/2` once from part 2 on, where litho
stayed silent: page 4's `/CS0`, read under the page's `/DefaultCMYK`, has the
Default's space as its alternate, and page 5's direct Separation `/Spot`, which
no Default reaches, has DeviceCMYK, two alternates for one colourant. litho now compares a
Separation's alternate as the Default that replaces it where the Separation is
read, as veraPDF builds it.

Remediation now gives each shading's colour space the `/DefaultCMYK` space in
place of DeviceCMYK, as the space itself, an alternate or a `/Colorants`
entry's alternate, so every DeviceCMYK the output paints is characterised by
the one profile. Every output is conforming on both tools in both modes, but
PDF/A-4f's, which keeps `6.9/5`, the embedded file it requires, and both
PDF/UA outputs were conforming before and stay so. Page 4's `/CS0` and the
shadings naming `/Spot` then read as one alternate, so `6.2.4.4/2` stays silent
on the output.

### `realglyphs_ua1.pdf`

veraPDF 1.30.2 keys a `Glyph` by whether the stream showing it is real content.
A PDF/UA or WTPDF validation parses a page's content, and the forms it draws,
as semantic content, and a tiling cell, a Type 3 glyph procedure, an
annotation appearance and a form one of those draws as plain content. A glyph
with no Unicode value shown in both is two glyphs there, and one at PDF/A,
which parses nothing as semantic. Each page plants one shape through Type 3
fonts of its own, whose glyph names carry no Unicode value. A CMYK OutputIntent
covers the DeviceGray every glyph paints in:

| Page | The page shows the glyph, and so does                                   | `7.21.7/1` | `6.2.11.7.2/1` |
| ---- | ----------------------------------------------------------------------- | ---------- | -------------- |
| 1    | a cell the page selects                                                 | 2          | 1              |
| 2    | an annotation appearance                                                | 2          | 1              |
| 3    | a procedure of a second Type 3 font the page shows                      | 3          | 2              |
| 4    | a form a cell draws                                                     | 2          | 1              |
| 5    | a form the page draws (control)                                         | 1          | 1              |
| 6    | nothing; a cell the glyph's own procedure selects shows the other glyph | 2          | 2              |
| 7    | a cell the glyph's own procedure selects, the page showing it in mode 2 | 2          | 1              |

Page 3 counts the second font's glyph too. Pages 6 and 7 hold the cycle
[`type3cycle_2u.pdf`](#type3cycle_2updf) isolates: the cell's other glyph is
judged as any other, and the cell starts in the rendering mode its selecting
procedure starts in, the page's mode 2 on page 7, so the page's glyph is the
cell's at PDF/A.

veraPDF and litho report the same rule set in all thirteen cells and the same
counts: `7.21.7/1` and `8.4.5.8/1` 14 times (also at WTPDF 1.0), `6.2.11.7.2/1`
and `6.3.8/1` 9 times. Before the fix litho counted 8 at each, the rule set
unchanged, so the fixture pins counts the set comparison does not see (see
[Instance counts](#instance-counts-which-the-corpus-does-not-pin)).

Remediated at every level in both modes, both tools report the same rule set
and counts on every output. No fixer gives a glyph name with no Unicode value
one, so the ToUnicode rule stays: 9 times at PDF/A-2u and 3u, 15 at PDF/A-1a,
2a and 3a and at PDF/UA, where auto-tagging puts the page's glyphs in marked
content of their own.

### `type3cycle_2u.pdf`

A Type 3 glyph procedure (ISO 32000-1, 9.6.5) can select a tiling pattern
whose cell (8.7.3.1) shows text in the same font. The page shows code A,
glyph `A`, which the Adobe Glyph List gives a Unicode value; `A`'s procedure
fills with the pattern; the cell shows code B, glyph `r1`, which has none.
veraPDF 1.30.2 judges the cell's glyphs as any other, so `6.2.11.7.2/1` (and
`7.21.7/1`, `8.4.5.8/1` at PDF/UA) fails once, for `r1`, and `6.3.8/1` twice at
PDF/A-1a, which wants a `/ToUnicode` for `A` as well.

litho once judged no glyph of a Type 3 font in a stream its own procedures
reach, read off a page showing the glyph in mode 2 whose cell showed it again:
veraPDF judges that glyph once because the cell starts in the procedure's mode
2, not because it skips the cell. The rule was silent at PDF/A-2a, 2u, 3a, 3u,
UA-1 and UA-2, six rule-set cells. veraPDF and litho now report the same rule
set and counts in all thirteen cells, and remediated at every level in both
modes, on every output.

### `readorder_ua1.pdf`

ISO 14289-1, 7.2 asks the structure tree to follow the reading order (ISO
32000-1, 14.8.2.3 lets it differ from content order), and auto-tagging builds
that tree when remediation to PDF/UA finds a page untagged. The one page here
is untagged, and its content stream does not follow the reading order:

| Drawn | Content                                | Read |
| ----- | -------------------------------------- | ---- |
| 1     | page number `1` at the foot            | none |
| 2     | 14-point heading over the right column | 4    |
| 3     | left column, paragraph one             | 2    |
| 4     | right column, paragraph one            | 5    |
| 5     | left column, paragraph two             | 3    |
| 6     | right column, paragraph two            | 6    |
| 7     | 24-point title across both columns     | 1    |

The page number is a pagination artifact and takes no place in the tree.
Tagged in content order, the tree read the right column's heading first, as
`H1`, and the title last, as `H1` again; and the right column's first paragraph
and the left column's second, adjacent in the stream and a line apart in text
space, folded into one `P` that crossed the gutter. Tagged in reading order it
reads `H1`, `P`, `P`, `H2`, `P`, `P`, as the Read column says.

Before remediation both tools report the untagged page (`6.2/1`, `7.1/3` and
`7.1/11` at PDF/UA-1, `6.2/1`, `8.2.1/1` and `8.2.2/1` at PDF/UA-2) and the
non-embedded font, plus `5/2` and `5/5` at PDF/UA-2, where the packet's PDF/UA-1
claim is the wrong part. The two tools agree in all thirteen cells.
The PDF/UA-1 and PDF/UA-2 outputs, default and aggressive, keep only the font
(`7.21.4.1/1`, `8.4.5.5.1/1`) on both tools, in reading order as in content
order: veraPDF does not judge the order of a tree.

### `readcolumns_ua1.pdf`

The same reading-order question, asked of two shapes `readorder_ua1.pdf` does
not hold. Both pages are untagged, and each sets two columns of six lines line
by line across both, every line a text object of its own, so no two lines of a
column are adjacent in the content stream. The right column's baselines sit
half a line below the left's, so no row of the two reads as a table row.

A marked-content sequence is one contiguous run of the content (ISO 32000-1,
14.6), so one sequence cannot hold a column's lines. ISO 32000-1, 14.7.4.1 lets
a structure element hold several sequences, and auto-tagging now tags each
column as one `P` whose `/K` lists its six lines' MCIDs in reading order. It
tagged each line as a `P` of its own before.

Each page's running head is set in the text object that also sets the page's
title under it, so the object reaches out of the top margin and read as body
text: the head was tagged with the title, as one `H1`. Auto-tagging now cuts
the head off the text object at the line that leaves the margin and marks it
as a pagination artifact (ISO 14289-1, 7.8), its sequence kept between the
object's `BT` and `ET`, as ISO 32000-1, 14.6 asks. Each page reads `H1`, `P`,
`P`, with the head and the page number as artifacts.

Before remediation both tools report the untagged pages and the non-embedded
font, as on `readorder_ua1.pdf`: `6.2/1`, `7.1/3`, `7.1/11` and `7.21.4.1/1`
at PDF/UA-1, and `5/2`, `5/5`, `6.2/1`, `8.2.1/1`, `8.2.2/1` and
`8.4.5.5.1/1` at PDF/UA-2. The two tools agree in all thirteen cells. The
outputs remediated at every level, default and aggressive, keep only the font
on both tools, and at PDF/A-4f the missing `EmbeddedFiles` (`6.9/5`) too, as
they did when each line was a paragraph.

### `prosecolumns_ua1.pdf`

The question `readcolumns_ua1.pdf` steps around: its right column's baselines
sit half a line below the left's so that no row of the two reads as a table
row. Page 1 sets two columns of eight lines of running text line by line across
both, every line a text object of its own and every right-hand line on the
baseline of its left-hand neighbour, with no rule anywhere. Every row of the
two aligned, so auto-tagging tagged the columns as one `Table` of eight `TR`
of two `TD`, a grid of half-sentences that reads across the gutter at every
line. ISO 14289-1, 7.5 reserves `Table` for tabular content and 7.2 asks the
tree to follow the reading order: a grid whose cells are, in every column, for
the most part single lines of running text, twelve sizes wide at least and
with no gap inside them a gutter wide, and which no rule crosses (stroked, or
filled as on `filledrules_ua1.pdf`), now reads as columns. Each column is one `P` whose `/K` lists its eight lines'
MCIDs, left then right, under the page title, now an `H1`.

Page 2 is the control: a borderless table of five rows of three short cells,
an item, a quantity and a price, still tagged `Table > TR > TD`, under its
title, an `H1`.

Before remediation both tools report the untagged pages and the non-embedded
font, as on `readcolumns_ua1.pdf`: `6.2/1`, `7.1/3`, `7.1/11` and
`7.21.4.1/1` at PDF/UA-1, and `5/2`, `5/5`, `6.2/1`, `8.2.1/1`, `8.2.2/1` and
`8.4.5.5.1/1` at PDF/UA-2. The two tools agree in all thirteen cells. The
outputs remediated at every level, default and aggressive, keep only the font
on veraPDF, and at PDF/A-4f the missing `EmbeddedFiles` (`6.9/5`) too. The
PDF/UA-1 and PDF/UA-2 outputs drew the same rules when the columns were tagged
as a table: veraPDF does not judge whether a `Table` holds tabular content.

### `filledrules_ua1.pdf`

The ruling `prosecolumns_ua1.pdf` asks about, drawn as producers draw it. ISO
32000-1, 8.5.3.3 paints a filled path as it does a stroked one, and Word
through PDFMaker or Distiller, LibreOffice and TeX fill a rule as a rectangle a
rule's width tall. Page 1 is a table of five rows of two wide cells, each a
single line of text twelve sizes wide at least, ruled the way Word draws a
table's borders: every border a row of filled rectangles 0.48 points tall, one
per cell and a square at each corner, none of which crosses the gutter alone,
and the column borders filled the same way. Only stroked rules were read, so
auto-tagging read the grid as two columns of prose, each cell its own `P`,
left column then right. A filled bar at most 2 points tall now rules a grid as
a stroked line does, and a border drawn cell by cell is one rule where its
pieces abut: the page reads as one `Table` of five `TR` of two `TD`. The title
over it stays an `H1`: the heading pass judges a title left alone beside a
table against the size of the cells, not against its own.

Page 2 is the control: two columns of six lines of prose on shared baselines,
under a title that a full-width filled bar a point tall underlines more than a
line above the columns, on a light shaded panel that crosses the gutter. The
bar rules no row, being clear of the grid, and the panel is no rule, being as
tall as the text it sits behind. Each column still reads as one `P` whose `/K`
lists its six lines' MCIDs, under the title, an `H1`.

Before remediation both tools report the untagged pages and the non-embedded
font, as on `prosecolumns_ua1.pdf`: `6.2/1`, `7.1/3`, `7.1/11` and
`7.21.4.1/1` at PDF/UA-1, and `5/2`, `5/5`, `6.2/1`, `8.2.1/1`, `8.2.2/1` and
`8.4.5.5.1/1` at PDF/UA-2. The two tools agree in all thirteen cells. The
outputs remediated at every level, default and aggressive, keep only the font
on both tools, and at PDF/A-4f the missing `EmbeddedFiles` (`6.9/5`) too, as
they did when the cells were paragraphs.

### `texlig_2b.pdf`

ISO 32000-1, 9.10.2 derives a simple font's Unicode value from the glyph name a
code selects. TeX text fonts set the "fi" ligature at code `0C` under a name no
glyph list holds, so the code has no value. PDF.js shows the code as itself
and reads a form feed as a word space, which cuts "verification" in two. litho
reads it as no character and keeps the word whole: correct words over oracle
agreement is a user decision (2026-10-19). ISO 32000-1, 9.3.3 applies word
spacing to code `20` whatever glyph it selects, so code `20` reads as a space
whatever its name. Font `F1` embeds a Type 1 program on `StandardEncoding`
holding the letters it shows and `T0C`, carries no `/ToUnicode`, and states
`/Differences [12 /T0C 32 /T20]`. The program holds no `T20`, and `/Widths`
gives code `20` no advance, so no gap divides the words it separates:

| Line                             | litho reads       | Read with `0C` as a space |
| -------------------------------- | ----------------- | ------------------------- |
| `veri` `0C` `cation` `20` `text` | `verication text` | `veri cation text`        |
| `code` `20` `space`              | `code space`      | `code space`              |

The `text` cell scores the reading against `text-overrides.json`, which
replaces PDF.js's `veri cation` with it (see [Scope](#scope)); the `mutate-*`
cells compare litho's text on the output with litho's on the input, and
compliance, auto-tagging and remediation do not move when `0C` reads as a
space. `TestExtractTextKeepsAnUnmappedLigatureInsideItsWord` in `pkg/pdf` also
reads the fixture, and fails on the wave-54 reading.

Code `20` reaches no fallback in litho: a `/Differences` name that maps
nothing leaves the base encoding's `space` in place, and every base litho
builds for a simple font fills code `20`. The second line still pins that the
code alone, with no gap, divides the words.

| Rule at PDF/A-2u                    | veraPDF | litho |
| ----------------------------------- | ------- | ----- |
| `6.2.11.4.1/2` glyph not in program | 1       | 1     |
| `6.2.11.7.2/1` no Unicode value     | 2       | 2     |
| `6.6.4/3` conformance claimed is B  | 1       | 1     |

Both codes fail the value rule, `20` included: veraPDF and litho read a value
from the name the encoding gives a code, and `T20` names nothing. The glyph
and value rules answer the same way at PDF/A-2a, 3a and 3u, at PDF/UA-1
(`7.21.4.1/2`, `7.21.7/1`) and at PDF/UA-2 (`8.4.5.5.1/2`, `8.4.5.8/1`). At
PDF/A-4, 4e and 4f only the glyph rule fires (`6.2.10.4.1/2`). At PDF/A-1a
`6.3.8/1` fires for both codes, and at PDF/A-1a and 1b `6.3.5/1` for the
missing glyph. At PDF/A-2b only the missing glyph fires.
veraPDF 1.30.2 and litho report the same rule set, with the same failed-check
counts, in all thirteen cells.

### `refreach_ua1.pdf`

ISO 14289-1, 7.20 forbids reference XObjects, form XObjects whose dictionary
carries `/Ref` (ISO 32000-1, 8.10.4, Table 95). veraPDF 1.30.2 states test 1 on
the form object a `Do` builds, as it states the PDF/A reference rules (ISO
19005-1, 6.2.6 t1, ISO 19005-2, 6.2.9 t2, ISO 19005-4, 6.2.8.2 t1), so it judges
a form at every `Do` that draws it, in every content stream it parses. litho
judged the forms the page's resources listed, and those their resources listed
in turn, once each: it missed every form a pattern cell or a Type 3 glyph draws,
counted a form drawn twice once, and failed a form nothing draws. No corpus file
draws a reference XObject at PDF/UA-1, so this fixture plants one path per page,
each with a reference form of its own:

| Page | Path                                             | `7.20/1`, veraPDF | litho before |
| ---- | ------------------------------------------------ | ----------------- | ------------ |
| 1    | tiling cell, `/R Do`                             | 1                 | 0            |
| 2    | Type 3 glyph the page shows, `/R Do`             | 1                 | 0            |
| 3    | tiling cell, Type 3 glyph, `/R Do`               | 1                 | 0            |
| 4    | the page's content, `/R Do /R Do`                | 2                 | 1            |
| 5    | annotation appearance, `/R Do`                   | 1                 | 0            |
| 6    | the page, a cell and a Type 3 font list R, no Do | 0                 | 1            |

Page 6 is the must-not-fire control: its page resources, the cell of the
pattern it selects and the Type 3 font it shows text in each list one reference
form that nothing draws. The PDF/A cells state their reference rule 6 times,
on the same forms, in veraPDF and in litho alike, and did before the fix.
veraPDF 1.30.2 and litho report the same rule set, with the same failed-check
counts, in all thirteen cells.

Remediated at every level in both modes, every output is clean in both tools
except PDF/A-4f, where `6.9/5` stays (no embedded file): litho deletes `/Ref`
from every form the rules name.

### `headingnest_ua2.pdf`

ISO 14289-2, 8.2.5.12 forbids the generic `H` heading. ISO 32000-1, Table 335
makes `H` the label of the division it is the first child of, whose level a
reader takes from the nesting of the sections, so in a strongly structured
document the nesting states the level `H1` to `H6` write out. Remediation
renamed only an `H` nested in no division. No corpus file nests an `H` more
than one `Sect` deep, so this fixture does:

| Element                       | Construct                    | Remediated |
| ----------------------------- | ---------------------------- | ---------- |
| `Document` > `Sect`           | `H`, then a `P` and a `Sect` | `H1`       |
| `Sect` > `Sect`               | `H`, then a `P` and a `Sect` | `H2`       |
| `Sect` > `Sect` > `Sect`      | `H`, then a `P`              | `H3`       |
| `Document` > `Sect`, the last | `H`, then a `P`              | `H1`       |

The four `H` are the file's one defect at PDF/UA-2: the font is embedded, the
XMP packet claims PDF/UA-2 and every element is a standard PDF 2.0 type. litho
and veraPDF 1.30.2 report the same rule set in all thirteen cells, and at both
WTPDF levels (`8.2.5.12/1` with the PDF Declaration WTPDF requires). PDF/UA-1
reports no heading rule: the document uses `H` alone, which ISO 14289-1, 7.4.4
accepts. Remediated at PDF/UA-2 and both WTPDF levels, default and aggressive,
every output is clean in both tools; before the repair every one kept
`8.2.5.12/1`.

### `headingart_ua2.pdf`

`headingnest_ua2.pdf` numbers an `H` by the `Sect` elements around it. ISO
32000-1, 14.8.4.3.5 has a strongly structured document's grouping elements nest
to reflect its organization "into articles, sections, subsections", and ISO/TS
32005, Table 5 bounds an `Art`, like a `Sect`, to one `H`, so an `Art` adds a
level as a `Sect` does. A `Part` or a `Div` adds none: Table 5 looks through
both, and the repair leaves an `H` under one alone. The PDF 2.0 namespace
defines no `Art`, so each `Art` here is in the PDF 1.7 namespace and every
other element in the PDF 2.0 one. No corpus file nests an `H` in a standard
`Art`, so this fixture does:

| Element                      | Construct                    | Remediated |
| ---------------------------- | ---------------------------- | ---------- |
| `Document` > `Art`           | `H`, then a `P` and a `Sect` | `H1`       |
| `Art` > `Sect`               | `H`, then a `P`              | `H2`       |
| `Document` > `Art`, the last | `H`, then a `P`              | `H1`       |

The three `H` are the file's one defect at PDF/UA-2: the font is embedded, the
XMP packet claims PDF/UA-2 and every element is a standard type. litho and
veraPDF 1.30.2 report the same rule set in all thirteen cells, and at both
WTPDF levels (`8.2.5.12/1` with the PDF Declaration WTPDF requires). PDF/UA-1
reports no heading rule. Remediated at PDF/UA-2 and both WTPDF levels, default
and aggressive, every output is clean in both tools; before the repair counted
`Art`, every one kept `8.2.5.12/1`.

### `formreuse_ua1.pdf`

ISO 14289-1, 7.20 has a form XObject whose content holds MCIDs referenced once
(ISO 32000-1, 14.7.2). veraPDF 1.30.2 states test 2 on the form object a `Do`
or an appearance entry builds: a form carrying `/StructParents` passes at its
first reference and fails at every later one. It parses a content stream once
for the document, so a `Do` counts once however many pages or streams draw the
stream holding it, and an appearance entry counts once per annotation. litho
counted the `Do` operators of the pages' own content alone, so it missed every
second reference a pattern cell, a Type 3 glyph, a form or an appearance makes.
No corpus file references a marked form twice at PDF/UA-1, so this fixture
plants one path per page, each with a marked form of its own:

| Page | Path                                                 | `7.20/2`, veraPDF | litho before |
| ---- | ---------------------------------------------------- | ----------------- | ------------ |
| 1    | the page and a tiling cell it selects, `/F Do` each  | 1                 | 0            |
| 2    | the page and a Type 3 glyph it shows, `/F Do` each   | 1                 | 0            |
| 3    | the page and a form it draws, `/F Do` each           | 1                 | 0            |
| 4    | the normal appearance of two annotations             | 1                 | 0            |
| 5    | one annotation's normal and down appearance          | 1                 | 0            |
| 6    | the page's `/F Do` and an annotation's appearance    | 1                 | 0            |
| 7    | a form, a cell and a glyph drawn twice, each `/F Do` | 0                 | 0            |

Page 7 is the must-not-fire control: it draws twice a form that draws a marked
form once, selects twice a cell that draws another, shows twice a glyph that
draws a third, and draws twice a form without `/StructParents` and a form
carrying `/StructParent`, the key of a form that is one marked item itself.
veraPDF 1.30.2 and litho report the same rule set, with the same failed-check
counts, in all thirteen cells.

Remediated at PDF/UA-1 in both modes, both tools keep `7.20/2` six times and
`7.1/3` once: no fixer gives a reference a form of its own.

### `formshare_ua1.pdf`

A `Do` in a `/Contents` two pages share is one reference to the form it draws:
veraPDF 1.30.2 parses the stream once, whether the second page reads it under
the same resources or under resources of its own. litho read the content of
every page again and stated `7.20/2` once for each marked form such a stream
draws. Pages 1 and 2 share a stream drawing one marked form, page 2 under
resources of its own that name the same form and add an ExtGState; pages 3
and 4 share another under one resource dictionary. Every page is a
must-not-fire control: veraPDF states `7.20/2` nowhere, litho stated it twice.
veraPDF 1.30.2 and litho report the same rule set, with the same failed-check
counts, in all thirteen cells.

Remediated at PDF/UA-1 in both modes, both tools report nothing. The fixer
wrapping untagged page content in a marked-content sequence wrote one stream
per page, so each marked form gained a second reference and both tools stated
`7.20/2` twice. Pages naming one `/Contents` object now name one wrapped
stream, its MCID resolving through each page's own `/StructParents`.

### `sharedtag_ua1.pdf`

The auto-tagger's twin of `formshare_ua1.pdf`. Pages 1 and 2 share a stream
showing Type 3 text and drawing a marked form under one resource dictionary;
page 3 shows the same text from a stream of its own. Every page is a
must-not-fire control for `7.20/2`. veraPDF 1.30.2 and litho report the same
rule set in all thirteen cells, before remediation and after it, default and
aggressive.

Remediated at PDF/UA-1, both tools state `7.1/2` once, the marked form drawn
inside the artifact the auto-tagger makes of the graphics around it. The
auto-tagger marked each page in a stream of its own, and both tools stated
`7.20/2` beside it; pages naming one `/Contents` object that they mark alike
now name one marked stream.

### `sharedclose_ua1.pdf`

The open-sequence twin of `formshare_ua1.pdf`. Pages 1 and 2 share a stream
drawing a marked form, then opening an `/Artifact` sequence it never closes;
page 3 opens the same sequence in a stream of its own. Every page is a
must-not-fire control for `7.20/2`, and the unclosed sequences are the one
UA-1 content finding, `7.1/3` on both tools. veraPDF 1.30.2 and litho report
the same rule set in all thirteen cells, before remediation and after it,
default and aggressive.

Remediated at PDF/UA-1 in both modes, both tools report nothing. The fixer
closing the sequence appended its EMC in an array of its own for each page, so
veraPDF parsed the shared stream once per array and both tools stated `7.20/2`;
pages naming one `/Contents` object now name one closed array.

### `qshyphen_2b.pdf`

PDF.js advances its text position past no soft hyphen (U+00AD), so it reads
every glyph after one, until a text-positioning operator sets the position,
the hyphen's advance before where ISO 32000-1, 9.4.4 puts it. It saves its
whole text state at `q`, text matrix included, and restores it at `Q`, as
litho's machine does. ISO 32000-1, 8.2, Figure 9 allows no `q` or `Q` inside a
text object, and veraPDF logs each one there as a warning, so no clause says
what `Q` does to the text matrix. No corpus file shows a soft hyphen inside
`q`...`Q` within a text object. Font `F1` embeds a Type 1 program whose glyphs
are 600 units wide and maps code `AD` to U+00AD through its `/ToUnicode` CMap;
at 12 points the TJ number -300 leaves a 3.6-unit gap, which PDF.js reads as a
word space:

| Line                                         | PDF.js and litho read | Before       |
| -------------------------------------------- | --------------------- | ------------ |
| `(left)` `q` `(AD)` `Q` `[-300 (right)]`     | `left right`          | `leftright`  |
| `(outerAD)` `q` `(AD)` `Q` `[-300 (inner)]`  | `outer inner`         | `outerinner` |
| `(deep)` `q q (AD) Q (AD) Q` `[-300 (nest)]` | `deep nest`           | `deepnest`   |
| `(softAD)` `q 0 -30 Td Q` `(ware)`           | `software`            | `soft ware`  |
| `(plain)` `(AD)` `[-300 (text)]`, control    | `plain text`          | `plain text` |

Before, litho kept the shift of a soft hyphen shown inside `q`...`Q` after
`Q`, which closed the gap on the first three lines, and dropped the shift of
the one before `q` at the `Td` inside, which opened one on the fourth. The
shift now rides the graphics state with the text matrix it measures.
veraPDF 1.30.2 and litho report the same rule set, with the same failed-check
counts, in all thirteen cells; at PDF/A-2b both report nothing.

### `transparency_2b.pdf`

The renderer composites every painting into the transparency group being
painted (ISO 32000-1, 11.4). The render goldens of the corpora held soft masks
and blend modes, but no knockout group, no luminosity mask with a `/BC` or a
`/TR`, and no blend mode over the bare page, so the `render` cell compared
none of them. The file paints in DeviceRGB and DeviceGray:

| Page | Draws                                                                                   |
| ---- | --------------------------------------------------------------------------------------- |
| 1    | four groups at ca 0.8 over a band: non-isolated, isolated, knockout, isolated knockout, |
|      | each two squares in Multiply at ca 0.6; an isolated group holding a knockout group of   |
|      | Difference squares, composited in Screen                                                |
| 2    | a luminosity mask over a grey ramp, `/BC` 0.5 outside its box, and the same through an  |
|      | inverting `/TR`; an alpha mask whose group holds an opaque square and one at ca 0.4     |
| 3    | the sixteen blend modes, one per cell, over two backdrop colours and the bare page      |

Rendered at 72 dpi, litho matches poppler 26.09.0 on all three pages, where
litho before transparency mismatched all three (1880, 14750 and 8181 differing
pixels). The page group's transparent backdrop (11.4.7) is what page 3's top
row shows: poppler, like litho, paints Screen and Difference over the bare page
in the source's own colour. poppler ignores the alpha source flag, text
knockout and a graphics-state soft mask over an image with its own `/SMask`,
so the file holds none of them; litho's unit tests check those against the
specification.

The file declares no OutputIntent and its pages no `/Group`, so DeviceRGB
fails `6.2.4.3/2`, DeviceGray `6.2.4.3/4`, and every
page holding transparency `6.2.10/2` at PDF/A-2b. veraPDF 1.30.2 and litho
report the same rule set in all thirteen cells.

### `annotsynth_2b.pdf`

The renderer draws an annotation with no appearance stream from its dictionary
where ISO 32000-1, 12.5.6 defines how (`pkg/internal/render/annot`, rendering
only). The render goldens of the corpora held Square, Circle, Line, PolyLine,
Ink, Highlight, StrikeOut and Link annotations with no `/AP`, but no Polygon,
Underline or Squiggly, so the `render` cell compared none of them. The page
draws, in DeviceRGB:

| Annotation | Draws                                                                  |
| ---------- | ---------------------------------------------------------------------- |
| Polygon    | a triangle filled blue with a red border 3 wide                        |
| Polygon    | a pentagon filled yellow with a dashed border, at `/CA` 0.5 over a bar |
| Underline  | a red line along the bottom of two quadrilaterals over cyan bars       |
| Squiggly   | a green zig-zag along the bottom of a quadrilateral over a cyan bar    |

Rendered at 72 dpi, litho matches poppler 26.09.0 (320 differing pixels of
40000), where litho drawing appearance streams only left all four undrawn.

Each annotation lacks the `/AP` ISO 19005-2, 6.3.3 requires, so `6.3.3/1` fires
at PDF/A-2 and later, and the file declares no OutputIntent, so DeviceRGB fails
`6.2.4.3/2`. PDF/A-1 requires no appearance stream; there the Polygon, a PDF
1.5 type, fails `6.5.2/1`, the `/C` and `/IC` arrays `6.5.3/3`, the `/CA` of 0.5
`6.5.3/1`, and DeviceRGB `6.2.3.3/1`. veraPDF 1.30.2 and litho report the same
rule set in all thirteen cells.

### `tounicodepair_2u.pdf`

ISO 32000-1, 9.6.1 makes a simple font's codes one byte each, so a
`/ToUnicode` CMap whose codespace is two bytes wide does not make a byte pair
one code. PDF.js keys the CMap by each code's value and looks each byte up
there, so `<006C>` maps code `6C`; veraPDF looks a simple font's code up by its
value too. No file of the seven corpora shows a byte pair its simple font's
`/ToUnicode` maps. Font `F1` embeds a Type 1 program on `StandardEncoding`
holding the glyphs it shows, and its `/ToUnicode` declares `<0000>` to
`<FFFF>`, maps every shown code by its two-byte value, and maps the pairs
`<6C6C>` to `X` and `<6F20>` to `Y`. The page shows one line:

| Line          | litho and PDF.js read | Read two bytes a mapped pair |
| ------------- | --------------------- | ---------------------------- |
| `Hello world` | `Hello world`         | `HeXYworld`                  |

`TestExtractTextReadsASimpleFontOneByteACode` in `pkg/pdf` reads the fixture,
and fails on the reading before wave C2. Redacting the first ten glyphs leaves
`d`. Every shown code has a Unicode value, so the file conforms at PDF/A-2b and
PDF/A-2u; elsewhere only the claim and the profile's own structure rules fire.
veraPDF 1.30.2 and litho report the same rule set in all thirteen cells.

### `flattentext_2b.pdf`

`Writer.FlattenTransparency` composites each region of transparent content into
an opaque image and keeps every text object as text, drawn above the image. The
corpora hold transparency, but no blend mode drawn over text, no transparent
text over content, no transparency group showing text and no text under a
constant alpha of 0, so the `flatten` cell never saw what flattening does with
text. Every glyph is a Type 3 glyph painting in the text's colour, with a
`/ToUnicode` map, so the text extracts and draws alike everywhere:

| Page | Draws                                                                                |
| ---- | ------------------------------------------------------------------------------------ |
| 1    | text at ca 0.5 and Multiply text on the bare page, and a square at ca 0.5            |
| 2    | a knockout group of two squares at ca 0.6 with text between them; a non-isolated     |
|      | group holding an isolated one that shows text and a square under a luminosity mask   |
| 3    | text under ca 0 over a grey page, text stroked under CA 0 and filled, a square at ca |
|      | 0.5                                                                                  |
| 4    | a line of text, then a Multiply rectangle and a Screen rectangle over parts of it    |
| 5    | text at ca 0.5 over a blue band, and text under a luminosity soft mask               |

Flattened, page 1's text draws exactly in its colour composited over white,
page 2's groups are composited with their text above them, and page 3's
invisible text is drawn in rendering mode 3 and its stroked text in mode 0.
The `flatten` cell renders those three pages, and litho and poppler 26.09.0
draw each like its source. Pages 4 and 5 hold what flattening approximates and
reports: the text the Screen rectangle tinted draws above it untinted
(`TextUnderTransparency` 1; Multiply leaves black text black, which stays
exact), the text over the band and the soft-masked text draw opaque
(`TransparentText` 2). The cell's text and transparency checks read all five pages; no
transparency is left.

The file declares no OutputIntent and its pages no `/Group`, so DeviceRGB
fails `6.2.4.3/2`, DeviceGray `6.2.4.3/4`, and every page holding
transparency `6.2.10/2` at PDF/A-2b. veraPDF 1.30.2 and litho report the same
rule set in all thirteen cells.

### `blendspace_2b.pdf`

A transparency group composites its elements in the blending colour space its
`/CS` names (ISO 32000-1, 11.3.4, Table 147): a blend mode in a CMYK group
applies to the complements of the inks (11.3.5), a non-separable one takes K
from one side, and a luminosity mask's group in DeviceGray blends greys. The
corpora hold CMYK groups that blend, but none the render goldens sample, so no
cell compared a group blending outside sRGB. Every colour is DeviceCMYK:

| Page | Draws                                                                                 |
| ---- | ------------------------------------------------------------------------------------- |
| 1    | in a CMYK page group, an isolated CMYK group of a cyan square and a magenta one       |
|      | Multiply over its right half, and an isolated knockout CMYK group of the same squares |
|      | Multiply at ca 0.7 over yellow                                                        |
| 2    | a luminosity mask whose group blends cyan and magenta Multiply in DeviceGray, over a  |
|      | black fill                                                                            |
| 3    | in a CMYK page group, black ink and cyan ink at ca 0.5, each over the other           |

litho draws page 1's overlap as the ink of both, `1 1 0 0 k`; poppler 26.09.0
and PDFium 4.30.0 (pypdfium2) blend every group in RGB and draw it as the RGB
product of the two colours, 48 levels apart in blue, inside the render cell's
tolerance, so all three pages match poppler. Flattened, page 3's composite is
a DeviceCMYK image holding black and cyan ink alone, and the `flatten` cell
draws all three pages like their source.

The file declares no OutputIntent, so DeviceCMYK fails `6.2.4.3/3` at
PDF/A-2b. veraPDF 1.30.2 and litho report the same rule set in all thirteen
cells.

### `flattenannots_2b.pdf`

`Writer.FlattenTransparency` draws into the page content the appearance of each
annotation that paints with transparency, as a viewer places it (ISO 32000-1,
12.5.5, Algorithm 8.1), leaves the annotation with an appearance that paints
nothing, and flattens the page. The corpora hold transparent annotations, but
the `flatten` cell's render sample reaches few kinds, none synthesised from its
dictionary, no annotation a viewer draws under a transparent one, and no
stroked straight line, whose path box has no area. Every glyph is a Type 3
glyph painting in the text's colour, with a `/ToUnicode` map:

| Page | `/Annots`, in order                                                                |
| ---- | ---------------------------------------------------------------------------------- |
| 1    | an opaque Circle the Highlight overlaps, a Multiply Highlight over text with a     |
|      | closed Popup, a Square at `/CA` 0.5 with an opaque appearance, a Square at `/CA`   |
|      | 0.5 with no appearance, an opaque Ink apart from the rest, a hidden Square at 0.5  |
| 2    | a FreeText whose background paints at ca 0.6 under opaque text                     |
| 3    | an Ink annotation stroking a straight line at ca 0.7 below text                    |
| 4    | the same Ink line through the text, and a FreeText whose text paints at ca 0.6 too |

Flattened, page 1 draws four annotations into its content: the Highlight and
both Squares paint with transparency, the one with no appearance drawn as litho
synthesises it, and the Circle, which a viewer drew under the Highlight, is
drawn before it so the order holds. The Popup and the opaque Ink stay
annotations, and the hidden Square loses its appearance with nothing drawn
(`EmptiedAnnotations` 1). The FreeText's text joins its page's text. The
`flatten` cell renders the first three pages, and litho and poppler 26.09.0
draw each like its source; before wave F2 the cell skipped the file as having
nothing to flatten, with eight annotations left painting with transparency.
Page 4 holds what flattening approximates and reports: the text the Ink line
crosses draws above it untinted (`TextUnderTransparency` 1), and the
FreeText's transparent text draws opaque (`TransparentText` 1). No
transparency is left.

The file declares no OutputIntent and its pages no `/Group`, so DeviceRGB
fails `6.2.4.3/2`, DeviceGray `6.2.4.3/4`, and every page holding
transparency `6.2.10/2` at PDF/A-2b; the Square with no appearance fails
`6.3.3/1` and the hidden one `6.3.2/2`. veraPDF 1.30.2 and litho report the
same rule set in all thirteen cells.

### `flattenrefuse_1b.pdf`

A PDF/A-1 remediation flattens the transparency ISO 19005-1, 6.4 forbids, page
by page when a page refuses, and sets opaque what flattening leaves. Every page
the corpora flatten fits the render pixel budget at some resolution, so no
page reached the refusal. This file holds one beside a page that flattens:

| Page | Size (pt)   | Draws                                                             |
| ---- | ----------- | ----------------------------------------------------------------- |
| 1    | 612 x 792   | a red square at ca 0.5 over a blue band                           |
| 2    | 2000 x 2000 | the same, the square inside eight nested isolated knockout groups |

Flattening composites page 2 with two page-sized planes for each open isolated
knockout group, which no renderer can elide, and eight of them pass the 50
million pixel render budget at every resolution it tries, down to 72 dpi:
`litho flatten-transparency` refuses the file. Its PDF/A-1a and PDF/A-1b
remediations, default and aggressive, flatten page 1 and set page 2's
transparency opaque, and veraPDF 1.30.2 and litho report nothing on the
outputs. Page 1 of the output draws like its source; page 2 draws its square
opaque, as the opaque normalization does. Each group holds one element,
composited over the group's transparent initial backdrop and the result over
the page (ISO 32000-1, 11.4.6 and 11.4.7), so a renderer drawing page 2
without the groups draws the same page: litho at 72 dpi paints the groups past
its budget into the page, and poppler 26.09.0 draws both pages as litho does.

The file declares no OutputIntent, so DeviceRGB fails `6.2.3.3/1` at PDF/A-1b
and `6.2.4.3/2` at the later parts; the groups fail `6.4/3` and the constant
alpha `6.4/6` at PDF/A-1b, and `6.2.10/2` at PDF/A-2b, where the pages state no
`/Group`. veraPDF 1.30.2 and litho report the same rule set in all thirteen
cells.

### `iccalternate_2b.pdf`

An ICCBased space whose profile a reader cannot use reads as the space its
stream's `/Alternate` names, the colour values taken as they are, and as the
device space of `/N` components only when `/Alternate` is absent (ISO 32000-1,
8.6.5.5, Table 66). The corpora's unusable profiles carry no `/Alternate` or
name the device space of `/N`, which reads the same either way, so no cell
reached an alternate of another family. Each profile here is a version 2.1
monitor header with an empty tag table, as `iccbased_4.pdf`'s are: it holds no
transform, so litho, poppler 26.09.0 (lcms2 reports "Can't create transform")
and PDF.js all fall back to the alternate:

| Draws                                   | Space                                              |
| --------------------------------------- | -------------------------------------------------- |
| a square, `50 20 -30 sc`                | an RGB header, `/Alternate` Lab, `/Range` of Lab's |
| a square, `0.5 sc`                      | a GRAY header, `/Alternate` CalGray of gamma 1     |
| a 2x1 image of two Lab samples          | the RGB-header space                               |
| a 2x1 image of an Indexed space over it | Indexed, its base the RGB-header space             |

litho read every one as the device space of `/N`, 381,700 pixels per million
apart from poppler under the render cell's tolerances; it now reads the
alternates and draws the page as poppler does, 0 pixels apart. `convert-colors`
already read the alternate, and reads it now through the same rule.

ISO 19005-2, 6.2.4.2 accepts the profiles' version and device class, and the
file needs no OutputIntent: Lab and CalGray are CIE-based. It is compliant at
PDF/A-2b; the other cells report the part number and the missing structure
and language entries. veraPDF 1.30.2 and litho report the same rule set in all
thirteen cells.

### `formplacement_2b.pdf`

A viewer draws a widget's appearance by ISO 32000-1, 12.5.5, Algorithm 8.1: it
transforms the form's `/BBox` by its `/Matrix` and scales and translates the
box it gets onto the annotation's `/Rect`. Flattening a form, and the drawing
of signed signatures every rewrite does, translated the appearance to the first
corner `/Rect` names instead, right only for a box at the origin sized to
`/Rect`. The corpora reach few other appearances, none turned by `/Matrix`, so
this file holds one text field, with no value, per case, each widget tagged
under a `Form` element of its own:

| Field       | `/Rect`             | `/BBox`             | `/Matrix`           | A viewer draws it      |
| ----------- | ------------------- | ------------------- | ------------------- | ---------------------- |
| `control`   | `[20 200 100 260]`  | `[0 0 80 60]`       | none                | translated             |
| `offset`    | `[120 200 200 260]` | `[200 100 280 160]` | none                | its box moved          |
| `scaled`    | `[220 200 300 260]` | `[0 0 40 30]`       | none                | scaled by 2            |
| `rotated`   | `[20 40 80 120]`    | `[0 0 80 60]`       | a quarter turn      | turned upright         |
| `combined`  | `[120 40 180 120]`  | `[10 20 50 50]`     | a quarter turn      | turned, scaled by 2    |
| `reversed`  | `[300 120 220 40]`  | `[0 0 80 80]`       | translates `40 -30` | the translation undone |
| `collapsed` | `[350 50 350 50]`   | `[0 0 20 20]`       | none                | not at all             |

Each appearance fills its box in a light colour, a dark square in its
lower-left corner and a red border, so a drawing moved, unturned or unscaled
shows. `reversed` names its corners in the other order (7.9.5), and
`collapsed` has no area, so Algorithm 8.1 scales it to nothing.

`litho flatten-form` draws the six visible appearances where litho and poppler
26.09.0 draw them in the source: litho's and poppler's renderings of the output
match the source's to the pixel at 72 dpi, and `collapsed` leaves drawing
nothing. The translation drew every case but `control` elsewhere, 21,996
pixels apart from the source in poppler's rendering. Each appearance is drawn
as the content of its widget's `Form` element. The output fails what the source
fails at every PDF/A level, and at PDF/UA it passes the widget rules the source
fails (`7.18.1/3` and `7.18.3/1` at PDF/UA-1; `8.9.3.3/1`, `8.10.2.3/1` and
`8.9.2.4.13/1` at PDF/UA-2) and fails nothing new, on litho and veraPDF 1.30.2
alike. Untagged, as the file first was, the drawing went into the page
unmarked, which added `7.1/3` at PDF/UA-1 and `8.2.2/1` at PDF/UA-2 to the
`mutate-flatten` cell (an untagged document gets artifacts only when it
claims PDF/UA or WTPDF, see `formuntagged_ua1.pdf`). The file holds no signed
signature, so a rewrite draws nothing more.

The file declares an RGB OutputIntent and every widget prints, so it conforms
to PDF/A-2b. The other cells report what the part-2, level-B claim draws at
the levels it does not match: identification, at part 4 the `%PDF-1.7`
header, `6.9/5` at PDF/A-4f, and at PDF/UA the document-level rules
flattening does not touch (`5/1`, `7.1/10` and `7.2/33` at PDF/UA-1).
veraPDF 1.30.2 and litho report the same rule set in all thirteen cells, on
the source and on the `flatten-form` output.

### `formuntagged_ua1.pdf`

ISO 14289-1, 7.1 and ISO 14289-2, 8.2.2 require every piece of page content
of a document claiming conformance to be tagged as real content or marked as
an artifact. A widget is not page content, so a document with no structure
tree passes both tests while its page draws nothing of its own. Flattening the
form draws each appearance into the page, where no structure element can take
it. In a document whose XMP claims PDF/UA (`pdfuaid:part`) or WTPDF,
`FlattenForm` and both signature bakes mark it as an artifact (ISO 32000-1,
14.8.2.2), a plain `/Artifact` sequence: Table 330 makes every property
optional, and no artifact `/Type` names a field's look. A document claiming
neither keeps it as plain content, which default text extraction returns. The
other fixtures whose widgets paint are tagged, and the untagged ones paint
nothing or claim no PDF/UA, so this file gives the `mutate-flatten` cell the
shape:

| Field     | Kind      | `/Rect`            | Appearance                                   |
| --------- | --------- | ------------------ | -------------------------------------------- |
| `name`    | text      | `[40 220 240 250]` | a light box, a dark corner and a border      |
| `agree`   | check box | `[40 170 60 190]`  | `/On`, a filled square, selected by `/AS`    |
| `comment` | text      | `[40 60 360 140]`  | the same box, its `/BBox` offset from `0, 0` |

The page's own content draws nothing. The file has no `/StructTreeRoot` and no
`/MarkInfo`, claims PDF/UA-1 in its XMP, and declares an RGB OutputIntent.

`litho flatten-form` draws the three appearances where litho and poppler
26.09.0 draw them in the source, to the pixel at 72 dpi, each inside an
`/Artifact` sequence. The output fails a subset of what the source fails at
all thirteen levels, on litho and veraPDF 1.30.2 alike: with the widgets gone
it passes the annotation rules (`7.18.3/1`, no `/Tabs`, and `7.18.4/1`, no
`Form` element, at PDF/UA-1; `8.9.3.3/1`, no `/Tabs`, at PDF/UA-2) and adds
nothing. Drawn unmarked, as before, the output added `7.1/3` at PDF/UA-1 and
`8.2.2/1` at PDF/UA-2 on both tools.

The file also fails `6.2/1` (no `/MarkInfo`) at both PDF/UA parts, the missing
structure tree (`7.1/11`, `8.2.1/1`), and at PDF/UA-2 the identification
`5/2` and `5/5` its PDF/UA-1 claim draws. The PDF/A cells report the missing
PDF/A identification and the undescribed `pdfuaid` schema (`6.7.9` at part 1,
`6.6.2.3.1` at parts 2 and 3), at level A the tagging, at part 4 the
`%PDF-1.7` header, and `6.9/5` at PDF/A-4f. veraPDF 1.30.2 and litho report
the same rule set in all thirteen cells, on the source and on the
`flatten-form` output.

### `smaskindata_2b.pdf`

ISO 32000-1, 8.9.5, Table 89 lets a JPXDecode image say, by `/SMaskInData`,
that the opacity channel of its JPEG 2000 data is its soft mask: 1 for straight
colour, 2 for colour preblended with the background, which JPEG 2000
premultiplies against zero (ITU-T T.800, Table I.16). The image decoder and the
renderer apply it, and an explicit `/SMask` takes its place. The only such
images in the corpora are three pdfjs files, all value 1 and none in the render
sample, so the `render` cell drew none. The page draws two 32 by 32 RGBA images
over yellow and blue bands, each a gradient whose opacity falls from 255 at its
centre to 0 at a radius of 14 pixels:

| Image        | Data                                                   | Entries                                       |
| ------------ | ------------------------------------------------------ | --------------------------------------------- |
| `ImStraight` | straight colour, an opacity channel (`cdef` type 1)    | `/SMaskInData 1`, colour from its sRGB `colr` |
| `ImPremul`   | colour premultiplied, a premultiplied opacity (type 2) | `/SMaskInData 2`, `/ColorSpace /DeviceRGB`    |

litho draws both as the same disc with the bands showing through its edge. Its
decoding of each image matches OpenJPEG 2.5's (`opj_decompress`), opacity and
colour, premultiplication undone for `ImPremul`. The two other renderers read
the file apart from litho and from each other (see
[the pages the render cell leaves mismatched](#known-divergences)).

The file declares no OutputIntent and its page no `/Group`, so DeviceRGB fails
`6.2.3.3/1` at PDF/A-1 and `6.2.4.3/2` at the later parts, and the page holding
transparency `6.2.10/2` at PDF/A-2 and PDF/A-3. ISO 19005-1, 6.4 names `/SMask`,
not `/SMaskInData`, and veraPDF 1.30.2 reports no 6.4 rule at PDF/A-1b. veraPDF
1.30.2 and litho report the same rule set in all thirteen cells.

### `flattentiles_1b.pdf`

A PDF/A-1 remediation encodes a composite of images and shadings only as JPEG
at quality 90, and cuts a composite mixing a photograph with vector art into
128-pixel tiles: those only images and shadings reach are JPEG, the others stay
lossless. The corpus files that reach the tiles are govdocs reports of tens of
megabytes. This page holds one such region:

| Draws                                                                    |
| ------------------------------------------------------------------------ |
| a 160 x 120 photograph, smooth colour with grain, over 192 x 144 pt      |
| a stroked chart beside it: a frame, a polyline and filled triangle marks |
| a red band at ca 0.5 across both, which joins them into one region       |

The band's composite at 300 dpi is cut into a JPEG tile over the photograph, a
lossless tile over the chart, whose frame, line and marks reach it, and a
lossless strip along the band's bottom, too short to hold 256 colours. The
PDF/A-1b remediation, default and aggressive, writes 105,289 bytes, where one
lossless composite took 111,451, and `litho flatten-transparency
--jpeg-quality 90` 73,044 bytes, where the lossless default takes 79,206.
litho and poppler 26.09.0 draw each output like its source; drawn at 72, 150
and 300 dpi by litho, poppler and PDFium, the tiled output shows no seam: no
row or column where it departs from the lossless one, and no paper between the
tiles. veraPDF 1.30.2 and litho report nothing on the PDF/A-1b remediation.

The file declares no OutputIntent, so DeviceRGB fails `6.2.3.3/1` and
DeviceGray `6.2.3.3/3` at PDF/A-1b and `6.2.4.3/2` and `6.2.4.3/4` at the later
parts; the constant alpha fails `6.4/6` at PDF/A-1b and `6.2.10/2` at PDF/A-2b,
where the page states no `/Group`. veraPDF 1.30.2 and litho report the same rule
set in all thirteen cells.

### `iccgroupalt_2b.pdf`

An ICCBased space whose profile a reader cannot use reads as its stream's
`/Alternate` (ISO 32000-1, 8.6.5.5, Table 66), and that holds where the space
is a transparency group's blending colour space (11.3.4, Table 147) and where
flattening composites transparent text over white paper in the text's own
space (11.3.7.2). The corpora's unusable profiles in a group's `/CS` or under
transparent text carry no `/Alternate`, or name the device space of `/N`, so
no cell reached either. Each profile is a version 2.1 monitor header with an
empty tag table, as `iccalternate_2b.pdf`'s are, and every glyph a Type 3 glyph
painting in the text's colour:

| Page | Draws                                                                        |
| ---- | ---------------------------------------------------------------------------- |
| 1    | an isolated group whose `/CS` has a CMYK header and `/Alternate` DeviceCMYK, |
|      | blue at ca 0.6 over yellow, and an isolated group whose `/CS` has a GRAY     |
|      | header and a Separation `/Alternate`, red then green Multiply                |
| 2    | text at ca 0.5 in the GRAY-header space, tint 0.6                            |

litho sorted a group's `/CS` by its `/N` alone: the second group blended in
grey and read its composite as Separation tints, 6,400 pixels past the render
cell's tolerance from poppler 26.09.0 at 72 dpi. A Separation is no blending
space Table 147 admits, so the group now blends in sRGB and draws the black
and green poppler draws, 0 pixels apart. The first group blends in CMYK either
way, but converts sRGB colours into it through DeviceCMYK's profile, where it
used the device formula for want of the header's. Flattening composited the
text over paper of tint 1, the grey white of `/N` 1, and drew it as tint 0.8,
1,294 pixels past the tolerance from its source; it now composites over tint
0, no ink (8.6.6.4), and draws tint 0.3, as the source shows. `convert-colors`
reads both spaces as before.

The file declares no OutputIntent, so DeviceRGB fails `6.2.4.3/2` and DeviceCMYK
`6.2.4.3/3` at PDF/A-2b, `6.2.3.3/1` and `6.2.3.3/2` at PDF/A-1b; the
transparency fails `6.2.10/2` at PDF/A-2b and four `6.4` tests at PDF/A-1b.
veraPDF 1.30.2 and litho report the same rule set in all thirteen cells.

### `flatuntagged_ua1.pdf`

ISO 14289-1, 7.1 and ISO 14289-2, 8.2.2 require every piece of page content
of a document claiming conformance to be tagged as real content or marked as
an artifact. An annotation is not page content, so a document with no
structure tree passes both tests while its page draws nothing of its own.
`FlattenTransparency` draws each annotation that paints with transparency into
the page, where no structure element can take it. In a document whose XMP
claims PDF/UA (`pdfuaid:part`) or WTPDF it marks the drawing as an artifact,
as `FlattenForm` marks a baked widget (see `formuntagged_ua1.pdf`); a document
claiming neither keeps it as plain content, which default text extraction
returns. The corpora's untagged files that claim PDF/UA hold no annotation
painting with transparency, so the `flatten` cell drew none:

| Annotation | `/Rect`          | Appearance                                            |
| ---------- | ---------------- | ----------------------------------------------------- |
| Square     | `[10 10 60 60]`  | a blue square, `/CA` 0.5 and an ExtGState at `ca` 0.5 |
| FreeText   | `[80 20 190 80]` | a background at `ca` 0.6 under opaque text `DEA`      |

The page's own content draws nothing. Every glyph is a Type 3 glyph with a
`/ToUnicode` map. The file has no `/StructTreeRoot` and no `/MarkInfo`, claims
PDF/UA-1 in its XMP, and declares an RGB OutputIntent.

`litho flatten-transparency` draws both annotations into the page, each inside
an `/Artifact` sequence, and composites the two regions. The output fails a
subset of what the source fails at all thirteen levels, on litho and veraPDF
1.30.2 alike: with the transparency gone it passes `6.4/6` and `6.5.3/1` at
PDF/A-1a and PDF/A-1b, and adds nothing. Drawn unmarked, as before, the output
added `7.1/3` at PDF/UA-1 and `8.2.2/1` at PDF/UA-2 on both tools. Default text
extraction leaves the artifact text `DEA` out, and with artifacts returns it.
litho and poppler 26.09.0 draw the output within 0.5% of the source's pixels at
72 dpi, the composites' edges.

The file also fails `6.2/1` (no `/MarkInfo`) at both PDF/UA parts, the missing
structure tree (`7.1/11`, `8.2.1/1`), the annotations outside an `Annot`
element (`7.18.1/1`) and the page with no `/Tabs` (`7.18.3/1`, `8.9.3.3/1`),
and at PDF/UA-2 the identification `5/2` and `5/5` its PDF/UA-1 claim draws.
The PDF/A cells report the missing PDF/A identification (`6.7.11/1` at part 1,
`6.6.4/1` at parts 2 and 3, `6.7.3/1` at part 4) and the undescribed `pdfuaid`
schema (`6.7.9` at part 1, `6.6.2.3.1` at parts 2 and 3), at level A the
tagging, at part 4 the `%PDF-1.7` header, `6.9/5` at PDF/A-4f, and at PDF/A-1
the appearances' constant alpha (`6.4/6`) and the Square's `/CA` 0.5
(`6.5.3/1`). veraPDF 1.30.2 and litho report the
same rule set in all thirteen cells, on the source and on the
`flatten-transparency` output.

## Scope

`manifest.json` expands every fixture across the thirteen profiles it lists,
every PDF/A and PDF/UA conformance level veraPDF validates, so the runner scores
225 x 13 = 2925 cells, the whole hand-measured matrix. litho and veraPDF return
the same verdict in all of them: in each cell either both report nothing or both
report something.

Verdict agreement is the weaker property. Rule-set agreement is what these
fixtures exist to protect, and it holds in 2901 of the 2925 cells. The 24
exceptions, two on `complex_ua2` and eleven each on `sharedicc_2b` and
`imagereach_2b`, are marked `*` in the counts table and covered under
[Known divergences](#known-divergences); veraPDF reports a non-empty set in
each, which is why the verdict still matches.

The runner's `compliance` feature scores both. `matched` counts the cells whose
verdicts agree, and `ruleset_mismatch`, a cost metric the baseline gates, counts
the cells whose sets of failed rules (clause and test) differ. Every cell but
seventeen is non-compliant on both sides (`drivetitle_1b` at PDF/A-1b; `baretree_2b`, `boxorigin_2b`,
`pagelabels_2b`, `qshyphen_2b`, `recolorimg_2b`, `siglock_2b`, `sizetie_2b`, `type3cycle_2u` and `viewerboxes_2b` at PDF/A-2b; `tounicodepair_2u` at PDF/A-2b and PDF/A-2u; `formhidden_ua1`
and `sigtagged_ua1` at PDF/UA-1; and `outlinedest_ua2`, `remapchain_ua2` and
`structcopy_ua2` at PDF/UA-2 are the corpus's conforming cells), so a change that drops or invents a
rule class leaves `matched` at 2834 and moves only `ruleset_mismatch`, and on
those seventeen cells it moves both. Every exception sits in the manifest's
profiles, `complex_ua2` at PDF/UA-1 and PDF/UA-2 and the two shared-stream
fixtures at the eleven PDF/A levels, so the metric reads 24 on this corpus, and
the text scoreboard names the rules only one side reports. A 25th cell in the
runner is a regression, or a divergence to cover under Known divergences and
mark `*`.

The `remediation` and `remediation-aggressive` features validate each
remediated output with both tools, and their `disagreement` metric sums, over
the cells, the difference between the numbers of rules each reports. It reads
11 on this corpus in both modes, all on `sharedicc_2b` (see
[Known divergences](#known-divergences)), so any other cell that moves it is a
regression or a divergence to cover there.

The `text` feature scores each fixture once, not once per profile: the text a
page holds does not depend on the profile, so the runner keeps each PDF's
first fixture under its basename. The goldens are PDF.js's reading, generated
at provisioning with the PDF.js commit `versions.env` pins, as for the other
text corpora. These fixtures exercise font and CMap constructs PDF.js reads
wrong, so `text-overrides.json` corrects the goldens page by page, and the
goldens stay PDF.js's raw output. Each entry records the PDF.js reading it
replaces, so it fails its fixture when a new PDF.js reads the page otherwise,
and states the clause behind its verdict. It either gives the right reading,
where ISO 32000-1, 9.10.2 or the page geometry decides it, or marks the page
unscored, where the readers disagree only on codes 9.10.2 gives no Unicode
value, since no reading of those is right. Where PDF.js and litho agree, the
golden stands. Of the 214 fixtures, 206 match and 8 are unscored
(`cmapcidtext_2b`, `cmapnotdef_2b`, `cmaps_2b`, `cmapusecmap_2b`,
`cmapusestream_2b`, `cmapverabase_2b`, `type0nodesc_2b`, `usecmap_2u`; page 16
of `fontcopies_2b` too, whose other pages score), so the cell's `matched`,
`words_match` and `chars_match` all read 206 and a mismatch is a regression.
A new fixture needs
its golden regenerated at provisioning, and an override when PDF.js reads it
wrong.

PDF/A-4e and PDF/A-4f are the manifest's ninth and tenth profiles, after the eight it
listed first. Appended rather than sorted in, they leave the determinism
feature dealing each fixture's operations over the same first seven profiles,
and the mutation anchor on PDF/A-1b. The two bookmark generations the feature
gained later come after those seven operations, so they take the eighth and
ninth profiles, PDF/UA-2 and PDF/A-4e, which skipped before them. The fixture sections that say "the first
eight manifest profiles" were measured before the two joined. At both, live
veraPDF 1.30.2 reports the rule set the counts table gives in all 320 cells, and
litho the same set in each. Remediated at both, every output carries the same
rule set on both tools, no rule appears that the input did not fail, and the
fully fixed cells, measured before `symbolname_2u` joined, are:

| Profile  | Default | Aggressive | Rules left most often                          |
| -------- | ------- | ---------- | ---------------------------------------------- |
| PDF/A-4e | 63      | 67         | `6.2.10.4.1/1`, the unembedded fonts (76)      |
| PDF/A-4f | 1       | 1          | `6.9/5`, no `/EmbeddedFiles` (152), then fonts |

ISO 19005-4, 6.9 requires a PDF/A-4f file to name its embedded files in an
`/EmbeddedFiles` entry of the catalog's name dictionary, and remediation cannot
write a file the document does not have, so `6.9/5` stays on the 152 fixtures
without one. Under PDF/A-4e remediation keeps the `/3D` and `/RichMedia`
annotations of `annotkinds_ua2`, `threed_4e`, `threedcs_4` and `afref_3b` (ISO
19005-4, 6.3.1 and Annex B). Under PDF/A-4f it keeps every `FileAttachment`
annotation but the hidden one of `laterreads_2u`, which it removes at every
profile as it removes every annotation no viewer shows, and in aggressive mode
it keeps the embedded files of other formats that it strips at PDF/A-4.

PDF/A-1a, 3a and 3u follow them, appended for the same reason. At the three,
live veraPDF 1.30.2 reports the rule set the counts table gives in all 486
cells, and litho the same set in each. The instance counts differ only in the
families described under
[Instance counts](#instance-counts-which-the-corpus-does-not-pin). Remediated
at the three, every output carries the same rule set on both tools, no rule
appears that the input did not fail, and the fully fixed cells, measured
before `fontunread_2b` and `type1notdef_2u` joined, are:

| Profile  | Default | Aggressive | Rules left most often                     |
| -------- | ------- | ---------- | ----------------------------------------- |
| PDF/A-1a | 50      | 54         | `6.3.4/1`, the unembedded fonts (76)      |
| PDF/A-3a | 53      | 58         | `6.2.11.4.1/1`, the unembedded fonts (76) |
| PDF/A-3u | 57      | 62         | `6.2.11.4.1/1`, the unembedded fonts (76) |

Remediation removes at the three what it removes at PDF/A-1b and PDF/A-3b, and
at level A also the invalid `/Lang` entries and a role mapping that remaps a
standard type. At PDF/A-3a and 3u the aggressive mode keeps the embedded files
of other formats that it strips at PDF/A-2a and 2u, since ISO 19005-3, 6.8
permits them. At PDF/A-1a `6.8.3.4/1` stays on `structnest_ua1` and
`tableregular_ua2`, on both tools, beside the seven fixtures it stays on at
PDF/A-2a: PDF Reference 1.4, 9.7.4 has no row group types, and remediation maps
no type it has no standard equivalent for, so `THead`, `TBody` and `TFoot`
stay unmapped.

WTPDF-1.0 Accessibility and WTPDF-1.0 Reuse stay out of the manifest. Their
rules are the PDF/UA-2 ones with WTPDF's identification in place of clause 5,
and Reuse drops some more: 75 of the 160 Accessibility cells report PDF/UA-2's
set exactly, and the other 85 differ from it only in `6.1.3/1` and the clause-5
rules, so the fixtures exercise little that the PDF/UA-2 cells and the veraPDF
corpus, which runs both levels, do not. Measured on veraPDF 1.30.2, the
two levels agree with litho in 318 of their 320 cells; the exceptions are
`complex_ua2` at both, the `/P`-less element of `8.2.1/2` described under Known
divergences, which would add two cells to `ruleset_mismatch`.

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
