package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/digest"
)

func TestChatAPIErrorPrintsSharedDiagnosticLine(t *testing.T) {
	var out strings.Builder
	printError(&out, &client.APIError{Code: "ORIGIN_NOT_ALLOWED", Message: "origin rejected", Hint: "Add this exact origin.", Docs: digest.EmbassyDocsFor("ORIGIN_NOT_ALLOWED")})
	want := "[ReplyPen] ORIGIN_NOT_ALLOWED: Add this exact origin. — " + digest.EmbassyErrorsBase + "origin_not_allowed\n"
	if out.String() != want {
		t.Fatalf("stderr = %q, want %q", out.String(), want)
	}
}

func TestChatSecretRotatePrintsSecretOnce(t *testing.T) {
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("POST /api/v1/projects/alpha/chat/secret/rotate", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		_, _ = w.Write([]byte(`{"secret":"chat-once","source":"dedicated"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected %s %s", r.Method, r.URL.String()) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "alpha", "project", "chat", "secret", "rotate"); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "chat-once\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestChatBriefPrintsServerMarkdownAndCarriesTenant(t *testing.T) {
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("GET /api/v1/projects/alpha/chat/brief", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("tenant"); got != "north" {
			t.Fatalf("tenant = %q", got)
		}
		if got := r.URL.Query().Get("target"); got != "page" {
			t.Fatalf("target = %q", got)
		}
		_, _ = w.Write([]byte("# Brief\n\n```yaml\nreplypen_brief: v1\n```\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "alpha", "--tenant", "north", "project", "chat", "brief", "--target", "page"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "replypen_brief: v1") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestPrincipalsSetAcceptsYAML(t *testing.T) {
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("PATCH /api/v1/projects/alpha/principals", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		kinds, ok := body["kinds"].(map[string]any)
		if !ok || kinds["app_user"] == nil {
			t.Fatalf("manifest = %#v", body)
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected %s %s", r.Method, r.URL.String()) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "principals.yaml")
	if err := os.WriteFile(path, []byte("kinds:\n  app_user:\n    claims:\n      user_id:\n        type: text\n        value: ':external_id'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, out, _ := newTestEnv(t, srv, "json")
	if err := run(t, e, "--project", "alpha", "project", "principals", "set", path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "app_user") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestPrincipalsResolvePrintsExternalID(t *testing.T) {
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("POST /api/v1/projects/alpha/principals/resolve", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["kind"] != "app_user" || body["email"] != "person@example.com" || body["tenant"] != "north" {
			t.Fatalf("resolve body = %#v", body)
		}
		_, _ = w.Write([]byte(`{"kind":"app_user","external_id":"user-42","source":"email_lookup"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "alpha", "--tenant", "north", "project", "principals", "resolve", "--kind", "app_user", "--email", "person@example.com"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Scope: alpha / north\nuser-42\n" {
		t.Fatalf("output = %q", out.String())
	}
}

func TestChatDoctorBundleIsRedacted(t *testing.T) {
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("GET /api/v1/projects/alpha/chat", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"chat_enabled":{"effective":true},"chat_origins":{"effective":["https://app.example"]}}`))
	})
	mux.HandleFunc("GET /api/v1/projects/alpha/principals", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"kinds":{"app_user":{"claims":{"user_id":{"sql":"SELECT secret_schema.users"}}}},"email_lookup":{"sql":"SELECT internal_id"}}`))
	})
	mux.HandleFunc("GET /api/v1/projects/alpha/chat/secret", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"present":true,"source":"dedicated"}`))
	})
	mux.HandleFunc("GET /api/v1/branding", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":{"effective":"Example"}}`))
	})
	mux.HandleFunc("GET /api/v1/projects/alpha/chat/rejects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"rejects":[{"code":"BAD_TOKEN","origin":"https://app.example","ip_prefix":"192.0.2.0/24","session_id":"11111111-1111-1111-1111-111111111111","timestamp":"2026-09-01T00:00:00Z"}]}`))
	})
	mux.HandleFunc("GET /chat/widget/v1/loader.js", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("/* loader */")) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected %s %s", r.Method, r.URL.String()) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "alpha", "project", "chat", "doctor", "--origin", "https://app.example", "--principal-kind", "app_user", "--bundle"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(out.String()), "secret\"") && strings.Contains(out.String(), "chat-once") {
		t.Fatal("bundle leaked secret")
	}
	for _, forbidden := range []string{"secret_schema", "internal_id", "192.0.2.0/24", "11111111-1111-1111-1111-111111111111"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("bundle leaked %q: %s", forbidden, out.String())
		}
	}
	var bundle map[string]any
	if err := json.Unmarshal(out.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle["project"] != "alpha" {
		t.Fatalf("bundle = %#v", bundle)
	}
	findings, _ := bundle["findings"].([]any)
	first, _ := findings[0].(map[string]any)
	if first["code"] != "OK" || first["check"] != "CHAT_ENABLED" {
		t.Fatalf("success finding lost check identity: %#v", first)
	}
	if _, ok := bundle["raw"]; ok {
		t.Fatalf("--bundle carries raw server bodies: %s", out.String())
	}
}

