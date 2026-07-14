//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM CLI commands.
// [E2E-WEBHOOK] Tests for webhook delivery, retry, and signature handling.
package e2e

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type WebhookPayload struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Timestamp time.Time         `json:"timestamp"`
	Session   string            `json:"session,omitempty"`
	Pane      string            `json:"pane,omitempty"`
	Agent     string            `json:"agent,omitempty"`
	Message   string            `json:"message"`
	Details   map[string]string `json:"details,omitempty"`
}

type webhookCapture struct {
	payload       WebhookPayload
	raw           []byte
	signature     string
	eventTypeHdr  string
	attemptHeader string
}

type WebhookTestSuite struct {
	t          *testing.T
	logger     *TestLogger
	ntmPath    string
	baseDir    string
	projectDir string
	session    string
	cleanup    []func()
}

func NewWebhookTestSuite(t *testing.T, scenario string) *WebhookTestSuite {
	logger := NewTestLogger(t, scenario)

	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm binary not found in PATH")
	}

	baseDir, err := os.MkdirTemp("", "ntm-webhook-e2e-")
	if err != nil {
		t.Fatalf("[E2E-WEBHOOK] Failed to create temp dir: %v", err)
	}

	session := fmt.Sprintf("e2e_webhook_%d", time.Now().UnixNano())
	projectDir := filepath.Join(baseDir, session)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("[E2E-WEBHOOK] Failed to create project dir: %v", err)
	}

	suite := &WebhookTestSuite{
		t:          t,
		logger:     logger,
		ntmPath:    ntmPath,
		baseDir:    baseDir,
		projectDir: projectDir,
		session:    session,
	}

	suite.cleanup = append(suite.cleanup, func() {
		os.RemoveAll(baseDir)
	})

	return suite
}

func (s *WebhookTestSuite) Cleanup() {
	s.logger.Close()
	for _, fn := range s.cleanup {
		fn()
	}
}

func (s *WebhookTestSuite) WriteConfig(contents string) {
	path := filepath.Join(s.projectDir, ".ntm.yaml")
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		s.t.Fatalf("[E2E-WEBHOOK] write .ntm.yaml failed: %v", err)
	}
}

func (s *WebhookTestSuite) env() []string {
	return append(os.Environ(), "NTM_PROJECTS_BASE="+s.baseDir)
}

func (s *WebhookTestSuite) runNTM(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, s.ntmPath, args...)
	cmd.Env = s.env()
	cmd.Dir = s.projectDir
	return cmd.CombinedOutput()
}

func (s *WebhookTestSuite) spawn(agentType string) error {
	flag, ok := agentFlag(agentType)
	if !ok {
		return fmt.Errorf("unsupported agent type: %s", agentType)
	}

	args := []string{"--json", "spawn", s.session, "--no-hooks", flag}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s.logger.Log("[E2E-WEBHOOK] Running: ntm %s", strings.Join(args, " "))
	out, err := s.runNTM(ctx, args...)
	if err != nil {
		s.logger.Log("[E2E-WEBHOOK] spawn error: %v output=%s", err, string(out))
		return fmt.Errorf("spawn failed: %w output=%s", err, string(out))
	}

	return nil
}

