# Meme Monitor 🐸

多链 meme coin 早期发现监控系统，通过 Dexscreener 获取新币数据，Telegram Bot 推送通知。

## 支持链
- Solana (SOL)
- Base
- BSC

## 快速开始

### 方式一：uvx 直接运行（推荐）

```bash
# 首次配置
uvx --from git+https://github.com/Xeron2000/meme-monitor meme-monitor-setup

# 运行监控
uvx --from git+https://github.com/Xeron2000/meme-monitor meme-monitor
```

### 方式二：pip 安装

```bash
pip install git+https://github.com/Xeron2000/meme-monitor

# 首次配置
meme-monitor-setup

# 运行监控
meme-monitor
```

### 方式三：本地开发

```bash
git clone https://github.com/Xeron2000/meme-monitor
cd meme-monitor

# 安装依赖
uv sync

# 配置
cp config.example.yaml config.yaml
# 编辑 .env 填入 TG_BOT_TOKEN 和 TG_CHAT_ID

# 运行
uv run meme-monitor
```

## 配置说明

配置文件位置：`~/.config/meme-monitor/`

### Telegram Bot 配置

1. 打开 Telegram，搜索 @BotFather
2. 发送 `/newbot` 创建新 bot
3. 复制 bot token

获取 Chat ID：
1. 搜索 @userinfobot
2. 发送任意消息，复制返回的 ID

### 过滤条件

编辑 `config.yaml`：

```yaml
filters:
  min_liquidity: 5000        # 最小流动性 $5k
  max_liquidity: 100000      # 最大流动性 $100k
  min_market_cap: 10000      # 最小市值 $10k
  max_market_cap: 5000000    # 最大市值 $5M
  max_age_minutes: 1440      # 最大币龄 24 小时
  reject_honeypot: true      # 拒绝蜜罐
```

## 数据源

- [Dexscreener](https://dexscreener.com/) - 新币发现 + 详情

## License

MIT
