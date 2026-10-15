package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rootcause-org/rootcause-cli/internal/render"
)

// chatReplayCmd replays a scripted conversation through the same send path as `send`, one turn at a
// time, so depth routing (Quick/Deeper) and its switches can be checked over a whole session.
func chatReplayCmd(e *env) *cobra.Command {
	var token, origin, sessionID string
	var lane chatLane
	cmd := &cobra.Command{
		Use:   "replay <file>",
		Short: "Replay a file of user messages as one chat session and tabulate tier, time and answer per turn",
		Long: "One user message per non-empty line; lines starting with # are skipped and a line `---` starts a " +
			"fresh session for the lines after it. Turns run sequentially, each waiting for its answer. " +
			"--tier is the initial pick, sent only on a session's first turn; later turns let the host route. " +
			"`-o json` prints the array of turns.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := lane.validate(); err != nil {
				return err
			}
			if sessionID != "" && lane.intent != "" {
				return fmt.Errorf("--intent opens a new conversation; drop --session")
			}
			script, err := readReplayScript(args[0])
			if err != nil {
				return err
			}
			plane, c, err := resolveChatPlane(e, lane, token, origin)
			if err != nil {
				return err
			}
			var turns []chatTurnResult
			var failed error
			session, first := sessionID, true
			n := 0
			for _, line := range script {
				if line == replaySessionBreak {
					session, first = "", true
					continue
				}
				n++
				tier := ""
				if first {
					tier = lane.tier
				}
				turn, err := sendChatTurn(e, c, plane, session, tier, []string{line}, nil, io.Discard)
				turn.Turn, turn.Message = n, line
				if turn.SessionID == "" {
					failed = fmt.Errorf("turn %d: %w", n, err)
					break
				}
				turns = append(turns, turn)
				if err != nil && failed == nil {
					failed = fmt.Errorf("turn %d: %w", n, err)
				}
				if !e.jsonOut() {
					_, _ = fmt.Fprintf(e.err, "[%d] %s %.1fs %s\n", n, dashCLI(turn.Tier), turn.Secs, dashCLI(turn.RunID))
				}
				session, first = turn.SessionID, false
			}
			if e.jsonOut() {
				if turns == nil {
					turns = []chatTurnResult{}
				}
				raw, err := json.Marshal(turns)
				if err != nil {
					return err
				}
				if err := render.JSON(e.out, raw); err != nil {
					return err
				}
				return failed
			}
			render.ChatReplay(e.out, turns)
			return failed
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "embed chat token (default: RC_CHAT_TOKEN)")
	cmd.Flags().StringVar(&origin, "origin", "", "embedding-page origin (default: token origin claim)")
	cmd.Flags().StringVar(&sessionID, "session", "", "continue this existing session (until the first ---)")
	lane.flags(cmd, true)
	return cmd
}

const replaySessionBreak = "---"

// readReplayScript returns the messages in order, with session breaks kept as replaySessionBreak.
func readReplayScript(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	messages := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line != replaySessionBreak {
			messages++
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if messages == 0 {
		return nil, fmt.Errorf("%s has no messages", path)
	}
	return out, nil
}

func dashCLI(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
