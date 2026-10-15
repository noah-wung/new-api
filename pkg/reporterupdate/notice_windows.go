package reporterupdate

import (
	"context"
	"encoding/base64"
	"os/exec"
	"syscall"
)

func NotifyUpgrade(ctx context.Context, version, page string) error {
	message := base64.StdEncoding.EncodeToString([]byte("发现新版本 " + version + "。请打开 " + page + "，生成安装命令并手动升级。"))
	script := `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$Notice = New-Object System.Windows.Forms.NotifyIcon
try {
  $Notice.Icon = [System.Drawing.SystemIcons]::Information
  $Notice.Visible = $true
  $Notice.BalloonTipTitle = 'new-api collector update'
  $Notice.BalloonTipText = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + message + `'))
  $Notice.ShowBalloonTip(10000)
  Start-Sleep -Seconds 5
} finally { $Notice.Dispose() }`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Run()
}
