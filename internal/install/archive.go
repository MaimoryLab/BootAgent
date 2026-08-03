package install

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxRuntimeEntryBytes bounds one extracted file. The largest entry in the
// pinned artifacts is the node binary at roughly 120 MB, so this leaves room
// while still refusing a decompression bomb.
const maxRuntimeEntryBytes = 512 << 20

// safeJoin resolves an archive entry inside destination and refuses anything
// that escapes it. Archive members are untrusted input even when the archive
// checksum matched: the checksum proves provenance, not that every path is
// benign.
func safeJoin(destination, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("archive entry has an empty name")
	}
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) || cleaned == ".." {
		return "", fmt.Errorf("archive entry %q escapes the extraction directory", name)
	}
	if len(cleaned) > 1 && cleaned[1] == ':' {
		return "", fmt.Errorf("archive entry %q contains a drive letter", name)
	}
	return filepath.Join(destination, cleaned), nil
}

func extractTarGz(archive, destination string) error {
	file, err := os.Open(archive)
	if err != nil {
		return runtimeError("Cannot open the downloaded runtime archive", err)
	}
	defer file.Close()
	decompressed, err := gzip.NewReader(file)
	if err != nil {
		return runtimeError("Cannot read the downloaded runtime archive", err)
	}
	defer decompressed.Close()

	reader := tar.NewReader(decompressed)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return runtimeError("Cannot read the downloaded runtime archive", err)
		}
		path, err := safeJoin(destination, header.Name)
		if err != nil {
			return runtimeError("The runtime archive contains an unsafe path", err)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o700); err != nil {
				return runtimeError("Cannot create a runtime directory", err)
			}
		case tar.TypeReg:
			if err := writeEntry(path, reader, header.FileInfo().Mode().Perm()); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// node ships bin/npm and bin/npx as relative symlinks into
			// lib/node_modules. Dropping them would leave the runtime without a
			// package manager, so they are recreated after the same escape check.
			if err := writeSymlink(destination, path, header.Linkname); err != nil {
				return err
			}
		default:
			// Hard links, devices and fifos are not part of any pinned artifact.
			continue
		}
	}
}

func extractZip(archive, destination string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return runtimeError("Cannot read the downloaded runtime archive", err)
	}
	defer reader.Close()
	for _, entry := range reader.File {
		path, err := safeJoin(destination, entry.Name)
		if err != nil {
			return runtimeError("The runtime archive contains an unsafe path", err)
		}
		info := entry.FileInfo()
		if info.IsDir() {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return runtimeError("Cannot create a runtime directory", err)
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := readZipSymlink(entry)
			if err != nil {
				return runtimeError("Cannot read a runtime archive symlink", err)
			}
			if err := writeSymlink(destination, path, target); err != nil {
				return err
			}
			continue
		}
		source, err := entry.Open()
		if err != nil {
			return runtimeError("Cannot read the downloaded runtime archive", err)
		}
		writeErr := writeEntry(path, source, info.Mode().Perm())
		source.Close()
		if writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func readZipSymlink(entry *zip.File) (string, error) {
	reader, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer reader.Close()
	target, err := io.ReadAll(io.LimitReader(reader, 4096))
	if err != nil {
		return "", err
	}
	return string(target), nil
}

func writeEntry(path string, source io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return runtimeError("Cannot create a runtime directory", err)
	}
	// The archive is a downloaded runtime, not a secret, but it lives under the
	// user's private OneAgent directory: keep it owner-only and preserve only
	// the executable bit the archive declared.
	permissions := os.FileMode(0o600)
	if mode&0o111 != 0 {
		permissions = 0o700
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, permissions)
	if err != nil {
		return runtimeError("Cannot write a runtime file", err)
	}
	written, err := io.Copy(file, io.LimitReader(source, maxRuntimeEntryBytes+1))
	closeErr := file.Close()
	if err != nil {
		return runtimeError("Cannot write a runtime file", err)
	}
	if closeErr != nil {
		return runtimeError("Cannot write a runtime file", closeErr)
	}
	if written > maxRuntimeEntryBytes {
		return runtimeError("A runtime archive entry exceeded the size limit", fmt.Errorf("%s is larger than %d bytes", filepath.Base(path), maxRuntimeEntryBytes))
	}
	return nil
}

func writeSymlink(destination, path, target string) error {
	if target == "" {
		return runtimeError("The runtime archive contains an unsafe path", fmt.Errorf("empty symlink target"))
	}
	// A symlink's target is checked as a path relative to the link's own
	// directory: an entry pointing at ../../etc must not survive extraction.
	resolved := target
	if !filepath.IsAbs(filepath.FromSlash(target)) {
		resolved = filepath.Join(filepath.Dir(path), filepath.FromSlash(target))
	}
	relative, err := filepath.Rel(destination, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return runtimeError("The runtime archive contains an unsafe path", fmt.Errorf("symlink %q escapes the extraction directory", target))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return runtimeError("Cannot create a runtime directory", err)
	}
	_ = os.Remove(path)
	if err := os.Symlink(filepath.FromSlash(target), path); err != nil {
		return runtimeError("Cannot create a runtime symlink", err)
	}
	return nil
}
