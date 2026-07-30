// Package note implements nt's markdown notes with light YAML frontmatter
// (SPEC §5). Notes are one file each under notes/, so they need no shared lock:
// creation and edits are atomic single-file writes.
package note

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/ulid"
)

// Note is a parsed markdown note.
type Note struct {
	Path     string
	Rel      string // path relative to notes/ (slash-separated), set by List
	ID       string
	Title    string
	Tags     []string
	Aliases  []string
	Source   string
	Created  string
	Updated  string // stamped when nt rewrites the note (retag, --field)
	Archived bool   // frontmatter archived: true — retired from active views, still on disk
	Favorite bool   // frontmatter favorite: true — starred/pinned for quick access
	// SupersededBy is the id of the note that replaces this one (frontmatter
	// superseded_by:). A superseded note is dropped from active views like an
	// archived one — so a resume sees the single canonical decision, not both
	// forks — while the pointer preserves the trail.
	SupersededBy string
	// ValidFrom/ValidUntil are optional frontmatter dates (YYYY-MM-DD or
	// RFC3339) marking a fact's validity window — Zep's idea, without a
	// temporal graph database: a note can be "true as of" or "true until"
	// without a full supersede. Unlike Archived/SupersededBy (which retire a
	// note from active views entirely), an expired note stays visible — just
	// down-ranked in nt_recall and flagged, so an agent still finds it but
	// knows to doubt it. See Expired/NotYetValid.
	ValidFrom  string
	ValidUntil string
	// HalfLife/Reviewed drive relevance decay (memory-dynamics spec §3): a
	// note with a half_life fades smoothly in recall ranking and index tiering
	// as it ages — never hidden, floored, and flagged — the smooth complement
	// to ValidUntil's hard cliff. Reviewed is the decay clock's reset point
	// ("re-confirmed true on this date", set by `nt touch`); age is measured
	// from the latest of reviewed/updated/created/mtime. Both unset ⇒ no
	// decay, byte-identical behavior. See decay.go.
	HalfLife string // duration: Nd / Nw / Nm / Ny, or "none" (explicit opt-out)
	Reviewed string // YYYY-MM-DD or RFC3339
	// ModTime is the note file's last-modified time, set by List/Load/cache. It
	// captures every change — including edits made outside nt (Obsidian, git) that
	// never touch the `updated:` frontmatter — so "changed since T" is reliable.
	ModTime time.Time
	Body    string
	Extra   []string // raw frontmatter lines for keys nt doesn't model (preserved verbatim)
	// DupKeys lists frontmatter keys that appeared more than once in a form
	// where repetition isn't a supported authoring pattern (tags:/aliases: in
	// their list/plural form) — the injection signature that let PR #177's bug
	// attach a forged second tags: line and promote a note into memory-core,
	// nt's always-loaded tier. Load doesn't reject on this (one tampered file
	// must not make the whole store unloadable) or merge the duplicate (that's
	// what made the original bug invisible for months); it keeps the FIRST
	// occurrence and records the anomaly here so `nt doctor` can surface it.
	// Empty for every well-formed note, including ones using the legitimate
	// repeated singular `tag:` convention — see parseFrontmatter.
	DupKeys []string
}

