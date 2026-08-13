package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

const maxRegistryBytes = 8 << 20

const (
	maxAssociationValues     = 64
	maxAssociationValueBytes = 128
)

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
	return filepath.Join(s.home, ".bootagent", "skill-registry.json")
}
func (s Store) SkillsRoot() string { return filepath.Join(s.home, ".bootagent", "skills") }
func (s Store) BackupRoot() string {
	root := s.fs.BackupRoot()
	if root == "" {
		root = filepath.Join(s.home, ".bootagent", "backup")
	}
	return filepath.Join(root, "skills")
}
func (s Store) LegacyBackupRoot() string         { return filepath.Join(s.home, ".bootagent", "skill-backups") }
func (s Store) skillBackupRoot(id string) string { return filepath.Join(s.BackupRoot(), id) }
func (s Store) VariantPath(id, hash string) string {
	return filepath.Join(s.SkillsRoot(), id, "variants", hash)
}

func (s Store) Load() (Registry, error) {
	empty := Registry{SchemaVersion: RegistrySchemaVersion, Skills: map[string]Fact{}}
	b, err := readBoundedFile(s.RegistryPath(), maxRegistryBytes)
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
	if err := rejectSymlinkComponents(s.home, s.RegistryPath()); err != nil {
		return err
	}
	if _, err := os.Lstat(s.RegistryPath()); err == nil {
		if _, err := s.Load(); err != nil {
			return fmt.Errorf("refusing to overwrite invalid Skill registry: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	b, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	if len(b)+1 > maxRegistryBytes {
		return errors.New("Skill registry exceeds size limit")
	}
	payload := append(b, '\n')
	_, err = s.fs.AtomicWrite(ctx, s.RegistryPath(), payload, true)
	return err
}

func (s Store) SaveVariant(ctx context.Context, id, source string, stats TreeStats) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if !hashPattern.MatchString(stats.Hash) {
		return errors.New("invalid Skill hash")
	}
	if err := s.ensurePrivateRoot(ctx, s.SkillsRoot()); err != nil {
		return err
	}
	parent := filepath.Join(s.SkillsRoot(), id, "variants")
	if err := s.ensurePrivateDir(ctx, parent); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".bootagent-private-")
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
	actual, err := HashTree(ctx, stage)
	if err != nil {
		return err
	}
	if actual != stats {
		return errors.New("Skill tree changed before publication")
	}
	return PublishTree(ctx, stage, s.VariantPath(id, stats.Hash))
}

func (s Store) RemoveSkill(ctx context.Context, id string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := rejectSymlinkComponents(s.home, s.SkillsRoot()); err != nil {
		return err
	}
	target := filepath.Join(s.SkillsRoot(), id)
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("Skill storage path is not a regular directory")
	}
	return os.RemoveAll(target)
}

func (s Store) CreateBackup(ctx context.Context, id string, fact Fact) (summary BackupSummary, err error) {
	if err = validateFact(id, fact); err != nil {
		return summary, err
	}
	if err = rejectSymlinkComponents(s.home, s.SkillsRoot()); err != nil {
		return summary, err
	}
	if err = s.validateStoredFact(ctx, id, fact, filepath.Join(s.SkillsRoot(), id)); err != nil {
		return summary, err
	}
	root := s.skillBackupRoot(id)
	if err = s.ensureSkillBackupRoot(ctx, root); err != nil {
		return summary, err
	}
	created := time.Now().UTC()
	prefix := created.Format("20060102T150405.000000000Z") + "-" + id + "-"
	dir, err := os.MkdirTemp(root, ".bootagent-backup-pending-")
	if err != nil {
		return summary, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(dir)
			if entries, readErr := os.ReadDir(root); readErr == nil && len(entries) == 0 {
				_ = os.Remove(root)
			}
		}
	}()
	if err = s.ensurePrivateDir(ctx, dir); err != nil {
		return summary, err
	}
	if err = CopyTree(ctx, filepath.Join(s.SkillsRoot(), id), filepath.Join(dir, "content")); err != nil {
		return summary, err
	}
	if err = s.secureTree(ctx, filepath.Join(dir, "content")); err != nil {
		return summary, err
	}
	if err = s.validateStoredFact(ctx, id, fact, filepath.Join(dir, "content")); err != nil {
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
	final := filepath.Join(root, prefix+filepath.Base(dir))
	if err = os.Rename(dir, final); err != nil {
		return summary, err
	}
	dir = final
	complete = true
	summary = BackupSummary{ID: id, BackupID: filepath.Base(dir), CreatedAt: metadata.CreatedAt, Variants: len(fact.Variants)}
	return summary, s.pruneBackups(id)
}

