from __future__ import annotations

import threading
import time
from typing import Any, Callable, TypeVar

import httpx
import structlog

from pm_bot.core.config_loader import get_clob_creds, get_private_key, get_sizing, load_config

log = structlog.get_logger()

CLOB_HOST = "https://clob.polymarket.com"
CHAIN_ID = 137
DEFAULT_HTTP_TIMEOUT = 15.0
MAX_425_RETRIES = 3
HEARTBEAT_RECOVERY_ATTEMPTS = 3
GAMMA_POSITIONS_URL = "https://data-api.polymarket.com/positions"


def compute_v2_taker_fee(fee_rate_bps: int, price: float, exponent: float = 1.0) -> float:
    base = fee_rate_bps / 10000.0
    return (
        base * price * (price * (1.0 - price)) ** (exponent - 1.0) if exponent != 1.0 else base * price * (1.0 - price)
    )


T = TypeVar("T")


def _retry_on_425(fn: Callable[[], T], max_retries: int = MAX_425_RETRIES) -> T:
    for attempt in range(max_retries + 1):
        try:
            return fn()
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 425 and attempt < max_retries:
                wait = 2**attempt * 5
                log.warning("425_matching_engine_restart", attempt=attempt, retry_after_s=wait)
                time.sleep(wait)
                continue
            raise
    raise RuntimeError("unreachable")


