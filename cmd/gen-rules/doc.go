// Command gen-rules generates rules.go from VeraPDF validation profile XML files.
//
// Usage:
//
//	go run ./cmd/gen-rules                          # use local profiles/
//	go run ./cmd/gen-rules --download               # download latest profiles first
//	go run ./cmd/gen-rules --download --tag v1.28.2  # download specific release
//	go generate ./pkg/internal/compliance/...
package main
