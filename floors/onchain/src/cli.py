"""CLI commands for meme-monitor."""
import os
import sys
from pathlib import Path

import structlog

logger = structlog.get_logger()

CONFIG_DIR = Path.home() / ".config" / "meme-monitor"
CONFIG_FILE = CONFIG_DIR / "config.yaml"
ENV_FILE = CONFIG_DIR / ".env"


def setup():
    """Interactive setup wizard."""
    print("\n🐸 Meme Monitor 配置向导\n")
    print("=" * 50)

    # Check if already configured
    if CONFIG_FILE.exists() and ENV_FILE.exists():
        response = input("已存在配置，是否覆盖? [y/N]: ").strip().lower()
        if response != 'y':
            print("取消配置")
            return

    # Create config directory
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)

    # ==================== Telegram 配置 ====================
    print("\n📱 Telegram 配置")
    print("-" * 50)
    print("1. 打开 Telegram，搜索 @BotFather")
    print("2. 发送 /newbot 创建新 bot")
    print("3. 复制 bot token")
    print()

    bot_token = input("Bot Token: ").strip()
    if not bot_token:
        print("❌ Token 不能为空")
        sys.exit(1)

    print()
    print("获取 Chat ID:")
    print("1. 打开 Telegram，搜索 @userinfobot")
    print("2. 发送任意消息，复制返回的 ID")
    print()

    chat_id = input("Chat ID: ").strip()
    if not chat_id:
        print("❌ Chat ID 不能为空")
        sys.exit(1)

    # ==================== 链配置 ====================
    print("\n⛓️ 监控链配置")
    print("-" * 50)
    print("支持的链: sol (Solana), base (Base), bsc (BSC)")
    print("多个链用逗号分隔，留空使用默认 [sol,base,bsc]")
    print()

    chains_input = input("监控链: ").strip()
    if chains_input:
        chains = [c.strip().lower() for c in chains_input.split(",")]
    else:
        chains = ["sol", "base", "bsc"]

    # ==================== 轮询配置 ====================
    print("\n⏱️ 轮询配置")
    print("-" * 50)

    interval = input("轮询间隔秒数 [30]: ").strip()
    interval = int(interval) if interval else 30

    # ==================== 过滤条件 ====================
    print("\n🔍 过滤条件")
    print("-" * 50)

    print("\n流动性过滤 (USD):")
    min_liq = input("  最小流动性 [5000]: ").strip()
    min_liq = float(min_liq) if min_liq else 5000

    max_liq = input("  最大流动性 [100000]: ").strip()
    max_liq = float(max_liq) if max_liq else 100000

    print("\n市值过滤 (USD):")
    min_mcap = input("  最小市值 [10000]: ").strip()
    min_mcap = float(min_mcap) if min_mcap else 10000

    max_mcap = input("  最大市值 [5000000]: ").strip()
    max_mcap = float(max_mcap) if max_mcap else 5000000

    print("\n币龄过滤:")
    max_age = input("  最大币龄(分钟) [1440] (1440=24小时): ").strip()
    max_age = int(max_age) if max_age else 1440

    print("\n聪明钱过滤:")
    min_sm = input("  最少聪明钱数量 [0]: ").strip()
    min_smart_money = int(min_sm) if min_sm else 0

    print("\n安全过滤:")
    reject_hp = input("  拒绝蜜罐? [Y/n]: ").strip().lower()
    reject_honeypot = reject_hp != 'n'

    # ==================== 生成配置 ====================
    # Write .env
    env_content = f"""# Telegram Bot 配置
TG_BOT_TOKEN={bot_token}
TG_CHAT_ID={chat_id}
"""
    ENV_FILE.write_text(env_content)
    print(f"\n✅ 已保存: {ENV_FILE}")

    # Write config.yaml
    chains_yaml = "\n".join(f"  - {c}" for c in chains)
    config_content = f"""# Meme Coin Monitor Configuration

telegram:
  bot_token: "${{TG_BOT_TOKEN}}"
  chat_id: "${{TG_CHAT_ID}}"

chains:
{chains_yaml}

polling:
  interval_seconds: {interval}

filters:
  min_liquidity: {min_liq}
  max_liquidity: {max_liq}
  min_market_cap: {min_mcap}
  max_market_cap: {max_mcap}
  max_age_minutes: {max_age}
  min_smart_money_count: {min_smart_money}
  require_lp_burned: false
  reject_honeypot: {str(reject_honeypot).lower()}

sources:
  dexscreener:
    enabled: true
"""
    CONFIG_FILE.write_text(config_content)
    print(f"✅ 已保存: {CONFIG_FILE}")

    # ==================== 显示配置摘要 ====================
    print("\n" + "=" * 50)
    print("📋 配置摘要")
    print("-" * 50)
    print(f"  监控链: {', '.join(chains)}")
    print(f"  轮询间隔: {interval}s")
    print(f"  流动性: ${min_liq:,.0f} - ${max_liq:,.0f}")
    print(f"  市值: ${min_mcap:,.0f} - ${max_mcap:,.0f}")
    print(f"  最大币龄: {max_age} 分钟 ({max_age/60:.1f} 小时)")
    print(f"  最少聪明钱: {min_smart_money}")
    print(f"  拒绝蜜罐: {'是' if reject_honeypot else '否'}")

    # ==================== 测试连接 ====================
    print("\n🔄 测试 Telegram 连接...")
    try:
        import asyncio
        from telegram import Bot

        async def test_bot():
            bot = Bot(token=bot_token)
            await bot.send_message(
                chat_id=chat_id,
                text="✅ Meme Monitor 配置成功！\n\n"
                     f"监控链: {', '.join(c.upper() for c in chains)}\n"
                     f"流动性: ${min_liq:,.0f} - ${max_liq:,.0f}\n"
                     f"市值: ${min_mcap:,.0f} - ${max_mcap:,.0f}\n"
                     f"最少聪明钱: {min_smart_money}"
            )
            await bot.shutdown()

        asyncio.run(test_bot())
        print("✅ Telegram 发送成功！请检查是否收到消息")
    except Exception as e:
        print(f"⚠️ Telegram 测试失败: {e}")
        print("请检查:")
        print("  1. Bot Token 是否正确")
        print("  2. Chat ID 是否正确")
        print("  3. 是否已向 bot 发送过消息")

    print("\n" + "=" * 50)
    print("🎉 配置完成！")
    print("\n运行监控:")
    print("  meme-monitor")
    print("  # 或")
    print("  uvx --from git+https://github.com/Xeron2000/meme-monitor meme-monitor")
    print()


def get_config_path() -> Path:
    """Get config file path, prefer local then global."""
    local = Path("config.yaml")
    if local.exists():
        return local
    if CONFIG_FILE.exists():
        return CONFIG_FILE
    return local  # Will trigger not found error


def get_env_path() -> Path:
    """Get .env file path."""
    local = Path(".env")
    if local.exists():
        return local
    if ENV_FILE.exists():
        return ENV_FILE
    return local
