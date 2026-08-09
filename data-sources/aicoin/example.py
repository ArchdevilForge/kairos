#!/usr/bin/env python3
"""AiCoin API usage examples."""

from aicoin_api import *

print("=== Global Indices ===")
for idx in global_index()[:3]:
    print(f"  {idx['index_name']:<20} {idx['last']:<10} {idx['degree']}%")

print("\n=== BTC/USDT Detail ===")
tp = tp_detail('btcusdt:binance')[0]
print(f"  Pair: {tp['detail']['en_name']} @ {tp['detail']['market_en']}")
ts, o, h, l, c, v = tp['open']['open']
print(f"  24h: O={o} H={h} L={l} C={c} V={v}")

print("\n=== BTC Technical Signals ===")
ss = side_summary('btcusdt:binance')
print(f"  Signal rate:     {ss['signal_rate']}  (0=bearish 1=bullish)")
print(f"  Net inflow:      {ss['main_net_inflow']} USD")
print(f"  L/S ratio:       {ss['btc_ls_ratio']}")
print(f"  L/S signal:      {ss['btc_ls_signal']}  (1=bullish -1=bearish)")
print(f"  24h liquidation: {ss['liq']}")

print("\n=== News ===")
news = hot_news()
nf = news['latest_newsflash']
print(f"  {nf['title']}")
print(f"  👍{nf['up_count']} 👎{nf['down_count']}")

print("\n=== Search ETH ===")
r = search_key('ETH')
print(f"  Key: {r['data']['key']}")

print("\n=== Geolocation ===")
print(f"  {geoip()}")