func TestChatSendPrintsSSE(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"alpha","origin":"https://app.example"}`))
	token := "e30." + payload + ".sig"
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/v1/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-RC-Embed-Origin") != "https://app.example" {
			t.Fatalf("bad embed headers")
		}
		_, _ = w.Write([]byte(`{"session_id":"11111111-1111-1111-1111-111111111111"}`))
	})
	mux.HandleFunc("POST /chat/v1/message", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Chat-Tier", "standard")
		_, _ = w.Write([]byte("data: {\"type\":\"start\",\"messageId\":\"run-123\"}\n\ndata: {\"type\":\"text-delta\"}\n\ndata: [DONE]\n\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "project", "chat", "send", "hello", "--token", token); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[DONE]") {
		t.Fatalf("output = %q", out.String())
	}
	if !strings.HasSuffix(out.String(), "session_id: 11111111-1111-1111-1111-111111111111\nrun_id: run-123\ntier: standard\n") {
		t.Fatalf("missing final session/run IDs: %q", out.String())
	}
}

func TestChatSessionPrintsTranscript(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"alpha","origin":"https://app.example"}`))
	token := "e30." + payload + ".sig"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /chat/v1/session/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-RC-Embed-Origin") != "https://app.example" || r.URL.Query().Get("project") != "alpha" {
			t.Fatalf("bad embed request: %s %s", r.URL.String(), r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"session_id":"` + r.PathValue("id") + `","messages":[{"parts":[{"type":"data-action","data":{"action_run_id":"ac-1","status":"proposed"}}]}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "project", "chat", "session", "22222222-2222-2222-2222-222222222222", "--token", token); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"action_run_id": "ac-1"`) || !strings.Contains(out.String(), "22222222-2222-2222-2222-222222222222") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestChatSendAnswersLatestQuestionInExistingSession(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"alpha","origin":"https://app.example"}`))
	token := "e30." + payload + ".sig"
	const sessionID = "11111111-1111-1111-1111-111111111111"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /chat/v1/session/"+sessionID, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"messages":[{"role":"assistant","parts":[{"type":"data-questions","data":{"question_set_id":"qs-1","questions":[{"id":"area","kind":"single_select"},{"id":"detail","kind":"free_text"}]}}]}]}`))
	})
	mux.HandleFunc("POST /chat/v1/message", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Message struct {
				Parts []map[string]any `json:"parts"`
			} `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Message.Parts) != 1 || body.Message.Parts[0]["type"] != "data-answers" {
			t.Fatalf("parts = %#v", body.Message.Parts)
		}
		data := body.Message.Parts[0]["data"].(map[string]any)
		answers := data["answers"].(map[string]any)
		if answers["area"].(map[string]any)["value"] != "billing" || answers["detail"].(map[string]any)["text"] != "nightly" {
			t.Fatalf("answers = %#v", answers)
		}
		_, _ = w.Write([]byte("data: {\"type\":\"start\",\"messageId\":\"run-answer\"}\n\ndata: [DONE]\n\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "project", "chat", "send", "--token", token, "--session", sessionID, "--answer", "area=billing", "--answer", "detail=nightly"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "run_id: run-answer\n") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestChatAnswerPartDoesNotRepeatAnsweredQuestionSet(t *testing.T) {
	raw := json.RawMessage(`{"messages":[{"parts":[{"type":"data-questions","data":{"question_set_id":"qs-1","questions":[{"id":"area","kind":"single_select"}]}}]},{"parts":[{"type":"data-answers","data":{"question_set_id":"qs-1","answers":{"area":{"value":"billing"}}}}]}]}`)
	if _, err := chatAnswerPart(raw, []string{"area=billing"}); err == nil || !strings.Contains(err.Error(), "no unanswered") {
		t.Fatalf("error = %v", err)
	}
}

func TestChatSendSSEErrorIsNonZeroAndKeepsRunID(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"alpha","origin":"https://app.example"}`))
	token := "e30." + payload + ".sig"
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/v1/message", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data: {\"type\":\"start\",\"messageId\":\"run-error\"}\n\ndata: {\"type\":\"error\",\"errorText\":\"grounding failed\"}\n\ndata: [DONE]\n\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	err := run(t, e, "project", "chat", "send", "hello", "--token", token, "--session", "11111111-1111-1111-1111-111111111111")
	if err == nil || !strings.Contains(err.Error(), "grounding failed") {
		t.Fatalf("error = %v", err)
	}
	if !strings.HasSuffix(out.String(), "run_id: run-error\n") {
		t.Fatalf("output = %q", out.String())
	}
}

