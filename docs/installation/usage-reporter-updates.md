# 采集器自动升级与手动升级

当前分支已恢复签名自动升级入口，采集器版本为 `1.0.3`；是否可用取决于站点 HTTPS、信任公钥、签名资产及已激活发布。此文档不表示服务器已经部署或启用。

## 安装选择与兼容

个人后台“外部用量”的统一安装入口先检查签名发布是否可用。HTTPS 和已激活、未暂停、未过期的签名发布就绪时，默认勾选“自动检查并静默升级采集器”。用户可以取消：仍以签名托管方式安装，但不自动检查和切换，可使用本机管理命令手动更新。

签名发布尚未配置、暂停或不可用时，仍可普通安装，使用每日版本检查、通知与手动升级。检查可用性失败时页面明确提示，不悄悄启用自动升级。原不带参数、或仅传 `auto_update=false` 的旧安装 API 继续普通安装；新界面通过 `managed_install=true` 明确选择签名托管。自动升级请求在签发安装令牌前检查 HTTPS 和发布可用性。

Codex 和 Claude Code 共用更新、签名验证、持久化与回退机制，每 5 分钟上报用量。新版本先下载、验签和自检，切换失败恢复旧任务；不重置凭据、队列和过滤规则。用户启用或关闭自动升级均不扩大采集来源权限。

旧设备需执行一次新版签名安装命令完成托管迁移。Claude 用户配置只有在首次托管安装成功后才写入稳定 helper，安装失败或回退时保留旧 telemetry 设置。成功后需重启 Claude CLI/VS Code 会话；后续自动升级不再改写设置或要求每次重启会话。

普通安装仍提供每天一次、同网关/平台/版本去重的系统通知和后台状态。Windows 使用 GUI 子系统程序，后台上报和升级不启动可见控制台。系统登录任务、杀毒占用和实际无闪窗体验仍需 Windows 实机验收。

## 后续发布新版

采集器版本独立维护在 `pkg/reporterupdate/VERSION`。修改采集器后提高该版本，再构建、部署网关镜像即可发布新下载资产，不需要配置签名发布流程。

版本查询读取**实际下载二进制的 Go 构建信息**，不使用网关版本推测。Docker 和开发环境下载构建已自动注入版本；外置资产必须在构建时注入对应版本，例如：

```sh
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
  -ldflags "-s -w -X github.com/QuantumNous/new-api/pkg/reporterupdate.Version=$(cat pkg/reporterupdate/VERSION)" \
  -o reporter-binaries/darwin-arm64/usage-reporter ./cmd/usage-reporter
```

Windows 构建还需使用 `-H=windowsgui`。未包含版本元数据的旧资产不显示为可用新版，版本查询不会触发编译。三平台资产应随同一镜像一起发布。关闭系统通知时，用户仍可在个人后台查看待升级状态。

本机可以执行以下命令检查（保留每日缓存与失败退避）：

```sh
<binary> --version
<binary> --config <config.json> --check-version --log-file <check.log>
```

## 签名自动升级能力

以下为签名托管安装与发布操作。没有执行过签名托管安装的设备不会注册或运行升级任务。服务器发布须安排独立上线窗口，Windows 实机和数据库验收范围见文末。

## 发布准备

自动更新使用独立采集器版本和 Ed25519 签名。普通网关 Docker 构建内的旧式下载资产仍供兼容入口使用；**它们不会自动成为可分发的签名发布包**。明文签名私钥不得进入源码、网关镜像、客户端或数据库。按项目约定，标准 age 加密的私钥与公钥保存在 `release/signing/`，可随源码管理，解密口令单独保管。

macOS 首次在本机生成密钥（已有密文时不要重新生成）：

```sh
sh tools/reporter-release.sh --mode generate-key
sh tools/reporter-release.sh --mode check-key
```

命令生成 `release/signing/release-1.key.age` 和 `public-keys.json`；256 bit 随机解密口令存入 macOS Keychain（service：`new-api.reporter-release`，account：`release-1`），不显示口令，也不落盘明文私钥。解密和签名在内存中完成。口令须另做可信备份，详见 [密钥管理说明](../../release/signing/README.md)。发布时从项目根目录运行：

