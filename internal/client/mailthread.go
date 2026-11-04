package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// MailThread is GET /api/v1/runs/{id}/mail-thread and GET /api/v1/projects/{project}/mailboxes/{id}/threads/{thread}:
// one conversation re-read live from the customer's mailbox (read-only, not stored server-side), so it
// survives ReplyPen's body retention. Field names mirror the server verbatim.
type MailThread struct {
	RunID            string              `json:"run_id,omitempty"`
	MailboxID        string              `json:"mailbox_id"`
	MailboxAddress   string              `json:"mailbox_address"`
	Provider         string              `json:"provider"`
	ExternalThreadID string              `json:"external_thread_id"`
	FetchedAt        string              `json:"fetched_at"`
	Messages         []MailThreadMessage `json:"messages"`
}

type MailThreadMessage struct {
	ExternalMessageID string                 `json:"external_message_id"`
	Direction         string                 `json:"direction"` // inbound | outbound
	AuthorKind        string                 `json:"author_kind,omitempty"`
	Date              string                 `json:"date"`
	From              string                 `json:"from"`
	To                []string               `json:"to,omitempty"`
	Cc                []string               `json:"cc,omitempty"`
	Subject           string                 `json:"subject"`
	Body              string                 `json:"body"`
	Attachments       []MailThreadAttachment `json:"attachments,omitempty"`
}

type MailThreadAttachment struct {
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type,omitempty"`
	SizeBytes int    `json:"size_bytes,omitempty"`
	Inline    bool   `json:"inline,omitempty"`
}

// RunMailThread fetches the run's email thread live from its mailbox.
func (c *Client) RunMailThread(ctx context.Context, runID, project, tenant string) (*MailThread, json.RawMessage, error) {
	return c.mailThread(ctx, collectionScopePath("/api/v1/runs/"+url.PathEscape(runID)+"/mail-thread", project, tenant))
}

// MailboxThread fetches one provider thread id live from a mailbox.
func (c *Client) MailboxThread(ctx context.Context, mailboxID, threadID, project, tenant string) (*MailThread, json.RawMessage, error) {
	path := watchedProjectPath(project, "/mailboxes/"+url.PathEscape(mailboxID)+"/threads/"+url.PathEscape(threadID))
	if path == "" {
		return nil, nil, &APIError{Status: http.StatusBadRequest, Code: "PROJECT_REQUIRED", Message: "fetching a mailbox thread requires a project scope"}
	}
	return c.mailThread(ctx, collectionScopePath(path, "", tenant))
}

func (c *Client) mailThread(ctx context.Context, path string) (*MailThread, json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, nil, err
	}
	var out MailThread
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, fmt.Errorf("decode mail thread: %w", err)
	}
	return &out, raw, nil
}