func chatTestProjects(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"11111111-1111-1111-1111-111111111111","name":"alpha"}]}`))
	})
	mux.HandleFunc("GET /api/v1/whoami", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"dev@example.test","project":{"id":"11111111-1111-1111-1111-111111111111","name":"alpha","slug":"alpha"}}`))
	})
}

func TestChatSecretRevealPrintsRotationAttribution(t *testing.T) {
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("POST /api/v1/projects/alpha/chat/secret/reveal", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"secret":"shown-once","rotated_by":"admin@example.com","rotated_at":"2026-09-01T10:00:00Z"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, errOut := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "alpha", "project", "chat", "secret", "reveal"); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "shown-once" {
		t.Fatalf("stdout = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "Rotated by admin@example.com at 2026-09-01T10:00:00Z") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestChatDoctorSinceMarksOldRejectStale(t *testing.T) {
	now := time.Now().UTC()
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("GET /api/v1/projects/alpha/chat", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"chat_enabled":{"effective":true},"chat_origins":{"effective":["https://app.example"]}}`))
	})
	mux.HandleFunc("GET /api/v1/projects/alpha/principals", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"kinds":{}}`)) })
	mux.HandleFunc("GET /api/v1/projects/alpha/chat/secret", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"source":"dedicated"}`)) })
	mux.HandleFunc("GET /api/v1/branding", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":{"effective":"Example"}}`))
	})
	mux.HandleFunc("GET /api/v1/projects/alpha/chat/rejects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"rejects":[{"code":"BAD_TOKEN","timestamp":%q},{"code":"PRINCIPAL_UNVERIFIED","timestamp":%q}]}`, now.Add(-2*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))
	})
	mux.HandleFunc("GET /chat/widget/v1/loader.js", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "alpha", "project", "chat", "doctor", "--since", "1h"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stale   BAD_TOKEN") || !strings.Contains(out.String(), "recent  PRINCIPAL_UNVERIFIED") {
		t.Fatalf("output = %q", out.String())
	}
}

