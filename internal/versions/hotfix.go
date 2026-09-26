package versions

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

func (w *Workflows) CurrentVersion(ctx context.Context) (string, error) {
	o, e := w.run.Run(ctx, "gitversion", "-showvariable", "MajorMinorPatch")
	if e != nil {
		return "", e
	}
	v := normalizeVersion(strings.TrimSpace(o))
	if !semverRE.MatchString(v) {
		return "", fmt.Errorf("gitversion returned invalid MajorMinorPatch %q", v)
	}
	return v, nil
}
func (w *Workflows) HotfixStart(ctx context.Context, version string) error {
	// Fetch exactly once before choosing a support line. Version calculation must
	// happen only after its chosen base is checked out and proven equal to origin.
	if _, err := w.git(ctx, "fetch", w.remote, "--prune"); err != nil {
		return err
	}
	base, err := w.hotfixStartBase(ctx, version)
	if err != nil {
		return err
	}
	if err = w.checkoutFetchedBase(ctx, base); err != nil {
		return err
	}
	if version == "" {
		v, e := w.CurrentVersion(ctx)
		if e != nil {
			return e
		}
		version, e = bump(v, "patch")
		if e != nil {
			return e
		}
	}
	version = normalizeVersion(version)
	if err = w.ensureSafeRef(version); err != nil {
		return err
	}
	_, err = w.git(ctx, "checkout", "-b", "hotfix/"+version, "refs/heads/"+base)
	return err
}

func (w *Workflows) hotfixStartBase(ctx context.Context, version string) (string, error) {
	main, err := w.Main(ctx)
	if err != nil || !w.hasDevelop() {
		return main, err
	}
	if version != "" {
		return w.hotfixTargetFromFetched(ctx, normalizeVersion(version))
	}
	// Without an explicit version, the only unambiguous support-line signal is
	// the current support branch. Otherwise calculate the next patch from main.
	current, err := w.Current(ctx)
	if err != nil {
		return "", err
	}
	if _, _, support := supportLine(current); support {
		return current, nil
	}
	return main, nil
}

// hotfixBranch resolves an explicit version to canonical hotfix/*, but keeps
// the complete current branch for omitted names so fix/* cannot be redirected
// to a distinct hotfix/* branch.
func (w *Workflows) hotfixBranch(ctx context.Context, name string) (branch, version string, err error) {
	if name != "" {
		name = normalizeVersion(name)
		if e := w.ensureSafeRef(name); e != nil {
			return "", "", e
		}
		return "hotfix/" + name, name, nil
	}
	b, e := w.Current(ctx)
	if e != nil {
		return "", "", e
	}
	n, ok := branchValue(b, "hotfix")
	if !ok {
		return "", "", fmt.Errorf("hotfix version is required unless current branch is hotfix/* or fix/*")
	}
	return b, n, nil
}
func (w *Workflows) HotfixPublish(ctx context.Context, name string) error {
	b, _, e := w.hotfixBranch(ctx, name)
	if e != nil {
		return e
	}
	if _, e = w.git(ctx, "checkout", "--no-guess", b); e != nil {
		return e
	}
	_, e = w.git(ctx, "push", "--set-upstream", w.remote, "refs/heads/"+b+":refs/heads/"+b)
	return e
}
func (w *Workflows) HotfixFinish(ctx context.Context, name string, local bool) error {
	resumedBranch := ""
	if local && name == "" {
		if resumed, found, err := w.resumeLocalFinishName(ctx, "hotfix-finish", "hotfix"); err != nil {
			return err
		} else if found {
			name = resumed
			journal, _, journalErr := w.readJournal(ctx)
			if journalErr != nil {
				return fmt.Errorf("read resumed hotfix finish journal: %w", journalErr)
			}
			if journal == nil {
				return fmt.Errorf("resumed hotfix finish journal disappeared")
			}
			resumedBranch = journal.Source
		}
	}
	branch, n, e := w.hotfixBranch(ctx, name)
	if e != nil {
		return e
	}
	if resumedBranch != "" {
		branch = resumedBranch
	}
	if !local {
		if e = w.RequireParity(ctx, branch); e != nil {
			return e
		}
		main, e := w.Main(ctx)
		if e != nil {
			return e
		}
		_, e = w.gh(ctx, "pr", "create", "--head", branch, "-B", main, "-b", fmt.Sprintf("Hotfix Release %s, will merge into %s", branch, main), "-t", fmt.Sprintf("chore: Hotfix Release from %s", branch))
		return e
	}
	return w.finishHotfixLocal(ctx, branch, n)
}
func (w *Workflows) finishHotfixLocal(ctx context.Context, branch, version string) error {
	if w.isDryRun() {
		return nil
	}
	unlock, e := w.acquireJournalLock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	if _, e = w.git(ctx, "fetch", w.remote, "--prune", "--tags"); e != nil {
		return e
	}
	target, e := w.hotfixTargetFromFetched(ctx, version)
	if e != nil {
		return e
	}
	j, p, e := w.startLocalFinishJournal(ctx, "hotfix-finish", branch, target, version, w.localFinishSteps("hotfix-finish"))
	if e != nil {
		return e
	}
	for j.Done < len(j.Steps) {
		s := j.Steps[j.Done]
		if s == "delete-remote" {
			if err := w.verifyFinished(ctx, target, j.SourceSHA, version); err != nil {
				return err
			}
		}
		switch s {
		case "checkout-target":
			e = w.checkoutFetchedBase(ctx, target)
		case "merge":
			e = w.checkoutFinishBranch(ctx, target)
			if e == nil {
				e = w.mergeContinue(ctx)
			}
			if e == nil {
				_, e = w.git(ctx, "merge", "--no-ff", j.SourceSHA, "-m", fmt.Sprintf("chore: Hotfix Release %s", version))
			}
		case "tag":
			e = w.recordFinishTagTarget(ctx, p, j)
			if e == nil {
				e = w.ensureAnnotatedTag(ctx, version, fmt.Sprintf("chore: Hotfix Release %s", version), j.TagTargetSHA)
			}
		case "push-target":
			_, e = w.git(ctx, "push", w.remote, "refs/heads/"+target+":refs/heads/"+target)
		case "push-tag":
			tagRef := "refs/tags/" + version
			_, e = w.git(ctx, "push", w.remote, tagRef+":"+tagRef)
		case "delete-remote":
			e = w.deleteRemoteBranch(ctx, branch, j.SourceSHA)
		case "delete-local":
			e = w.deleteLocalBranch(ctx, branch)
		}
		if e != nil {
			return fmt.Errorf("%s: %w", s, e)
		}
		if e = w.advanceJournal(p, j); e != nil {
			return e
		}
	}
	return clearJournal(p)
}

