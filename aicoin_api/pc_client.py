#!/usr/bin/env python3
"""AiCoin Windows PC client protocol (v2.16.10) — reversed from app.asar.

Transport: HTTPS POST https://apipc.aicoin.com/api/<path>
Body envelope (header compress:1):
  {
    "p":  Base64( AES-CBC-PKCS7( plaintext_json ) ),
    "k":  Base64( RSA-PKCS1v1.5( utf8(key_hex) ) ),
    "v":  public key version string from conn/load,
    "iv": Base64( RSA-PKCS1v1.5( utf8(iv_hex) ) )
  }

Key material (CryptoJS WordArray semantics):
  key_bytes = random(16); key_hex = key_bytes.hex()          # 32 hex chars
  iv_bytes  = random(8);  iv_hex  = iv_bytes.hex()           # 16 hex chars
  AES_key   = key_hex.encode('utf-8')                        # 32 bytes → AES-256
  AES_iv    = iv_bytes + b'\\x00'*8                          # 8-byte WA zero-extended to 16

RSA public key: POST /api/conn/load  body {}  (skipCrypt / plaintext JSON)
Open timestamp: GET/POST /api/server/timestamp-plain

Common plaintext fields injected by client interceptor:
  lan, pc_client, pc_client_version, token(optional)
"""
from __future__ import annotations

import base64
import json
import secrets
import ssl
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import padding
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes

API_BASE = "https://apipc.aicoin.com/api"
VIP_API_BASE = "https://vip-pcapi.aicoin.com/api"
TRADE_API_BASE = "https://trade.aicoin.com/api"
WS_STREAM = "ws://stream.pc.aicoin.com:8080"
DEFAULT_UA = "AiCoin/2.16.10"


def _pkcs7_pad(data: bytes, bs: int = 16) -> bytes:
    n = bs - (len(data) % bs)
    return data + bytes([n]) * n


def _pkcs7_unpad(data: bytes) -> bytes:
    n = data[-1]
    if n < 1 or n > 16 or data[-n:] != bytes([n]) * n:
        raise ValueError("bad pkcs7")
    return data[:-n]


@dataclass
class Session:
    pub_pem: str
    version: str
    token: str = ""
    lan: str = "cn"
    pc_client: str = "windows"
    pc_client_version: str = "2.16.10"
    timeout: float = 20.0

    def __post_init__(self) -> None:
        self._pub = serialization.load_pem_public_key(self.pub_pem.encode())
        self._ctx = ssl.create_default_context()

    @classmethod
    def bootstrap(cls, **kw: Any) -> "Session":
        raw = _http_json("POST", f"{API_BASE}/conn/load", b"{}", {
            "Content-Type": "application/json",
            "User-Agent": DEFAULT_UA,
            "compress": "0",
        })
        if not raw.get("success"):
            raise RuntimeError(f"conn/load failed: {raw}")
        return cls(pub_pem=raw["data"]["info"], version=str(raw["data"]["v"]), **kw)

    def _rsa(self, data: bytes) -> str:
        return base64.b64encode(
            self._pub.encrypt(data, padding.PKCS1v15())
        ).decode()

    def encrypt_body(self, obj: dict[str, Any]) -> tuple[dict[str, str], bytes, bytes]:
        """Return (envelope, aes_key_bytes_for_decrypt, aes_iv_bytes_for_decrypt)."""
        plain = json.dumps(obj, ensure_ascii=False, separators=(",", ":")).encode()
        key_raw = secrets.token_bytes(16)
        iv_raw = secrets.token_bytes(8)
        key_hex = key_raw.hex()
        iv_hex = iv_raw.hex()
        aes_key = key_hex.encode("utf-8")  # 32 bytes
        aes_iv = iv_raw + b"\x00" * 8
        ct = Cipher(algorithms.AES(aes_key), modes.CBC(aes_iv)).encryptor()
        p = base64.b64encode(ct.update(_pkcs7_pad(plain)) + ct.finalize()).decode()
        env = {
            "p": p,
            "k": self._rsa(key_hex.encode()),
            "v": self.version,
            "iv": self._rsa(iv_hex.encode()),
        }
        return env, aes_key, aes_iv

    def decrypt_field(self, blob: str, aes_key: bytes, aes_iv: bytes, *, gzip_mode: bool = True) -> Any:
        """AES-CBC decrypt. If response Decompress:1, plaintext is gzip(JSON)."""
        import gzip

        raw = base64.b64decode(blob)
        pt = Cipher(algorithms.AES(aes_key), modes.CBC(aes_iv)).decryptor()
        data = _pkcs7_unpad(pt.update(raw) + pt.finalize())
        if gzip_mode or data[:2] == b"\x1f\x8b":
            return json.loads(gzip.decompress(data).decode())
        return json.loads(data.decode())

    def post(self, path: str, data: dict[str, Any] | None = None, *, auth: bool = False) -> Any:
        body = {
            "lan": self.lan,
            "pc_client": self.pc_client,
            "pc_client_version": self.pc_client_version,
            **(data or {}),
        }
        if self.token and "token" not in body:
            body["token"] = self.token
        env, aes_key, aes_iv = self.encrypt_body(body)
        raw, resp_headers = _http_json(
            "POST",
            f"{API_BASE}/{path.lstrip('/')}",
            json.dumps(env).encode(),
            {
                "Content-Type": "application/json",
                "User-Agent": DEFAULT_UA,
                "Accept": "application/json",
                "compress": "1",
            },
            self._ctx,
            self.timeout,
            return_headers=True,
        )
        if isinstance(raw.get("data"), str) and raw.get("data"):
            gzip_mode = bool(resp_headers.get("decompress") or resp_headers.get("Decompress") or True)
            try:
                raw = {**raw, "data": self.decrypt_field(raw["data"], aes_key, aes_iv, gzip_mode=gzip_mode)}
            except Exception as e:
                raw = {**raw, "_decrypt_error": str(e)}
        return raw


