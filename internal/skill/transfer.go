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
)

const maxExportBytes = 128 << 20

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