func (s Store) ensureSkillBackupRoot(ctx context.Context, root string) error {
	base := filepath.Dir(filepath.Dir(root))
	for _, path := range []string{base, filepath.Dir(root), root} {
		if err := s.ensurePrivateRoot(ctx, path); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) ListBackups() ([]BackupSummary, error) {
	located, err := s.listLocatedBackups("")
	if err != nil {
		return nil, err
	}
	result := make([]BackupSummary, 0, len(located))
	for _, item := range located {
		metadata, err := s.readBackupMetadata(item.path)
		if err != nil {
			continue
		}
		if err := s.validateStoredFact(context.Background(), metadata.ID, metadata.Fact, filepath.Join(item.path, "content")); err != nil {
			continue
		}
		result = append(result, BackupSummary{ID: metadata.ID, BackupID: item.id, CreatedAt: metadata.CreatedAt, Variants: len(metadata.Fact.Variants)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].BackupID > result[j].BackupID })
	return result, nil
}

// InspectBackup validates a backup without publishing it. Callers use this to
// validate user choices before restore mutates the private SSOT.
func (s Store) InspectBackup(ctx context.Context, backupID string) (string, Fact, error) {
	if err := contextError(ctx); err != nil {
		return "", Fact{}, err
	}
	metadata, backupDir, err := s.loadBackup(backupID)
	if err != nil {
		return "", Fact{}, err
	}
	if err := s.validateStoredFact(ctx, metadata.ID, metadata.Fact, filepath.Join(backupDir, "content")); err != nil {
		return "", Fact{}, err
	}
	return metadata.ID, metadata.Fact, nil
}

func (s Store) RestoreBackup(ctx context.Context, backupID string) (Fact, error) {
	metadata, backupDir, err := s.loadBackup(backupID)
	if err != nil {
		return Fact{}, err
	}
	content := filepath.Join(backupDir, "content")
	if err := s.validateStoredFact(ctx, metadata.ID, metadata.Fact, content); err != nil {
		return Fact{}, err
	}
	if err := s.ensurePrivateRoot(ctx, s.SkillsRoot()); err != nil {
		return Fact{}, err
	}
	stage, err := os.MkdirTemp(s.SkillsRoot(), ".bootagent-restore-")
	if err != nil {
		return Fact{}, err
	}
	defer os.RemoveAll(stage)
	if err := CopyTree(ctx, content, stage); err != nil {
		return Fact{}, err
	}
	if err := s.secureTree(ctx, stage); err != nil {
		return Fact{}, err
	}
	if err := s.validateStoredFact(ctx, metadata.ID, metadata.Fact, stage); err != nil {
		return Fact{}, err
	}
	if err := PublishTree(ctx, stage, filepath.Join(s.SkillsRoot(), metadata.ID)); err != nil {
		return Fact{}, err
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
	if len(fact.Name) > 256 || len(fact.Description) > 1024 {
		return fmt.Errorf("Skill %q metadata exceeds size limit", id)
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
		for _, source := range variant.ImportSources {
			if source != "agent" && source != "folder" && source != "zip" {
				return fmt.Errorf("Skill %q has invalid import source", id)
			}
		}
	}
	return nil
}

func sortedUnique(values []string) bool {
	if len(values) > maxAssociationValues {
		return false
	}
	for i, value := range values {
		if len(value) > maxAssociationValueBytes || strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) || i > 0 && values[i-1] >= value {
			return false
		}
	}
	return true
}

func (s Store) loadBackup(backupID string) (backupMetadata, string, error) {
	if backupID == "" || backupID == "." || backupID == ".." || backupID != filepath.Base(backupID) || strings.ContainsAny(backupID, `/\\`) {
		return backupMetadata{}, "", errors.New("invalid Skill backup ID")
	}
	if strings.HasPrefix(backupID, ".bootagent-backup-pending-") {
		return backupMetadata{}, "", errors.New("invalid Skill backup ID")
	}
	located, err := s.listLocatedBackups(backupID)
	if err != nil {
		return backupMetadata{}, "", err
	}
	for _, item := range located {
		if item.id != backupID {
			continue
		}
		metadata, readErr := s.readBackupMetadata(item.path)
		if readErr == nil {
			return metadata, item.path, nil
		}
	}
	return backupMetadata{}, "", errors.New("Skill backup is invalid")
}

type locatedBackup struct {
	id   string
	path string
}

func (s Store) listLocatedBackups(backupID string) ([]locatedBackup, error) {
	result := make([]locatedBackup, 0)
	for _, root := range []string{s.BackupRoot(), s.LegacyBackupRoot()} {
		if err := rejectSymlinkComponents(s.home, root); err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if root == s.BackupRoot() {
			for _, skillEntry := range entries {
				if !skillEntry.IsDir() || strings.HasPrefix(skillEntry.Name(), ".bootagent-backup-pending-") {
					continue
				}
				skillRoot := filepath.Join(root, skillEntry.Name())
				if err := rejectSymlinkComponents(s.home, skillRoot); err != nil {
					return nil, err
				}
				backups, readErr := os.ReadDir(skillRoot)
				if readErr != nil {
					return nil, readErr
				}
				for _, entry := range backups {
					if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".bootagent-backup-pending-") {
						continue
					}
					if backupID == "" || entry.Name() == backupID {
						result = append(result, locatedBackup{id: entry.Name(), path: filepath.Join(skillRoot, entry.Name())})
					}
				}
			}
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".bootagent-backup-pending-") {
				continue
			}
			if backupID == "" || entry.Name() == backupID {
				result = append(result, locatedBackup{id: entry.Name(), path: filepath.Join(root, entry.Name())})
			}
		}
	}
	return result, nil
}