func (s *WebhookTestSuite) kill() error {
	args := []string{"--json", "kill", s.session, "--force"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s.logger.Log("[E2E-WEBHOOK] Running: ntm %s", strings.Join(args, " "))
	out, err := s.runNTM(ctx, args...)
	if err != nil {
		s.logger.Log("[E2E-WEBHOOK] kill error: %v output=%s", err, string(out))
		return fmt.Errorf("kill failed: %w output=%s", err, string(out))
	}
	return nil
}

func agentFlag(agentType string) (string, bool) {
	switch agentType {
	case "cc", "claude":
		return "--cc=1", true
	case "cod", "codex":
		return "--cod=1", true
	case "gmi", "gemini":
		return "--gmi=1", true
	default:
		return "", false
	}
}

func waitForWebhook(t *testing.T, ch <-chan webhookCapture, timeout time.Duration) webhookCapture {
	t.Helper()
	select {
	case capture := <-ch:
		return capture
	case <-time.After(timeout):
		t.Fatalf("[E2E-WEBHOOK] timeout waiting for webhook event")
	}
	return webhookCapture{}
}

func verifySignature(t *testing.T, secret string, capture webhookCapture) {
	t.Helper()
	if secret == "" {
		return
	}
	if capture.signature == "" {
		t.Fatalf("[E2E-WEBHOOK] missing X-NTM-Signature header")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(capture.raw)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if capture.signature != expected {
		t.Fatalf("[E2E-WEBHOOK] signature mismatch: got %q want %q", capture.signature, expected)
	}
}

// =============================================================================
// Tests
// =============================================================================

func TestWebhookE2E_SessionCreated(t *testing.T) {
	CommonE2EPrerequisites(t)
	SkipIfNoAgents(t)

	agentType := GetAvailableAgent()
	if agentType == "" {
		t.Skip("no agent CLI available")
	}

	suite := NewWebhookTestSuite(t, "webhook_session_created")
	defer suite.Cleanup()

	secret := "e2e-webhook-secret"
	recv := make(chan webhookCapture, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		var payload WebhookPayload
		_ = json.Unmarshal(body, &payload)
		recv <- webhookCapture{
			payload:       payload,
			raw:           body,
			signature:     r.Header.Get("X-NTM-Signature"),
			eventTypeHdr:  r.Header.Get("X-NTM-Event-Type"),
			attemptHeader: r.Header.Get("X-NTM-Attempt"),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	suite.WriteConfig(fmt.Sprintf(`
webhooks:
  - name: session_created
    url: %s
    events: ["session.created"]
    formatter: json
    secret: "%s"
`, srv.URL, secret))

	if err := suite.spawn(agentType); err != nil {
		t.Fatalf("[E2E-WEBHOOK] spawn failed: %v", err)
	}
	defer suite.kill()

	capture := waitForWebhook(t, recv, 30*time.Second)
	suite.logger.LogJSON("[E2E-WEBHOOK] payload", capture.payload)

	if capture.payload.Type != "session.created" {
		t.Fatalf("[E2E-WEBHOOK] unexpected type: %q", capture.payload.Type)
	}
	if capture.payload.Session != suite.session {
		t.Fatalf("[E2E-WEBHOOK] unexpected session: %q", capture.payload.Session)
	}
	if capture.eventTypeHdr != "session.created" {
		t.Fatalf("[E2E-WEBHOOK] unexpected X-NTM-Event-Type: %q", capture.eventTypeHdr)
	}

	verifySignature(t, secret, capture)
}

func TestWebhookE2E_RetryOnFailure(t *testing.T) {
	CommonE2EPrerequisites(t)
	SkipIfNoAgents(t)

	agentType := GetAvailableAgent()
	if agentType == "" {
		t.Skip("no agent CLI available")
	}

	suite := NewWebhookTestSuite(t, "webhook_retry")
	defer suite.Cleanup()

	var attempts int32
	recv := make(chan webhookCapture, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		var payload WebhookPayload
		_ = json.Unmarshal(body, &payload)

		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		recv <- webhookCapture{
			payload:       payload,
			raw:           body,
			signature:     r.Header.Get("X-NTM-Signature"),
			eventTypeHdr:  r.Header.Get("X-NTM-Event-Type"),
			attemptHeader: r.Header.Get("X-NTM-Attempt"),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	suite.WriteConfig(fmt.Sprintf(`
webhooks:
  - name: session_killed
    url: %s
    events: ["session.killed"]
    formatter: json
    retry:
      max_attempts: 3
`, srv.URL))

	if err := suite.spawn(agentType); err != nil {
		t.Fatalf("[E2E-WEBHOOK] spawn failed: %v", err)
	}

	if err := suite.kill(); err != nil {
		t.Fatalf("[E2E-WEBHOOK] kill failed: %v", err)
	}

	capture := waitForWebhook(t, recv, 30*time.Second)
	suite.logger.LogJSON("[E2E-WEBHOOK] payload", capture.payload)

	if capture.payload.Type != "session.killed" {
		t.Fatalf("[E2E-WEBHOOK] unexpected type: %q", capture.payload.Type)
	}

	if got := atomic.LoadInt32(&attempts); got < 3 {
		t.Fatalf("[E2E-WEBHOOK] expected retries (>=3 attempts), got %d", got)
	}
}

func TestWebhookE2E_TimeoutRecovery(t *testing.T) {
	CommonE2EPrerequisites(t)
	SkipIfNoAgents(t)

	agentType := GetAvailableAgent()
	if agentType == "" {
		t.Skip("no agent CLI available")
	}

	suite := NewWebhookTestSuite(t, "webhook_timeout_recovery")
	defer suite.Cleanup()

	var attempts int32
	recv := make(chan webhookCapture, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		var payload WebhookPayload
		_ = json.Unmarshal(body, &payload)

		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			// Simulate a timeout on the first attempt.
			time.Sleep(300 * time.Millisecond)
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}

		recv <- webhookCapture{
			payload:       payload,
			raw:           body,
			signature:     r.Header.Get("X-NTM-Signature"),
			eventTypeHdr:  r.Header.Get("X-NTM-Event-Type"),
			attemptHeader: r.Header.Get("X-NTM-Attempt"),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	suite.WriteConfig(fmt.Sprintf(`
webhooks:
  - name: session_killed_timeout
    url: %s
    events: ["session.killed"]
    formatter: json
    timeout: 100ms
    retry:
      max_attempts: 2
`, srv.URL))

	if err := suite.spawn(agentType); err != nil {
		t.Fatalf("[E2E-WEBHOOK] spawn failed: %v", err)
	}

	if err := suite.kill(); err != nil {
		t.Fatalf("[E2E-WEBHOOK] kill failed: %v", err)
	}

	capture := waitForWebhook(t, recv, 30*time.Second)
	suite.logger.LogJSON("[E2E-WEBHOOK] payload", capture.payload)

	if capture.payload.Type != "session.killed" {
		t.Fatalf("[E2E-WEBHOOK] unexpected type: %q", capture.payload.Type)
	}

	if got := atomic.LoadInt32(&attempts); got < 2 {
		t.Fatalf("[E2E-WEBHOOK] expected timeout retry (>=2 attempts), got %d", got)
	}
}

func TestWebhookE2E_NetworkFailureRecovery(t *testing.T) {
	CommonE2EPrerequisites(t)
	SkipIfNoAgents(t)

	agentType := GetAvailableAgent()
	if agentType == "" {
		t.Skip("no agent CLI available")
	}

	suite := NewWebhookTestSuite(t, "webhook_network_failure")
	defer suite.Cleanup()

	var attempts int32
	recv := make(chan webhookCapture, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		var payload WebhookPayload
		_ = json.Unmarshal(body, &payload)

		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			// Simulate a hard network failure by closing the connection.
			if hijacker, ok := w.(http.Hijacker); ok {
				conn, _, err := hijacker.Hijack()
				if err == nil {
					_ = conn.Close()
					return
				}
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		recv <- webhookCapture{
			payload:       payload,
			raw:           body,
			signature:     r.Header.Get("X-NTM-Signature"),
			eventTypeHdr:  r.Header.Get("X-NTM-Event-Type"),
			attemptHeader: r.Header.Get("X-NTM-Attempt"),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	suite.WriteConfig(fmt.Sprintf(`
webhooks:
  - name: session_killed_network
    url: %s
    events: ["session.killed"]
    formatter: json
    retry:
      max_attempts: 2
`, srv.URL))

	if err := suite.spawn(agentType); err != nil {
		t.Fatalf("[E2E-WEBHOOK] spawn failed: %v", err)
	}

	if err := suite.kill(); err != nil {
		t.Fatalf("[E2E-WEBHOOK] kill failed: %v", err)
	}

	capture := waitForWebhook(t, recv, 30*time.Second)
	suite.logger.LogJSON("[E2E-WEBHOOK] payload", capture.payload)

	if capture.payload.Type != "session.killed" {
		t.Fatalf("[E2E-WEBHOOK] unexpected type: %q", capture.payload.Type)
	}

	if got := atomic.LoadInt32(&attempts); got < 2 {
		t.Fatalf("[E2E-WEBHOOK] expected network failure retry (>=2 attempts), got %d", got)
	}
}

func TestWebhookE2E_MultipleEndpoints(t *testing.T) {
	CommonE2EPrerequisites(t)
	SkipIfNoAgents(t)

	agentType := GetAvailableAgent()
	if agentType == "" {
		t.Skip("no agent CLI available")
	}

	suite := NewWebhookTestSuite(t, "webhook_multiple_endpoints")
	defer suite.Cleanup()

	recvA := make(chan webhookCapture, 1)
	recvB := make(chan webhookCapture, 1)

	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		var payload WebhookPayload
		_ = json.Unmarshal(body, &payload)
		recvA <- webhookCapture{payload: payload}
		w.WriteHeader(http.StatusOK)
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		var payload WebhookPayload
		_ = json.Unmarshal(body, &payload)
		recvB <- webhookCapture{payload: payload}
		w.WriteHeader(http.StatusOK)
	}))
	defer srvB.Close()

	suite.WriteConfig(fmt.Sprintf(`
webhooks:
  - name: endpoint_a
    url: %s
    events: ["session.created"]
    formatter: json
  - name: endpoint_b
    url: %s
    events: ["session.created"]
    formatter: json
`, srvA.URL, srvB.URL))

	if err := suite.spawn(agentType); err != nil {
		t.Fatalf("[E2E-WEBHOOK] spawn failed: %v", err)
	}
	defer suite.kill()

	captureA := waitForWebhook(t, recvA, 30*time.Second)
	captureB := waitForWebhook(t, recvB, 30*time.Second)

	if captureA.payload.Type != "session.created" || captureB.payload.Type != "session.created" {
		t.Fatalf("[E2E-WEBHOOK] expected session.created for both endpoints")
	}
	if captureA.payload.Session != suite.session || captureB.payload.Session != suite.session {
		t.Fatalf("[E2E-WEBHOOK] expected session %q for both endpoints", suite.session)
	}
}
