package release

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// ErrSourceURL reports a core asset URL that is not an allowed release asset
// of the expected project. Only the exact download prefix, with no userinfo,
// query, fragment, path traversal or extra segment, is accepted.
var ErrSourceURL = errors.New("core release URL must be an official Xray-core, official sing-box or qui sing-box release asset")

const quiCorePrefix = "https://github.com/jingniao/qui/releases/download/"

var quiCoreAsset = regexp.MustCompile(`^sing-box-[0-9]+\.[0-9]+\.[0-9]+-qui\.[1-9][0-9]*-linux-(amd64|arm64)\.tar\.gz$`)

// QuiCoreSource identifies our release source after CoreAssetURL validation.
func QuiCoreSource(value string) bool {
	return strings.HasPrefix(value, quiCorePrefix)
}

var coreSourcePrefixes = map[string]string{
	"xray":     "https://github.com/XTLS/Xray-core/releases/download/",
	"sing-box": "https://github.com/SagerNet/sing-box/releases/download/",
}

// CoreRedirectHosts are the only hosts an official asset download may be
// redirected to. GitHub answers release-asset downloads with 302 responses.
var CoreRedirectHosts = []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}

func CoreRedirectHost(host string) bool {
	for _, allowed := range CoreRedirectHosts {
		if host == allowed {
			return true
		}
	}
	return false
}

// CoreAssetURL validates an operator-entered allowed release asset URL and
// returns it with the expected <tag>/<asset> parts. Remote data never supplies
// this value: it comes from the Owner and is re-validated by the Agent.
func CoreAssetURL(component, value string) (*url.URL, string, error) {
	prefix, ok := coreSourcePrefixes[component]
	qui := component == "sing-box" && QuiCoreSource(value)
	if qui {
		prefix = quiCorePrefix
	}
	if !ok || !strings.HasPrefix(value, prefix) || len(value) > 512 {
		return nil, "", ErrSourceURL
	}
	remainder := strings.TrimPrefix(value, prefix)
	if strings.Count(remainder, "/") != 1 {
		return nil, "", ErrSourceURL
	}
	tag, asset, _ := strings.Cut(remainder, "/")
	if qui && (!quiCoreAsset.MatchString(asset) || strings.Contains(tag, "..")) {
		return nil, "", ErrSourceURL
	}
	if !validAssetPart(tag, 64) || !validAssetPart(asset, 128) || tag == "." || tag == ".." || asset == "." || asset == ".." {
		return nil, "", ErrSourceURL
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || u.Path != strings.TrimPrefix(prefix, "https://github.com")+tag+"/"+asset {
		return nil, "", ErrSourceURL
	}
	if ArchiveKind(asset) == "" {
		return nil, "", ErrSourceURL
	}
	if !allowedArchive(component, asset) {
		return nil, "", ErrSourceURL
	}
	return u, tag, nil
}

func validAssetPart(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.ContainsAny(value, "\\\x00") {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz._+-", r) {
			return false
		}
	}
	return true
}

// ArchiveKind reports the accepted archive container for an asset name, or an
// empty string when the name is not an accepted archive.
func ArchiveKind(asset string) string {
	lower := strings.ToLower(asset)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return "tar.gz"
	case strings.HasSuffix(lower, ".zip"):
		return "zip"
	default:
		return ""
	}
}

// allowedArchive reports whether the asset's container is published by the
// component: Xray ships zip archives only, sing-box ships zip and tar.gz.
func allowedArchive(component, asset string) bool {
	kind := ArchiveKind(asset)
	if component == "xray" {
		return kind == "zip"
	}
	return component == "sing-box" && (kind == "zip" || kind == "tar.gz")
}

// CoreAssetMatches checks the advisory platform agreement between an asset name
// and a node's operating system and architecture. It is a panel-side sanity
// check; the Agent still validates the extracted executable natively.
func CoreAssetMatches(component, osName, architecture, asset string) bool {
	lower := strings.ToLower(asset)
	if !allowedArchive(component, lower) || !strings.Contains(lower, "-"+strings.ToLower(osName)+"-") {
		return false
	}
	switch component {
	case "xray":
		if architecture == "arm64" {
			return strings.Contains(lower, "arm64")
		}
		return architecture == "amd64" && strings.Contains(lower, "64") && !strings.Contains(lower, "arm")
	case "sing-box":
		return architecture == "amd64" && strings.Contains(lower, "-amd64") || architecture == "arm64" && strings.Contains(lower, "-arm64")
	default:
		return false
	}
}

// CoreBinaryName is the single expected executable base name inside an archive.
func CoreBinaryName(component, goos string) string {
	name := component
	if goos == "windows" {
		name += ".exe"
	}
	return name
}