// A doctor that exits 0 when piped is a false clean bill of health: the verdict must not depend on the
// output mode, and the bundle must still be printed alongside the non-zero exit.
func TestChatDoctorJSONStillFailsWhenACheckFails(t *testing.T) {
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("GET /api/v1/projects/alpha/chat", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"chat_enabled":{"effective":false},"chat_origins":{"effective":[]}}`))
	})
	mux.HandleFunc("GET /api/v1/projects/alpha/principals", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"kinds":{}}`)) })
	mux.HandleFunc("GET /api/v1/projects/alpha/chat/secret", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"source":"dedicated"}`)) })
	mux.HandleFunc("GET /api/v1/branding", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":{"effective":"Example"}}`))
	})
	mux.HandleFunc("GET /api/v1/projects/alpha/chat/rejects", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"rejects":[]}`)) })
	mux.HandleFunc("GET /chat/widget/v1/loader.js", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "json")
	err := run(t, e, "--project", "alpha", "project", "chat", "doctor")
	var cmdErr *commandError
	if !errors.As(err, &cmdErr) || cmdErr.name != "CHAT_DOCTOR_FAILED" || cmdErr.code == 0 {
		t.Fatalf("err = %v, want a non-zero CHAT_DOCTOR_FAILED", err)
	}
	var bundle map[string]any
	if jsonErr := json.Unmarshal(out.Bytes(), &bundle); jsonErr != nil {
		t.Fatalf("bundle not printed: %v (%q)", jsonErr, out.String())
	}
	if bundle["project"] != "alpha" {
		t.Fatalf("bundle = %#v", bundle)
	}
}

func TestChatSendLaneSetupUsesDashboardBearerMount(t *testing.T) {
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("POST /api/v1/projects/alpha/chat/dashboard/session/intent", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		if r.URL.Query().Get("intent") != "integration_setup" || r.URL.Query().Get("integration") != "clickup" {
			t.Fatalf("intent query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"session_id":"22222222-2222-2222-2222-222222222222","flavor":"setup","tier":"pro"}`))
	})
	mux.HandleFunc("POST /api/v1/projects/alpha/chat/dashboard/message", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["session_id"] != "22222222-2222-2222-2222-222222222222" {
			t.Fatalf("body = %v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Chat-Tier", "pro")
		_, _ = w.Write([]byte("data: {\"type\":\"start\",\"messageId\":\"run-setup\"}\n\ndata: {\"type\":\"text-delta\",\"delta\":\"hi\"}\n\ndata: [DONE]\n\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected %s %s", r.Method, r.URL.String()) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "alpha", "project", "chat", "send", "--lane", "setup", "--intent", "integration_setup", "--integration", "clickup", "set it up"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "session_id: 22222222-2222-2222-2222-222222222222\nrun_id: run-setup\ntier: pro\n") {
		t.Fatalf("missing final session/run IDs: %q", out.String())
	}
}

func TestChatSendLaneTenantScopeAndSessionRead(t *testing.T) {
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("POST /api/v1/projects/alpha/tenants/north/chat/dashboard/session", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["flavor"] != "data" || body["tier"] != "pro" {
			t.Fatalf("open body = %v", body)
		}
		_, _ = w.Write([]byte(`{"session_id":"33333333-3333-3333-3333-333333333333","flavor":"data"}`))
	})
	mux.HandleFunc("POST /api/v1/projects/alpha/tenants/north/chat/dashboard/message", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"start\",\"messageId\":\"run-data\"}\n\ndata: [DONE]\n\n"))
	})
	mux.HandleFunc("GET /api/v1/projects/alpha/tenants/north/chat/dashboard/session/{id}", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		_, _ = w.Write([]byte(`{"session_id":"` + r.PathValue("id") + `","messages":[]}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected %s %s", r.Method, r.URL.String()) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "alpha", "--tenant", "north", "project", "chat", "send", "--lane", "data", "--tier", "pro", "how many?"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "run_id: run-data\n") {
		t.Fatalf("output = %q", out.String())
	}
	e, out, _ = newTestEnv(t, srv, "json")
	if err := run(t, e, "--project", "alpha", "--tenant", "north", "project", "chat", "session", "33333333-3333-3333-3333-333333333333", "--lane", "data"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"33333333-3333-3333-3333-333333333333"`) {
		t.Fatalf("session output = %q", out.String())
	}
}

func TestChatSendLaneRejectsBadCombinations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected %s %s", r.Method, r.URL.String()) }))
	defer srv.Close()
	for _, args := range [][]string{
		{"project", "chat", "send", "--lane", "sideways", "x"},
		{"project", "chat", "send", "--lane", "data", "--intent", "integration_setup", "x"},
		{"project", "chat", "send", "--lane", "setup", "--tier", "ultra", "x"},
	} {
		e, _, _ := newTestEnv(t, srv, "table")
		if err := run(t, e, args...); err == nil {
			t.Fatalf("%v: expected a usage error", args)
		}
	}
}

