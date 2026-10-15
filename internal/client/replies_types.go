package client

// TenantReplyList is GET /api/v1/projects/{project}/tenants/{slug}/replies: one item per answered turn.
// Field names mirror the server's replyItem exactly.
type TenantReplyList struct {
	Items      []TenantReply `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type TenantReply struct {
	RunID          string               `json:"run_id"`
	DraftID        string               `json:"draft_id"`
	ThreadID       string               `json:"thread_id"`
	DraftStatus    string               `json:"draft_status"`
	CreatedAt      string               `json:"created_at"`
	Subject        string               `json:"subject"`
	Shadow         bool                 `json:"shadow"`
	AutoSent       bool                 `json:"auto_sent"`
	Trigger        *TenantReplyTrigger  `json:"trigger"`
	Draft          TenantReplyDraft     `json:"draft"`
	Sent           *TenantReplySent     `json:"sent"`
	Feedback       *TenantReplyFeedback `json:"feedback"`
	BodiesScrubbed bool                 `json:"bodies_scrubbed"`
}

type TenantReplyTrigger struct {
	MessageID   string `json:"message_id"`
	From        string `json:"from"`
	FromAddress string `json:"from_address"`
	FromName    string `json:"from_name"`
	Date        string `json:"date"`
	Body        string `json:"body"`
}

type TenantReplyDraft struct {
	BodyMarkdown string `json:"body_markdown"`
}

type TenantReplySent struct {
	MessageID string `json:"message_id"`
	Date      string `json:"date"`
	Body      string `json:"body"`
	Edited    bool   `json:"edited"`
}

// Comment is absent (nil) for a caller below the server's review-content tier.
type TenantReplyFeedback struct {
	Score        *int    `json:"score"`
	Comment      *string `json:"comment,omitempty"`
	ScoreSetAt   string  `json:"score_set_at,omitempty"`
	CommentSetAt string  `json:"comment_set_at,omitempty"`
}
