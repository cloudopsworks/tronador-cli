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
const journalSchemaVersion = 2

type journal struct {
	Version    int       `json:"version"`
	WayOfWork  string    `json:"wayOfWork"`
	Repository string    `json:"repository"`
	Worktree   string    `json:"worktree"`
	Operation  string    `json:"operation"`
	Source     string    `json:"source"`
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

// startLocalFinishJournal verifies the source has been fetched and exactly
// published before writing any durable state or mutating a target. A matching
// existing journal is a resume and deliberately skips this check: later finish
// steps may already have removed the source branch.
func (w *Workflows) startLocalFinishJournal(ctx context.Context, op, source, target string, steps []string) (*journal, string, error) {
	j, _, err := w.readJournal(ctx)
	if err != nil {
		return nil, "", err
	}
	if j == nil {
		if _, err = w.git(ctx, "fetch", w.remote, "--prune"); err != nil {
			return nil, "", err
		}
		if err = w.RequireParity(ctx, source); err != nil {
			return nil, "", err
		}
	}
	return w.startJournal(ctx, op, source, target, steps)
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

// acquireJournalLock serializes finish invocations within this worktree.  It
// is intentionally short-lived: a merge conflict leaves the journal (the
// resume breadcrumb) but never an orphaned lock that would make recovery
// impossible after a process crash.
func (w *Workflows) acquireJournalLock(ctx context.Context) (func(), error) {
	p, err := w.journalPath(ctx)
	if err != nil {
		return nil, err
	}
	lock := p + ".lock"
	if err = os.MkdirAll(filepath.Dir(lock), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another versions finish is already running for this worktree")
		}
		return nil, err
	}
	_, _ = fmt.Fprintf(f, "pid=%d\ncreated=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
	if err = f.Close(); err != nil {
		_ = os.Remove(lock)
		return nil, err
	}
	return func() { _ = os.Remove(lock) }, nil
}

func (w *Workflows) startJournal(ctx context.Context, op, source, target string, steps []string) (*journal, string, error) {
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
		if j.Version != journalSchemaVersion || j.WayOfWork != w.wow || j.Repository != repository || j.Worktree != worktree || !reflect.DeepEqual(j.Steps, steps) || j.Done < 0 || j.Done > len(j.Steps) {
			return nil, "", fmt.Errorf("unfinished workflow journal does not match this repository, workflow, or expected step plan; resolve it before continuing")
		}
		return j, p, nil
	}
	j = &journal{Version: journalSchemaVersion, WayOfWork: w.wow, Repository: repository, Worktree: worktree, Operation: op, Source: source, Target: target, Steps: append([]string(nil), steps...), Created: time.Now().UTC()}
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
