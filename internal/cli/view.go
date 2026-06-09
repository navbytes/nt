package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/navbytes/nt/internal/store"
	"github.com/navbytes/nt/internal/view"
)

// cmdView manages saved smart views — named bundles of `nt list` filter flags
// (T4). They persist in $NT_DIR/views.json via the shared internal/view package,
// so a saved view filters and sorts exactly as the equivalent flags would.
//
//	nt view save <name> [list flags]   capture a filter under <name>
//	nt view <name> [--json]            run the saved view (alias: nt view recall <name>)
//	nt view list                       list saved views and their flags
//	nt view rm <name>                  delete a saved view
func cmdView(args []string) int {
	if len(args) == 0 {
		return viewList()
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "save":
		return viewSave(rest)
	case "list", "ls":
		return viewList()
	case "rm", "delete":
		return viewRemove(rest)
	case "recall", "show":
		return viewRecall(rest)
	default:
		// `nt view <name>` is shorthand for `nt view recall <name>`.
		return viewRecall(args)
	}
}

// viewSave captures the given `nt list` flags under a name. The first positional
// token is the name; the rest are the same filter flags `nt list` accepts.
func viewSave(args []string) int {
	flagTokens, pos := splitArgs(args, map[string]bool{"all": true, "show-blocked": true, "tree": true})
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "nt: view save needs a name, e.g. `nt view save week --sort due`")
		return 2
	}
	name := pos[0]
	if err := view.ValidName(name); err != nil {
		return fail(err)
	}

	fs := flag.NewFlagSet("view save", flag.ContinueOnError)
	status := fs.String("status", "", "open|doing|blocked|done")
	tag := fs.String("tag", "", "filter by tag")
	project := fs.String("project", "", "filter by project")
	sortBy := fs.String("sort", "", "urgency|due|created")
	all := fs.Bool("all", false, "include done tasks")
	showBlocked := fs.Bool("show-blocked", false, "include dependency-blocked tasks")
	tree := fs.Bool("tree", false, "render as a parent/child tree")
	if err := fs.Parse(flagTokens); err != nil {
		return 2
	}
	spec := view.Spec{
		Status:      *status,
		Tag:         *tag,
		Project:     *project,
		Sort:        *sortBy,
		All:         *all,
		ShowBlocked: *showBlocked,
		Tree:        *tree,
	}

	dir, err := store.ResolveDir()
	if err != nil {
		return fail(err)
	}
	views, err := view.Load(dir)
	if err != nil {
		return fail(err)
	}
	_, existed := views[name]
	views[name] = spec
	if err := view.Save(dir, views); err != nil {
		return fail(err)
	}
	verb := "Saved"
	if existed {
		verb = "Updated"
	}
	fmt.Printf("%s view %q → nt list %s\n", verb, name, spec.Summary())
	return 0
}

// viewRecall runs the task list for a saved view. The first positional is the
// view name; --json is honored as a recall-time output choice.
func viewRecall(args []string) int {
	fs := flag.NewFlagSet("view recall", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	flagTokens, pos := splitArgs(args, map[string]bool{"json": true})
	if err := fs.Parse(flagTokens); err != nil {
		return 2
	}
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "nt: view recall needs a name (try `nt view list`)")
		return 2
	}
	name := pos[0]

	dir, err := store.ResolveDir()
	if err != nil {
		return fail(err)
	}
	views, err := view.Load(dir)
	if err != nil {
		return fail(err)
	}
	spec, ok := views[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "nt: no saved view %q (try `nt view list`)\n", name)
		return 1
	}
	return runList(spec, *asJSON)
}

// viewList prints saved views aligned with their flag summaries.
func viewList() int {
	dir, err := store.ResolveDir()
	if err != nil {
		return fail(err)
	}
	views, err := view.Load(dir)
	if err != nil {
		return fail(err)
	}
	if len(views) == 0 {
		fmt.Println("no saved views — create one with `nt view save <name> [list flags]`")
		return 0
	}
	names := view.Names(views)
	width := 0
	for _, n := range names {
		if len(n) > width {
			width = len(n)
		}
	}
	for _, n := range names {
		fmt.Printf("%-*s  %s\n", width, n, views[n].Summary())
	}
	return 0
}

// viewRemove deletes a saved view by name.
func viewRemove(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "nt: view rm needs a name")
		return 2
	}
	name := args[0]
	dir, err := store.ResolveDir()
	if err != nil {
		return fail(err)
	}
	views, err := view.Load(dir)
	if err != nil {
		return fail(err)
	}
	if _, ok := views[name]; !ok {
		fmt.Fprintf(os.Stderr, "nt: no saved view %q\n", name)
		return 1
	}
	delete(views, name)
	if err := view.Save(dir, views); err != nil {
		return fail(err)
	}
	fmt.Printf("Deleted view %q\n", name)
	return 0
}
