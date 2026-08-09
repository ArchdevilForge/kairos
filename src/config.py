"""Configuration management."""
import os
from pathlib import Path
from typing import Optional

import yaml
from pydantic import BaseModel, Field
from pydantic_settings import BaseSettings


class TelegramConfig(BaseModel):
    bot_token: str
    chat_id: str


class FiltersConfig(BaseModel):
    min_liquidity: float = 5000
    max_liquidity: float = 100000
    min_market_cap: float = 10000
    max_market_cap: float = 500000
    max_age_minutes: int = 120
    min_smart_money_count: int = 0
    require_lp_burned: bool = False
    reject_honeypot: bool = True


class SourcesConfig(BaseModel):
    dexscreener_enabled: bool = True


class PollingConfig(BaseModel):
    interval_seconds: int = 30


class Config(BaseSettings):
    telegram: TelegramConfig
    chains: list[str] = Field(default_factory=lambda: ["sol", "base", "bsc"])
    polling: PollingConfig = Field(default_factory=PollingConfig)
    filters: FiltersConfig = Field(default_factory=FiltersConfig)
    sources: SourcesConfig = Field(default_factory=SourcesConfig)

    @classmethod
    def from_yaml(cls, path: Path) -> "Config":
        with open(path) as f:
            data = yaml.safe_load(f)

        # Expand env vars
        def expand_env(obj):
            if isinstance(obj, str) and obj.startswith("${") and obj.endswith("}"):
                var_name = obj[2:-1]
                return os.environ.get(var_name, "")
            elif isinstance(obj, dict):
                return {k: expand_env(v) for k, v in obj.items()}
            elif isinstance(obj, list):
                return [expand_env(v) for v in obj]
            return obj

        data = expand_env(data)

        # Flatten sources config
        if "sources" in data:
            sources = data["sources"]
            data["sources"] = {
                "dexscreener_enabled": sources.get("dexscreener", {}).get("enabled", True),
            }

        return cls(**data)


def load_config(config_path: Optional[Path] = None) -> Config:
    if config_path is None:
        config_path = Path(__file__).parent.parent / "config.yaml"
    return Config.from_yaml(config_path)
