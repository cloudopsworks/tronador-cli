package versions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// Journal makes local finishing restartable after a merge conflict. It is kept
// under git-path, which scopes it to the repository/worktree instead of /tmp.
const journalSchemaVersion = 3

type journal struct {
	Version    int       `json:"version"`
	WayOfWork  string    `json:"wayOfWork"`
	Repository string    `json:"repository"`
	Worktree   string    `json:"worktree"`
	Operation  string    `json:"operation"`
	Source     string    `json:"source"`
	SourceSHA  string    `json:"sourceSHA"`
	Target     string    `json:"target"`
	Steps      []string  `json:"steps"`
	Done       int       `json:"done"`
	Created    time.Time `json:"created"`
}

func (w *Workflows) journalPath(ctx context.Context) (string, error) {
	return w.gitPath(ctx, "tronador/versions-journal.json")
}
func (w *Workflows) readJournal(ctx context.Context) (*journal, string, error) {
	p, e := w.journalPath(ctx)
	if e != nil {
		return nil, "", e
	}
	b, e := os.ReadFile(p)
	if os.IsNotExist(e) {
		return nil, p, nil
	}
	if e != nil {
		return nil, p, e
	}
	var j journal
	if e = json.Unmarshal(b, &j); e != nil {
		return nil, p, fmt.Errorf("read workflow journal: %w", e)
	}
	return &j, p, nil
}

// resumeLocalFinishName resolves a persisted local-finish breadcrumb before
// looking at HEAD. A failed merge leaves HEAD on the target branch, so current
// branch inference cannot recover the source name. The journal remains the
// authoritative source only when it belongs to this exact worktree/workflow.
func (w *Workflows) resumeLocalFinishName(ctx context.Context, operation, prefix string) (string, bool, error) {
	j, _, err := w.readJournal(ctx)
	if err != nil || j == nil {
		return "", j != nil, err
	}
	if j.Version != journalSchemaVersion || j.WayOfWork != w.wow || j.Operation != operation || j.SourceSHA == "" || !safeRef(j.Source) || !safeRef(j.Target) || !reflect.DeepEqual(j.Steps, w.localFinishSteps(operation)) || j.Done < 0 || j.Done > len(j.Steps) {
		return "", true, fmt.Errorf("unfinished workflow journal does not match the requested %s finish; resolve it before continuing", prefix)
	}
	worktree, err := w.journalWorktree(ctx)
	if err != nil {
		return "", true, err
	}
	repository, err := w.journalRepository(ctx)
	if err != nil {
		return "", true, err
	}
	if j.Worktree != worktree || j.Repository != repository {
		return "", true, fmt.Errorf("unfinished workflow journal does not belong to this repository/worktree")
	}
	name, ok := branchValue(j.Source, prefix)
	if !ok {
		return "", true, fmt.Errorf("unfinished workflow journal source %q is not a %s branch", j.Source, prefix)
	}
	return name, true, nil
}

func (w *Workflows) localFinishSteps(operation string) []string {
	switch operation {
	case "hotfix-finish":
		return []string{"checkout-target", "merge", "tag", "push-target", "push-tag", "delete-remote", "delete-local"}
	case "release-finish":
		steps := []string{"checkout-main", "merge-main", "tag", "push-main", "push-tag"}
		if w.hasDevelop() {
			steps = append(steps, "checkout-develop", "merge-develop", "push-develop")
		}
		return append(steps, "delete-remote", "delete-local")
	default:
		return nil
	}
}
func writeAtomic(path string, v *journal) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".versions-journal-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(b); e == nil {
		e = f.Chmod(0600)
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(tmp, path)
}

