package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAPIURL       = "https://api.github.com/repos/daoleno/zen/releases?per_page=100"
	ManifestAsset       = "release-manifest.json"
	ManifestSignature   = "release-manifest.json.sig"
	maxManifestSize     = 1 << 20
	maxSignatureSize    = 1 << 10
	maxDaemonArchive    = 256 << 20
	CacheTTL            = 24 * time.Hour
	UpdatePublicKeyPKIX = "MCowBQYDK2VwAyEAD+GvALNRsWlmuB8FL5nwWchsLuXI7tasTGYSqkdabzw="
)

type Artifact struct {
	Path   string `json:"path"`
	Role   string `json:"role"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	GOOS   string `json:"goos,omitempty"`
	GOARCH string `json:"goarch,omitempty"`
}

type Manifest struct {
	SchemaVersion int        `json:"schema_version"`
	Product       string     `json:"product"`
	Version       string     `json:"version"`
	Artifacts     []Artifact `json:"artifacts"`
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type release struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type Candidate struct {
	Version     string
	Tag         string
	Prerelease  bool
	Artifact    Artifact
	ArtifactURL string
}

type Client struct {
	HTTPClient *http.Client
	APIURL     string
	PublicKey  ed25519.PublicKey
	GOOS       string
	GOARCH     string
}

func NewClient() (*Client, error) {
	der, err := base64.StdEncoding.DecodeString(UpdatePublicKeyPKIX)
	if err != nil {
		return nil, fmt.Errorf("decode update public key: %w", err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse update public key: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("update public key is not Ed25519")
	}
	return &Client{
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIURL:     DefaultAPIURL,
		PublicKey:  key,
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
	}, nil
}

func PlatformArtifactName(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "zen-linux-amd64.tar.gz", nil
	case "linux/arm64":
		return "zen-linux-arm64.tar.gz", nil
	case "darwin/arm64":
		return "zen-darwin-arm64.tar.gz", nil
	default:
		return "", fmt.Errorf("self-update is not supported on %s/%s", goos, goarch)
	}
}

func (c *Client) Latest(ctx context.Context, current string) (*Candidate, error) {
	wantName, err := PlatformArtifactName(c.GOOS, c.GOARCH)
	if err != nil {
		return nil, err
	}
	releases, err := c.listReleases(ctx)
	if err != nil {
		return nil, err
	}
	type versionedRelease struct {
		version semVersion
		release release
	}
	var eligible []versionedRelease
	for _, item := range releases {
		if item.Draft {
			continue
		}
		version, err := parseSemVersion(strings.TrimPrefix(item.TagName, "v"))
		if err != nil {
			continue
		}
		eligible = append(eligible, versionedRelease{version: version, release: item})
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		return compareSemVersion(eligible[i].version, eligible[j].version) > 0
	})

	currentVersion, currentErr := parseSemVersion(strings.TrimPrefix(current, "v"))
	for _, item := range eligible {
		if currentErr == nil && compareSemVersion(item.version, currentVersion) <= 0 {
			continue
		}
		assets := assetsByName(item.release.Assets)
		manifestAsset, hasManifest := assets[ManifestAsset]
		signatureAsset, hasSignature := assets[ManifestSignature]
		artifactAsset, hasArtifact := assets[wantName]
		if !hasManifest || !hasSignature || !hasArtifact {
			continue
		}
		manifestBytes, err := c.download(ctx, manifestAsset.URL, maxManifestSize)
		if err != nil {
			continue
		}
		signature, err := c.download(ctx, signatureAsset.URL, maxSignatureSize)
		if err != nil {
			continue
		}
		manifest, err := VerifyManifest(manifestBytes, signature, c.PublicKey)
		if err != nil || manifest.Version != item.version.String() {
			continue
		}
		artifact, ok := manifestArtifact(manifest, c.GOOS, c.GOARCH, wantName)
		if !ok {
			continue
		}
		return &Candidate{
			Version:     manifest.Version,
			Tag:         item.release.TagName,
			Prerelease:  item.release.Prerelease,
			Artifact:    artifact,
			ArtifactURL: artifactAsset.URL,
		}, nil
	}
	return nil, nil
}

func (c *Client) DownloadBinary(ctx context.Context, candidate Candidate) ([]byte, error) {
	if candidate.Artifact.Size <= 0 || candidate.Artifact.Size > maxDaemonArchive {
		return nil, fmt.Errorf("invalid authenticated archive size %d", candidate.Artifact.Size)
	}
	archive, err := c.download(ctx, candidate.ArtifactURL, candidate.Artifact.Size+1)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", candidate.Artifact.Path, err)
	}
	if int64(len(archive)) != candidate.Artifact.Size {
		return nil, fmt.Errorf("archive size mismatch: got %d want %d", len(archive), candidate.Artifact.Size)
	}
	digest := sha256.Sum256(archive)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), candidate.Artifact.SHA256) {
		return nil, errors.New("archive checksum mismatch")
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return nil, err
	}
	return binary, nil
}

func (c *Client) listReleases(ctx context.Context) ([]release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "zen-self-update")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list GitHub releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list GitHub releases: HTTP %s", resp.Status)
	}
	var releases []release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode GitHub releases: %w", err)
	}
	return releases, nil
}

func (c *Client) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "zen-self-update")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func ParseManifest(raw []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("parse signed release manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return manifest, errors.New("signed release manifest has trailing data")
	}
	if manifest.SchemaVersion != 2 || manifest.Product != "zen" {
		return manifest, errors.New("unsupported signed release manifest")
	}
	if _, err := parseSemVersion(manifest.Version); err != nil {
		return manifest, fmt.Errorf("invalid manifest version: %w", err)
	}
	return manifest, nil
}

func VerifyManifest(raw, signature []byte, publicKey ed25519.PublicKey) (Manifest, error) {
	if len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, raw, signature) {
		return Manifest{}, errors.New("release manifest signature verification failed")
	}
	return ParseManifest(raw)
}

func manifestArtifact(manifest Manifest, goos, goarch, name string) (Artifact, bool) {
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == name && artifact.Role == "daemon_archive" && artifact.GOOS == goos && artifact.GOARCH == goarch && artifact.Size > 0 && len(artifact.SHA256) == sha256.Size*2 {
			if _, err := hex.DecodeString(artifact.SHA256); err == nil {
				return artifact, true
			}
		}
	}
	return Artifact{}, false
}

func assetsByName(assets []releaseAsset) map[string]releaseAsset {
	result := make(map[string]releaseAsset, len(assets))
	duplicates := make(map[string]bool)
	for _, asset := range assets {
		if duplicates[asset.Name] {
			continue
		}
		if _, exists := result[asset.Name]; exists {
			delete(result, asset.Name)
			duplicates[asset.Name] = true
			continue
		}
		result[asset.Name] = asset
	}
	return result
}

func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open daemon archive: %w", err)
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	var binary []byte
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read daemon archive: %w", err)
		}
		if strings.TrimPrefix(filepath.Clean(header.Name), "./") != "zen" {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxDaemonArchive {
			return nil, errors.New("daemon archive has invalid zen entry")
		}
		if binary != nil {
			return nil, errors.New("daemon archive has duplicate zen entries")
		}
		binary, err = io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if err != nil || int64(len(binary)) != header.Size {
			return nil, errors.New("read zen binary from archive")
		}
	}
	if len(binary) == 0 {
		return nil, errors.New("daemon archive does not contain zen")
	}
	return binary, nil
}

var renameFile = os.Rename

func ReplaceExecutable(executable string, binary []byte) error {
	target, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("stat executable: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".zen-update-*")
	if err != nil {
		return fmt.Errorf("create update beside executable: %w", err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(info.Mode().Perm() | 0o100); err != nil {
		return fmt.Errorf("set update permissions: %w", err)
	}
	if _, err := temp.Write(binary); err != nil {
		return fmt.Errorf("write update: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync update: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close update: %w", err)
	}
	if err := renameFile(tempPath, target); err != nil {
		return fmt.Errorf("replace executable: %w", err)
	}
	keep = true
	if dir, err := os.Open(filepath.Dir(target)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

type Cache struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version,omitempty"`
}