// parseValidityDate parses a frontmatter validity date in either YYYY-MM-DD or
// full RFC3339 form. ok=false for "", unparseable, or malformed input — always
// treated as "no constraint" by Expired/NotYetValid, never as an error.
func parseValidityDate(s string) (t time.Time, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// Expired reports whether n's valid_until has passed as of now. false when
// valid_until is unset or unparseable — absence is never treated as expired.
func (n *Note) Expired(now time.Time) bool {
	t, ok := parseValidityDate(n.ValidUntil)
	return ok && now.After(t)
}

// NotYetValid reports whether n's valid_from is still in the future as of now.
// false when valid_from is unset or unparseable.
func (n *Note) NotYetValid(now time.Time) bool {
	t, ok := parseValidityDate(n.ValidFrom)
	return ok && now.Before(t)
}

// Slug derives a filesystem-safe slug from a title, falling back to a timestamp
// when the title yields nothing usable (à la nb).
func Slug(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	// Cap the slug well under the filesystem's 255-byte filename limit (slug is
	// pure ASCII, so len == bytes); cut at a word boundary so capped slugs stay
	// readable. claimPath's -N suffixing disambiguates any resulting collisions.
	const maxSlug = 120
	if len(slug) > maxSlug {
		cut := slug[:maxSlug]
		if j := strings.LastIndexByte(cut, '-'); j > 0 {
			cut = cut[:j]
		}
		slug = strings.Trim(cut, "-")
	}
	if slug == "" {
		slug = time.Now().Format("2006-01-02-150405")
	}
	return slug
}

// SplitPathTitle interprets the path-style filing shorthand shared by the CLI
// and web note-create surfaces: "work/Auth design" files "Auth design" under
// work/. The slash counts as filing syntax ONLY when everything before the
// last slash is path-like (non-empty, no whitespace) and a non-empty title
// remains after it. Otherwise the slash is prose — "…valid at .claude/x/",
// "docs live under apps/web" — and the whole string stays the title; returning
// folder "" tells the caller no filing choice was made.
func SplitPathTitle(raw string) (folder, title string) {
	title = strings.TrimSpace(raw)
	i := strings.LastIndex(title, "/")
	if i < 0 {
		return "", title
	}
	prefix, rest := title[:i], strings.TrimSpace(title[i+1:])
	if prefix == "" || rest == "" || strings.ContainsFunc(prefix, unicode.IsSpace) {
		return "", title
	}
	return prefix, rest
}

// TaskNoteFolder is the subfolder under notes/ where a task's "body" notes live
// (auto-split paragraph captures and explicit task detail). The double-underscore
// name is deliberately "reserved-looking" so it won't collide with a plain
// "tasks" folder a user might keep for their own hand-curated notes; grouping
// these machine-created notes here keeps them out of a human's folders — like the
// "journal" folder does for daily notes.
const TaskNoteFolder = "__tasks__"

// Kind is the canonical folder + tag pair for one note class (see Kinds).
type Kind struct {
	Folder string // canonical folder under notes/
	Tag    string // canonical tag stamped on the note
}

// Kinds maps a note class (CLI --kind / MCP kind:) to its canonical folder +
// tag, so multi-agent stores converge on one taxonomy instead of inventing
// folders. Shared by the CLI and MCP surfaces — keep them identical. "memory"
// is the always-loaded core-memory layer (the OpenCode plugin injects it):
// files under memory/ carrying the memory-core tag, NOT a bare "memory" tag.
var Kinds = map[string]Kind{
	"lesson":   {Folder: "lessons", Tag: "lesson"},
	"decision": {Folder: "decisions", Tag: "decision"},
	"ref":      {Folder: "ref", Tag: "ref"},
	"rule":     {Folder: "rules", Tag: "rule"},
	"memory":   {Folder: "memory", Tag: "memory-core"},
}

// Create builds and writes a new note, returning it. The body is prefixed with
// an H1 title when it doesn't already start with one.
// Create writes a new note. folder, when non-empty, is a slash-separated
// subfolder under notes/ (e.g. "work" or "work/auth"); it is created as needed.
// The filename is slugged from the title; the body and frontmatter are written
// by Save.
func Create(s *store.Store, title, body string, tags []string, source, folder string) (*Note, error) {
	n := &Note{
		ID:      ulid.New(),
		Title:   title,
		Tags:    tags,
		Source:  source,
		Created: time.Now().Format(time.RFC3339),
		Body:    body,
	}
	notesDir := s.NotesDir()
	dir := notesDir
	if clean, err := cleanFolder(folder); err != nil {
		return nil, err
	} else if clean != "" {
		dir = filepath.Clean(filepath.Join(dir, filepath.FromSlash(clean)))
		// Containment barrier before the mkdir sink: the folder is untrusted (web
		// input), so assert the resolved dir stays under notes/ before creating it.
		if !contained(notesDir, dir) {
			return nil, fmt.Errorf("refusing to create folder outside notes/: %q", dir)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create folder: %w", err)
		}
	}
	p, err := claimPath(notesDir, dir, Slug(title))
	if err != nil {
		return nil, err
	}
	n.Path = p
	// Re-assert containment in this function before Save writes the file. claimPath
	// already guards its own create sink, but a CodeQL barrier guard only sanitizes
	// sinks in the same control-flow graph — the value flowing on into Save →
	// store.WriteAtomic (os.Rename) lives in *this* CFG, so the barrier has to be
	// here too, exactly as the pre-refactor withinDir check was.
	if !contained(notesDir, n.Path) {
		return nil, fmt.Errorf("refusing to write note outside notes/: %q", n.Path)
	}
	if err := n.Save(); err != nil {
		return nil, err
	}
	return n, nil
}

// cleanFolder normalizes a slash-separated subfolder and refuses paths that
// would escape notes/ (absolute, or containing "." / ".." segments).
func cleanFolder(folder string) (string, error) {
	if filepath.IsAbs(folder) {
		return "", fmt.Errorf("folder must be relative to notes/: %q", folder)
	}
	f := strings.Trim(filepath.ToSlash(strings.TrimSpace(folder)), "/")
	if f == "" {
		return "", nil
	}
	for _, seg := range strings.Split(f, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("invalid folder %q", folder)
		}
	}
	return f, nil
}

