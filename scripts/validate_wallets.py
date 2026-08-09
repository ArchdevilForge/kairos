#!/usr/bin/env python3
"""验证 smart_wallets.json 中地址的格式和可用性"""

import json
import re
import sys
from pathlib import Path

# Base58 字符集 (Solana)
BASE58_ALPHABET = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"


def is_valid_solana_address(address: str) -> tuple[bool, str]:
    """验证 Solana 地址格式"""
    if not address:
        return False, "空地址"

    # 长度检查 (32-44 字符)
    if len(address) < 32 or len(address) > 44:
        return False, f"长度不正确: {len(address)} (应为 32-44)"

    # Base58 字符集检查
    for char in address:
        if char not in BASE58_ALPHABET:
            return False, f"非法字符: '{char}'"

    return True, "✓"


def is_valid_evm_address(address: str) -> tuple[bool, str]:
    """验证 EVM (Base/BSC) 地址格式"""
    if not address:
        return False, "空地址"

    # 必须以 0x 开头
    if not address.startswith("0x") and not address.startswith("0X"):
        return False, "缺少 0x 前缀"

    # 长度检查 (42 字符: 0x + 40 hex)
    if len(address) != 42:
        return False, f"长度不正确: {len(address)} (应为 42)"

    # 十六进制字符检查
    hex_part = address[2:]
    if not re.match(r'^[0-9a-fA-F]{40}$', hex_part):
        return False, "非法十六进制字符"

    return True, "✓"


def validate_wallets(json_path: Path) -> dict:
    """验证所有钱包地址"""
    with open(json_path) as f:
        data = json.load(f)

    results = {
        "sol": {"valid": 0, "invalid": 0, "errors": []},
        "base": {"valid": 0, "invalid": 0, "errors": []},
        "bsc": {"valid": 0, "invalid": 0, "errors": []},
    }

    # 验证 Solana 地址
    print("\n=== Solana 地址验证 ===")
    for wallet in data.get("sol", []):
        addr = wallet.get("address", "")
        label = wallet.get("label", "unknown")
        valid, msg = is_valid_solana_address(addr)

        if valid:
            results["sol"]["valid"] += 1
            print(f"  ✓ {label}: {addr[:20]}...")
        else:
            results["sol"]["invalid"] += 1
            results["sol"]["errors"].append(f"{label}: {msg}")
            print(f"  ✗ {label}: {msg} - {addr}")

    # 验证 Base 地址
    print("\n=== Base 地址验证 ===")
    for wallet in data.get("base", []):
        addr = wallet.get("address", "")
        label = wallet.get("label", "unknown")
        valid, msg = is_valid_evm_address(addr)

        if valid:
            results["base"]["valid"] += 1
            print(f"  ✓ {label}: {addr}")
        else:
            results["base"]["invalid"] += 1
            results["base"]["errors"].append(f"{label}: {msg}")
            print(f"  ✗ {label}: {msg} - {addr}")

    # 验证 BSC 地址
    print("\n=== BSC 地址验证 ===")
    for wallet in data.get("bsc", []):
        addr = wallet.get("address", "")
        label = wallet.get("label", "unknown")
        valid, msg = is_valid_evm_address(addr)

        if valid:
            results["bsc"]["valid"] += 1
            print(f"  ✓ {label}: {addr}")
        else:
            results["bsc"]["invalid"] += 1
            results["bsc"]["errors"].append(f"{label}: {msg}")
            print(f"  ✗ {label}: {msg} - {addr}")

    return results


def main():
    json_path = Path(__file__).parent.parent / "data" / "smart_wallets.json"

    if not json_path.exists():
        print(f"错误: 找不到文件 {json_path}")
        sys.exit(1)

    print(f"验证文件: {json_path}")
    results = validate_wallets(json_path)

    # 汇总
    print("\n" + "=" * 50)
    print("验证结果汇总:")
    print("=" * 50)

    total_valid = 0
    total_invalid = 0

    for chain, data in results.items():
        total = data["valid"] + data["invalid"]
        total_valid += data["valid"]
        total_invalid += data["invalid"]
        status = "✓" if data["invalid"] == 0 else "⚠"
        print(f"  {chain.upper():6s}: {data['valid']}/{total} 有效 {status}")

        if data["errors"]:
            for err in data["errors"]:
                print(f"         └─ {err}")

    print("-" * 50)
    print(f"  总计: {total_valid}/{total_valid + total_invalid} 有效")

    if total_invalid > 0:
        print(f"\n⚠ 发现 {total_invalid} 个无效地址，请检查修复")
        sys.exit(1)
    else:
        print("\n✓ 所有地址格式验证通过")
        sys.exit(0)


if __name__ == "__main__":
    main()
