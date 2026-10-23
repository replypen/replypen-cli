package cli

import (
	"strings"
	"testing"
)

// --wait polls with the POST's kb_commit as since and prints the changed article files.
func TestKnowledgeSyncRunWaitPrintsChangedArticles(t *testing.T) {
	fastKnowledgeSyncPoll(t)
	srv := stubServer(t)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "alpha", "project", "knowledge", "sync", "run", "--wait"); err != nil {
		t.Fatal(err)
	}
	want := "kb sync job 7 completed · health ok · 61 articles · 1 changed\n  bundle/tips/track-form.md\n"
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
}

// A sync that completes while its source could not be read must fail the command (scripts gate on it).
func TestKnowledgeSyncRunWaitFailsOnUnreadableSource(t *testing.T) {
	fastKnowledgeSyncPoll(t)
	srv := stubServer(t)
	defer srv.Close()
	e, _, _ := newTestEnv(t, srv, "table")
	err := run(t, e, "--project", "alpha", "--tenant", "acme", "project", "knowledge", "sync", "run", "--wait")
	if err == nil || !strings.Contains(err.Error(), "fetch failed") {
		t.Fatalf("err = %v, want the unreadable-source failure", err)
	}
}

func fastKnowledgeSyncPoll(t *testing.T) {
	orig := knowledgeSyncPollInterval
	knowledgeSyncPollInterval = 0
	t.Cleanup(func() { knowledgeSyncPollInterval = orig })
}

// A KB source change queues a sync server-side; its X-Replypen-Notice reaches stderr, stdout stays the bag.
func TestKnowledgeSyncSetPrintsServerNotice(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()
	e, _, errb := newTestEnv(t, srv, "table")
	if err := run(t, e, "project", "knowledge", "sync", "set", "provider=intercom", "base_url=https://acme.intercom.io"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errb.String(), "kb sync job 9 queued") {
		t.Fatalf("stderr = %q, want the server notice", errb.String())
	}
}