// contained reports whether target resolves inside root (no "../" escape) — the
// path-traversal barrier guarding every filesystem sink that consumes untrusted
// (title/folder/web) input. It uses the filepath.Rel + ".." idiom that CodeQL's
// TaintedPath query recognizes as a sanitizing guard (a strings.HasPrefix compare
// is not recognized), so placing it in a sink's own function sanitizes that sink.
func contained(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// claimPath atomically reserves a free note path for a slug under the notes root.
// It O_EXCL-creates an empty placeholder so two concurrent processes can never pick
// — and then clobber — the same filename (a stat-then-write race that silently loses
// one write). Save later rewrites the placeholder we now own. On a collision it
// advances the "-N" suffix, exactly like the old uniquePath.
func claimPath(root, dir, slug string) (string, error) {
	for i := 1; i < 1_000_000; i++ {
		name := slug + ".md"
		if i > 1 {
			name = fmt.Sprintf("%s-%d.md", slug, i)
		}
		p := filepath.Clean(filepath.Join(dir, name))
		// Containment barrier before the create sink: the resolved path must stay
		// under notes/. Slug strips titles to [a-z0-9-] and cleanFolder rejects
		// ".."/absolute folders, so this can't fail in practice — but asserting it
		// here defeats any path traversal from untrusted (e.g. web) input.
		if !contained(root, p) {
			return "", fmt.Errorf("refusing note path outside notes/: %q", p)
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return p, nil
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("claim note path: %w", err)
		}
		// exists — another note (or a racing writer) has this slug; try the next.
	}
	return "", fmt.Errorf("too many slug collisions for %q", slug)
}

// invalidFrontmatterLine reports whether line — one physical row of the
// frontmatter block, already formatted as "key: value" (or a raw Extra
// line) — can corrupt the file when written. Load finds the frontmatter's
// end by scanning for the substring "\n---", so an embedded \n/\r puts
// attacker-controlled text on its own line, where it either becomes a
// forged extra key (the memory-core tag-injection CVE this guards against)
// or, if that new line starts with "---", truncates the block early. The
// latter is also reachable with NO embedded newline: via --field's
// attacker-chosen key (e.g. --field ---=x, giving a row that starts with
// "---"), or via a value of "---" itself — harmless today since it's
// preceded by "key: " on the same physical line, but rejected anyway as
// defense in depth against any future writer that emits the value alone.
func invalidFrontmatterLine(line string) bool {
	if strings.ContainsAny(line, "\n\r") {
		return true
	}
	if strings.HasPrefix(strings.TrimSpace(line), fmDelim) {
		return true
	}
	if _, val, ok := strings.Cut(line, ": "); ok && strings.HasPrefix(strings.TrimSpace(val), fmDelim) {
		return true
	}
	return false
}

// Save writes the note atomically with frontmatter. Returns an error — and
// writes nothing — if any frontmatter value would corrupt the block (see
// invalidFrontmatterLine); we reject rather than silently strip/escape the
// offending bytes, because a value that trips this came from a caller (or
// whatever untrusted text it captured) trying to smuggle extra frontmatter,
// and silently sanitizing it would hide that instead of surfacing it.
func (n *Note) Save() error {
	var b strings.Builder
	b.WriteString("---\n")
	// line validates and appends one "key: value\n" frontmatter row.
	line := func(key, val string) error {
		row := key + ": " + val
		if invalidFrontmatterLine(row) {
			return fmt.Errorf("note: %s contains a newline or a %q line — refusing to write corrupt frontmatter", key, fmDelim)
		}
		b.WriteString(row)
		b.WriteByte('\n')
		return nil
	}
	if n.ID != "" {
		if err := line("id", n.ID); err != nil {
			return err
		}
	}
	if len(n.Tags) > 0 {
		if err := line("tags", "["+strings.Join(n.Tags, ", ")+"]"); err != nil {
			return err
		}
	}
	if len(n.Aliases) > 0 {
		if err := line("aliases", "["+strings.Join(n.Aliases, ", ")+"]"); err != nil {
			return err
		}
	}
	// Persist the title when the body's own H1 would otherwise win on reload
	// (Load's precedence is frontmatter title → alias → first body heading). Without
	// this, `nt note --title X` with a body starting "# Y" silently becomes titled
	// "Y" — breaking the index and get-by-title. Only emitted when it actually
	// differs, so Obsidian notes whose H1 == title stay frontmatter-clean.
	if n.Title != "" {
		if bh := firstHeading(n.Body); bh != "" && bh != n.Title {
			if err := line("title", n.Title); err != nil {
				return err
			}
		}
	}
	if n.Source != "" {
		if err := line("source", n.Source); err != nil {
			return err
		}
	}
	if n.Created != "" {
		if err := line("created", n.Created); err != nil {
			return err
		}
	}
	if n.Updated != "" {
		if err := line("updated", n.Updated); err != nil {
			return err
		}
	}
	if n.Archived {
		b.WriteString("archived: true\n")
	}
	if n.Favorite {
		b.WriteString("favorite: true\n")
	}
	if n.SupersededBy != "" {
		if err := line("superseded_by", n.SupersededBy); err != nil {
			return err
		}
	}
	if n.ValidFrom != "" {
		if err := line("valid_from", n.ValidFrom); err != nil {
			return err
		}
	}
	if n.ValidUntil != "" {
		if err := line("valid_until", n.ValidUntil); err != nil {
			return err
		}
	}
	if n.HalfLife != "" {
		if err := line("half_life", n.HalfLife); err != nil {
			return err
		}
	}
	if n.Reviewed != "" {
		if err := line("reviewed", n.Reviewed); err != nil {
			return err
		}
	}
	for _, extra := range n.Extra { // unknown keys (Obsidian properties, --field, description:), verbatim
		if invalidFrontmatterLine(extra) {
			return fmt.Errorf("note: frontmatter field %q contains a newline or a %q line — refusing to write corrupt frontmatter", extra, fmDelim)
		}
		b.WriteString(extra)
		b.WriteByte('\n')
	}
	b.WriteString("---\n\n")
	body := n.Body
	if n.Title != "" && !strings.HasPrefix(strings.TrimSpace(body), "#") {
		fmt.Fprintf(&b, "# %s\n\n", n.Title)
	}
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	return store.WriteAtomic(n.Path, []byte(b.String()), 0o644)
}

// StaleNoteError reports that a note changed on disk since the caller last
// saw it — SaveIfUnchanged's refusal.
type StaleNoteError struct {
	Path string
}

func (e *StaleNoteError) Error() string {
	return fmt.Sprintf("%s changed on disk since it was loaded", e.Path)
}

// MTimeToken returns a stable, comparable string for n's ModTime — the
// optimistic-concurrency token callers round-trip through SaveIfUnchanged.
// "" when ModTime is unset (e.g. a just-created note that hasn't been loaded
// from disk).
func (n *Note) MTimeToken() string {
	if n.ModTime.IsZero() {
		return ""
	}
	return n.ModTime.UTC().Format(time.RFC3339Nano)
}

// SaveIfUnchanged writes n atomically, refusing with a *StaleNoteError when
// expect is non-empty and doesn't match the file's CURRENT on-disk mtime (a
// fresh os.Stat, not n's own possibly-stale ModTime) — "only save if the file
// still looks like what I last saw." expect == "" (the default — most callers
// have no token to offer) skips the check entirely, matching plain Save.
//
// This is best-effort optimistic concurrency, not a hard guarantee: it
// catches the dominant real case (an agent saving a copy it loaded some time
// ago while another writer touched the file meanwhile), the same way the web
// layer's If-Match does — except the web layer serializes through one
// process, while the CLI/MCP path is multi-process, so a residual
// stat-then-rename race window remains between the check and the write.
func (n *Note) SaveIfUnchanged(expect string) error {
	if expect != "" {
		if info, err := os.Stat(n.Path); err == nil {
			if cur := info.ModTime().UTC().Format(time.RFC3339Nano); cur != expect {
				return &StaleNoteError{Path: n.Path}
			}
		}
		// A missing file isn't "stale" in the sense this guards against — Save's
		// own WriteAtomic recreates it, matching plain Save's existing behavior
		// for a note deleted then re-saved.
	}
	return n.Save()
}

var fmDelim = "---"

// Load parses a note file (frontmatter + body). Unknown frontmatter keys are
// ignored, not an error.
func Load(path string) (*Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n := &Note{Path: path}
	if info, serr := os.Stat(path); serr == nil {
		n.ModTime = info.ModTime()
	}
	text := string(data)
	if strings.HasPrefix(text, fmDelim+"\n") {
		rest := text[len(fmDelim)+1:]
		if end := strings.Index(rest, "\n"+fmDelim); end >= 0 {
			parseFrontmatter(rest[:end], n)
			body := rest[end+len(fmDelim)+1:]
			n.Body = strings.TrimPrefix(body, "\n")
		}
	} else {
		n.Body = text
	}
	// Title precedence: frontmatter title (set during parse) → first alias →
	// first H1 → humanized filename. Covers Obsidian notes that have no H1.
	if n.Title == "" && len(n.Aliases) > 0 {
		n.Title = n.Aliases[0]
	}
	if n.Title == "" {
		n.Title = firstHeading(n.Body)
	}
	if n.Title == "" {
		n.Title = humanizeFilename(path)
	}
	return n, nil
}

var listRe = regexp.MustCompile(`\[(.*)\]`)

// parseFrontmatter reads the keys nt understands from a YAML-ish frontmatter
// block. Beyond nt's own output it tolerates Obsidian conventions: block-list
// and bare-comma tags/aliases, a title:/aliases: key, and the deprecated
// singular tag:. Unknown keys are ignored.
//
// tags:/aliases: are list-form keys with no legitimate reason to repeat — a
// second occurrence is dropped (not merged) into DupKeys instead, since
// merging is what let an injected duplicate escalate a note silently. This is
// distinct from the deprecated singular tag:, which legitimately repeats
// (one tag per line, an Obsidian convention) and always accumulates.
func parseFrontmatter(fm string, n *Note) {
	lines := strings.Split(fm, "\n")
	seenTags, seenAliases := false, false
	for i := 0; i < len(lines); i++ {
		ci := strings.IndexByte(lines[i], ':')
		if ci < 0 {
			continue
		}
		key := strings.TrimSpace(lines[i][:ci])
		val := strings.TrimSpace(lines[i][ci+1:])
		switch key {
		case "id":
			n.ID = unquote(val)
		case "source":
			n.Source = unquote(val)
		case "created":
			n.Created = unquote(val)
		case "updated":
			n.Updated = unquote(val)
		case "archived":
			n.Archived = unquote(val) == "true"
		case "favorite":
			n.Favorite = unquote(val) == "true"
		case "superseded_by":
			n.SupersededBy = unquote(val)
		case "valid_from":
			n.ValidFrom = unquote(val)
		case "valid_until":
			n.ValidUntil = unquote(val)
		case "half_life":
			n.HalfLife = unquote(val)
		case "reviewed":
			n.Reviewed = unquote(val)
		case "title":
			if v := unquote(val); v != "" {
				n.Title = v
			}
		case "tag": // deprecated singular form — repeated lines legitimately accumulate
			n.Tags = appendClean(n.Tags, val)
		case "tags":
			vals := parseList(val, lines, &i) // advances i past any block-list lines regardless
			if seenTags {
				n.DupKeys = appendUniqueKey(n.DupKeys, "tags")
				continue
			}
			n.Tags = append(n.Tags, vals...)
			seenTags = true
		case "alias", "aliases":
			vals := parseList(val, lines, &i)
			if seenAliases {
				n.DupKeys = appendUniqueKey(n.DupKeys, "aliases")
				continue
			}
			n.Aliases = append(n.Aliases, vals...)
			seenAliases = true
		default:
			// Unknown key (e.g. an Obsidian property): preserve it verbatim,
			// including any block-list continuation lines, so a later rewrite
			// (retag, --field, updated stamp) never clobbers it.
			n.Extra = append(n.Extra, lines[i])
			if val == "" {
				for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "- ") {
					n.Extra = append(n.Extra, lines[i+1])
					i++
				}
			}
		}
	}
}

