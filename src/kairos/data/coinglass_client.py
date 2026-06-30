"""CoinGlass encrypted API client — minimal: fetch+decrypt and symbol normalization.

Decryption algorithm from github.com/ArchdevilForge/coinglass-decrypt.
"""

from __future__ import annotations

import base64
import gzip
import json
import time
from typing import Any
from urllib.parse import urlparse

import httpx
from cryptography.hazmat.primitives import padding
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes

COINGLASS_BASE = "https://capi.coinglass.com"
_KEY_TABLE = {"55": "170b070da9654622", "66": "d6537d845a964081", "77": "863f08689c97435b"}


class CoinGlassError(RuntimeError):
    """Base for CoinGlass client failures."""


class CoinGlassAPIError(CoinGlassError):
    """CoinGlass returned an application-level error."""


class CoinGlassDecodeError(CoinGlassError):
    """Decryption or parsing failed."""


def decrypt_coinglass_response(encrypted_body: str | bytes, user_token_b64: str, v: str, url: str = "") -> Any:
    """Decrypt an encrypted CoinGlass response body into Python objects."""
    try:
        outer = json.loads(encrypted_body.decode() if isinstance(encrypted_body, bytes) else encrypted_body)
        payload = base64.b64decode(outer["data"])
        token = base64.b64decode(user_token_b64)

        parsed = urlparse(url)
        key0 = base64.b64encode((parsed.path or url.split("?", 1)[0]).encode()).decode()[:16] if v == "1" else ""
        if not key0:
            const = _KEY_TABLE.get(v)
            if const is None:
                raise ValueError(f"Unknown CoinGlass encryption version: {v}")
            key0 = base64.b64encode(const.encode()).decode()[:16]

        step1 = _aes_decrypt(token, key0.encode())
        actual_key = gzip.decompress(step1).decode()
        step2 = _aes_decrypt(payload, actual_key.encode())
        return json.loads(gzip.decompress(step2).decode())
    except (KeyError, ValueError, json.JSONDecodeError, OSError) as exc:
        raise CoinGlassDecodeError(f"Cannot decrypt CoinGlass response: {exc}") from exc


def fetch_coinglass_endpoint(path: str, params: dict | None = None, *, timeout: float = 10.0) -> Any:
    """Fetch a CoinGlass endpoint and decrypt when encrypted headers are present."""
    url = f"{COINGLASS_BASE}/{path.lstrip('/')}" if not path.startswith("http") else path
    headers = {
        "Accept": "application/json, text/plain, */*",
        "cache-ts-v2": str(int(time.time() * 1000)),
        "encryption": "true",
        "language": "en",
        "Origin": "https://www.coinglass.com",
        "Referer": "https://www.coinglass.com",
        "User-Agent": "Mozilla/5.0 (X11; Linux x86_64) Chrome/125.0.0.0 Safari/537.36",
    }
    try:
        resp = httpx.get(url, params=params or {}, headers=headers, timeout=timeout)
        resp.raise_for_status()
    except httpx.HTTPError as exc:
        raise CoinGlassAPIError(f"CoinGlass request failed for {path}: {exc}") from exc

    user = resp.headers.get("user")
    version = resp.headers.get("v")
    if user and version:
        return decrypt_coinglass_response(resp.content, user, version, str(resp.url))

    try:
        payload = resp.json()
    except json.JSONDecodeError as exc:
        raise CoinGlassAPIError(f"CoinGlass returned non-JSON for {path}") from exc
    if isinstance(payload, dict) and payload.get("data") is not None and len(payload) <= 4:
        return payload["data"]
    return payload


def normalize_coin_symbol(symbol: str) -> str:
    """Normalize exchange symbols to a CoinGlass base symbol (e.g. BTC)."""
    value = str(symbol or "").upper().strip()
    if not value:
        raise CoinGlassError("symbol is required")
    value = value.split(":", 1)[0]
    for sep in ("/", "-", "_"):
        if sep in value:
            value = value.split(sep, 1)[0]
            break
    for suffix in ("USDT", "USDC", "USD", "PERP"):
        if value.endswith(suffix) and len(value) > len(suffix):
            return value[: -len(suffix)]
    return value


def _aes_decrypt(ciphertext: bytes, key: bytes) -> bytes:
    cipher = Cipher(algorithms.AES(key), modes.ECB())
    decryptor = cipher.decryptor()
    padded = decryptor.update(ciphertext) + decryptor.finalize()
    unpadder = padding.PKCS7(algorithms.AES.block_size).unpadder()
    return unpadder.update(padded) + unpadder.finalize()
