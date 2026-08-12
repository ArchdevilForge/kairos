# kairos research layer

JSONL → DuckDB 复盘/归因查询层。

## 数据流

```text
raw JSONL (source of truth)
   │  daily/weekly transform(load)
   ▼
DuckDB (derived research layer)  ← 本目录
   │
   ▼
research report / ad-hoc SQL
```

原则:JSONL 是不可变事实源,生产系统不依赖 DuckDB;本层只读消费。

## 用法

```bash
uv sync

# 装载(可重复,幂等;视图重建)
uv run python research.py load --events ../bus/out --journal ../../data/trading-journal.jsonl --db research.duckdb

# 内置报告
uv run python research.py report --db research.duckdb
uv run python research.py report --which launch_events --db research.duckdb

# 任意 SQL(ad-hoc 归因)
uv run python research.py sql "SELECT event_type, count(*) FROM events GROUP BY 1" --db research.duckdb

# H-005 launch demand 判定(读 bus/inbound/launch,样本<20 只报进度)
uv run python research.py h005 --launch-dir ../../bus/inbound/launch \
  --out ../../docs/40-research/experiments/H-005-report.md
```

## 常用分析方向

- 按 floor/event_type/severity 统计事件量 → attention budget 复盘
- launch_confirmed 事件 → 4h 延续率(与 kairos-calibrate lift 交叉验证)
- journal ticket/outcome → 胜率、HumanAlpha、SelectionAlpha(配合 kairos-eval)
