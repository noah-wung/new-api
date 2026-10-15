package usagereporter

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/QuantumNous/new-api/common"
)

// SelfCheck reads configuration and data without creating queues, changing
// checkpoints, contacting upstreams or binding the receiver port.
func SelfCheck(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	registration, ok := clientRegistrations[cfg.Source]
	if !ok {
		return errors.New("unsupported source")
	}
	if registration.selfCheck != nil {
		if err := registration.selfCheck(cfg); err != nil {
			return err
		}
	}
	namespace := sha256Hex(serverIdentity(cfg.Server) + "\x00" + cfg.Credential)
	queueRoot := filepath.Join(cfg.StatePath+".usage-queue", cfg.Source, namespace)
	if registration.legacyQueueSuffix != "" {
		queueRoot = filepath.Join(cfg.StatePath+registration.legacyQueueSuffix, namespace)
	}
	return filepath.WalkDir(queueRoot, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("queue symlink is not supported")
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".json" && entry.Name() != ".confirmed" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var record map[string]any
		return common.Unmarshal(data, &record)
	})
}

func checkCodexLocalData(cfg Config) error {
	state, err := LoadState(cfg.StatePath)
	if err != nil {
		return err
	}
	if state.SessionContextVersion > reporterStateSessionContextVersion {
		return errors.New("incompatible state format")
	}
	if info, err := os.Stat(cfg.CodexDir); err == nil && !info.IsDir() {
		return errors.New("invalid source directory")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func checkClaudeLocalConfig(cfg Config) error {
	if _, err := claudeListenAddress(cfg); err != nil {
		return err
	}
	if len(cfg.ClaudeReceiverToken) < 32 {
		return errors.New("invalid receiver token")
	}
	return nil
}
