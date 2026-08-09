# Hypothesis Template

每一条交易认知必须走完生命周期才可能成为规则：
`Source → Hypothesis → Experiment → Finding → Canonical`

```markdown
---
id: H-001
status: proposed | testing | rejected | promoted
source: <来源文档/链接, 如 SOURCE_BIT_LANGLANG.md §2.3 / newsliquid 教程>
created: 2026-08-09
owner: kairos
---

# H-001: <一句话假设>

## 原始观点(Source)
<原文摘录, 标注出处>

## 可验证表述
<在 X 条件下, Y 指标是否 Z?>

## 可观测数据
- 数据源: <binance fapi / coinglass / dexscreener ...>
- 端点/字段: <具体到 endpoint + field>
- 样本窗口: <时间范围 / 事件数>

## 测试设计
- 模式: shadow | backtest | paper
- 对照组: <non-event baseline, 剔除事件窗口>
- 判定标准: <lift / hit rate / median excess return 阈值>

## 结果
- 样本数:
- 结论: rejected | promoted
- 去向: <promoted → 写入 KAIROS_DOCTRINE 或 playbook>
```
