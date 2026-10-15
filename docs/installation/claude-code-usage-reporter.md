# Claude Code 客户端自动用量上报

`usage-reporter` 支持接收官方 Claude Code CLI 和官方 VS Code 扩展的 OpenTelemetry 请求事件，使用 `claude_code` 来源写入 new-api 外部用量统计。它与 Codex 共用二进制和上报 API，使用独立配置、进程及本机接收凭据。外部上报不扣 Quota，也不写入网关消费日志。

本机持续接收事件，每 5 分钟向网关批量上报，与 Codex 采集器保持一致。只转换 `claude_code.api_request`，不扫描历史 transcript、不读取 Claude 登录凭据，也不将 OTel metrics 或父会话累计值再次计入。子 agent 发出的独立 API 请求按相同规则采集。CLI 和扩展必须支持官方文档列出的请求事件与 `http/json` exporter。

## 服务端准备

部署包含本功能的 new-api 版本，开启外部用量功能，并在 `external_usage_setting.allowed_sources` 中加入 `claude_code`，例如 `["codex", "claude_code"]`。现有站点的配置和设备权限不会被代码自动扩大；仅运行旧版本服务端无法接收新来源。

在外部用量页面创建或重新签发允许 `claude_code` 的设备上报凭据。也可通过已登录用户的 `POST /api/external-usage/self/devices` 创建，参数示例：

```json
{
  "device_name": "my-claude-device",
  "device_fingerprint": "a-stable-random-device-identifier",
  "allowed_sources": ["claude_code"]
}
```

设备上限仍受站点配置约束。Codex 专用凭据不会自动获得 Claude 权限；如重新签发同一设备凭据，需要同步更新使用它的客户端。

## 安装与配置

### 通过个人后台安装

登录后进入“外部用量”，使用统一的“客户端用量采集器”入口；可同时勾选 Codex 和 Claude Code，一条命令安装全部所选采集器。完整流程见 [统一安装说明](client-usage-collectors.md)。

1. 选择 macOS 或 Windows，并勾选 Claude Code。
2. 填写 Claude Code 实际使用的 API 地址；若是本网关的地址或别名，勾选“此地址的用量已由本网关计量”。地址不能包含 API 密钥、认证信息、查询参数或片段。
3. 点击“安装所选采集器”，复制生成的限时命令，在运行 Claude Code 的设备终端执行。
4. 安装脚本下载工具、兑换绑定当前登录用户的单次令牌、核对本机路由、合并 telemetry 配置，并注册持续运行的用户后台服务。完成后重启 Claude Code CLI 和 VS Code 会话。

来源未由管理员启用时，该客户端选项会禁用；其他已启用的客户端仍可安装。管理员查看其他用户时不能代该用户生成命令；需以目标用户登录。路由选择保存在服务器令牌记录中，不能通过篡改下载 URL 改变，且令牌不能兑换为 Codex 凭据。

自动安装路径为 `~/.new-api-usage-reporter/claude/`，包含独立二进制、`config.json` 和状态文件；不会覆盖 Codex 的配置或后台任务。macOS 注册 `pro.newapi.usage-reporter.claude` LaunchAgent，Windows 注册 `new-api-claude-usage-reporter` 当前用户登录任务。相同账户、相同设备的有效旧上报凭据会复用，保留本机 token、过滤策略与待上报队列；来源权限不一致、设备被撤销或切换账户时重新签发，并隔离旧队列。

安装前请确认用户配置、启动环境和扩展专用配置的路由一致。发现本机地址与后台声明不符时，安装会停止，不替换已有后台服务；修正地址后重新生成安装命令即可。脚本会备份首次修改前的 Claude 用户设置。

以下手动步骤适用于 Linux、自定义后台管理或排障；通过后台命令安装的用户无需手动创建凭据和配置文件。

### 手动安装

可以在客户端从源码构建：

```sh
go build -o usage-reporter ./cmd/usage-reporter
```

Windows 后台版本必须使用 GUI 子系统构建，避免系统创建控制台窗口：

```powershell
go build -ldflags "-s -w -H=windowsgui" -o usage-reporter.exe ./cmd/usage-reporter
```

