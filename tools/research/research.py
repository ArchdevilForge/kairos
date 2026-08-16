#!/usr/bin/env python3
"""JSONL → DuckDB research layer.

数据流: raw JSONL(source of truth) → DuckDB(derived research layer)。
生产系统不直接依赖本层;本层只读消费 bus/out 聚合事件 + journal JSONL。

用法:
  python research.py load --events ../bus/out --journal ../../data/trading-journal.jsonl --db research.duckdb
  python research.py report --db research.duckdb
  python research.py sql "SELECT event_type, count(*) FROM events GROUP BY 1" --db research.duckdb
"""
from __future__ import annotations

import argparse
import sys
from pathlib import Path

import duckdb


def _paths(events_dir, journal):
    ev_files = []
    if events_dir:
        d = Path(events_dir)
        if d.is_dir():
            ev_files = sorted(d.glob("*.jsonl"))
        elif d.is_file():
            ev_files = [d]
    jf = Path(journal) if journal else None
    if jf and not jf.is_file():
        jf = None
    return ev_files, jf


def load(events_dir, journal, db):
    con = duckdb.connect(db)
    ev_files, jf = _paths(events_dir, journal)

    if ev_files:
        files = ", ".join(f"'{f}'" for f in ev_files)
        con.execute(f"CREATE OR REPLACE VIEW events AS SELECT * FROM read_json_auto([{files}])")
        n = con.execute("SELECT count(*) FROM events").fetchone()[0]
        print(f"events: {n} 行 ({len(ev_files)} 文件)")

    if jf:
        con.execute("CREATE OR REPLACE VIEW journal_raw AS SELECT * FROM read_json_auto(?)", [str(jf)])
        n = con.execute("SELECT count(*) FROM journal_raw").fetchone()[0]
        print(f"journal_raw: {n} 行")
        con.execute("""
            CREATE OR REPLACE VIEW journal_tickets AS
            SELECT id, at, payload
            FROM journal_raw WHERE kind = 'ticket';
        """)
    con.close()
    print(f"db: {db}")


REPORTS = {
    "events_by_floor": "SELECT floor, event_type, severity, count(*) n FROM events GROUP BY 1,2,3 ORDER BY n DESC",
    "launch_events": (
        # strategy_id/mode 是研究元数据,旧 JSONL 可能缺失;COLUMNS() 容错
        "SELECT ts, floor, event_type, symbol, severity FROM events "
        "WHERE event_type IN ('launch_confirmed','launch_fading','oi_surge') "
        "ORDER BY ts DESC LIMIT 20"
    ),
    "journal_by_kind": "SELECT kind, count(*) n FROM journal_raw GROUP BY 1 ORDER BY n DESC",
    "tickets": "SELECT id, payload FROM journal_tickets LIMIT 10",
}


def report(db, which=None):
    con = duckdb.connect(db, read_only=True)
    names = [which] if which else list(REPORTS)
    for name in names:
        q = REPORTS.get(name)
        if not q:
            print(f"未知报告: {name};可用: {', '.join(REPORTS)}")
            continue
        print(f"\n=== {name} ===")
        try:
            for row in con.execute(q).fetchall():
                print("  ", row)
        except Exception as e:
            print("  ERR:", e)
    con.close()


def sql(db, query):
    con = duckdb.connect(db, read_only=True)
    for row in con.execute(query).fetchall():
        print(row)
    con.close()


# ── H-005: Launch Demand → Aftermarket(docs/40-research/hypotheses/H-005) ────

H005_SQL = """
WITH closed AS (
    SELECT key AS auction,
           CAST(data.unique_bidders AS INT)    AS unique_bidders,
           CAST(data.bid_count AS INT)         AS bid_count,
           TRY_CAST(data.amount_total AS DOUBLE) AS amount_total,
           CAST(data.top1_share AS DOUBLE)     AS top1_share,
           CAST(data.top5_share AS DOUBLE)     AS top5_share,
           COALESCE(CAST(data.auction_state.isGraduated AS INT), 0) AS graduated
    FROM launch_events
    WHERE event_type = 'launch_auction_closed'
),
bucketed AS (
    SELECT *, NTILE(4) OVER (ORDER BY unique_bidders) AS demand_q
    FROM closed
)
SELECT demand_q,
       count(*)                    AS n,
       median(unique_bidders)      AS med_bidders,
       median(bid_count)           AS med_bids,
       round(avg(top5_share), 3)   AS avg_top5_share,
       round(avg(graduated), 3)    AS graduation_rate
FROM bucketed
GROUP BY demand_q
ORDER BY demand_q
"""


