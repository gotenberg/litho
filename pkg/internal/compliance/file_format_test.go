package compliance

import "testing"

// TestStreamFormatRule_MatchesStreamLengthClause verifies that a stream /Length
// format violation is attributed to the same clause as the direct stream-length
// check (§6.1.7.1 for parts 2/3, CosStream), not to §6.1.8, which is the
// CosUnicodeName UTF-8 rule. Attributing it to §6.1.8 fired a spurious UTF-8-name
// violation alongside the correct §6.1.7.1 one.
func TestStreamFormatRule_MatchesStreamLengthClause(t *testing.T) {
	t.Parallel()
	for _, profile := range []string{"PDF/A-2b", "PDF/A-2u", "PDF/A-2a", "PDF/A-3b"} {
		got := streamFormatRule(profile)
		if got.Clause == "6.1.8" {
			t.Errorf("%s: streamFormatRule attributed a /Length mismatch to §6.1.8 (CosUnicodeName)", profile)
		}
		if got != streamLengthRule(profile) {
			t.Errorf("%s: streamFormatRule (%s) must match streamLengthRule (%s)",
				profile, got.Clause, streamLengthRule(profile).Clause)
		}
	}
}
