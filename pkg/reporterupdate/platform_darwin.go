package reporterupdate

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type nativePlatform struct{ root, agents, domain string }

var plistArguments = regexp.MustCompile(`(?s)<key>ProgramArguments</key>\s*<array>.*?</array>`)

func (p *nativePlatform) Retarget(t Task, binary string, args []string) (Task, error) {
	if !plistArguments.MatchString(t.Definition) {
		return t, errors.New("invalid_task")
	}
	var b strings.Builder
	b.WriteString("<key>ProgramArguments</key><array>")
	for _, arg := range append([]string{binary}, args...) {
		fmt.Fprintf(&b, "<string>%s</string>", xmlText(arg))
	}
	b.WriteString("</array>")
	t.Definition = plistArguments.ReplaceAllStringFunc(t.Definition, func(string) string { return b.String() })
	return t, nil
}

func newPlatform(root string) (taskPlatform, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	return &nativePlatform{root: root, agents: filepath.Join(home, "Library", "LaunchAgents"), domain: "gui/" + strconv.Itoa(os.Getuid())}, nil
}
func hideProcess(*exec.Cmd)                           {}
func (p *nativePlatform) taskPath(name string) string { return filepath.Join(p.agents, name+".plist") }
func launchctl(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
}
func (p *nativePlatform) Snapshot(name string) (Task, error) {
	t := Task{Name: name}
	if !validTaskName(name) {
		return t, errors.New("invalid_task")
	}
	data, e := os.ReadFile(p.taskPath(name))
	if os.IsNotExist(e) {
		return t, nil
	}
	if e != nil {
		return t, e
	}
	t.Definition = string(data)
	t.Exists = true
	return t, nil
}
func (p *nativePlatform) Disable(name string) error {
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	_, e := launchctl(ctx, "disable", p.domain+"/"+name)
	return e
}
func (p *nativePlatform) Install(t Task) error {
	if !validTaskName(t.Name) {
		return errors.New("invalid_task")
	}
	if e := os.MkdirAll(p.agents, 0o700); e != nil {
		return e
	}
	ctx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_, _ = launchctl(ctx, "bootout", p.domain+"/"+t.Name)
	if e := WritePrivate(p.taskPath(t.Name), []byte(t.Definition)); e != nil {
		return e
	}
	if _, e := launchctl(ctx, "enable", p.domain+"/"+t.Name); e != nil {
		return e
	}
	_, e := launchctl(ctx, "bootstrap", p.domain, p.taskPath(t.Name))
	return e
}
func (p *nativePlatform) Start(name string) error {
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	_, e := launchctl(ctx, "kickstart", p.domain+"/"+name)
	return e
}
func (p *nativePlatform) Delete(name string) error {
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	_, _ = launchctl(ctx, "bootout", p.domain+"/"+name)
	e := os.Remove(p.taskPath(name))
	if os.IsNotExist(e) {
		return nil
	}
	return e
}
func (p *nativePlatform) Running(name string) (bool, error) {
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	data, e := launchctl(ctx, "print", p.domain+"/"+name)
	if e != nil {
		if _, se := os.Stat(p.taskPath(name)); os.IsNotExist(se) {
			return false, nil
		}
		if bytes.Contains(data, []byte("Could not find service")) {
			return false, nil
		}
		return false, e
	}
	return bytes.Contains(data, []byte("state = running")), nil
}
func xmlText(text string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(text))
	return b.String()
}
func (p *nativePlatform) Definition(name, binary string, args []string, minutes int, keepAlive bool) (Task, error) {
	if !validTaskName(name) || !filepath.IsAbs(binary) {
		return Task{}, errors.New("invalid_task")
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string>`, xmlText(name), xmlText(binary))
	for _, arg := range args {
		fmt.Fprintf(&b, "<string>%s</string>", xmlText(arg))
	}
	b.WriteString("</array>")
	if keepAlive {
		b.WriteString("<key>KeepAlive</key><true/>")
	}
	if minutes > 0 {
		fmt.Fprintf(&b, "<key>StartInterval</key><integer>%d</integer>", minutes*60)
	}
	// Collectors are started explicitly. Updater/recovery also receive login wakeups.
	if name == updaterTask || name == recoveryTask {
		b.WriteString("<key>RunAtLoad</key><true/>")
	}
	if name == applyTask {
		b.WriteString("<key>RunAtLoad</key><true/><key>ExitTimeOut</key><integer>180</integer>")
	}
	b.WriteString("</dict></plist>")
	return Task{Name: name, Definition: b.String(), Exists: true}, nil
}
func (p *nativePlatform) Detach(binary, root, id string) error {
	task, e := p.Definition(applyTask, binary, []string{"--update-apply", id, "--runtime", root, "--log-file", filepath.Join(root, "update.log")}, 0, false)
	if e != nil {
		return e
	}
	return p.Install(task)
}

func (p *nativePlatform) AbortApply() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, e := launchctl(ctx, "kill", "SIGKILL", p.domain+"/"+applyTask)
	return e
}
func stopLegacyTask(ctx context.Context, name string) error {
	domain := "gui/" + strconv.Itoa(os.Getuid()) + "/" + name
	if _, e := launchctl(ctx, "kill", "SIGTERM", domain); e != nil {
		return e
	}
	for {
		data, e := launchctl(ctx, "print", domain)
		if e != nil || !bytes.Contains(data, []byte("state = running")) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
