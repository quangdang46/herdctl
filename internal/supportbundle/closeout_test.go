package supportbundle

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func closeoutClock() time.Time {
	return time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
}

func TestBuildCloseout_EmptyRunHasNoRisks(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{Now: closeoutClock()})

	if b.SchemaVersion != CloseoutSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", b.SchemaVersion, CloseoutSchemaVersion)
	}
	if len(b.ResidualRisks) != 0 {
		t.Errorf("ResidualRisks = %v, want none on empty run", b.ResidualRisks)
	}
	if b.Counts.Commits != 0 || b.Counts.Verifications != 0 {
		t.Errorf("Counts = %+v, want zeros", b.Counts)
	}
}

func TestBuildCloseout_QueueDryCleanCloseoutHasNoRisks(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{
		Now: closeoutClock(),
		Run: RunMeta{SwarmName: "ntm-night", AgentName: "Alice", StartedAt: closeoutClock().Add(-2 * time.Hour), EndedAt: closeoutClock()},
		Commits: []CommitEntry{
			{Hash: "abc123", Subject: "feat: ship the thing"},
		},
		Beads: BeadsDelta{
			Closed: []string{"bd-3v1gs.7", "bd-3v1gs.8", "bd-3v1gs.7"}, // dedupe
		},
		Verifications: []VerificationEntry{
			{Command: "go test -short ./...", Outcome: OutcomePassed, Duration: 10 * time.Second},
		},
		Mail:  MailSnapshot{UnackedUrgent: 0, PendingAck: 0},
		Queue: QueueState{Ready: 0, InProgress: 0, QueueDry: true},
	})

	if len(b.ResidualRisks) != 0 {
		t.Errorf("ResidualRisks = %v, want none for clean queue-dry closeout", b.ResidualRisks)
	}
	if !equalSlice(b.Beads.Closed, []string{"bd-3v1gs.7", "bd-3v1gs.8"}) {
		t.Errorf("Beads.Closed = %v, want deduped + sorted", b.Beads.Closed)
	}
	if b.Counts.VerificationsPassed != 1 {
		t.Errorf("VerificationsPassed = %d, want 1", b.Counts.VerificationsPassed)
	}
	if !b.Queue.QueueDry {
		t.Errorf("Queue.QueueDry = false, want true (clean closeout)")
	}
}

func TestBuildCloseout_PartialVerificationFlagsInconclusive(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{
		Now: closeoutClock(),
		Verifications: []VerificationEntry{
			{Command: "go test -short ./...", Outcome: OutcomePassed},
			{Command: "go test -race ./...", Outcome: OutcomeUnknown, Notes: "rch worker offline"},
			{Command: "go vet ./...", Outcome: OutcomeSkipped},
		},
	})

	codes := riskCodeSet(b.ResidualRisks)
	if !codes["verification_inconclusive"] {
		t.Errorf("missing verification_inconclusive risk: %+v", b.ResidualRisks)
	}
	if codes["verification_failed"] {
		t.Errorf("verification_failed should not fire when no Outcome=failed: %+v", b.ResidualRisks)
	}
	// Severity must be Medium for inconclusive (not High).
	for _, r := range b.ResidualRisks {
		if r.Code == "verification_inconclusive" && r.Severity != RiskSeverityMedium {
			t.Errorf("verification_inconclusive severity = %s, want medium", r.Severity)
		}
	}
}

func TestBuildCloseout_FailedVerificationIsHigh(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{
		Now: closeoutClock(),
		Verifications: []VerificationEntry{
			{Command: "go test ./internal/foo/...", Outcome: OutcomeFailed, Notes: "TestXyz failing"},
		},
	})
	if !riskCodeSet(b.ResidualRisks)["verification_failed"] {
		t.Fatalf("missing verification_failed risk: %+v", b.ResidualRisks)
	}
	for _, r := range b.ResidualRisks {
		if r.Code == "verification_failed" && r.Severity != RiskSeverityHigh {
			t.Errorf("verification_failed severity = %s, want high", r.Severity)
		}
	}
}

// bd-55myk: Counts.ResidualRisks must reflect len(bundle.ResidualRisks).
// Pre-fix this field was always 0 because computeCloseoutCounts ran
// before computeResidualRisks and never assigned the field anyway.
func TestBuildCloseout_CountsResidualRisksMatchesArrayLength(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{
		Now: closeoutClock(),
		Verifications: []VerificationEntry{
			{Command: "go test ./internal/foo/...", Outcome: OutcomeFailed},
		},
		Reservations: []ReservationSnapshot{
			{PathPattern: "internal/auth/**", AgentName: "Bob", Exclusive: true, AcquiredAt: closeoutClock().Add(-1 * time.Hour)},
		},
		Mail:              MailSnapshot{UnackedUrgent: 1},
		DegradedProviders: []string{"agentmail"},
	})
	if len(b.ResidualRisks) == 0 {
		t.Fatal("setup error: expected this fixture to produce residual risks")
	}
	if b.Counts.ResidualRisks != len(b.ResidualRisks) {
		t.Fatalf("Counts.ResidualRisks = %d, want %d (len of ResidualRisks array)",
			b.Counts.ResidualRisks, len(b.ResidualRisks))
	}
}

