---
id: H-006
status: proposed
source: 链上行为分析(chain-trace 方法迁移到 launchpad)
created: 2026-08-09
---

# H-006: Creator / Funder Alpha

## 原始观点(Source)
"同一个 creator 历史 launch 表现可预测下一次 launch 质量"

## 可验证表述
creator 历史特征(funding source, wallet age, previous launches, previous ATH,
rug count, dump latency, linked wallets)组成的 creator_score 能否预测新 launch
的 aftermarket 表现 / rug 概率?

## 可观测数据
- 数据源: Robinhood Chain RPC(creator 钱包历史)+ chain-trace 工具
- 特征: creator_score 各分量

## 测试设计
- 模式: shadow;Tier A(历史 2x 率/rug 数) vs HARD REJECT(12 launches 11 dead)
- 判定: Tier A 组 median 1h/4h return vs 全体

## 结果
- 样本数: 0
- 结论: pending
