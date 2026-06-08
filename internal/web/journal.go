package web

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/navbytes/nt/internal/web/apitypes"
)

// journalFolder is the subfolder under notes/ where daily notes live. Each daily
// note is journal/YYYY-MM-DD.md — an ordinary note (so it gets backlinks, graph
// membership, search) whose slug is the date.
const journalFolder = "journal"

// apiJournal returns the daily-notes index: today's date plus every existing
// daily note (date + handle), newest first. The SPA's /journal route uses it to
// show the current day, offer day navigation, and list past entries.
func (s *Server) apiJournal(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	prefix := journalFolder + "/"
	days := make([]apitypes.JournalDay, 0)
	for _, n := range snap.notes {
		if !strings.HasPrefix(n.Rel, prefix) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(n.Rel, prefix), ".md")
		if !isISODate(name) {
			continue // only YYYY-MM-DD notes are daily notes
		}
		days = append(days, apitypes.JournalDay{Date: name, Handle: noteHandle(n)})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date > days[j].Date }) // newest first
	writeJSON(w, apitypes.JournalResponse{
		Today:  time.Now().Format("2006-01-02"),
		Folder: journalFolder,
		Days:   days,
	})
}

// isISODate reports whether s is a strict YYYY-MM-DD date.
func isISODate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil && len(s) == 10
}
