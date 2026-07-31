// Command oneagent-release builds and validates the native Wails distribution.
// It deliberately uses only the Go and Node toolchains already required by the
// application; release output never embeds a language runtime.
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/version"
)

const previewChannel = "technical-preview-unsigned"

type targetInfo struct {
	OS   string
	Arch string
}

type toolchainInfo struct {
	Go       string `json:"go"`
	Wails    string `json:"wails"`
	Frontend string `json:"frontend"`
}

type artifactInfo struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type releaseManifest struct {
	SchemaVersion   int               `json:"schema_version"`
	OneAgentVersion string            `json:"oneagent_version"`
	Channel         string            `json:"channel"`
	Unsigned        bool              `json:"unsigned"`
	Platform        string            `json:"platform"`
	Arch            string            `json:"arch"`
	Toolchain       toolchainInfo     `json:"toolchain"`
	SystemWebView   string            `json:"system_webview"`
	BuiltAt         string            `json:"built_at"`
	AgentVersions   map[string]string `json:"agent_versions"`
	Artifacts       []artifactInfo    `json:"artifacts"`
}

type packageLock struct {
	Packages map[string]struct {
		Version string `json:"version"`
		License any    `json:"license"`
		Dev     bool   `json:"dev"`
	} `json:"packages"`
}

type moduleInfo struct {
	Path    string
	Version string
	Dir     string
	Main    bool
}

var (
	remoteAssetPattern = regexp.MustCompile(`(?i)(?:src|href)\s*=\s*["']https?://|@import\s+(?:url\()?\s*["']?https?://|url\(\s*["']?https?://`)
	secretPattern      = regexp.MustCompile(`(?i)sk-[A-Za-z0-9_-]{20,}|Bearer\s+[A-Za-z0-9._-]{24,}`)
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	switch args[0] {
	case "build":
		if err := buildRelease(root, args[1:], stdout, stderr); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "check":
		if err := checkRelease(commandPath(args[1:])); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "release policy checks passed")
		return 0
	default:
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: oneagent-release build [--source] [--skip-frontend] [--channel technical-preview-unsigned]")
	fmt.Fprintln(w, "       oneagent-release check [release-directory]")
}

func commandPath(args []string) string {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return "release"
	}
	return args[0]
}

func repositoryRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("could not locate the OneAgent repository root")
		}
		directory = parent
	}
}

func buildRelease(root string, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	flags.SetOutput(stderr)
	channel := flags.String("channel", previewChannel, "release channel")
	source := flags.Bool("source", false, "also create a source archive")
	skipFrontend := flags.Bool("skip-frontend", false, "use an existing frontend/dist")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *channel != previewChannel {
		return fmt.Errorf("only %q is publishable while Wails is Alpha", previewChannel)
	}
	target := currentTarget()
	if err := ensureFrontend(root, *skipFrontend); err != nil {
		return err
	}
	metadata := filepath.Join(root, "build", "metadata")
	if err := os.RemoveAll(metadata); err != nil {
		return fmt.Errorf("clear release metadata: %w", err)
	}
	if err := generateNotices(root, metadata); err != nil {
		return err
	}
	stage, err := buildBinaries(root, target, metadata)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "release"), 0o755); err != nil {
		return err
	}
	artifactName := fmt.Sprintf("OneAgent-%s-%s-%s-%s.zip", version.Version, *channel, target.OS, target.Arch)
	artifactPath := filepath.Join(root, "release", artifactName)
	if err := zipDirectory(stage, artifactPath, "OneAgent"); err != nil {
		return fmt.Errorf("create release archive: %w", err)
	}
	artifacts := []string{artifactPath}
	if *source {
		sourcePath := filepath.Join(root, fmt.Sprintf("release/OneAgent-%s-source.zip", version.Version))
		if err := zipSource(root, metadata, sourcePath, version.Version); err != nil {
			return fmt.Errorf("create source archive: %w", err)
		}
		artifacts = append(artifacts, sourcePath)
	}
	manifestPath := filepath.Join(root, fmt.Sprintf("release/release-manifest-%s-%s.json", target.OS, target.Arch))
	manifest, err := makeManifest(root, target, *channel, artifacts)
	if err != nil {
		return err
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestData = append(manifestData, '\n')
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		return fmt.Errorf("write release manifest: %w", err)
	}
	checksumPath := filepath.Join(root, fmt.Sprintf("release/SHA256SUMS-%s-%s.txt", target.OS, target.Arch))
	if err := writeChecksums(checksumPath, append(artifacts, manifestPath)); err != nil {
		return err
	}
	fmt.Fprintln(stdout, artifactPath)
	return nil
}

