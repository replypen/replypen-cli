package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFleetValueRoutesAndTotals(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/projects" {
			_, _ = w.Write([]byte(`{"projects":[{"id":"p-1","name":"kampadmin"}]}`))
			return
		}
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"project":"kampadmin","tenant":"lbv","since":"2026-09-01","until":"2026-12-01","items":[
			{"month":"2026-09","tenant":"lbv","mailbox":"info@lbv.test","runs":380,"drafted_runs":363,"continued_runs":305,"provider":"google","sent_verbatim":5,"sent_minor_edit":2,"sent_major_edit":58,"sent_rewritten":159,"auto_sent":0,"drafted_threads":262,"continued_threads":244},
			{"month":"2026-10","tenant":"lbv","mailbox":"info@lbv.test","runs":290,"drafted_runs":276,"continued_runs":248,"provider":"google","sent_verbatim":0,"sent_minor_edit":6,"sent_major_edit":45,"sent_rewritten":197,"auto_sent":0,"drafted_threads":218,"continued_threads":207}]}`))
	}))
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "kampadmin", "--tenant", "lbv", "fleet", "value", "--months", "3"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/admin/projects/kampadmin/value-stats" || !strings.Contains(gotQuery, "months=3") || !strings.Contains(gotQuery, "tenant=lbv") {
		t.Fatalf("request %s?%s", gotPath, gotQuery)
	}
	if err := run(t, e, "--project", "kampadmin", "fleet", "value", "--months", "2", "--since", "2026-09-01"); err == nil {
		t.Fatal("--months with --since must be refused")
	}
	// 553/639 = 87% runs, 451/480 = 94% threads.
	for _, want := range []string{"TOTAL", "639", "553", "87%", "480", "451", "94%"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output lacks %q:\n%s", want, out.String())
		}
	}
}