Windows 手动运行时建议同时传入 `--log-file` 的绝对路径，以便没有控制台时仍能查看结果和错误。

已部署的服务还提供以下二进制下载；查询参数让只启用 Claude 的站点也能下载：

```text
/api/external-usage/reporter/binary/darwin-arm64/usage-reporter?source=claude_code
/api/external-usage/reporter/binary/darwin-amd64/usage-reporter?source=claude_code
/api/external-usage/reporter/binary/windows-amd64/usage-reporter.exe?source=claude_code
```

Linux 可从源码构建。将对应二进制放到当前用户的 `~/.new-api-usage-reporter/` 下。在同一目录保存 `claude-config.json`，将示例服务器和凭据替换为实际值：

```json
{
  "server": "https://token.example",
  "source": "claude_code",
  "credential": "eur_REPLACE_WITH_DEVICE_REPORTING_CREDENTIAL",
  "state_path": "./claude-state.json",
  "accept_window_days": 90,
  "claude_listen": "127.0.0.1:4318",
  "gateway_urls": ["http://172.16.0.37:3000"],
  "claude_external_urls": ["https://api.anthropic.com"]
}
```

`gateway_urls` 应列出实际使用的同一计量网关别名，示例 IP/端口需要按部署核对。`server` 自身始终属于网关，网关规则优先于外部允许列表。URL 比较以协议、主机名及端口为准，忽略 API 路径；不同端口和相似域名不匹配。地址不得包含认证信息、query 或 fragment。

省略 `claude_external_urls` 时只允许官方 API；显式 `[]` 表示不允许任何外部路由。其他外部服务需明确列入允许列表。`accept_window_days` 应与站点一致；省略或设为 0 时，CLI 启动会尝试从服务端状态接口补齐。

对直连官方 API 的用户执行一次：

```sh
~/.new-api-usage-reporter/usage-reporter \
  --config ~/.new-api-usage-reporter/claude-config.json \
  --configure-claude --claude-upstream https://api.anthropic.com
```

Windows 使用相同参数调用 `usage-reporter.exe`。此命令生成独立的随机 `claude_receiver_token` 并保存到 reporter 配置，合并 Claude 用户设置中的日志 exporter，保留权限、hooks 和其他设置；首次修改前备份为 `settings.json.before-new-api-telemetry`。文件在 Unix 上按 `0600` 写入。new-api 设备上报凭据不会写入 Claude 设置。

默认设置文件为 `CLAUDE_CONFIG_DIR/settings.json`，未指定该环境变量则为 `~/.claude/settings.json`；也可显式传入 `--claude-settings /absolute/path/settings.json`。这必须是 Claude 实际使用的用户配置，不能放到仓库设置里：Claude Code 会忽略仓库级 exporter 目的地。完成后重启 CLI 会话和 VS Code 中的 Claude 会话。

程序使用日志专用的 `OTEL_EXPORTER_OTLP_LOGS_*` 变量，协议固定为无压缩 `http/json`，并关闭 prompt、response、tool 和原始 API body 内容导出。其他已有指标/追踪 exporter 设置保留，但不会被本程序转换为用量。

## 网关过滤与路由约束

**Claude 官方事件没有可靠的实际上游 URL 字段。** 本实现要求部署方通过自定义 OTel 属性 `new_api.upstream_origin` 声明有效路由；配置命令将 `--claude-upstream` 的标准化 origin 写入 `OTEL_RESOURCE_ATTRIBUTES`。接收器没有此属性、路由未获批准、计数无效或时间超窗时，会以 OTLP partial success 拒收相关记录，不上传原始内容。

配置命令会核对 Claude 用户设置中的 `ANTHROPIC_BASE_URL`（未设置时按官方地址）及当前启动环境，发现与声明不符便拒绝修改。它不会改变 API 路由。若使用 TOKEN 网关，应先在实际 Claude 配置中设置该网关，然后以它的 URL 运行配置命令；这类事件收到后直接排除，不进入上报队列。

