package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mailThreadFixture = `{"run_id":"41b8785a-0000-0000-0000-000000000001","mailbox_id":"mbx-1","mailbox_address":"camp@example.test","provider":"google","external_thread_id":"1a0c","fetched_at":"2026-11-04T08:00:00Z","messages":[` +
	`{"external_message_id":"m1","direction":"inbound","date":"2026-09-22T09:00:00Z","from":"parent@example.test","to":["camp@example.test"],"subject":"Insurance","body":"Where are the papers?"},` +
	`{"external_message_id":"m2","direction":"outbound","author_kind":"human_admin","date":"2026-09-23T10:00:00Z","from":"camp@example.test","to":["parent@example.test"],"subject":"Re: Insurance","body":"Forwarded to the insurer.","attachments":[{"filename":"papers.pdf","mime_type":"application/pdf","size_bytes":12}]}]}`

// TestMailboxThreadWritesPrivateMarkdown: the text view writes the whole thread to a 0600 Markdown file
// and keeps the bodies off stdout; --run hits the run route with the tenant selector.
func TestMailboxThreadWritesPrivateMarkdown(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/projects" { // --project validation
			_, _ = w.Write([]byte(`{"projects":[{"id":"aaaaaaaa-0000-0000-0000-000000000001","name":"alpha"}]}`))
			return
		}
		gotPath = r.URL.RequestURI()
		_, _ = w.Write([]byte(mailThreadFixture))
	}))
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	outDir := t.TempDir()
	if err := run(t, e, "--out-dir", outDir, "--project", "alpha", "--tenant", "lbv", "project", "mailbox", "thread", "--run", "#41b8785a"); err != nil {
		t.Fatalf("thread: %v", err)
	}
	if gotPath != "/api/v1/runs/41b8785a/mail-thread?project=alpha&tenant=lbv" {
		t.Fatalf("request = %s", gotPath)
	}
	if strings.Contains(out.String(), "Forwarded to the insurer") || !strings.Contains(out.String(), "outbound") {
		t.Fatalf("stdout must be a body-free index:\n%s", out.String())
	}
	path := filepath.Join(outDir, "mail-thread-41b8785a", "thread.md")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("thread file %s: %v mode %v", path, err, info)
	}
	md, _ := os.ReadFile(path)
	for _, want := range []string{"## 2 · outbound · 2026-09-23T10:00:00Z", "- to: parent@example.test", "- attachments: papers.pdf", "Forwarded to the insurer."} {
		if !strings.Contains(string(md), want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}

	e, _, _ = newTestEnv(t, srv, "table")
	if err := run(t, e, "--out-dir", outDir, "--project", "alpha", "--tenant", "lbv", "project", "mailbox", "thread", "mbx-1", "1a0c"); err != nil {
		t.Fatalf("mailbox form: %v", err)
	}
	if gotPath != "/api/v1/projects/alpha/mailboxes/mbx-1/threads/1a0c?tenant=lbv" {
		t.Fatalf("mailbox request = %s", gotPath)
	}
	// Piped/-o json: bodies to a 0600 file, stdout a manifest; --raw-output alone prints them inline.
	e, out, _ = newTestEnv(t, srv, "json")
	if err := run(t, e, "--out-dir", outDir, "--project", "alpha", "project", "mailbox", "thread", "--run", "41b8785a"); err != nil {
		t.Fatalf("json form: %v", err)
	}
	if strings.Contains(out.String(), "Forwarded to the insurer") || !strings.Contains(out.String(), "thread.json") {
		t.Fatalf("json stdout must be a body-free manifest:\n%s", out.String())
	}
	e, out, _ = newTestEnv(t, srv, "json")
	if err := run(t, e, "--raw-output", "--project", "alpha", "project", "mailbox", "thread", "--run", "41b8785a"); err != nil || !strings.Contains(out.String(), "Forwarded to the insurer") {
		t.Fatalf("raw-output = %v:\n%s", err, out.String())
	}
	if err := run(t, e, "--project", "alpha", "project", "mailbox", "thread", "--run", "x", "mbx-1", "1a0c"); err == nil {
		t.Fatal("--run plus positional args must be rejected")
	}
}
