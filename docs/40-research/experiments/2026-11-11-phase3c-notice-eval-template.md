# Phase 3c 机会通知层离线对比报告模板（只记计数 / 到期后对比）

> **设计边界（§0 / §3 B5）**：
> - 本模板与离线脚本 [`tools/research/notice_eval.py`](../../../tools/research/notice_eval.py)（CLI: `uv run python research.py notice-eval`）仅用于记录所有者真实使用后的账本数据；**本工单不产出任何评估结论**。
> - 对比口径：待未来实盘/主观样本自然积累到期后，比较**「通知来源交易」**（`notice-journal.jsonl` 中所有者反馈为 `做了 / acted` 且关联到 `trading-journal.jsonl` 已结案交易票）与同期**「非通知交易」**（未关联通知的所有者已结案交易票）的所有者实际实现结果。
> - **数据不足守卫**：当「通知来源交易」或「非通知交易」任一组已结案笔数 `< 30`（`--min-per-arm 30`）时，脚本**只输出通知、抑制与三动作反馈计数，不输出均值/胜率/对比表，不下任何结论**。

---

## 运行命令

```bash
cd tools/research
uv run python research.py notice-eval \
  --notice-journal ../../data/notice-journal.jsonl \
  --trading-journal ../../data/trading-journal.jsonl \
  --min-per-arm 30 \
  --out ../../docs/40-research/experiments/YYYY-MM-DD-phase3c-notice-eval.md
```

---

## 报告结构（数据不足期输出样式）

### 1. 通知与反馈计数

- 通知总记录数: `<total_notices>`
- 已发送 (`sent`): `<sent_notices>`（干跑 `dry_run`: `<dry_run_notices>`，失败 `failed`: `<failed_notices>`）
- 已抑制 (`suppressed`): `<suppressed_notices>`（拆分: `daily_cap=..., inside_box=..., symbol_cooldown=...`）
- 所有者反馈计数: 看了=`<seen>` · 做了=`<acted>` · 忽略=`<ignored>` · 待反馈=`<pending>`
- 已结案「通知来源交易」样本数: `<n_notice_trades>`
- 已结案「非通知交易」样本数: `<n_non_notice_trades>`

### 2. 队列结果对比

**样本不足**（通知来源交易 `n=<n_notice_trades>`，非通知交易 `n=<n_non_notice_trades>`，最低对比门槛每组 `≥30` 笔已结案交易）：**仅报计数，不产出任何评估结论。**

---

## 报告结构（未来双组样本均 `≥30` 笔到期后才解锁的描述表）

| 交易来源分组 | 已结案笔数 n | 单笔均值 R | 单笔中位数 R | 累计实现 R | 保护止损占比 |
|---|---:|---:|---:|---:|---:|
| 通知来源交易 (Notice-Linked) | `<n1>` | `<mean_r1>` | `<median_r1>` | `<sum_r1>` | `<stop_pct1>` |
| 非通知交易 (Non-Notice) | `<n2>` | `<mean_r2>` | `<median_r2>` | `<sum_r2>` | `<stop_pct2>` |

> 注：上表仅呈现所有者主观决策后的实际结案分布差异，解释权与去留决定权永远归所有者。
