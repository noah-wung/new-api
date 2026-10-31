# ZCode 外部用量采集（本地实现，尚待验收）

本实现只读取 ZCode `model_usage` 的计量字段，以 `zcode` 来源复用外部用量 Reporter；不会修改余额、额度或网关计费。只覆盖所选数据库中存在的请求。桌面版 3.16.3 的结构已做只读观察；独立 CLI、Windows 和远端 Agent 的真实数据路径及行为仍需在对应平台验证。

## 基线与本次核查

2026-10-29 12:43–12:45（Asia/Shanghai），只读检查 `172.16.0.37` 的 `new-api` 容器：镜像 `new-api:30941dc65-20261016`，OCI revision `30941dc6534ec64c554f683fbaa1d0cb86af0049`，`pro.newapi.branch=codex/claude-code-usage-reporter`，版本 `v1.0.0-rc.10-185-g30941dc65`；本地该分支 HEAD 与 revision 一致。发布目录只读挂载。基线外的 Windows 未提交修复未纳入。生产来源设置、事件索引和重复事实本次未核实。

本机源库只读汇总为 928 行（completed 主会话 784、标题 17、子 Agent 115；cancelled/error 共 12）；总量与 input+output 不一致为 0 行，cache-write 非零 0 行，reasoning 非零 26 行。该样本和先前方案均为特定时间快照，不能外推其他版本及平台。

## 数据库与配置

数据库依次使用 `--zcode-db`、配置文件 `zcode_db_path`、`ZCODE_SESSION_DB_PATH`、`ZCODE_SESSION_DB`，默认 `~/.zcode/cli/db/db.sqlite`。采集器只读打开数据库并读取 WAL，不创建缺失数据库，不读取消息、原始 usage 或认证字段。`--self-check --config <path>` 只读检查本地源和队列，不上传、不改状态。

ZCode 的路由历史不能仅靠当前 provider 配置推断。管理员或用户应依据可核实的请求期证据，在独立 ZCode 配置中指定 provider 与时间区间；秒级 Unix 时间区间 `[from, until)`，下例为示意值，不代表实际服务或授权：

```json
{
  "source": "zcode",
  "server": "https://gateway.example",
  "credential": "<reporting credential>",
  "state_path": "~/.new-api-usage-reporter/zcode/state.json",
  "zcode_db_path": "~/.zcode/cli/db/db.sqlite",
  "gateway_urls": ["https://gateway-alias.example"],
  "zcode_gateway_hosts": ["token.einyun.com"],
  "zcode_excluded_providers": ["internal-gateway"],
  "zcode_provider_routes": [
    {"provider_id": "external-provider", "origin": "https://api.example", "from": 1790000000, "until": 1800000000}
  ]
}
```

`zcode_gateway_hosts` 按精确主机名排除网关，覆盖 HTTP、HTTPS 和所有端口，不自动匹配子域名。这个规则不会证明历史记录的请求地址；仍需要请求期间有效的 provider 路由证据。

仅当同一条路由证据覆盖请求从 started_at 到 completed_at 的整个区间时才允许上报；跨路由切换、重叠、不完整或不可信的路由证据不作外部上报；服务地址、网关别名和明确排除的 provider 优先排除。未知路由保存最小快照，补充有效映射后可在源行清理后重放。配置和状态包含本地路由信息，应按私有文件保护；上报 payload 不含上游地址、provider、路径或凭据。修改映射时应核实历史生效时段，不能把当前配置自动套用于安装前请求。

只计 `input + output`，缓存读和 reasoning 分别是输入和输出的子集；缓存写仍在 input 中，不额外相加。正用量终态按调用行计量，零用量不提交。数据库允许同 ID 更新；进入持有状态后观察到的改变保存冲突，不生成新 ID 或自动更正。异常计量保存在本地隔离状态，并显示计数。本地快照区分已扫描、已入队与服务器 accepted/duplicate/rejected；缺少 ACK 的事件保留在队列。队列和快照各有 10,000 条上限；容量不足停止处理并报告，不能扩大限制或假称完整。当前快照不会自动清理，长期使用可能触及上限；部署前需要制定经验证的归档或清理机制。

## 安装与升级

ZCode 仅允许签名托管安装，单独使用 `~/.new-api-usage-reporter/zcode/` 配置和状态；使用用户级调度，不配置 ZCode telemetry 或接收端口。合并安装仍为每个来源生成独立的一次性授权；设备上限沿用服务器设置。下载的二进制先执行 `--supports-source zcode`，不支持时不会兑换 ZCode token。该检查与签名验证均需通过；旧二进制不具备 ZCode 能力。

源库本地保留期与服务器接受窗口不同，源数据已经清理时不能保证补齐。仅有当前路由配置不能证明历史链路，首次安装后应检查 unresolved、excluded、invalid 和 conflict 计数；不要在未核实路由时将其标为外部。

凭据或服务器变更时，采集器拒绝自动切换状态及队列命名空间。保留原配置、状态与整个队列，先核对并处理待确认/拒收记录，再制定显式迁移步骤；不可删除旧队列或新建身份后假定已迁移。回滚到不认识 ZCode 的二进制时，应暂停该 profile 并保留配置、状态和队列；不能让旧程序继续运行该任务。Codex/Claude profile 和队列不属于本次迁移范围。

## 部署前阻塞项

- 在三种数据库分别验证并发提交、事务、聚合与删除重放；只读审计生产 `external_usage_client_events` 的 `(user_id, source, event_identity)` 重复项，比较事实与聚合，解决任何冲突后再启用；新 ZCode 身份账本和用户锁由本地迁移定义创建，不能据旧索引名推断唯一性。
- 在目标 Windows 实机验证数据库路径、活动 WAL、安装、调度、升级和回退；macOS 两种架构的跨编译不是原生验收。
- 验证终态是否经常被修订、重试及异常终态有用量的真实样本。若需自动更正，先设计受控更正协议和聚合事务；当前只隔离冲突。
- 测试真实 signed catalog、旧版本能力拒绝、三来源组合安装、队列满、部分 ACK、凭据轮换和策略变化；签名、上传、激活和生产启用需单独授权。

### 历史重复预检（只读）

以下查询使用三种数据库都支持的基本 SQL；在隔离副本或经授权的只读连接上执行。返回的用户及身份仅供授权人员处理，不应公开：

```sql
SELECT user_id, event_identity, COUNT(*) AS duplicate_count
FROM external_usage_client_events
WHERE source = 'zcode'
GROUP BY user_id, event_identity
HAVING COUNT(*) > 1;
```

逐项核对计量值、发生时间与相应小时聚合，保留审计记录和备份，再制定人工处理方案。明细可能已按保留期清理而聚合仍在，因此不能把“现存明细总和不等于聚合”直接视为错误，也不能自动删除重复项或重建聚合。已存在的历史 ZCode 明细应在清理前建立一致的身份账本；代码提供只读审计 `AuditZCodeDuplicateIdentities` 和显式事务回填 `BackfillZCodeIdentitiesTx`；回填期间须暂停 ZCode 上报和明细清理，先解决重复与冲突并备份。它不自动修复聚合，目前也没有生产执行入口；上线前仍须准备并审查具体执行方式。
