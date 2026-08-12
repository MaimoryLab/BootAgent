package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/securefs"
)

const backupRetention = 20

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Store struct {
	home string
	fs   securefs.Store
}

type BackupSummary struct {
	ID        string `json:"id"`
	BackupID  string `json:"backup_id"`
	CreatedAt string `json:"created_at"`
	Variants  int    `json:"variants"`
}

type backupMetadata struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	CreatedAt     string `json:"created_at"`
	Fact          Fact   `json:"fact"`
}

func NewStore(home string, fs securefs.Store) Store { return Store{home: home, fs: fs} }

func (s Store) RegistryPath() string {
	return filepath.Join(s.home, ".oneagent", "skill-registry.json")
}
func (s Store) SkillsRoot() string { return filepath.Join(s.home, ".oneagent", "skills") }
func (s Store) BackupRoot() string { return filepath.Join(s.home, ".oneagent", "skill-backups") }
func (s Store) VariantPath(id, hash string) string {
	return filepath.Join(s.SkillsRoot(), id, "variants", hash)
}

func (s Store) Load() (Registry, error) {
	empty := Registry{SchemaVersion: RegistrySchemaVersion, Skills: map[string]Fact{}}
	b, err := os.ReadFile(s.RegistryPath())
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return Registry{}, fmt.Errorf("cannot read Skill registry: %w", err)
	}
	var registry Registry
	if json.Unmarshal(b, &registry) != nil {
		return Registry{}, errors.New("Skill registry is invalid")
	}
	if err := validateRegistry(registry); err != nil {
		return Registry{}, err
	}
	return registry, nil
}

func (s Store) Save(ctx context.Context, registry Registry) error {
	if err := validateRegistry(registry); err != nil {
		return err
	}
	b, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	_, err = s.fs.AtomicWrite(ctx, s.RegistryPath(), append(b, '\n'), true)
	return err
}

func (s Store) SaveVariant(ctx context.Context, id, source string, stats TreeStats) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if !hashPattern.MatchString(stats.Hash) {
		return errors.New("invalid Skill hash")
	}
	actual, err := HashTree(ctx, source)
	if err != nil {
		return err
	}
	if actual != stats {
		return errors.New("Skill tree changed before publication")
	}
	if err := s.fs.EnsurePrivateDir(ctx, s.SkillsRoot()); err != nil {
		return err
	}
	return s.publishPrivate(ctx, source, s.VariantPath(id, stats.Hash))
}

func (s Store) RemoveSkill(ctx context.Context, id string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.SkillsRoot(), id))
}

func (s Store) CreateBackup(ctx context.Context, id string, fact Fact) (summary BackupSummary, err error) {
	if err = validateFact(id, fact); err != nil {
		return summary, err
	}
	if err = s.fs.EnsurePrivateDir(ctx, s.BackupRoot()); err != nil {
		return summary, err
	}
	created := time.Now().UTC()
	prefix := created.Format("20060102T150405.000000000Z") + "-" + id + "-"
	dir, err := os.MkdirTemp(s.BackupRoot(), prefix)
	if err != nil {
		return summary, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(dir)
		}
	}()
	if err = s.fs.EnsurePrivateDir(ctx, dir); err != nil {
		return summary, err
	}
	if err = CopyTree(ctx, filepath.Join(s.SkillsRoot(), id), filepath.Join(dir, "content")); err != nil {
		return summary, err
	}
	if err = s.secureTree(ctx, filepath.Join(dir, "content")); err != nil {
		return summary, err
	}
	metadata := backupMetadata{SchemaVersion: RegistrySchemaVersion, ID: id, CreatedAt: created.Format(time.RFC3339Nano), Fact: fact}
	b, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return summary, err
	}
	if _, err = s.fs.AtomicWrite(ctx, filepath.Join(dir, "metadata.json"), append(b, '\n'), true); err != nil {
		return summary, err
	}
	complete = true
	summary = BackupSummary{ID: id, BackupID: filepath.Base(dir), CreatedAt: metadata.CreatedAt, Variants: len(fact.Variants)}
	return summary, s.pruneBackups()
}

func (s Store) ListBackups() ([]BackupSummary, error) {
	entries, err := os.ReadDir(s.BackupRoot())
	if os.IsNotExist(err) {
		return []BackupSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]BackupSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metadata, err := s.loadBackup(entry.Name())
		if err != nil {
			return nil, err
		}
		result = append(result, BackupSummary{ID: metadata.ID, BackupID: entry.Name(), CreatedAt: metadata.CreatedAt, Variants: len(metadata.Fact.Variants)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].BackupID > result[j].BackupID })
	return result, nil
}

