package repos

import (
	"bytes"
	"io"
	"path"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// maxTemplateHistoryYAML bounds each historical template file read into memory.
const maxTemplateHistoryYAML = 4 << 20

// readTemplateHistoryConfig returns every distinct version of the template's
// CloudOps YAML files across its release tags, keyed by path under
// .cloudopsworks. The merge uses them to recognize inline comments that a
// template release wrote, so an upgrade replaces stale template wording instead
// of appending the new wording beside it. Comments can lag several releases
// behind the repository's _VERSION, so a single previous version is not enough.
// Every tag counts, including prereleases and tags newer than the target: local
// text matching any released template wording on the same key is template
// owned. The data is advisory: any failure yields nil (or skips the unreadable
// tag) and the merge keeps its two-way behavior.
func (r *Runner) readTemplateHistoryConfig() map[string][][]byte {
	repository, err := git.PlainOpen(r.templatePath())
	if err != nil {
		return nil
	}
	tags, err := repository.Tags()
	if err != nil {
		return nil
	}
	history := make(map[string][][]byte)
	// Deduplicate per path: identical blobs can live at several paths, and
	// each path needs its own history entry.
	seen := make(map[string]bool)
	_ = tags.ForEach(func(ref *plumbing.Reference) error {
		hash, err := repository.ResolveRevision(plumbing.Revision(ref.Name().String()))
		if err != nil {
			return nil
		}
		commit, err := repository.CommitObject(*hash)
		if err != nil {
			return nil
		}
		root, err := commit.Tree()
		if err != nil {
			return nil
		}
		tree, err := root.Tree(".cloudopsworks")
		if err != nil {
			return nil
		}
		_ = tree.Files().ForEach(func(file *object.File) error {
			if file.Mode != filemode.Regular && file.Mode != filemode.Executable {
				return nil
			}
			if !isYAMLFile(file.Name) || file.Size > maxTemplateHistoryYAML {
				return nil
			}
			name := path.Clean(file.Name)
			key := name + "\x00" + file.Hash.String()
			if seen[key] {
				return nil
			}
			seen[key] = true
			reader, err := file.Reader()
			if err != nil {
				return nil
			}
			defer reader.Close()
			data, err := io.ReadAll(io.LimitReader(reader, maxTemplateHistoryYAML+1))
			if err != nil {
				return nil
			}
			history[name] = append(history[name], data)
			return nil
		})
		return nil
	})
	if len(history) == 0 {
		return nil
	}
	return history
}

// withoutStaleTemplateComments removes stale template wording that a merge
// appended after the target's current inline comment. It returns merged
// unchanged when nothing applies or when the cleanup cannot be proven to be a
// comment-only change.
func withoutStaleTemplateComments(name string, merged, target []byte, owned map[string]map[string]bool) []byte {
	cleaned := stripStaleTemplateComments(merged, target, owned)
	if bytes.Equal(cleaned, merged) || !commentOnlyChange(name, merged, cleaned) {
		return merged
	}
	return cleaned
}

// commentOnlyChange reports whether after carries exactly the YAML values of
// before, so only comments or whitespace differ.
func commentOnlyChange(name string, before, after []byte) bool {
	beforeDoc, err := parseYAMLDocument(name, before)
	if err != nil {
		return false
	}
	afterDoc, err := parseYAMLDocument(name, after)
	if err != nil {
		return false
	}
	return semanticYAMLNodeEqual(documentRoot(afterDoc.node), documentRoot(beforeDoc.node))
}

// stripStaleTemplateComments acts only where duplication actually happened:
// an active line whose inline comment starts with the target's current comment
// for that key, verbatim, followed by fragments that mergeRawInlineComments
// appended from the local file. Appended fragments that a historical template
// wrote on the same key path, in any CloudOps file (a local file can carry the
// wording of a different deployment scaffold), are stale template wording and
// are dropped; operator
// notes stay. Lines the renderer kept verbatim, local wording equal to the
// target's, and keys the target no longer declares never match, so they keep
// their comments. The target comment itself is kept byte-for-byte, including
// semicolons used as punctuation.
func stripStaleTemplateComments(merged, target []byte, owned map[string]map[string]bool) []byte {
	if len(merged) == 0 || len(owned) == 0 {
		return merged
	}
	current := rawKeyInlineComments(splitYAMLLines(target))
	lines := splitYAMLLines(merged)
	changed := false
	for spanPath, span := range yamlRawSpans(lines) {
		targetComment, stale := current[spanPath], owned[spanPath]
		if span.commented || targetComment == "" || len(stale) == 0 || span.start < 0 || span.start >= len(lines) {
			continue
		}
		value, comment := splitRawInlineComment(lines[span.start])
		comment = strings.TrimSpace(comment)
		if len(comment) <= len(targetComment) || !strings.HasPrefix(comment, targetComment) {
			continue
		}
		// The target comment must end at a fragment boundary: `# a` is not
		// the prefix of an operator's `# ab; # x`.
		if !strings.HasPrefix(strings.TrimLeft(comment[len(targetComment):], " \t"), ";") {
			continue
		}
		currentFragments := make(map[string]bool)
		for _, fragment := range rawInlineCommentFragments(targetComment) {
			currentFragments[fragment.normalized] = true
		}
		rebuilt, removed := targetComment, false
		for _, fragment := range rawInlineCommentFragments(comment[len(targetComment):]) {
			if stale[fragment.normalized] && !currentFragments[fragment.normalized] {
				removed = true
				continue
			}
			rebuilt += "; # " + fragment.normalized
		}
		if !removed {
			continue
		}
		// value keeps its trailing spaces: the merge's comment alignment.
		lines[span.start] = value + rebuilt
		changed = true
	}
	if !changed {
		return merged
	}
	return restoreTrailingLineFeeds([]byte(strings.Join(lines, "\n")), trailingLineFeedCount(bytes.ReplaceAll(merged, []byte("\r\n"), []byte("\n"))))
}

// rawKeyInlineComments maps each raw key path to its trimmed inline comment,
// reading a commented key as if it were active.
func rawKeyInlineComments(lines []string) map[string]string {
	comments := make(map[string]string)
	for spanPath, span := range yamlRawSpans(lines) {
		if span.start < 0 || span.start >= len(lines) {
			continue
		}
		line := lines[span.start]
		if span.commented {
			line = uncommentStructuralLine(line)
		}
		if _, comment := splitRawInlineComment(line); strings.TrimSpace(comment) != "" {
			comments[spanPath] = strings.TrimSpace(comment)
		}
	}
	return comments
}

// templateOwnedCommentFragments maps each raw key path to the normalized inline
// comment fragments any historical template file wrote on that key. Files are
// pooled: the same key path means the same setting in every scaffold. Nil
// history yields nil.
func templateOwnedCommentFragments(history map[string][][]byte) map[string]map[string]bool {
	if len(history) == 0 {
		return nil
	}
	owned := make(map[string]map[string]bool)
	for _, documents := range history {
		for _, document := range documents {
			for spanPath, comment := range rawKeyInlineComments(splitYAMLLines(document)) {
				for _, fragment := range rawInlineCommentFragments(comment) {
					if owned[spanPath] == nil {
						owned[spanPath] = make(map[string]bool)
					}
					owned[spanPath][fragment.normalized] = true
				}
			}
		}
	}
	return owned
}
