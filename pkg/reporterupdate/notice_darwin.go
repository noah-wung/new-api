package reporterupdate

import (
	"context"
	"os/exec"
)

func NotifyUpgrade(ctx context.Context, version, page string) error {
	// Pass notification text as argv, never interpolate a server response into code.
	return exec.CommandContext(ctx, "/usr/bin/osascript", "-e", `on run argv
display notification (item 1 of argv) with title "new-api 采集器更新"
end run`, "发现新版本 "+version+"。请打开 "+page+"，生成安装命令并手动升级。").Run()
}