// parseList reads a YAML list value in any of the forms Obsidian/nt emit: inline
// flow `[a, b]`, bare comma `a, b`, or a block list of following `- item` lines
// (consuming them, advancing *i).
func parseList(val string, lines []string, i *int) []string {
	var out []string
	switch {
	case strings.HasPrefix(val, "["):
		if m := listRe.FindStringSubmatch(val); m != nil {
			for _, t := range strings.Split(m[1], ",") {
				out = appendClean(out, t)
			}
		}
	case val != "":
		for _, t := range strings.Split(val, ",") {
			out = appendClean(out, t)
		}
	default: // block list: indented "- item" lines on following rows
		for *i+1 < len(lines) {
			t := strings.TrimSpace(lines[*i+1])
			if !strings.HasPrefix(t, "- ") {
				break
			}
			out = appendClean(out, t[2:])
			*i++
		}
	}
	return out
}

// appendClean trims quotes/whitespace and a stray leading '#', dropping empties.
func appendClean(out []string, s string) []string {
	s = strings.TrimPrefix(unquote(strings.TrimSpace(s)), "#")
	if s = strings.TrimSpace(s); s != "" {
		out = append(out, s)
	}
	return out
}

// appendUniqueKey records a duplicate frontmatter key name once, even if the
// key repeats 3+ times — DupKeys is a set of offending key names, not a tally.
func appendUniqueKey(out []string, key string) []string {
	for _, k := range out {
		if k == key {
			return out
		}
	}
	return append(out, key)
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func firstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return ""
}

