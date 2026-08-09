from __future__ import annotations

import os
import tomllib
from pathlib import Path

import structlog

log = structlog.get_logger()

_CONFIG_DIR = Path(__file__).resolve().parent.parent.parent
_DEFAULT_CONFIG_PATH = _CONFIG_DIR / "config.toml"


def find_config_path() -> Path:
    env = os.environ.get("PM_BOT_CONFIG")
    if env:
        return Path(env)
    return _DEFAULT_CONFIG_PATH


def load_config(path: Path | None = None) -> dict:
    if path is None:
        path = find_config_path()
    if not path.exists():
        log.debug("config_not_found", path=str(path))
        return {}
    with open(path, "rb") as f:
        data = tomllib.load(f)
    return data


def get_clob_creds(config: dict) -> dict[str, str]:
    clob = config.get("clob", {})
    return {
        "api_key": os.environ.get("CLOB_API_KEY", clob.get("api_key", "")),
        "api_secret": os.environ.get("CLOB_SECRET", clob.get("api_secret", "")),
        "api_passphrase": os.environ.get("CLOB_PASS_PHRASE", clob.get("api_passphrase", "")),
    }


def get_private_key() -> str:
    return os.environ.get("POLY_PK", "")


def get_sizing(config: dict) -> dict[str, float]:
    sizing = config.get("sizing", {})
    return {
        "max_single": float(os.environ.get("PM_BOT_MAX_SINGLE", sizing.get("max_single", 5.0))),
        "max_daily": float(os.environ.get("PM_BOT_MAX_DAILY", sizing.get("max_daily", 50.0))),
        "kelly_fraction": float(os.environ.get("PM_BOT_KELLY", sizing.get("kelly_fraction", 0.25))),
        "max_per_city": float(os.environ.get("PM_BOT_MAX_PER_CITY", sizing.get("max_per_city", 100.0))),
        "max_total_pct": float(os.environ.get("PM_BOT_MAX_TOTAL_PCT", sizing.get("max_total_pct", 0.30))),
        "bankroll": float(os.environ.get("PM_BOT_BANKROLL", sizing.get("bankroll", 500.0))),
        "stop_loss": float(os.environ.get("PM_BOT_STOP_LOSS", sizing.get("stop_loss", 0.0))),
    }


def get_strategy_params(config: dict, strategy_name: str) -> dict:
    strategies = config.get("strategies", {})
    return dict(strategies.get(strategy_name, {}))


def get_stations(config: dict) -> dict:
    return dict(config.get("stations", {}))


def get_notifications(config: dict) -> dict:
    return dict(config.get("notifications", {}))


def get_station_for_city(config: dict, city: str) -> dict[str, str | float] | None:
    stations = get_stations(config)
    for icao, info in stations.items():
        if info.get("city", "").lower() == city.lower():
            return {"icao": icao, **info}
    return None


def get_dune_api_key(config: dict) -> str:
    """Get Dune Analytics API key from config or environment."""
    dune = config.get("dune", {})
    return str(os.environ.get("DUNE_API_KEY", dune.get("api_key", "")))
