# openxmlsdk corpus fixtures

The Open XML SDK's test documents scored by the `diff` feature (see `FEATURES.md`) against LibreOffice as Gotenberg's LibreOffice route prints them, as the libreoffice corpora are. The SDK's test data storage holds documents written by Microsoft Office: Office 2007's own templates, documents saved in the ISO 29500 Strict conformance class by Office 2010, and test documents of most WordprocessingML, SpreadsheetML and PresentationML features. Only the lists and the hashes are committed: `make corpus-fetch CORPUS_NAME=openxmlsdk` downloads the documents at the commit `OPENXMLSDK_COMMIT` pins, and `make office-references SOURCES=openxmlsdk` builds their references in the testdata.

| Corpus            | Documents | Extensions       |
| ----------------- | --------- | ---------------- |
| `openxmlsdk/docx` | 250       | `.docx`, `.dotx` |
| `openxmlsdk/xlsx` | 63        | `.xlsx`, `.xltx` |
| `openxmlsdk/pptx` | 182       | `.pptx`, `.potx` |

| Path                      | What it holds                                                                                                                    |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `<format>/documents.json` | The documents, each `{"id", "path"}`: the ID its path under the test data storage made plain ASCII, the path its path in the SDK |
| `<format>/files.sha256`   | `<sha256>  <path>` for each document, then the SDK's `LICENSE`                                                                   |
| `<format>/manifest.json`  | The corpus manifest; `references.source_pin` names `OPENXMLSDK_COMMIT`                                                           |
| `<format>/download.sh`    | Installs the references from the testdata volume as the corpus PDFs the diff goldens come from                                   |
| `corpus select`           | `make corpus-select CORPUS_NAME=openxmlsdk/<format> CHECKOUT=<dir>`: rewrites the two lists above from a checkout at the pin     |
| `corpus fetch`            | `make corpus-fetch CORPUS_NAME=openxmlsdk`: downloads the listed files from GitHub at the pin, hash-checked                      |

## How the documents were chosen

`corpus select` walks `test/DocumentFormat.OpenXml.Tests.Assets/assets/TestDataStorage` and keeps a document of the format's extensions when it is at most 150 KiB, is not under `Robustness/` (malformed on purpose), qualifies as `corpus select` (`cmd/corpus/select_qualify.go`) decides (see `poi/FIXTURES.md`), and its bytes are not those of a document already kept: the storage holds several sets twice. A file name holds spaces and other characters a shell or a form field mangles, so its ID writes each run of them `_`. The qualifying documents are sorted by ID and the first 250 of each format kept.

The Open XML SDK is under the MIT licence; its test documents are downloaded for testing, never committed.