// Replay: one session per `---` segment, --tier only on a segment's first turn, the header-driven tier
// per turn, and a reported switch between consecutive turns of one session.
func TestChatReplayRunsTurnsOnOneSessionAndReportsSwitch(t *testing.T) {
	mux := http.NewServeMux()
	chatTestProjects(mux)
	sessions := 0
	mux.HandleFunc("POST /api/v1/projects/alpha/chat/dashboard/session", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["tier"] != "standard" || body["flavor"] != "data" {
			t.Fatalf("open body = %v", body)
		}
		sessions++
		_, _ = fmt.Fprintf(w, `{"session_id":"sess-%d"}`, sessions)
	})
	tiers := []string{"standard", "pro", "pro"}
	turn := 0
	mux.HandleFunc("POST /api/v1/projects/alpha/chat/dashboard/message", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		wantSession := "sess-1"
		if turn == 2 {
			wantSession = "sess-2"
		}
		_, hasTier := body["tier"]
		if body["session_id"] != wantSession || hasTier != (turn != 1) {
			t.Fatalf("turn %d body = %v", turn+1, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Chat-Tier", tiers[turn])
		turn++
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"start\",\"messageId\":\"run-%d\"}\n\ndata: {\"type\":\"text-delta\",\"delta\":\"answer \"}\n\ndata: {\"type\":\"text-delta\",\"delta\":\"%d\\nmore\"}\n\ndata: [DONE]\n\n", turn, turn)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected %s %s", r.Method, r.URL.String()) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "script.txt")
	if err := os.WriteFile(path, []byte("# warm-up\nhow many bookings?\n\nand per week?\n---\nhello again\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "alpha", "project", "chat", "replay", path, "--lane", "data", "--tier", "standard"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"1  standard", "2  pro", "run-2", "answer 2 more", "session_id: sess-1\nsession_id: sess-2\nswitched at turn 2 (standard → pro)\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "turn 3") {
		t.Fatalf("a new session is not a switch:\n%s", got)
	}

	sessions, turn = 0, 0
	e, out, _ = newTestEnv(t, srv, "json")
	if err := run(t, e, "--project", "alpha", "project", "chat", "replay", path, "--lane", "data", "--tier", "standard"); err != nil {
		t.Fatal(err)
	}
	var rows []chatTurnResult
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("json: %v (%q)", err, out.String())
	}
	if len(rows) != 3 || rows[2].SessionID != "sess-2" || rows[1].Tier != "pro" || rows[0].Message != "how many bookings?" {
		t.Fatalf("rows = %+v", rows)
	}
}

// --brief drops the timeline but keeps the host's depth decision.
func TestBriefTraceKeepsDepth(t *testing.T) {
	body, err := briefTrace(json.RawMessage(`{"run":{"run_id":"r1","system_prompt":"x"},"events":[{"tool":"bash","status":"ok"},{"tool":"host","status":"depth","args":{"tier":"pro","source":"user"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); !strings.Contains(got, `"depth":{"tier":"pro","source":"user"}`) || strings.Contains(got, "system_prompt") {
		t.Fatalf("brief = %s", got)
	}
}

// chatDoctorServer is the healthy doctor fixture with a caller-supplied rejects body.
func chatDoctorServer(t *testing.T, rejects string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("GET /api/v1/projects/alpha/chat", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"chat_enabled":{"effective":true},"chat_origins":{"effective":["https://app.example.com"]}}`))
	})
	mux.HandleFunc("GET /api/v1/projects/alpha/principals", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"kinds":{}}`)) })
	mux.HandleFunc("GET /api/v1/projects/alpha/chat/secret", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"source":"dedicated"}`)) })
	mux.HandleFunc("GET /api/v1/branding", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":{"effective":"Example"}}`))
	})
	mux.HandleFunc("GET /api/v1/projects/alpha/chat/rejects", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(rejects)) })
	mux.HandleFunc("GET /chat/widget/v1/loader.js", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	return httptest.NewServer(mux)
}