```sh
sh tools/reporter-release.sh --mode build \
  --out /srv/reporter-releases --release reporter-1.0.3 \
  --version 1.0.3 --sequence 1 --rollout 10 --seed stable-1
```

工具构建 darwin-arm64、darwin-amd64 和 windows-amd64，注入采集器版本、Git commit、release ID；Windows 使用 GUI 子系统。

仓库同时提供手动触发的 `Signed collector release` 工作流。在 GitHub 的 `collector-release` environment 配置 `REPORTER_RELEASE_KEY_PASSPHRASE`（与上述密文对应的解密口令）后，可输入版本、发布 ID、序号和灰度参数生成签名资产 artifact。工作流从仓库读取密文和公钥，不写明文私钥文件，不再使用旧 `REPORTER_RELEASE_SIGNING_KEY`。工作流不会自动部署或激活发布，artifact 不含私钥。

非 macOS 环境可从受控 Secret 注入 `REPORTER_RELEASE_KEY_PASSPHRASE` 后运行相同脚本。旧的项目外 `--key-file` 明文私钥方式仍兼容，但不能与 `--encrypted-key-file` 同时使用；缺少解密口令时不会自动回退到明文方式。

发布目录格式：

```text
/srv/reporter-releases/reporter-1.0.3/
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

部署更新功能版本的网关后，管理员在“外部用量 → 采集器发布管理”输入 `reporter-1.0.3` 并激活。激活会验证签名、有效期、三个平台资产、摘要及版本不可变性。缺少资产、校验失败或清单序号倒退均不允许激活。发布操作写入管理员系统日志。

发布目录中的二进制不可覆盖。同一版本和 release ID 只能调整部署清单；代码变化必须生成更高版本和新 release ID。服务端将当前签名清单及激活指针存入数据库，多副本必须同时能读取相同的只读资产目录。

## 灰度、暂停与密钥轮换

同一发布扩大灰度时，只重新签署清单，不重新编译二进制：

```sh
sh tools/reporter-release.sh --mode sign \
  --out /srv/reporter-releases --release reporter-1.0.3 \
  --version 1.0.3 --sequence 2 --rollout 50 --seed stable-1
