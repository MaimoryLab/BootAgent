package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxExportBytes = 128 << 20

// ExtractArchive validates and extracts a single Skill archive into dest.
// The destination is caller-owned and is never an Agent directory.
func ExtractArchive(ctx context.Context, data []byte, dest string) (ExportManifest, error) {
	if len(data) == 0 || len(data) > maxExportBytes {
		return ExportManifest{}, errors.New("Skill archive exceeds size limit")
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ExportManifest{}, errors.New("Skill archive is invalid")
	}
	var manifest ExportManifest
	seen := map[string]bool{}
	total := int64(0)
	files := 0
	for _, entry := range r.File {
		name := filepath.ToSlash(entry.Name)
		clean := filepath.Clean(filepath.FromSlash(name))
		if name == "" || clean != filepath.FromSlash(name) || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.ContainsRune(name, '\x00') {
			return ExportManifest{}, errors.New("Skill archive contains unsafe path")
		}
		if seen[name] {
			return ExportManifest{}, errors.New("Skill archive contains duplicate paths")
		}
		seen[name] = true
		if name == "manifest.json" {
			reader, e := entry.Open()
			if e != nil {
				return ExportManifest{}, e
			}
			raw, e := io.ReadAll(io.LimitReader(reader, maxMetadataBytes+1))
			_ = reader.Close()
			if e != nil || len(raw) > maxMetadataBytes || json.Unmarshal(raw, &manifest) != nil {
				return ExportManifest{}, errors.New("Skill archive manifest is invalid")
			}
			continue
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 || !entry.FileInfo().Mode().IsRegular() {
			return ExportManifest{}, errors.New("Skill archive contains unsupported file type")
		}
		if entry.UncompressedSize64 > uint64(maxExportBytes-total) {
			return ExportManifest{}, errors.New("Skill archive exceeds size limit")
		}
		reader, e := entry.Open()
		if e != nil {
			return ExportManifest{}, e
		}
		content, e := io.ReadAll(io.LimitReader(reader, maxExportBytes-total+1))
		_ = reader.Close()
		if e != nil || int64(len(content)) > maxExportBytes-total {
			return ExportManifest{}, errors.New("Skill archive exceeds size limit")
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return ExportManifest{}, err
		}
		if err := os.WriteFile(target, content, 0o600); err != nil {
			return ExportManifest{}, err
		}
		total += int64(len(content))
		files++
		if err := contextError(ctx); err != nil {
			return ExportManifest{}, err
		}
	}
	if manifest.Format != "bootagent-skill" || manifest.Version != 1 || manifest.ID == "" {
		return ExportManifest{}, errors.New("Skill archive manifest is invalid")
	}
	if err := ValidateID(manifest.ID); err != nil {
		return ExportManifest{}, err
	}
	manifest.Variant.Files = files
	manifest.Variant.Bytes = total
	return manifest, nil
}

type ExportManifest struct {
	Format      string        `json:"format"`
	Version     int           `json:"version"`
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Variant     ExportVariant `json:"variant"`
}

type ExportVariant struct {
	Hash  string `json:"hash"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// Export creates a portable single-Skill archive. It never includes registry
// paths, credentials, symlinks, or files outside the selected variant.
func (s Store) Export(ctx context.Context, id, hash string) ([]byte, error) {
	if err := ValidateID(id); err != nil || !hashPattern.MatchString(hash) {
		return nil, errors.New("invalid Skill export target")
	}
	registry, err := s.Load()
	if err != nil {
		return nil, err
	}
	fact, ok := registry.Skills[id]
	if !ok {
		return nil, errors.New("Skill was not found")
	}
	path := s.VariantPath(id, hash)
	stats, err := HashTree(ctx, path)
	if err != nil {
		return nil, err
	}
	if !s.hasVariant(fact, hash) || stats.Hash != hash {
		return nil, errors.New("Skill variant is not registered")
	}
	manifest := ExportManifest{Format: "bootagent-skill", Version: 1, ID: id, Name: fact.Name, Description: fact.Description, Variant: ExportVariant{Hash: hash, Files: stats.Files, Bytes: stats.Bytes}}
	return zipTree(ctx, path, manifest)
}

func (s Store) hasVariant(fact Fact, hash string) bool {
	for _, variant := range fact.Variants {
		if variant.Hash == hash && variant.Stored {
			return true
		}
	}
	return false
}

func zipTree(ctx context.Context, root string, manifest ExportManifest) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	part, err := writer.Create("manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(append(manifestBytes, '\n')); err != nil {
		return nil, err
	}
	var files []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := contextError(ctx); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("Skill tree contains unsupported file type")
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	for _, path := range files {
		name, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		name = filepath.ToSlash(name)
		if name == "manifest.json" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if int64(buffer.Len())+info.Size() > maxExportBytes {
			return nil, errors.New("Skill export exceeds size limit")
		}
		entry, err := writer.Create(name)
		if err != nil {
			return nil, err
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		_, copyErr := io.Copy(entry, file)
		closeErr := file.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if buffer.Len() > maxExportBytes {
		return nil, fmt.Errorf("Skill export exceeds %d bytes", maxExportBytes)
	}
	return buffer.Bytes(), nil
}
