package tablelobby

func Guidance(v View) (string, string) {
	if v.Stale {
		return "Evidence out of date", "Reconnect before requesting a financial action."
	}
	if v.Stage == StageClosed {
		return "Table closed", v.ClosedReason + " · Recovery is in dcrpulse after the locks mature."
	}
	if v.Stage == StageReady {
		return "Ready to play", "Payout requires both seats to sign."
	}
	if v.CanFund {
		return "Ready to request stake funding", "Approve in dcrpulse. Do not submit a pending payment again."
	}
	return v.Stage.Label(), "Table formation and payment approvals are managed by dcrpulse."
}