```

重新激活同一个发布 ID，网关验证序号增加且二进制身份未变。后续可使用更高序号推进到 100%。保持 seed 不变，设备基于本机随机安装 ID 稳定分组，无需上传硬件信息。按钮“暂停采集器更新”阻止后续获取部署清单；已经开始切换的事务不保证瞬间撤回。已提交版本的问题应通过更高版本修复。

清单默认有效 30 天。到期前须使用 `--mode sign` 和更高序号续签、重新激活；客户端不会安装过期清单。支持 `--next-keys /secure/next-public-keys.json` 在旧密钥签名的清单中授权新公钥。须保留旧、新密钥的重叠发布期，让在线客户端先收到授权；不能让客户端从未受信的新清单直接接受新公钥。

## 安装、迁移与身份

网关须通过可信 HTTPS 提供新版统一安装命令。安装程序会对自身二进制、清单、公钥信任和现有配置做预检，再兑换所选来源的限时令牌。用户原有客户端未安装更新能力，需最后执行一次新版命令；仅更新服务器不会更新旧采集器。

灰度分组限制自动升级；用户主动执行新生成的安装命令时，安装当前已激活且未暂停的签名版本，不会因随机安装 ID 未入组而安装失败。日常 `--update-now` 仍遵守灰度规则。

相同用户、网关、设备、来源且凭据有效时重用凭据。跨账户、跨网关或无效旧凭据会停止安装，不自动把旧队列转给新身份。配置、状态、队列、过滤规则和 Claude telemetry 身份留在原位置；升级不重写 telemetry。初次增加 Claude 来源仍需重启 CLI/VS Code 会话。

原有不带 `auto_update` 参数、或仅传 `false` 的单来源和统一安装接口保持普通安装兼容。新版前端在签名就绪时显式发送 `managed_install=true` 和 `auto_update`，无论开启还是关闭自动检查，均使用签名管理安装流程。HTTP 站点不会生成携带凭据的托管安装命令，需要先配置 HTTPS。

## 本机运行和恢复

- 公共升级任务每 15 分钟唤起，根据 `runtime/update-state.json` 判断是否到期；正常网络检查每天一次，附加随机延迟。网络失败按 15 分钟、1 小时、6 小时退避。
- 新程序先下载、验签和自检，旧采集器继续运行。切换时通过当前用户受限的 Unix socket/Windows 命名管道请求优雅退出，确认原进程退出后才更改系统任务引用。
- 配置和队列在原目录，程序位于 `runtime/releases/<release_id>/<target>/`。Claude 的固定 helper 位于 `runtime/launcher/`，校验安装记录中当前版本的路径与 SHA-256 后调用该版本；更新和回退只改变已提交版本指针，helper 不在切换时等待升级锁。不覆盖运行中的可执行文件；升级器自身的任务也切换到新版。
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

本分支已在 macOS 运行上述隔离 LaunchAgent 测试，验证 Codex 与 Claude Code 同时切换、凭据与状态保留、Claude 设置不改写、稳定 helper 调用当前版本及升级后真实 OTLP 入队；提交/回退/篡改由自动回归另行覆盖。真实 Claude CLI 2.1.236 也已通过隔离 HOME 与本地模拟 API 的用户设置、项目覆盖和启动环境三路验证。测试未访问生产服务器或真实模型。

Windows 需要实机验收计划任务、无闪窗、登录补跑、杀毒占用及旧版迁移体验；跨平台编译不能替代实机验收。数据库结构使用 GORM 和跨数据库语句；发布验收还需在可用的 MySQL/PostgreSQL 环境执行迁移与并发激活验证。


## 安装时选择与追加采集来源

个人后台“外部用量”的统一安装入口只需选择系统，生成命令后在本机终端执行。
脚本会将管理员已启用的来源显示为一个多选清单，首次运行默认不勾选：

```text
> [ ] Codex
  [ ] Claude Code (CLI and VS Code)
Up/Down: move  Space: select  Enter: install  q: cancel
```

使用上、下方向键移动 `>` 光标，按空格勾选或取消当前来源，按回车统一安装勾选的来源，按 `q` 取消。方向键在首尾循环，选择实时显示，每个来源独立勾选，没有组合选项。没有勾选时确认直接退出，不下载采集程序、不兑换令牌、不修改本地配置。输入中断或 EOF 不会默认安装。请使用后台提供的命令在终端运行：macOS 由 Bash 读取按键，Windows 由 PowerShell 控制台读取按键。确认、取消或中断时恢复终端回显与光标状态，不要将安装脚本直接管道传给 `sh`。

以后需要追加来源时，重新生成并执行命令，只选择新增来源即可。跳过已有来源不会卸载或停止它；原有配置、凭据和采集进度保留。签名托管安装继续共用一个升级器，自动升级开关作用于整个托管安装。

后台“上报设备”的“已注册采集来源”列显示实际兑换授权的来源，同一电脑的 Codex、Claude Code 各有一条记录。只生成安装命令不会产生设备记录；本机完成注册后刷新后台即可查看。注册成功不等于后台任务已运行，请结合最后上报时间检查。手工签发的设备记录同样只表示已授权来源。

接口兼容：新入口提交 `interactive: true`，`sources` 表示可供本机选择的授权范围；脚本只把所选来源的令牌交给原有普通或签名安装器。未选择令牌不会兑换，按原有 15 分钟有效期过期。旧请求省略 `interactive` 时继续按原方式安装指定来源，无需交互。没有签名发布时仍可使用普通交互安装；已有托管来源仍受旧安装器覆盖保护。

选择器按通用选项列表渲染，每个选项包含显示名称及对应授权令牌参数，焦点和勾选状态仅用于本次交互，后台仍记录稳定的来源标识。以后新增客户端需先完成适配器、来源授权和安装接入，再加入可用选项声明；清单自动增加一行，勾选、确认、过滤及共用升级流程不需要增加组合分支。