func ReadCache(path string, now time.Time) (Cache, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Cache{}, false
	}
	var cache Cache
	if json.Unmarshal(raw, &cache) != nil || cache.CheckedAt.After(now.Add(time.Minute)) || now.Sub(cache.CheckedAt) > CacheTTL {
		return Cache{}, false
	}
	if cache.LatestVersion != "" {
		if _, err := parseSemVersion(cache.LatestVersion); err != nil {
			return Cache{}, false
		}
	}
	return cache, true
}

func WriteCache(path string, cache Cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".update-cache-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(append(raw, '\n')); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func NoticeLine(current, latest string) string {
	currentVersion, err := parseSemVersion(current)
	if err != nil || latest == "" {
		return ""
	}
	latestVersion, err := parseSemVersion(latest)
	if err != nil || compareSemVersion(latestVersion, currentVersion) <= 0 {
		return ""
	}
	return fmt.Sprintf("Zen %s is available; run: zen update", latest)
}

type semVersion struct {
	major, minor, patch uint64
	prerelease          []string
}

func (v semVersion) String() string {
	result := fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
	if len(v.prerelease) > 0 {
		result += "-" + strings.Join(v.prerelease, ".")
	}
	return result
}

func parseSemVersion(raw string) (semVersion, error) {
	var result semVersion
	if strings.Contains(raw, "+") {
		raw = strings.SplitN(raw, "+", 2)[0]
	}
	core := raw
	if before, after, ok := strings.Cut(raw, "-"); ok {
		core = before
		if after == "" {
			return result, errors.New("empty prerelease")
		}
		result.prerelease = strings.Split(after, ".")
		for _, part := range result.prerelease {
			if part == "" || !isSemVersionIdentifier(part) || (len(part) > 1 && part[0] == '0' && isNumeric(part)) {
				return result, errors.New("invalid prerelease")
			}
		}
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return result, errors.New("version must have major.minor.patch")
	}
	values := []*uint64{&result.major, &result.minor, &result.patch}
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') || !isNumeric(part) {
			return result, errors.New("invalid numeric version")
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return result, err
		}
		*values[i] = value
	}
	return result, nil
}

func compareSemVersion(a, b semVersion) int {
	for _, pair := range [][2]uint64{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(a.prerelease) == 0 && len(b.prerelease) == 0 {
		return 0
	}
	if len(a.prerelease) == 0 {
		return 1
	}
	if len(b.prerelease) == 0 {
		return -1
	}
	for i := 0; i < len(a.prerelease) && i < len(b.prerelease); i++ {
		left, right := a.prerelease[i], b.prerelease[i]
		if left == right {
			continue
		}
		leftNumeric, rightNumeric := isNumeric(left), isNumeric(right)
		switch {
		case leftNumeric && rightNumeric:
			if len(left) < len(right) || (len(left) == len(right) && left < right) {
				return -1
			}
			return 1
		case leftNumeric:
			return -1
		case rightNumeric:
			return 1
		case left < right:
			return -1
		default:
			return 1
		}
	}
	if len(a.prerelease) < len(b.prerelease) {
		return -1
	}
	if len(a.prerelease) > len(b.prerelease) {
		return 1
	}
	return 0
}

func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func isSemVersionIdentifier(value string) bool {
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && char != '-' {
			return false
		}
	}
	return true
}