// startLocalFinishJournal verifies the fetched source and finish tag before
// writing any durable state or mutating a target. An existing journal remains
// resumable after its remote-deletion step, but otherwise its immutable source
// identity must still be exactly published before it can mutate a target.
func (w *Workflows) startLocalFinishJournal(ctx context.Context, op, source, target, tag string, steps []string) (*journal, string, error) {
	j, _, err := w.readJournal(ctx)
	if err != nil {
		return nil, "", err
	}
	if j == nil {
		if err = w.preflightFinishTag(ctx, tag, nil); err != nil {
			return nil, "", err
		}
		sha, parityErr := w.remoteParitySHA(ctx, source)
		if parityErr != nil {
			return nil, "", parityErr
		}
		return w.startJournal(ctx, op, source, target, sha, steps)
	}
	started, path, err := w.startJournal(ctx, op, source, target, "", steps)
	if err != nil {
		return nil, "", err
	}
	if err = w.revalidateJournalSource(ctx, started); err != nil {
		return nil, "", err
	}
	if err = w.preflightFinishTag(ctx, tag, started); err != nil {
		return nil, "", err
	}
	return started, path, nil
}

// revalidateJournalSource keeps a resumed finish bound to the exact source
// that was journaled. At its remote-deletion step, the remote must still match
// or be proven absent: a crash after the server accepts deletion but before the
// journal advances must remain restartable, and no target mutation remains.
func (w *Workflows) revalidateJournalSource(ctx context.Context, j *journal) error {
	deleteStep, err := journalStepBoundary(j.Steps, "delete-remote")
	if err != nil {
		return err
	}
	if j.Done > deleteStep {
		return nil
	}
	if j.Done == deleteStep {
		remoteSHA, exists, err := w.remoteBranchSHA(ctx, j.Source)
		if err != nil {
			return fmt.Errorf("revalidate journaled source %s: %w", j.Source, err)
		}
		if !exists {
			return nil
		}
		if remoteSHA != j.SourceSHA {
			return fmt.Errorf("journaled source %s changed: expected %s, got %s", j.Source, j.SourceSHA, remoteSHA)
		}
		return nil
	}
	sha, err := w.remoteParitySHA(ctx, j.Source)
	if err != nil {
		return fmt.Errorf("revalidate journaled source %s: %w", j.Source, err)
	}
	if sha != j.SourceSHA {
		return fmt.Errorf("journaled source %s changed: expected %s, got %s", j.Source, j.SourceSHA, sha)
	}
	return nil
}

func (w *Workflows) remoteBranchSHA(ctx context.Context, branch string) (string, bool, error) {
	if err := w.ensureSafeRef(branch); err != nil {
		return "", false, err
	}
	out, err := w.git(ctx, "ls-remote", w.remote, "refs/heads/"+branch)
	if err != nil {
		return "", false, err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", false, nil
	}
	if len(fields) < 2 {
		return "", false, fmt.Errorf("unexpected remote branch response %q", strings.TrimSpace(out))
	}
	return fields[0], true, nil
}

func (w *Workflows) preflightFinishTag(ctx context.Context, tag string, j *journal) error {
	commit, exists, err := w.probeAnnotatedFinishTag(ctx, tag)
	if err != nil {
		return err
	}
	if !exists {
		if j != nil {
			boundary, boundaryErr := finishTagBoundary(j.Steps)
			if boundaryErr != nil {
				return boundaryErr
			}
			if j.Done > boundary {
				return fmt.Errorf("finish journal requires annotated tag %s, but it is absent", tag)
			}
		}
		return nil
	}
	if j == nil {
		return fmt.Errorf("existing annotated tag %s blocks a new local finish", tag)
	}
	boundary, err := finishTagBoundary(j.Steps)
	if err != nil {
		return err
	}
	if j.Done < boundary {
		return fmt.Errorf("existing annotated tag %s is incompatible with unfinished local finish state", tag)
	}
	targetCommit, err := w.git(ctx, "rev-parse", "--verify", j.Target+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve finished target %s: %w", j.Target, err)
	}
	if commit != strings.TrimSpace(targetCommit) {
		return fmt.Errorf("existing annotated tag %s is incompatible with finished target %s", tag, j.Target)
	}
	return nil
}

func finishTagBoundary(steps []string) (int, error) {
	return journalStepBoundary(steps, "tag")
}

