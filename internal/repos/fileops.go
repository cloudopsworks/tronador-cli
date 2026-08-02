package repos

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func (r *Runner) copyFileIfExists(src, dst string) error {
	if !exists(src) {
		return nil
	}
	return r.copyFile(src, dst)
}

func (r *Runner) copyFile(src, dst string) error {
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN cp %s %s\n", src, dst)
		return nil
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("copyFile source is a directory: %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (r *Runner) copyFileAtomically(src, dst string) error {
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN cp %s %s\n", src, dst)
		return nil
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("copyFile source is a directory: %s", src)
	}

	parent := filepath.Dir(dst)
	createdDirs, err := mkdirAllTracked(parent, 0o755)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if !cleanup {
			return
		}
		removeEmptyDirs(createdDirs)
	}()

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(parent, ".tronador-copy-*")
	if err != nil {
		_ = in.Close()
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	_, copyErr := io.Copy(tmp, in)
	inCloseErr := in.Close()
	if copyErr != nil {
		_ = tmp.Close()
		return copyErr
	}
	if inCloseErr != nil {
		_ = tmp.Close()
		return inCloseErr
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if pathExists(dst) {
		return fmt.Errorf("destination already exists: %s", dst)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	tmpPath = ""
	cleanup = false
	return nil
}

func mkdirAllTracked(path string, mode os.FileMode) ([]string, error) {
	missing := make([]string, 0)
	current := filepath.Clean(path)
	for {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("path is not a directory: %s", current)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return nil, err
	}
	return missing, nil
}

func removeEmptyDirs(paths []string) {
	for index := len(paths) - 1; index >= 0; index-- {
		_ = os.Remove(paths[index])
	}
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (r *Runner) copyDirIfExists(src, dst string) error {
	if !exists(src) {
		return nil
	}
	return r.copyDir(src, dst)
}

func (r *Runner) copyDir(src, dst string) error {
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN cp -R %s %s\n", src, dst)
		return nil
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("copyDir source is not a directory: %s", src)
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return copyDirTree(src, dst, info.Mode())
}

func (r *Runner) copyDirContentsIfExists(src, dst string) error {
	if !exists(src) {
		return nil
	}
	return r.copyDirContents(src, dst)
}

func (r *Runner) copyDirContents(src, dst string) error {
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN cp -R %s/* %s\n", src, dst)
		return nil
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("copyDirContents source is not a directory: %s", src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyDirTree(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(dst, mode); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return copyDirTree(src, dst, info.Mode())
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
