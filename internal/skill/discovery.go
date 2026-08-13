package skill

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	maxZIPCompressed = 128 << 20
	maxZIPEntries    = 10_000
)

var renamePath = os.Rename

func ScanAgentRoot(ctx context.Context, root, source string) ([]Candidate, error) {
	root = filepath.Clean(root)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	result := make([]Candidate, 0, len(entries))
	for _, entry := range entries {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), "SKILL.md")); err != nil {
			continue
		}
		candidate, err := candidateFromDir(ctx, filepath.Join(root, entry.Name()), source)
		if err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, nil
}

func DiscoverFolder(ctx context.Context, root string) ([]Candidate, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("selected source is not a directory")
	}
	if _, err := os.Stat(filepath.Join(root, "SKILL.md")); err == nil {
		candidate, err := candidateFromDir(ctx, root, "folder")
		return []Candidate{candidate}, err
	}
	var result []Candidate
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := contextError(ctx); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
			return nil
		}
		candidate, err := candidateFromDir(ctx, path, "folder")
		if err != nil {
			return err
		}
		result = append(result, candidate)
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func DiscoverZIP(ctx context.Context, zipPath, stagingParent string) ([]Candidate, error) {
	info, err := os.Stat(zipPath)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxZIPCompressed {
		return nil, errors.New("zip archive exceeds compressed size limit")
	}
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	if len(archive.File) > maxZIPEntries {
		return nil, errors.New("zip archive exceeds entry limit")
	}
	root, err := os.MkdirTemp(stagingParent, ".oneagent-skill-zip-")
	if err != nil {
		return nil, err
	}
	keepRoot := false
	defer func() {
		if !keepRoot {
			_ = os.RemoveAll(root)
		}
	}()
	seen := make(map[string]struct{}, len(archive.File))
	var compressed, expanded int64
	for _, entry := range archive.File {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		name, isDir, err := safeZIPName(entry.Name)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[name]; exists {
			return nil, errors.New("zip archive contains duplicate paths")
		}
		seen[name] = struct{}{}
		compressed += int64(entry.CompressedSize64)
		if compressed > maxZIPCompressed {
			return nil, errors.New("zip archive exceeds compressed size limit")
		}
		fileType := entry.Mode() & os.ModeType
		if (isDir && fileType != os.ModeDir) || (!isDir && fileType != 0) {
			return nil, errors.New("zip archive contains unsupported file type")
		}
		target, err := safeJoin(root, name)
		if err != nil {
			return nil, err
		}
		if isDir {
			if err := os.MkdirAll(target, 0700); err != nil {
				return nil, err
			}
			continue
		}
		if entry.UncompressedSize64 > uint64(maxTreeBytes) || expanded > maxTreeBytes-int64(entry.UncompressedSize64) {
			return nil, errors.New("zip archive exceeds expanded size limit")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, err
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			rc.Close()
			return nil, err
		}
		written, copyErr := io.CopyN(f, rc, int64(entry.UncompressedSize64))
		closeErr := f.Close()
		rc.Close()
		if copyErr != nil && !(copyErr == io.EOF && written == int64(entry.UncompressedSize64)) {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		expanded += written
	}
	result, err := DiscoverFolder(ctx, root)
	if err != nil {
		return nil, err
	}
	for i := range result {
		result[i].Source = "zip"
		result[i].cleanupRoot = root
	}
	keepRoot = len(result) > 0
	return result, nil
}

// CleanupCandidates removes temporary extraction trees returned by DiscoverZIP.
// Candidates from agent roots and folders are left untouched.
func CleanupCandidates(candidates []Candidate) error {
	removed := make(map[string]struct{})
	for _, candidate := range candidates {
		root := candidate.cleanupRoot
		if root == "" {
			continue
		}
		if _, ok := removed[root]; !ok {
			if err := os.RemoveAll(root); err != nil {
				return err
			}
			removed[root] = struct{}{}
		}
	}
	return nil
}

func CopyTree(ctx context.Context, source, destination string) error {
	source = filepath.Clean(source)
	destination = filepath.Clean(destination)
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("skill source is not a regular directory")
	}
	if existing, err := os.Lstat(destination); err == nil {
		if existing.Mode()&os.ModeSymlink != 0 || !existing.IsDir() {
			return errors.New("skill destination is not a regular directory")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		return err
	}
	return copyDir(ctx, source, destination)
}

func PublishTree(ctx context.Context, source, destination string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	destination = filepath.Clean(destination)
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".oneagent-skill-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := CopyTree(ctx, source, stage); err != nil {
		return err
	}
	if info, err := os.Lstat(destination); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("skill destination is not a regular directory")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	rollback := filepath.Join(parent, fmt.Sprintf(".oneagent-rollback-%d-%d", time.Now().UnixNano(), rand.Int63()))
	hadDestination := false
	if _, err := os.Lstat(destination); err == nil {
		hadDestination = true
		if err := renamePath(destination, rollback); err != nil {
			return err
		}
	}
	if err := renamePath(stage, destination); err != nil {
		if hadDestination {
			if restoreErr := renamePath(rollback, destination); restoreErr != nil {
				return fmt.Errorf("publish failed: %v; rollback restore failed: %w", err, restoreErr)
			}
		}
		return err
	}
	if hadDestination {
		_ = os.RemoveAll(rollback)
	}
	return nil
}