// Empty-input bundle has zero residual risks AND zero count — sanity
// check that the zero case is also pinned.
func TestBuildCloseout_EmptyRunHasZeroCountsResidualRisks(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{Now: closeoutClock()})
	if len(b.ResidualRisks) != 0 {
		t.Fatalf("empty closeout produced risks: %+v", b.ResidualRisks)
	}
	if b.Counts.ResidualRisks != 0 {
		t.Fatalf("Counts.ResidualRisks = %d, want 0", b.Counts.ResidualRisks)
	}
}

func TestBuildCloseout_ActiveReservationFiresMediumRisk(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{
		Now: closeoutClock(),
		Reservations: []ReservationSnapshot{
			{PathPattern: "internal/auth/**", AgentName: "Bob", Exclusive: true, AcquiredAt: closeoutClock().Add(-1 * time.Hour)},
		},
	})
	if !riskCodeSet(b.ResidualRisks)["active_reservations_outstanding"] {
		t.Fatalf("missing active_reservations_outstanding risk: %+v", b.ResidualRisks)
	}
	if b.Counts.ActiveReservations != 1 {
		t.Errorf("ActiveReservations = %d, want 1", b.Counts.ActiveReservations)
	}
}

func TestBuildCloseout_UnackedUrgentMailIsHigh(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{
		Now:  closeoutClock(),
		Mail: MailSnapshot{UnackedUrgent: 2, PendingAck: 0},
	})
	codes := riskCodeSet(b.ResidualRisks)
	if !codes["unacked_urgent_mail"] {
		t.Fatalf("missing unacked_urgent_mail risk: %+v", b.ResidualRisks)
	}
	if codes["pending_ack_mail"] {
		t.Errorf("pending_ack_mail must not fire when only urgent is non-zero: %+v", b.ResidualRisks)
	}
}

// bd-056vx: when both UnackedUrgent and PendingAck are non-zero, BOTH
// residual risks must surface — pending_ack_mail's description names
// itself as "non-urgent mail", so the two signals are orthogonal and
// the operator must see both at closeout. Pre-fix an else-if dropped
// the LOW pending_ack_mail risk whenever HIGH unacked_urgent_mail
// fired, hiding non-urgent ack-required mail.
func TestBuildCloseout_BothUrgentAndPendingAckMailFireWhenBothNonZero(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{
		Now:  closeoutClock(),
		Mail: MailSnapshot{UnackedUrgent: 2, PendingAck: 5},
	})
	codes := riskCodeSet(b.ResidualRisks)
	if !codes["unacked_urgent_mail"] {
		t.Errorf("missing unacked_urgent_mail risk: %+v", b.ResidualRisks)
	}
	if !codes["pending_ack_mail"] {
		t.Errorf("missing pending_ack_mail risk; both must fire when both counts are non-zero: %+v", b.ResidualRisks)
	}

	// Severities must remain distinct so the dashboard rolls up
	// correctly (HIGH urgent vs LOW pending-ack).
	for _, r := range b.ResidualRisks {
		switch r.Code {
		case "unacked_urgent_mail":
			if r.Severity != RiskSeverityHigh {
				t.Errorf("unacked_urgent_mail Severity = %s, want high", r.Severity)
			}
		case "pending_ack_mail":
			if r.Severity != RiskSeverityLow {
				t.Errorf("pending_ack_mail Severity = %s, want low", r.Severity)
			}
		}
	}
}

func TestBuildCloseout_PendingAckMailAloneIsLow(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{
		Now:  closeoutClock(),
		Mail: MailSnapshot{UnackedUrgent: 0, PendingAck: 3},
	})
	if !riskCodeSet(b.ResidualRisks)["pending_ack_mail"] {
		t.Fatalf("missing pending_ack_mail risk: %+v", b.ResidualRisks)
	}
	for _, r := range b.ResidualRisks {
		if r.Code == "pending_ack_mail" && r.Severity != RiskSeverityLow {
			t.Errorf("pending_ack_mail severity = %s, want low", r.Severity)
		}
	}
}

