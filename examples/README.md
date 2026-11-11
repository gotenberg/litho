# Examples

Runnable programs, one per task. Each takes its files as arguments:

```bash
go run ./examples/extract document.pdf
```

| Example                             | Shows                                                                        |
| ----------------------------------- | ---------------------------------------------------------------------------- |
| [extract](extract/)                 | Text and image metadata of every page                                        |
| [validate](validate/)               | PDF/A-2b validation, each violation with its clause and severity             |
| [remediate](remediate/)             | PDF/A-2b remediation, fonts embedded from a directory, a pinned `/ID`        |
| [compose](compose/)                 | A pipeline: merge, rotate a page, stamp a watermark                          |
| [sign](sign/)                       | A PAdES signature from PEM certificate and key files                         |
| [render](render/)                   | The first page drawn to a PNG image                                          |
| [convert](convert/)                 | A plain text or HTML file laid out as a tagged PDF in the system's fonts     |
| [convert-archive](convert-archive/) | A document converted, then remediated to PDF/A-2b and PDF/UA-1               |
| [convert-merge](convert-merge/)     | Several documents converted at once, merged in order, remediated to PDF/A-2b |

The [godoc](https://pkg.go.dev/github.com/gotenberg/litho/pkg/pdf) carries more as testable examples, such as `ExampleWithFontFiles` and `ExampleNewSigner`.
