# 客户端采集器公共模块与适配器

`pkg/usagereporter` 现在按“客户端解析 → 标准事件 → 公共 Reporter”组织。Codex 和 Claude Code 都通过相同的 Reporter 完成持久化、过滤和发送，不再各自实现上报流程。CLI 通过 `RunClient` 的注册表选择运行入口。

## 职责

| 公共部分 | 客户端特有部分 |
| --- | --- |
| 标准 `UsageEvent` / `CollectedEvent`、身份隔离 | 原始文件、事件协议及字段解释 |
| `RoutePolicy`：origin 规范化、网关优先、允许列表、排除路由 ID | 从客户端配置或事件提取可靠的 origin / 路由证据 |
| `Reporter`：持久队列、待报去重、冲突检查、每批最多 100 条、ACK、拒绝隔离、失败重试 | 累计 token 转增量、缓存语义、请求稳定身份构造 |
| 每 5 分钟调度、超时、取消、空队列跳过 | 文件轮询或常驻接收的生命周期与特有诊断 |
| 原子私有文件写入、CLI 日志和后台执行 | 客户端 telemetry/hooks 的配置和扫描检查点 |

Codex 适配器保留 JSONL 文件位置、累计量基线、provider 解析和未知 provider 延后。它把 provider 转为标准 origin 和摘要路由 ID，地址是否属于网关由公共策略判断。Claude 适配器保留 OTLP HTTP/JSON、本机 Bearer 认证、请求事件解析和 telemetry 配置；它复用同一策略及发送队列。

## 公共接口

- `NewReporter(ReporterConfig)`：创建特定来源、服务器、凭据的发送器。`ExternalURLs == nil` 表示允许已知且不属于网关的路由；显式空列表拒绝所有外部路由。适配器必须按自己的可信路由能力选择规则，不能把未知路由伪装成官方地址。
- `Record(CollectedEvent)`：校验、过滤并持久化一条标准事件；网关用量直接跳过。失败时，采集端保留重试能力。
- `Report(ctx, Collection)`：处理一次轮询快照，复用同一队列和上传逻辑，成功的发送周期后调用适配器提供的 `Commit` 保存扫描检查点。服务端未确认的事件仍在队列中，不随检查点推进而丢失。分批发送期间，已确认的前缀暂存为私有 `.confirmed` 收据；上传或检查点提交失败后重新扫描也不会再次入队，检查点成功提交后收据即被清理。
- `FlushContext(ctx)`：按来源生成上报批次；只处理本次快照，每批最多 100 条，积压分批发送。网络失败或取消后，未确认事件留在磁盘。
- `RunUploads(ctx, diagnostics)`：供常驻接收器使用，在启动时及之后每 5 分钟调用同一发送流程。轮询入口通过系统任务以相同的 `DefaultReportInterval` 执行。

`CollectedEvent.Origin` 和可选 `RouteID` 只在本机队列内保存，不进入 HTTP 用量载荷。`RouteID` 使用摘要，供公共策略排除客户端中的历史 provider/profile；原始 provider 名、密钥、提示词、响应正文和任意原始属性都不应交给 Reporter。

队列仅按稳定事件 ID 去重并识别冲突；跨重放的最终幂等性仍由网关事件 ID 约束保证。只有当前批次的 accepted/duplicate/rejected ID 可以修改队列，其他 ID 被忽略。永久拒绝保存为 `.json.rejected`，不保存服务器返回的任意拒绝文本，重复采集不会重新入队。默认队列上限为 10,000 个事件文件，包含拒绝文件；`ReporterConfig.QueueCapacity` 可设置更小的上限。轮询快照超过容量时会先分批排空再继续入队，不会停留在满队列。

## 新增客户端

1. 在来源适配器中读取客户端数据，转换为 `CollectedEvent`。事件 ID 必须是稳定的 SHA-256 摘要；重试和重启不得换 ID。session/profile 等本地身份也应只保留必要摘要。
2. 按该客户端语义归一化输入、缓存和输出 token。公共层校验非负、32 位计数范围、时间窗口和模型字段；累计量转增量、缓存写入是否计入输入等语义由适配器负责。
3. 文件采集使用 `Reporter.Report`，只提供事件列表及保存扫描位置的 `Commit`。推送采集将规范化事件交给 `Record`，并运行公共 `RunUploads`；不能自己再次调用 HTTP 上传或另写过滤逻辑。
4. 在 `client.go` 注册来源与轮询/常驻模式，将来源加入服务端已实现来源声明、站点允许列表和安装选择。注册、权限和客户端配置属于接入声明；新客户端无需重写队列、网关判断、批次、ACK 或重试。
5. 新增该客户端的数据样本、语义与路由证据回归。公共契约测试已经使用未内置的 `future_client` 来源验证公共逻辑不依赖 Codex/Claude 分支，不需要为每个客户端重复这套测试。

轮询适配器的发送部分只需调用以下公共接口，不编写 HTTP、网关过滤或重试循环：

```go
reporter, err := usagereporter.NewReporter(usagereporter.ReporterConfig{
    Source: "example_client", Server: server, Credential: credential,
    StatePath: statePath, GatewayURLs: gatewayAliases,
    ExternalURLs: approvedOrigins,
})
if err != nil {
    return err
}
_, err = reporter.Report(ctx, usagereporter.Collection{
    Events: normalizedEvents,
    Commit: saveScanCheckpoint,
})
return err
```

`RunClient` 的注册表只负责选择来源及运行模式；`CollectUsage` 等旧扫描接口继续保留兼容，不作为新增适配器的上报入口。

## 升级兼容与验证

保留既有 `source` 值、上报 HTTP 结构、Codex 事件 ID、状态字段和 Claude 队列的目录及 JSON 格式。旧单来源安装和统一安装入口继续可用，Windows GUI 子系统及日志参数也不受适配器拆分影响。

新 Codex 待报事件位于 `<state_path>.usage-queue/codex/<server-and-credential-digest>/`；Claude 继续读取 `<state_path>.claude-queue/<server-and-credential-digest>/`，无需清空或重建。服务器、来源和凭据隔离待报数据；变更账户/凭据不会自动转移另一身份的队列，轮换前应排空或保留旧配置处理待报数据。

回归覆盖同模型的网关/直连区分、默认端口/IPv6/别名、未知路由拒收、排队后策略变更、跨批次 ACK、防止不完整 ACK 丢数据、重启重试、取消保留数据、拒绝重放和旧 Claude 队列直接读取。来源特有协议和既有安装端到端测试继续验证两个真实适配器。
