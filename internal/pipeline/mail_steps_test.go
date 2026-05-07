package pipeline

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMailSteps_ParseYAMLAndValidate(t *testing.T) {
	content := `
schema_version: "2.0"
name: mail-steps
steps:
  - id: notify
    mail_send:
      project_key: /data/projects/ntm
      agent_name: TealCrane
      to: [SageFern, OrangeFalcon]
      subject: "[bd-b5l8d] status"
      body: "Done"
      thread_id: bd-b5l8d
      ack_required: true
  - id: reserve
    file_reservation_paths:
      project_key: /data/projects/ntm
      agent_name: TealCrane
      paths: [internal/pipeline/schema.go, internal/pipeline/mail_steps.go]
      ttl_seconds: 3600
      exclusive: true
      reason: bd-b5l8d
  - id: inbox
    mail_inbox_check:
      project_key: /data/projects/ntm
      agent_name: TealCrane
      until_ack_count: 2
  - id: release
    file_reservation_release:
      project_key: /data/projects/ntm
      agent_name: TealCrane
      paths: internal/pipeline/schema.go
`

	workflow, err := ParseString(content, "yaml")
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if result := Validate(workflow); !result.Valid {
		t.Fatalf("Validate() failed: %+v", result.Errors)
	}

	send := workflow.Steps[0].MailSend
	if send == nil {
		t.Fatal("MailSend = nil")
	}
	if !reflect.DeepEqual(send.To, StringOrList{"SageFern", "OrangeFalcon"}) {
		t.Fatalf("MailSend.To = %#v, want two recipients", send.To)
	}
	if !send.AckRequired || send.ThreadID != "bd-b5l8d" {
		t.Fatalf("MailSend metadata = %#v", send)
	}

	reserve := workflow.Steps[1].FileReservationPaths
	if reserve == nil {
		t.Fatal("FileReservationPaths = nil")
	}
	if reserve.TTLSeconds != 3600 || !reserve.Exclusive || reserve.Reason != "bd-b5l8d" {
		t.Fatalf("FileReservationPaths = %#v", reserve)
	}

	inbox := workflow.Steps[2].MailInboxCheck
	if inbox == nil || inbox.UntilAckCount != 2 {
		t.Fatalf("MailInboxCheck = %#v, want until_ack_count=2", inbox)
	}

	release := workflow.Steps[3].FileReservationRelease
	if release == nil {
		t.Fatal("FileReservationRelease = nil")
	}
	if !reflect.DeepEqual(release.Paths, StringOrList{"internal/pipeline/schema.go"}) {
		t.Fatalf("FileReservationRelease.Paths = %#v, want scalar path as one-item list", release.Paths)
	}
}

func TestMailSteps_ParseTOMLKnownFields(t *testing.T) {
	content := `
schema_version = "2.0"
name = "mail-steps-toml"

[[steps]]
id = "notify"

[steps.mail_send]
project_key = "/data/projects/ntm"
agent_name = "TealCrane"
to = ["SageFern"]
subject = "[bd-b5l8d] status"
body = "Done"
thread_id = "bd-b5l8d"
ack_required = true
`

	workflow, err := ParseString(content, "toml")
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if result := Validate(workflow); !result.Valid {
		t.Fatalf("Validate() failed: %+v", result.Errors)
	}
	if got := workflow.Steps[0].MailSend.To; !reflect.DeepEqual(got, StringOrList{"SageFern"}) {
		t.Fatalf("MailSend.To = %#v, want SageFern", got)
	}
}

func TestMailSteps_JSONRoundTrip(t *testing.T) {
	step := Step{
		ID: "reserve",
		FileReservationPaths: &FileReservationPathsStep{
			ProjectKey: "/data/projects/ntm",
			AgentName:  "TealCrane",
			Paths:      StringOrList{"internal/pipeline/schema.go"},
			TTLSeconds: 3600,
			Exclusive:  true,
			Reason:     "bd-b5l8d",
		},
	}

	data, err := json.Marshal(step)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got Step
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v\nJSON:\n%s", err, data)
	}
	if !reflect.DeepEqual(got, step) {
		t.Fatalf("JSON round trip mismatch\nwant: %#v\n got: %#v\nJSON:\n%s", step, got, data)
	}
}

func TestMailSteps_ValidationConflicts(t *testing.T) {
	tests := []struct {
		name    string
		step    Step
		wantErr string
	}{
		{
			name: "mail send with command",
			step: Step{
				ID:       "bad",
				Command:  "echo should-not-run",
				MailSend: validMailSendStep(),
			},
			wantErr: "cannot combine Agent Mail step kind",
		},
		{
			name: "two mail kinds",
			step: Step{
				ID:             "bad",
				MailSend:       validMailSendStep(),
				MailInboxCheck: &MailInboxCheckStep{ProjectKey: "/data/projects/ntm", AgentName: "TealCrane"},
			},
			wantErr: "can only use one Agent Mail step kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Validate(&Workflow{
				SchemaVersion: SchemaVersion,
				Name:          "mail-step-conflict",
				Steps:         []Step{tt.step},
			})
			if result.Valid {
				t.Fatal("Validate() succeeded, want conflict")
			}
			for _, err := range result.Errors {
				if strings.Contains(err.Message, tt.wantErr) {
					return
				}
			}
			t.Fatalf("Validate() errors = %+v, want message containing %q", result.Errors, tt.wantErr)
		})
	}
}

