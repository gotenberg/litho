# poi corpus fixtures

Apache POI's test documents scored by the `diff` feature (see `FEATURES.md`) against Microsoft Office as Office for the web prints them through Microsoft Graph, as the libreoffice OOXML corpora are (see `corpus/libreoffice/docx/FIXTURES.md`). Office refuses some, which go unscored: in docx 4 it cannot open and 7 signed documents (`ErrorCodeLimitExceeded`); in xlsx 12 with nothing to print, 2 corrupt and 1 over its page limit; in pptx 2 corrupt and 3 signed; in doc 1 failing its security check; in xls 17 with nothing to print, 1 corrupt and 1 over the page limit; in ppt 2 it cannot read. POI's `test-data/` holds the documents its bug reports came with, most written by Microsoft Office, in the OOXML formats and in the Word, Excel and PowerPoint 97 to 2003 binary formats. Only the lists and the hashes are committed: `make corpus-fetch CORPUS=poi` downloads the documents at the commit `POI_COMMIT` pins, and `make office-references SOURCES=poi` builds their references in the testdata, both as the libreoffice corpora's `FIXTURES.md` documents.

| Corpus     | Documents | Extensions             |
| ---------- | --------- | ---------------------- |
| `poi/docx` | 129       | `.docx`, `.dotx`       |
| `poi/xlsx` | 200       | `.xlsx`, `.xltx`       |
| `poi/pptx` | 57        | `.pptx`, `.potx`       |
| `poi/doc`  | 85        | `.doc`, Word 97+       |
| `poi/xls`  | 200       | `.xls`, BIFF8          |
| `poi/ppt`  | 73        | `.ppt`, PowerPoint 97+ |

litho reads none of the binary formats yet: the `doc`, `xls` and `ppt` cells score a convert error on every document until it does. Their references are the target its converters work towards.

| Path                      | What it holds                                                                                                    |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `<format>/documents.json` | The documents, each `{"id", "path"}`: the ID its path under `test-data/`, the path its path in POI               |
| `<format>/files.sha256`   | `<sha256>  <path>` for each document, then POI's `legal/LICENSE` and `legal/NOTICE`                              |
| `<format>/manifest.json`  | The corpus manifest; `references.source_pin` names `POI_COMMIT`                                                  |
| `corpus update`           | `make corpus-update CORPUS=poi/<format> CHECKOUT=<dir>`: rewrites the two lists above from a checkout at the pin |
| `corpus fetch`            | `make corpus-fetch CORPUS=poi`: downloads the listed files from GitHub at the pin, hash-checked                  |

## How the documents were chosen

`corpus update` walks `test-data/` and keeps a document of the format's extensions when:

- it is at most 150 KiB, as the libreoffice corpora's documents are;
- POI's own `test-data/poi-integration-exceptions.csv` does not list it: those are the encrypted, malformed and fuzzed files POI expects to fail on;
- it qualifies as `corpus update` (`cmd/corpus/select_qualify.go`) decides: a well-formed package or compound file of its format with no macros, ActiveX controls or embedded objects (charts aside), no EMF, WMF, SVM or TIFF picture and no audio or video; a binary document must be Word 97 or later, Excel 97 or later (BIFF8) or PowerPoint 97 or later, and unencrypted;
- its bytes are not those of a document already kept.

The qualifying documents are sorted by ID and the first 200 of each format kept.

POI is under the Apache License 2.0; its test documents come from its bug reports and are downloaded for testing, never committed.