// humanizeFilename turns a note filename into a readable title fallback.
func humanizeFilename(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".md")
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

// List loads all notes in the store's notes directory, recursing into
// subfolders so an Obsidian-style nested vault works. Hidden dirs (.obsidian/,
// .trash/, .git/) and non-.md files are skipped. Each note's Rel (path relative
// to notes/, slash-separated) is set for link resolution; results are sorted by
// Rel for deterministic ordering.
// Active drops archived notes — the working set, for views/search that should
// hide retired notes. List itself returns everything (archived included) so
// link-rewriting and the archived view still see them.
// Description returns the note's one-line summary for index/stub views: its
// `description:` frontmatter if set (kept in Extra, since nt doesn't model the
// key), else the first non-heading body line. Clamped to a single line ≤max chars.
// This is the "one-sentence summary" granularity of progressive disclosure — what
// an agent reads to decide whether to open the full note.
func (n *Note) Description(max int) string {
	for _, line := range n.Extra {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), "description") {
			if d := strings.TrimSpace(strings.Trim(strings.TrimSpace(v), `"'`)); d != "" {
				return clampLine(d, max)
			}
		}
	}
	for _, raw := range strings.Split(n.Body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return clampLine(line, max)
	}
	return ""
}

func clampLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace to one line
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max-1]) + "…"
	}
	return s
}

