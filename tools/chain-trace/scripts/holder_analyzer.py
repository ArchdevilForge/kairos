"""
Holder Analyzer with DBSCAN clustering for insider/bundle detection.

Based on research:
- DBSCAN for blockchain address clustering (arXiv:2107.05749)
- Ethereum address clustering patterns (Towards Data Science)
- Bitcoin clustering techniques (Financial Cryptography 2022)

Features:
- co_amount clustering (CV < 15% = strong signal)
- Activity anomaly detection (single-tx holders)
- Known address filtering (CEX, LP, Dead)
"""

import numpy as np
from typing import List, Dict, Optional, Tuple, Set
from dataclasses import dataclass
from sklearn.cluster import DBSCAN
from sklearn.preprocessing import StandardScaler


@dataclass
class Holder:
    """Holder record"""
    address: str
    balance: float
    percentage: float
    tx_count: int = 0
    first_tx_block: Optional[int] = None
    last_tx_block: Optional[int] = None


@dataclass
class Cluster:
    """Holder cluster"""
    id: str
    members: List[str]
    total_balance: float
    total_percentage: float
    avg_balance: float
    balance_cv: float  # Coefficient of variation
    avg_tx_count: float
    single_tx_holders: int
    risk_score: float
    signals: List[str]


# Known addresses to exclude
KNOWN_ADDRESSES = {
    'bsc': {
        'cex_hot_wallets': [
            '0xeb2d2f1b8c558a40207669291fda468e50c8a0bb',  # Binance
            '0x28c6c06298d514db089934071355e5743bf21d60',  # Binance 2
            '0x21a31ee1afc51d94c2efccaa2092ad1028285549',  # Binance 3
            '0xdfd5293d8e347dfe59e90efd55b2956a1343963d',  # Binance 4
            '0x56eddb7aa87536c09ccc2793473599fd21a8b17f',  # Binance 5
            '0x9696f59e4d72e237be84ffd425dcad154bf96976',  # Binance 6
            '0x4976a4a02f38326660d17bf34b431dc6e2eb2327',  # Binance 7
            '0xbe0eb53f46cd790cd13851d5eff43d12404d33e8',  # Binance 8
        ],
        'routers': [
            '0x10ed43c718714eb63d5aa57b78b54704e256024e',  # PancakeSwap V2
            '0x13f4ea83d0bd40e75c8222255bc855a974568dd4',  # PancakeSwap V3
            '0x1b81d678ffb9c0263b24a97847620c99d213eb14',  # PancakeSwap Legacy
        ],
        'dead': [
            '0x000000000000000000000000000000000000dead',
            '0x0000000000000000000000000000000000000000',
        ],
        'launchpads': {
            'pinksale': '0x407993575c91ce7643a4d4ccacc9a98c36ee1bbe',
            'dxsale': '0x7ee058420e5937496f5a2096f04caa7721cf70cc',
        }
    },
    'base': {
        'cex_hot_wallets': [
            '0x3304e22ddaa22bcdc5fca2269b418046ae7b566a',  # Coinbase
        ],
        'routers': [
            '0x4752ba5dbc23f44d87826276bf6fd6b1c372ad24',  # BaseSwap
            '0x327df1e6de05895d2ab08513aadd9313fe505d86',  # Aerodrome
        ],
        'dead': [
            '0x000000000000000000000000000000000000dead',
            '0x0000000000000000000000000000000000000000',
        ],
    },
    'eth': {
        'cex_hot_wallets': [
            '0x28c6c06298d514db089934071355e5743bf21d60',  # Binance
            '0x3f5ce5fbfe3e9af3971dd833d26ba9b5c936f0be',  # Binance 2
            '0x21a31ee1afc51d94c2efccaa2092ad1028285549',  # Binance 3
        ],
        'routers': [
            '0x7a250d5630b4cf539739df2c5dacb4c659f2488d',  # Uniswap V2
            '0xe592427a0aece92de3edee1f18e0157c05861564',  # Uniswap V3
            '0xef1c6e67703c7bd7107eed8303fbe6ec2554bf6b',  # Uniswap Universal
        ],
        'dead': [
            '0x000000000000000000000000000000000000dead',
            '0x0000000000000000000000000000000000000000',
        ],
    }
}


