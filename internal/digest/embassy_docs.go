package digest

import "strings"

// EmbassyErrorsBase is the ONE place the integrator error index lives: every diagnostic the CLI prints
// (chat doctor, action doctor/probe/exec) links a code to its anchor there, so a moved doc is a one-line
// change and two commands can never drift to different URLs.
const EmbassyErrorsBase = "https://github.com/replypen/replypen-embassy/blob/main/docs/integrator/errors.md#"

// EmbassyDocsFor returns the docs URL for an error code (anchors are the lower-cased code).
func EmbassyDocsFor(code string) string {
	return EmbassyErrorsBase + strings.ToLower(code)
}
