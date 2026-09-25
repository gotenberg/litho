package content

import "testing"

// TestIsOperatorIsAnnexA pins the operator set to ISO 32000-1, Annex A, Table
// A.1: its 73 operators, and no other keyword, such as a run of two operators,
// the prefix of an operator or an operand keyword.
func TestIsOperatorIsAnnexA(t *testing.T) {
	t.Parallel()

	if n := len(operatorSet); n != 73 {
		t.Errorf("the operator set holds %d operators, Table A.1 lists 73", n)
	}
	for _, op := range []string{"b*", "BDC", "d0", "EMC", "f*", "ri", "SCN", "T*", "'", "\""} {
		if !IsOperator(op) {
			t.Errorf("IsOperator(%q) = false", op)
		}
	}
	for _, op := range []string{"ETBT", "BM", "BD", "fa", "nul", "true", "false", "null", "T", ""} {
		if IsOperator(op) {
			t.Errorf("IsOperator(%q) = true", op)
		}
	}
}
