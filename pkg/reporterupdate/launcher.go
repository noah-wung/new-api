package reporterupdate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// stageVerifiedBinary never overwrites an existing immutable executable.
func stageVerifiedBinary(source, dest string, artifact Artifact) error {
	if err := SecureDir(filepath.Dir(dest)); err != nil {
		return err
	}
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		src, err := os.Open(source)
		if err != nil {
			return err
		}
		defer src.Close()
		file, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, src)
		err = errors.Join(copyErr, file.Sync(), file.Close())
		if err != nil {
			_ = os.Remove(dest)
			return err
		}
	} else if err != nil {
		return err
	}
	return verifyFile(dest, artifact)
}

// EnsureLauncher installs a stable, verified bootstrap executable. Telemetry
// refers to this path; it dispatches to the current immutable release, so both
// committed updates and rollbacks work without changing Claude settings.
func EnsureLauncher(root, source string, artifact Artifact) (string, error) {
	name, err := TargetName(runtime.GOOS + "-" + runtime.GOARCH)
	if err != nil {
		return "", err
	}
	if artifact.Target != runtime.GOOS+"-"+runtime.GOARCH {
		return "", errors.New("invalid_launcher_target")
	}
	dest := filepath.Join(root, "launcher", name)
	var original Artifact
	metadata := filepath.Join(root, "launcher", "artifact.json")
	if err := Load(metadata, &original); err == nil {
		if original.Target != artifact.Target {
			return "", errors.New("invalid_launcher_target")
		}
		return dest, verifyFile(dest, original)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := verifyFile(source, artifact); err != nil {
		return "", err
	}
	if err := stageVerifiedBinary(source, dest, artifact); err != nil {
		return "", err
	}
	if err := Save(metadata, artifact); err != nil {
		return "", err
	}
	return dest, nil
}

// CurrentBinary reads an atomically committed installation. It deliberately
// does not take the update lock: telemetry must keep working while an update
// is being prepared or tested. Current and previous releases stay on disk.
func CurrentBinary(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	m := &Manager{Root: abs}
	i, _, err := m.read()
	if err != nil {
		return "", err
	}
	target := runtime.GOOS + "-" + runtime.GOARCH
	name, err := TargetName(target)
	if err != nil {
		return "", err
	}
	expected := filepath.Join(abs, "releases", i.Current.ID, target, name)
	if i.Current.Binary != expected {
		return "", errors.New("invalid_current_binary")
	}
	info, err := os.Stat(expected)
	if err != nil {
		return "", err
	}
	if err := verifyFile(expected, Artifact{Target: target, SHA256: i.Current.SHA256, Size: info.Size()}); err != nil {
		return "", err
	}
	return expected, nil
}

// ManagedProfile prevents a legacy installer from replacing a task still owned
// by the shared updater, including while release distribution is paused.
func ManagedProfile(root, config string) (bool, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	i, _, err := (&Manager{Root: abs}).read()
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	path, err := filepath.Abs(config)
	if err != nil {
		return false, err
	}
	for _, profile := range i.Profiles {
		if filepath.Clean(profile.Config) == path {
			return true, nil
		}
	}
	return false, nil
}
