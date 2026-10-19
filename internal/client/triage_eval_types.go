package client

// TriageEvalRequest mirrors replypen internal/triageeval.Request.
type TriageEvalRequest struct {
	BrainRef string   `json:"brain_ref,omitempty"`
	CasesRef string   `json:"cases_ref,omitempty"`
	Mailbox  string   `json:"mailbox,omitempty"`
	Only     []string `json:"only,omitempty"`
	Repeat   int      `json:"repeat,omitempty"`
}

// TriageEvalReport mirrors replypen internal/triageeval.Report: raw per-case attempts, scored here.
type TriageEvalReport struct {
	Brain      TriageEvalRevision     `json:"brain"`
	Cases      TriageEvalCases        `json:"cases"`
	Policy     TriageEvalPolicy       `json:"policy"`
	Model      TriageEvalModel        `json:"model"`
	HostBuild  string                 `json:"host_build,omitempty"`
	PromptHash string                 `json:"prompt_hash"`
	Repeat     int                    `json:"repeat"`
	NotCovered []string               `json:"not_covered"`
	Results    []TriageEvalCaseResult `json:"results"`
}

type TriageEvalRevision struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type TriageEvalCases struct {
	Ref   string `json:"ref"`
	SHA   string `json:"sha"`
	Dir   string `json:"dir"`
	Hash  string `json:"hash"`
	Count int    `json:"count"`
}

type TriageEvalPolicy struct {
	Scope          string                 `json:"scope"`
	Tenant         string                 `json:"tenant,omitempty"`
	Mailbox        string                 `json:"mailbox,omitempty"`
	Guidance       []TriageEvalScopedHash `json:"guidance"`
	GuidanceHash   string                 `json:"guidance_hash"`
	RulesHash      string                 `json:"rules_hash"`
	RuleCount      int                    `json:"rule_count"`
	BrainFiles     []string               `json:"brain_files"`
	BrainHash      string                 `json:"brain_hash"`
	Language       string                 `json:"language"`
	EmailTypesGate bool                   `json:"email_types_gate"`
}

type TriageEvalScopedHash struct {
	Scope string `json:"scope"`
	Hash  string `json:"hash"`
}

type TriageEvalModel struct {
	Name        string `json:"name"`
	Fallback    string `json:"fallback,omitempty"`
	Fingerprint string `json:"fingerprint"`
}

type TriageEvalCaseResult struct {
	ID             string              `json:"id"`
	Path           string              `json:"path"`
	Status         string              `json:"status"`
	Tags           []string            `json:"tags,omitempty"`
	Expect         TriageEvalExpect    `json:"expect"`
	Why            string              `json:"why"`
	Messages       int                 `json:"messages"`
	CurrentMessage string              `json:"current_message"`
	Attempts       []TriageEvalAttempt `json:"attempts"`
}

type TriageEvalExpect struct {
	Process          *bool `json:"process"`
	NeedsAction      *bool `json:"needs_action"`
	NeedsPatientText *bool `json:"needs_patient_text"`
}

type TriageEvalAttempt struct {
	ShouldProcess  bool    `json:"should_process"`
	DecisionSource string  `json:"decision_source"`
	Category       string  `json:"category"`
	Reason         string  `json:"reason"`
	Confidence     float64 `json:"confidence,omitempty"`
	EmailType      string  `json:"email_type,omitempty"`
	EmailTypeGated bool    `json:"email_type_gated,omitempty"`
	FailedOpen     bool    `json:"failed_open,omitempty"`
}
