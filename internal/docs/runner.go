package docs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Options controls docs command execution.
type Options struct {
	WorkDir string
	DocsDir string
	DryRun  bool
	Stdout  io.Writer
	Stderr  io.Writer
}

// Runner executes docs commands.
type Runner struct {
	Opts Options
}

// TargetsOptions controls docs/targets.md generation.
type TargetsOptions struct {
	Output     string
	MakePath   string
	HelpTarget string
	Title      string
}

// TerraformOptions controls docs/terraform.md generation.
type TerraformOptions struct {
	Output            string
	TerraformDocsPath string
	Format            string
	SkipInit          bool
}

// CopyrightOptions controls source copyright header injection.
type CopyrightOptions struct {
	Command             string
	Software            string
	SoftwareDescription string
	License             string
	Holder              string
	Year                string
	OutputDir           string
	WordWrap            string
}

// NewRunner normalizes docs command options.
func NewRunner(opts Options) (*Runner, error) {
	if opts.WorkDir == "" {
		opts.WorkDir = envDefault("TRONADOR_WORKDIR", ".")
	}
	abs, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	opts.WorkDir = abs
	if opts.DocsDir == "" {
		opts.DocsDir = envDefault("DOCS_DIR", "docs")
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	return &Runner{Opts: opts}, nil
}

// Init ensures the docs directory exists.
func (r *Runner) Init() error {
	dir := r.workPath(r.Opts.DocsDir)
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN: mkdir -p %s\n", dir)
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

// Targets generates a Markdown file from Make help output.
func (r *Runner) Targets(ctx context.Context, opts TargetsOptions) error {
	if opts.Output == "" {
		opts.Output = filepath.Join(r.Opts.DocsDir, "targets.md")
	}
	if opts.HelpTarget == "" {
		opts.HelpTarget = envDefault("DEFAULT_HELP_TARGET", "help/short")
	}
	if opts.Title == "" {
		opts.Title = "## Makefile Targets"
	}
	makePath, err := resolveMake(opts.MakePath)
	if err != nil {
		return err
	}
	if err := r.Init(); err != nil {
		return err
	}
	args := []string{"--no-print-directory", "--quiet", "--silent", opts.HelpTarget}
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN: %s %s > %s\n", makePath, strings.Join(args, " "), opts.Output)
		return nil
	}
	cmd := exec.CommandContext(ctx, makePath, args...)
	cmd.Dir = r.Opts.WorkDir
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			_, _ = io.Copy(r.Opts.Stderr, &stderr)
		}
		return fmt.Errorf("run make help target %s: %w", opts.HelpTarget, err)
	}
	var content bytes.Buffer
	content.WriteString(opts.Title)
	content.WriteString("\n```\n")
	content.WriteString(strings.TrimRight(StripANSI(out.String()), "\n"))
	content.WriteString("\n```\n")
	return writeFileAtomic(r.workPath(opts.Output), content.Bytes())
}

// Terraform generates terraform docs using terraform-docs.
func (r *Runner) Terraform(ctx context.Context, opts TerraformOptions) error {
	if opts.Output == "" {
		opts.Output = filepath.Join(r.Opts.DocsDir, "terraform.md")
	}
	if opts.Format == "" {
		opts.Format = "md"
	}
	terraformDocs := opts.TerraformDocsPath
	if !r.Opts.DryRun || terraformDocs == "" {
		var err error
		terraformDocs, err = resolveExecutable(terraformDocs, "TERRAFORM_DOCS", "terraform-docs")
		if err != nil {
			return err
		}
	}
	if !opts.SkipInit {
		if err := r.Init(); err != nil {
			return err
		}
	}
	args := []string{opts.Format, "."}
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN: %s %s > %s\n", terraformDocs, strings.Join(args, " "), opts.Output)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, terraformDocs, args...)
	cmd.Dir = r.Opts.WorkDir
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			_, _ = io.Copy(r.Opts.Stderr, &stderr)
		}
		return fmt.Errorf("run terraform-docs: %w", err)
	}
	return writeFileAtomic(r.workPath(opts.Output), out.Bytes())
}