func currentTarget() targetInfo {
	osID := "linux"
	switch runtime.GOOS {
	case "darwin":
		osID = "macos"
	case "windows":
		osID = "windows"
	}
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	return targetInfo{OS: osID, Arch: arch}
}

func ensureFrontend(root string, skip bool) error {
	dist := filepath.Join(root, "frontend", "dist")
	if !skip {
		npm, err := exec.LookPath("npm")
		if err != nil {
			return errors.New("npm is required to build the React frontend")
		}
		if err := runCommand(root, npm, "run", "build"); err != nil {
			return fmt.Errorf("build frontend: %w", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		return errors.New("frontend/dist/index.html is missing; build the frontend first")
	}
	var maps []string
	if err := filepath.WalkDir(dist, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(path), ".map") {
			maps = append(maps, path)
		}
		return nil
	}); err != nil {
		return err
	}
	if len(maps) > 0 {
		return fmt.Errorf("source maps are forbidden in release assets: %s", maps[0])
	}
	return nil
}

func buildBinaries(root string, target targetInfo, metadata string) (string, error) {
	stage := filepath.Join(root, "build", "release-stage", target.OS+"-"+target.Arch)
	if err := os.RemoveAll(stage); err != nil {
		return "", err
	}
	oneDir := filepath.Join(stage, "OneAgent")
	if err := os.MkdirAll(oneDir, 0o755); err != nil {
		return "", err
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		return "", errors.New("go is required to build the release")
	}
	desktop := filepath.Join(oneDir, "OneAgent")
	desktopTags := "wails,production"
	if target.OS == "linux" {
		desktopTags += ",gtk3"
	}
	if target.OS == "macos" {
		appDir := filepath.Join(oneDir, "OneAgent.app", "Contents", "MacOS")
		if err := os.MkdirAll(appDir, 0o755); err != nil {
			return "", err
		}
		desktop = filepath.Join(appDir, "OneAgent")
	}
	ldflags := "-w -s"
	if target.OS == "windows" {
		desktop = filepath.Join(oneDir, "OneAgent.exe")
		ldflags += " -H windowsgui"
	}
	if err := runCommand(root, goTool, "build", "-tags", desktopTags, "-trimpath", "-buildvcs=false", "-ldflags="+ldflags, "-o", desktop, "./cmd/oneagent-desktop"); err != nil {
		return "", fmt.Errorf("build desktop binary: %w", err)
	}
	cliName := "oneagent"
	if target.OS == "windows" {
		cliName += ".exe"
	}
	if err := runCommand(root, goTool, "build", "-trimpath", "-buildvcs=false", "-ldflags=-w -s", "-o", filepath.Join(oneDir, cliName), "./cmd/oneagent"); err != nil {
		return "", fmt.Errorf("build CLI binary: %w", err)
	}
	if target.OS == "macos" {
		plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleDisplayName</key><string>OneAgent</string>
<key>CFBundleExecutable</key><string>OneAgent</string>
<key>CFBundleIdentifier</key><string>com.maimorylab.oneagent</string>
<key>CFBundleName</key><string>OneAgent</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>0.3.0</string>
<key>CFBundleVersion</key><string>1</string>
</dict></plist>
`
		if err := os.WriteFile(filepath.Join(oneDir, "OneAgent.app", "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
			return "", err
		}
	}
	if err := copyFile(filepath.Join(root, "README.md"), filepath.Join(oneDir, "README.md")); err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(root, "agents.lock.json"), filepath.Join(oneDir, "agents.lock.json")); err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(metadata, "THIRD_PARTY_NOTICES.md"), filepath.Join(oneDir, "THIRD_PARTY_NOTICES.md")); err != nil {
		return "", err
	}
	if err := copyDir(filepath.Join(metadata, "licenses"), filepath.Join(oneDir, "licenses")); err != nil {
		return "", err
	}
	return stage, nil
}

func runCommand(dir, executable string, args ...string) error {
	command := exec.Command(executable, args...)
	command.Dir = dir
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func makeManifest(root string, target targetInfo, channel string, files []string) (releaseManifest, error) {
	lock, err := catalog.LoadEmbedded()
	if err != nil {
		return releaseManifest{}, err
	}
	frontendVersion := "unknown"
	if data, readErr := os.ReadFile(filepath.Join(root, "frontend", "package.json")); readErr == nil {
		var packageJSON struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(data, &packageJSON) == nil && packageJSON.Version != "" {
			frontendVersion = packageJSON.Version
		}
	}
	tools := readToolVersions(filepath.Join(root, "build", "tool-versions.env"))
	agentVersions := make(map[string]string)
	for id, agent := range lock.Agents {
		if agent.Package != nil {
			agentVersions[id] = agent.Package.Version
		}
	}
	artifacts := make([]artifactInfo, 0, len(files))
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return releaseManifest{}, err
		}
		digest, err := fileSHA256(file)
		if err != nil {
			return releaseManifest{}, err
		}
		artifacts = append(artifacts, artifactInfo{File: filepath.Base(file), SHA256: digest, Bytes: info.Size()})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].File < artifacts[j].File })
	return releaseManifest{
		SchemaVersion: 2, OneAgentVersion: version.Version, Channel: channel,
		Unsigned: true, Platform: target.OS, Arch: target.Arch,
		Toolchain:     toolchainInfo{Go: runtime.Version(), Wails: tools["WAILS_VERSION"], Frontend: frontendVersion},
		SystemWebView: webViewRequirement(target.OS), BuiltAt: time.Now().UTC().Format(time.RFC3339),
		AgentVersions: agentVersions, Artifacts: artifacts,
	}, nil
}

func readToolVersions(file string) map[string]string {
	values := map[string]string{}
	data, err := os.ReadFile(file)
	if err != nil {
		return values
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}

func webViewRequirement(osID string) string {
	switch osID {
	case "macos":
		return "WKWebView (macOS 12+)"
	case "windows":
		return "WebView2 Runtime"
	default:
		return "GTK3 + WebKitGTK 4.1"
	}
}

func writeChecksums(file string, paths []string) error {
	sort.Slice(paths, func(i, j int) bool { return filepath.Base(paths[i]) < filepath.Base(paths[j]) })
	var builder strings.Builder
	for _, path := range paths {
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&builder, "%s  %s\n", digest, filepath.Base(path))
	}
	return os.WriteFile(file, []byte(builder.String()), 0o644)
}

func fileSHA256(file string) (string, error) {
	hash := sha256.New()
	handle, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	if _, err := io.Copy(hash, handle); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func zipDirectory(source, destination, rootName string) error {
	return createZip(destination, func(writer *zip.Writer) error {
		return filepath.WalkDir(source, func(file string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(source, file)
			if err != nil {
				return err
			}
			return addZipFile(writer, file, path.Join(rootName, filepath.ToSlash(relative)))
		})
	})
}

func zipSource(root, metadata, destination, versionValue string) error {
	return createZip(destination, func(writer *zip.Writer) error {
		command := exec.Command("git", "ls-files", "-co", "--exclude-standard", "-z")
		command.Dir = root
		output, err := command.Output()
		if err != nil {
			return fmt.Errorf("list source files: %w", err)
		}
		files := strings.SplitSeq(string(output), "\x00")
		for relative := range files {
			if relative == "" {
				continue
			}
			if sourceArchiveExcluded(relative) {
				continue
			}
			file := filepath.Join(root, filepath.FromSlash(relative))
			if info, statErr := os.Stat(file); statErr == nil && info.Mode().IsRegular() {
				if err := addZipFile(writer, file, path.Join("OneAgent-"+versionValue, filepath.ToSlash(relative))); err != nil {
					return err
				}
			}
		}
		for _, relative := range []string{"THIRD_PARTY_NOTICES.md", "licenses"} {
			file := filepath.Join(metadata, relative)
			if info, statErr := os.Stat(file); statErr == nil {
				if info.IsDir() {
					if err := addZipTree(writer, file, path.Join("OneAgent-"+versionValue, relative)); err != nil {
						return err
					}
				} else if err := addZipFile(writer, file, path.Join("OneAgent-"+versionValue, relative)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func sourceArchiveExcluded(relative string) bool {
	relative = filepath.ToSlash(relative)
	for _, prefix := range []string{"build/metadata/", "build/release-stage/", "release/", "frontend/dist/"} {
		if strings.HasPrefix(relative, prefix) {
			return true
		}
	}
	return false
}

func createZip(destination string, fill func(*zip.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	fillErr := fill(writer)
	closeErr := writer.Close()
	fileCloseErr := file.Close()
	if fillErr != nil {
		return fillErr
	}
	if closeErr != nil {
		return closeErr
	}
	return fileCloseErr
}

func addZipTree(writer *zip.Writer, source, rootName string) error {
	return filepath.WalkDir(source, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}
		return addZipFile(writer, file, path.Join(rootName, filepath.ToSlash(relative)))
	})
}

func addZipFile(writer *zip.Writer, file, name string) error {
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(name)
	header.Method = zip.Deflate
	header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	input, err := os.Open(file)
	if err != nil {
		return err
	}
	defer input.Close()
	_, err = io.Copy(entry, input)
	return err
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o644)
}

func copyDir(source, destination string) error {
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return os.MkdirAll(destination, 0o755)
	}
	return filepath.WalkDir(source, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(file, target)
	})
}

func generateNotices(root, output string) error {
	if err := os.MkdirAll(filepath.Join(output, "licenses"), 0o755); err != nil {
		return err
	}
	goPackages, err := goPackages(root)
	if err != nil {
		return err
	}
	frontendPackages, err := frontendPackages(root, filepath.Join(output, "licenses"))
	if err != nil {
		return err
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return err
	}
	var builder strings.Builder
	builder.WriteString("# OneAgent Third-Party Notices\n\n")
	builder.WriteString("OneAgent bundles the Go application and its built React assets. Agent packages are not bundled; they are installed from the listed upstream source only after user confirmation.\n\n")
	builder.WriteString("## Runtime Components\n\n")
	builder.WriteString("- Go standard library: BSD-3-Clause, https://go.dev/LICENSE\n")
	builder.WriteString("- Wails and Go modules: see the bundled `licenses/` files and module source URLs below.\n\n")
	builder.WriteString("| Go module | Version | License file | Source |\n| --- | --- | --- | --- |\n")
	for _, item := range goPackages {
		fmt.Fprintf(&builder, "| `%s` | `%s` | `%s` | https://pkg.go.dev/%s@%s |\n", item.Name, item.Version, item.License, item.Name, item.Version)
	}
	builder.WriteString("\n## Frontend Runtime Packages\n\n| Package | Version | License | License file |\n| --- | --- | --- | --- |\n")
	for _, item := range frontendPackages {
		fmt.Fprintf(&builder, "| `%s` | `%s` | %s | %s |\n", item.Name, item.Version, item.License, item.LicenseFile)
	}
	builder.WriteString("\n## Agent Installation Targets (Not Bundled)\n\n| Agent | Locked package | License | Source | License reference |\n| --- | --- | --- | --- | --- |\n")
	for id, agent := range manifest.Agents {
		if agent.Package == nil {
			continue
		}
		fmt.Fprintf(&builder, "| %s | `%s@%s` | %s | %s | %s |\n", agent.Name, agent.Package.Name, agent.Package.Version, agent.Package.License, agent.Package.Source, agent.Package.LicenseURL)
		_ = id
	}
	return os.WriteFile(filepath.Join(output, "THIRD_PARTY_NOTICES.md"), []byte(builder.String()), 0o644)
}

type noticeItem struct {
	Name        string
	Version     string
	License     string
	LicenseFile string
}

func goPackages(root string) ([]noticeItem, error) {
	command := exec.Command("go", "list", "-m", "-json", "all")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list Go modules: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	items := []noticeItem{}
	for {
		var module moduleInfo
		if err := decoder.Decode(&module); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode Go module list: %w", err)
		}
		if module.Main || module.Path == "" {
			continue
		}
		license := "see bundled source"
		licenseFile := ""
		if module.Dir != "" {
			licenseFile = copyLicense(module.Dir, filepath.Join(root, "build", "metadata", "licenses"), "go-"+module.Path)
		}
		if licenseFile != "" {
			license = licenseFile
		}
		items = append(items, noticeItem{Name: module.Path, Version: module.Version, License: license, LicenseFile: licenseFile})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func frontendPackages(root, licenseDir string) ([]noticeItem, error) {
	data, err := os.ReadFile(filepath.Join(root, "frontend", "package-lock.json"))
	if err != nil {
		return nil, err
	}
	var lock packageLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("read frontend lock: %w", err)
	}
	items := []noticeItem{}
	for relative, metadata := range lock.Packages {
		if !strings.HasPrefix(relative, "node_modules/") || metadata.Dev || metadata.Version == "" {
			continue
		}
		name := strings.TrimPrefix(relative, "node_modules/")
		license := licenseValue(metadata.License)
		licenseFile := ""
		packageDir := filepath.Join(root, "frontend", relative)
		if packageData, readErr := os.ReadFile(filepath.Join(packageDir, "package.json")); readErr == nil {
			var packageJSON struct {
				Name    string `json:"name"`
				License any    `json:"license"`
			}
			if json.Unmarshal(packageData, &packageJSON) == nil {
				if packageJSON.Name != "" {
					name = packageJSON.Name
				}
				if packageJSON.License != nil {
					license = licenseValue(packageJSON.License)
				}
			}
		}
		if copied := copyLicense(packageDir, licenseDir, "npm-"+name+"-"+metadata.Version); copied != "" {
			licenseFile = copied
		}
		if license == "" {
			license = "see package metadata"
		}
		if licenseFile == "" {
			licenseFile = "not provided by package"
		}
		items = append(items, noticeItem{Name: name, Version: metadata.Version, License: license, LicenseFile: licenseFile})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func licenseValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		if name, ok := typed["type"].(string); ok {
			return name
		}
		if name, ok := typed["name"].(string); ok {
			return name
		}
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := licenseValue(item); value != "" {
				parts = append(parts, value)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

func copyLicense(source, destination, label string) string {
	entries, err := os.ReadDir(source)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		lower := strings.ToLower(entry.Name())
		if !(strings.HasPrefix(lower, "license") || strings.HasPrefix(lower, "licence") || strings.HasPrefix(lower, "copying") || strings.HasPrefix(lower, "notice")) {
			continue
		}
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return ""
		}
		name := safeName(label) + filepath.Ext(entry.Name())
		if err := copyFile(filepath.Join(source, entry.Name()), filepath.Join(destination, name)); err != nil {
			return ""
		}
		return filepath.ToSlash(filepath.Join("licenses", name))
	}
	return ""
}

func safeName(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '.' || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}

func checkRelease(directory string) error {
	manifests, _ := filepath.Glob(filepath.Join(directory, "release-manifest-*.json"))
	archives, _ := filepath.Glob(filepath.Join(directory, "OneAgent-*.zip"))
	problems := []string{}
	if len(manifests) == 0 {
		problems = append(problems, "release manifest is missing")
	}
	if len(archives) == 0 {
		problems = append(problems, "release archive is missing")
	}
	for _, manifest := range manifests {
		problems = append(problems, validateManifest(manifest)...)
	}
	for _, archive := range archives {
		problems = append(problems, inspectArchive(archive)...)
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}

func validateManifest(file string) []string {
	problems := []string{}
	data, err := os.ReadFile(file)
	if err != nil {
		return []string{fmt.Sprintf("read manifest %s: %v", filepath.Base(file), err)}
	}
	var manifest releaseManifest
	var raw map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil || json.Unmarshal(data, &raw) != nil {
		return []string{fmt.Sprintf("invalid release manifest %s", filepath.Base(file))}
	}
	if manifest.SchemaVersion != 2 {
		problems = append(problems, "unsupported release manifest schema: "+filepath.Base(file))
	}
	if _, present := raw["python"]; present {
		problems = append(problems, "release manifest contains a removed python field: "+filepath.Base(file))
	}
	if manifest.Channel != previewChannel || !manifest.Unsigned {
		problems = append(problems, "release manifest must be an unsigned technical preview: "+filepath.Base(file))
	}
	if manifest.Toolchain.Go == "" || manifest.Toolchain.Wails == "" || manifest.Toolchain.Frontend == "" || manifest.SystemWebView == "" {
		problems = append(problems, "release manifest toolchain or WebView requirement is incomplete: "+filepath.Base(file))
	}
	if len(manifest.Artifacts) == 0 {
		return append(problems, "artifact list is missing: "+filepath.Base(file))
	}
	expectedVersions := manifest.AgentVersions
	for _, artifact := range manifest.Artifacts {
		if filepath.Base(artifact.File) != artifact.File {
			problems = append(problems, "artifact filename contains a path: "+artifact.File)
			continue
		}
		artifactPath := filepath.Join(filepath.Dir(file), artifact.File)
		info, statErr := os.Stat(artifactPath)
		if statErr != nil {
			problems = append(problems, "manifest artifact is missing: "+artifact.File)
			continue
		}
		if info.Size() != artifact.Bytes {
			problems = append(problems, "artifact size mismatch: "+artifact.File)
		}
		if digest, hashErr := fileSHA256(artifactPath); hashErr != nil || digest != artifact.SHA256 {
			problems = append(problems, "artifact checksum mismatch: "+artifact.File)
		}
		if !strings.Contains(artifact.File, "-source.zip") && !strings.Contains(artifact.File, manifest.Channel) {
			problems = append(problems, "artifact channel mismatch: "+artifact.File)
		}
		if versions := archiveAgentVersions(artifactPath); versions == nil {
			problems = append(problems, "lock manifest is missing or invalid: "+artifact.File)
		} else if !mapsEqual(versions, expectedVersions) {
			problems = append(problems, "locked Agent versions mismatch: "+artifact.File)
		}
	}
	suffix := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file), "release-manifest-"), ".json")
	checksumFile := filepath.Join(filepath.Dir(file), "SHA256SUMS-"+suffix+".txt")
	checksums := map[string]string{}
	if checksumData, readErr := os.ReadFile(checksumFile); readErr != nil {
		problems = append(problems, "checksum file is missing: "+filepath.Base(checksumFile))
	} else {
		scanner := bufio.NewScanner(bytes.NewReader(checksumData))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) != 2 || len(fields[0]) != sha256.Size*2 {
				problems = append(problems, "invalid checksum line in "+filepath.Base(checksumFile))
				continue
			}
			checksums[fields[1]] = fields[0]
		}
		for _, expected := range append([]string{filepath.Base(file)}, artifactNames(manifest.Artifacts)...) {
			candidate := filepath.Join(filepath.Dir(file), expected)
			if digest, hashErr := fileSHA256(candidate); hashErr == nil && checksums[expected] != digest {
				problems = append(problems, "checksum file mismatch: "+expected)
			}
		}
	}
	return problems
}

func artifactNames(artifacts []artifactInfo) []string {
	names := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		names = append(names, artifact.File)
	}
	return names
}

func archiveAgentVersions(file string) map[string]string {
	archive, err := zip.OpenReader(file)
	if err != nil {
		return nil
	}
	defer archive.Close()
	var matches []*zip.File
	for _, entry := range archive.File {
		if path.Base(entry.Name) == "agents.lock.json" {
			matches = append(matches, entry)
		}
	}
	if len(matches) != 1 {
		return nil
	}
	reader, err := matches[0].Open()
	if err != nil {
		return nil
	}
	defer reader.Close()
	var manifest struct {
		Agents map[string]struct {
			Package *struct {
				Version string `json:"version"`
			} `json:"package"`
		} `json:"agents"`
	}
	if json.NewDecoder(reader).Decode(&manifest) != nil {
		return nil
	}
	result := map[string]string{}
	for id, agent := range manifest.Agents {
		if agent.Package != nil {
			result[id] = agent.Package.Version
		}
	}
	return result
}

func mapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func inspectArchive(file string) []string {
	problems := []string{}
	archive, err := zip.OpenReader(file)
	if err != nil {
		return []string{fmt.Sprintf("cannot open %s: %v", filepath.Base(file), err)}
	}
	defer archive.Close()
	required := map[string]bool{}
	for _, entry := range archive.File {
		name := filepath.ToSlash(entry.Name)
		base := strings.ToLower(path.Base(name))
		parts := strings.Split(name, "/")
		if strings.HasPrefix(name, "/") || slicesContain(parts, "..") {
			problems = append(problems, "unsafe archive path in "+filepath.Base(file)+": "+name)
		}
		if strings.EqualFold(base, "agents.lock.json") {
			required["agents.lock.json"] = true
		}
		if strings.EqualFold(base, "THIRD_PARTY_NOTICES.md") {
			required["THIRD_PARTY_NOTICES.md"] = true
		}
		lowerName := strings.ToLower(name)
		for _, suffix := range []string{".py", ".pyc", ".pyo", ".pyd", ".whl"} {
			if strings.HasSuffix(lowerName, suffix) {
				problems = append(problems, "Python artifact in "+filepath.Base(file)+": "+name)
			}
		}
		if strings.Contains(lowerName, "pyinstaller") || strings.Contains(base, "libpython") {
			problems = append(problems, "Python runtime in "+filepath.Base(file)+": "+name)
		}
		if strings.HasSuffix(lowerName, ".map") {
			problems = append(problems, "source map in "+filepath.Base(file)+": "+name)
		}
		if slicesContain([]string{"codex", "codex.exe", "claude", "claude.exe", "opencode", "opencode.exe", "kilo", "kilo.exe", "aider", "aider.exe"}, base) {
			problems = append(problems, "Agent binary in "+filepath.Base(file)+": "+name)
		}
		if !entry.FileInfo().IsDir() && isTextArchiveFile(lowerName) {
			reader, openErr := entry.Open()
			if openErr != nil {
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(reader, 8<<20))
			reader.Close()
			if readErr == nil && secretPattern.Match(data) {
				problems = append(problems, "possible secret in "+filepath.Base(file)+": "+name)
			}
			if readErr == nil && strings.Contains(lowerName, "/frontend/dist/") && remoteAssetPattern.Match(data) {
				problems = append(problems, "remote asset reference in "+filepath.Base(file)+": "+name)
			}
		}
	}
	for _, name := range []string{"agents.lock.json", "THIRD_PARTY_NOTICES.md"} {
		if !required[name] {
			problems = append(problems, "missing "+name+" in "+filepath.Base(file))
		}
	}
	return problems
}

func isTextArchiveFile(name string) bool {
	for _, suffix := range []string{".html", ".css", ".js", ".json", ".md", ".txt", ".toml", ".ps1", ".sh"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func slicesContain(values []string, target string) bool {
	return slices.Contains(values, target)
}
