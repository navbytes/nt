package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/navbytes/nt/internal/note"
)

func TestHookBashErrorRecallIgnoresNonBashEvents(t *testing.T) {
	msg, fire := hookBashErrorRecall(nil, []byte(`{"tool_name":"TodoWrite","tool_input":{"todos":[]}}`))
	if fire {
		t.Errorf("a non-Bash event must never fire: msg=%q", msg)
	}
}

func TestHookBashErrorRecallIgnoresSuccessfulCommands(t *testing.T) {
	payload := `{"tool_name":"Bash","tool_input":{"command":"go test ./..."},"tool_response":{"is_error":false,"stdout":"ok"}}`
	if _, fire := hookBashErrorRecall(nil, []byte(payload)); fire {
		t.Error("a successful command must never fire")
	}
}

func TestHookBashErrorRecallIgnoresMalformedOrAmbiguousPayloads(t *testing.T) {
	for _, payload := range []string{
		`not json`,
		`{"tool_name":"Bash"}`, // no tool_response at all — can't tell, don't fire
		`{"tool_name":"Bash","tool_input":{"command":""}}`,       // empty command
		`{"tool_name":"Bash","tool_response":{"is_error":true}}`, // failed but no command to search on
	} {
		if _, fire := hookBashErrorRecall(nil, []byte(payload)); fire {
			t.Errorf("payload %q must not fire", payload)
		}
	}
}

func TestHookBashErrorRecallFindsAMatchingLesson(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Singleflight the token refresh",
		"--body", "Two parallel refresh calls double-spend the refresh token.",
		"--lesson", "--description", "when refreshing an OAuth token concurrently, single-flight the call")

	e, ok := engine()
	if !ok {
		t.Fatal("open engine")
	}
	notes := note.Active(mustNotes(e))

	payload := `{"tool_name":"Bash","tool_input":{"command":"curl -X POST /oauth/refresh & curl -X POST /oauth/refresh"},` +
		`"tool_response":{"exit_code":1,"stderr":"refresh token already used"}}`
	msg, fire := hookBashErrorRecall(notes, []byte(payload))
	if !fire {
		t.Fatal("expected a matching lesson to fire")
	}
	if !strings.Contains(msg, "Singleflight the token refresh") {
		t.Errorf("message missing the matched lesson title: %q", msg)
	}
}

func TestHookBashErrorRecallExitCodeAltFieldNameFires(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Always vendor before offline builds",
		"--body", "go build fails offline without a vendor dir.",
		"--lesson", "--description", "when building offline, vendor dependencies first")

	e, ok := engine()
	if !ok {
		t.Fatal("open engine")
	}
	notes := note.Active(mustNotes(e))

	// camelCase "exitCode" instead of "exit_code" — the defensive multi-key check.
	payload := `{"tool_name":"Bash","tool_input":{"command":"go build ./... offline"},"tool_response":{"exitCode":1,"stderr":"vendor dir missing"}}`
	msg, fire := hookBashErrorRecall(notes, []byte(payload))
	if !fire {
		t.Fatal("expected exitCode (camelCase) to be recognized as a failure signal")
	}
	if !strings.Contains(msg, "Always vendor before offline builds") {
		t.Errorf("message missing the matched lesson title: %q", msg)
	}
}

// End-to-end: `nt hook` on a failing Bash PostToolUse event exits 2 with the
// lesson on stderr — Claude Code's block+reason contract — while a passing
// TodoWrite event (the existing mirror) still exits 0.
func TestCmdHookBashFailureExitsTwoWithStderr(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "Always run gofmt before commit",
		"--body", "CI rejects unformatted Go files.",
		"--lesson", "--description", "when committing Go code, run gofmt first")

	payload := `{"tool_name":"Bash","tool_input":{"command":"git commit -m wip"},"tool_response":{"is_error":true,"stderr":"gofmt: files not formatted"}}`
	out, errOut, code := runHookWithStdin(payload)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stdout=%q stderr=%q", code, out, errOut)
	}
	if !strings.Contains(errOut, "Always run gofmt before commit") {
		t.Errorf("stderr missing the lesson: %q", errOut)
	}
}

func TestCmdHookTodoWriteStillExitsZero(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	payload := `{"session_id":"s1","tool_name":"TodoWrite","tool_input":{"todos":[{"content":"ship it","status":"pending"}]}}`
	out, errOut, code := runHookWithStdin(payload)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, out, errOut)
	}
}

// runHookWithStdin runs `nt hook` with stdin set to payload, capturing stdout,
// stderr, and the exit code.
func runHookWithStdin(payload string) (stdout, stderr string, code int) {
	oldStdin, oldStdout, oldStderr := os.Stdin, os.Stdout, os.Stderr
	inR, inW, _ := os.Pipe()
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdin, os.Stdout, os.Stderr = inR, outW, errW

	go func() {
		_, _ = inW.WriteString(payload)
		_ = inW.Close()
	}()

	code = Run([]string{"hook"})

	outW.Close()
	errW.Close()
	os.Stdin, os.Stdout, os.Stderr = oldStdin, oldStdout, oldStderr

	outBytes, _ := io.ReadAll(outR)
	errBytes, _ := io.ReadAll(errR)
	return string(outBytes), string(errBytes), code
}
