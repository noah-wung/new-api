package reporterupdate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const updaterTask = "pro.newapi.usage-reporter.updater"
const recoveryTask = "pro.newapi.usage-reporter.recovery"
const applyTask = "pro.newapi.usage-reporter.apply"

func validTaskName(name string) bool { return ValidID(name) }
func (m *Manager) readTransaction() (transaction, error) {
	var t transaction
	e := Load(filepath.Join(m.Root, "transaction.json"), &t)
	if e != nil {
		return t, e
	}
	if !ValidID(t.ID) || len(t.Profiles) == 0 {
		return t, errors.New("invalid_transaction")
	}
	return t, nil
}

// LaunchApply is called only after CheckAndPrepare has released the update lock.
func (m *Manager) LaunchApply(id string) error {
	i, _, e := m.read()
	if e != nil {
		return e
	}
	if !ValidID(id) {
		return errors.New("invalid_transaction")
	}
	if e = m.platform.Detach(i.Current.Binary, m.Root, id); e != nil {
		unlock, le := m.lock()
		if le != nil {
			return e
		}
		defer unlock()
		t, te := m.readTransaction()
		_, s, se := m.read()
		if te == nil && se == nil && t.ID == id && t.Phase == "prepared" {
			_ = m.cancelPrepared(&t, &s, e)
		}
		return e
	}
	return nil
}