func candidateFromDir(ctx context.Context, root, source string) (Candidate, error) {
	id := filepath.Base(root)
	candidate := Candidate{ID: id, Source: source, Path: root}
	stats, err := HashTree(ctx, root)
	if err != nil {
		candidate.Diagnostic = "skill tree unavailable"
		return candidate, nil
	}
	candidate.Hash, candidate.Files, candidate.Bytes = stats.Hash, stats.Files, stats.Bytes
	candidate.Name, candidate.Description, candidate.Diagnostic = ReadMetadata(ctx, root, id)
	if err := ValidateID(id); err != nil {
		candidate.Diagnostic = "invalid Skill ID"
	}
	return candidate, nil
}

func copyDir(ctx context.Context, source, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := contextError(ctx); err != nil {
			return err
		}
		src := filepath.Join(source, entry.Name())
		dst, err := safeJoin(destination, entry.Name())
		if err != nil {
			return err
		}
		info, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("skill tree contains unsupported file type")
		}
		if info.IsDir() {
			if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
				return err
			}
			if err := copyDir(ctx, src, dst); err != nil {
				return err
			}
			continue
		}
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeIn, closeOut := in.Close(), out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeIn != nil {
			return closeIn
		}
		if closeOut != nil {
			return closeOut
		}
	}
	return nil
}

func safeZIPName(raw string) (string, bool, error) {
	if raw == "" || strings.IndexByte(raw, 0) >= 0 || strings.Contains(raw, `\`) {
		return "", false, errors.New("zip archive contains invalid path")
	}
	if len(raw) >= 2 && raw[1] == ':' && ((raw[0] >= 'A' && raw[0] <= 'Z') || (raw[0] >= 'a' && raw[0] <= 'z')) {
		return "", false, errors.New("zip archive contains unsafe path")
	}
	isDir := strings.HasSuffix(raw, "/")
	if slices.Contains(strings.Split(strings.TrimSuffix(raw, "/"), "/"), "..") {
		return "", false, errors.New("zip archive contains unsafe path")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(raw)))
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", false, errors.New("zip archive contains unsafe path")
	}
	return strings.TrimSuffix(clean, "/"), isDir, nil
}

func safeJoin(root, rel string) (string, error) {
	target := filepath.Join(root, filepath.FromSlash(rel))
	check, err := filepath.Rel(root, target)
	if err != nil || check == ".." || strings.HasPrefix(check, ".."+string(filepath.Separator)) || filepath.IsAbs(check) {
		return "", errors.New("path escapes staging directory")
	}
	return target, nil
}
