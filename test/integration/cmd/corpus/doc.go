// Command corpus compares litho's compliance verdicts against VeraPDF and
// PDF.js reference results.
//
// Usage:
//
//	go run ./test/integration/cmd/corpus verapdf [--init] [--format text|json] [--profile PDF_A-4] [--max-fn 3] [--max-fp 0]
//	go run ./test/integration/cmd/corpus pdfjs   [--init] [--format text|json] [--max-text-errors 50]
package main