// hotfixTarget preserves the legacy support-branch behavior: if a support
// branch exists for the hotfix major/minor line, it receives the fix instead
// of main. GitHub Flow and trunk-based repositories always use main.
func (w *Workflows) hotfixTarget(ctx context.Context, version string) (string, error) {
	if w.hasDevelop() {
		if _, err := w.git(ctx, "fetch", w.remote, "--prune"); err != nil {
			return "", err
		}
	}
	return w.hotfixTargetFromFetched(ctx, version)
}

func (w *Workflows) hotfixTargetFromFetched(ctx context.Context, version string) (string, error) {
	main, err := w.Main(ctx)
	if err != nil || !w.hasDevelop() {
		return main, err
	}
	m := semverRE.FindStringSubmatch(version)
	if m == nil {
		return "", fmt.Errorf("invalid hotfix version %q", version)
	}
	refs := []string{"refs/heads/support/", "refs/remotes/" + w.remote + "/support/"}
	matchesByName := map[string]struct{}{}
	for _, ref := range refs {
		out, listErr := w.git(ctx, "for-each-ref", "--format=%(refname:short)", ref)
		if listErr != nil {
			return "", listErr
		}
		for _, candidate := range strings.Fields(out) {
			candidate = strings.TrimPrefix(candidate, w.remote+"/")
			major, minor, ok := supportLine(candidate)
			if ok && major == m[1] && minor == m[2] {
				matchesByName[candidate] = struct{}{}
			}
		}
	}
	var matches []string
	for candidate := range matchesByName {
		matches = append(matches, candidate)
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return main, nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous support branches for hotfix %s: %s", version, strings.Join(matches, ", "))
	}
	return matches[0], nil
}

var supportBranchRE = regexp.MustCompile(`^support/v(\d+)\.(\d+)(?:\.\d+)?(?:[-+][^/]+)?$`)

func supportLine(branch string) (major, minor string, ok bool) {
	m := supportBranchRE.FindStringSubmatch(branch)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

func (w *Workflows) HotfixPurge(ctx context.Context, name string) error {
	branch, _, e := w.hotfixBranch(ctx, name)
	if e != nil {
		return e
	}
	return w.purge(ctx, branch)
}
