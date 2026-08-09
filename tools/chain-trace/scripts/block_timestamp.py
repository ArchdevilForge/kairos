"""
Block Timestamp Converter - Convert between block numbers and timestamps.

Uses binary search with caching for efficient conversion.

Based on research:
- Average block times per chain
- Binary search optimization patterns
- Caching strategies for blockchain data
"""

import time
from typing import Optional, Dict
from datetime import datetime, timedelta
from dataclasses import dataclass

try:
    from .rpc_manager import RPCManager, Chain
except ImportError:
    from rpc_manager import RPCManager, Chain


@dataclass
class BlockInfo:
    """Block information"""
    number: int
    timestamp: int
    datetime: datetime


# Average block times (seconds)
BLOCK_TIMES = {
    'eth': 12,
    'base': 2,
    'bsc': 3,
    'solana': 0.4,
}


class BlockTimestampConverter:
    """
    Convert between block numbers and timestamps.

    Features:
    - Binary search for timestamp -> block
    - Caching for repeated queries
    - Multi-chain support
    """

    def __init__(
        self,
        chain: str,
        rpc_manager: Optional[RPCManager] = None
    ):
        """
        Initialize converter.

        Args:
            chain: Chain name (eth, base, bsc, solana)
            rpc_manager: Optional RPC manager instance
        """
        self.chain = chain.lower()
        self.rpc_manager = rpc_manager
        self.cache: Dict[int, BlockInfo] = {}
        self.avg_block_time = BLOCK_TIMES.get(self.chain, 12)

    def block_to_timestamp(self, block_number: int) -> Optional[int]:
        """
        Convert block number to UTC timestamp.

        Args:
            block_number: Block number

        Returns:
            Unix timestamp (seconds)
        """

        # Check cache
        if block_number in self.cache:
            return self.cache[block_number].timestamp

        # Fetch from RPC
        if not self.rpc_manager:
            return None

        try:
            if self.chain == 'solana':
                # Solana: getBlockTime
                timestamp = self.rpc_manager.call(
                    "getBlockTime",
                    [block_number]
                )
            else:
                # EVM: eth_getBlockByNumber
                block_hex = hex(block_number)
                block_data = self.rpc_manager.call(
                    "eth_getBlockByNumber",
                    [block_hex, False]
                )

                if not block_data:
                    return None

                timestamp = int(block_data['timestamp'], 16)

            # Cache result
            self.cache[block_number] = BlockInfo(
                number=block_number,
                timestamp=timestamp,
                datetime=datetime.utcfromtimestamp(timestamp)
            )

            return timestamp

        except Exception as e:
            print(f"[BlockConverter] Error fetching block {block_number}: {e}")
            return None

    def timestamp_to_block(
        self,
        timestamp: int,
        tolerance: int = 60
    ) -> Optional[int]:
        """
        Convert UTC timestamp to approximate block number.

        Uses binary search to find closest block.

        Args:
            timestamp: Unix timestamp (seconds)
            tolerance: Acceptable time difference (seconds)

        Returns:
            Block number (approximate)
        """

        if not self.rpc_manager:
            return None

        try:
            # Get current block
            if self.chain == 'solana':
                current_block = self.rpc_manager.call("getSlot", [])
            else:
                current_block_hex = self.rpc_manager.call("eth_blockNumber", [])
                current_block = int(current_block_hex, 16)

            # Get current timestamp
            current_timestamp = self.block_to_timestamp(current_block)
            if not current_timestamp:
                return None

            # Check if timestamp is in future
            if timestamp > current_timestamp:
                print(f"[BlockConverter] Timestamp {timestamp} is in future")
                return None

            # Estimate starting point
            time_diff = current_timestamp - timestamp
            blocks_back = int(time_diff / self.avg_block_time)
            estimated_block = max(0, current_block - blocks_back)

            # Binary search
            result = self._binary_search_block(
                timestamp,
                start=estimated_block,
                end=current_block,
                tolerance=tolerance
            )

            return result

        except Exception as e:
            print(f"[BlockConverter] Error converting timestamp {timestamp}: {e}")
            return None

    def _binary_search_block(
        self,
        target_timestamp: int,
        start: int,
        end: int,
        tolerance: int = 60,
        max_iterations: int = 20
    ) -> Optional[int]:
        """
        Binary search to find block closest to timestamp.

        Args:
            target_timestamp: Target timestamp
            start: Start block
            end: End block
            tolerance: Acceptable time difference
            max_iterations: Max search iterations

        Returns:
            Block number
        """

        left, right = start, end
        best_block = None
        best_diff = float('inf')

        for iteration in range(max_iterations):
            if left > right:
                break

            mid = (left + right) // 2

            # Get block timestamp
            mid_timestamp = self.block_to_timestamp(mid)
            if not mid_timestamp:
                # RPC error, try to continue
                if iteration < max_iterations - 1:
                    time.sleep(1)
                    continue
                break

            # Calculate difference
            diff = abs(mid_timestamp - target_timestamp)

            # Update best
            if diff < best_diff:
                best_diff = diff
                best_block = mid

            # Check if within tolerance
            if diff <= tolerance:
                return mid

            # Adjust search range
            if mid_timestamp < target_timestamp:
                left = mid + 1
            else:
                right = mid - 1

            # Rate limit protection
            time.sleep(0.2)

        return best_block

    def get_block_info(self, block_number: int) -> Optional[BlockInfo]:
        """
        Get full block information.

        Args:
            block_number: Block number

        Returns:
            BlockInfo object
        """

        # Check cache
        if block_number in self.cache:
            return self.cache[block_number]

        # Fetch timestamp
        timestamp = self.block_to_timestamp(block_number)
        if not timestamp:
            return None

        return self.cache.get(block_number)

    def get_block_range_for_timespan(
        self,
        start_timestamp: int,
        end_timestamp: int
    ) -> Optional[tuple[int, int]]:
        """
        Get block range for time span.

        Args:
            start_timestamp: Start timestamp
            end_timestamp: End timestamp

        Returns:
            (start_block, end_block) tuple
        """

        start_block = self.timestamp_to_block(start_timestamp)
        end_block = self.timestamp_to_block(end_timestamp)

        if start_block is None or end_block is None:
            return None

        return (start_block, end_block)

    def format_block_info(self, block_info: BlockInfo) -> str:
        """Format block info for display"""
        return (
            f"Block {block_info.number}\n"
            f"Timestamp: {block_info.timestamp}\n"
            f"UTC: {block_info.datetime.strftime('%Y-%m-%d %H:%M:%S')}\n"
        )


