新建.env文件，然后填写主网和测试网的API密钥

```bash
pip install argparse apscheduler pytz python-dotenv colorama requests
```

1. **参数模式**：
```bash
# 立即下单
python trader.py --testnet --symbol BTCUSDT --side BUY --quantity 0.01 --price 50000

# 定时下单（上海时间18:30）
python trader.py --symbol ETHUSDT --side SELL --quantity 1 --price 3000 --schedule_time 18:30

# 查看账户余额
python trader.py --show_balance
```

2. **交互模式**：
```bash
python trader.py
```