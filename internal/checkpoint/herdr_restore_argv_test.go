package checkpoint

import (
	"reflect"
	"testing"
)

func TestHerdrRestoreArgv(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"claude", []string{"claude"}},
		{"claude --dangerously-skip-permissions", []string{"claude", "--dangerously-skip-permissions"}},
		{"FOO=bar claude", []string{"sh", "-c", "FOO=bar claude"}},
		{"claude && echo x", []string{"sh", "-c", "claude && echo x"}},
		{"codex --dangerously-bypass-approvals-and-sandbox", []string{"codex", "--dangerously-bypass-approvals-and-sandbox"}},
	}
	for _, tc := range tests {
		got := herdrRestoreArgv(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("herdrRestoreArgv(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}
