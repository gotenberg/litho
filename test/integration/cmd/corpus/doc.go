// Command corpus scores litho across a matrix of (corpus, feature)
// cells. Each corpus supplies PDF fixtures; each feature pairs a litho
// entry point with a golden reference.
//
// Layout:
//
//	test/integration/corpus/<name>/manifest.json    what the corpus holds and how each step treats it
//	test/integration/corpus/Dockerfile              the image that derives the goldens
//	test/integration/testdata/<name>/               everything the steps write (gitignored)
//
// A corpus goes through four steps before it is scored, each skipping what is
// done, and prepare runs every one it needs:
//
//	corpus fetch       download the fixture fonts and the fetched corpora's files
//	corpus build       generate the inputs kept as sources in git
//	corpus references  convert the sources with the pinned Gotenberg image
//	corpus goldens     derive the goldens in the provisioning image
//	corpus prepare     all of the above, as needed
//	corpus run|check|baseline   score
//
// Every subcommand takes --corpus, comma-separated names, globs or sources
// (litho/docx,*/xlsx,poi). See printUsage for the rest.
//
// Every provisioning tool and corpus source comes at the version
// test/integration/corpus/versions.env pins; run, baseline and check refuse
// goldens recorded with other versions.
package main