// Project returns the note's `project:` frontmatter value ("" when unset). The
// key isn't modeled (it rides in Extra, preserved verbatim); this accessor is
// how recall's same-project boost reads a note's declared project membership —
// alongside its tags and folder path.
func (n *Note) Project() string {
	for _, line := range n.Extra {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "project") {
			return strings.TrimSpace(unquote(strings.TrimSpace(v)))
		}
	}
	return ""
}

// Reserved reports whether a note lives in a machine-managed folder that isn't
// part of the human/agent knowledge base — currently notes/__tasks__/, where
// nt files the detail bodies of split tasks. These are reachable by id/link but
// are kept out of the KB catalog (nt index) and search so they don't pollute it.
func (n *Note) Reserved() bool { return strings.HasPrefix(n.Rel, "__tasks__/") }

// FindSimilar returns active, non-reserved notes that look like near-duplicates of
// a note with the given title, tags and project — a guard against concurrent
// forks (two agents independently recording the same decision). A candidate
// matches when it has the identical slug, OR it shares a tag AND its title
// word-set overlaps heavily (Jaccard ≥ 0.5) — UNLESS the pair is a "parallel
// sibling": each note carries a distinguishing tag the other lacks that also
// appears in its own title ("taskly repo map" @taskly vs "ratelim repo map"
// @ratelim). Multi-project stores legitimately hold same-shaped notes per
// project; the project tag in the title is how the pair self-identifies as
// distinct. This is a cheap heuristic, not semantic dedup.
//
// project is folded into the shared-tag test alongside tags: `--project` stores
// it as a separate `project:` frontmatter field, not a tag, but for a note whose
// only tag is a class marker (lesson/rule/memory-core — stripped by
// structuralTag below) the tag set would otherwise be empty and the Jaccard
// branch could never fire, silently disabling the guard for the most common
// kind of note. project isn't written to disk as a tag; it only joins the
// in-memory set this function compares on.
func FindSimilar(notes []*Note, title string, tags []string, project string) []*Note {
	want := titleTokens(title)
	slug := Slug(title)
	tagset := similarityTags(tags, project)
	var out []*Note
	for _, n := range notes {
		if n.Archived || n.SupersededBy != "" || n.Reserved() {
			continue
		}
		sharedTag := false
		for t := range similarityTags(n.Tags, n.Project()) {
			if tagset[t] {
				sharedTag = true
				break
			}
		}
		if Slug(n.Title) == slug {
			out = append(out, n)
			continue
		}
		if sharedTag && jaccard(want, titleTokens(n.Title)) >= 0.5 && !parallelSiblings(title, tags, project, n) {
			out = append(out, n)
		}
	}
	return out
}

