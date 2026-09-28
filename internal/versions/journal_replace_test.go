package versions

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicCleansTemporaryOnReplacementFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "versions-journal.json")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalReplace := replaceJournalFile
	replaceJournalFile = func(string, string) error { return errors.New("replace failed") }
	t.Cleanup(func() { replaceJournalFile = originalReplace })

	if err := writeAtomic(path, &journal{Version: journalSchemaVersion}); err == nil {
		t.Fatal("writeAtomic unexpectedly succeeded")
	}
	if got := mustReadFile(t, path); got != "original\n" {
		t.Fatalf("journal replaced after replacement failure: %q", got)
	}
	if temporary, err := filepath.Glob(filepath.Join(dir, ".versions-journal-*")); err != nil || len(temporary) != 0 {
		t.Fatalf("temporary files = %v, %v", temporary, err)
	}
}
