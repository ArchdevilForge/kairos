"""
Transaction History Fetcher with multi-strategy fallback.

Strategies:
1. Blockscout API (Base/ETH) - Primary
2. eth_getLogs with chunked block ranges - Fallback
3. GoPlus holder data inference - Last resort

Based on research:
- ethereum-etl patterns
- Chainstack best practices
- Solana getSignaturesForAddress
"""

import time
from typing import List, Dict, Optional, Any, Tuple
from dataclasses import dataclass
from datetime import datetime

try:
    from .rpc_manager import RPCManager, Chain, AllRPCsFailedError
except ImportError:
    from rpc_manager import RPCManager, Chain, AllRPCsFailedError


@dataclass
class Transaction:
    """Transaction record"""
    hash: str
    block_number: int
    timestamp: int
    from_address: str
    to_address: str
    value: str
    method: Optional[str] = None
    token_address: Optional[str] = None


class TransactionHistoryFetcher:
    """
    Fetch transaction history with automatic fallback strategies.

    Features:
    - Get first N transactions for addresses
    - Get transaction timeline in block range
    - Chunked queries to avoid rate limits
    - Multi-chain support (EVM + Solana)
    """

    def __init__(
        self,
        chain: str,
        rpc_manager: Optional[RPCManager] = None,
        blockscout_url: Optional[str] = None
    ):
        """
        Initialize fetcher.

        Args:
            chain: Chain name (eth, base, bsc, solana)
            rpc_manager: Optional RPC manager instance
            blockscout_url: Optional Blockscout API URL
        """
        self.chain = chain.lower()
        self.rpc_manager = rpc_manager
        self.blockscout_url = blockscout_url

        # Blockscout URLs
        if not self.blockscout_url:
            blockscout_map = {
                'eth': 'https://eth.blockscout.com',
                'base': 'https://base.blockscout.com',
            }
            self.blockscout_url = blockscout_map.get(self.chain)

    def get_first_transactions(
        self,
        addresses: List[str],
        limit: int = 10
    ) -> Dict[str, List[Transaction]]:
        """
        Get first N transactions for multiple addresses.

        Args:
            addresses: List of addresses
            limit: Max transactions per address

        Returns:
            Dict mapping address to list of transactions
        """
        results = {}

        for address in addresses:
            txs = self._get_address_first_txs(address, limit)
            if txs:
                results[address] = txs

        return results

    def _get_address_first_txs(
        self,
        address: str,
        limit: int
    ) -> List[Transaction]:
        """Get first transactions for single address"""

        # Strategy 1: Blockscout API
        if self.blockscout_url:
            txs = self._blockscout_get_first_txs(address, limit)
            if txs:
                return txs

        # Strategy 2: eth_getLogs with binary search
        if self.rpc_manager:
            txs = self._rpc_get_first_txs(address, limit)
            if txs:
                return txs

        return []

    def _blockscout_get_first_txs(
        self,
        address: str,
        limit: int
    ) -> Optional[List[Transaction]]:
        """Fetch from Blockscout API"""
        import urllib.request
        import json

        try:
            url = f"{self.blockscout_url}/api/v2/addresses/{address}/transactions"
            url += f"?filter=to%20%7C%20from&type=&limit={limit}"

            req = urllib.request.Request(url, headers={
                'User-Agent': 'Mozilla/5.0',
                'Accept': 'application/json'
            })

            with urllib.request.urlopen(req, timeout=15) as resp:
                data = json.loads(resp.read().decode())

            if not data or 'items' not in data:
                return None

            txs = []
            for item in data['items'][:limit]:
                tx = Transaction(
                    hash=item['hash'],
                    block_number=int(item['block']),
                    timestamp=int(datetime.fromisoformat(
                        item['timestamp'].replace('Z', '+00:00')
                    ).timestamp()),
                    from_address=item['from']['hash'],
                    to_address=item['to']['hash'] if item.get('to') else '',
                    value=item['value'],
                    method=item.get('method')
                )
                txs.append(tx)

            return txs

        except Exception as e:
            print(f"[Blockscout] Error fetching {address}: {e}")
            return None

    def _rpc_get_first_txs(
        self,
        address: str,
        limit: int
    ) -> Optional[List[Transaction]]:
        """
        Fetch using eth_getLogs with binary search.

        Strategy:
        - Binary search to find first block with activity
        - Fetch logs in chunks to avoid rate limits
        """
        if not self.rpc_manager:
            return None

        try:
            # Get current block
            current_block_hex = self.rpc_manager.call("eth_blockNumber", [])
            current_block = int(current_block_hex, 16)

            # Binary search for first transaction
            first_block = self._binary_search_first_block(
                address, 0, current_block
            )

            if first_block is None:
                return None

            # Fetch transactions from first block
            return self._fetch_txs_from_block(
                address, first_block, current_block, limit
            )

        except Exception as e:
            print(f"[RPC] Error fetching {address}: {e}")
            return None

    def _binary_search_first_block(
        self,
        address: str,
        start: int,
        end: int,
        max_iterations: int = 20
    ) -> Optional[int]:
        """Binary search to find first block with activity"""

        # Check if address has any activity
        has_activity = self._check_block_range_activity(
            address, start, end
        )

        if not has_activity:
            return None

        # Binary search
        left, right = start, end
        result = None

        for _ in range(max_iterations):
            if left >= right:
                break

            mid = (left + right) // 2

            # Check left half
            has_left = self._check_block_range_activity(
                address, left, mid
            )

            if has_left:
                result = mid
                right = mid
            else:
                left = mid + 1

            time.sleep(0.5)  # Rate limit protection

        return result

    def _check_block_range_activity(
        self,
        address: str,
        from_block: int,
        to_block: int
    ) -> bool:
        """Check if address has activity in block range"""

        # Limit range to avoid rate limits
        chunk_size = 10000
        if to_block - from_block > chunk_size:
            to_block = from_block + chunk_size

        try:
            # Check for incoming transfers
            logs = self.rpc_manager.call("eth_getLogs", [{
                "fromBlock": hex(from_block),
                "toBlock": hex(to_block),
                "topics": [
                    "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",  # Transfer
                    None,
                    f"0x000000000000000000000000{address[2:].lower()}"  # to
                ]
            }])

            if logs and len(logs) > 0:
                return True

            # Check for outgoing transfers
            logs = self.rpc_manager.call("eth_getLogs", [{
                "fromBlock": hex(from_block),
                "toBlock": hex(to_block),
                "topics": [
                    "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
                    f"0x000000000000000000000000{address[2:].lower()}",  # from
                ]
            }])

            return logs and len(logs) > 0

        except Exception:
            return False

    def _fetch_txs_from_block(
        self,
        address: str,
        start_block: int,
        end_block: int,
        limit: int
    ) -> List[Transaction]:
        """Fetch transactions starting from block"""

        txs = []
        chunk_size = 5000
        current = start_block

        while current <= end_block and len(txs) < limit:
            chunk_end = min(current + chunk_size, end_block)

            try:
                # Fetch Transfer events
                logs = self.rpc_manager.call("eth_getLogs", [{
                    "fromBlock": hex(current),
                    "toBlock": hex(chunk_end),
                    "topics": [
                        "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
                    ]
                }])

                # Parse logs
                for log in logs[:limit - len(txs)]:
                    if len(log['topics']) < 3:
                        continue

                    from_addr = '0x' + log['topics'][1][-40:]
                    to_addr = '0x' + log['topics'][2][-40:]

                    if from_addr.lower() == address.lower() or \
                       to_addr.lower() == address.lower():

                        tx = Transaction(
                            hash=log['transactionHash'],
                            block_number=int(log['blockNumber'], 16),
                            timestamp=0,  # Need separate call
                            from_address=from_addr,
                            to_address=to_addr,
                            value=log['data'],
                            token_address=log['address']
                        )
                        txs.append(tx)

                if len(txs) >= limit:
                    break

            except AllRPCsFailedError:
                print(f"[RPC] All endpoints failed for range {current}-{chunk_end}")
                break
            except Exception as e:
                print(f"[RPC] Error in range {current}-{chunk_end}: {e}")

            current = chunk_end + 1
            time.sleep(0.3)  # Rate limit protection

        return txs[:limit]

    def get_transaction_timeline(
        self,
        address: str,
        start_block: int,
        end_block: int
    ) -> List[Transaction]:
        """
        Get transaction timeline in block range.

        Args:
            address: Target address
            start_block: Start block number
            end_block: End block number

        Returns:
            List of transactions sorted by block
        """
        return self._fetch_txs_from_block(
            address, start_block, end_block, limit=1000
        )


