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


def run_notice_eval_cli(
    notice_journal: str,
    trading_journal: str,
    min_per_arm: int,
    link_window_hours: float,
    out: str | None,
) -> int:
    """Run Phase 3c offline Notice cohort comparison skeleton (count-only when sample insufficient)."""
    from notice_eval import evaluate_notice_cohorts, format_notice_eval_markdown

    summary = evaluate_notice_cohorts(
        notice_journal,
        trading_journal,
        min_trades_per_arm=min_per_arm,
        link_window_hours=link_window_hours,
    )
    md = format_notice_eval_markdown(summary)
    print(md)
    if out:
        op = Path(out)
        op.parent.mkdir(parents=True, exist_ok=True)
        op.write_text(md + "\n", encoding="utf-8")
        print(f"\n已写入: {op}")
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
    p_neval = sub.add_parser(
        "notice-eval",
        help="Phase 3c 机会通知层离线对比骨架（数据不足时只报计数，不下结论）",
    )
    p_neval.add_argument("--notice-journal", default="../../data/notice-journal.jsonl")
    p_neval.add_argument("--trading-journal", default="../../data/trading-journal.jsonl")
    p_neval.add_argument("--min-per-arm", type=int, default=30)
    p_neval.add_argument("--link-window-hours", type=float, default=4.0)
    p_neval.add_argument("--out", help="可选: 输出离线对比报告 Markdown")
    args = ap.parse_args()

    if args.cmd == "load":
        load(args.events, args.journal, args.db)
    elif args.cmd == "report":
        report(args.db, args.which)
    elif args.cmd == "sql":
        sql(args.db, args.query)
    elif args.cmd == "notice-eval":
        return run_notice_eval_cli(
            args.notice_journal,
            args.trading_journal,
            args.min_per_arm,
            args.link_window_hours,
            args.out,
        )


if __name__ == "__main__":
    sys.exit(main())