class ClobTrader:
    def __init__(self, config: dict | None = None) -> None:
        self._config = config or load_config()
        self._client: Any = None
        self._heartbeat_thread: threading.Thread | None = None
        self._heartbeat_id: str = ""
        self._running: bool = False
        self._daily_spent: float = 0.0

    def _get_client(self) -> Any:
        if self._client is not None:
            return self._client

        pk = get_private_key()
        creds_dict = get_clob_creds(self._config)

        if not pk:
            raise ValueError("POLY_PK environment variable is required for trading")

        if not creds_dict["api_key"]:
            raise ValueError("CLOB API credentials not configured. Set [clob] in config.toml or CLOB_API_KEY env var.")

        from py_clob_client_v2 import ApiCreds, ClobClient  # type: ignore[import-untyped]

        creds = ApiCreds(
            api_key=creds_dict["api_key"],
            api_secret=creds_dict["api_secret"],
            api_passphrase=creds_dict["api_passphrase"],
        )
        self._client = ClobClient(
            host=CLOB_HOST,
            chain_id=CHAIN_ID,
            key=pk,
            creds=creds,
        )
        return self._client

    def _check_sizing(self, amount_usd: float) -> str | None:
        sizing = get_sizing(self._config)
        if amount_usd > sizing["max_single"]:
            return f"Amount ${amount_usd:.2f} exceeds max_single ${sizing['max_single']:.2f}"
        if self._daily_spent + amount_usd > sizing["max_daily"]:
            remaining = sizing["max_daily"] - self._daily_spent
            return f"Daily limit ${sizing['max_daily']:.2f} reached (remaining: ${remaining:.2f})"
        return None

    def _recover_heartbeat_id(self) -> str:
        for _ in range(HEARTBEAT_RECOVERY_ATTEMPTS):
            try:
                client = self._get_client()
                resp = client.post_heartbeat("")  # type: ignore[attr-defined]
                if isinstance(resp, dict) and "heartbeat_id" in resp:
                    new_id = str(resp["heartbeat_id"])
                    log.info("heartbeat_id_recovered", new_id=new_id)
                    return new_id
            except Exception as e2:
                log.warning("heartbeat_recovery_failed", error=str(e2))
                time.sleep(2)
        return self._heartbeat_id

    def place_limit_buy(
        self,
        token_id: str,
        price: float,
        size: float,
        tick_size: str = "0.01",
        neg_risk: bool = True,
    ) -> dict | None:
        amount_usd = price * size
        err = self._check_sizing(amount_usd)
        if err:
            log.error("sizing_check_failed", error=err)
            return None

        def _place() -> dict:
            from py_clob_client_v2 import OrderArgs, OrderType, PartialCreateOrderOptions  # type: ignore[import-untyped]
            from py_clob_client_v2.order_builder.constants import BUY  # type: ignore[import-untyped]

            client = self._get_client()
            return client.create_and_post_order(  # type: ignore[attr-defined,no-any-return]
                order_args=OrderArgs(
                    token_id=token_id,
                    price=price,
                    size=size,
                    side=BUY,
                ),
                options=PartialCreateOrderOptions(
                    tick_size=tick_size,
                    neg_risk=neg_risk,
                ),
                order_type=OrderType.GTC,
            )

        try:
            response = _retry_on_425(_place)
            self._daily_spent += amount_usd
            log.info("order_placed", token_id=token_id, side="BUY", price=price, size=size)
            return response  # type: ignore[no-any-return]
        except Exception as e:
            log.error("order_failed", token_id=token_id, error=str(e))
            return None

    def place_limit_sell(
        self,
        token_id: str,
        price: float,
        size: float,
        tick_size: str = "0.01",
        neg_risk: bool = True,
    ) -> dict | None:
        amount_usd = price * size
        err = self._check_sizing(amount_usd)
        if err:
            log.error("sizing_check_failed", error=err)
            return None

        def _place() -> dict:
            from py_clob_client_v2 import OrderArgs, OrderType, PartialCreateOrderOptions  # type: ignore[import-untyped]
            from py_clob_client_v2.order_builder.constants import SELL  # type: ignore[import-untyped]

            client = self._get_client()
            return client.create_and_post_order(  # type: ignore[attr-defined,no-any-return]
                order_args=OrderArgs(
                    token_id=token_id,
                    price=price,
                    size=size,
                    side=SELL,
                ),
                options=PartialCreateOrderOptions(
                    tick_size=tick_size,
                    neg_risk=neg_risk,
                ),
                order_type=OrderType.GTC,
            )

        try:
            response = _retry_on_425(_place)
            self._daily_spent += amount_usd
            log.info("order_placed", token_id=token_id, side="SELL", price=price, size=size)
            return response  # type: ignore[no-any-return]
        except Exception as e:
            log.error("order_failed", token_id=token_id, error=str(e))
            return None

    def place_market_buy(
        self,
        token_id: str,
        amount: float,
        price: float = 0.99,
        tick_size: str = "0.01",
        neg_risk: bool = True,
    ) -> dict | None:
        err = self._check_sizing(amount)
        if err:
            log.error("sizing_check_failed", error=err)
            return None

        def _place() -> dict:
            from py_clob_client_v2 import MarketOrderArgs, OrderType, PartialCreateOrderOptions  # type: ignore[import-untyped]
            from py_clob_client_v2.order_builder.constants import BUY  # type: ignore[import-untyped]

            client = self._get_client()
            return client.create_and_post_market_order(  # type: ignore[attr-defined,no-any-return]
                order_args=MarketOrderArgs(
                    token_id=token_id,
                    side=BUY,
                    amount=amount,
                    price=price,
                ),
                options=PartialCreateOrderOptions(
                    tick_size=tick_size,
                    neg_risk=neg_risk,
                ),
                order_type=OrderType.FOK,
            )

        try:
            response = _retry_on_425(_place)
            self._daily_spent += amount
            log.info("market_order_placed", token_id=token_id, side="BUY", amount=amount)
            return response  # type: ignore[no-any-return]
        except Exception as e:
            log.error("market_order_failed", token_id=token_id, error=str(e))
            return None

    def cancel_order(self, order_id: str) -> dict | None:
        try:
            from py_clob_client_v2 import OrderPayload  # type: ignore[import-untyped]

            client = self._get_client()
            response = client.cancel_order(OrderPayload(orderID=order_id))  # type: ignore[attr-defined]
            log.info("order_cancelled", order_id=order_id)
            return response  # type: ignore[no-any-return]
        except Exception as e:
            log.error("cancel_failed", order_id=order_id, error=str(e))
            return None

    def cancel_all_orders(self) -> dict | None:
        try:
            client = self._get_client()
            response = client.cancel_all()  # type: ignore[attr-defined]
            log.info("all_orders_cancelled")
            return response  # type: ignore[no-any-return]
        except Exception as e:
            log.error("cancel_all_failed", error=str(e))
            return None

    def get_open_orders(self, market: str | None = None) -> list[dict]:
        try:
            from py_clob_client_v2 import OpenOrderParams  # type: ignore[import-untyped]

            client = self._get_client()
            params = OpenOrderParams(market=market) if market else None
            orders = client.get_open_orders(params)  # type: ignore[attr-defined]
            return orders if isinstance(orders, list) else []  # type: ignore[no-any-return]
        except Exception as e:
            log.warning("get_orders_failed", error=str(e))
            return []

    def get_order_status(self, order_id: str) -> dict | None:
        try:
            client = self._get_client()
            return client.get_order(order_id)  # type: ignore[attr-defined,no-any-return]
        except Exception as e:
            log.error("get_order_failed", order_id=order_id, error=str(e))
            return None

    def get_trades(self, market: str | None = None) -> list[dict]:
        try:
            from py_clob_client_v2 import TradeParams  # type: ignore[import-untyped]

            client = self._get_client()
            params = TradeParams(market=market) if market else None
            trades = client.get_trades(params)  # type: ignore[attr-defined]
            return trades if isinstance(trades, list) else []  # type: ignore[no-any-return]
        except Exception as e:
            log.error("get_trades_failed", error=str(e))
            return []

    def is_neg_risk_market(self, token_id: str) -> bool:
        try:
            client = self._get_client()
            result = client.get_neg_risk(token_id)  # type: ignore[attr-defined]
            return bool(result)
        except Exception as e:
            log.warning("neg_risk_check_failed", token_id=token_id, error=str(e))
            return True

    def fetch_market_fee_rate_bps(self, condition_id: str) -> int | None:
        try:
            import httpx

            resp = httpx.get(
                f"{CLOB_HOST}/markets/{condition_id}",
                timeout=DEFAULT_HTTP_TIMEOUT,
            )
            resp.raise_for_status()
            data = resp.json()
            rate = data.get("feeRateBps")
            if rate is not None:
                return int(rate)
        except Exception as e:
            log.warning("fetch_fee_failed", condition_id=condition_id, error=str(e))
        return None

    def start_heartbeat(self) -> None:
        self._running = True
        self._heartbeat_thread = threading.Thread(target=self._heartbeat_loop, daemon=True)
        self._heartbeat_thread.start()
        log.info("heartbeat_started")

    def stop_heartbeat(self) -> None:
        self._running = False
        if self._heartbeat_thread:
            self._heartbeat_thread.join(timeout=3)
        log.info("heartbeat_stopped")

    def _heartbeat_loop(self) -> None:
        consecutive_errors = 0
        while self._running:
            try:
                client = self._get_client()
                resp = client.post_heartbeat(self._heartbeat_id)  # type: ignore[attr-defined]
                if isinstance(resp, dict) and "heartbeat_id" in resp:
                    self._heartbeat_id = resp["heartbeat_id"]
                    consecutive_errors = 0
            except Exception as e:
                consecutive_errors += 1
                log.warning("heartbeat_error", error=str(e), consecutive=consecutive_errors)
                if consecutive_errors >= 3:
                    self._heartbeat_id = self._recover_heartbeat_id()
                    consecutive_errors = 0
            time.sleep(5)

    @property
    def daily_spent(self) -> float:
        return self._daily_spent

    def is_configured(self) -> bool:
        pk = get_private_key()
        creds = get_clob_creds(self._config)
        return bool(pk and creds["api_key"])

    def get_redeemable_positions(self) -> list[dict]:
        pk = get_private_key()
        if not pk:
            return []
        try:
            from web3 import Web3

            wallet = Web3().eth.account.from_key(pk).address
        except Exception:
            return []
        try:
            resp = httpx.get(
                GAMMA_POSITIONS_URL,
                params={"user": wallet, "redeemable": "true"},
                timeout=DEFAULT_HTTP_TIMEOUT,
            )
            resp.raise_for_status()
            positions = resp.json()
            return positions if isinstance(positions, list) else []
        except Exception as e:
            log.error("fetch_redeemable_failed", error=str(e))
            return []

    def settle_resolved(self, condition_ids: list[str] | None = None) -> dict:
        pk = get_private_key()
        if not pk:
            log.error("settle_no_key")
            return {"redeemed": 0, "errors": ["no private key"]}

        try:
            from poly_web3 import PolyWeb3Service
        except ImportError:
            log.error("poly_web3_not_installed")
            return {"redeemed": 0, "errors": ["poly-web3 not installed"]}

        client = self._get_client()
        relayer = None
        try:
            from py_builder_relayer_client import RelayClient

            relayer = RelayClient()
        except Exception:
            pass

        try:
            svc = PolyWeb3Service(clob_client=client, relayer_client=relayer)
        except Exception as e:
            log.error("web3_service_init_failed", error=str(e))
            return {"redeemed": 0, "errors": [str(e)]}

        if condition_ids:
            try:
                result = svc.redeem(condition_ids=condition_ids)
                redeemed = len(result.success) if hasattr(result, "success") else 0
                errors = [str(e) for e in result.errors] if hasattr(result, "errors") else []
                log.info("settle_specific", condition_ids=condition_ids, redeemed=redeemed)
                return {"redeemed": redeemed, "errors": errors}
            except Exception as e:
                log.error("settle_specific_failed", error=str(e))
                return {"redeemed": 0, "errors": [str(e)]}
        else:
            try:
                result = svc.redeem_all()
                redeemed = len(result.success) if hasattr(result, "success") else 0
                errors = [str(e) for e in result.errors] if hasattr(result, "errors") else []
                log.info("settle_all", redeemed=redeemed)
                return {"redeemed": redeemed, "errors": errors}
            except Exception as e:
                log.error("settle_all_failed", error=str(e))
                return {"redeemed": 0, "errors": [str(e)]}

    def merge_positions(self, condition_id: str, amount: float) -> dict | None:
        pk = get_private_key()
        if not pk:
            log.error("merge_no_key")
            return None

        try:
            from poly_web3 import PolyWeb3Service

            client = self._get_client()
            relayer = None
            try:
                from py_builder_relayer_client import RelayClient

                relayer = RelayClient()
            except Exception:
                pass
            svc = PolyWeb3Service(clob_client=client, relayer_client=relayer)
            return svc.merge(condition_id, amount)  # type: ignore[no-any-return]
        except Exception as e:
            log.error("merge_failed", condition_id=condition_id, error=str(e))
            return None
