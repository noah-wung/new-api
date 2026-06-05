---
name: usage-ranking
description: 查询系统用量排名报告并发送飞书卡片。当用户要求查看账户用量排名、模型用量排名、使用统计、消耗排行时触发。
user-invocable: true
---

# Usage Ranking — 用量排名报告

查询账户和模型用量排名，构建飞书卡片，发送到飞书群。

## 执行流程

### 1. 确定时间范围

根据 prompt 关键词自动选择时间范围：
- "yesterday" / "昨天" → `yesterday`
- "today" / "今天" → `today`
- "last_week" / "上周" → `last_week`
- "week" / "最近 7 天" → `week`
- "month" / "最近 30 天" → `month`
- "year" / "最近 1 年" → `year`
- "all" / "全部" → `all`

如果无法判断，默认选择 `yesterday`。

### 2. 执行脚本

直接运行以下命令（将 `{TIME_RANGE}` 替换为步骤 1 确定的值）：

```bash
bash /opt/data/skills/usage-ranking/usage-ranking.sh {TIME_RANGE}
```

脚本会自动完成：连接数据库查询排名 → 构建飞书卡片 JSON → 通过 lark-cli 发送到飞书群。

### 3. 报告结果

脚本执行成功后，报告已发送至飞书群。输出结果中包含飞书返回的 message_id。

如果脚本执行失败，检查错误信息并重试。

## 注意事项

- 脚本路径：`/opt/data/skills/usage-ranking/usage-ranking.sh`（通过 HERMES_HOME 卷挂载自动可用）
- 脚本已内置所有配置（数据库连接、汇率、飞书 chat_id）
- 仅统计 `type = 2`（消费日志），排除 `role >= 10`（管理员）