// Near-miss findings come from a map: they must still print in a stable (sorted) order.
func TestChatDoctorNearMissFindingsAreSorted(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	var rows []string
	for _, host := range []string{"f", "e", "d", "c", "b", "a"} {
		rows = append(rows, fmt.Sprintf(`{"code":"ORIGIN_NOT_ALLOWED","origin":"https://%s.example.com","timestamp":%q}`, host, now))
	}
	srv := chatDoctorServer(t, `{"rejects":[`+strings.Join(rows, ",")+`]}`)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "json")
	if err := run(t, e, "--project", "alpha", "project", "chat", "doctor"); err != nil {
		var cmdErr *commandError
		if !errors.As(err, &cmdErr) {
			t.Fatal(err)
		}
	}
	var bundle chatDoctorJSON
	if err := json.Unmarshal(out.Bytes(), &bundle); err != nil {
		t.Fatalf("bundle: %v (%q)", err, out.String())
	}
	var got []string
	for _, f := range bundle.Findings {
		if f.Code == "ORIGIN_NEAR_MISS" {
			got = append(got, strings.Fields(f.Hint)[4])
		}
	}
	want := []string{"https://a.example.com,", "https://b.example.com,", "https://c.example.com,", "https://d.example.com,", "https://e.example.com,", "https://f.example.com,"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("near-miss order = %q, want %q", got, want)
	}
}