func (m *Manager) cancelPrepared(t *transaction, s *UpdateState, reason error) error {
	t.Phase = "rolled_back"
	if e := m.saveTransaction(*t); e != nil {
		return e
	}
	s.Phase = "check_failed"
	s.Error = ErrorCode(reason)
	s.NextCheck = m.now().Add(15*time.Minute + jitter(time.Minute)).Unix()
	if e := m.saveState(*s); e != nil {
		return e
	}
	_ = m.platform.Delete(recoveryTask)
	_ = m.platform.Delete(applyTask)
	return nil
}
func (m *Manager) probeBinary(ctx context.Context, binary string, profiles []Profile) error {
	for _, p := range profiles {
		probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		cmd := exec.CommandContext(probeCtx, binary, "--self-check", "--config", p.Config)
		hideProcess(cmd)
		e := cmd.Run()
		cancel()
		if e != nil {
			return e
		}
	}
	return nil
}
func (m *Manager) stopProfile(ctx context.Context, p Profile) error {
	if p.Legacy {
		return m.platformStopLegacy(ctx, p)
	}
	for {
		running, e := m.platform.Running(p.TaskName)
		if e != nil {
			return e
		}
		if !running {
			return nil
		}
		_, _ = QueryControl(ctx, p.Config, "stop")
		select {
		case <-ctx.Done():
			return errors.New("stop_failed")
		case <-time.After(200 * time.Millisecond):
		}
	}
}
func (m *Manager) profileHealthy(ctx context.Context, p Profile, release, tx string) error {
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	var since time.Time
	for {
		h, e := QueryControl(ctx, p.Config, "health")
		if !p.Streaming && e != nil {
			e = Load(p.Config+".health.json", &h)
		}
		if e == nil && h.Ready && h.ReleaseID == release && h.Source == p.Source && (tx == "" || h.Transaction == tx) {
			if !p.Streaming {
				return nil
			}
			if since.IsZero() {
				since = time.Now()
			}
			if time.Since(since) >= 60*time.Second {
				return nil
			}
		} else {
			since = time.Time{}
		}
		select {
		case <-ctx.Done():
			return errors.New("health_check_failed")
		case <-deadline.C:
			return errors.New("health_check_failed")
		case <-time.After(250 * time.Millisecond):
		}
	}
}
func workerArguments(p Profile, tx string) []string {
	args := []string{"--config", p.Config, "--managed", "--transaction", tx, "--log-file", filepath.Join(filepath.Dir(p.Config), "scheduled-task.log")}
	if p.Streaming {
		args = append(args, "--serve")
	}
	return args
}
func (m *Manager) Apply(ctx context.Context, id string) (err error) {
	unlock, e := m.lock()
	if e != nil {
		return e
	}
	defer unlock()
	i, s, e := m.read()
	if e != nil {
		return e
	}
	t, e := m.readTransaction()
	if e != nil {
		return e
	}
	if t.ID != id || t.Phase != "prepared" || t.Previous != i.Current {
		return errors.New("invalid_transaction")
	}
	switchStarted := false
	defer func() {
		if err != nil && !switchStarted {
			_ = m.cancelPrepared(&t, &s, err)
		}
	}()
	c, e := Verify(t.Catalog, i.Keys, m.now())
	if e != nil {
		return e
	}
	a, e := c.Artifact(m.Target)
	if e != nil {
		return e
	}
	name, _ := TargetName(a.Target)
	expected := filepath.Join(m.Root, "releases", c.ReleaseID, a.Target, name)
	if t.Candidate.Binary != expected || t.Candidate.ID != c.ReleaseID || t.Candidate.Version != c.Version || t.Candidate.SHA256 != a.SHA256 {
		return errors.New("invalid_transaction")
	}
	if e = verifyFile(expected, a); e != nil {
		return e
	}
	data, e := m.fetch(ctx, i.Server, "/api/external-usage/reporter/updates/stable", MaxManifestBytes)
	if e != nil {
		return e
	}
	latest, e := acceptCatalog(data, &i, &s, m.now())
	if e != nil {
		return e
	}
	if latest.Paused || !latest.SameBuild(c) || (!t.Enrollment && !InRollout(i.ID, latest)) {
		return errors.New("release_withdrawn")
	}
	if e = m.saveState(s); e != nil {
		return e
	}
	if e = Save(filepath.Join(m.Root, "installation.json"), i); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	t.Phase = "switching"
	if e = m.saveTransaction(t); e != nil {
		return e
	}
	switchStarted = true
	defer func() {
		if err != nil {
			s.Quarantined = t.Candidate.ID
			s.Error = ErrorCode(err)
			s.Phase = "rolling_back"
			_ = m.saveState(s)
			recoveryCtx, stop := context.WithTimeout(context.Background(), 90*time.Second)
			defer stop()
			if e := m.rollback(recoveryCtx, &t, &i, &s); e != nil {
				s.Phase = "recovery_required"
				s.Error = "recovery_required"
				_ = m.saveState(s)
			}
		}
		_ = m.platform.Delete(applyTask)
	}()
	s.Phase = "switching"
	_ = m.saveState(s)
	for _, p := range t.Profiles {
		snapshot, e := m.platform.Snapshot(p.TaskName)
		if e != nil {
			return e
		}
		if !snapshot.Exists {
			continue
		}
		if e = m.platform.Disable(p.TaskName); e != nil {
			return errors.New("task_switch_failed")
		}
	}
	for _, p := range t.Profiles {
		stopCtx, stop := context.WithTimeout(ctx, 30*time.Second)
		e = m.stop(stopCtx, p)
		stop()
		if e != nil {
			return errors.New("stop_failed")
		}
	}
	for _, p := range t.Profiles {
		interval := 5
		if p.Streaming {
			interval = 0
		}
		task, e := m.platform.Definition(p.TaskName, t.Candidate.Binary, workerArguments(p, t.ID), interval, p.Streaming)
		if !p.Legacy {
			for _, previous := range t.Tasks {
				if previous.Name == p.TaskName && previous.Exists {
					task, e = m.platform.Retarget(previous, t.Candidate.Binary, workerArguments(p, t.ID))
					break
				}
			}
		}
		if e != nil {
			return e
		}
		if e = m.platform.Install(task); e != nil {
			return errors.New("task_switch_failed")
		}
		if e = m.platform.Start(p.TaskName); e != nil {
			return e
		}
	}
	t.Phase = "health_check"
	if e = m.saveTransaction(t); e != nil {
		return e
	}
	s.Phase = "health_check"
	_ = m.saveState(s)
	for _, p := range t.Profiles {
		if e = m.health(ctx, p, t.Candidate.ID, t.ID); e != nil {
			return errors.New("health_check_failed")
		}
	}
	u, e := m.platform.Definition(updaterTask, t.Candidate.Binary, []string{"--update-check", "--runtime", m.Root, "--log-file", filepath.Join(m.Root, "update.log")}, 15, false)
	for _, previous := range t.Tasks {
		if previous.Name == updaterTask && previous.Exists {
			u, e = m.platform.Retarget(previous, t.Candidate.Binary, []string{"--update-check", "--runtime", m.Root, "--log-file", filepath.Join(m.Root, "update.log")})
			break
		}
	}
	if e != nil {
		return e
	}
	if e = m.platform.Install(u); e != nil {
		return errors.New("task_switch_failed")
	}
	i.Current = t.Candidate
	for n := range i.Profiles {
		i.Profiles[n].Legacy = false
	}
	if e = Save(filepath.Join(m.Root, "installation.json"), i); e != nil {
		return e
	}
	s.Previous = t.Previous
	s.Version = t.Candidate.Version
	s.Phase = "committed"
	s.Error = ""
	s.Quarantined = ""
	if e = m.saveState(s); e != nil {
		return e
	}
	t.Phase = "committed"
	if e = m.saveTransaction(t); e != nil {
		return e
	}
	_ = m.platform.Delete(recoveryTask)
	m.cleanup(i, s)
	return nil
}

