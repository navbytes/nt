# nt hero demo — render with:  vhs docs/demo.tape   (→ docs/demo.gif)
# Requires charmbracelet/vhs (+ ttyd + ffmpeg). Uses a throwaway $NT_DIR so it
# never touches a real store. Trim the Sleeps after a first render to taste.

Output docs/demo.gif

Set Shell "bash"
Set FontSize 18
Set Width 1200
Set Height 720
Set Padding 24
Set Theme "TokyoNight"
Set TypingSpeed 55ms

# Fresh, empty store — hidden setup.
Hide
Type "export NT_DIR=$(mktemp -d) && clear" Enter
Show
Sleep 600ms

Type "nt add 'fix auth bug' --pri high --due today --tag backend" Enter
Sleep 1s
Type "nt add 'write migration' --due fri --project api" Enter
Sleep 1s
Type "nt note 'JWT expiry' --tag auth" Enter
Sleep 1200ms

# What should I do next?
Type "nt ready" Enter
Sleep 2s

# Read it back — the AI-memory loop.
Type "nt recall" Enter
Sleep 2s

# The terminal UI.
Type "nt" Enter
Sleep 2500ms
Type "q"
Sleep 800ms