func validMailSendStep() *MailSendStep {
	return &MailSendStep{
		ProjectKey: "/data/projects/ntm",
		AgentName:  "TealCrane",
		To:         StringOrList{"SageFern"},
		Subject:    "[bd-b5l8d] status",
		Body:       "Done",
		ThreadID:   "bd-b5l8d",
	}
}

func TestMailSteps_ValidationRequiredFields(t *testing.T) {
	// bd-vv7ij: each Agent Mail step kind must surface required-field
	// errors at parse time. Before this fix, mail_send: {} validated
	// successfully, file_reservation_paths with no paths validated, and
	// mail_inbox_check / file_reservation_release with no project_key /
	// agent_name validated.
	tests := []struct {
		name    string
		step    Step
		wantErr string
	}{
		{
			name:    "mail_send empty",
			step:    Step{ID: "send", MailSend: &MailSendStep{}},
			wantErr: "mail_send requires project_key",
		},
		{
			name:    "mail_send missing recipients",
			step:    Step{ID: "send", MailSend: &MailSendStep{ProjectKey: "/p", AgentName: "A", Subject: "s", Body: "b"}},
			wantErr: "mail_send requires at least one recipient in to",
		},
		{
			name:    "mail_send missing subject and body",
			step:    Step{ID: "send", MailSend: &MailSendStep{ProjectKey: "/p", AgentName: "A", To: StringOrList{"B"}}},
			wantErr: "mail_send requires subject or body",
		},
		{
			name:    "file_reservation_paths missing paths",
			step:    Step{ID: "lock", FileReservationPaths: &FileReservationPathsStep{ProjectKey: "/p", AgentName: "A"}},
			wantErr: "file_reservation_paths requires at least one path",
		},
		{
			name:    "file_reservation_paths negative ttl",
			step:    Step{ID: "lock", FileReservationPaths: &FileReservationPathsStep{ProjectKey: "/p", AgentName: "A", Paths: StringOrList{"a.go"}, TTLSeconds: -1}},
			wantErr: "ttl_seconds must be non-negative",
		},
		{
			name:    "mail_inbox_check empty",
			step:    Step{ID: "inbox", MailInboxCheck: &MailInboxCheckStep{}},
			wantErr: "mail_inbox_check requires project_key",
		},
		{
			name:    "mail_inbox_check missing agent",
			step:    Step{ID: "inbox", MailInboxCheck: &MailInboxCheckStep{ProjectKey: "/p"}},
			wantErr: "mail_inbox_check requires agent_name",
		},
		{
			name:    "file_reservation_release missing paths",
			step:    Step{ID: "release", FileReservationRelease: &FileReservationReleaseStep{ProjectKey: "/p", AgentName: "A"}},
			wantErr: "file_reservation_release requires at least one path",
		},
		{
			name:    "file_reservation_release blank path",
			step:    Step{ID: "release", FileReservationRelease: &FileReservationReleaseStep{ProjectKey: "/p", AgentName: "A", Paths: StringOrList{"   "}}},
			wantErr: "file_reservation_release path cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Validate(&Workflow{
				SchemaVersion: SchemaVersion,
				Name:          "mail-step-required",
				Steps:         []Step{tt.step},
			})
			if result.Valid {
				t.Fatal("Validate() succeeded, want required-field error")
			}
			for _, err := range result.Errors {
				if strings.Contains(err.Message, tt.wantErr) {
					return
				}
			}
			t.Fatalf("Validate() errors = %+v, want message containing %q", result.Errors, tt.wantErr)
		})
	}
}

func TestMailSteps_ValidationAcceptsValid(t *testing.T) {
	// bd-vv7ij: a fully populated step of each Agent Mail kind must remain
	// valid after the new required-field checks land.
	steps := []Step{
		{ID: "send", MailSend: validMailSendStep()},
		{ID: "lock", FileReservationPaths: &FileReservationPathsStep{ProjectKey: "/p", AgentName: "A", Paths: StringOrList{"a.go", "b.go"}, TTLSeconds: 60}},
		{ID: "inbox", MailInboxCheck: &MailInboxCheckStep{ProjectKey: "/p", AgentName: "A", UntilAckCount: 1}},
		{ID: "release", FileReservationRelease: &FileReservationReleaseStep{ProjectKey: "/p", AgentName: "A", Paths: StringOrList{"a.go"}}},
	}
	result := Validate(&Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "mail-step-valid",
		Steps:         steps,
	})
	if !result.Valid {
		t.Fatalf("Validate() errors = %+v, want all four mail steps to validate", result.Errors)
	}
}