// similarityTags is the tag set FindSimilar/parallelSiblings compare on: a
// note's ordinary tags plus its project (if any), with class-marker tags
// (lesson/rule/memory-core) stripped since they'd otherwise make every note of
// that class look topically related. project stands in for a tag here because
// that's the role it plays for dedup purposes — "which topic/scope does this
// note belong to" — even though it lives in a separate frontmatter field.
// project is trimmed and lowercased before folding in: it's compared exactly
// (unlike TAG case, which is pre-existing and left alone), and the write path
// (commands.go) trims before storing while callers here don't always trim
// before calling — without normalizing, "wtc" and " WTC " would silently
// stop pairing, matching recall.go's projectTokens normalization.
func similarityTags(tags []string, project string) map[string]bool {
	out := map[string]bool{}
	for _, t := range tags {
		if structuralTag[t] {
			continue // class markers (lesson/rule/memory-core) aren't a topical match
		}
		out[t] = true
	}
	if p := strings.ToLower(strings.TrimSpace(project)); p != "" {
		out[p] = true
	}
	return out
}

// parallelSiblings reports whether the (title, tags, project) triple and note n
// look like the same kind of note for two DIFFERENT projects: each side has a
// tag (or project) the other lacks, and that word appears in its own title but
// not the other's. Such pairs share their title shape ("X repo map" / "Y repo
// map") yet are deliberately distinct — refusing them as duplicates was the
// dedup guard's worst failure mode in multi-project field use.
func parallelSiblings(title string, tags []string, project string, n *Note) bool {
	aTokens, bTokens := titleTokens(title), titleTokens(n.Title)
	aTags, bTags := tagsWithProject(tags, project), tagsWithProject(n.Tags, n.Project())
	distinguishes := func(ownTags []string, otherTags []string, ownTokens, otherTokens map[string]bool) bool {
		other := map[string]bool{}
		for _, t := range otherTags {
			other[t] = true
		}
		for _, t := range ownTags {
			tok := normTitleToken(t)
			if !other[t] && ownTokens[tok] && !otherTokens[tok] {
				return true
			}
		}
		return false
	}
	return distinguishes(aTags, bTags, aTokens, bTokens) && distinguishes(bTags, aTags, bTokens, aTokens)
}

