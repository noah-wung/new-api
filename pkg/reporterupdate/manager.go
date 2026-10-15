package reporterupdate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type Profile struct {
	Source    string `json:"source"`
	Config    string `json:"config"`
	TaskName  string `json:"task_name"`
	Streaming bool   `json:"streaming"`
	Legacy    bool   `json:"legacy,omitempty"`
}
type Release struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Binary  string `json:"binary"`
	SHA256  string `json:"sha256"`
}
type Installation struct {
	Schema     int       `json:"schema"`
	ID         string    `json:"id"`
	Server     string    `json:"server"`
	AutoUpdate bool      `json:"auto_update"`
	Keys       TrustKeys `json:"keys"`
	Profiles   []Profile `json:"profiles"`
	Current    Release   `json:"current"`
}
type UpdateState struct {
	Version     string  `json:"reporter_version"`
	Available   string  `json:"available_version"`
	Phase       string  `json:"phase"`
	Error       string  `json:"error_code"`
	LastCheck   int64   `json:"last_check_at"`
	NextCheck   int64   `json:"next_check_at"`
	Failures    int     `json:"failures"`
	Sequence    int64   `json:"catalog_sequence"`
	CatalogHash string  `json:"catalog_hash"`
	Quarantined string  `json:"quarantined_release,omitempty"`
	Previous    Release `json:"previous"`
}
type Task struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	Exists     bool   `json:"exists"`
}
type transaction struct {
	Enrollment bool      `json:"enrollment,omitempty"`
	ID         string    `json:"id"`
	CreatedAt  int64     `json:"created_at"`
	Phase      string    `json:"phase"`
	Catalog    []byte    `json:"catalog"`
	Candidate  Release   `json:"candidate"`
	Previous   Release   `json:"previous"`
	Profiles   []Profile `json:"profiles"`
	Tasks      []Task    `json:"tasks"`
}
type taskPlatform interface {
	Snapshot(string) (Task, error)
	Disable(string) error
	Install(Task) error
	Start(string) error
	Delete(string) error
	Running(string) (bool, error)
	Definition(string, string, []string, int, bool) (Task, error)
	Retarget(Task, string, []string) (Task, error)
	Detach(string, string, string) error
}
type Manager struct {
	Target   string
	Root     string
	HTTP     *http.Client
	platform taskPlatform
	now      func() time.Time
	probe    func(context.Context, string, []Profile) error
	health   func(context.Context, Profile, string, string) error
	stop     func(context.Context, Profile) error
}

