package release

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

const MaxUpdateRequestBytes = 24 << 10
const MaxUpdateResultBytes = 4096

var ErrUpdate = errors.New("invalid fixed core update request or result")
var updateIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

type UpdateRequest struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	NodeID        string `json:"node_id"`
	Revision      int64  `json:"revision"`
	Operation     string `json:"operation"`
	OriginalID    string `json:"original_id,omitempty"`
	Release       Signed `json:"release"`
}
type UpdateResult struct {
	ID                string `json:"id"`
	NodeID            string `json:"node_id"`
	Revision          int64  `json:"revision"`
	TargetDigest      string `json:"target_digest"`
	Status            string `json:"status"`
	Code              string `json:"code"`
	InstalledDigest   string `json:"installed_digest,omitempty"`
	InstalledVersion  string `json:"installed_version,omitempty"`
	InstalledSequence int64  `json:"installed_sequence,omitempty"`
}

func ValidUpdateID(id string) bool { return updateIDPattern.MatchString(id) }
func (r UpdateRequest) Validate() error {
	return validateUpdateRequest(r, r.Release.Manifest.Component == "xray" || r.Release.Manifest.Component == "sing-box")
}

func validateUpdateRequest(r UpdateRequest, componentAllowed bool) error {
	if r.SchemaVersion != 1 || !ValidUpdateID(r.ID) || r.NodeID == "" || len(r.NodeID) > 128 || r.Revision < 1 || r.Release.Manifest.Validate() != nil || !componentAllowed {
		return ErrUpdate
	}
	if r.Operation == "install" {
		if r.OriginalID != "" {
			return ErrUpdate
		}
	} else if r.Operation == "recover" {
		if !ValidUpdateID(r.OriginalID) || r.ID == r.OriginalID {
			return ErrUpdate
		}
	} else {
		return ErrUpdate
	}
	return nil
}
func validUpdateDigest(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == 32 && hex.EncodeToString(data) == value
}
func (r UpdateResult) Validate() error {
	if !ValidUpdateID(r.ID) || r.NodeID == "" || len(r.NodeID) > 128 || r.Revision < 1 || !validUpdateDigest(r.TargetDigest) || r.InstalledSequence < 0 {
		return ErrUpdate
	}
	if r.InstalledDigest != "" && (!validUpdateDigest(r.InstalledDigest) || !versionPattern.MatchString(r.InstalledVersion)) || r.InstalledDigest == "" && (r.InstalledVersion != "" || r.InstalledSequence != 0) {
		return ErrUpdate
	}
	switch r.Status {
	case "success":
		if r.Code != "success" || r.InstalledDigest != r.TargetDigest || r.InstalledSequence < 1 {
			return ErrUpdate
		}
	case "rolled_back":
		if r.Code != "rolled_back" || r.InstalledDigest == "" {
			return ErrUpdate
		}
	case "rejected":
		switch r.Code {
		case "download_failed", "trust_failed", "scope_mismatch", "preflight_failed":
		default:
			return ErrUpdate
		}
	case "failed_closed":
		switch r.Code {
		case "installation_failed", "recovery_failed", "history_invalid":
		default:
			return ErrUpdate
		}
	default:
		return ErrUpdate
	}
	return nil
}
func strictUpdate(data []byte, maximum int, target any) error {
	if len(data) == 0 || len(data) > maximum || uniqueJSONKeys(data) != nil {
		return ErrUpdate
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(&struct{}{}) != io.EOF {
		return ErrUpdate
	}
	return nil
}
func DecodeUpdateRequest(data []byte) (UpdateRequest, error) {
	var r UpdateRequest
	if err := strictUpdate(data, MaxUpdateRequestBytes, &r); err != nil {
		return r, err
	}
	return r, r.Validate()
}
func DecodeUpdateResult(data []byte) (UpdateResult, error) {
	var r UpdateResult
	if err := strictUpdate(data, MaxUpdateResultBytes, &r); err != nil {
		return r, err
	}
	return r, r.Validate()
}

// CoreUpdateRequest is the schema-2 fixed core update command. It carries only
// an official asset URL, the expected stable version and the archive digest;
// no signed catalogue, no script and no executable path.
type CoreUpdateRequest struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	NodeID        string `json:"node_id"`
	Revision      int64  `json:"revision"`
	Sequence      int64  `json:"sequence"`
	Component     string `json:"component"`
	Version       string `json:"version"`
	URL           string `json:"url"`
	SHA256        string `json:"sha256"`
}

var coreVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// StableCoreVersion accepts only plain release versions, so pre-release,
// beta and alpha suffixes are refused before any download.
func StableCoreVersion(value string) bool { return coreVersionPattern.MatchString(value) }

func (r CoreUpdateRequest) Validate() error {
	if r.SchemaVersion != 2 || !ValidUpdateID(r.ID) || r.NodeID == "" || len(r.NodeID) > 128 || r.Revision < 1 || r.Sequence < 1 || !CoreComponent(r.Component) || !StableCoreVersion(r.Version) || !validUpdateDigest(r.SHA256) {
		return ErrUpdate
	}
	if _, _, err := CoreAssetURL(r.Component, r.URL); err != nil {
		return ErrUpdate
	}
	return nil
}

// CoreComponent reports whether a component is a managed proxy core.
func CoreComponent(value string) bool { return value == "xray" || value == "sing-box" }

func DecodeCoreUpdateRequest(data []byte) (CoreUpdateRequest, error) {
	var r CoreUpdateRequest
	if err := strictUpdate(data, MaxUpdateRequestBytes, &r); err != nil {
		return r, err
	}
	return r, r.Validate()
}

// ValidateCore checks a core update result. Unlike the signed Agent path, a
// successful core installation reports the installed binary digest, which is
// not the digest of the archive that was verified.
func (r UpdateResult) ValidateCore() error {
	if !ValidUpdateID(r.ID) || r.NodeID == "" || len(r.NodeID) > 128 || r.Revision < 1 || !validUpdateDigest(r.TargetDigest) || r.InstalledSequence < 0 {
		return ErrUpdate
	}
	if r.InstalledDigest != "" && (!validUpdateDigest(r.InstalledDigest) || !versionPattern.MatchString(r.InstalledVersion)) || r.InstalledDigest == "" && (r.InstalledVersion != "" || r.InstalledSequence != 0) {
		return ErrUpdate
	}
	switch r.Status {
	case "success":
		if r.Code != "success" || r.InstalledDigest == "" || r.InstalledSequence < 1 {
			return ErrUpdate
		}
	case "rolled_back":
		if r.Code != "rolled_back" || r.InstalledDigest == "" {
			return ErrUpdate
		}
	case "rejected":
		switch r.Code {
		case "checksum_mismatch", "version_mismatch", "download_failed", "unsupported_url", "scope_mismatch", "archive_invalid", "preflight_failed":
		default:
			return ErrUpdate
		}
	case "failed_closed":
		switch r.Code {
		case "installation_failed", "recovery_failed", "history_invalid":
		default:
			return ErrUpdate
		}
	default:
		return ErrUpdate
	}
	return nil
}

func DecodeCoreUpdateResult(data []byte) (UpdateResult, error) {
	var r UpdateResult
	if err := strictUpdate(data, MaxUpdateResultBytes, &r); err != nil {
		return r, err
	}
	return r, r.ValidateCore()
}
