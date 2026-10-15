// Package reporterupdate owns signed releases and recoverable, user-scoped updates.
package reporterupdate

import (
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

//go:embed VERSION
var Version string

func init() { Version = strings.TrimSpace(Version) }

var Commit = "development"
var BuildReleaseID = "development"

const MaxManifestBytes = 64 << 10
const MaxBinaryBytes = 100 << 20
const ProtocolVersion = 1
const ConfigSchema = 1
const StateSchema = 2
const QueueSchema = 1

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`)
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

func (r Range) Contains(v int) bool { return r.Min > 0 && r.Min <= v && v <= r.Max }

type Artifact struct {
	Target string `json:"target"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Catalog struct {
	SchemaVersion  int               `json:"schema_version"`
	Sequence       int64             `json:"catalog_sequence"`
	IssuedAt       int64             `json:"issued_at"`
	ExpiresAt      int64             `json:"expires_at"`
	ReleaseID      string            `json:"release_id"`
	Version        string            `json:"reporter_version"`
	Commit         string            `json:"commit"`
	Protocol       Range             `json:"report_protocol"`
	Config         Range             `json:"config_schema"`
	State          Range             `json:"state_schema"`
	Queue          Range             `json:"queue_schema"`
	RolloutPercent int               `json:"rollout_percent"`
	RolloutSeed    string            `json:"rollout_seed"`
	Paused         bool              `json:"paused"`
	Targets        []Artifact        `json:"targets"`
	NextKeys       map[string]string `json:"next_keys,omitempty"`
}

type Envelope struct {
	KeyID     string `json:"key_id"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}
type TrustKeys map[string]string

func ValidID(value string) bool      { return identifier.MatchString(value) }
func ValidVersion(value string) bool { return versionPattern.MatchString(value) }
func Newer(a, b string) bool {
	if !ValidVersion(a) || !ValidVersion(b) {
		return false
	}
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range aa {
		if len(aa[i]) != len(bb[i]) {
			return len(aa[i]) > len(bb[i])
		}
		if aa[i] != bb[i] {
			return aa[i] > bb[i]
		}
	}
	return false
}
func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func TargetName(target string) (string, error) {
	switch target {
	case "darwin-arm64", "darwin-amd64":
		return "usage-reporter", nil
	case "windows-amd64":
		return "usage-reporter.exe", nil
	}
	return "", errors.New("unsupported reporter target")
}
func (c Catalog) Artifact(target string) (Artifact, error) {
	for _, a := range c.Targets {
		if a.Target == target {
			return a, nil
		}
	}
	return Artifact{}, errors.New("target unavailable")
}

func (c Catalog) SameBuild(other Catalog) bool {
	if c.ReleaseID != other.ReleaseID || c.Version != other.Version || c.Commit != other.Commit || len(c.Targets) != len(other.Targets) {
		return false
	}
	for _, artifact := range c.Targets {
		match, err := other.Artifact(artifact.Target)
		if err != nil || match != artifact {
			return false
		}
	}
	return true
}

func Verify(data []byte, keys TrustKeys, now time.Time) (Catalog, error) {
	var env Envelope
	var c Catalog
	if len(data) > MaxManifestBytes || common.Unmarshal(data, &env) != nil {
		return c, errors.New("invalid_catalog")
	}
	key, err := base64.StdEncoding.DecodeString(keys[env.KeyID])
	if err != nil || len(key) != ed25519.PublicKeySize {
		return c, errors.New("untrusted_key")
	}
	payload, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return c, errors.New("invalid_catalog")
	}
	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(key), payload, sig) {
		return c, errors.New("invalid_signature")
	}
	if common.Unmarshal(payload, &c) != nil || c.SchemaVersion != 1 || c.Sequence < 1 || !ValidID(c.ReleaseID) || (len(c.Version) > 64 || !ValidVersion(c.Version)) || len(c.Commit) == 0 || len(c.Commit) > 64 {
		return c, errors.New("invalid_catalog")
	}
	if c.ExpiresAt <= c.IssuedAt || c.IssuedAt > now.Add(5*time.Minute).Unix() || c.ExpiresAt <= now.Unix() {
		return c, errors.New("expired_catalog")
	}
	if c.RolloutPercent < 0 || c.RolloutPercent > 100 || !ValidID(c.RolloutSeed) {
		return c, errors.New("invalid_rollout")
	}
	seen := map[string]bool{}
	for _, a := range c.Targets {
		name, err := TargetName(a.Target)
		if err != nil || seen[a.Target] || a.Size < 1 || a.Size > MaxBinaryBytes || len(a.SHA256) != 64 {
			return c, errors.New("invalid_artifact")
		}
		if _, err := hex.DecodeString(a.SHA256); err != nil || a.SHA256 != strings.ToLower(a.SHA256) {
			return c, errors.New("invalid_artifact")
		}
		expected := "/api/external-usage/reporter/releases/" + c.ReleaseID + "/" + a.Target + "/" + name
		if a.Path != expected {
			return c, errors.New("invalid_artifact_path")
		}
		seen[a.Target] = true
	}
	if len(seen) != 3 {
		return c, errors.New("incomplete_release")
	}
	if !c.Protocol.Contains(ProtocolVersion) || !c.Config.Contains(ConfigSchema) || !c.State.Contains(StateSchema) || !c.Queue.Contains(QueueSchema) {
		return c, errors.New("incompatible_release")
	}
	for id, encoded := range c.NextKeys {
		k, e := base64.StdEncoding.DecodeString(encoded)
		if !ValidID(id) || e != nil || len(k) != ed25519.PublicKeySize {
			return c, errors.New("invalid_next_key")
		}
	}
	return c, nil
}

func Sign(c Catalog, id string, key ed25519.PrivateKey) ([]byte, error) {
	if !ValidID(id) || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid signing key")
	}
	payload, err := common.Marshal(c)
	if err != nil {
		return nil, err
	}
	return common.Marshal(Envelope{KeyID: id, Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, payload))})
}
func VerifyBinary(r io.Reader, a Artifact) error {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, a.Size+1))
	if err != nil {
		return err
	}
	if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return errors.New("artifact_mismatch")
	}
	return nil
}
func Origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("invalid_gateway_origin")
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), nil
}
func ResourceURL(server, resource string) (string, error) {
	origin, err := Origin(server)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(resource, "/api/external-usage/reporter/") || path.Clean(resource) != resource || strings.ContainsAny(resource, "%?#\\") {
		return "", errors.New("invalid_resource")
	}
	return origin + resource, nil
}
func InRollout(id string, c Catalog) bool {
	sum := sha256.Sum256([]byte(id + "\x00" + c.RolloutSeed))
	bucket := (int(sum[0])*256 + int(sum[1])) % 100
	return bucket < c.RolloutPercent
}
func SchemaDescription() string {
	return fmt.Sprintf("protocol=%d config=%d state=%d queue=%d", ProtocolVersion, ConfigSchema, StateSchema, QueueSchema)
}