func (s Store) readBackupMetadata(backupDir string) (backupMetadata, error) {
	info, err := os.Lstat(backupDir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return backupMetadata{}, errors.New("Skill backup is invalid")
	}
	b, err := readBoundedFile(filepath.Join(backupDir, "metadata.json"), maxRegistryBytes)
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

func (s Store) validateStoredFact(ctx context.Context, id string, fact Fact, contentRoot string) error {
	if err := rejectSymlinkComponents(s.home, contentRoot); err != nil {
		return err
	}
	contentInfo, err := os.Lstat(contentRoot)
	if err != nil || !contentInfo.IsDir() || contentInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("Skill backup is incomplete")
	}
	entries, err := os.ReadDir(contentRoot)
	if err != nil {
		return errors.New("Skill backup is incomplete")
	}
	if len(entries) != 1 || entries[0].Name() != "variants" || !entries[0].IsDir() {
		return errors.New("Skill backup content layout is invalid")
	}
	variantsRoot := filepath.Join(contentRoot, "variants")
	if err := rejectSymlinkComponents(s.home, variantsRoot); err != nil {
		return err
	}
	variantEntries, err := os.ReadDir(variantsRoot)
	if err != nil {
		return errors.New("Skill backup content layout is invalid")
	}
	expected := make(map[string]struct{})
	for _, variant := range fact.Variants {
		if variant.Stored {
			expected[variant.Hash] = struct{}{}
		}
	}
	if len(variantEntries) != len(expected) {
		return errors.New("Skill backup contains undeclared content")
	}
	for _, entry := range variantEntries {
		if _, ok := expected[entry.Name()]; !ok || !entry.IsDir() {
			return errors.New("Skill backup contains undeclared content")
		}
	}
	for _, variant := range fact.Variants {
		if !variant.Stored {
			continue
		}
		root := filepath.Join(contentRoot, "variants", variant.Hash)
		if err := rejectSymlinkComponents(s.home, root); err != nil {
			return err
		}
		info, err := os.Lstat(filepath.Join(root, "SKILL.md"))
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("Skill backup is incomplete")
		}
		stats, err := HashTree(ctx, root)
		if err != nil || stats.Hash != variant.Hash {
			return errors.New("Skill backup content is invalid")
		}
	}
	return nil
}

func (s Store) ensurePrivateRoot(ctx context.Context, root string) error {
	if err := rejectSymlinkComponents(s.home, filepath.Dir(root)); err != nil {
		return err
	}
	return s.ensurePrivateDir(ctx, root)
}

func (s Store) ensurePrivateDir(ctx context.Context, path string) error {
	if err := rejectSymlinkComponents(s.home, path); err != nil {
		return err
	}
	return s.fs.EnsurePrivateDir(ctx, path)
}

func rejectSymlinkComponents(base, path string) error {
	base = filepath.Clean(base)
	rel, err := filepath.Rel(base, filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("private storage path escapes home")
	}
	current := base
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("private storage path contains a symlink")
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("Skill registry exceeds size limit")
	}
	b := make([]byte, info.Size())
	if _, err := io.ReadFull(f, b); err != nil {
		return nil, err
	}
	return b, nil
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

func (s Store) pruneBackups(id string) error {
	located, err := s.listLocatedBackups("")
	if err != nil {
		return err
	}
	type validBackup struct {
		item    locatedBackup
		created time.Time
	}
	backups := make([]validBackup, 0)
	for _, item := range located {
		metadata, readErr := s.readBackupMetadata(item.path)
		if readErr != nil || metadata.ID != id {
			continue
		}
		created, parseErr := time.Parse(time.RFC3339Nano, metadata.CreatedAt)
		if parseErr != nil {
			continue
		}
		backups = append(backups, validBackup{item: item, created: created})
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].created.Equal(backups[j].created) {
			return backups[i].item.id > backups[j].item.id
		}
		return backups[i].created.After(backups[j].created)
	})
	retention := s.fs.BackupRetention()
	if len(backups) <= retention {
		return nil
	}
	for _, backup := range backups[retention:] {
		if err := os.RemoveAll(backup.item.path); err != nil {
			return err
		}
	}
	return nil
}
