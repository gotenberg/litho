package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// withoutUnbuiltMicrosoftReferences splits the fixtures of a corpus whose
// references Microsoft builds into those whose reference was ever built or
// refused, kept, and those it never was, with the fixtures that borrow
// theirs, unbuilt. A fixture that sets litho's own options is kept: no
// engine is asked for its reference, and its record needs no sign-in (see
// [referencePlan]). Only a Microsoft Graph token builds such a reference, so
// a fixture added without one is left unscored, named, rather than failing
// the whole corpus; a reference built from another source or pin still
// fails it (see [referenceProblems]). Every fixture is kept for another
// engine, whose references prepare builds without a credential, and when
// the reference manifest is unreadable, which [referenceProblems] reports.
func withoutUnbuiltMicrosoftReferences(m *Manifest, root string, fixtures []Fixture) (kept []Fixture, unbuilt []string) {
	if m.References == nil || m.References.Engine != engineMicrosoft {
		return fixtures, nil
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m.References.Dir), referenceManifestName)) //nolint:gosec // provisioned record under the corpus testdata root
	if err != nil {
		return fixtures, nil
	}
	var man referenceManifest
	if err := json.Unmarshal(data, &man); err != nil {
		return fixtures, nil
	}
	plan, err := planReferences(fixtures)
	if err != nil {
		return fixtures, nil
	}
	hostile := unreferencedFixtures(m)
	missing := map[string]bool{}
	for _, fx := range fixtures {
		if plan.ownsNoReference(fx.ID) || hostile[fx.ID] {
			continue
		}
		name := reportName(fx.ID)
		if !hasKey(man.References, name) && !hasKey(man.Failed, name) {
			missing[fx.ID] = true
		}
	}
	for _, fx := range fixtures {
		if missing[fx.ID] || missing[plan.borrowed[fx.ID]] {
			unbuilt = append(unbuilt, fx.ID)
			continue
		}
		kept = append(kept, fx)
	}
	return kept, unbuilt
}

// unbuiltMicrosoftNotice is what a run says of the fixtures whose Microsoft
// reference was never built, with the command that builds them.
func unbuiltMicrosoftNotice(m *Manifest, unbuilt []string) string {
	return fmt.Sprintf("%s: %d fixtures have no Microsoft reference yet, not scored: %s (build them with %s=<token> go run ./test/integration/cmd/corpus references --corpus=%s)",
		m.Name, len(unbuilt), summarize(unbuilt), msGraphTokenEnv, m.Name)
}

// microsoftSignInAvailable reports whether the environment names a
// Microsoft Graph token or app registration a reference build can sign in
// with.
func microsoftSignInAvailable() bool {
	return os.Getenv(msGraphTokenEnv) != "" || os.Getenv(msGraphClientIDEnv) != ""
}