func (m *Manager) rollback(ctx context.Context, t *transaction, i *Installation, s *UpdateState) error {
	t.Phase = "rolling_back"
	if e := m.saveTransaction(*t); e != nil {
		return e
	}
	for _, p := range t.Profiles {
		if task, e := m.platform.Snapshot(p.TaskName); e != nil {
			return e
		} else if !task.Exists {
			continue
		}
		_ = m.platform.Disable(p.TaskName)
		if e := m.stop(ctx, p); e != nil {
			return e
		}
	}
	for _, task := range t.Tasks {
		if task.Exists {
			if e := m.platform.Install(task); e != nil {
				return e
			}
		} else {
			if e := m.platform.Delete(task.Name); e != nil {
				return e
			}
		}
	}
	i.Current = t.Previous
	i.Profiles = nil
	for _, p := range t.Profiles {
		existed := false
		for _, task := range t.Tasks {
			if task.Name == p.TaskName {
				existed = task.Exists
			}
		}
		if !existed {
			continue
		}
		i.Profiles = append(i.Profiles, p)
		if e := m.platform.Start(p.TaskName); e != nil {
			return e
		}
		if !p.Legacy {
			if e := m.health(ctx, p, t.Previous.ID, ""); e != nil {
				return e
			}
		}
	}
	if len(i.Profiles) > 0 {
		if e := Save(filepath.Join(m.Root, "installation.json"), i); e != nil {
			return e
		}
	} else {
		if e := os.Remove(filepath.Join(m.Root, "installation.json")); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	s.Version = t.Previous.Version
	s.Phase = "rolled_back"
	s.Quarantined = t.Candidate.ID
	if e := m.saveState(*s); e != nil {
		return e
	}
	t.Phase = "rolled_back"
	if e := m.saveTransaction(*t); e != nil {
		return e
	}
	_ = m.platform.Delete(recoveryTask)
	return nil
}

// Recover is idempotent and must run even when automatic checks are disabled.
func (m *Manager) Recover(ctx context.Context) error {
	unlock, e := m.lock()
	if e != nil {
		pending, pe := m.readTransaction()
		if pe != nil || m.now().Unix()-pending.CreatedAt < 300 || pending.Phase == "committed" || pending.Phase == "rolled_back" {
			return e
		}
		aborter, ok := m.platform.(interface{ AbortApply() error })
		if !ok {
			return e
		}
		if ae := aborter.AbortApply(); ae != nil {
			return e
		}
		unlock, e = m.lock()
		if e != nil {
			return e
		}
	}
	defer unlock()
	t, e := m.readTransaction()
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if t.Phase == "committed" || t.Phase == "rolled_back" {
		_ = m.platform.Delete(recoveryTask)
		_ = m.platform.Delete(applyTask)
		return nil
	}
	if m.now().Unix()-t.CreatedAt < 300 {
		return nil
	}
	i, s, e := m.read()
	if e != nil {
		return e
	}
	if t.Phase == "prepared" {
		t.Phase = "rolled_back"
		s.Phase = "idle"
		if e = m.saveTransaction(t); e != nil {
			return e
		}
		_ = m.platform.Delete(recoveryTask)
		_ = m.platform.Delete(applyTask)
		return m.saveState(s)
	}
	if e = m.rollback(ctx, &t, &i, &s); e != nil {
		s.Phase = "recovery_required"
		s.Error = "recovery_required"
		_ = m.saveState(s)
	}
	return e
}

func (m *Manager) cleanup(i Installation, s UpdateState) {
	entries, e := os.ReadDir(filepath.Join(m.Root, "releases"))
	if e != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !ValidID(entry.Name()) || entry.Name() == i.Current.ID || entry.Name() == s.Previous.ID {
			continue
		}
		_ = os.RemoveAll(filepath.Join(m.Root, "releases", entry.Name()))
	}
	entries, _ = os.ReadDir(filepath.Join(m.Root, "staging"))
	for _, entry := range entries {
		if entry.IsDir() && ValidID(entry.Name()) {
			_ = os.RemoveAll(filepath.Join(m.Root, "staging", entry.Name()))
		}
	}
}
func (m *Manager) SetAutomatic(enabled bool) error {
	unlock, e := m.lock()
	if e != nil {
		return e
	}
	defer unlock()
	i, _, e := m.read()
	if e != nil {
		return e
	}
	i.AutoUpdate = enabled
	return Save(filepath.Join(m.Root, "installation.json"), i)
}

func (m *Manager) platformStopLegacy(ctx context.Context, p Profile) error {
	if !p.Streaming {
		for {
			running, e := m.platform.Running(p.TaskName)
			if e != nil {
				return e
			}
			if !running {
				return nil
			}
			select {
			case <-ctx.Done():
				return errors.New("stop_failed")
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return stopLegacyTask(ctx, p.TaskName)
}

func validateBinaryPath(root, binary string) bool {
	rel, e := filepath.Rel(root, binary)
	return e == nil && !strings.HasPrefix(rel, "..") && filepath.IsAbs(binary)
}
