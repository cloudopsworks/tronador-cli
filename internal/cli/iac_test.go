package cli

import "testing"

func TestIACCommandExposesModuleAsPrimarySurface(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"iac", "module"})
	if err != nil {
		t.Fatalf("find iac module: %v", err)
	}
	if cmd == nil || cmd.Name() != "module" {
		t.Fatalf("iac module command not found: %#v", cmd)
	}
}

func TestIACModuleKeepsModuleVersionsAliases(t *testing.T) {
	primary := newIACModuleVersionsCommand()
	if primary.Use != "module" {
		t.Fatalf("Use = %q, want module", primary.Use)
	}
	for _, alias := range []string{"module-versions", "module_versions"} {
		if !hasString(primary.Aliases, alias) {
			t.Fatalf("aliases = %v, want %s", primary.Aliases, alias)
		}
	}

	moduleCmd, _, err := rootCmd.Find([]string{"iac", "module"})
	if err != nil {
		t.Fatalf("find iac module: %v", err)
	}
	moduleVersionsCmd, _, err := rootCmd.Find([]string{"iac", "module-versions"})
	if err != nil {
		t.Fatalf("find iac module-versions alias: %v", err)
	}
	moduleUnderscoreCmd, _, err := rootCmd.Find([]string{"iac", "module_versions"})
	if err != nil {
		t.Fatalf("find iac module_versions alias: %v", err)
	}
	if moduleVersionsCmd != moduleCmd || moduleUnderscoreCmd != moduleCmd {
		t.Fatalf("aliases did not resolve to primary command")
	}
}

func TestIACModuleExposesPrereleaseChannelFlags(t *testing.T) {
	cmd := newIACModuleVersionsCommand()
	for _, name := range []string{"alpha", "beta", "minor", "major"} {
		if cmd.Flag(name) == nil {
			t.Fatalf("iac module flag --%s is missing", name)
		}
	}
}

func TestIACModuleTierFlagsAreMutuallyExclusive(t *testing.T) {
	t.Cleanup(func() {
		iacModuleVersionsMinor = false
		iacModuleVersionsMajor = false
		iacModuleVersionsUpgrade = false
	})
	cmd := newIACModuleVersionsCommand()
	cmd.SetArgs([]string{"--minor", "--major"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("parsing --minor and --major succeeded, want mutually-exclusive error")
	}
}

func TestIACModuleExposesMergeInputsFlagWithAliases(t *testing.T) {
	cmd := newIACModuleVersionsCommand()
	flag := cmd.Flag("merge-inputs")
	if flag == nil || flag.Shorthand != "m" {
		t.Fatalf("--merge-inputs/-m missing: %#v", flag)
	}
	if cmd.Flags().Lookup("merge-input") != flag {
		t.Fatal("--merge-input does not resolve to --merge-inputs")
	}
}

func TestIACModuleMergeInputsRequiresUpgrade(t *testing.T) {
	for _, args := range [][]string{{"--merge-inputs"}, {"-m"}, {"--merge-input"}} {
		t.Run(args[0], func(t *testing.T) {
			t.Cleanup(func() {
				iacModuleVersionsMerge = false
				iacModuleVersionsUpgrade = false
			})
			cmd := newIACModuleVersionsCommand()
			cmd.SetArgs(args)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			err := cmd.Execute()
			if err == nil || err.Error() != "--merge-inputs requires --upgrade" {
				t.Fatalf("Execute(%v) error = %v, want --merge-inputs requires --upgrade", args, err)
			}
		})
	}
}