func TestBuildCloseout_DegradedProvidersAreFlagged(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{
		Now:               closeoutClock(),
		DegradedProviders: []string{"mail", "cass", "mail"}, // dedupe
	})
	if !riskCodeSet(b.ResidualRisks)["providers_degraded"] {
		t.Fatalf("missing providers_degraded risk: %+v", b.ResidualRisks)
	}
	if !equalSlice(b.DegradedProviders, []string{"cass", "mail"}) {
		t.Errorf("DegradedProviders = %v, want sorted+deduped [cass mail]", b.DegradedProviders)
	}
}

func TestBuildCloseout_RisksSortedHighFirstThenCode(t *testing.T) {
	t.Parallel()
	b := BuildCloseout(CloseoutInputs{
		Now: closeoutClock(),
		Verifications: []VerificationEntry{
			{Command: "x", Outcome: OutcomeFailed},
			{Command: "y", Outcome: OutcomeUnknown},
		},
		Reservations: []ReservationSnapshot{
			{PathPattern: "p", AgentName: "A"},
		},
		Mail:              MailSnapshot{PendingAck: 1},
		DegradedProviders: []string{"mail"},
	})
	if len(b.ResidualRisks) < 2 {
		t.Fatalf("expected multiple risks, got %d", len(b.ResidualRisks))
	}
	for i := 1; i < len(b.ResidualRisks); i++ {
		ri := riskRank(b.ResidualRisks[i-1].Severity)
		rj := riskRank(b.ResidualRisks[i].Severity)
		if rj > ri {
			t.Errorf("risks not sorted by severity desc at index %d: %s precedes %s",
				i, b.ResidualRisks[i-1].Severity, b.ResidualRisks[i].Severity)
		}
		if rj == ri && b.ResidualRisks[i].Code < b.ResidualRisks[i-1].Code {
			t.Errorf("risks not sorted by code asc within severity at index %d: %s precedes %s",
				i, b.ResidualRisks[i-1].Code, b.ResidualRisks[i].Code)
		}
	}
}

func TestBuildCloseout_JSONShapeIsStable(t *testing.T) {
	t.Parallel()
	in := CloseoutInputs{
		Now: closeoutClock(),
		Run: RunMeta{SwarmName: "swarm-1"},
		Commits: []CommitEntry{
			{Hash: "z9z9", Subject: "later commit"},
			{Hash: "abc1", Subject: "earlier"},
		},
		Beads: BeadsDelta{Closed: []string{"bd-2", "bd-1"}},
		Verifications: []VerificationEntry{
			{Command: "go test", Outcome: OutcomePassed},
		},
		Reservations: []ReservationSnapshot{
			{PathPattern: "z/**", AgentName: "B", Exclusive: true},
			{PathPattern: "a/**", AgentName: "A", Exclusive: true},
		},
	}
	a, _ := json.Marshal(BuildCloseout(in))
	c, _ := json.Marshal(BuildCloseout(in))
	if string(a) != string(c) {
		t.Errorf("Closeout JSON drifted between Build calls:\nfirst:  %s\nsecond: %s", a, c)
	}
	for _, want := range []string{
		`"schema_version":1`,
		`"counts"`,
		`"abc1"`, // commits sorted by hash, abc1 should appear before z9z9
	} {
		if !strings.Contains(string(a), want) {
			t.Errorf("JSON missing %s: %s", want, a)
		}
	}
	// Reservations must be sorted by PathPattern asc.
	first := strings.Index(string(a), `"a/**"`)
	second := strings.Index(string(a), `"z/**"`)
	if first < 0 || second < 0 || first > second {
		t.Errorf("reservations not in sorted order: a/**=%d z/**=%d", first, second)
	}
}

func TestBuildCloseout_NoVerificationInferenceFromCommits(t *testing.T) {
	t.Parallel()
	// Acceptance criterion: "Does not guess command success; only
	// records supplied or observed evidence." Even with commits
	// landed, no Verifications means no automatic "passed" inference.
	b := BuildCloseout(CloseoutInputs{
		Now:     closeoutClock(),
		Commits: []CommitEntry{{Hash: "abc", Subject: "feat: x"}},
	})
	if b.Counts.VerificationsPassed != 0 {
		t.Errorf("VerificationsPassed = %d, want 0 (no inference allowed)", b.Counts.VerificationsPassed)
	}
	for _, r := range b.ResidualRisks {
		if r.Code == "verification_failed" {
			t.Errorf("verification_failed must not fire without an explicit Outcome=failed")
		}
	}
}

func riskCodeSet(risks []ResidualRisk) map[string]bool {
	out := make(map[string]bool, len(risks))
	for _, r := range risks {
		out[r.Code] = true
	}
	return out
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
