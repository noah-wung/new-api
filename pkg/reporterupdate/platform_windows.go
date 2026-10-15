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
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

type nativePlatform struct{ root string }

var taskCommand = regexp.MustCompile(`(?s)<Command>.*?</Command>`)
var taskArguments = regexp.MustCompile(`(?s)<Arguments>.*?</Arguments>`)
var taskEncoding = regexp.MustCompile(`(?i)encoding\s*=\s*["']utf-16["']`)

func (p *nativePlatform) Retarget(t Task, binary string, args []string) (Task, error) {
	if !taskCommand.MatchString(t.Definition) || !taskArguments.MatchString(t.Definition) {
		return t, errors.New("invalid_task")
	}
	var quoted []string
	for _, arg := range args {
		quoted = append(quoted, quoteWindowsArg(arg))
	}
	t.Definition = taskCommand.ReplaceAllStringFunc(t.Definition, func(string) string { return "<Command>" + xmlText(binary) + "</Command>" })
	t.Definition = taskArguments.ReplaceAllStringFunc(t.Definition, func(string) string { return "<Arguments>" + xmlText(strings.Join(quoted, " ")) + "</Arguments>" })
	return t, nil
}

func newPlatform(root string) (taskPlatform, error) { return &nativePlatform{root: root}, nil }
func hideProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
func schtasks(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "schtasks.exe", args...)
	hideProcess(cmd)
	return cmd.CombinedOutput()
}
func (p *nativePlatform) Snapshot(name string) (Task, error) {
	t, _, e := readScheduledTask(name)
	return t, e
}
func (p *nativePlatform) Disable(name string) error {
	ctx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_, e := schtasks(ctx, "/Change", "/TN", name, "/DISABLE")
	return e
}
func (p *nativePlatform) Install(t Task) error {
	if !validTaskName(t.Name) {
		return errors.New("invalid_task")
	}
	f, e := os.CreateTemp(p.root, "task-*.xml")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.WriteString(t.Definition); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	ctx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_, e = schtasks(ctx, "/Create", "/TN", t.Name, "/XML", f.Name(), "/F")
	return e
}
func (p *nativePlatform) Start(name string) error {
	ctx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_, e := schtasks(ctx, "/Run", "/TN", name)
	return e
}
func (p *nativePlatform) Delete(name string) error {
	if t, _, e := readScheduledTask(name); e != nil {
		return e
	} else if !t.Exists {
		return nil
	}
	ctx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_, e := schtasks(ctx, "/Delete", "/TN", name, "/F")
	return e
}
func (p *nativePlatform) Running(name string) (bool, error) {
	_, running, e := readScheduledTask(name)
	return running, e
}
func readScheduledTask(name string) (Task, bool, error) {
	t := Task{Name: name}
	if !validTaskName(name) {
		return t, false, errors.New("invalid_task")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if e := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); e != nil {
		if oe, ok := e.(*ole.OleError); !ok || oe.Code() != 1 {
			return t, false, e
		}
	}
	defer ole.CoUninitialize()
	unknown, e := oleutil.CreateObject("Schedule.Service")
	if e != nil {
		return t, false, e
	}
	defer unknown.Release()
	scheduler, e := unknown.QueryInterface(ole.IID_IDispatch)
	if e != nil {
		return t, false, e
	}
	defer scheduler.Release()
	connected, e := oleutil.CallMethod(scheduler, "Connect")
	if e != nil {
		return t, false, e
	}
	connected.Clear()
	folder, e := oleutil.CallMethod(scheduler, "GetFolder", `\`)
	if e != nil {
		return t, false, e
	}
	defer folder.Clear()
	value, e := oleutil.CallMethod(folder.ToIDispatch(), "GetTask", name)
	if e != nil {
		if oe, ok := e.(*ole.OleError); ok && oe.Code() == 0x80070002 {
			return t, false, nil
		}
		return t, false, e
	}
	defer value.Clear()
	task := value.ToIDispatch()
	definition, e := oleutil.GetProperty(task, "Xml")
	if e != nil {
		return t, false, e
	}
	defer definition.Clear()
	state, e := oleutil.GetProperty(task, "State")
	if e != nil {
		return t, false, e
	}
	defer state.Clear()
	t.Exists = true
	t.Definition = taskEncoding.ReplaceAllString(definition.ToString(), `encoding="UTF-8"`)
	return t, state.Val == 4, nil
}
func xmlText(text string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(text))
	return b.String()
}
func quoteWindowsArg(s string) string { return syscall.EscapeArg(s) }
func (p *nativePlatform) Definition(name, binary string, args []string, minutes int, keepAlive bool) (Task, error) {
	if !validTaskName(name) || !filepath.IsAbs(binary) {
		return Task{}, errors.New("invalid_task")
	}
	token, e := windows.OpenCurrentProcessToken()
	if e != nil {
		return Task{}, e
	}
	defer token.Close()
	user, e := token.GetTokenUser()
	if e != nil {
		return Task{}, e
	}
	sid := user.User.Sid.String()
	var triggers strings.Builder
	if keepAlive || name == updaterTask || name == recoveryTask {
		fmt.Fprintf(&triggers, "<LogonTrigger><Enabled>true</Enabled><UserId>%s</UserId></LogonTrigger>", sid)
	}
	if minutes > 0 {
		delay := minutes
		if name == recoveryTask {
			delay = 5
		}
		fmt.Fprintf(&triggers, `<TimeTrigger><Repetition><Interval>PT%dM</Interval><StopAtDurationEnd>false</StopAtDurationEnd></Repetition><StartBoundary>%s</StartBoundary><Enabled>true</Enabled></TimeTrigger>`, minutes, time.Now().Add(time.Duration(delay)*time.Minute).Format("2006-01-02T15:04:05"))
	}
	var quoted []string
	for _, arg := range args {
		quoted = append(quoted, quoteWindowsArg(arg))
	}
	limit := "PT0S"
	if name == applyTask {
		limit = "PT3M"
	}
	definition := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Triggers>%s</Triggers><Principals><Principal id="Author"><UserId>%s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><Enabled>true</Enabled><Hidden>true</Hidden><ExecutionTimeLimit>%s</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure></Settings><Actions Context="Author"><Exec><Command>%s</Command><Arguments>%s</Arguments></Exec></Actions></Task>`, triggers.String(), sid, limit, xmlText(binary), xmlText(strings.Join(quoted, " ")))
	return Task{Name: name, Definition: definition, Exists: true}, nil
}
func (p *nativePlatform) Detach(binary, root, id string) error {
	task, e := p.Definition(applyTask, binary, []string{"--update-apply", id, "--runtime", root, "--log-file", filepath.Join(root, "update.log")}, 0, false)
	if e != nil {
		return e
	}
	if e = p.Install(task); e != nil {
		return e
	}
	return p.Start(applyTask)
}
func stopLegacyTask(ctx context.Context, name string) error {
	_, e := schtasks(ctx, "/End", "/TN", name)
	return e
}

func (p *nativePlatform) AbortApply() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, e := schtasks(ctx, "/End", "/TN", applyTask)
	return e
}
