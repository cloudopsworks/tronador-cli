//go:build !darwin && !freebsd && !linux && !windows

package versions

import (
	"fmt"
	"os"
)

func lockJournalFile(_ *os.File) error {
	return fmt.Errorf("local finish journal locking is unsupported on this platform")
}

func unlockJournalFile(_ *os.File) error { return nil }

func journalLockBusy(_ error) bool { return false }