声明是部署约束，不能证明每次请求的实际路由。项目配置、managed settings、启动环境、扩展专用设置和代理转发仍可能覆盖路由。**同一 telemetry 配置只能用于路由固定且已经核验的执行环境；切换直连/网关时，须同步更新声明并重启会话。** 不要给混合或不确定的路由统一打上官方 API 标签。需要混用时，应使用独立的 Claude 配置目录/执行环境，分别配好真实路由及 telemetry，或由管理员提供一致的 managed settings。Bedrock、Vertex、Foundry 的自动配置会被拒绝，需单独核验并管理其路由与 telemetry 属性。

远程 SSH、WSL、容器中的 CLI/扩展，接收器必须运行在 Claude 进程实际所在环境；`127.0.0.1` 不能跨主机使用。无法读取当前环境的 exporter/路由设置时，不要启用该环境的采集。

## 运行与自动启动

前台启动：

```sh
~/.new-api-usage-reporter/usage-reporter \
  --config ~/.new-api-usage-reporter/claude-config.json --serve
```

接收器只允许数值形式的 loopback IP；使用独立的本机 Bearer token，拒绝带浏览器 Origin 的请求。需要改端口时更新 `claude_listen` 并重新运行配置命令，然后重启双方进程。

Linux 可保存以下用户 unit 到 `~/.config/systemd/user/new-api-claude-usage.service`：

```ini
[Unit]
Description=new-api Claude Code usage reporter

[Service]
ExecStart="%h/.new-api-usage-reporter/usage-reporter" --config "%h/.new-api-usage-reporter/claude-config.json" --serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now new-api-claude-usage.service
```

macOS 可保存以下 LaunchAgent 到 `~/Library/LaunchAgents/pro.newapi.usage-reporter.claude.plist`，将 `/Users/USER` 替换为当前用户的绝对主目录：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>pro.newapi.usage-reporter.claude</string>
  <key>ProgramArguments</key><array>
    <string>/Users/USER/.new-api-usage-reporter/usage-reporter</string>
    <string>--config</string>
    <string>/Users/USER/.new-api-usage-reporter/claude-config.json</string>
    <string>--serve</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict></plist>
