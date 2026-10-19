package content

// UnclosedMarkedContent reports how ops leave their marked-content sequences.
// tail is the index of the first operator past the last point where every
// sequence ops open is closed, or len(ops) when ops close all of them, and open
// is how many sequences are still open at the end. ISO 32000-1, 14.6 delimits a
// sequence by its BMC or BDC and the matching EMC and requires the pair to lie
// in one content stream, so a sequence open at the end of a page's content or
// of a form is no sequence at all. An EMC with no sequence open closes nothing.
func UnclosedMarkedContent(ops []Operation) (tail, open int) {
	for i, op := range ops {
		switch op.Operator {
		case "BMC", "BDC":
			open++
		case "EMC":
			if open == 0 {
				continue
			}
			open--
			if open == 0 {
				tail = i + 1
			}
		}
	}
	if open == 0 {
		return len(ops), 0
	}
	return tail, open
}
