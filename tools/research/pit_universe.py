"""Point-in-time (PIT) perpetual universe layer in DuckDB (Work Item 1).

Maintains `dim_perp_universe_pit(symbol, list_ts, delist_ts)` and causal 24h
quote-volume observations so any historical candidate pool at timestamp `t`
includes ONLY contracts listed at `<= t`, not yet delisted at `t`, and meeting
the ex-ante 24h volume floor.
"""
from __future__ import annotations

import json
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, Sequence

import duckdb

MAJOR_BASES: frozenset[str] = frozenset({"BTC", "ETH"})


@dataclass(frozen=True)
class PerpUniverseRecord:
    """One perpetual contract's listing lifecycle metadata."""

    symbol: str
    list_ts: int
    delist_ts: int | None = None
    tick_size: float = 0.01
    exchange: str = "okx"


@dataclass(frozen=True)
class VolumeObservation:
    """Ex-ante trailing 24h quote volume known at timestamp `ts` (Unix seconds)."""

    symbol: str
    ts: int
    quote_vol_24h: float


def normalize_base_coin(symbol: str) -> str:
    """Extract canonical uppercase base asset from an exchange or CCXT symbol."""
    val = symbol.strip().upper()
    if not val:
        raise ValueError("symbol is required")
    if ":" in val:
        val = val.split(":", 1)[0]
    for sep in ("/", "-", "_"):
        if sep in val:
            val = val.split(sep, 1)[0]
            break
    else:
        for suffix in ("USDT", "USDC", "USD", "PERP"):
            if val.endswith(suffix) and len(val) > len(suffix):
                val = val[: -len(suffix)]
                break
    if not val:
        raise ValueError(f"invalid symbol: {symbol}")
    return val


def normalize_perp_symbol(symbol: str) -> str:
    """Normalize USDT perpetual symbol to Kairos canonical `BASE/USDT:USDT`."""
    base = normalize_base_coin(symbol)
    return f"{base}/USDT:USDT"


def is_major_symbol(symbol: str) -> bool:
    """Return True for BTC and ETH perpetual contracts."""
    return normalize_base_coin(symbol) in MAJOR_BASES


def ensure_pit_schema(con: duckdb.DuckDBPyConnection) -> None:
    """Create `dim_perp_universe_pit` and `fact_perp_volume_24h` if absent."""
    con.execute(
        """
        CREATE TABLE IF NOT EXISTS dim_perp_universe_pit (
            symbol VARCHAR PRIMARY KEY,
            list_ts BIGINT NOT NULL,
            delist_ts BIGINT,
            tick_size DOUBLE DEFAULT 0.01,
            is_major BOOLEAN DEFAULT FALSE,
            exchange VARCHAR DEFAULT 'okx'
        )
        """
    )
    con.execute(
        """
        CREATE TABLE IF NOT EXISTS fact_perp_volume_24h (
            symbol VARCHAR NOT NULL,
            ts BIGINT NOT NULL,
            quote_vol_24h DOUBLE NOT NULL,
            PRIMARY KEY (symbol, ts)
        )
        """
    )


def upsert_pit_universe(
    con: duckdb.DuckDBPyConnection, records: Iterable[PerpUniverseRecord]
) -> int:
    """Insert or replace PIT perpetual universe rows."""
    ensure_pit_schema(con)
    rows = []
    for r in records:
        sym = normalize_perp_symbol(r.symbol)
        if r.delist_ts is not None and r.delist_ts < r.list_ts:
            raise ValueError(
                f"delist_ts ({r.delist_ts}) < list_ts ({r.list_ts}) for {sym}"
            )
        rows.append(
            (
                sym,
                int(r.list_ts),
                int(r.delist_ts) if r.delist_ts is not None else None,
                float(r.tick_size) if r.tick_size > 0 else 0.01,
                is_major_symbol(sym),
                r.exchange or "okx",
            )
        )
    if rows:
        con.executemany(
            """
            INSERT OR REPLACE INTO dim_perp_universe_pit
                (symbol, list_ts, delist_ts, tick_size, is_major, exchange)
            VALUES (?, ?, ?, ?, ?, ?)
            """,
            rows,
        )
    return len(rows)


