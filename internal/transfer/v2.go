package transfer

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

const (
	Format            = "bootagent-transfer"
	Version           = 2
	MaxArchiveBytes   = 256 << 20
	MaxArchiveEntries = 20_000
)

type Manifest struct {
	Format   string   `json:"format"`
	Version  int      `json:"version"`
	Sections []string `json:"sections"`
}

type Package struct {
	Manifest Manifest
	Files    map[string][]byte
}

func Build(files map[string][]byte) ([]byte, error) {
	sections := make([]string, 0, len(files))
	for name := range files {
		if err := validPath(name); err != nil {
			return nil, err
		}
		if name == "manifest.json" {
			return nil, errors.New("manifest.json is reserved")
		}
		sections = append(sections, name)
	}
	sort.Strings(sections)
	manifest := Manifest{Format: Format, Version: Version, Sections: sections}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	write := func(name string, data []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if err := write("manifest.json", append(manifestBytes, '\n')); err != nil {
		return nil, err
	}
	for _, name := range sections {
		data := files[name]
		if err := write(name, data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if out.Len() > MaxArchiveBytes {
		return nil, errors.New("transfer archive exceeds size limit")
	}
	return out.Bytes(), nil
}

func Parse(data []byte) (Package, error) {
	if len(data) > MaxArchiveBytes {
		return Package{}, errors.New("transfer archive exceeds size limit")
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Package{}, errors.New("transfer archive is invalid")
	}
	if len(r.File) > MaxArchiveEntries {
		return Package{}, errors.New("transfer archive exceeds entry limit")
	}
	files := make(map[string][]byte, len(r.File))
	var manifest Manifest
	for _, entry := range r.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		if err := validPath(entry.Name); err != nil {
			return Package{}, err
		}
		if _, exists := files[entry.Name]; exists {
			return Package{}, errors.New("transfer archive contains duplicate paths")
		}
		if entry.UncompressedSize64 > MaxArchiveBytes {
			return Package{}, errors.New("transfer entry exceeds size limit")
		}
		reader, err := entry.Open()
		if err != nil {
			return Package{}, err
		}
		content, err := io.ReadAll(io.LimitReader(reader, MaxArchiveBytes+1))
		_ = reader.Close()
		if err != nil {
			return Package{}, err
		}
		if len(content) > MaxArchiveBytes {
			return Package{}, errors.New("transfer entry exceeds size limit")
		}
		files[entry.Name] = content
	}
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil || manifest.Format != Format || manifest.Version != Version {
		return Package{}, errors.New("unsupported transfer package version")
	}
	return Package{Manifest: manifest, Files: files}, nil
}

func validPath(name string) error {
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || clean != name || path.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || strings.ContainsRune(name, '\x00') {
		return fmt.Errorf("unsafe transfer path %q", name)
	}
	return nil
}