// `-o json` is a computed report: next to the unchanged report keys it carries every fetched server body
// verbatim under raw, so a field the doctor drops (ip_prefix) or does not know yet survives there.
func TestChatDoctorJSONCarriesRawServerBodies(t *testing.T) {
	srv := chatDoctorServer(t, `{"rejects":[{"code":"BAD_TOKEN","ip_prefix":"192.0.2.0/24","timestamp":"2026-09-01T00:00:00Z"}],"future_field":"kept"}`)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "json")
	if err := run(t, e, "--project", "alpha", "project", "chat", "doctor"); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Project string                                `json:"project"`
		Rejects []map[string]any                      `json:"rejects"`
		Raw     map[string]map[string]json.RawMessage `json:"raw"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v (%s)", err, out.String())
	}
	if report.Project != "alpha" || len(report.Rejects) != 1 || report.Rejects[0]["ip_prefix"] != nil {
		t.Fatalf("report keys changed: %s", out.String())
	}
	if string(report.Raw["chat_rejects"]["future_field"]) != `"kept"` || !strings.Contains(string(report.Raw["chat_rejects"]["rejects"]), "192.0.2.0/24") {
		t.Fatalf("raw.chat_rejects not verbatim: %s", out.String())
	}
	for _, source := range []string{"chat_settings", "principals", "chat_secret", "branding"} {
		if len(report.Raw[source]) == 0 {
			t.Fatalf("raw lacks %s: %s", source, out.String())
		}
	}
}

// A rejects shape the doctor cannot decode must fail the doctor, not read as "no recent rejects".
func TestChatDoctorUndecodableRejectsFails(t *testing.T) {
	srv := chatDoctorServer(t, `{"rejects":{"code":"ORIGIN_NOT_ALLOWED"}}`)
	defer srv.Close()
	e, _, _ := newTestEnv(t, srv, "json")
	err := run(t, e, "--project", "alpha", "project", "chat", "doctor")
	if err == nil || !strings.Contains(err.Error(), "decode chat rejects") {
		t.Fatalf("err = %v, want a decode chat rejects error", err)
	}
}

// The website-change E2E loop a script drives after `send`: wait out the preview build and the host's
// review turn, follow that turn while it runs (204 once settled), fetch its screenshot — on the
// dashboard plane (tenant-nested bearer mount) and the embed plane.
func TestChatCardWaitFollowAndAttachment(t *testing.T) {
	defer func(d time.Duration) { chatCardPollInterval = d }(chatCardPollInterval)
	chatCardPollInterval = time.Millisecond
	const base = "/api/v1/projects/alpha/tenants/north/chat/dashboard"
	states := []string{
		`{"status":"proposed","preview_state":"building","review_state":"pending"}`,
		`{"status":"proposed","preview_state":"ready","review_state":"reviewing","review_run_id":"run-review"}`,
		`{"status":"proposed","preview_state":"ready","review_state":"reviewed"}`,
	}
	polls := 0
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("GET "+base+"/session/{id}/website-changes/{card_id}", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		_, _ = w.Write([]byte(states[min(polls, len(states)-1)]))
		polls++
	})
	streams := 0
	stream := func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("run_id") != "run-review" || r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("follow %s accept %q", r.URL.Path, r.Header.Get("Accept"))
		}
		if streams++; streams > 1 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"start\",\"messageId\":\"run-review\"}\n\ndata: {\"type\":\"text-delta\",\"delta\":\"looks right\"}\n\ndata: [DONE]\n\n"))
	}
	mux.HandleFunc("GET "+base+"/session/{id}/runs/{run_id}/stream", stream)
	file := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != "s1" || r.PathValue("id") != "att-1" {
			t.Fatalf("attachment %s", r.URL.String())
		}
		if strings.HasPrefix(r.URL.Path, "/chat/v1/") && (r.Header.Get("X-RC-Embed-Origin") != "https://site.example.test" || r.URL.Query().Get("project") != "alpha") {
			t.Fatalf("embed attachment: origin %q query %s", r.Header.Get("X-RC-Embed-Origin"), r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("JPEG"))
	}
	mux.HandleFunc("GET "+base+"/attachments/{id}", file)
	mux.HandleFunc("GET /chat/v1/attachments/{id}", file)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected %s %s", r.Method, r.URL.String()) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	lane := []string{"--project", "alpha", "--tenant", "north", "project", "chat"}

	e, out, errOut := newTestEnv(t, srv, "json")
	if err := run(t, e, append(lane, "card", "s1", "wc_01", "--lane", "setup", "--wait")...); err != nil {
		t.Fatal(err)
	}
	if polls != 3 || !strings.Contains(out.String(), `"reviewed"`) || !strings.Contains(errOut.String(), "waiting: preview building\nwaiting: review reviewing\n") {
		t.Fatalf("wait: polls=%d out=%q err=%q", polls, out.String(), errOut.String())
	}

	for i, want := range []string{"looks right", "already settled"} {
		e, out, _ = newTestEnv(t, srv, "table")
		if err := run(t, e, append(lane, "follow", "s1", "run-review", "--lane", "setup")...); err != nil || !strings.Contains(out.String(), want) {
			t.Fatalf("follow #%d: err=%v out=%q", i, err, out.String())
		}
	}

	dir := t.TempDir()
	for _, args := range [][]string{
		append(lane, "attachment", "s1", "att-1", "--lane", "setup", "--file", filepath.Join(dir, "dash.jpg")),
		{"--project", "alpha", "project", "chat", "attachment", "s1", "att-1", "--token", "embed-jwt", "--origin", "https://site.example.test", "--file", filepath.Join(dir, "embed.jpg")},
	} {
		e, out, _ = newTestEnv(t, srv, "table")
		if err := run(t, e, args...); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n") // a TTY run leads with the scope banner
		path := lines[len(lines)-1]
		if b, err := os.ReadFile(path); err != nil || string(b) != "JPEG" {
			t.Fatalf("attachment %s = %q %v", path, b, err)
		}
	}
}

// Review round 1: --timeout bounds the requests themselves, a follow outlives the client's total
// request timeout, and a failed download leaves an existing file alone.
func TestChatCardWaitFollowAndAttachmentEdges(t *testing.T) {
	defer func(d time.Duration) { chatCardPollInterval = d }(chatCardPollInterval)
	chatCardPollInterval = time.Millisecond
	const base = "/api/v1/projects/alpha/chat/dashboard"
	mux := http.NewServeMux()
	chatTestProjects(mux)
	mux.HandleFunc("GET "+base+"/session/{id}/website-changes/{card_id}", func(w http.ResponseWriter, r *http.Request) {
		select { // a stalled poll
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		_, _ = w.Write([]byte(`{"status":"proposed","preview_state":"building"}`))
	})
	mux.HandleFunc("GET "+base+"/session/{id}/runs/{run_id}/stream", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"start\",\"messageId\":\"run-long\"}\n\n"))
		w.(http.Flusher).Flush()
		time.Sleep(300 * time.Millisecond) // longer than RC_HTTP_TIMEOUT below
		_, _ = w.Write([]byte("data: {\"type\":\"text-delta\",\"delta\":\"still here\"}\n\ndata: [DONE]\n\n"))
	})
	mux.HandleFunc("GET "+base+"/attachments/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"UNKNOWN_ATTACHMENT","message":"Not Found"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	lane := []string{"--project", "alpha", "project", "chat"}

	e, _, _ := newTestEnv(t, srv, "json")
	started := time.Now()
	err := run(t, e, append(lane, "card", "s1", "wc_01", "--lane", "setup", "--wait", "--timeout", "100ms")...)
	if err == nil || !strings.Contains(err.Error(), "after 100ms") || time.Since(started) > time.Second {
		t.Fatalf("stalled poll: err=%v after %s", err, time.Since(started))
	}

	t.Setenv("RC_HTTP_TIMEOUT", "100ms")
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, append(lane, "follow", "s1", "run-long", "--lane", "setup")...); err != nil || !strings.Contains(out.String(), "still here") {
		t.Fatalf("long follow: err=%v out=%q", err, out.String())
	}

	existing := filepath.Join(t.TempDir(), "shot.jpg")
	if err := os.WriteFile(existing, []byte("KEEP"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, _, _ = newTestEnv(t, srv, "table")
	if err := run(t, e, append(lane, "attachment", "s1", "att-x", "--lane", "setup", "--file", existing)...); err == nil {
		t.Fatal("404 download succeeded")
	}
	if b, err := os.ReadFile(existing); err != nil || string(b) != "KEEP" {
		t.Fatalf("existing file after failed download = %q %v", b, err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(existing), ".*part")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestCardIDArgStripsFramePrefix(t *testing.T) {
	const id = "0c24867a-f29e-45d2-b79e-88ec7b15f076"
	for in, want := range map[string]string{"bc_" + id: id, " mem_" + id: id, id: id, "wc_abc123": "wc_abc123"} {
		if got := cardIDArg(in); got != want {
			t.Errorf("cardIDArg(%q) = %q, want %q", in, got, want)
		}
	}
}
