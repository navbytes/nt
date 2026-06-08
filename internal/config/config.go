// Package config loads nt's optional config file, $NT_DIR/config.toml. It is a
// deliberately tiny TOML subset — [section] headers and `key = value` lines with
// string / int / bool values — so nt keeps its zero-runtime-dependency, plain-
// text ethos (no TOML library). A missing or partial file is never an error;
// unset keys keep their zero value and callers fall back to built-in defaults.
//
// Recognized keys (all optional):
//
//	[defaults]
//	priority    = "high"        # default --pri for `nt add`
//	source      = "cli"         # default --source
//	agenda_days = 7             # default horizon for `nt agenda`
//	editor      = "nvim"        # overrides $EDITOR for `nt edit`
//
//	[web]
//	port = 4321                 # default `nt web` port
//	host = "127.0.0.1"          # default bind address
//
//	[tui]
//	theme = "auto"              # auto | light | dark
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the parsed config. Zero values mean "unset" — callers OR them with
// their existing defaults.
type Config struct {
	DefaultPriority string // [defaults] priority
	DefaultSource   string // [defaults] source
	AgendaDays      int    // [defaults] agenda_days
	Editor          string // [defaults] editor
	WebPort         int    // [web] port
	WebHost         string // [web] host
	TUITheme        string // [tui] theme (auto|light|dark)
}

// FileName is the config file's basename within $NT_DIR.
const FileName = "config.toml"

// Load reads $NT_DIR/config.toml. A missing file returns an empty Config and no
// error. Parse errors in individual lines are skipped, not fatal — a typo in one
// key never stops nt from running.
func Load(dir string) (*Config, error) {
	c := &Config{}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	section := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		c.set(section, strings.TrimSpace(key), stripInlineComment(strings.TrimSpace(val)))
	}
	return c, nil
}

func (c *Config) set(section, key, val string) {
	switch section + "." + key {
	case "defaults.priority":
		c.DefaultPriority = unquote(val)
	case "defaults.source":
		c.DefaultSource = unquote(val)
	case "defaults.agenda_days":
		c.AgendaDays = atoi(val)
	case "defaults.editor":
		c.Editor = unquote(val)
	case "web.port":
		c.WebPort = atoi(val)
	case "web.host":
		c.WebHost = unquote(val)
	case "tui.theme":
		c.TUITheme = unquote(val)
	}
}

// stripInlineComment drops a trailing ` # comment` from a value, but not a '#'
// inside a quoted string.
func stripInlineComment(val string) string {
	if val == "" || val[0] == '"' || val[0] == '\'' {
		return val // quoted: leave as-is (unquote handles it)
	}
	if i := strings.IndexByte(val, '#'); i >= 0 {
		return strings.TrimSpace(val[:i])
	}
	return val
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
