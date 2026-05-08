package supportbundle

import (
	"sort"
	"strings"
	"time"
)

// CloseoutSchemaVersion is the version of the closeout bundle JSON
// shape. Bump on backward-incompatible changes; additive fields keep
// the same version.
const CloseoutSchemaVersion = 1

// VerificationOutcome is a closed enum for command outcomes the
// closeout bundle records. The package never *runs* commands or
// guesses success — these values are supplied by the caller from
// observed evidence.
type VerificationOutcome string

const (
	OutcomePassed  VerificationOutcome = "passed"
	OutcomeFailed  VerificationOutcome = "failed"
	OutcomeSkipped VerificationOutcome = "skipped"
	OutcomeUnknown VerificationOutcome = "unknown"
)

// ResidualRiskSeverity grades a risk for dashboard rollup.
type ResidualRiskSeverity string

const (
	RiskSeverityHigh   ResidualRiskSeverity = "high"
	RiskSeverityMedium ResidualRiskSeverity = "medium"
	RiskSeverityLow    ResidualRiskSeverity = "low"
)

// RunMeta is the high-level identity of the run being closed out.
type RunMeta struct {
	SwarmName string    `json:"swarm_name,omitempty"`
	AgentName string    `json:"agent_name,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

// CommitEntry is one committed change. Hash should be the short or
// long SHA the caller saw in `git log`.
type CommitEntry struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Author  string `json:"author,omitempty"`
}

// BeadsDelta records the bead state change across the run.
type BeadsDelta struct {
	Opened    []string `json:"opened,omitempty"`
	Closed    []string `json:"closed,omitempty"`
	StillOpen []string `json:"still_open,omitempty"`
}

// VerificationEntry records one command the operator (or harness)
// observed running. Outcome is whatever the caller saw — the closeout
// bundle does not infer success.
type VerificationEntry struct {
	Command  string              `json:"command"`
	Outcome  VerificationOutcome `json:"outcome"`
	Notes    string              `json:"notes,omitempty"`
	Duration time.Duration       `json:"duration,omitempty"`
}

// ReservationSnapshot is one outstanding reservation at closeout time.
type ReservationSnapshot struct {
	PathPattern string    `json:"path_pattern"`
	AgentName   string    `json:"agent_name"`
	Exclusive   bool      `json:"exclusive"`
	AcquiredAt  time.Time `json:"acquired_at"`
}

// MailSnapshot rolls up the operator's outstanding mail at closeout.
type MailSnapshot struct {
	UnackedUrgent int `json:"unacked_urgent"`
	PendingAck    int `json:"pending_ack"`
}

// QueueState rolls up the bv ready / in-progress queue at closeout.
type QueueState struct {
	Ready      int  `json:"ready"`
	InProgress int  `json:"in_progress"`
	Blocked    int  `json:"blocked"`
	QueueDry   bool `json:"queue_dry"`
}

// ResidualRisk is one outstanding concern derived from inputs. Codes
// are stable so consumers can route on them.
type ResidualRisk struct {
	Code        string               `json:"code"`
	Severity    ResidualRiskSeverity `json:"severity"`
	Description string               `json:"description"`
	Evidence    []string             `json:"evidence,omitempty"`
}

// CloseoutInputs is the full set of evidence BuildCloseout reduces.
type CloseoutInputs struct {
	Run               RunMeta
	Commits           []CommitEntry
	Beads             BeadsDelta
	Verifications     []VerificationEntry
	Reservations      []ReservationSnapshot
	Mail              MailSnapshot
	Queue             QueueState
	DegradedProviders []string
	Notes             []string
	Now               time.Time
}

// CloseoutBundle is the JSON-first artifact a swarm or long agent
// run emits at end-of-run. It is read-only data — no commands are
// executed by this package.
type CloseoutBundle struct {
	SchemaVersion     int                   `json:"schema_version"`
	GeneratedAt       time.Time             `json:"generated_at"`
	Run               RunMeta               `json:"run"`
	Commits           []CommitEntry         `json:"commits,omitempty"`
	Beads             BeadsDelta            `json:"beads"`
	Verifications     []VerificationEntry   `json:"verifications,omitempty"`
	Reservations      []ReservationSnapshot `json:"active_reservations,omitempty"`
	Mail              MailSnapshot         `json:"mail"`
	Queue             QueueState           `json:"queue"`
	DegradedProviders []string              `json:"degraded_providers,omitempty"`
	ResidualRisks     []ResidualRisk        `json:"residual_risks,omitempty"`
	Counts            CloseoutCounts        `json:"counts"`
	Notes             []string              `json:"notes,omitempty"`
}

// CloseoutCounts is a small rollup so a dashboard can summarize a
// bundle without re-traversing every field.
type CloseoutCounts struct {
	Commits             int `json:"commits"`
	BeadsOpened         int `json:"beads_opened"`
	BeadsClosed         int `json:"beads_closed"`
	BeadsStillOpen      int `json:"beads_still_open"`
	Verifications       int `json:"verifications"`
	VerificationsPassed int `json:"verifications_passed"`
	VerificationsFailed int `json:"verifications_failed"`
	ActiveReservations  int `json:"active_reservations"`
	ResidualRisks       int `json:"residual_risks"`
}

// BuildCloseout reduces inputs into a CloseoutBundle. Pure: no I/O,
// never infers verification success — Outcome must be supplied.
//
// Residual risks are derived deterministically from inputs:
//   - verification_failed [HIGH]: any Verification.Outcome == failed
//   - verification_inconclusive [MEDIUM]: any Outcome == unknown / skipped
//   - active_reservations_outstanding [MEDIUM]: at least one reservation
//   - unacked_urgent_mail [HIGH]: Mail.UnackedUrgent > 0
//   - pending_ack_mail [LOW]: Mail.PendingAck > 0 (and no urgent)
//   - beads_still_open [MEDIUM]: any bead in StillOpen
//   - providers_degraded [MEDIUM]: any DegradedProviders entry
func BuildCloseout(in CloseoutInputs) CloseoutBundle {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}

	bundle := CloseoutBundle{
		SchemaVersion:     CloseoutSchemaVersion,
		GeneratedAt:       now.UTC(),
		Run:               in.Run,
		Commits:           append([]CommitEntry(nil), in.Commits...),
		Beads:             dedupedBeadsDelta(in.Beads),
		Verifications:     append([]VerificationEntry(nil), in.Verifications...),
		Reservations:      append([]ReservationSnapshot(nil), in.Reservations...),
		Mail:              in.Mail,
		Queue:             in.Queue,
		DegradedProviders: uniqueSortedCloseout(in.DegradedProviders),
		Notes:             append([]string(nil), in.Notes...),
	}

	// Residual risks first so the count below reflects them. Reversing
	// these two lines silently always reports counts.residual_risks=0
	// regardless of how many entries computeResidualRisks produced
	// (bd-55myk).
	bundle.ResidualRisks = computeResidualRisks(bundle)
	bundle.Counts = computeCloseoutCounts(bundle)

	// Sort emitted lists deterministically so the bundle is byte-stable.
	sort.SliceStable(bundle.Commits, func(i, j int) bool {
		return bundle.Commits[i].Hash < bundle.Commits[j].Hash
	})
	sort.SliceStable(bundle.Reservations, func(i, j int) bool {
		if bundle.Reservations[i].PathPattern != bundle.Reservations[j].PathPattern {
			return bundle.Reservations[i].PathPattern < bundle.Reservations[j].PathPattern
		}
		return bundle.Reservations[i].AgentName < bundle.Reservations[j].AgentName
	})
	// Verifications keep insertion order — operators care about the
	// observed sequence — but the count fields are already aggregated.

	return bundle
}

func dedupedBeadsDelta(b BeadsDelta) BeadsDelta {
	return BeadsDelta{
		Opened:    uniqueSortedCloseout(b.Opened),
		Closed:    uniqueSortedCloseout(b.Closed),
		StillOpen: uniqueSortedCloseout(b.StillOpen),
	}
}

func computeCloseoutCounts(b CloseoutBundle) CloseoutCounts {
	c := CloseoutCounts{
		Commits:            len(b.Commits),
		BeadsOpened:        len(b.Beads.Opened),
		BeadsClosed:        len(b.Beads.Closed),
		BeadsStillOpen:     len(b.Beads.StillOpen),
		Verifications:      len(b.Verifications),
		ActiveReservations: len(b.Reservations),
		ResidualRisks:      len(b.ResidualRisks),
	}
	for _, v := range b.Verifications {
		switch v.Outcome {
		case OutcomePassed:
			c.VerificationsPassed++
		case OutcomeFailed:
			c.VerificationsFailed++
		}
	}
	return c
}

func computeResidualRisks(b CloseoutBundle) []ResidualRisk {
	var risks []ResidualRisk

	// Verification failures (HIGH) — the most serious signal.
	failedCmds := []string{}
	inconclusiveCmds := []string{}
	for _, v := range b.Verifications {
		switch v.Outcome {
		case OutcomeFailed:
			failedCmds = append(failedCmds, v.Command)
		case OutcomeUnknown, OutcomeSkipped:
			inconclusiveCmds = append(inconclusiveCmds, v.Command+":"+string(v.Outcome))
		}
	}
	if len(failedCmds) > 0 {
		sort.Strings(failedCmds)
		risks = append(risks, ResidualRisk{
			Code:        "verification_failed",
			Severity:    RiskSeverityHigh,
			Description: "one or more recorded verifications failed; the run shipped with known regressions",
			Evidence:    failedCmds,
		})
	}
	if len(inconclusiveCmds) > 0 {
		sort.Strings(inconclusiveCmds)
		risks = append(risks, ResidualRisk{
			Code:        "verification_inconclusive",
			Severity:    RiskSeverityMedium,
			Description: "one or more verifications were skipped or returned an unknown outcome; success is not proven",
			Evidence:    inconclusiveCmds,
		})
	}

	// bd-056vx: emit each mail-attention residual risk independently.
	// Pre-fix the LOW pending_ack_mail risk was suppressed by an
	// else-if whenever HIGH unacked_urgent_mail fired — but the LOW
	// risk's description explicitly names itself "non-urgent mail",
	// so the two signals are orthogonal and both must surface when
	// both input counts are non-zero. Mirror of bd-zy2c1 fix in
	// internal/robot/assurance/quiescence.go.
	if b.Mail.UnackedUrgent > 0 {
		risks = append(risks, ResidualRisk{
			Code:        "unacked_urgent_mail",
			Severity:    RiskSeverityHigh,
			Description: "ack-required urgent mail was outstanding at closeout",
			Evidence:    []string{"unacked_urgent=" + itoa(b.Mail.UnackedUrgent)},
		})
	}
	if b.Mail.PendingAck > 0 {
		risks = append(risks, ResidualRisk{
			Code:        "pending_ack_mail",
			Severity:    RiskSeverityLow,
			Description: "non-urgent mail awaits acknowledgement",
			Evidence:    []string{"pending_ack=" + itoa(b.Mail.PendingAck)},
		})
	}

	if len(b.Reservations) > 0 {
		evidence := make([]string, 0, len(b.Reservations))
		for _, r := range b.Reservations {
			evidence = append(evidence, r.PathPattern+" by "+r.AgentName)
		}
		sort.Strings(evidence)
		risks = append(risks, ResidualRisk{
			Code:        "active_reservations_outstanding",
			Severity:    RiskSeverityMedium,
			Description: "file reservations were still held at closeout; downstream agents may collide",
			Evidence:    evidence,
		})
	}

	if len(b.Beads.StillOpen) > 0 {
		risks = append(risks, ResidualRisk{
			Code:        "beads_still_open",
			Severity:    RiskSeverityMedium,
			Description: "beads opened during the run remain open at closeout",
			Evidence:    append([]string(nil), b.Beads.StillOpen...),
		})
	}

	if len(b.DegradedProviders) > 0 {
		risks = append(risks, ResidualRisk{
			Code:        "providers_degraded",
			Severity:    RiskSeverityMedium,
			Description: "one or more coordination providers were degraded during the run; evidence may be incomplete",
			Evidence:    append([]string(nil), b.DegradedProviders...),
		})
	}

	// Sort: high severity first, then code asc, for stable JSON.
	sort.SliceStable(risks, func(i, j int) bool {
		ri := riskRank(risks[i].Severity)
		rj := riskRank(risks[j].Severity)
		if ri != rj {
			return ri > rj
		}
		return risks[i].Code < risks[j].Code
	})
	return risks
}

func riskRank(s ResidualRiskSeverity) int {
	switch s {
	case RiskSeverityHigh:
		return 3
	case RiskSeverityMedium:
		return 2
	case RiskSeverityLow:
		return 1
	default:
		return 0
	}
}

func uniqueSortedCloseout(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
