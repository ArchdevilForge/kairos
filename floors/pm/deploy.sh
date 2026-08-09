#!/bin/bash
# PM-Bot 部署脚本
# 用法: ./deploy.sh [服务器地址]

set -e

SERVER=${1:-"user@server"}
REPO="https://github.com/Xeron2000/pm-bot.git"
INSTALL_DIR="$HOME/pm-bot"

echo "=========================================="
echo "PM-Bot 部署脚本"
echo "=========================================="
echo "服务器: $SERVER"
echo "安装目录: $INSTALL_DIR"
echo ""

# SSH 到服务器执行部署
ssh "$SERVER" << 'EOF'
set -e

echo "1. 安装 uv..."
curl -LsSf https://astral.sh/uv/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"

echo "2. 克隆仓库..."
if [ -d "$HOME/pm-bot" ]; then
    cd "$HOME/pm-bot"
    git pull
else
    git clone https://github.com/Xeron2000/pm-bot.git "$HOME/pm-bot"
    cd "$HOME/pm-bot"
fi

echo "3. 安装依赖..."
uv sync

echo "4. 创建配置目录..."
mkdir -p ~/.pm-bot

echo "5. 创建配置文件..."
if [ ! -f config.toml ]; then
    cp config.template.toml config.toml
    echo "已创建 config.toml，请编辑配置"
fi

echo "6. 验证安装..."
uv run pm-bot-v2 --help

echo ""
echo "=========================================="
echo "部署完成！"
echo "=========================================="
echo ""
echo "下一步："
echo "1. 编辑配置: nano ~/pm-bot/config.toml"
echo "2. 设置环境变量:"
echo "   export PM_BOT_BANKROLL=100"
echo "   export PM_BOT_DRY_RUN=true"
echo "3. 启动纸面交易:"
echo "   cd ~/pm-bot && uv run pm-bot daemon start --dry-run --cities 'Chicago,Miami'"
echo ""
echo "或使用 pm-bot-v2:"
echo "   cd ~/pm-bot && uv run pm-bot-v2 wallet-list"
echo "   cd ~/pm-bot && uv run pm-bot-v2 wallet-scan"
EOF
