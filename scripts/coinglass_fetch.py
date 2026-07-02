#!/usr/bin/env python3
"""Fetch and decrypt a CoinGlass API endpoint; print JSON to stdout for kairos."""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Any


def _decrypt_root() -> Path:
    env = os.environ.get("KAIROS_COINGLASS_DECRYPT", "").strip()
    if env:
        return Path(env).expanduser().resolve()
    # sibling repo: ../coinglass-decrypt next to kairos root
    kairos_root = Path(__file__).resolve().parents[1]
    sibling = (kairos_root.parent / "coinglass-decrypt").resolve()
    if (sibling / "decrypt.py").is_file():
        return sibling
    raise SystemExit(
        "coinglass-decrypt not found; clone https://github.com/ArchdevilForge/coinglass-decrypt "
        "or set KAIROS_COINGLASS_DECRYPT",
        2,
    )


def _load_fetch_and_decrypt():
    root = _decrypt_root()
    sys.path.insert(0, str(root))
    from decrypt import fetch_and_decrypt  # noqa: WPS433

    return fetch_and_decrypt


def _parse_params(raw: str) -> dict[str, str]:
    if not raw.strip():
        return {}
    out: dict[str, str] = {}
    for pair in raw.split(","):
        pair = pair.strip()
        if not pair:
            continue
        if "=" not in pair:
            raise SystemExit(f"invalid param pair: {pair}", 2)
        key, val = pair.split("=", 1)
        out[key.strip()] = val.strip()
    return out


def main() -> None:
    ap = argparse.ArgumentParser(description="CoinGlass fetch+decrypt helper for kairos")
    ap.add_argument("--path", required=True, help="API path or full URL")
    ap.add_argument("--params", default="", help="comma-separated k=v query params")
    ap.add_argument("--timeout", type=int, default=30)
    args = ap.parse_args()

    fetch_and_decrypt = _load_fetch_and_decrypt()
    path = args.path.strip()
    url = path if path.startswith("http") else f"https://capi.coinglass.com/{path.lstrip('/')}"
    params = _parse_params(args.params)

    data: Any = fetch_and_decrypt(url, params or None, timeout=args.timeout)
    json.dump(data, sys.stdout, ensure_ascii=False)


if __name__ == "__main__":
    main()
