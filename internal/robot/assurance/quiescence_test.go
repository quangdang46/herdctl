package assurance

import "testing"

func TestEvaluateQuiescenceQueueDry(t *testing.T) {
	got := EvaluateQuiescence(QuiescenceInput{})

	if got.State != QuiescenceQueueDry {
		t.Fatalf("State = %q, want %q", got.State, QuiescenceQueueDry)
	}
	if !got.SafeToStandDown {
		t.Fatalf("SafeToStandDown = false, want true")
	}
	assertReasonCodes(t, got.ReasonCodes, []ReasonCode{ReasonQuiescenceQueueDry})
	if got.Signal.Type != SignalQuiescenceCandidate {
		t.Fatalf("Signal.Type = %q, want %q", got.Signal.Type, SignalQuiescenceCandidate)
	}
	if got.Signal.Status != SignalStatusHealthy {
		t.Fatalf("Signal.Status = %q, want %q", got.Signal.Status, SignalStatusHealthy)
	}
}

func TestEvaluateQuiescenceBlockedByPeer(t *testing.T) {
	got := EvaluateQuiescence(QuiescenceInput{
		InProgressCount:        1,
		ActiveReservationCount: 2,
	})

	if got.State != QuiescenceBlockedByPeer {
		t.Fatalf("State = %q, want %q", got.State, QuiescenceBlockedByPeer)
	}
	if got.SafeToStandDown {
		t.Fatalf("SafeToStandDown = true, want false")
	}
	assertReasonCodes(t, got.ReasonCodes, []ReasonCode{
		ReasonQuiescenceInProgressWork,
		ReasonReservationPathConflict,
	})
}

func TestEvaluateQuiescenceSaturatedReviewLoop(t *testing.T) {
	got := EvaluateQuiescence(QuiescenceInput{
		ReviewRounds:         3,
		RecentReviewFindings: 0,
	})

	if got.State != QuiescenceSaturatedReviewLoop {
		t.Fatalf("State = %q, want %q", got.State, QuiescenceSaturatedReviewLoop)
	}
	if !got.SafeToStandDown {
		t.Fatalf("SafeToStandDown = false, want true")
	}
	assertReasonCodes(t, got.ReasonCodes, []ReasonCode{
		ReasonQuiescenceQueueDry,
		ReasonQuiescenceReviewSaturated,
	})
}

func TestEvaluateQuiescenceUnsafeReadyWork(t *testing.T) {
	got := EvaluateQuiescence(QuiescenceInput{
		ReadyCount:      1,
		ActionableCount: 1,
	})

	if got.State != QuiescenceUnsafeToStandDown {
		t.Fatalf("State = %q, want %q", got.State, QuiescenceUnsafeToStandDown)
	}
	if got.SafeToStandDown {
		t.Fatalf("SafeToStandDown = true, want false")
	}
	assertReasonCodes(t, got.ReasonCodes, []ReasonCode{ReasonQuiescenceReadyWork})
}

func TestEvaluateQuiescenceUnsafeUrgentMail(t *testing.T) {
	got := EvaluateQuiescence(QuiescenceInput{
		UrgentMailCount: 1,
	})

	if got.State != QuiescenceUnsafeToStandDown {
		t.Fatalf("State = %q, want %q", got.State, QuiescenceUnsafeToStandDown)
	}
	assertReasonCodes(t, got.ReasonCodes, []ReasonCode{ReasonQuiescenceUrgentMail})
}

func TestEvaluateQuiescenceUnsafeDirtyTracker(t *testing.T) {
	got := EvaluateQuiescence(QuiescenceInput{
		TrackerNeedsFlush: true,
	})

	if got.State != QuiescenceUnsafeToStandDown {
		t.Fatalf("State = %q, want %q", got.State, QuiescenceUnsafeToStandDown)
	}
	assertReasonCodes(t, got.ReasonCodes, []ReasonCode{ReasonQuiescenceTrackerDirty})
}

func TestEvaluateQuiescenceUnsafeDirtyWorktree(t *testing.T) {
	got := EvaluateQuiescence(QuiescenceInput{
		DirtyWorktree: true,
	})

	if got.State != QuiescenceUnsafeToStandDown {
		t.Fatalf("State = %q, want %q", got.State, QuiescenceUnsafeToStandDown)
	}
	assertReasonCodes(t, got.ReasonCodes, []ReasonCode{ReasonQuiescenceDirtyWorktree})
}

func TestEvaluateQuiescenceReviewFindingsRemainUnsafe(t *testing.T) {
	got := EvaluateQuiescence(QuiescenceInput{
		ReviewRounds:         2,
		RecentReviewFindings: 1,
	})

	if got.State != QuiescenceUnsafeToStandDown {
		t.Fatalf("State = %q, want %q", got.State, QuiescenceUnsafeToStandDown)
	}
	assertReasonCodes(t, got.ReasonCodes, []ReasonCode{ReasonQuiescencePendingWork})
}

func assertReasonCodes(t *testing.T, got, want []ReasonCode) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("reason codes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reason codes = %v, want %v", got, want)
		}
		if !KnownReasonCode(got[i]) {
			t.Fatalf("reason code %q is not registered", got[i])
		}
	}
}
