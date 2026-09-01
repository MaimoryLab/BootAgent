// Package transfer contains versioned import and export formats for local configuration.
package transfer

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
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

type Preview struct {
	Providers int      `json:"providers"`
	Profiles  int      `json:"profiles"`
	MCP       int      `json:"mcp"`
	Skills    []string `json:"skills"`
	// SkillConflicts lists duplicate Skill IDs in the package. Existing local
	// library conflicts are resolved by the confirmation/apply layer.
	SkillConflicts []string `json:"skill_conflicts,omitempty"`
}

// PreviewPackage validates all declared resources, including nested Skill ZIPs,
// without touching the user's configuration. It is intentionally stricter than
// Parse so an import confirmation never describes a package that cannot apply.
func PreviewPackage(data []byte) (Preview, Package, error) {
	pkg, err := Parse(data)
	if err != nil {
		return Preview{}, Package{}, err
	}
	preview := Preview{}
	if raw := pkg.Files["config.json"]; len(raw) > 0 {
		var config map[string]json.RawMessage
		if err := json.Unmarshal(raw, &config); err != nil {
			return Preview{}, Package{}, errors.New("invalid config section")
		}
		if providers := config["providers"]; len(providers) > 0 {
			var p struct {
				Providers []json.RawMessage `json:"providers"`
			}
			if json.Unmarshal(providers, &p) != nil {
				return Preview{}, Package{}, errors.New("invalid providers section")
			}
			preview.Providers = len(p.Providers)
		}
		if profiles := config["profiles"]; len(profiles) > 0 {
			var p []json.RawMessage
			if json.Unmarshal(profiles, &p) != nil {
				return Preview{}, Package{}, errors.New("invalid profiles section")
			}
			preview.Profiles = len(p)
		}
		if mcp := config["mcp"]; len(mcp) > 0 {
			var p map[string]json.RawMessage
			if json.Unmarshal(mcp, &p) != nil {
				return Preview{}, Package{}, errors.New("invalid MCP section")
			}
			if list, ok := p["servers"]; ok {
				var s []json.RawMessage
				if json.Unmarshal(list, &s) == nil {
					preview.MCP = len(s)
				}
			}
		}
	}
	if raw := pkg.Files["providers.json"]; len(raw) > 0 {
		var payload struct {
			Providers []json.RawMessage `json:"providers"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Preview{}, Package{}, errors.New("invalid providers section")
		}
		preview.Providers = len(payload.Providers)
	}
	if raw := pkg.Files["profiles.json"]; len(raw) > 0 {
		var payload []json.RawMessage
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Preview{}, Package{}, errors.New("invalid profiles section")
		}
		preview.Profiles = len(payload)
	}
	if raw := pkg.Files["mcp.json"]; len(raw) > 0 {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Preview{}, Package{}, errors.New("invalid MCP section")
		}
		if servers, ok := payload["servers"]; ok {
			var list []json.RawMessage
			if json.Unmarshal(servers, &list) == nil {
				preview.MCP = len(list)
			}
		}
	}
	for name, raw := range pkg.Files {
		if name == "skills.zip" {
			nested, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
			if e != nil {
				return Preview{}, Package{}, errors.New("invalid skills archive")
			}
			for _, entry := range nested.File {
				if entry.FileInfo().IsDir() || !strings.HasSuffix(entry.Name, ".skill.zip") {
					continue
				}
				if err := validPath(entry.Name); err != nil {
					return Preview{}, Package{}, err
				}
				reader, e := entry.Open()
				if e != nil {
					return Preview{}, Package{}, e
				}
				content, e := io.ReadAll(reader)
				_ = reader.Close()
				if e != nil {
					return Preview{}, Package{}, e
				}
				if _, e = zip.NewReader(bytes.NewReader(content), int64(len(content))); e != nil {
					return Preview{}, Package{}, fmt.Errorf("invalid nested Skill archive %q", entry.Name)
				}
				id := strings.TrimSuffix(path.Base(entry.Name), ".skill.zip")
				if slices.Contains(preview.Skills, id) && !slices.Contains(preview.SkillConflicts, id) {
					preview.SkillConflicts = append(preview.SkillConflicts, id)
				}
				preview.Skills = append(preview.Skills, id)
				pkg.Files["skills/"+id+".skill.zip"] = content
			}
			continue
		}
		if !strings.HasPrefix(name, "skills/") || !strings.HasSuffix(name, ".skill.zip") {
			continue
		}
		if _, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw))); err != nil {
			return Preview{}, Package{}, fmt.Errorf("invalid nested Skill archive %q", name)
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, "skills/"), ".skill.zip")
		if slices.Contains(preview.Skills, id) && !slices.Contains(preview.SkillConflicts, id) {
			preview.SkillConflicts = append(preview.SkillConflicts, id)
		}
		preview.Skills = append(preview.Skills, id)
	}
	sort.Strings(preview.Skills)
	sort.Strings(preview.SkillConflicts)
	return preview, pkg, nil
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