func (s Store) RestoreBackup(ctx context.Context, backupID string) (Fact, error) {
	metadata, err := s.loadBackup(backupID)
	if err != nil {
		return Fact{}, err
	}
	type restoreItem struct{ source, destination string }
	items := make([]restoreItem, 0, len(metadata.Fact.Variants))
	for _, variant := range metadata.Fact.Variants {
		if !variant.Stored {
			continue
		}
		source := filepath.Join(s.BackupRoot(), backupID, "content", "variants", variant.Hash)
		if _, err := os.Stat(filepath.Join(source, "SKILL.md")); err != nil {
			return Fact{}, errors.New("Skill backup is incomplete")
		}
		stats, err := HashTree(ctx, source)
		if err != nil || stats.Hash != variant.Hash {
			return Fact{}, errors.New("Skill backup content is invalid")
		}
		items = append(items, restoreItem{source, s.VariantPath(metadata.ID, variant.Hash)})
	}
	if err := s.fs.EnsurePrivateDir(ctx, s.SkillsRoot()); err != nil {
		return Fact{}, err
	}
	for _, item := range items {
		if err := s.publishPrivate(ctx, item.source, item.destination); err != nil {
			return Fact{}, err
		}
	}
	return metadata.Fact, nil
}

func validateRegistry(registry Registry) error {
	if registry.SchemaVersion != RegistrySchemaVersion {
		return fmt.Errorf("unsupported Skill registry schema version %d", registry.SchemaVersion)
	}
	if registry.Skills == nil {
		return errors.New("Skill registry entries are missing")
	}
	for id, fact := range registry.Skills {
		if err := validateFact(id, fact); err != nil {
			return err
		}
	}
	return nil
}

func validateFact(id string, fact Fact) error {
	if err := ValidateID(id); err != nil {
		return fmt.Errorf("invalid Skill ID %q: %w", id, err)
	}
	seen := map[string]bool{}
	for _, variant := range fact.Variants {
		if !hashPattern.MatchString(variant.Hash) || seen[variant.Hash] {
			return fmt.Errorf("Skill %q has invalid variant hash", id)
		}
		seen[variant.Hash] = true
		for _, values := range [][]string{variant.ObservedAgents, variant.ImportSources, variant.ManagedTargets} {
			if !sortedUnique(values) {
				return fmt.Errorf("Skill %q has invalid association list", id)
			}
		}
	}
	return nil
}

func sortedUnique(values []string) bool {
	for i, value := range values {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) || i > 0 && values[i-1] >= value {
			return false
		}
	}
	return true
}

func (s Store) loadBackup(backupID string) (backupMetadata, error) {
	if backupID == "" || backupID == "." || backupID == ".." || backupID != filepath.Base(backupID) || strings.ContainsAny(backupID, `/\\`) {
		return backupMetadata{}, errors.New("invalid Skill backup ID")
	}
	b, err := os.ReadFile(filepath.Join(s.BackupRoot(), backupID, "metadata.json"))
	if err != nil {
		return backupMetadata{}, err
	}
	var metadata backupMetadata
	if json.Unmarshal(b, &metadata) != nil || metadata.SchemaVersion != RegistrySchemaVersion || metadata.CreatedAt == "" {
		return backupMetadata{}, errors.New("Skill backup metadata is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, metadata.CreatedAt); err != nil {
		return backupMetadata{}, errors.New("Skill backup metadata is invalid")
	}
	if err := validateFact(metadata.ID, metadata.Fact); err != nil {
		return backupMetadata{}, err
	}
	return metadata, nil
}

func (s Store) publishPrivate(ctx context.Context, source, destination string) error {
	parent := filepath.Dir(destination)
	for _, path := range []string{s.SkillsRoot(), filepath.Dir(parent), parent} {
		if err := s.fs.EnsurePrivateDir(ctx, path); err != nil {
			return err
		}
	}
	stage, err := os.MkdirTemp(parent, ".oneagent-private-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := CopyTree(ctx, source, stage); err != nil {
		return err
	}
	if err := s.secureTree(ctx, stage); err != nil {
		return err
	}
	return PublishTree(ctx, stage, destination)
}

func (s Store) secureTree(ctx context.Context, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return s.fs.EnsurePrivateDir(ctx, path)
		}
		return s.fs.SecureFile(ctx, path)
	})
}

func (s Store) pruneBackups() error {
	backups, err := s.ListBackups()
	if err != nil {
		return err
	}
	if len(backups) <= backupRetention {
		return nil
	}
	for _, backup := range backups[backupRetention:] {
		if err := os.RemoveAll(filepath.Join(s.BackupRoot(), backup.BackupID)); err != nil {
			return err
		}
	}
	return nil
}