// CopyrightAdd runs the configured copyright-header command.
func (r *Runner) CopyrightAdd(ctx context.Context, opts CopyrightOptions) error {
	if opts.Software == "" {
		opts.Software = envDefault("COPYRIGHT_SOFTWARE", filepath.Base(r.Opts.WorkDir))
	}
	if opts.SoftwareDescription == "" {
		opts.SoftwareDescription = os.Getenv("COPYRIGHT_SOFTWARE_DESCRIPTION")
	}
	if opts.SoftwareDescription == "" {
		return fmt.Errorf("COPYRIGHT_SOFTWARE_DESCRIPTION is required")
	}
	if opts.License == "" {
		opts.License = envDefault("COPYRIGHT_LICENSE", "ASL2")
	}
	if opts.Holder == "" {
		opts.Holder = envDefault("COPYRIGHT_HOLDER", "Cloud Ops Works <info@cloudops.works>")
	}
	if opts.Year == "" {
		opts.Year = envDefault("COPYRIGHT_YEAR", strconv.Itoa(time.Now().Year()))
	}
	if opts.OutputDir == "" {
		opts.OutputDir = envDefault("COPYRIGHT_OUTPUT_DIR", "/usr/src")
	}
	if opts.WordWrap == "" {
		opts.WordWrap = envDefault("COPYRIGHT_WORD_WRAP", "100")
	}
	if opts.Command == "" {
		if env := os.Getenv("COPYRIGHT_CMD"); env != "" {
			opts.Command = env
		} else {
			opts.Command = fmt.Sprintf("docker run --rm --volume %s:%s osterman/copyright-header:latest", shellQuote(r.Opts.WorkDir), shellQuote(opts.OutputDir))
		}
	}
	args := []string{
		"--license", opts.License,
		"--add-path", "cmd:main.go",
		"--guess-extension",
		"--copyright-holder", opts.Holder,
		"--copyright-software", opts.Software,
		"--copyright-software-description", opts.SoftwareDescription,
		"--copyright-year", opts.Year,
		"--word-wrap", opts.WordWrap,
		"--output-dir", opts.OutputDir,
	}
	commandLine := opts.Command + " " + shellJoin(args)
	if r.Opts.DryRun {
		fmt.Fprintf(r.Opts.Stdout, "DRY-RUN: %s\n", commandLine)
		return nil
	}
	cmd := shellCommand(ctx, commandLine)
	cmd.Dir = r.Opts.WorkDir
	cmd.Stdout = r.Opts.Stdout
	cmd.Stderr = r.Opts.Stderr
	return cmd.Run()
}

// StripANSI removes terminal color/control sequences from text.
func StripANSI(input string) string {
	return ansiRE.ReplaceAllString(input, "")
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func (r *Runner) workPath(value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(r.Opts.WorkDir, value)
}

func envDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func resolveMake(configured string) (string, error) {
	if configured != "" {
		return resolveExecutable(configured, "MAKE", "make")
	}
	if env := os.Getenv("SELF"); env != "" {
		return resolveExecutable(env, "SELF", "make")
	}
	if env := os.Getenv("MAKE"); env != "" {
		return resolveExecutable(env, "MAKE", "make")
	}
	if p, err := exec.LookPath("make"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("gmake"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("make executable not found; install make/gmake or pass --make")
}

func resolveExecutable(configured, envName, fallback string) (string, error) {
	name := configured
	if name == "" {
		name = os.Getenv(envName)
	}
	if name == "" {
		name = fallback
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		if _, err := os.Stat(name); err != nil {
			return "", fmt.Errorf("%s executable not found at %s", fallback, name)
		}
		return name, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s executable not found; install it or pass --%s", fallback, fallback)
	}
	return p, nil
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if runtime.GOOS == "windows" {
		return strconv.Quote(value)
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
