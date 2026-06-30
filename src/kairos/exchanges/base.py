import asyncio
import logging
import threading
import time
from typing import Any

import ccxt  # type: ignore[import-untyped]

from kairos.detectors.base import BaseDetector


class BaseExchange:
    def __init__(self, exchange_name: str):
        if exchange_name not in ccxt.exchanges:
            raise ValueError(f"Exchange {exchange_name} not supported by ccxt")
        self.exchange_name = exchange_name
        self.exchange = getattr(ccxt, exchange_name)({"enableRateLimit": True, "timeout": 8000})
        self.ws = None
        self.ws_connected = False
        self.ws_data: dict[str, Any] = {}
        self.last_prices: dict[str, Any] = {}
        self._price_lock = threading.Lock()
        self.ws_thread = None
        self.running = False
        self._detectors: list[BaseDetector] = []
        logging.info("BaseExchange initialized for %s", exchange_name)

    def register_detector(self, detector: BaseDetector) -> None:
        self._detectors.append(detector)

    def _notify_detectors_price(self, symbol: str, price: float) -> None:
        if not (0 < price < 1e12):
            return
        ts = time.time()
        for d in self._detectors:
            try:
                d.on_price_update(symbol, price, ts)
            except Exception:
                pass

    def _notify_detectors_volume(self, symbol: str, cumulative_volume: float) -> None:
        ts = time.time()
        for d in self._detectors:
            try:
                d.on_volume_update(symbol, cumulative_volume, ts)
            except Exception:
                pass

    def start_websocket(self, symbols):
        try:
            logging.info("Starting WebSocket for %s (%d symbols)", self.exchange_name, len(symbols))
            self.running = True

            def run_ws():
                loop = asyncio.new_event_loop()
                asyncio.set_event_loop(loop)
                try:
                    loop.run_until_complete(self._ws_connect(symbols))
                except Exception as e:
                    logging.error("WebSocket thread error on %s: %s", self.exchange_name, e)
                finally:
                    loop.close()

            self.ws_thread = threading.Thread(target=run_ws, daemon=True)
            self.ws_thread.start()
            timeout = 10
            start = time.time()
            while not self.ws_connected and time.time() - start < timeout:
                time.sleep(0.1)
            if not self.ws_connected:
                raise ConnectionError("WebSocket connection timeout")
            logging.info("WebSocket connected: %s", self.exchange_name)
        except Exception:
            logging.error("Failed to start WebSocket for %s", self.exchange_name)
            raise

    def stop_websocket(self):
        self.running = False
        if self.ws_thread:
            self.ws_thread.join(timeout=5)
            self.ws_thread = None

    def close(self):
        self.stop_websocket()
        if hasattr(self.exchange, "close"):
            self.exchange.close()
