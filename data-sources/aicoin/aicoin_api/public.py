#!/usr/bin/env python3
"""AiCoin public API — stdlib only, no auth, no key.

Usage:
  from aicoin_api import *
  data = global_index()
  for idx in data[:5]:
      print(idx['index_name'], idx['last'], idx['degree']+'%')
"""

from __future__ import annotations
import json
import urllib.request
import urllib.parse
from typing import Any, Dict, List, Optional

BASE_URL = "https://www.aicoin.com"

def fetch(path: str, params: Optional[Dict[str, str]] = None,
          body: Optional[Dict[str, Any]] = None,
          method: str = "GET") -> Dict[str, Any]:
    url = f"{BASE_URL}{path}"
    if params:
        url += "?" + urllib.parse.urlencode(params, doseq=True)
    headers = {"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36", "Accept": "application/json"}
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    else:
        data = None
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=15) as resp:
        return json.loads(resp.read())


def global_index() -> List[Dict[str, Any]]:
    """20+ global real-time indices (Nasdaq, Gold, Fear & Greed...)"""
    return fetch("/api/chart/quote/global-index").get("data", {}).get("list", [])

def market_chance(key: str = "") -> Dict[str, Any]:
    """Market opportunities: signal win rate, arbitrage, gainers, breakouts. key=pagination cursor."""
    return fetch("/api/chart/common/market-chance", params={"key": key} if key else None).get("data", {})

def hot_news(coin_type: Optional[str] = None, lang: str = "en") -> Dict[str, Any]:
    """News flash + analyst long/short opinions."""
    params: Dict[str, str] = {"lan": lang}
    if coin_type:
        params["coin_type"] = coin_type
    return fetch("/api/chart/kline/common/hot-news", params=params).get("data", {})

def tp_detail(*symbols: str) -> List[Dict[str, Any]]:
    """Trading pair details + 24h OHLCV. Format: btcusdt:binance"""
    if not symbols:
        return []
    return fetch("/api/chart/multi/tp-detail", params={"symbols": ",".join(symbols)}).get("data", {}).get("list", [])

def event_point(symbol: str = "btcusdt:binance") -> List[List[Any]]:
    """Calendar event timestamps."""
    return fetch("/api/chart/event/point", params={"symbol": symbol}).get("data", {}).get("list", [])

def napi_indices() -> List[Dict[str, Any]]:
    """On-chain indices (Gold, USD index, on-chain tokens)."""
    return fetch("/napi/indices").get("data", [])

def indicator_all_config(lang: str = "en") -> Dict[str, Any]:
    """Full definitions for all technical indicators (78)."""
    return fetch("/api/chart/indicator/all-config", params={"lan": lang}).get("data", {})

def fd_options(symbol: str = "btcusdt:binance") -> List[Dict[str, Any]]:
    """Fund flow filter thresholds."""
    return fetch("/api/chart/config/fd-options", params={"symbol": symbol}).get("data", {}).get("filter", [])

def search_key(q: str) -> Dict[str, Any]:
    """Search trading pairs."""
    return fetch("/api/chart/market/search-key", body={"q": q}, method="POST")

def geoip() -> Dict[str, Any]:
    """IP geolocation."""
    return fetch("/api/common/geoip").get("data", {})

def custom_recommend(page: int = 1, page_size: int = 8,
                     tp: str = "tp", lang: str = "en") -> List[Dict[str, Any]]:
    """Recommended trading pairs or indices. type='tp'=pairs 'index'=indices."""
    return fetch("/api/common/custom/recommend",
                 params={"page": str(page), "page_size": str(page_size), "type": tp, "lan": lang}
                 ).get("data", {}).get("list", [])

def indicator_common(lang: str = "en") -> Dict[str, Any]:
    """Lightweight indicator cache."""
    return fetch("/api/chart/indicator/common", params={"lan": lang}).get("data", {})

def side_summary(symbol: str = "btcusdt:binance",
                 open_time: int = 0, period: int = 60,
                 currency: str = "usd", lang: str = "en") -> Dict[str, Any]:
    """Technical signals: signal_rate (L/S), attend_score,
    main_net_inflow, btc_ls_ratio, liq."""
    return fetch("/api/chart/stat/side-summary",
                 params={"symbol": symbol, "open_time": str(open_time),
                         "period": str(period), "currency": currency, "lan": lang}
                 ).get("data", {})