class HolderAnalyzer:
    """
    Analyze holder patterns for insider/bundle detection.

    Methods:
    - DBSCAN clustering on balance amounts
    - Activity anomaly detection
    - Known address filtering
    - Holder origin tracking
    """

    def __init__(self, chain: str = 'bsc', rpc_manager=None):
        """
        Initialize analyzer.

        Args:
            chain: Chain name (bsc, base, eth)
            rpc_manager: Optional RPC manager for origin tracking
        """
        self.chain = chain.lower()
        self.known_addresses = self._load_known_addresses()
        self.rpc_manager = rpc_manager

    def _load_known_addresses(self) -> Set[str]:
        """Load known addresses for this chain"""
        known = set()

        chain_data = KNOWN_ADDRESSES.get(self.chain, {})

        for category in chain_data.values():
            if isinstance(category, list):
                known.update(addr.lower() for addr in category)
            elif isinstance(category, dict):
                known.update(addr.lower() for addr in category.values())

        return known

    def is_known_address(self, address: str) -> bool:
        """Check if address is known (CEX/LP/Dead)"""
        return address.lower() in self.known_addresses

    def analyze_holder_patterns(
        self,
        holders: List[Holder],
        min_cluster_size: int = 3,
        eps: float = 0.5
    ) -> Dict[str, any]:
        """
        Analyze holder patterns and detect clusters.

        Args:
            holders: List of holder records
            min_cluster_size: Minimum cluster size for DBSCAN
            eps: DBSCAN epsilon parameter

        Returns:
            Analysis results with clusters and risk scores
        """

        # Filter out known addresses
        filtered_holders = [
            h for h in holders
            if not self.is_known_address(h.address)
        ]

        if len(filtered_holders) < min_cluster_size:
            return {
                'clusters': [],
                'anomalies': [],
                'risk_score': 0.0,
                'total_holders': len(holders),
                'filtered_holders': len(filtered_holders)
            }

        # Cluster by amount
        clusters = self._cluster_by_amount(
            filtered_holders,
            min_cluster_size=min_cluster_size,
            eps=eps
        )

        # Detect anomalies
        anomalies = self._detect_activity_anomalies(filtered_holders)

        # Calculate overall risk
        risk_score = self._calculate_risk(clusters, anomalies)

        return {
            'clusters': clusters,
            'anomalies': anomalies,
            'risk_score': risk_score,
            'total_holders': len(holders),
            'filtered_holders': len(filtered_holders)
        }

    def _cluster_by_amount(
        self,
        holders: List[Holder],
        min_cluster_size: int = 3,
        eps: float = 0.5
    ) -> List[Cluster]:
        """
        Cluster holders by balance amount using DBSCAN.

        Args:
            holders: List of holders
            min_cluster_size: Minimum cluster size
            eps: DBSCAN epsilon (distance threshold)

        Returns:
            List of clusters
        """

        if len(holders) < min_cluster_size:
            return []

        # Prepare features: log(balance) for better clustering
        balances = np.array([
            [np.log10(h.balance + 1)] for h in holders
        ])

        # Standardize
        scaler = StandardScaler()
        balances_scaled = scaler.fit_transform(balances)

        # DBSCAN clustering
        dbscan = DBSCAN(eps=eps, min_samples=min_cluster_size)
        labels = dbscan.fit_predict(balances_scaled)

        # Group by cluster
        clusters_dict: Dict[int, List[Holder]] = {}
        for holder, label in zip(holders, labels):
            if label == -1:  # Noise
                continue
            if label not in clusters_dict:
                clusters_dict[label] = []
            clusters_dict[label].append(holder)

        # Build cluster objects
        clusters = []
        for cluster_id, members in clusters_dict.items():
            cluster = self._build_cluster(
                f"CLUSTER_{cluster_id:03d}",
                members
            )
            clusters.append(cluster)

        # Sort by risk score
        clusters.sort(key=lambda c: c.risk_score, reverse=True)

        return clusters

    def _build_cluster(
        self,
        cluster_id: str,
        members: List[Holder]
    ) -> Cluster:
        """Build cluster object with statistics"""

        balances = [h.balance for h in members]
        percentages = [h.percentage for h in members]
        tx_counts = [h.tx_count for h in members if h.tx_count > 0]

        # Calculate statistics
        total_balance = sum(balances)
        total_percentage = sum(percentages)
        avg_balance = np.mean(balances)
        balance_std = np.std(balances)
        balance_cv = (balance_std / avg_balance) if avg_balance > 0 else 0

        avg_tx_count = np.mean(tx_counts) if tx_counts else 0
        single_tx_holders = sum(1 for h in members if h.tx_count == 1)

        # Detect signals
        signals = []

        # co_amount signal (CV < 15%)
        if balance_cv < 0.15:
            signals.append(f"co_amount (CV={balance_cv:.1%})")

        # Single-tx holders
        if single_tx_holders >= len(members) * 0.5:
            signals.append(f"single_tx_holders ({single_tx_holders}/{len(members)})")

        # Low activity
        if avg_tx_count < 10:
            signals.append(f"low_activity (avg={avg_tx_count:.1f})")

        # Calculate risk score
        risk_score = self._calculate_cluster_risk(
            len(members),
            balance_cv,
            single_tx_holders,
            avg_tx_count
        )

        return Cluster(
            id=cluster_id,
            members=[h.address for h in members],
            total_balance=total_balance,
            total_percentage=total_percentage,
            avg_balance=avg_balance,
            balance_cv=balance_cv,
            avg_tx_count=avg_tx_count,
            single_tx_holders=single_tx_holders,
            risk_score=risk_score,
            signals=signals
        )

    def _calculate_cluster_risk(
        self,
        size: int,
        balance_cv: float,
        single_tx_holders: int,
        avg_tx_count: float
    ) -> float:
        """
        Calculate cluster risk score (0-100).

        Factors:
        - Cluster size (larger = higher risk)
        - Balance CV (lower = higher risk)
        - Single-tx holders (more = higher risk)
        - Low activity (lower = higher risk)
        """

        score = 0.0

        # Size factor (0-30 points)
        if size >= 5:
            score += 30
        elif size >= 3:
            score += 20

        # co_amount factor (0-40 points)
        if balance_cv < 0.10:
            score += 40
        elif balance_cv < 0.15:
            score += 30
        elif balance_cv < 0.25:
            score += 15

        # Single-tx holders (0-20 points)
        single_tx_ratio = single_tx_holders / size
        if single_tx_ratio >= 0.8:
            score += 20
        elif single_tx_ratio >= 0.5:
            score += 10

        # Low activity (0-10 points)
        if avg_tx_count < 5:
            score += 10
        elif avg_tx_count < 10:
            score += 5

        return min(score, 100.0)

    def _detect_activity_anomalies(
        self,
        holders: List[Holder]
    ) -> List[Dict[str, any]]:
        """
        Detect activity anomalies.

        Anomalies:
        - Single-transaction holders
        - Holders with identical tx counts
        - Holders with no recent activity
        """

        anomalies = []

        # Single-tx holders
        single_tx = [h for h in holders if h.tx_count == 1]
        if len(single_tx) >= 3:
            anomalies.append({
                'type': 'single_tx_holders',
                'count': len(single_tx),
                'addresses': [h.address for h in single_tx[:10]],
                'severity': 'high' if len(single_tx) >= 5 else 'medium'
            })

        # Identical tx counts (co_activity)
        tx_count_groups: Dict[int, List[Holder]] = {}
        for h in holders:
            if h.tx_count > 0:
                if h.tx_count not in tx_count_groups:
                    tx_count_groups[h.tx_count] = []
                tx_count_groups[h.tx_count].append(h)

        for tx_count, group in tx_count_groups.items():
            if len(group) >= 3 and tx_count < 20:
                anomalies.append({
                    'type': 'identical_tx_count',
                    'tx_count': tx_count,
                    'count': len(group),
                    'addresses': [h.address for h in group[:10]],
                    'severity': 'medium'
                })

        return anomalies

    def _calculate_risk(
        self,
        clusters: List[Cluster],
        anomalies: List[Dict]
    ) -> float:
        """Calculate overall risk score"""

        if not clusters and not anomalies:
            return 0.0

        # Max cluster risk
        max_cluster_risk = max(
            (c.risk_score for c in clusters),
            default=0.0
        )

        # Anomaly score
        anomaly_score = 0.0
        for anomaly in anomalies:
            if anomaly['severity'] == 'high':
                anomaly_score += 20
            elif anomaly['severity'] == 'medium':
                anomaly_score += 10

        # Combined score
        return min(max_cluster_risk + anomaly_score * 0.3, 100.0)

    def analyze_holder_origin(
        self,
        holder_address: str,
        token_address: str
    ) -> Optional[Dict[str, any]]:
        """
        Analyze where holder received tokens from.

        Args:
            holder_address: Holder address to analyze
            token_address: Token contract address

        Returns:
            Dict with origin info or None if RPC manager not available
        """
        if not self.rpc_manager:
            return None

        # ERC20 Transfer event signature
        TRANSFER_TOPIC = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

        # Pad address to 32 bytes (64 hex chars)
        holder_padded = "0x" + holder_address[2:].lower().zfill(64)

        try:
            # Query Transfer events TO holder
            # Note: This may fail due to rate limits, use small block ranges
            logs = self.rpc_manager.call("eth_getLogs", [{
                "fromBlock": "earliest",
                "toBlock": "latest",
                "address": token_address,
                "topics": [
                    TRANSFER_TOPIC,
                    None,  # from any
                    holder_padded  # to holder
                ]
            }])

            if logs and len(logs) > 0:
                first_log = logs[0]
                from_addr = "0x" + first_log["topics"][1][-40:]
                block_num = int(first_log["blockNumber"], 16)
                tx_hash = first_log["transactionHash"]

                return {
                    "first_receive_from": from_addr,
                    "first_receive_block": block_num,
                    "first_receive_tx": tx_hash,
                    "total_receives": len(logs)
                }

        except Exception as e:
            print(f"[HolderAnalyzer] Origin tracking failed: {e}")

        return None

    def batch_analyze_origins(
        self,
        holders: List[Holder],
        token_address: str
    ) -> Dict[str, Dict]:
        """
        Batch analyze holder origins.

        Args:
            holders: List of holders
            token_address: Token contract address

        Returns:
            Dict mapping address to origin info
        """
        origins = {}

        for holder in holders:
            origin = self.analyze_holder_origin(holder.address, token_address)
            if origin:
                origins[holder.address] = origin

        return origins

    def detect_coordinated_distribution(
        self,
        origins: Dict[str, Dict]
    ) -> List[Dict[str, any]]:
        """
        Detect coordinated distributions from same source.

        Args:
            origins: Dict mapping address to origin info

        Returns:
            List of coordinated distribution patterns
        """
        # Group by first_receive_from
        from_groups: Dict[str, List[str]] = {}

        for addr, origin in origins.items():
            from_addr = origin.get('first_receive_from')
            if from_addr:
                if from_addr not in from_groups:
                    from_groups[from_addr] = []
                from_groups[from_addr].append(addr)

        # Find groups with 3+ recipients
        coordinated = []

        for from_addr, recipients in from_groups.items():
            if len(recipients) >= 3:
                coordinated.append({
                    'distributor': from_addr,
                    'recipients': recipients,
                    'count': len(recipients),
                    'severity': 'high' if len(recipients) >= 5 else 'medium'
                })

        return coordinated


