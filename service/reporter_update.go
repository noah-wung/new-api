package service

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/reporterupdate"
)

func ReporterTrustKeys() (reporterupdate.TrustKeys, error) {
	var keys reporterupdate.TrustKeys
	if e := common.UnmarshalJsonStr(os.Getenv("NEW_API_USAGE_REPORTER_TRUST_KEYS"), &keys); e != nil || len(keys) == 0 {
		return nil, errors.New("reporter signing trust is not configured")
	}
	return keys, nil
}
func reporterReleaseRoot() (string, error) {
	root := os.Getenv("NEW_API_USAGE_REPORTER_RELEASE_DIR")
	if root == "" {
		return "", errors.New("reporter release directory is not configured")
	}
	abs, e := filepath.Abs(root)
	if e != nil {
		return "", e
	}
	return filepath.EvalSymlinks(abs)
}
func reporterAssetPath(id, target string) (string, error) {
	if !reporterupdate.ValidID(id) {
		return "", errors.New("invalid release")
	}
	name, e := reporterupdate.TargetName(target)
	if e != nil {
		return "", e
	}
	root, e := reporterReleaseRoot()
	if e != nil {
		return "", e
	}
	path := filepath.Join(root, id, target, name)
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", e
	}
	if resolved != path {
		return "", errors.New("release symlinks are not allowed")
	}
	return path, nil
}
func ValidateReporterRelease(id string) ([]byte, reporterupdate.Catalog, error) {
	var catalog reporterupdate.Catalog
	if !reporterupdate.ValidID(id) {
		return nil, catalog, errors.New("invalid release")
	}
	root, e := reporterReleaseRoot()
	if e != nil {
		return nil, catalog, e
	}
	file, e := os.Open(filepath.Join(root, id, "catalog.json"))
	if e != nil {
		return nil, catalog, e
	}
	defer file.Close()
	data, e := io.ReadAll(io.LimitReader(file, reporterupdate.MaxManifestBytes+1))
	if e != nil {
		return nil, catalog, e
	}
	keys, e := ReporterTrustKeys()
	if e != nil {
		return nil, catalog, e
	}
	catalog, e = reporterupdate.Verify(data, keys, time.Now())
	if e != nil || catalog.ReleaseID != id {
		return nil, catalog, errors.New("invalid signed release")
	}
	for _, a := range catalog.Targets {
		path, e := reporterAssetPath(id, a.Target)
		if e != nil {
			return nil, catalog, e
		}
		f, e := os.Open(path)
		if e != nil {
			return nil, catalog, e
		}
		e = reporterupdate.VerifyBinary(f, a)
		f.Close()
		if e != nil {
			return nil, catalog, e
		}
	}
	return data, catalog, nil
}
func ActivateReporterRelease(id string, userID int) error {
	data, c, e := ValidateReporterRelease(id)
	if e != nil {
		return e
	}
	sort.Slice(c.Targets, func(i, j int) bool { return c.Targets[i].Target < c.Targets[j].Target })
	identity, e := common.Marshal(struct {
		Version, Commit string
		Targets         []reporterupdate.Artifact
	}{c.Version, c.Commit, c.Targets})
	if e != nil {
		return e
	}
	return model.ActivateReporterDeployment(model.ReporterRelease{ReleaseID: id, Version: c.Version, ArtifactDigest: reporterupdate.Digest(identity), ActivatedBy: userID}, model.ReporterDeployment{ReleaseID: id, Sequence: c.Sequence, Catalog: string(data), CatalogHash: reporterupdate.Digest(data), Paused: c.Paused})
}
func ActiveReporterCatalog() ([]byte, reporterupdate.Catalog, error) {
	var c reporterupdate.Catalog
	d, e := model.GetReporterDeployment()
	if e != nil {
		return nil, c, e
	}
	if d.Paused {
		return nil, c, errors.New("release distribution paused")
	}
	keys, e := ReporterTrustKeys()
	if e != nil {
		return nil, c, e
	}
	data := []byte(d.Catalog)
	c, e = reporterupdate.Verify(data, keys, time.Now())
	if e != nil {
		return nil, c, e
	}
	for _, a := range c.Targets {
		p, e := reporterAssetPath(c.ReleaseID, a.Target)
		if e != nil {
			return nil, c, e
		}
		info, e := os.Stat(p)
		if e != nil || info.Size() != a.Size {
			return nil, c, errors.New("release assets unavailable")
		}
	}
	return data, c, nil
}
func ResolveReporterReleaseAsset(id, target string) (string, error) {
	var row model.ReporterRelease
	if e := model.DB.Where("release_id = ?", id).First(&row).Error; e != nil {
		return "", e
	}
	return reporterAssetPath(id, target)
}
func SaveReporterDeviceStatus(device *model.ExternalUsageDevice, status model.ReporterDeviceStatus) error {
	if device.Status != model.ExternalUsageStatusActive || !isSourceAllowed(device.GetAllowedSources(), status.Source) {
		return errors.New("device source is not authorized")
	}
	if e := ValidateExternalUsageSourceEnabled(status.Source); e != nil {
		return e
	}
	u, e := model.GetUserById(device.UserID, false)
	if e != nil || u.Status != common.UserStatusEnabled {
		return errors.New("user is not active")
	}
	if !reporterupdate.ValidVersion(status.Version) || (status.Available != "" && !reporterupdate.ValidVersion(status.Available)) {
		return errors.New("invalid reporter version")
	}
	if !reporterupdate.ValidPhase(status.Phase) || !reporterupdate.ValidErrorCode(status.ErrorCode) {
		return errors.New("invalid reporter status")
	}
	if status.LastCheck < 0 || status.LastCheck > common.GetTimestamp()+300 {
		return errors.New("invalid check timestamp")
	}
	status.DeviceID = device.Id
	status.UserID = device.UserID
	status.ReceivedAt = common.GetTimestamp()
	return model.SaveReporterStatus(status)
}
