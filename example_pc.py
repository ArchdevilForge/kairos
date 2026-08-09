#!/usr/bin/env python3
"""PC encrypted API example (needs account)."""
import os
from aicoin_api.pc_client import login, Session, timestamp_plain

print("server time", timestamp_plain())
print("bootstrap pubkey v", Session.bootstrap().version)

acc = os.environ.get("AICOIN_ACCOUNT")
pwd = os.environ.get("AICOIN_PASSWORD")
token = os.environ.get("AICOIN_TOKEN")
if token:
    s = Session.bootstrap(); s.token = token
elif acc and pwd:
    s, user = login(acc, pwd)
    print("login", user.get("uid"), user.get("nick_name"))
else:
    print("set AICOIN_TOKEN or AICOIN_ACCOUNT+AICOIN_PASSWORD to continue")
    raise SystemExit(0)

r = s.post("upgrade/bottom/hotCoins", {})
print("hotCoins", r.get("success"), (r.get("data") or {}).get("list", [])[:3])
r = s.post("v2/transaction/index", {
    "symbol": "btcusdt:binance", "data_type": "bill",
    "open_time": 24, "currency": "cny", "size": 5,
})
print("depth", r.get("success"), list((r.get("data") or {}).keys())[:8])
