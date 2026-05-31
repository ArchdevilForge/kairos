"""Trade executor - handles order execution across exchanges."""

import logging
from dataclasses import dataclass, field
from enum import Enum
from typing import Optional

import ccxt

from kairos.utils.error_handler import ErrorSeverity, error_handler


class OrderSide(str, Enum):
    BUY = "buy"
    SELL = "sell"


class OrderType(str, Enum):
    MARKET = "market"
    LIMIT = "limit"
    STOP = "stop"
    STOP_LIMIT = "stop_limit"


class PositionSide(str, Enum):
    LONG = "long"
    SHORT = "short"


@dataclass
class Order:
    """Represents a trade order."""
    symbol: str
    side: OrderSide
    order_type: OrderType
    amount: float
    price: Optional[float] = None
    stop_price: Optional[float] = None
    position_side: PositionSide = PositionSide.LONG
    leverage: int = 1
    reduce_only: bool = False
    params: dict = field(default_factory=dict)


@dataclass
class OrderResult:
    """Result of order execution."""
    success: bool
    order_id: Optional[str] = None
    filled_price: Optional[float] = None
    filled_amount: Optional[float] = None
    fee: Optional[float] = None
    error: Optional[str] = None
    raw_response: dict = field(default_factory=dict)


class TradeExecutor:
    """Executes trades on exchanges via ccxt."""

    def __init__(self, exchange_name: str, config: dict):
        self.exchange_name = exchange_name
        self.config = config
        self.logger = logging.getLogger("kairos.trades.executor")

        # Initialize exchange
        exchange_class = getattr(ccxt, exchange_name)
        self.exchange = exchange_class({
            "enableRateLimit": True,
            "apiKey": config.get("apiKey"),
            "secret": config.get("secret"),
            "password": config.get("password"),  # For OKX
        })

        # Set to testnet if configured
        if config.get("testnet", False):
            self.exchange.set_sandbox_mode(True)

        # Trading config
        self.default_leverage = config.get("defaultLeverage", 5)
        self.max_leverage = config.get("maxLeverage", 10)
        self.margin_mode = config.get("marginMode", "isolated")  # isolated or cross

    async def set_leverage(self, symbol: str, leverage: int) -> bool:
        """Set leverage for a symbol."""
        try:
            leverage = min(leverage, self.max_leverage)
            await self.exchange.set_leverage(leverage, symbol)
            self.logger.info(f"Set leverage {leverage}x for {symbol}")
            return True
        except Exception as e:
            self.logger.error(f"Failed to set leverage for {symbol}: {e}")
            return False

    async def set_margin_mode(self, symbol: str, mode: str = None) -> bool:
        """Set margin mode (isolated/cross)."""
        try:
            mode = mode or self.margin_mode
            await self.exchange.set_margin_mode(mode, symbol)
            self.logger.info(f"Set margin mode {mode} for {symbol}")
            return True
        except Exception as e:
            self.logger.error(f"Failed to set margin mode for {symbol}: {e}")
            return False

    async def execute_order(self, order: Order) -> OrderResult:
        """Execute a single order."""
        try:
            # Set leverage first
            await self.set_leverage(order.symbol, order.leverage)

            # Prepare order params
            params = order.params.copy()
            if order.position_side == PositionSide.SHORT:
                params["positionSide"] = "short"
            else:
                params["positionSide"] = "long"

            if order.reduce_only:
                params["reduceOnly"] = True

            # Execute based on order type
            if order.order_type == OrderType.MARKET:
                result = await self.exchange.create_order(
                    symbol=order.symbol,
                    type="market",
                    side=order.side.value,
                    amount=order.amount,
                    params=params
                )
            elif order.order_type == OrderType.LIMIT:
                if not order.price:
                    return OrderResult(success=False, error="Limit order requires price")
                result = await self.exchange.create_order(
                    symbol=order.symbol,
                    type="limit",
                    side=order.side.value,
                    amount=order.amount,
                    price=order.price,
                    params=params
                )
            elif order.order_type == OrderType.STOP:
                if not order.stop_price:
                    return OrderResult(success=False, error="Stop order requires stop_price")
                params["stopPrice"] = order.stop_price
                result = await self.exchange.create_order(
                    symbol=order.symbol,
                    type="market",
                    side=order.side.value,
                    amount=order.amount,
                    params=params
                )
            else:
                return OrderResult(success=False, error=f"Unsupported order type: {order.order_type}")

            # Parse result
            return OrderResult(
                success=True,
                order_id=result.get("id"),
                filled_price=result.get("average") or result.get("price"),
                filled_amount=result.get("filled") or result.get("amount"),
                fee=result.get("fee", {}).get("cost"),
                raw_response=result
            )

        except ccxt.InsufficientFunds as e:
            return OrderResult(success=False, error=f"Insufficient funds: {e}")
        except ccxt.InvalidOrder as e:
            return OrderResult(success=False, error=f"Invalid order: {e}")
        except ccxt.NetworkError as e:
            return OrderResult(success=False, error=f"Network error: {e}")
        except Exception as e:
            error_handler.handle_config_error(
                e,
                {"component": "TradeExecutor", "operation": "execute_order", "order": str(order)},
                ErrorSeverity.ERROR
            )
            return OrderResult(success=False, error=str(e))

    async def close_position(self, symbol: str, side: PositionSide, amount: float = None) -> OrderResult:
        """Close a position (full or partial)."""
        # Get current position
        positions = await self.get_positions()
        position = None
        for p in positions:
            if p["symbol"] == symbol and p["side"] == side.value:
                position = p
                break

        if not position:
            return OrderResult(success=False, error=f"No {side.value} position found for {symbol}")

        close_amount = amount or abs(position["contracts"])
        close_side = OrderSide.SELL if side == PositionSide.LONG else OrderSide.BUY

        order = Order(
            symbol=symbol,
            side=close_side,
            order_type=OrderType.MARKET,
            amount=close_amount,
            position_side=side,
            reduce_only=True
        )

        return await self.execute_order(order)

    async def get_positions(self) -> list[dict]:
        """Get all open positions."""
        try:
            positions = await self.exchange.fetch_positions()
            return [p for p in positions if abs(p.get("contracts", 0)) > 0]
        except Exception as e:
            self.logger.error(f"Failed to fetch positions: {e}")
            return []

    async def get_balance(self) -> dict:
        """Get account balance."""
        try:
            balance = await self.exchange.fetch_balance()
            return {
                "total": balance.get("total", {}),
                "free": balance.get("free", {}),
                "used": balance.get("used", {})
            }
        except Exception as e:
            self.logger.error(f"Failed to fetch balance: {e}")
            return {}

    async def get_ticker(self, symbol: str) -> dict:
        """Get current ticker for a symbol."""
        try:
            return await self.exchange.fetch_ticker(symbol)
        except Exception as e:
            self.logger.error(f"Failed to fetch ticker for {symbol}: {e}")
            return {}

    async def get_funding_rate(self, symbol: str) -> dict:
        """Get current funding rate for a symbol."""
        try:
            funding = await self.exchange.fetch_funding_rate(symbol)
            return {
                "symbol": symbol,
                "rate": funding.get("fundingRate"),
                "next_time": funding.get("fundingDatetime"),
                "timestamp": funding.get("timestamp")
            }
        except Exception as e:
            self.logger.error(f"Failed to fetch funding rate for {symbol}: {e}")
            return {}
