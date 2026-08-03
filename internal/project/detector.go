package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Detection is the immutable result of marker inspection.
type Detection struct {
	WorkDir         string   `json:"workdir"`
	ProfileID       string   `json:"implementation"`
	DisplayName     string   `json:"display_name"`
	Marker          string   `json:"detected_marker"`
	Markers         []string `json:"markers,omitempty"`
	RegistryVersion string   `json:"registry_version"`
	Warnings        []string `json:"warnings,omitempty"`
}

// Detect inspects exactly workdir/.cloudopsworks and never falls back to
// repository names, language files, .github, Git remotes, or Makefiles.
func (r Registry) Detect(workdir string) (Detection, error) {
	if err := r.Validate(); err != nil {
		return Detection{}, err
	}
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return Detection{}, wrapProjectError("project_implementation_unknown", "resolve workdir", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		return Detection{}, wrapProjectError("project_implementation_unknown", fmt.Sprintf("workdir %s is unavailable", abs), err)
	}
	markerDir := filepath.Join(abs, ".cloudopsworks")
	matched := map[string][]string{}
	for _, profile := range r.Profiles {
		for _, marker := range profile.Markers {
			path := filepath.Join(markerDir, marker.Name)
			entry, statErr := os.Lstat(path)
			if os.IsNotExist(statErr) {
				continue
			}
			if statErr != nil {
				return Detection{}, wrapProjectError("project_marker_invalid", fmt.Sprintf("inspect marker %s", path), statErr)
			}
			if entry.Mode()&os.ModeSymlink != 0 || !entry.Mode().IsRegular() {
				return Detection{}, projectError("project_marker_invalid", fmt.Sprintf("marker %s must be a regular file, not a directory or symlink", path))
			}
			matched[profile.ID] = append(matched[profile.ID], marker.Name)
		}
	}
	if len(matched) == 0 {
		names := make([]string, 0)
		for _, profile := range r.Profiles {
			for _, marker := range profile.Markers {
				names = append(names, marker.Name)
			}
		}
		sort.Strings(names)
		return Detection{}, projectError("project_implementation_unknown", fmt.Sprintf("no recognized marker in %s; inspected %s; recognized markers: %s", abs, markerDir, strings.Join(names, ", ")))
	}
	if len(matched) > 1 {
		ids := make([]string, 0, len(matched))
		for id := range matched {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, fmt.Sprintf("%s (%s)", id, strings.Join(matched[id], ", ")))
		}
		return Detection{}, projectError("project_implementation_ambiguous", "multiple implementations matched: "+strings.Join(parts, "; "))
	}
	for id, markers := range matched {
		profile, _ := r.Profile(id)
		sort.Strings(markers)
		canonical := markers[0]
		for _, marker := range profile.Markers {
			if marker.Canonical && hasString(markers, marker.Name) {
				canonical = marker.Name
				break
			}
		}
		return Detection{WorkDir: abs, ProfileID: id, DisplayName: profile.DisplayName, Marker: filepath.ToSlash(filepath.Join(".cloudopsworks", canonical)), Markers: append([]string(nil), markers...), RegistryVersion: profile.Version}, nil
	}
	return Detection{}, projectError("project_implementation_unknown", "marker scan returned no profile")
}