def _http_json(
    method: str,
    url: str,
    data: bytes | None,
    headers: dict,
    ctx=None,
    timeout=20.0,
    return_headers: bool = False,
) -> Any:
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout, context=ctx or ssl.create_default_context()) as r:
            body = json.loads(r.read().decode())
            if return_headers:
                return body, dict(r.headers.items())
            return body
    except urllib.error.HTTPError as e:
        b = e.read().decode(errors="replace")
        try:
            body = json.loads(b)
        except json.JSONDecodeError:
            body = {"success": False, "httpStatus": e.code, "raw": b[:500]}
        if return_headers:
            return body, dict(e.headers.items()) if e.headers else {}
        return body


def timestamp_plain() -> int:
    r = _http_json("GET", f"{API_BASE}/server/timestamp-plain", None, {"User-Agent": DEFAULT_UA})
    return int(r["data"])


def login(
    account: str,
    password: str,
    *,
    version_info: str = "2.16.10",
    device_name: str = "AiCoin PC (Win)",
    os_info: str = "win32 x64",
    user_client_id: str | None = None,
) -> tuple[Session, dict[str, Any]]:
    """POST account/login (encrypted). Returns (session_with_token, user_data)."""
    import hashlib

    s = Session.bootstrap()
    mid = user_client_id or hashlib.md5(f"aicoin:{account}".encode()).hexdigest()[:20]
    r = s.post(
        "account/login",
        {
            "account": account,
            "pwd": password,
            "code": "",
            "os_info": os_info,
            "2fa_code": "",
            "captcha_key": "",
            "version_info": version_info,
            "user_client_id": mid,
            "device_name": device_name,
        },
    )
    if not r.get("success"):
        raise RuntimeError(f"login failed: {r.get('status')} {r.get('message')} {r.get('data')}")
    data = r["data"]
    s.token = data["token"]
    return s, data


def _selfcheck() -> None:
    ts = timestamp_plain()
    assert ts > 1_700_000_000, ts
    s = Session.bootstrap()
    assert "BEGIN PUBLIC KEY" in s.pub_pem
    r = s.post("upgrade/geoip", {})
    assert r.get("success") is True, r
    assert isinstance(r.get("data"), dict), r
    print("selfcheck OK")
    print("timestamp", ts)
    print("pubkey_v", s.version)
    print("geoip", json.dumps(r["data"], ensure_ascii=False)[:400])


if __name__ == "__main__":
    if len(sys.argv) < 2 or sys.argv[1] in {"-h", "--help"}:
        print("usage:")
        print("  aicoin_pc_client.py selfcheck")
        print("  aicoin_pc_client.py dump-key")
        print("  aicoin_pc_client.py login <account> <password>")
        print("  aicoin_pc_client.py call <path> [json_body]")
        print("env: AICOIN_TOKEN | AICOIN_ACCOUNT+AICOIN_PASSWORD")
        sys.exit(0)
    cmd = sys.argv[1]
    if cmd == "selfcheck":
        _selfcheck()
    elif cmd == "dump-key":
        s = Session.bootstrap()
        print(json.dumps({"v": s.version, "pem": s.pub_pem}, indent=2))
    elif cmd == "login":
        acc = sys.argv[2]
        pw = sys.argv[3]
        s, data = login(acc, pw)
        out = {
            "uid": data.get("uid"),
            "nick_name": data.get("nick_name"),
            "email": data.get("email"),
            "token": data.get("token"),
        }
        print(json.dumps(out, ensure_ascii=False, indent=2))
    elif cmd == "call":
        path = sys.argv[2]
        body = json.loads(sys.argv[3]) if len(sys.argv) > 3 else {}
        import os

        if os.environ.get("AICOIN_TOKEN"):
            s = Session.bootstrap()
            s.token = os.environ["AICOIN_TOKEN"]
        elif os.environ.get("AICOIN_ACCOUNT") and os.environ.get("AICOIN_PASSWORD"):
            s, _ = login(os.environ["AICOIN_ACCOUNT"], os.environ["AICOIN_PASSWORD"])
        else:
            s = Session.bootstrap()
        print(json.dumps(s.post(path, body), ensure_ascii=False, indent=2)[:8000])
    else:
        print("unknown", cmd)
        sys.exit(2)
