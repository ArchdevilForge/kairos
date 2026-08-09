"""
Configuration management for chain-trace.

Supports:
- Optional API keys (BscScan, Etherscan, Solscan)
- Cache settings
- Monitoring preferences
- Rate limit strategies
"""

import json
import os
from pathlib import Path
from typing import Optional, Dict, Any
from dataclasses import dataclass, asdict


@dataclass
class CacheConfig:
    """Cache configuration"""
    enabled: bool = True
    ttl: int = 300  # seconds
    max_size_mb: int = 100
    directory: str = "~/.chain-trace/cache"


@dataclass
class MonitoringConfig:
    """Monitoring configuration"""
    enabled: bool = False
    interval: int = 300  # seconds
    alert_on_bnb_deposit: bool = True
    alert_on_first_tx: bool = True
    alert_on_balance_drop_pct: float = 20.0
    webhook_url: Optional[str] = None


@dataclass
class APIKeysConfig:
    """Optional API keys"""
    bscscan_api_key: Optional[str] = None
    etherscan_api_key: Optional[str] = None
    basescan_api_key: Optional[str] = None
    solscan_api_key: Optional[str] = None


@dataclass
class RPCConfig:
    """RPC configuration"""
    rate_limit_strategy: str = "aggressive"  # aggressive, moderate, conservative
    max_retries: int = 3
    timeout: int = 12
    probe_on_init: bool = False


@dataclass
class Config:
    """Main configuration"""
    cache: CacheConfig = None
    monitoring: MonitoringConfig = None
    api_keys: APIKeysConfig = None
    rpc: RPCConfig = None

    def __post_init__(self):
        if self.cache is None:
            self.cache = CacheConfig()
        if self.monitoring is None:
            self.monitoring = MonitoringConfig()
        if self.api_keys is None:
            self.api_keys = APIKeysConfig()
        if self.rpc is None:
            self.rpc = RPCConfig()


class ConfigManager:
    """Configuration manager"""

    DEFAULT_CONFIG_PATH = Path.home() / ".chain-trace" / "config.json"

    def __init__(self, config_path: Optional[Path] = None):
        """
        Initialize config manager.

        Args:
            config_path: Path to config file (default: ~/.chain-trace/config.json)
        """
        self.config_path = config_path or self.DEFAULT_CONFIG_PATH
        self.config = self.load()

    def load(self) -> Config:
        """Load configuration from file"""
        if not self.config_path.exists():
            return Config()

        try:
            with open(self.config_path, 'r') as f:
                content = f.read().strip()
                if not content:
                    # Empty file, use defaults
                    return Config()
                data = json.loads(content)

            return Config(
                cache=CacheConfig(**data.get('cache', {})),
                monitoring=MonitoringConfig(**data.get('monitoring', {})),
                api_keys=APIKeysConfig(**data.get('api_keys', {})),
                rpc=RPCConfig(**data.get('rpc', {}))
            )
        except Exception as e:
            print(f"[ConfigManager] Error loading config: {e}")
            return Config()

    def save(self, config: Optional[Config] = None):
        """Save configuration to file"""
        if config:
            self.config = config

        # Create directory if needed
        self.config_path.parent.mkdir(parents=True, exist_ok=True)

        # Convert to dict
        data = {
            'cache': asdict(self.config.cache),
            'monitoring': asdict(self.config.monitoring),
            'api_keys': asdict(self.config.api_keys),
            'rpc': asdict(self.config.rpc)
        }

        # Save
        with open(self.config_path, 'w') as f:
            json.dump(data, f, indent=2)

        print(f"[ConfigManager] Config saved to {self.config_path}")

    def get_api_key(self, service: str) -> Optional[str]:
        """Get API key for service"""
        return getattr(self.config.api_keys, f"{service}_api_key", None)

    def has_api_key(self, service: str) -> bool:
        """Check if API key is configured"""
        key = self.get_api_key(service)
        return key is not None and key != ""


# Global config instance
_config_manager: Optional[ConfigManager] = None


def get_config() -> Config:
    """Get global config instance"""
    global _config_manager
    if _config_manager is None:
        _config_manager = ConfigManager()
    return _config_manager.config


def reload_config():
    """Reload configuration from file"""
    global _config_manager
    _config_manager = ConfigManager()


# CLI
if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="Chain-trace configuration")
    parser.add_argument("--init", action="store_true", help="Initialize config file")
    parser.add_argument("--show", action="store_true", help="Show current config")
    parser.add_argument("--set-api-key", nargs=2, metavar=("SERVICE", "KEY"),
                        help="Set API key (e.g., bscscan YOUR_KEY)")
    args = parser.parse_args()

    manager = ConfigManager()

    if args.init:
        print("Initializing config file...")
        manager.save()
        print(f"Config file created at: {manager.config_path}")
        print("\nEdit the file to add API keys and customize settings.")

    elif args.show:
        print("=== Current Configuration ===\n")
        print(json.dumps(asdict(manager.config), indent=2, default=str))

    elif args.set_api_key:
        service, key = args.set_api_key
        service = service.lower()

        if not hasattr(manager.config.api_keys, f"{service}_api_key"):
            print(f"Unknown service: {service}")
            print("Available: bscscan, etherscan, basescan, solscan")
        else:
            setattr(manager.config.api_keys, f"{service}_api_key", key)
            manager.save()
            print(f"API key for {service} updated")

    else:
        parser.print_help()
