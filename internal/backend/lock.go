// Package backend implements RFC 0052's verified C-compiler backend: the
// lock record naming the pinned Zig distribution, archive verification,
// verified-install markers, backend identity, and child-process compile and
// link operations with separated streams and captured arguments.
//
// The backend never searches PATH, downloads during a build, or probes the
// host for target facts. Callers supply explicit paths and identities; this
// package verifies and invokes.
package backend

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// LockRecord names one pinned backend archive exactly: official filename,
// release version, byte length, and SHA-256 digest. The digest protects
// every later download and packaged copy against substitution or
// corruption.
type LockRecord struct {
	Filename   string `json:"filename"`
	Version    string `json:"version"`
	ByteLength int64  `json:"byte_length"`
	SHA256     string `json:"sha256"`
}

// PinnedZigWindows is the RFC 0052 lock record: Zig 0.16.0 for x86-64
// Windows, recorded from Zig's official release index. A version bump is a
// deliberate re-qualification recorded here, never an incidental upgrade.
func PinnedZigWindows() LockRecord {
	return LockRecord{
		Filename:   "zig-x86_64-windows-0.16.0.zip",
		Version:    "0.16.0",
		ByteLength: 97217739,
		SHA256:     "68659eb5f1e4eb1437a722f1dd889c5a322c9954607f5edcf337bc3684a75a7e",
	}
}

// VerifyArchive checks that the archive at path is the artifact the lock
// record names: filename match on the base name, then byte length, then
// SHA-256 over the complete content. Length is checked first so a truncated
// download fails without hashing.
func VerifyArchive(path string, record LockRecord) error {
	if filepath.Base(path) != record.Filename {
		return fmt.Errorf("archive %q is not the locked %q", filepath.Base(path), record.Filename)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("archive %q is unreadable: %w", path, err)
	}
	if info.Size() != record.ByteLength {
		return fmt.Errorf("archive %q is %d bytes, want %d", path, info.Size(), record.ByteLength)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("archive %q is unreadable: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("archive %q failed to hash: %w", path, err)
	}
	if digest := hex.EncodeToString(hash.Sum(nil)); digest != record.SHA256 {
		return fmt.Errorf("archive %q digest %s does not match the lock record", path, digest)
	}
	return nil
}

// VerifiedMarker is written into a staged backend directory only after every
// check succeeds. An ordinary build validates the marker, exact version,
// executable, and lib_dir; it never hashes the complete installation.
type VerifiedMarker struct {
	Filename string `json:"filename"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
}

// markerName is the marker filename inside a staged backend root.
const markerName = "hexal-backend-verified.json"

// WriteMarker serializes the lock-record identity into the staging root.
func WriteMarker(root string, record LockRecord) error {
	encoded, err := json.Marshal(VerifiedMarker{
		Filename: record.Filename,
		Version:  record.Version,
		SHA256:   record.SHA256,
	})
	if err != nil {
		return fmt.Errorf("cannot encode backend marker: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, markerName), append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("cannot write backend marker: %w", err)
	}
	return nil
}

// ReadMarker loads and parses the verified-install marker, or reports its
// absence or corruption.
func ReadMarker(root string) (VerifiedMarker, error) {
	var marker VerifiedMarker
	raw, err := os.ReadFile(filepath.Join(root, markerName))
	if err != nil {
		return marker, fmt.Errorf("no verified backend marker in %q: %w", root, err)
	}
	if err := json.Unmarshal(raw, &marker); err != nil {
		return marker, fmt.Errorf("backend marker in %q is corrupt: %w", root, err)
	}
	return marker, nil
}

// StageVerified extracts an already-verified archive into a fresh staging
// directory and publishes the verified marker. Extraction uses Zip Slip
// protection: entries escaping the staging root are rejected.
func StageVerified(archivePath, stagingDir string, record LockRecord) error {
	if err := VerifyArchive(archivePath, record); err != nil {
		return err
	}
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return fmt.Errorf("cannot create staging directory %q: %w", stagingDir, err)
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("archive %q is not a valid zip: %w", archivePath, err)
	}
	defer reader.Close()
	for _, entry := range reader.File {
		target := filepath.Join(stagingDir, filepath.FromSlash(entry.Name))
		if !strings.HasPrefix(target, stagingDir+string(os.PathSeparator)) && target != stagingDir {
			return fmt.Errorf("archive entry %q escapes the staging directory", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("cannot create directory %q: %w", target, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("cannot create directory %q: %w", filepath.Dir(target), err)
		}
		if err := extractEntry(entry, target); err != nil {
			return err
		}
	}
	return WriteMarker(stagingDir, record)
}

func extractEntry(entry *zip.File, target string) error {
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("cannot read archive entry %q: %w", entry.Name, err)
	}
	defer source.Close()
	destination, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("cannot write %q: %w", target, err)
	}
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		return fmt.Errorf("cannot write %q: %w", target, err)
	}
	return destination.Close()
}

// FetchArchive downloads the artifact at url to destination without
// executing or trusting it; verification against the lock record is a
// separate step. It is release-assembly tooling, never part of a build.
func FetchArchive(url, destination string) error {
	response, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return fmt.Errorf("cannot fetch %q: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("cannot fetch %q: status %s", url, response.Status)
	}
	out, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("cannot write %q: %w", destination, err)
	}
	if _, err := io.Copy(out, response.Body); err != nil {
		out.Close()
		return fmt.Errorf("cannot write %q: %w", destination, err)
	}
	return out.Close()
}