func NewManager(root string) (*Manager, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = SecureDir(abs); err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	p, err := newPlatform(abs)
	if err != nil {
		return nil, err
	}
	m := &Manager{Root: abs, Target: runtime.GOOS + "-" + runtime.GOARCH, HTTP: &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, platform: p, now: time.Now}
	m.probe = m.probeBinary
	m.health = m.profileHealthy
	m.stop = m.stopProfile
	return m, nil
}
func (m *Manager) read() (Installation, UpdateState, error) {
	var i Installation
	var s UpdateState
	if err := Load(filepath.Join(m.Root, "installation.json"), &i); err != nil {
		return i, s, err
	}
	if len(i.Keys) == 0 || i.Schema != 1 || !ValidID(i.ID) || !ValidID(i.Current.ID) || !ValidVersion(i.Current.Version) || !validateBinaryPath(m.Root, i.Current.Binary) || len(i.Profiles) == 0 {
		return i, s, errors.New("invalid_installation")
	}
	if _, err := Origin(i.Server); err != nil {
		return i, s, err
	}
	seen := map[string]bool{}
	for _, p := range i.Profiles {
		if !ValidID(p.Source) || seen[p.Source] || !filepath.IsAbs(p.Config) || !validTaskName(p.TaskName) {
			return i, s, errors.New("invalid_profile")
		}
		seen[p.Source] = true
	}
	if err := Load(filepath.Join(m.Root, "update-state.json"), &s); err != nil && !os.IsNotExist(err) {
		return i, s, err
	}
	s.Version = i.Current.Version
	return i, s, nil
}
func (m *Manager) saveState(s UpdateState) error {
	if e := Save(filepath.Join(m.Root, "update-state.json"), s); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = m.SendStatus(ctx)
	return nil
}
func (m *Manager) saveTransaction(t transaction) error {
	return Save(filepath.Join(m.Root, "transaction.json"), t)
}
func (m *Manager) lock() (func(), error) { return Lock(filepath.Join(m.Root, "update.lock")) }
func randomID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func jitter(max time.Duration) time.Duration {
	n, e := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if e != nil {
		return 0
	}
	return time.Duration(n.Int64())
}
func (m *Manager) fetch(ctx context.Context, server, path string, limit int64) ([]byte, error) {
	endpoint, e := ResourceURL(server, path)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if e != nil {
		return nil, e
	}
	resp, e := m.HTTP.Do(req)
	if e != nil {
		return nil, errors.New("network_error")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 || resp.StatusCode == 204 {
		return nil, os.ErrNotExist
	}
	if resp.StatusCode != 200 {
		return nil, errors.New("release_unavailable")
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if e != nil {
		return nil, errors.New("network_error")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("download_too_large")
	}
	return data, nil
}
func acceptCatalog(data []byte, i *Installation, s *UpdateState, now time.Time) (Catalog, error) {
	c, e := Verify(data, i.Keys, now)
	if e != nil {
		return c, e
	}
	h := Digest(data)
	if c.Sequence < s.Sequence || (c.Sequence == s.Sequence && s.CatalogHash != "" && s.CatalogHash != h) {
		return c, errors.New("catalog_replay")
	}
	s.Sequence = c.Sequence
	s.CatalogHash = h
	for id, key := range c.NextKeys {
		if old, ok := i.Keys[id]; ok && old != key {
			return c, errors.New("key_conflict")
		}
		i.Keys[id] = key
	}
	return c, nil
}

// CheckAndPrepare never stops collectors. A nonempty result is a durable plan
// for a detached Apply process; candidates are verified again before execution.
func (m *Manager) CheckAndPrepare(ctx context.Context, force bool) (plan string, err error) {
	unlock, e := m.lock()
	if e != nil {
		return "", e
	}
	defer unlock()
	i, s, e := m.read()
	if e != nil {
		return "", e
	}
	var pending transaction
	if e = Load(filepath.Join(m.Root, "transaction.json"), &pending); e == nil && pending.Phase != "committed" && pending.Phase != "rolled_back" {
		return "", errors.New("recovery_pending")
	}
	if e != nil && !os.IsNotExist(e) {
		return "", e
	}
	if !force && (!i.AutoUpdate || s.NextCheck > m.now().Unix()) {
		return "", nil
	}
	s.LastCheck = m.now().Unix()
	previousError := s.Error
	s.Phase = "checking"
	s.Error = ""
	defer func() {
		if err != nil {
			s.Failures++
			s.Error = ErrorCode(err)
			s.Phase = "check_failed"
			delay := 6 * time.Hour
			if s.Failures == 1 {
				delay = 15 * time.Minute
			}
			if s.Failures == 2 {
				delay = time.Hour
			}
			s.NextCheck = m.now().Add(delay + jitter(time.Minute)).Unix()
		} else {
			s.Failures = 0
			s.NextCheck = m.now().Add(24*time.Hour + jitter(time.Hour)).Unix()
		}
		if saveErr := m.saveState(s); err == nil {
			err = saveErr
		}
	}()
	data, e := m.fetch(ctx, i.Server, "/api/external-usage/reporter/updates/stable", MaxManifestBytes)
	if errors.Is(e, os.ErrNotExist) {
		s.Phase = "idle"
		return "", nil
	}
	if e != nil {
		return "", e
	}
	c, e := acceptCatalog(data, &i, &s, m.now())
	if e != nil {
		return "", e
	}
	if e = Save(filepath.Join(m.Root, "installation.json"), i); e != nil {
		return "", e
	}
	s.Available = c.Version
	if !Newer(c.Version, i.Current.Version) {
		s.Available = i.Current.Version
	}
	if s.Quarantined == c.ReleaseID && !force {
		s.Phase = "rolled_back"
		s.Error = previousError
		return "", nil
	}
	if c.Paused || !Newer(c.Version, i.Current.Version) || !InRollout(i.ID, c) || (s.Quarantined == c.ReleaseID && !force) {
		s.Phase = "idle"
		return "", nil
	}
	a, e := c.Artifact(m.Target)
	if e != nil {
		return "", e
	}
	free, e := FreeBytes(m.Root)
	if e != nil {
		return "", e
	}
	if free < uint64(a.Size)+(20<<20) {
		return "", errors.New("insufficient_space")
	}
	stage := filepath.Join(m.Root, "staging", randomID())
	if e = SecureDir(stage); e != nil {
		return "", e
	}
	defer os.RemoveAll(stage)
	s.Phase = "downloading"
	_ = m.saveState(s)
	binary, e := m.download(ctx, i.Server, a, stage)
	if e != nil {
		return "", e
	}
	if e = m.probe(ctx, binary, i.Profiles); e != nil {
		return "", errors.New("self_check_failed")
	}
	name, _ := TargetName(a.Target)
	dest := filepath.Join(m.Root, "releases", c.ReleaseID, a.Target, name)
	if e = SecureDir(filepath.Dir(dest)); e != nil {
		return "", e
	}
	if _, e = os.Stat(dest); e == nil {
		if e = verifyFile(dest, a); e != nil {
			return "", e
		}
	} else if os.IsNotExist(e) {
		if e = os.Rename(binary, dest); e != nil {
			return "", e
		}
	} else {
		return "", e
	}
	t := transaction{ID: randomID(), CreatedAt: m.now().Unix(), Phase: "prepared", Catalog: data, Candidate: Release{ID: c.ReleaseID, Version: c.Version, Binary: dest, SHA256: a.SHA256}, Previous: i.Current, Profiles: i.Profiles}
	for _, p := range i.Profiles {
		task, e := m.platform.Snapshot(p.TaskName)
		if e != nil {
			return "", e
		}
		t.Tasks = append(t.Tasks, task)
	}
	updateTask, e := m.platform.Snapshot(updaterTask)
	if e != nil {
		return "", e
	}
	t.Tasks = append(t.Tasks, updateTask)
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
	s.Phase = "prepared"
	return t.ID, nil
}
func (m *Manager) download(ctx context.Context, server string, a Artifact, stage string) (string, error) {
	endpoint, e := ResourceURL(server, a.Path)
	if e != nil {
		return "", e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if e != nil {
		return "", e
	}
	resp, e := m.HTTP.Do(req)
	if e != nil {
		return "", errors.New("network_error")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("release_unavailable")
	}
	file, e := os.OpenFile(filepath.Join(stage, "candidate"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if e != nil {
		return "", e
	}
	n, e := io.Copy(file, io.LimitReader(resp.Body, a.Size+1))
	if e != nil {
		file.Close()
		return "", e
	}
	if e = file.Sync(); e != nil {
		file.Close()
		return "", e
	}
	if e = file.Close(); e != nil {
		return "", e
	}
	if n != a.Size {
		return "", errors.New("artifact_mismatch")
	}
	if e = verifyFile(file.Name(), a); e != nil {
		return "", e
	}
	return file.Name(), nil
}
func verifyFile(path string, a Artifact) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return VerifyBinary(f, a)
}