```

```sh
launchctl bootstrap "gui/$(id -u)" ~/Library/LaunchAgents/pro.newapi.usage-reporter.claude.plist
```

Windows 可用当前用户的 PowerShell 注册登录任务。使用绝对配置路径，二进制与配置均不应放在临时目录：

```powershell
$Root = Join-Path $env:USERPROFILE '.new-api-usage-reporter'
$Binary = Join-Path $Root 'usage-reporter.exe'
$Config = Join-Path $Root 'claude-config.json'
$Log = Join-Path $Root 'claude-scheduled-task.log'
$User = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
$Action = New-ScheduledTaskAction -Execute $Binary -Argument ('--config "{0}" --serve --log-file "{1}"' -f $Config, $Log)
$Trigger = New-ScheduledTaskTrigger -AtLogOn -User $User
$Principal = New-ScheduledTaskPrincipal -UserId $User -LogonType Interactive -RunLevel Limited
$Settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName 'new-api-claude-usage-reporter' -Action $Action -Trigger $Trigger -Principal $Principal -Settings $Settings -Force
Start-ScheduledTask -TaskName 'new-api-claude-usage-reporter'
```

两种采集器均在启动时尝试上报已有数据，随后每 5 分钟上报；没有新增/待上报事件时不发送用量请求。Codex 由系统定时启动扫描，Claude exporter 需要本机接收器持续运行，接收的事件先写入本地队列，再按相同周期上传。

Windows 自动安装任务直接运行 GUI 子系统二进制，不启动 PowerShell 包装进程，也不在每次上报时显示控制台窗口；运行信息和错误追加到各自安装目录的 `scheduled-task.log`，Claude 安装配置结果保存在其 `install.log`。首次主动执行安装命令的终端仍显示安装结果。旧版本需重新执行个人后台生成的安装命令，才能替换二进制和任务配置。

停止时，Linux 禁用该 user unit；macOS 使用 `launchctl bootout`；Windows 停止并注销对应任务。然后移除本功能添加的 Claude `env` 项；备份仅保存首次配置前的状态，恢复时需保留之后新增的其他设置。

## 计数、重试和本地数据

- 输入总量为 `input_tokens + cache_read_tokens + cache_creation_tokens`；缓存命中仅为 `cache_read_tokens`；总量为输入总量加输出。缓存创建已计入输入，不伪装成缓存命中；当前协议不另存缓存创建明细或 TTL 细分。没有独立 reasoning 字段时记为 0。
- 有 `request_id` 时按来源、上游 origin 和请求 ID 生成稳定事件 ID；没有时优先使用 `client_request_id`。两者均无时，以 session、原始纳秒时间、sequence、可用进程实例、模型和计数的摘要退化去重。退化模式不能保证合并被其他系统改写时间或字段的副本，不能只靠 session + sequence 去重。
- 服务端对 Claude 仅按事件 ID 去重，保留同一秒内数值相同的独立请求；跨入口或子 agent 不再使用父会话累计量相加。
- 接收端在确认前将白名单记录写入磁盘，文件名及 session 使用摘要；不保存 prompts、responses、邮件、账户 ID、项目路径、任意属性或原始请求 ID。标准化上游 origin 仅在本地保存，以便上报前再次执行网关排除，不传给 new-api。
- 每 5 分钟处理本周期开始时的待上报事件，每个 HTTP 批次最多 100 条；积压超过 100 条时在同一周期继续分批处理。处理中新增的事件和未被 ACK 的事件留到下一周期；网络失败立即结束本周期，下一周期重试。失败、ACK 丢失或 ACK 未列出的事件保留原 ID；明确 accepted/duplicate 后才删除，明确 rejected 后保存为 `.json.rejected` 待检查。服务端拒绝原因不会原样落盘。
- 队列在 `<state_path>.claude-queue/<server-and-credential-digest>/`，最多 10,000 个文件，达到上限返回可重试错误。永久拒绝文件也占上限，处理后需自行清理。服务端/凭据更换会隔离旧队列，避免把另一身份的用量上报给新账户；轮换前应先排空旧队列，保留旧配置用于排障。
- 持久化从本机成功接收开始。接收器未运行、Claude exporter 自身缓冲耗尽或进程在导出前退出的用量可能丢失；本实现不承诺通过历史 JSONL 自动补齐。

## 验证范围

自动回归覆盖：CLI/VS Code 入口及子 agent 风格事件、四类 token 的归一化、负数/溢出/异常时间、网关别名与相似域名、配置保留、无请求 ID 的重试、持久队列重启、丢 ACK、部分 ACK、永久拒绝、设备与站点来源权限、真实 report API 与数据库汇总，以及不扣 Quota/不写网关日志。安装回归覆盖个人命令生成、跨来源令牌拒绝、单次兑换、过期、设备上限回滚、跨账户隔离，以及真实二进制经下载脚本在隔离 HOME 中安装、重装和注册服务描述文件（系统服务命令使用桩，不操作开发机真实服务）。

已通过 `go test ./... -count=1`、采集器与 CLI 的 `-race` 检查、相关包的 `go vet`；采集器已构建验证 darwin/arm64、darwin/amd64、windows/amd64、linux/amd64、linux/arm64，主服务已构建验证 Linux/amd64。

另已使用本机 Claude Code CLI **2.1.236**、隔离 HOME/配置和本地模拟 Anthropic API，验证真实 CLI 的 OTLP 导出经本机接收器进入 report API：1 个真实 CLI 请求形成 1 个用量事件，输入 `100 + 200 + 300 = 600`、缓存读 `200`、输出 `40`、总量 `640`。没有调用真实模型服务，也没有读取现有 Claude 登录信息。

其余自动回归使用脱敏合成 OTLP 事件。目标 VS Code 扩展仍需在实际运行环境验收，核对 `api_request` 四类 token、子 agent 和网关/直连两路；Windows 二进制已按 GUI 子系统构建，但登录任务、周期执行和实际无闪窗体验仍需在 Windows 实机验证。开发过程不修改现有 Claude 用户设置、不使用真实模型凭据、不向生产服务上报测试用量。

官方契约参考：[Monitoring usage](https://code.claude.com/docs/en/monitoring-usage)、[VS Code configuration](https://code.claude.com/docs/en/vs-code#configure-settings)。
