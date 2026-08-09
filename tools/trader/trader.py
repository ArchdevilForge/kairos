import argparse
import requests
import time
import hmac
import hashlib
import json
from os import environ
from dotenv import load_dotenv
from colorama import Fore, Style, init
from apscheduler.schedulers.background import BackgroundScheduler
import pytz
from datetime import datetime, timedelta

# 加载环境变量
load_dotenv()

# 初始化颜色输出
init(autoreset=True)

class BinanceTrader:
    def __init__(self, testnet=False):
        self.testnet = testnet
        self.base_url = "https://testnet.binance.vision" if testnet else "https://api.binance.com"
        self.api_key = environ.get("TESTNET_API_KEY" if testnet else "BINANCE_API_KEY")
        self.secret_key = environ.get("TESTNET_SECRET_KEY" if testnet else "BINANCE_SECRET_KEY")
        self.scheduler = BackgroundScheduler()
        self.scheduler.start()

    def _generate_signature(self, params):
        query_string = "&".join([f"{k}={v}" for k, v in params.items()])
        return hmac.new(
            self.secret_key.encode("utf-8"),
            query_string.encode("utf-8"),
            hashlib.sha256
        ).hexdigest()

    def _print_header(self, text):
        print(f"\n{Fore.CYAN}{'='*40}")
        print(f"{text.center(40)}")
        print(f"{'='*40}{Style.RESET_ALL}")

    def get_account_info(self):
        endpoint = "/api/v3/account"
        params = {"timestamp": int(time.time() * 1000)}
        params["signature"] = self._generate_signature(params)
        
        try:
            response = requests.get(
                self.base_url + endpoint,
                headers={"X-MBX-APIKEY": self.api_key},
                params=params
            )
            return response.json()
        except Exception as e:
            print(f"{Fore.RED}获取账户信息失败: {e}{Style.RESET_ALL}")
            return None

    def place_limit_order(self, symbol, side, quantity, price):
        self._print_header("正在提交限价单")
        endpoint = "/api/v3/order"
        params = {
            "symbol": symbol.upper(),
            "side": side.upper(),
            "type": "LIMIT",
            "timeInForce": "GTC",
            "quantity": quantity,
            "price": price,
            "timestamp": int(time.time() * 1000)
        }

        params["signature"] = self._generate_signature(params)

        try:
            response = requests.post(
                self.base_url + endpoint,
                headers={"X-MBX-APIKEY": self.api_key},
                params=params
            )
            response.raise_for_status()
            return response.json()
        except requests.exceptions.RequestException as e:
            error_msg = json.loads(e.response.text)['msg'] if e.response else str(e)
            print(f"{Fore.RED}订单提交失败: {error_msg}{Style.RESET_ALL}")
            return None

    def schedule_order(self, symbol, side, quantity, price, target_time):
        """定时下单功能"""
        def job():
            current_time = datetime.now(pytz.timezone('Asia/Shanghai')).strftime('%Y-%m-%d %H:%M:%S')
            self._print_header(f"定时订单触发 {current_time}")
            result = self.place_limit_order(symbol, side, quantity, price)
            if result:
                print(f"{Fore.GREEN}订单创建成功！订单ID: {result['orderId']}{Style.RESET_ALL}")
            else:
                print(f"{Fore.RED}定时下单失败{Style.RESET_ALL}")

        self.scheduler.add_job(
            job,
            'date',
            run_date=target_time.astimezone(pytz.utc)
        )
        return target_time

def parse_time_input(time_str):
    """解析时间字符串"""
    shanghai_tz = pytz.timezone('Asia/Shanghai')
    try:
        hour, minute = map(int, time_str.split(':'))
        now = datetime.now(shanghai_tz)
        target_time = shanghai_tz.localize(
            datetime(now.year, now.month, now.day, hour, minute)
        )
        if target_time < now:
            target_time += timedelta(days=1)
        return target_time
    except ValueError:
        raise argparse.ArgumentTypeError("时间格式错误，请使用 HH:MM 格式")

def print_balance(account):
    """美化显示账户余额"""
    print(f"\n{Fore.GREEN}【账户资产概览】")
    print(f"{'-'*30}")
    for balance in account['balances']:
        free = float(balance['free'])
        if free > 0:
            print(f"{balance['asset']:>6}: {Fore.YELLOW}{free:<15.8f}{Style.RESET_ALL}")
    print(f"{'-'*30}")

def get_valid_time_input():
    """获取有效的时间输入"""
    shanghai_tz = pytz.timezone('Asia/Shanghai')
    while True:
        time_input = input(f"{Fore.CYAN}请输入挂单时间（上海时间，格式HH:MM，例如18:00）: {Style.RESET_ALL}").strip()
        try:
            hour, minute = map(int, time_input.split(':'))
            now = datetime.now(shanghai_tz)
            target_time = shanghai_tz.localize(
                datetime(now.year, now.month, now.day, hour, minute)
            )
            if target_time < now:
                target_time += timedelta(days=1)
            return target_time
        except ValueError:
            print(f"{Fore.RED}时间格式错误，请使用HH:MM格式{Style.RESET_ALL}")

