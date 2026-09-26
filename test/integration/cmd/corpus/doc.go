// Command corpus scores litho across a matrix of (corpus, feature)
// cells. Each corpus supplies PDF fixtures; each feature pairs a litho
// entry point with a golden reference.
//
// Layout:
//
//	test/integration/corpus/<name>/manifest.json    provisioning + feature map
//	test/integration/corpus/<name>/Dockerfile       image that emits PDFs + goldens
//	test/integration/testdata/<name>/               provisioned output (gitignored)
//
// Subcommands:
//
//	corpus init [--corpus=<names>]
//	corpus run  [--corpus=...] [--feature=...] [--profile=...] [--format=text|json] [--workers=N] [--allow-version-drift]
//	corpus versions [--build-args]
//	corpus tools
//
// Every provisioning tool and corpus source comes at the version
// test/integration/corpus/versions.env pins; run, baseline and check refuse
// goldens recorded with other versions.
package main