// tagsWithProject appends project to tags (as a plain string, not stripped of
// structural tags — parallelSiblings works over the raw tag/word vocabulary),
// leaving tags untouched when project is unset. project is trimmed and
// lowercased for the same reason as similarityTags above.
func tagsWithProject(tags []string, project string) []string {
	p := strings.ToLower(strings.TrimSpace(project))
	if p == "" {
		return tags
	}
	return append(append([]string{}, tags...), p)
}

// TitleOverlap is the word-set Jaccard (0..1) of two titles, ignoring short and
// stopword tokens — the similarity heuristic behind duplicate detection, exported
// so task-side dedup can reuse the exact same notion nt uses for notes.
func TitleOverlap(a, b string) float64 { return jaccard(titleTokens(a), titleTokens(b)) }

// structuralTag marks tags that classify a note (which memory layer it belongs
// to) rather than describe its topic. They're excluded from similarity's
// shared-tag test so that every note tagged `lesson` isn't treated as topically
// related to every other lesson — which would collapse dedup to pure title
// overlap across the whole corpus and mis-flag distinct lessons as duplicates
// (the failure mode that silently dropped parallel agents' captures).
var structuralTag = map[string]bool{"lesson": true, "rule": true, "memory-core": true}

var titleStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "over": true, "via": true, "with": true,
	"vs": true, "not": true, "use": true, "using": true, "into": true, "from": true,
}

// normTitleToken folds a tag into the token form titleTokens produces, so a tag
// like "Rate-Lim" can be looked up against title words ("ratelim").
func normTitleToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func titleTokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if len(w) >= 3 && !titleStopwords[w] {
			out[w] = true
		}
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// Active drops notes retired from the working set: archived notes and superseded
// ones (a superseded note has a newer canonical version, so views show only the
// current decision, not both forks).
func Active(ns []*Note) []*Note {
	out := ns[:0:0]
	for _, n := range ns {
		if !n.Archived && n.SupersededBy == "" {
			out = append(out, n)
		}
	}
	return out
}

func List(s *store.Store) ([]*Note, error) {
	dir := s.NotesDir()
	var out []*Note
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // tolerate unreadable entries rather than aborting the walk
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		n, e := Load(path)
		if e != nil {
			return nil
		}
		if rel, e := filepath.Rel(dir, path); e == nil {
			n.Rel = filepath.ToSlash(rel)
		}
		out = append(out, n)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}