def upsert_volume_24h(
    con: duckdb.DuckDBPyConnection, observations: Iterable[VolumeObservation]
) -> int:
    """Insert or replace ex-ante 24h quote volume observations."""
    ensure_pit_schema(con)
    rows = [
        (normalize_perp_symbol(o.symbol), int(o.ts), float(o.quote_vol_24h))
        for o in observations
    ]
    if rows:
        con.executemany(
            """
            INSERT OR REPLACE INTO fact_perp_volume_24h (symbol, ts, quote_vol_24h)
            VALUES (?, ?, ?)
            """,
            rows,
        )
    return len(rows)


def load_pit_jsonl(con: duckdb.DuckDBPyConnection, path: str | Path) -> int:
    """Load PIT universe rows from JSONL (`symbol`, `list_ts`, optional `delist_ts`)."""
    p = Path(path)
    records: list[PerpUniverseRecord] = []
    for raw in p.read_text().splitlines():
        line = raw.strip()
        if not line:
            continue
        obj = json.loads(line)
        delist = obj.get("delist_ts")
        records.append(
            PerpUniverseRecord(
                symbol=str(obj["symbol"]),
                list_ts=int(obj["list_ts"]),
                delist_ts=int(delist) if delist not in (None, "", 0) else None,
                tick_size=float(obj.get("tick_size") or 0.01),
                exchange=str(obj.get("exchange") or "okx"),
            )
        )
    return upsert_pit_universe(con, records)


def query_pit_candidates(
    con: duckdb.DuckDBPyConnection,
    at_ts: int,
    min_quote_vol_24h: float = 30_000_000.0,
) -> list[str]:
    """Return candidate pool at `at_ts`: listed at `<= at_ts`, not delisted at `at_ts`,
    and causal latest 24h quote volume (`ts <= at_ts`) >= `min_quote_vol_24h`.

    When `min_quote_vol_24h <= 0`, only the PIT listing lifecycle filter applies.
    """
    ensure_pit_schema(con)
    if min_quote_vol_24h <= 0:
        rows = con.execute(
            """
            SELECT symbol
            FROM dim_perp_universe_pit
            WHERE list_ts <= ?
              AND (delist_ts IS NULL OR delist_ts > ?)
            ORDER BY symbol
            """,
            [int(at_ts), int(at_ts)],
        ).fetchall()
        return [r[0] for r in rows]

    rows = con.execute(
        """
        WITH active AS (
            SELECT symbol
            FROM dim_perp_universe_pit
            WHERE list_ts <= ?
              AND (delist_ts IS NULL OR delist_ts > ?)
        ),
        latest_vol AS (
            SELECT v.symbol,
                   arg_max(v.quote_vol_24h, v.ts) AS quote_vol_24h
            FROM fact_perp_volume_24h v
            JOIN active a ON a.symbol = v.symbol
            WHERE v.ts <= ?
            GROUP BY v.symbol
        )
        SELECT a.symbol
        FROM active a
        JOIN latest_vol lv ON lv.symbol = a.symbol
        WHERE lv.quote_vol_24h >= ?
        ORDER BY a.symbol
        """,
        [int(at_ts), int(at_ts), int(at_ts), float(min_quote_vol_24h)],
    ).fetchall()
    return [r[0] for r in rows]


def fetch_okx_usdt_swaps(timeout_sec: float = 10.0) -> Sequence[PerpUniverseRecord]:
    """Fetch USDT-margined SWAP instruments from OKX public REST API."""
    url = "https://www.okx.com/api/v5/public/instruments?instType=SWAP"
    req = urllib.request.Request(url, headers={"User-Agent": "kairos-research/0.1"})
    with urllib.request.urlopen(req, timeout=timeout_sec) as resp:
        payload = json.loads(resp.read().decode("utf-8"))
    if payload.get("code") != "0":
        raise RuntimeError(f"OKX instruments error: {payload.get('msg')}")

    out: list[PerpUniverseRecord] = []
    for item in payload.get("data", []):
        inst_id = str(item.get("instId", ""))
        if not inst_id.endswith("-USDT-SWAP"):
            continue
        if item.get("ctType") != "linear":
            continue
        list_ms = int(item.get("listTime") or 0)
        exp_raw = item.get("expTime") or ""
        delist_ts = int(exp_raw) // 1000 if str(exp_raw).strip() else None
        tick_sz = float(item.get("tickSz") or 0.01)
        out.append(
            PerpUniverseRecord(
                symbol=normalize_perp_symbol(inst_id),
                list_ts=list_ms // 1000 if list_ms > 0 else 0,
                delist_ts=delist_ts,
                tick_size=tick_sz,
                exchange="okx",
            )
        )
    return out