# CLI
if __name__ == "__main__":
    import sys
    from datetime import datetime

    if len(sys.argv) < 3:
        print("Usage:")
        print("  python block_timestamp.py <chain> block <number>")
        print("  python block_timestamp.py <chain> timestamp <unix_timestamp>")
        print("  python block_timestamp.py <chain> date <YYYY-MM-DD>")
        print("\nChains: eth, base, bsc, solana")
        sys.exit(1)

    chain = sys.argv[1]
    mode = sys.argv[2]

    # Create RPC manager
    from rpc_manager import create_rpc_manager
    rpc_manager = create_rpc_manager(chain)

    # Create converter
    converter = BlockTimestampConverter(chain, rpc_manager=rpc_manager)

    print(f"[Chain] {chain}\n")

    if mode == "block" and len(sys.argv) > 3:
        # Block -> Timestamp
        block_number = int(sys.argv[3])
        block_info = converter.get_block_info(block_number)

        if block_info:
            print(converter.format_block_info(block_info))
        else:
            print("Failed to fetch block info")

    elif mode == "timestamp" and len(sys.argv) > 3:
        # Timestamp -> Block
        timestamp = int(sys.argv[3])
        block_number = converter.timestamp_to_block(timestamp)

        if block_number:
            print(f"Timestamp {timestamp} ≈ Block {block_number}")

            # Verify
            block_info = converter.get_block_info(block_number)
            if block_info:
                print(f"Actual block time: {block_info.datetime.strftime('%Y-%m-%d %H:%M:%S')} UTC")
                print(f"Time difference: {abs(block_info.timestamp - timestamp)}s")
        else:
            print("Failed to find block")

    elif mode == "date" and len(sys.argv) > 3:
        # Date -> Block
        date_str = sys.argv[3]
        dt = datetime.strptime(date_str, "%Y-%m-%d")
        timestamp = int(dt.timestamp())

        print(f"Date: {date_str}")
        print(f"Timestamp: {timestamp}\n")

        block_number = converter.timestamp_to_block(timestamp)

        if block_number:
            print(f"Approximate block: {block_number}")

            # Verify
            block_info = converter.get_block_info(block_number)
            if block_info:
                print(f"Actual block time: {block_info.datetime.strftime('%Y-%m-%d %H:%M:%S')} UTC")
        else:
            print("Failed to find block")

    else:
        print("Invalid arguments")
        sys.exit(1)
