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
    args = ap.parse_args()

    if args.cmd == "load":
        load(args.events, args.journal, args.db)
    elif args.cmd == "report":
        report(args.db, args.which)
    elif args.cmd == "sql":
        sql(args.db, args.query)


if __name__ == "__main__":
    sys.exit(main())