func journalStepBoundary(steps []string, wanted string) (int, error) {
	index := -1
	for i, step := range steps {
		if step != wanted {
			continue
		}
		if index >= 0 {
			return 0, fmt.Errorf("finish journal plan has multiple %s steps", wanted)
		}
		index = i
	}
	if index < 0 {
		return 0, fmt.Errorf("finish journal plan has no %s step", wanted)
	}
	return index, nil
}

func (w *Workflows) journalWorktree(ctx context.Context) (string, error) {
	out, err := w.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	if worktree := strings.TrimSpace(out); worktree != "" {
		return filepath.Abs(worktree)
	}
	return filepath.Abs(w.dir)
}

// journalRepository identifies the common git directory.  The journal file is
// already worktree-scoped by git-path; recording both identities makes a
// copied/corrupted journal fail closed instead of being replayed in another
// checkout of the same repository.
func (w *Workflows) journalRepository(ctx context.Context) (string, error) {
	out, err := w.git(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(out)
	if p == "" {
		return "", fmt.Errorf("git returned an empty common directory")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(w.dir, p)
	}
	p, err = filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(p); resolveErr == nil {
		return resolved, nil
	}
	return p, nil
}

// acquireJournalLock serializes finish invocations within this worktree. The
// lock file is intentionally persistent: OS lock ownership is bound to the open
// file descriptor and is released by the OS after crashes/SIGKILL. Keeping the
// inode avoids unlink/recreate races that could permit concurrent owners.
func (w *Workflows) acquireJournalLock(ctx context.Context) (func(), error) {
	p, err := w.journalPath(ctx)
	if err != nil {
		return nil, err
	}
	lock := p + ".lock"
	if err = os.MkdirAll(filepath.Dir(lock), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lock, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockJournalFile(f); err != nil {
		_ = f.Close()
		if journalLockBusy(err) {
			return nil, fmt.Errorf("another versions finish is already running for this worktree")
		}
		return nil, err
	}
	return func() {
		_ = unlockJournalFile(f)
		_ = f.Close()
	}, nil
}

func (w *Workflows) startJournal(ctx context.Context, op, source, target, sourceSHA string, steps []string) (*journal, string, error) {
	worktree, e := w.journalWorktree(ctx)
	if e != nil {
		return nil, "", e
	}
	repository, e := w.journalRepository(ctx)
	if e != nil {
		return nil, "", e
	}
	j, p, e := w.readJournal(ctx)
	if e != nil {
		return nil, "", e
	}
	if j != nil {
		if j.Operation != op || j.Source != source || j.Target != target {
			return nil, "", fmt.Errorf("unfinished %s workflow for %s; resume or resolve it before starting %s", j.Operation, j.Source, op)
		}
		if j.Version != journalSchemaVersion || j.WayOfWork != w.wow || j.Repository != repository || j.Worktree != worktree || j.SourceSHA == "" || !reflect.DeepEqual(j.Steps, steps) || j.Done < 0 || j.Done > len(j.Steps) {
			return nil, "", fmt.Errorf("unfinished workflow journal does not match this repository, workflow, or expected step plan; resolve it before continuing")
		}
		return j, p, nil
	}
	if sourceSHA == "" {
		return nil, "", fmt.Errorf("cannot create finish journal without exact published source SHA")
	}
	j = &journal{Version: journalSchemaVersion, WayOfWork: w.wow, Repository: repository, Worktree: worktree, Operation: op, Source: source, SourceSHA: sourceSHA, Target: target, Steps: append([]string(nil), steps...), Created: time.Now().UTC()}
	if e = writeAtomic(p, j); e != nil {
		return nil, "", e
	}
	return j, p, nil
}
func (w *Workflows) advanceJournal(path string, j *journal) error {
	j.Done++
	return writeAtomic(path, j)
}
func clearJournal(path string) error {
	if e := os.Remove(path); e != nil && !os.IsNotExist(e) {
		return e
	}
	return nil
}
