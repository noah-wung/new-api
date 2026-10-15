# 客户端采集器自动更新

Codex 和 Claude Code 共用 `pkg/reporterupdate`。个人后台安装默认开启自动检查，用户可以关闭；关闭后仍安装统一版本管理和恢复能力，用量采集继续每 5 分钟上报。更新只作用于本机已接管的来源，不会安装新客户端或扩大设备权限。

## 发布准备

自动更新使用独立采集器版本和 Ed25519 签名。普通网关 Docker 构建内的旧式下载资产仍供兼容入口使用；**它们不会自动成为可分发的签名发布包**。签名私钥不得进入源码、网关镜像、客户端或数据库。

在受控发布环境中生成密钥，私钥路径放在项目目录之外：

```sh
go run ./cmd/reporter-release --mode generate-key \
  --key-id release-1 --key-file /secure/reporter-release.key
```

命令以 `0600` 创建私钥，输出可公开的 base64 公钥。发布时从项目根目录运行：

```sh
go run ./cmd/reporter-release --mode build \
  --key-file /secure/reporter-release.key --key-id release-1 \
  --out /srv/reporter-releases --release reporter-1.0.0 \
  --version 1.0.0 --sequence 1 --rollout 10 --seed stable-1
```

工具构建 darwin-arm64、darwin-amd64 和 windows-amd64，注入采集器版本、Git commit、release ID；Windows 使用 GUI 子系统。

仓库同时提供手动触发的 `Signed collector release` 工作流。在 GitHub 的 `collector-release` environment 配置 `REPORTER_RELEASE_SIGNING_KEY`（上述私钥文件的 base64 文本）后，可输入版本、发布 ID、序号和灰度参数生成签名资产 artifact。工作流不会自动部署或激活发布，artifact 不含私钥。

发布目录格式：

```text
/srv/reporter-releases/reporter-1.0.0/
  catalog.json
  darwin-arm64/usage-reporter
  darwin-amd64/usage-reporter
  windows-amd64/usage-reporter.exe
```

将完整资产目录只读挂载给所有网关副本，配置：

```text
NEW_API_USAGE_REPORTER_RELEASE_DIR=/srv/reporter-releases
NEW_API_USAGE_REPORTER_TRUST_KEYS={"release-1":"<base64-public-key>"}
```

部署更新功能版本的网关后，管理员在“外部用量 → 采集器发布管理”输入 `reporter-1.0.0` 并激活。激活会验证签名、有效期、三个平台资产、摘要及版本不可变性。缺少资产、校验失败或清单序号倒退均不允许激活。发布操作写入管理员系统日志。

发布目录中的二进制不可覆盖。同一版本和 release ID 只能调整部署清单；代码变化必须生成更高版本和新 release ID。服务端将当前签名清单及激活指针存入数据库，多副本必须同时能读取相同的只读资产目录。

## 灰度、暂停与密钥轮换

同一发布扩大灰度时，只重新签署清单，不重新编译二进制：

```sh
go run ./cmd/reporter-release --mode sign \
  --key-file /secure/reporter-release.key --key-id release-1 \
  --out /srv/reporter-releases --release reporter-1.0.0 \
  --version 1.0.0 --sequence 2 --rollout 50 --seed stable-1
```

重新激活同一个发布 ID，网关验证序号增加且二进制身份未变。后续可使用更高序号推进到 100%。保持 seed 不变，设备基于本机随机安装 ID 稳定分组，无需上传硬件信息。按钮“暂停采集器更新”阻止后续获取部署清单；已经开始切换的事务不保证瞬间撤回。已提交版本的问题应通过更高版本修复。

清单默认有效 30 天。到期前须使用 `--mode sign` 和更高序号续签、重新激活；客户端不会安装过期清单。支持 `--next-keys /secure/next-public-keys.json` 在旧密钥签名的清单中授权新公钥。须保留旧、新密钥的重叠发布期，让在线客户端先收到授权；不能让客户端从未受信的新清单直接接受新公钥。

## 安装、迁移与身份

