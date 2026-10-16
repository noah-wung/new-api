package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/pkg/reporterupdate"
	"github.com/QuantumNous/new-api/pkg/usagereporter"
)

func installClients(server, codexToken, claudeToken, keysPath, catalogPath string, automatic bool) error {
	if !strings.HasPrefix(server, "https://") {
		return errors.New("managed installation requires HTTPS for credential exchange")
	}
	if codexToken == "" && claudeToken == "" {
		return errors.New("select at least one client")
	}
	var keys reporterupdate.TrustKeys
	if e := reporterupdate.Load(keysPath, &keys); e != nil {
		return e
	}
	catalog, e := os.ReadFile(catalogPath)
	if e != nil {
		return e
	}
	c, e := reporterupdate.Verify(catalog, keys, time.Now())
	if e != nil {
		return e
	}
	if c.ReleaseID != reporterupdate.BuildReleaseID || c.Version != reporterupdate.Version || c.Commit != reporterupdate.Commit {
		return errors.New("installer_build_mismatch")
	}
	a, e := c.Artifact(runtime.GOOS + "-" + runtime.GOARCH)
	if e != nil {
		return e
	}
	self, e := os.Executable()
	if e != nil {
		return e
	}
	f, e := os.Open(self)
	if e != nil {
		return e
	}
	e = reporterupdate.VerifyBinary(f, a)
	f.Close()
	if e != nil {
		return e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	root := filepath.Join(home, ".new-api-usage-reporter")
	if e = reporterupdate.SecureDir(root); e != nil {
		return e
	}
	var profiles []reporterupdate.Profile
	if codexToken != "" {
		profiles = append(profiles, reporterupdate.Profile{Source: usagereporter.SourceCodex, Config: filepath.Join(root, "config.json"), TaskName: "pro.newapi.usage-reporter.codex"})
		if runtime.GOOS == "windows" {
			profiles[len(profiles)-1].TaskName = "new-api-usage-reporter-codex"
		}
	}
	if claudeToken != "" {
		profiles = append(profiles, reporterupdate.Profile{Source: usagereporter.SourceClaudeCode, Config: filepath.Join(root, "claude", "config.json"), TaskName: "pro.newapi.usage-reporter.claude", Streaming: true})
		if runtime.GOOS == "windows" {
			profiles[len(profiles)-1].TaskName = "new-api-claude-usage-reporter"
		}
	}
	for _, p := range profiles {
		cfg, e := usagereporter.LoadConfig(p.Config)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		origin, e := reporterupdate.Origin(cfg.Server)
		target, te := reporterupdate.Origin(server)
		if e != nil || te != nil || origin != target || cfg.Source != p.Source {
			return errors.New("existing installation identity must be resolved first")
		}
	}
	m, e := reporterupdate.NewManager(filepath.Join(root, "runtime"))
	if e != nil {
		return e
	}
	if e = m.Recover(context.Background()); e != nil {
		return e
	}
	if e = m.CanEnroll(server); e != nil {
		return e
	}
	for _, path := range []string{filepath.Join(root, "config.json"), filepath.Join(root, "claude", "config.json")} {
		cfg, err := usagereporter.LoadConfig(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		origin, err := reporterupdate.Origin(cfg.Server)
		wanted, we := reporterupdate.Origin(server)
		if err != nil || we != nil || origin != wanted {
			return errors.New("mixed gateway installations require manual migration")
		}
	}
	helper := ""
	settingsPath := ""
	if claudeToken != "" {
		helper, e = reporterupdate.EnsureLauncher(m.Root, self, a)
		if e != nil {
			return e
		}
		dir := os.Getenv("CLAUDE_CONFIG_DIR")
		if dir == "" {
			dir = filepath.Join(home, ".claude")
		}
		settingsPath = filepath.Join(dir, "settings.json")
	}
	for _, p := range profiles {
		if e = reporterupdate.SecureDir(filepath.Dir(p.Config)); e != nil {
			return e
		}
		if p.Source == usagereporter.SourceCodex {
			e = usagereporter.InstallCodexReporter(server, codexToken, p.Config)
		} else {
			e = usagereporter.InstallClaudeReporter(server, claudeToken, p.Config, settingsPath, usagereporter.ClaudeInstallOptions{HelperBinary: helper, RuntimeDir: m.Root, DeferTelemetry: true})
		}
		if e != nil {
			return e
		}
	}
	id, e := m.Enroll(context.Background(), server, keys, catalog, profiles, automatic)
	if e != nil {
		return e
	}
	if e = m.Apply(context.Background(), id); e != nil {
		return e
	}
	if claudeToken != "" {
		configPath := filepath.Join(root, "claude", "config.json")
		cfg, err := usagereporter.LoadConfig(configPath)
		if err != nil {
			return err
		}
		name := "usage-reporter"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := usagereporter.ConfigureClaudeTelemetry(*cfg, settingsPath, helper, usagereporter.ClaudeTelemetryOptions{RuntimeDir: m.Root, LegacyBinary: filepath.Join(filepath.Dir(configPath), name)}); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = m.SendStatus(ctx)
	fmt.Println("Selected client collectors installed; automatic updates:", automatic)
	if claudeToken != "" {
		fmt.Println("Restart newly configured Claude Code CLI and VS Code sessions")
	}
	return nil
}

func runUpdate(root, applyID string, check, recoverNow, force bool, automatic string) (result error) {
	defer func() {
		if result != nil {
			result = errors.New(reporterupdate.ErrorCode(result))
		}
	}()
	if root == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		root = filepath.Join(home, ".new-api-usage-reporter", "runtime")
	}
	m, e := reporterupdate.NewManager(root)
	if e != nil {
		return e
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = m.SendStatus(ctx)
	}()
	if automatic != "" {
		if automatic != "on" && automatic != "off" {
			return errors.New("auto-update must be on or off")
		}
		return m.SetAutomatic(automatic == "on")
	}
	if recoverNow {
		return m.Recover(context.Background())
	}
	if applyID != "" {
		return m.Apply(context.Background(), applyID)
	}
	if check {
		if e = m.Recover(context.Background()); e != nil {
			return e
		}
		id, e := m.CheckAndPrepare(context.Background(), force)
		if e != nil {
			return e
		}
		if id != "" {
			return m.LaunchApply(id)
		}
	}
	return nil
}
