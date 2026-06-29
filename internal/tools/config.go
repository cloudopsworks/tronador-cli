package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultToolsConfigPath = "assets/tools.json"
)

// Config is the JSON-backed catalog of provisionable tools.
type Config struct {
	Tools []ToolConfig `json:"tools"`
}

// ToolConfig contains tool-specific data. Generic install behavior stays in Go.
type ToolConfig struct {
	Name              string                `json:"name"`
	Executable        string                `json:"executable,omitempty"`
	DefaultVersion    string                `json:"default_version,omitempty"`
	VersionEnvVars    []string              `json:"version_env_vars,omitempty"`
	URLTemplate       string                `json:"url_template,omitempty"`
	Format            string                `json:"format,omitempty"`
	BinaryName        string                `json:"binary_name,omitempty"`
	ArchiveBinaryPath string                `json:"archive_binary_path,omitempty"`
	ChecksumSHA256    string                `json:"checksum_sha256,omitempty"`
	PlatformOverrides map[string]ToolConfig `json:"platform_overrides,omitempty"`
}

// LoadConfig returns the effective embedded-plus-runtime override config.
func LoadConfig(workDir, explicitPath string) (Config, error) {
	data, err := defaultConfigFS.ReadFile(defaultToolsConfigPath)
	if err != nil {
		return Config{}, fmt.Errorf("load embedded tool config: %w", err)
	}
	config, err := parseConfigBytes("embedded:"+defaultToolsConfigPath, data)
	if err != nil {
		return Config{}, err
	}
	for _, path := range configOverridePaths(workDir, explicitPath) {
		if path == "" || !Exists(path) {
			continue
		}
		override, err := loadConfigFile(path)
		if err != nil {
			return Config{}, err
		}
		config = mergeConfig(config, override)
	}
	return config, nil
}

// Tool returns a platform-effective tool definition from the config.
func (c Config) Tool(name string) (ToolConfig, bool) {
	for _, tool := range c.Tools {
		if tool.Name == name {
			return applyPlatformOverride(tool), true
		}
	}
	return ToolConfig{}, false
}

// ToolNames returns sorted tool names for inspection/tests.
func (c Config) ToolNames() []string {
	names := make([]string, 0, len(c.Tools))
	for _, tool := range c.Tools {
		if tool.Name != "" {
			names = append(names, tool.Name)
		}
	}
	sort.Strings(names)
	return names
}

func configOverridePaths(workDir, explicitPath string) []string {
	var paths []string
	if configDir, err := os.UserHomeDir(); err == nil && configDir != "" {
		paths = append(paths, filepath.Join(configDir, ".cloudopsworks", "tronador", "tools.json"))
	}
	if workDir == "" {
		workDir = "."
	}
	if abs, err := filepath.Abs(workDir); err == nil {
		paths = append(paths,
			filepath.Join(abs, ".cloudopsworks", "tronador", "tools.json"),
			filepath.Join(abs, ".tronador", "tools.json"),
		)
	}
	if env := firstSetEnv("TRONADOR_TOOLS_CONFIG", "TRONADOR_TOOLS_CONFIG_PATH"); env != "" {
		paths = append(paths, env)
	}
	if explicitPath != "" {
		paths = append(paths, explicitPath)
	}
	return normalizeConfigPaths(paths)
}

func normalizeConfigPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		expanded, err := ExpandHomePath(path)
		if err != nil {
			out = append(out, path)
			continue
		}
		abs := expanded
		if !filepath.IsAbs(abs) {
			if resolved, err := filepath.Abs(abs); err == nil {
				abs = resolved
			}
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	return out
}

func loadConfigFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("load tool config %s: %w", path, err)
	}
	return parseConfigBytes(path, data)
}

func parseConfigBytes(source string, data []byte) (Config, error) {
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse tool config %s: %w", source, err)
	}
	return config, nil
}

func mergeConfig(base, override Config) Config {
	index := map[string]int{}
	merged := Config{Tools: append([]ToolConfig(nil), base.Tools...)}
	for i, tool := range merged.Tools {
		if tool.Name != "" {
			index[tool.Name] = i
		}
	}
	for _, tool := range override.Tools {
		if tool.Name == "" {
			continue
		}
		if i, ok := index[tool.Name]; ok {
			merged.Tools[i] = mergeToolConfig(merged.Tools[i], tool)
			continue
		}
		index[tool.Name] = len(merged.Tools)
		merged.Tools = append(merged.Tools, tool)
	}
	return merged
}

func mergeToolConfig(base, override ToolConfig) ToolConfig {
	out := base
	if override.Name != "" {
		out.Name = override.Name
	}
	if override.Executable != "" {
		out.Executable = override.Executable
	}
	if override.DefaultVersion != "" {
		out.DefaultVersion = override.DefaultVersion
	}
	if len(override.VersionEnvVars) > 0 {
		out.VersionEnvVars = append([]string(nil), override.VersionEnvVars...)
	}
	if override.URLTemplate != "" {
		out.URLTemplate = override.URLTemplate
	}
	if override.Format != "" {
		out.Format = override.Format
	}
	if override.BinaryName != "" {
		out.BinaryName = override.BinaryName
	}
	if override.ArchiveBinaryPath != "" {
		out.ArchiveBinaryPath = override.ArchiveBinaryPath
	}
	if override.ChecksumSHA256 != "" {
		out.ChecksumSHA256 = override.ChecksumSHA256
	}
	if len(override.PlatformOverrides) > 0 {
		if out.PlatformOverrides == nil {
			out.PlatformOverrides = map[string]ToolConfig{}
		}
		for key, platformOverride := range override.PlatformOverrides {
			if existing, ok := out.PlatformOverrides[key]; ok {
				out.PlatformOverrides[key] = mergeToolConfig(existing, platformOverride)
			} else {
				out.PlatformOverrides[key] = platformOverride
			}
		}
	}
	return out
}

func applyPlatformOverride(tool ToolConfig) ToolConfig {
	if len(tool.PlatformOverrides) == 0 {
		return tool
	}
	for _, key := range []string{platformOS(), platformKey()} {
		if override, ok := tool.PlatformOverrides[key]; ok {
			tool = mergeToolConfig(tool, override)
		}
	}
	return tool
}

func (t ToolConfig) toTool(configuredPath, version string) Tool {
	return Tool{
		Name:           t.Name,
		Executable:     t.executable(),
		ConfiguredPath: configuredPath,
		Version:        version,
		Download: DownloadSpec{
			URLTemplate:       t.URLTemplate,
			Format:            t.Format,
			BinaryName:        t.BinaryName,
			ArchiveBinaryPath: t.ArchiveBinaryPath,
			ExpectedSHA256:    t.ChecksumSHA256,
			DefaultVersion:    t.DefaultVersion,
			VersionEnvNames:   append([]string(nil), t.VersionEnvVars...),
		},
	}
}

func (t ToolConfig) executable() string {
	if t.Executable != "" {
		return t.Executable
	}
	return t.Name
}

func firstSetEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
