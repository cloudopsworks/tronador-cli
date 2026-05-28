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
