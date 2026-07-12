package backend

import "testing"

func TestCurrentDefaultTmux(t *testing.T) {
	t.Setenv("NTM_BACKEND", "")
	t.Setenv("HERDCTL_BACKEND", "")
	t.Setenv("NTM_MUX", "")
	if Current() != Tmux {
		t.Fatalf("got %s", Current())
	}
	if !IsTmux() || IsHerdr() {
		t.Fatal("expected tmux")
	}
}

func TestCurrentHerdr(t *testing.T) {
	t.Setenv("NTM_BACKEND", "herdr")
	t.Setenv("HERDCTL_BACKEND", "")
	t.Setenv("NTM_MUX", "")
	if Current() != Herdr {
		t.Fatalf("got %s", Current())
	}
	if !IsHerdr() {
		t.Fatal("expected herdr")
	}
}

func TestCurrentAliasAndUnknown(t *testing.T) {
	t.Setenv("NTM_BACKEND", "")
	t.Setenv("HERDCTL_BACKEND", "HERD")
	t.Setenv("NTM_MUX", "")
	if Current() != Herdr {
		t.Fatalf("alias got %s", Current())
	}
	t.Setenv("HERDCTL_BACKEND", "")
	t.Setenv("NTM_BACKEND", "potato")
	if Current() != Tmux {
		t.Fatalf("unknown should fall back to tmux, got %s", Current())
	}
}
