package testhygiene

type helperDecision uint8

const (
	helperRejected helperDecision = iota
	helperAccepted
)

func observeHelperDecision(ok bool) helperDecision {
	if ok {
		return helperAccepted
	}
	return helperRejected
}

func observeHelperDecisions(values ...bool) []helperDecision {
	decisions := make([]helperDecision, 0, len(values))
	for _, value := range values {
		decisions = append(decisions, observeHelperDecision(value))
	}
	return decisions
}