网关须通过可信 HTTPS 提供新版统一安装命令。安装程序会对自身二进制、清单、公钥信任和现有配置做预检，再兑换所选来源的限时令牌。用户原有客户端未安装更新能力，需最后执行一次新版命令；仅更新服务器不会更新旧采集器。

灰度分组限制自动升级；用户主动执行新生成的安装命令时，安装当前已激活且未暂停的签名版本，不会因随机安装 ID 未入组而安装失败。日常 `--update-now` 仍遵守灰度规则。

相同用户、网关、设备、来源且凭据有效时重用凭据。跨账户、跨网关或无效旧凭据会停止安装，不自动把旧队列转给新身份。配置、状态、队列、过滤规则和 Claude telemetry 身份留在原位置；升级不重写 telemetry。初次增加 Claude 来源仍需重启 CLI/VS Code 会话。

原有不带 `auto_update` 参数的单来源和统一安装接口保持兼容；它们不会启用自动更新。新版前端显式发送该参数，无论开启还是关闭自动检查，均使用签名管理安装流程。HTTP 站点不会生成携带凭据的托管安装命令，需要先配置 HTTPS。

## 本机运行和恢复

- 公共升级任务每 15 分钟唤起，根据 `runtime/update-state.json` 判断是否到期；正常网络检查每天一次，附加随机延迟。网络失败按 15 分钟、1 小时、6 小时退避。
- 新程序先下载、验签和自检，旧采集器继续运行。切换时通过当前用户受限的 Unix socket/Windows 命名管道请求优雅退出，确认原进程退出后才更改系统任务引用。
- 配置和队列在原目录，程序位于 `runtime/releases/<release_id>/<target>/`。不覆盖运行中的可执行文件；升级器自身的任务也切换到新版。
- 健康检查核验来源、事务和 release ID。Claude 还会观察运行至少 60 秒。失败恢复上一版本和任务，隔离本次失败的 release ID；不恢复旧队列快照、不重置扫描游标。
- 事务、恢复任务和操作系统锁处理崩溃、部分切换及断电。无法安全退出候选或恢复旧任务时标记 `recovery_required` 并保留文件，下次恢复继续尝试，不启动第二个队列写入者。
- Claude 切换可能暂时停收，已落盘数据保留；在途 OTLP 是否重试由客户端决定，不能承诺零丢失。旧 Windows Claude 首次手动迁移可能需要系统停止旧任务，后续新版通过本机控制通道优雅退出。
- `runtime/update.log` 和采集任务日志按大小轮转。个人后台显示版本、检查时间、开关、错误代码和状态时间；超过 48 小时标记过期。未回传状态不等于设备已经最新。

自动检查开关作用于本机全部已接管来源。用当前版本程序执行本机管理命令（将 `<binary>` 和 `<runtime>` 换为实际绝对路径）：

```sh
<binary> --version
<binary> --update-now --runtime <runtime> --log-file <runtime>/update.log
<binary> --auto-update off --runtime <runtime> --log-file <runtime>/update.log
<binary> --auto-update on --runtime <runtime> --log-file <runtime>/update.log
<binary> --update-recover --runtime <runtime> --log-file <runtime>/update.log
```

Windows 同样运行 `.exe` 并指定日志查看结果，无需弹出后台终端。`--update-now` 仍验证签名、版本、兼容性和灰度，可主动重试被本机隔离的发布；不会绕过管理员暂停或来源权限。

## 验证范围

公共回归覆盖签名/摘要/序号/路径/格式拒绝、准备阶段不停止采集、双来源部分切换回滚、崩溃恢复、开关和调度、凭据和配置保留、本机 IPC、设备状态权限、签名发布激活及不可变版本。macOS 另有显式启用的真实 LaunchAgent 签名升级测试，使用唯一任务名，执行后清理，不接触用户现有任务：

```sh
NEW_API_TEST_NATIVE_UPDATER=1 go test ./pkg/reporterupdate -run '^TestNativeDarwinSignedUpdate' -count=1
```

Windows 需要实机验收计划任务、无闪窗、登录补跑、杀毒占用及旧版迁移体验；跨平台编译不能替代实机验收。数据库结构使用 GORM 和跨数据库语句；发布验收还需在可用的 MySQL/PostgreSQL 环境执行迁移与并发激活验证。
