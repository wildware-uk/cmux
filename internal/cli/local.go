package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/wildware-uk/cmux/internal/discover"
)

// cmdPanes lists the panes cmux can see.
func cmdPanes(r *run) error {
	ctx := context.Background()

	var panes []discover.Pane
	var err error
	if r.opts.allPanes {
		panes, err = r.finder.All(ctx)
	} else {
		panes, err = r.finder.Find(ctx)
	}
	if err != nil {
		return err
	}

	if len(panes) == 0 {
		if r.opts.allPanes {
			fmt.Fprintln(r.out, "no tmux panes found")
		} else {
			fmt.Fprintln(r.out, "no Claude Code panes found (try --all-panes)")
		}
		return nil
	}

	w := tabwriter.NewWriter(r.out, 0, 0, 2, ' ', 0)
	if r.opts.allPanes {
		fmt.Fprintln(w, "#\tPANE\tLOCATION\tCOMMAND\tCLAUDE\t")
	} else {
		fmt.Fprintln(w, "#\tPANE\tLOCATION\tCOMMAND\t")
	}
	for i, p := range panes {
		here := ""
		if p.ID == r.currentPane {
			here = "  (this pane)"
		}
		if r.opts.allPanes {
			claude := "no"
			if p.IsClaude {
				claude = "yes"
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", i, p.ID, p.Location(), p.Command, claude, here)
		} else {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", i, p.ID, p.Location(), p.Command, here)
		}
	}
	return w.Flush()
}

// cmdStatus answers "where am I, and what would a bare cmux compact hit?".
func cmdStatus(r *run) error {
	ctx := context.Background()

	if r.currentPane == "" && r.opts.to == "" {
		return discover.NotInTmuxError{}
	}

	w := tabwriter.NewWriter(r.out, 0, 0, 1, ' ', 0)
	defer w.Flush()

	version, err := r.client.Version(ctx)
	if err != nil {
		return err
	}

	target, resolveErr := r.finder.Resolve(ctx, r.opts.to)

	if r.currentPane != "" {
		fmt.Fprintf(w, "pane:\t%s\n", r.currentPane)
	} else {
		fmt.Fprintf(w, "pane:\tnot inside tmux\n")
	}

	if resolveErr != nil {
		fmt.Fprintf(w, "detected:\tunknown (%v)\n", resolveErr)
		fmt.Fprintf(w, "target:\tunresolved\n")
	} else {
		detected := "not a Claude Code pane"
		if target.IsClaude {
			detected = "claude"
		}
		fmt.Fprintf(w, "detected:\t%s\n", detected)
		how := "default: current pane"
		if r.opts.to != "" {
			how = "from --to " + r.opts.to
		}
		fmt.Fprintf(w, "target:\t%s (%s) [%s]\n", target.ID, target.Location(), how)
	}
	fmt.Fprintf(w, "tmux:\t%s\n", version)
	fmt.Fprintf(w, "cmux:\t%s\n", r.build.Version)
	return nil
}
