package session

import (
	"errors"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func TestResolveExplicitSessionName(t *testing.T) {
	sessions := []tmux.Session{
		{Name: "alpha"},
		{Name: "beta"},
		{Name: "my_project"},
		{Name: "my_test"},
	}

	cases := []struct {
		name        string
		input       string
		allowPrefix bool
		sessions    []tmux.Session
		want        string
		wantReason  string
		wantErr     ResolveExplicitSessionNameErrorKind
		errContains []string
	}{
		{
			name:        "exact match",
			input:       "my_project",
			allowPrefix: true,
			sessions:    sessions,
			want:        "my_project",
			wantReason:  "exact match",
		},
		{
			name:        "exact match wins over prefix",
			input:       "my",
			allowPrefix: false,
			sessions: []tmux.Session{
				{Name: "my"},
				{Name: "my_project"},
			},
			want:       "my",
			wantReason: "exact match",
		},
		{
			name:        "unique prefix",
			input:       "alp",
			allowPrefix: true,
			sessions:    sessions,
			want:        "alpha",
			wantReason:  "prefix match",
		},
		{
			name:        "ambiguous prefix",
			input:       "my",
			allowPrefix: true,
			sessions:    sessions,
			wantErr:     ResolveExplicitSessionNameErrorAmbiguous,
			errContains: []string{"matches multiple sessions", "my_project", "my_test"},
		},
		{
			name:        "prefix disabled",
			input:       "alp",
			allowPrefix: false,
			sessions:    sessions,
			wantErr:     ResolveExplicitSessionNameErrorNotFound,
			errContains: []string{"not found", "available"},
		},
		{
			name:        "no match",
			input:       "zzz",
			allowPrefix: true,
			sessions:    sessions,
			wantErr:     ResolveExplicitSessionNameErrorNotFound,
			errContains: []string{"not found", "available", "alpha", "beta"},
		},
		{
			name:        "no sessions",
			input:       "anything",
			allowPrefix: true,
			sessions:    nil,
			wantErr:     ResolveExplicitSessionNameErrorNoSessions,
			errContains: []string{"no sessions running"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved, reason, err := ResolveExplicitSessionName(tc.input, tc.sessions, tc.allowPrefix)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				var re *ResolveExplicitSessionNameError
				if !errors.As(err, &re) {
					t.Fatalf("expected ResolveExplicitSessionNameError, got %T", err)
				}
				if re.Kind != tc.wantErr {
					t.Fatalf("kind %q, want %q", re.Kind, tc.wantErr)
				}
				for _, substr := range tc.errContains {
					if !strings.Contains(err.Error(), substr) {
						t.Fatalf("expected error to contain %q, got %q", substr, err.Error())
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resolved != tc.want {
				t.Fatalf("resolved %q, want %q", resolved, tc.want)
			}
			if reason != tc.wantReason {
				t.Fatalf("reason %q, want %q", reason, tc.wantReason)
			}
		})
	}
}