def interactive_trading():
    print(f"\n{Fore.YELLOW}=== 币安智能交易终端 ==={Style.RESET_ALL}")
    
    # 选择网络
    network = input(f"{Fore.CYAN}选择交易网络 (1-主网 2-测试网): {Style.RESET_ALL}").strip()
    trader = BinanceTrader(testnet=(network == "2"))
    
    # 显示账户余额
    account = trader.get_account_info()
    if account:
        print_balance(account)
    
    # 订单参数输入
    trader._print_header("订单参数输入")
    symbol = input(f"{Fore.CYAN}输入交易对 (例如BTCUSDT): {Style.RESET_ALL}").upper()
    side = input(f"{Fore.CYAN}交易方向 (BUY/SELL): {Style.RESET_ALL}").upper()
    quantity = float(input(f"{Fore.CYAN}交易数量: {Style.RESET_ALL}"))
    price = float(input(f"{Fore.CYAN}限价单价格: {Style.RESET_ALL}"))
    
    # 定时功能
    use_schedule = input(f"{Fore.CYAN}是否定时挂单？(Y/N): {Style.RESET_ALL}").upper()
    if use_schedule == "Y":
        target_time = get_valid_time_input()
        confirm = input(f"\n{Fore.YELLOW}确认在 {target_time.strftime('%Y-%m-%d %H:%M:%S')} 上海时间提交订单？(Y/N): {Style.RESET_ALL}").upper()
        if confirm == "Y":
            scheduled_time = trader.schedule_order(symbol, side, quantity, price, target_time)
            print(f"\n{Fore.GREEN}定时任务已设置！计划执行时间: {scheduled_time.strftime('%Y-%m-%d %H:%M:%S')}（上海时间）{Style.RESET_ALL}")
            
            # 显示倒计时
            try:
                while True:
                    now = datetime.now(pytz.timezone('Asia/Shanghai'))
                    remaining = scheduled_time - now
                    if remaining.total_seconds() <= 0:
                        break
                    print(f"\r{Fore.MAGENTA}剩余等待时间: {str(remaining).split('.')[0]}", end="")
                    time.sleep(1)
                print()
            except KeyboardInterrupt:
                print(f"{Fore.YELLOW}\n程序已退出，定时任务可能尚未执行{Style.RESET_ALL}")
                return
        else:
            print(f"{Fore.YELLOW}已取消定时订单设置{Style.RESET_ALL}")
            return
    else:
        # 立即下单
        confirm = input(f"\n{Fore.YELLOW}确认立即提交订单？(Y/N): {Style.RESET_ALL}").upper()
        if confirm == "Y":
            result = trader.place_limit_order(symbol, side, quantity, price)
            if result:
                print(f"\n{Fore.GREEN}订单创建成功！")
                print(json.dumps(result, indent=2))
                print(f"订单ID: {result['orderId']}{Style.RESET_ALL}")
            else:
                print(f"{Fore.RED}订单创建失败{Style.RESET_ALL}")
        else:
            print(f"{Fore.YELLOW}已取消订单提交{Style.RESET_ALL}")

def main(args):
    trader = BinanceTrader(testnet=args.testnet)

    # 显示账户余额
    if args.show_balance:
        account = trader.get_account_info()
        if account:
            print(f"\n{Fore.GREEN}【账户资产概览】")
            print(f"{'-'*30}")
            for balance in account['balances']:
                free = float(balance['free'])
                if free > 0:
                    print(f"{balance['asset']:>6}: {Fore.YELLOW}{free:<15.8f}{Style.RESET_ALL}")
            print(f"{'-'*30}")

    # 下单逻辑
    if args.symbol and args.side and args.quantity and args.price:
        if args.schedule_time:
            target_time = parse_time_input(args.schedule_time)
            print(f"{Fore.YELLOW}设置定时订单于 {target_time.strftime('%Y-%m-%d %H:%M:%S')}（上海时间）")
            scheduled_time = trader.schedule_order(
                args.symbol,
                args.side.upper(),
                float(args.quantity),
                float(args.price),
                target_time
            )
            
            # 显示倒计时
            try:
                while True:
                    now = datetime.now(pytz.timezone('Asia/Shanghai'))
                    remaining = scheduled_time - now
                    if remaining.total_seconds() <= 0:
                        break
                    print(f"\r{Fore.MAGENTA}剩余等待时间: {str(remaining).split('.')[0]}", end="")
                    time.sleep(1)
                print()
            except KeyboardInterrupt:
                print(f"{Fore.YELLOW}\n程序已退出，定时任务可能尚未执行{Style.RESET_ALL}")
        else:
            result = trader.place_limit_order(
                args.symbol,
                args.side.upper(),
                float(args.quantity),
                float(args.price)
            )
            if result:
                print(f"\n{Fore.GREEN}订单创建成功！")
                print(json.dumps(result, indent=2))
                print(f"订单ID: {result['orderId']}{Style.RESET_ALL}")
            else:
                print(f"{Fore.RED}订单创建失败{Style.RESET_ALL}")

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description='币安命令行交易工具')
    parser.add_argument('--testnet', action='store_true', help='使用测试网络')
    parser.add_argument('--symbol', type=str, help='交易对符号，例如 BTCUSDT')
    parser.add_argument('--side', type=str, choices=['BUY', 'SELL'], help='交易方向')
    parser.add_argument('--quantity', type=float, help='交易数量')
    parser.add_argument('--price', type=float, help='限价单价格')
    parser.add_argument('--schedule_time', type=str, 
                      help='定时执行时间（上海时间格式 HH:MM）')
    parser.add_argument('--show_balance', action='store_true', 
                      help='显示账户余额')

    args = parser.parse_args()

    if not any(vars(args).values()):
        interactive_trading()
    else:
        main(args)