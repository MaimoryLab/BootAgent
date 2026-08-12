// Package skill contains the local Skills registry model and filesystem identity helpers.
package skill

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	RegistrySchemaVersion = 1
	MaxSkillIDLength      = 128
	maxTreeFiles          = 10_000
	maxTreeBytes          = 512 << 20
	maxMetadataBytes      = 64 << 10
)

type Registry struct {
	SchemaVersion int             `json:"schema_version"`
	Skills        map[string]Fact `json:"skills"`
}

type Fact struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Variants    []Variant `json:"variants"`
}

type Variant struct {
	Hash           string   `json:"hash"`
	Stored         bool     `json:"stored"`
	ObservedAgents []string `json:"observed_agents"`
	ImportSources  []string `json:"import_sources"`
	ManagedTargets []string `json:"managed_targets"`
}

type TreeStats struct {
	Hash  string `json:"hash"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

type Candidate struct {
	ID, Name, Description, Hash, Source string
	Files                               int
	Bytes                               int64
	Diagnostic, Path                    string
}

var skillIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func ValidateID(id string) error {
	if id == "" || len(id) > MaxSkillIDLength || !skillIDPattern.MatchString(id) || filepath.IsAbs(id) || filepath.VolumeName(id) != "" {
		return errors.New("invalid Skill ID")
	}
	if strings.ContainsAny(id, `/\\`) || id == "." || id == ".." || strings.HasPrefix(id, ".."+string(filepath.Separator)) {
		return errors.New("invalid Skill ID")
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return errors.New("invalid Skill ID")
		}
	}
	upper := strings.ToUpper(id)
	switch upper {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return errors.New("invalid Skill ID")
	}
	return nil
}

type treeEntry struct {
	path string
	mode os.FileMode
	info os.FileInfo
}

func HashTree(ctx context.Context, root string) (TreeStats, error) {
	root = filepath.Clean(root)
	if err := contextError(ctx); err != nil {
		return TreeStats{}, err
	}
	entries := make([]treeEntry, 0, 32)
	var total int64
	var files int
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err := contextError(ctx); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			if !info.IsDir() {
				return errors.New("skill root is not a directory")
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill tree contains symlink %q", filepath.ToSlash(rel))
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("skill tree contains special file %q", filepath.ToSlash(rel))
		}
		if !info.IsDir() {
			files++
			if files > maxTreeFiles || info.Size() > maxTreeBytes-total {
				return errors.New("skill tree exceeds size limits")
			}
			total += info.Size()
		}
		entries = append(entries, treeEntry{path: filepath.ToSlash(rel), mode: info.Mode(), info: info})
		return nil
	})
	if err != nil {
		return TreeStats{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	h := sha256.New()
	var buf [8]byte
	for _, entry := range entries {
		if err := contextError(ctx); err != nil {
			return TreeStats{}, err
		}
		if entry.mode.IsDir() {
			_, _ = h.Write([]byte{'d'})
		} else {
			_, _ = h.Write([]byte{'f'})
		}
		binary.BigEndian.PutUint64(buf[:], uint64(len(entry.path)))
		_, _ = h.Write(buf[:])
		_, _ = h.Write([]byte(entry.path))
		length := int64(0)
		if entry.mode.IsRegular() {
			length = maxInt64(entry.info.Size(), 0)
		}
		binary.BigEndian.PutUint64(buf[:], uint64(length))
		_, _ = h.Write(buf[:])
		if entry.mode.IsRegular() {
			f, err := os.Open(filepath.Join(root, filepath.FromSlash(entry.path)))
			if err != nil {
				return TreeStats{}, err
			}
			_, err = io.Copy(h, f)
			closeErr := f.Close()
			if err != nil {
				return TreeStats{}, err
			}
			if closeErr != nil {
				return TreeStats{}, closeErr
			}
		}
	}
	return TreeStats{Hash: fmt.Sprintf("%x", h.Sum(nil)), Files: countFiles(entries), Bytes: total}, nil
}

func ReadMetadata(ctx context.Context, root, fallbackID string) (name, description, diagnostic string) {
	name = fallbackID
	if err := contextError(ctx); err != nil {
		return name, "", "metadata unavailable"
	}
	b, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		return name, "", "metadata unavailable"
	}
	if len(b) > maxMetadataBytes {
		return name, "", "metadata exceeds size limit"
	}
	text := string(b)
	if !strings.HasPrefix(text, "---\n") {
		return name, "", "metadata front matter missing"
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return name, "", "metadata front matter invalid"
	}
	var front struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &front); err != nil {
		return name, "", "metadata front matter invalid"
	}
	if strings.TrimSpace(front.Name) != "" {
		name = trimBytes(strings.TrimSpace(front.Name), 256)
	}
	description = trimBytes(strings.TrimSpace(front.Description), 1024)
	return name, description, ""
}

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func countFiles(entries []treeEntry) int {
	n := 0
	for _, entry := range entries {
		if entry.mode.IsRegular() {
			n++
		}
	}
	return n
}

func maxInt64(value, fallback int64) int64 {
	if value < fallback {
		return fallback
	}
	return value
}

func trimBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := value[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