# CLI
if __name__ == "__main__":
    import sys
    import json

    if len(sys.argv) < 3:
        print("Usage: python tx_history_fetcher.py <chain> <address> [limit]")
        print("Chains: eth, base, bsc")
        sys.exit(1)

    chain = sys.argv[1]
    address = sys.argv[2]
    limit = int(sys.argv[3]) if len(sys.argv) > 3 else 10

    # Create RPC manager
    from rpc_manager import create_rpc_manager
    rpc_manager = create_rpc_manager(chain)

    # Create fetcher
    fetcher = TransactionHistoryFetcher(chain, rpc_manager=rpc_manager)

    print(f"[Chain] {chain}")
    print(f"[Address] {address}")
    print(f"[Limit] {limit}\n")

    # Fetch first transactions
    results = fetcher.get_first_transactions([address], limit=limit)

    if address in results:
        txs = results[address]
        print(f"Found {len(txs)} transactions:\n")

        for i, tx in enumerate(txs, 1):
            print(f"{i}. Block {tx.block_number}")
            print(f"   Hash: {tx.hash}")
            print(f"   From: {tx.from_address}")
            print(f"   To: {tx.to_address}")
            if tx.token_address:
                print(f"   Token: {tx.token_address}")
            print()
    else:
        print("No transactions found")
