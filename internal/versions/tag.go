package versions

import (
	"context"
	"fmt"
	"strings"
)

// Tag creates the legacy gitflow version tag. Stable branches use MajorMinorPatch;
// other branches preserve the GitVersion qualifier. qualifier maps to +deploy-X.
func (w *Workflows) Tag(ctx context.Context, qualifier string, publish bool) (string, error) {
	branch, e := w.Current(ctx)
	if e != nil {
		return "", e
	}
	if e = w.RequireParity(ctx, branch); e != nil {
		return "", e
	}
	main, e := w.Main(ctx)
	if e != nil {
		return "", e
	}
	variable := "SemVer"
	if branch == main {
		variable = "MajorMinorPatch"
	}
	o, e := w.gitVersion(ctx, variable)
	if e != nil {
		return "", e
	}
	v := normalizeVersion(strings.TrimSpace(o))
	if qualifier != "" {
		if e = w.ensureSafeRef(qualifier); e != nil {
			return "", e
		}
		v += "+deploy-" + qualifier
	}
	if e = w.ensureSafeRef(v); e != nil {
		return "", e
	}
	if e = w.ensureAnnotatedTag(ctx, v, fmt.Sprintf("chore: Version Tagging: %s", v), "HEAD"); e != nil {
		return "", e
	}
	if publish {
		tagRef := "refs/tags/" + v
		if _, e = w.git(ctx, "push", w.remote, tagRef+":"+tagRef); e != nil {
			return "", e
		}
	}
	return v, nil
}
