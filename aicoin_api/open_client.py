#!/usr/bin/env python3
"""AiCoin Open API client — reversed from docs.aicoin.com + blackbox probes.

Auth string (ONLY these 3 fields are signed, business params are not):
  AccessKeyId={id}&SignatureNonce={nonce}&Timestamp={ts}
Signature = Base64( hex( HMAC-SHA1(key=accessSecret, msg=auth_string) ) )
  note: base64 is over the ASCII hex string, NOT raw digest bytes.

Test vector (from official docs, keys are demo-only / invalid live):
  ak=975988f45090561684b7d8f4e45b85c2
  sk=957f23f2d6435e37d4ac21f3e9a67d45
  nonce=2 ts=1612149637
  sig=M2Y0ODNlYTUwNDFiMTg5MjRmMGQxNmY1YTMyMzc1NTc5NTUzNDAzYw==
"""
from __future__ import annotations

import base64
import hashlib
import hmac
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from typing import Any

BASE_V2 = "https://open.aicoin.com/api/v2"
BASE_UPGRADE = "https://open.aicoin.com/api/upgrade/v2"
# docs also mention openapi.aicoin.com for some upgrade paths
BASE_OPENAPI = "https://openapi.aicoin.com/api/upgrade/v2"


def generate_signature(
    access_key_id: str,
    access_secret: str,
    signature_nonce: str | None = None,
    timestamp: int | str | None = None,
) -> tuple[str, str, str]:
    """Return (signature, nonce, timestamp_str)."""
    nonce = signature_nonce or uuid.uuid4().hex[:8]
    ts = str(int(time.time()) if timestamp is None else int(timestamp))
    plain = f"AccessKeyId={access_key_id}&SignatureNonce={nonce}&Timestamp={ts}"
    digest = hmac.new(access_secret.encode(), plain.encode(), hashlib.sha1).digest()
    # official: hex encode digest, then base64 the hex *string*
    sig = base64.b64encode(digest.hex().encode("ascii")).decode("ascii")
    return sig, nonce, ts


def auth_params(access_key_id: str, access_secret: str) -> dict[str, str]:
    sig, nonce, ts = generate_signature(access_key_id, access_secret)
    return {
        "AccessKeyId": access_key_id,
        "SignatureNonce": nonce,
        "Timestamp": ts,
        "Signature": sig,
    }


class AiCoinOpenClient:
    def __init__(
        self,
        access_key_id: str,
        access_secret: str,
        *,
        timeout: float = 20.0,
        user_agent: str = "aicoin-re-client/1.0",
    ):
        self.ak = access_key_id
        self.sk = access_secret
        self.timeout = timeout
        self.ua = user_agent

    def get(self, url: str, **params: Any) -> dict[str, Any]:
        q = {**auth_params(self.ak, self.sk), **{k: str(v) for k, v in params.items()}}
        full = url + ("&" if "?" in url else "?") + urllib.parse.urlencode(q)
        req = urllib.request.Request(
            full,
            headers={"User-Agent": self.ua, "Accept": "application/json"},
            method="GET",
        )
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                return json.loads(resp.read().decode())
        except urllib.error.HTTPError as e:
            body = e.read().decode(errors="replace")
            try:
                return json.loads(body)
            except json.JSONDecodeError:
                return {"success": False, "httpStatus": e.code, "raw": body[:500]}

    def coin_list(self) -> dict[str, Any]:
        return self.get(f"{BASE_V2}/coin")

    def coin_ticker(self, coin_list: str = "bitcoin") -> dict[str, Any]:
        return self.get(f"{BASE_V2}/coin/ticker", coin_list=coin_list)

    def coin_search(self, search: str = "BTC", page: int = 1, page_size: int = 10) -> dict[str, Any]:
        # docs show openapi host for this path
        return self.get(
            f"{BASE_OPENAPI}/coin/search",
            search=search,
            page=page,
            page_size=page_size,
        )


def _selfcheck() -> None:
    ak = "975988f45090561684b7d8f4e45b85c2"
    sk = "957f23f2d6435e37d4ac21f3e9a67d45"
    sig, _, _ = generate_signature(ak, sk, "2", 1612149637)
    expect = "M2Y0ODNlYTUwNDFiMTg5MjRmMGQxNmY1YTMyMzc1NTc5NTUzNDAzYw=="
    assert sig == expect, f"sig mismatch: {sig}"
    print("selfcheck OK", sig)


if __name__ == "__main__":
    if len(sys.argv) == 1 or sys.argv[1] in {"-h", "--help"}:
        print("usage: aicoin_open_client.py selfcheck | ticker [coin] | coin")
        print("env: AICOIN_AK / AICOIN_SK")
        sys.exit(0)
    if sys.argv[1] == "selfcheck":
        _selfcheck()
        sys.exit(0)
    ak = os.environ.get("AICOIN_AK", "")
    sk = os.environ.get("AICOIN_SK", "")
    if not ak or not sk:
        print("set AICOIN_AK and AICOIN_SK", file=sys.stderr)
        sys.exit(2)
    c = AiCoinOpenClient(ak, sk)
    if sys.argv[1] == "ticker":
        print(json.dumps(c.coin_ticker(sys.argv[2] if len(sys.argv) > 2 else "bitcoin"), ensure_ascii=False, indent=2))
    elif sys.argv[1] == "coin":
        print(json.dumps(c.coin_list(), ensure_ascii=False, indent=2)[:2000])
    else:
        print("unknown cmd", sys.argv[1])
        sys.exit(2)
