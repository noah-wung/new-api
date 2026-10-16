package reporterupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// CanEnroll prevents consuming one-time tokens while another installation or
// unfinished transaction owns the local runtime.
func (m *Manager) CanEnroll(server string) error {
	unlock, e := m.lock()
	if e != nil {
		return e
	}
	defer unlock()
	origin, e := Origin(server)
	if e != nil {
		return e
	}
	i, _, e := m.read()
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if e == nil && i.Server != origin {
		return errors.New("gateway_identity_change")
	}
	t, e := m.readTransaction()
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if t.Phase != "committed" && t.Phase != "rolled_back" {
		return errors.New("recovery_pending")
	}
	return nil
}

// Enroll prepares the same recoverable transaction as an ordinary update. It
// adopts only explicitly selected profiles, leaving other legacy tasks alone.
func (m *Manager) Enroll(ctx context.Context, server string, keys TrustKeys, catalog []byte, profiles []Profile, automatic bool) (string, error) {
	unlock, e := m.lock()
	if e != nil {
		return "", e
	}
	defer unlock()
	origin, e := Origin(server)
	if e != nil {
		return "", e
	}
	c, e := Verify(catalog, keys, m.now())
	if e != nil {
		return "", e
	}
	if c.ReleaseID != BuildReleaseID || c.Version != Version || c.Commit != Commit {
		return "", errors.New("installer_build_mismatch")
	}
	a, e := c.Artifact(runtime.GOOS + "-" + runtime.GOARCH)
	if e != nil {
		return "", e
	}
	self, e := os.Executable()
	if e != nil {
		return "", e
	}
	if e = verifyFile(self, a); e != nil {
		return "", e
	}
	name, _ := TargetName(a.Target)
	dest := filepath.Join(m.Root, "releases", c.ReleaseID, a.Target, name)
	if e = stageVerifiedBinary(self, dest, a); e != nil {
		return "", e
	}
	i, s, e := m.read()
	fresh := os.IsNotExist(e)
	if e != nil && !fresh {
		return "", e
	}
	if fresh {
		i = Installation{Schema: 1, ID: randomID(), Server: origin, Keys: keys, Current: Release{ID: c.ReleaseID, Version: c.Version, Binary: dest, SHA256: a.SHA256}}
	} else if i.Server != origin {
		return "", errors.New("gateway_identity_change")
	}
	var prior transaction
	if e = Load(filepath.Join(m.Root, "transaction.json"), &prior); e == nil && prior.Phase != "committed" && prior.Phase != "rolled_back" {
		return "", errors.New("recovery_pending")
	}
	if e != nil && !os.IsNotExist(e) {
		return "", e
	}
	if _, e = acceptCatalog(catalog, &i, &s, m.now()); e != nil {
		return "", e
	}
	i.AutoUpdate = automatic
	for _, profile := range profiles {
		if !ValidID(profile.Source) || !validTaskName(profile.TaskName) || !filepath.IsAbs(profile.Config) {
			return "", errors.New("invalid_profile")
		}
		found := false
		for _, old := range i.Profiles {
			if old.Source == profile.Source {
				if old.Config != profile.Config || old.TaskName != profile.TaskName {
					return "", errors.New("profile_identity_change")
				}
				found = true
			}
		}
		if !found {
			snapshot, e := m.platform.Snapshot(profile.TaskName)
			if e != nil {
				return "", e
			}
			profile.Legacy = snapshot.Exists
			i.Profiles = append(i.Profiles, profile)
		}
	}
	if len(i.Profiles) == 0 {
		return "", errors.New("no_profiles")
	}
	if e = m.probe(ctx, dest, i.Profiles); e != nil {
		return "", e
	}
	t := transaction{ID: randomID(), CreatedAt: m.now().Unix(), Phase: "prepared", Catalog: catalog, Candidate: Release{ID: c.ReleaseID, Version: c.Version, Binary: dest, SHA256: a.SHA256}, Previous: i.Current, Profiles: i.Profiles}
	t.Enrollment = true
	for _, profile := range i.Profiles {
		snapshot, e := m.platform.Snapshot(profile.TaskName)
		if e != nil {
			return "", e
		}
		t.Tasks = append(t.Tasks, snapshot)
	}
	u, e := m.platform.Snapshot(updaterTask)
	if e != nil {
		return "", e
	}
	t.Tasks = append(t.Tasks, u)
	if e = Save(filepath.Join(m.Root, "installation.json"), i); e != nil {
		return "", e
	}
	if e = m.saveTransaction(t); e != nil {
		return "", e
	}
	recovery, e := m.platform.Definition(recoveryTask, i.Current.Binary, []string{"--update-recover", "--runtime", m.Root, "--log-file", filepath.Join(m.Root, "update.log")}, 1, false)
	if e != nil {
		return "", e
	}
	if e = m.platform.Install(recovery); e != nil {
		return "", e
	}
	s.Version = i.Current.Version
	s.Phase = "prepared"
	s.NextCheck = time.Now().Add(jitter(10 * time.Minute)).Unix()
	if e = m.saveState(s); e != nil {
		return "", e
	}
	return t.ID, nil
}