# CLI
if __name__ == "__main__":
    import sys
    import json

    # Test with sample data
    sample_holders = [
        Holder('0x1111', 1000000, 5.0, tx_count=1),
        Holder('0x2222', 1050000, 5.25, tx_count=1),
        Holder('0x3333', 980000, 4.9, tx_count=1),
        Holder('0x4444', 1020000, 5.1, tx_count=1),
        Holder('0x5555', 990000, 4.95, tx_count=1),
        Holder('0x6666', 5000000, 25.0, tx_count=50),
        Holder('0x7777', 200000, 1.0, tx_count=10),
    ]

    analyzer = HolderAnalyzer(chain='bsc')

    print("=== Holder Pattern Analysis ===\n")

    results = analyzer.analyze_holder_patterns(sample_holders)

    print(f"Total holders: {results['total_holders']}")
    print(f"Filtered holders: {results['filtered_holders']}")
    print(f"Overall risk score: {results['risk_score']:.1f}/100\n")

    print(f"Found {len(results['clusters'])} clusters:\n")

    for cluster in results['clusters']:
        print(f"{cluster.id}:")
        print(f"  Members: {len(cluster.members)}")
        print(f"  Total holdings: {cluster.total_percentage:.2f}%")
        print(f"  Balance CV: {cluster.balance_cv:.1%}")
        print(f"  Avg tx count: {cluster.avg_tx_count:.1f}")
        print(f"  Single-tx holders: {cluster.single_tx_holders}")
        print(f"  Risk score: {cluster.risk_score:.1f}/100")
        print(f"  Signals: {', '.join(cluster.signals)}")
        print()

    if results['anomalies']:
        print(f"Found {len(results['anomalies'])} anomalies:\n")
        for anomaly in results['anomalies']:
            print(f"- {anomaly['type']}: {anomaly['count']} addresses ({anomaly['severity']})")
