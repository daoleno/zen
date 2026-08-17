package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// PackageFile is one entry of the bounded file listing used by inspect.
type PackageFile struct {
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	Mode          string `json:"mode"`
	Kind          string `json:"kind"`
	MediaType     string `json:"media_type"`
	PreviewStatus string `json:"preview_status"`
}

type FilePreview struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	MediaType     string `json:"media_type"`
	Status        string `json:"status"`
	Size          int64  `json:"size"`
	BytesReturned int64  `json:"bytes_returned"`
	Content       string `json:"content,omitempty"`
	Notice        string `json:"notice,omitempty"`
}

// PackageDetail is the read-only inspector projection for one exact copy.
type PackageDetail struct {
	CopyID        string           `json:"copy_id"`
	SkillName     string           `json:"skill_name"`
	Description   string           `json:"description,omitempty"`
	Enabled       bool             `json:"enabled"`
	RootPath      string           `json:"root_path"`
	CanonicalPath string           `json:"canonical_path"`
	AllowedRoot   string           `json:"allowed_root"`
	Location      string           `json:"location"`
	ContentHash   string           `json:"content_hash,omitempty"`
	Scope         Scope            `json:"scope"`
	Agents        []Agent          `json:"agents"`
	Files         []PackageFile    `json:"files,omitempty"`
	Preview       *FilePreview     `json:"preview,omitempty"`
	Risk          []RiskSignal     `json:"risk,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
	Capability    DeleteCapability `json:"capability"`
}

// InspectPackageFile extends package detail with one bounded, read-only text
// file. Traversal and symlinks are rejected before content is read.
func InspectPackageFile(options InventoryOptions, name, relative string) (PackageDetail, error) {
	return inspectPackageFile(options, name, "", relative)
}

// InspectPackageCopyFile reads a file from one exact inventory copy. Copy IDs
// are opaque inventory identities; callers never submit or navigate host paths.
func InspectPackageCopyFile(options InventoryOptions, name, copyID, relative string) (PackageDetail, error) {
	return inspectPackageFile(options, name, copyID, relative)
}

func inspectPackageFile(options InventoryOptions, name, copyID, relative string) (PackageDetail, error) {
	detail, err := inspectPackage(options, name, copyID)
	if err != nil {
		return PackageDetail{}, err
	}
	relative = filepath.ToSlash(filepath.Clean(relative))
	if relative == "." || filepath.IsAbs(relative) || strings.HasPrefix(relative, "../") {
		return PackageDetail{}, errors.New("invalid Skill file path")
	}
	root := detail.CanonicalPath
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return PackageDetail{}, errors.New("Skill package root is unavailable")
	}
	defer rootFS.Close()
	file, info, err := openPackageFile(rootFS, relative)
	if err != nil {
		return PackageDetail{}, err
	}
	defer file.Close()
	detail.Preview, err = previewOpenedPackageFile(file, relative, info)
	if err != nil {
		return PackageDetail{}, err
	}
	return detail, nil
}

const maxInspectPreviewBytes = 64 << 10

// InspectPackage builds the inspection detail for one Skill name. It reads
// the central inventory plus the agent surfaces and never mutates state.
func InspectPackage(options InventoryOptions, name string) (PackageDetail, error) {
	return inspectPackage(options, name, "")
}

// InspectPackageCopy resolves a duplicate name to the exact copy advertised
// by the same inventory contract. A stale or mismatched ID fails closed.
func InspectPackageCopy(options InventoryOptions, name, copyID string) (PackageDetail, error) {
	if strings.TrimSpace(copyID) == "" {
		return PackageDetail{}, errors.New("a Skill copy ID is required")
	}
	return inspectPackage(options, name, copyID)
}

func inspectPackage(options InventoryOptions, name, copyID string) (PackageDetail, error) {
	if err := ValidateSkillName(name); err != nil {
		return PackageDetail{}, err
	}
	if copyID != "" && !validInstalledSkillID(copyID) {
		return PackageDetail{}, errors.New("invalid Skill copy ID")
	}
	normalized, err := normalizeInventoryOptions(options)
	if err != nil {
		return PackageDetail{}, err
	}
	inventory, err := DiscoverInventory(normalized)
	if err != nil {
		return PackageDetail{}, err
	}
	var installed *InstalledSkill
	for index := range inventory.Skills {
		if inventory.Skills[index].Name != name {
			continue
		}
		if copyID != "" && inventory.Skills[index].ID != copyID {
			continue
		}
		if installed != nil {
			return PackageDetail{}, fmt.Errorf("multiple local Skills named %q are installed; resolve the duplicate before inspecting", name)
		}
		installed = &inventory.Skills[index]
	}
	if installed == nil {
		if copyID != "" {
			return PackageDetail{}, fmt.Errorf("no matching copy of Skill %q is installed", name)
		}
		return PackageDetail{}, fmt.Errorf("no Skill named %q is installed or tracked", name)
	}
	detail := PackageDetail{
		CopyID: installed.ID, SkillName: name, Description: installed.Description,
		Enabled: installed.Enabled, RootPath: installed.RootPath,
		CanonicalPath: installed.CanonicalPath, AllowedRoot: installed.AllowedRoot,
		Location: installed.Location, ContentHash: installed.ContentHash,
		Scope: installed.Scope, Agents: append([]Agent{}, installed.Agents...),
		Risk:     append([]RiskSignal{}, installed.Risk...),
		Warnings: append([]string{}, installed.Warnings...), Capability: installed.Capability,
	}
	contentRoot := detail.CanonicalPath
	if contentRoot != "" {
		contentRoot, err = filepath.EvalSymlinks(contentRoot)
		if err != nil {
			detail.Warnings = append(detail.Warnings, "Skill package files are unavailable: "+err.Error())
			return detail, nil
		}
		detail.Files, err = scanPackageFiles(contentRoot)
		if err != nil {
			detail.Warnings = append(detail.Warnings, "Skill package files are unavailable: "+err.Error())
			return detail, nil
		}
		detail.Risk = scanRiskSignals(contentRoot)
		defaultPath := ""
		for _, file := range detail.Files {
			if file.Path == "SKILL.md" {
				defaultPath = file.Path
				break
			}
			if defaultPath == "" && file.PreviewStatus != "binary" {
				defaultPath = file.Path
			}
		}
		if defaultPath != "" {
			detail.Preview, err = previewPackageFile(contentRoot, defaultPath)
			if err != nil {
				detail.Warnings = append(detail.Warnings, "The default Skill file preview is unavailable.")
				return detail, nil
			}
		}
	}
	return detail, nil
}
func scanPackageFiles(root string) ([]PackageFile, error) {
	files, err := collectRegularFiles(root)
	if err != nil {
		return nil, err
	}
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return nil, errors.New("Skill package root is unavailable")
	}
	defer rootFS.Close()
	out := make([]PackageFile, 0, len(files))
	for _, relative := range files {
		file, info, err := openPackageFile(rootFS, relative)
		if err != nil {
			return nil, fmt.Errorf("read %q metadata: %w", relative, err)
		}
		kind, mediaType, status, err := classifyOpenedPackageFile(file, relative, info.Size())
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("classify %q: %w", relative, err)
		}
		out = append(out, PackageFile{Path: relative, Size: info.Size(), Mode: fmt.Sprintf("%04o", info.Mode().Perm()), Kind: kind, MediaType: mediaType, PreviewStatus: status})
	}
	return out, nil
}

func openPackageFile(rootFS *os.Root, relative string) (*os.File, os.FileInfo, error) {
	path := filepath.FromSlash(relative)
	info, err := rootFS.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, errors.New("Skill file is missing")
		}
		return nil, nil, fmt.Errorf("read Skill file metadata: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("Skill file is unavailable or not a regular file")
	}
	file, err := rootFS.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open Skill file: %w", err)
	}
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, nil, errors.New("Skill file changed while it was being opened")
	}
	return file, openedInfo, nil
}

func previewPackageFile(root, relative string) (*FilePreview, error) {
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return nil, errors.New("Skill package root is unavailable")
	}
	defer rootFS.Close()
	file, info, err := openPackageFile(rootFS, relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return previewOpenedPackageFile(file, relative, info)
}

func previewOpenedPackageFile(file *os.File, relative string, info os.FileInfo) (*FilePreview, error) {
	data, err := io.ReadAll(io.LimitReader(file, maxInspectPreviewBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Skill file: %w", err)
	}
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	kind, mediaType, status := classifyPackageSample(relative, info.Size(), sample)
	preview := &FilePreview{Path: relative, Kind: kind, MediaType: mediaType, Status: status, Size: info.Size()}
	if status == "binary" {
		preview.Notice = "Binary files are shown as metadata only."
		return preview, nil
	}
	if int64(len(data)) > maxInspectPreviewBytes {
		data = data[:maxInspectPreviewBytes]
		preview.Status = "truncated"
		preview.Notice = fmt.Sprintf("Preview is limited to %d bytes of a %d-byte file.", maxInspectPreviewBytes, info.Size())
	}
	preview.Content = string(data)
	preview.BytesReturned = int64(len(data))
	return preview, nil
}

func classifyOpenedPackageFile(file *os.File, relative string, size int64) (string, string, string, error) {
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", "", "", err
	}
	kind, mediaType, status := classifyPackageSample(relative, size, buf[:n])
	return kind, mediaType, status, nil
}

func classifyPackageSample(relative string, size int64, sample []byte) (string, string, string) {
	ext := strings.ToLower(filepath.Ext(relative))
	mediaType := mime.TypeByExtension(ext)
	if mediaType == "" {
		mediaType = http.DetectContentType(sample)
	}
	kind := "text"
	switch ext {
	case ".md", ".mdx":
		kind = "markdown"
	case ".json":
		kind = "json"
	}
	textualBytes := validUTF8Sample(sample)
	if !textualBytes {
		return "binary", mediaType, "binary"
	}
	if size > maxInspectPreviewBytes {
		return kind, mediaType, "large"
	}
	return kind, mediaType, "ready"
}

func validUTF8Sample(sample []byte) bool {
	if bytes.ContainsRune(sample, '\x00') {
		return false
	}
	if utf8.Valid(sample) {
		return true
	}
	for trim := 1; trim <= 3 && trim < len(sample); trim++ {
		prefix, suffix := sample[:len(sample)-trim], sample[len(sample)-trim:]
		if utf8.Valid(prefix) && !utf8.FullRune(suffix) {
			return true
		}
	}
	return false
}
