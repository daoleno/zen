package skills

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Safe archive extraction for skill packages. Rules (all enforced, no silent
// fallbacks):
//   - No absolute member paths.
//   - No ".." traversal.
//   - No symlink or hardlink members (a skill package is plain files).
//   - Bounded entry count, total bytes, and per-file bytes.
//   - Always extracted into a caller-chosen staging directory that exists.
//
// A member violating any rule aborts the whole extraction; partial staging
// output is removed by the caller via the returned cleanup path.

const (
	maxArchiveEntries = maxPackageFiles
	maxArchiveBytes   = maxPackageBytes
	maxPerFileBytes   = hashMaxFileBytes
	maxArchiveDepth   = maxPackageDepth
)

var (
	ErrUnsafeArchiveEntry = errors.New("archive contains an unsafe path")
	ErrArchiveTooLarge    = errors.New("archive exceeds its size bound")
)

// ExtractArchiveSafe stages a zip or tar/tar.gz archive into stagingDir. It
// returns the base directory holding the extracted tree (usually stagingDir
// itself, possibly a single top-level folder) when SKILL.md can be located.
func ExtractArchiveSafe(archivePath, stagingDir string) (string, error) {
	if strings.TrimSpace(archivePath) == "" || strings.TrimSpace(stagingDir) == "" {
		return "", errors.New("archive and staging paths are required")
	}
	if !filepath.IsAbs(stagingDir) {
		return "", errors.New("staging directory must be absolute")
	}
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return "", err
	}
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZipSafe(archivePath, stagingDir)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"), strings.HasSuffix(lower, ".tar"):
		return extractTarSafe(archivePath, stagingDir)
	default:
		return "", errors.New("unsupported archive format (expected .zip, .tar, .tar.gz, .tgz)")
	}
}

func extractZipSafe(archivePath, stagingDir string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > maxArchiveEntries {
		return "", ErrArchiveTooLarge
	}
	var total int64
	hasSKILL := false
	for _, member := range reader.File {
		if member.FileInfo().Mode()&fs.ModeSymlink != 0 {
			return "", ErrUnsafeArchiveEntry
		}
		clean, ok := safeArchiveMemberPath(member.Name)
		if !ok {
			return "", ErrUnsafeArchiveEntry
		}
		if strings.HasSuffix(clean, "/") {
			continue
		}
		if member.UncompressedSize64 > maxPerFileBytes {
			return "", ErrArchiveTooLarge
		}
		total += int64(member.UncompressedSize64)
		if total > maxArchiveBytes {
			return "", ErrArchiveTooLarge
		}
		target := filepath.Join(stagingDir, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		source, err := member.Open()
		if err != nil {
			return "", err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			_ = source.Close()
			return "", err
		}
		_, copyErr := io.Copy(output, io.LimitReader(source, maxPerFileBytes+1))
		closeErr := output.Close()
		_ = source.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if filepath.Base(clean) == "SKILL.md" {
			hasSKILL = true
		}
	}
	if !hasSKILL {
		return "", errors.New("archive contains no SKILL.md")
	}
	return locateSkillRoot(stagingDir), nil
}

func extractTarSafe(archivePath, stagingDir string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var reader io.Reader = file
	lower := strings.ToLower(archivePath)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return "", fmt.Errorf("open gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	}
	tarReader := tar.NewReader(reader)
	entries := 0
	var total int64
	hasSKILL := false
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		entries++
		if entries > maxArchiveEntries {
			return "", ErrArchiveTooLarge
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return "", ErrUnsafeArchiveEntry
		}
		if header.Size < 0 || header.Size > maxPerFileBytes {
			return "", ErrArchiveTooLarge
		}
		total += header.Size
		if total > maxArchiveBytes {
			return "", ErrArchiveTooLarge
		}
		clean, ok := safeArchiveMemberPath(header.Name)
		if !ok {
			return "", ErrUnsafeArchiveEntry
		}
		if header.Typeflag == tar.TypeDir || strings.HasSuffix(clean, "/") {
			if err := os.MkdirAll(filepath.Join(stagingDir, filepath.FromSlash(clean)), 0o700); err != nil {
				return "", err
			}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		target := filepath.Join(stagingDir, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(output, io.LimitReader(tarReader, maxPerFileBytes+1))
		closeErr := output.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if filepath.Base(clean) == "SKILL.md" {
			hasSKILL = true
		}
	}
	if !hasSKILL {
		return "", errors.New("archive contains no SKILL.md")
	}
	return locateSkillRoot(stagingDir), nil
}

// safeArchiveMemberPath validates a single member path and returns its clean
// slash form. Absolute paths, traversal, and drive-relative oddities are all
// rejected.
func safeArchiveMemberPath(member string) (string, bool) {
	if member == "" || strings.ContainsRune(member, '\x00') {
		return "", false
	}
	member = filepath.ToSlash(strings.ReplaceAll(member, "\\", "/"))
	if strings.HasPrefix(member, "/") {
		return "", false
	}
	if len(member) >= 2 && member[1] == ':' {
		return "", false
	}
	parts := strings.Split(member, "/")
	depth := 0
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", false
		}
		depth++
	}
	if depth > maxArchiveDepth {
		return "", false
	}
	clean := strings.TrimPrefix(member, "./")
	return clean, true
}

// locateSkillRoot finds the SKILL.md root in a staging tree: the staging dir
// itself when it holds SKILL.md, otherwise the single deepest directory chain
// containing SKILL.md (archives commonly wrap content in a top-level folder).
func locateSkillRoot(stagingDir string) string {
	if fileExists(filepath.Join(stagingDir, "SKILL.md")) {
		return stagingDir
	}
	candidate := stagingDir
	for {
		entries, err := os.ReadDir(candidate)
		if err != nil || len(entries) != 1 || !entries[0].IsDir() && entries[0].Type()&fs.ModeSymlink == 0 {
			return stagingDir
		}
		next := filepath.Join(candidate, entries[0].Name())
		if fileExists(filepath.Join(next, "SKILL.md")) {
			return next
		}
		isDir, _ := isSkillDirectory(entries[0], next)
		if !isDir {
			return stagingDir
		}
		candidate = next
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