def h005(launch_dir: str, out: str | None):
    """H-005 判定: demand 分位 × graduation 率(价格采样上线前的 v0 结果变量)。

    kill test(docs/GOAL_LAUNCH_DATA_AND_RISK_GATE.md §5): 样本 ≥100 且
    top 分位与全体无可辨别差异 → 假设 reject。样本不足时只报告进度,不下结论。
    """
    d = Path(launch_dir)
    files = sorted(d.glob("*.jsonl")) if d.is_dir() else []
    if not files:
        print(f"无数据: {launch_dir} 下没有 JSONL(采集器还没跑?)")
        return 1
    con = duckdb.connect()
    flist = ", ".join(f"'{f}'" for f in files)
    con.execute(
        f"CREATE VIEW launch_events AS SELECT * FROM read_json_auto([{flist}], union_by_name=true)"
    )
    total, closed = con.execute(
        "SELECT count(*), count(*) FILTER (event_type = 'launch_auction_closed') FROM launch_events"
    ).fetchone()

    lines = [
        f"# H-005 判定报告({__import__('datetime').date.today().isoformat()})",
        "",
        f"- launch 事件总数: {total}({len(files)} 个文件)",
        f"- 已收盘 CCA auction 样本: {closed}",
        "",
    ]
    if closed < 20:
        lines.append(f"**样本不足**(n={closed} < 20): 继续采集,不下结论。")
    else:
        lines.append("| demand 分位 | n | 中位 bidders | 中位 bids | 平均 top5 集中度 | graduation 率 |")
        lines.append("|---|---|---|---|---|---|")
        for row in con.execute(H005_SQL).fetchall():
            lines.append("| " + " | ".join(str(v) for v in row) + " |")
        lines.append("")
        lines.append("> 判定口径: top 分位 graduation 率显著高于 Q1 → demand 信号有值;")
        lines.append("> 无差异且 n≥100 → kill test 触发,H-005 reject。")
        lines.append("> 价格 return_1h/4h 结果变量待 outcome 采样上线后并入。")
    con.close()

    text = "\n".join(lines)
    print(text)
    if out:
        Path(out).parent.mkdir(parents=True, exist_ok=True)
        Path(out).write_text(text + "\n")
        print(f"\n已写入: {out}")
    return 0


def main():
    ap = argparse.ArgumentParser(description="kairos research layer (JSONL → DuckDB)")
    sub = ap.add_subparsers(dest="cmd", required=True)
    p_load = sub.add_parser("load")
    p_load.add_argument("--events", help="bus/out 目录或单个 JSONL")
    p_load.add_argument("--journal", help="trading-journal.jsonl 路径")
    p_load.add_argument("--db", default="research.duckdb")
    p_rep = sub.add_parser("report")
    p_rep.add_argument("--db", default="research.duckdb")
    p_rep.add_argument("--which", choices=sorted(REPORTS))
    p_sql = sub.add_parser("sql")
    p_sql.add_argument("--db", default="research.duckdb")
    p_sql.add_argument("query")
    p_h5 = sub.add_parser("h005", help="H-005 launch demand 判定(读 data/inbound/launch)")
    p_h5.add_argument("--launch-dir", default="../../data/inbound/launch")
    p_h5.add_argument("--out", help="可选: 输出 markdown 到 docs/40-research/experiments/")
    args = ap.parse_args()

    if args.cmd == "load":
        load(args.events, args.journal, args.db)
    elif args.cmd == "report":
        report(args.db, args.which)
    elif args.cmd == "sql":
        sql(args.db, args.query)
    elif args.cmd == "h005":
        return h005(args.launch_dir, args.out)


if __name__ == "__main__":
    sys.exit(main())
