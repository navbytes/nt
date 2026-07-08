package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/recall"
)

// bashHookPayload is the subset of a Claude Code PostToolUse hook event this
// cares about when tool_name is "Bash". Failure detection checks several
// plausible field names defensively — Claude Code's exact JSON shape for a
// failed Bash result isn't pinned down here, so this mirrors the
// multi-key-checking pattern the OpenCode/Pi integrations already use for the
// identical problem (their failedExit()/failed()): treat "can't tell" as "no
// signal, don't fire" rather than guessing.
type bashHookPayload struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
	ToolResponse struct {
		IsError     *bool  `json:"is_error"`
		IsErrorAlt  *bool  `json:"isError"`
		ExitCode    *int   `json:"exit_code"`
		ExitCodeAlt *int   `json:"exitCode"`
		Stderr      string `json:"stderr"`
		Stdout      string `json:"stdout"`
	} `json:"tool_response"`
}

func (p bashHookPayload) failed() bool {
	switch {
	case p.ToolResponse.IsError != nil:
		return *p.ToolResponse.IsError
	case p.ToolResponse.IsErrorAlt != nil:
		return *p.ToolResponse.IsErrorAlt
	case p.ToolResponse.ExitCode != nil:
		return *p.ToolResponse.ExitCode != 0
	case p.ToolResponse.ExitCodeAlt != nil:
		return *p.ToolResponse.ExitCodeAlt != 0
	default:
		return false
	}
}

// hookBashErrorRecall implements the same error-triggered lesson recall the
// OpenCode/Pi integrations already run (see integrations/{opencode,pi}'s
// recallLessons): on a failed Bash tool call, it searches recorded lessons for
// the command + error tail. If it finds anything, the caller feeds it back to
// Claude via the hook's block+reason contract (exit 2, reason on stderr) — a
// mistake summons its own antidote on the next turn instead of relying on the
// agent remembering to run `nt recall`. Returns ("", false) for anything that
// isn't a failed Bash call, or when nothing relevant is on record (recall's
// own precision floor — it returns nothing rather than a weak guess).
func hookBashErrorRecall(notes []*note.Note, data []byte) (message string, fire bool) {
	var p bashHookPayload
	if err := json.Unmarshal(data, &p); err != nil || p.ToolName != "Bash" || !p.failed() {
		return "", false
	}
	command := strings.TrimSpace(p.ToolInput.Command)
	if command == "" {
		return "", false
	}
	firstLine := command
	if i := strings.IndexByte(firstLine, '\n'); i >= 0 {
		firstLine = firstLine[:i]
	}
	firstLine = clampASCII(firstLine, 120)

	errText := p.ToolResponse.Stderr
	if strings.TrimSpace(errText) == "" {
		errText = p.ToolResponse.Stdout
	}
	tail := clampASCII(lastNonEmptyLines(errText, 2), 160)

	query := strings.TrimSpace(firstLine + " " + tail)
	if query == "" {
		return "", false
	}

	var lessons []*note.Note
	for _, n := range notes {
		if contains(n.Tags, recall.LessonTag) {
			lessons = append(lessons, n)
		}
	}
	results := recall.RankProject(lessons, query, 3, "")
	if len(results) == 0 {
		return "", false
	}

	var b strings.Builder
	b.WriteString("nt: recorded lessons from past sessions may explain this failure — check them BEFORE retrying:\n")
	for _, r := range results {
		desc := strings.TrimSpace(r.Note.Description(160))
		fmt.Fprintf(&b, "- %s %s", r.Note.ID, r.Note.Title)
		if desc != "" {
			fmt.Fprintf(&b, " — %s", desc)
		}
		b.WriteByte('\n')
	}
	b.WriteString("Fetch details with `nt show <id>`.")
	return b.String(), true
}

func clampASCII(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

// lastNonEmptyLines returns the last n non-empty lines of s, joined by a
// space — a compact error tail for the recall query.
func lastNonEmptyLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	var kept []string
	for i := len(lines) - 1; i >= 0 && len(kept) < n; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			kept = append([]string{l}, kept...)
		}
	}
	return strings.Join(kept, " ")
}
