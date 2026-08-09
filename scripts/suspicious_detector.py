"""
Suspicious Address Detector - Auto-flag suspicious holder patterns.

Detects:
- Zero-tx large holders (like Holder6)
- Single-tx large holders (like Holder3)
- Low BNB balance (insufficient gas)
- Large holdings with minimal activity
"""

from typing import List, Dict, Any, Optional
from dataclasses import dataclass
from enum import Enum


class SuspicionLevel(Enum):
    """Suspicion severity levels"""
    CRITICAL = "critical"
    HIGH = "high"
    MEDIUM = "medium"
    LOW = "low"


@dataclass
class SuspiciousFlag:
    """Individual suspicion flag"""
    type: str
    description: str
    severity: SuspicionLevel
    score: int


@dataclass
class SuspiciousHolder:
    """Suspicious holder with flags and risk score"""
    address: str
    balance: float
    balance_pct: float
    tx_count: int
    bnb_balance: float
    flags: List[SuspiciousFlag]
    risk_score: int
    recommendation: str


class SuspiciousDetector:
    """
    Detect suspicious holder patterns.

    Based on real-world findings:
    - Holder6: 0 tx, 1.22% holding, 0.001 BNB
    - Holder3: 1 tx, 1.63% holding, 0.002 BNB
    """

    def __init__(
        self,
        min_suspicious_pct: float = 1.0,
        min_gas_bnb: float = 0.005
    ):
        """
        Initialize detector.

        Args:
            min_suspicious_pct: Minimum holding % to flag (default: 1.0%)
            min_gas_bnb: Minimum BNB for gas (default: 0.005)
        """
        self.min_suspicious_pct = min_suspicious_pct
        self.min_gas_bnb = min_gas_bnb

    def detect(
        self,
        holders: List[Dict[str, Any]]
    ) -> List[SuspiciousHolder]:
        """
        Detect suspicious holders.

        Args:
            holders: List of holder dicts with keys:
                - address: str
                - balance: float
                - balance_pct: float
                - tx_count: int
                - bnb_balance: float

        Returns:
            List of suspicious holders sorted by risk score
        """
        suspicious = []

        for holder in holders:
            flags = self._analyze_holder(holder)

            if not flags:
                continue

            # Calculate total risk score
            risk_score = sum(f.score for f in flags)

            # Generate recommendation
            recommendation = self._generate_recommendation(flags, risk_score)

            suspicious.append(SuspiciousHolder(
                address=holder['address'],
                balance=holder['balance'],
                balance_pct=holder['balance_pct'],
                tx_count=holder['tx_count'],
                bnb_balance=holder['bnb_balance'],
                flags=flags,
                risk_score=risk_score,
                recommendation=recommendation
            ))

        # Sort by risk score (highest first)
        suspicious.sort(key=lambda x: x.risk_score, reverse=True)

        return suspicious

    def _analyze_holder(
        self,
        holder: Dict[str, Any]
    ) -> List[SuspiciousFlag]:
        """Analyze single holder for suspicious patterns"""
        flags = []

        address = holder['address']
        balance_pct = holder['balance_pct']
        tx_count = holder['tx_count']
        bnb_balance = holder['bnb_balance']

        # Flag 1: Zero transactions with large holding
        if tx_count == 0 and balance_pct >= self.min_suspicious_pct:
            flags.append(SuspiciousFlag(
                type="ZERO_TX_LARGE_HOLDING",
                description=f"Zero transactions but holds {balance_pct:.2f}% of supply",
                severity=SuspicionLevel.CRITICAL,
                score=40
            ))

        # Flag 2: Single transaction, never sold
        if tx_count == 1 and balance_pct >= self.min_suspicious_pct:
            flags.append(SuspiciousFlag(
                type="SINGLE_TX_LARGE_HOLDING",
                description=f"Only 1 transaction but holds {balance_pct:.2f}% of supply",
                severity=SuspicionLevel.HIGH,
                score=30
            ))

        # Flag 3: Insufficient gas to transact
        if bnb_balance < self.min_gas_bnb:
            flags.append(SuspiciousFlag(
                type="INSUFFICIENT_GAS",
                description=f"Only {bnb_balance:.6f} BNB (< {self.min_gas_bnb} threshold)",
                severity=SuspicionLevel.MEDIUM,
                score=20
            ))

        # Flag 4: Large holding with very low activity
        if balance_pct >= 1.5 and tx_count < 5 and tx_count > 0:
            flags.append(SuspiciousFlag(
                type="LARGE_HOLDING_LOW_ACTIVITY",
                description=f"Holds {balance_pct:.2f}% but only {tx_count} transactions",
                severity=SuspicionLevel.HIGH,
                score=25
            ))

        # Flag 5: Received tokens but never moved them
        if tx_count == 0 and balance_pct >= 0.5:
            flags.append(SuspiciousFlag(
                type="RECEIVED_NEVER_MOVED",
                description="Received tokens via internal tx, never initiated any transaction",
                severity=SuspicionLevel.HIGH,
                score=30
            ))

        # Flag 6: Cannot sell without external funding
        if bnb_balance < 0.001 and balance_pct >= 1.0:
            flags.append(SuspiciousFlag(
                type="LOCKED_BY_GAS",
                description=f"Effectively locked: {bnb_balance:.6f} BNB insufficient to move {balance_pct:.2f}%",
                severity=SuspicionLevel.CRITICAL,
                score=35
            ))

        return flags

    def _generate_recommendation(
        self,
        flags: List[SuspiciousFlag],
        risk_score: int
    ) -> str:
        """Generate monitoring recommendation"""

        if risk_score >= 70:
            return "🚨 CRITICAL: Monitor 24/7. Alert on ANY BNB deposit or transaction."
        elif risk_score >= 50:
            return "⚠️  HIGH: Monitor daily. Alert on BNB deposit or first transaction."
        elif risk_score >= 30:
            return "⚠️  MEDIUM: Monitor weekly. Check for balance changes."
        else:
            return "ℹ️  LOW: Periodic monitoring sufficient."

    def generate_report(
        self,
        suspicious: List[SuspiciousHolder]
    ) -> str:
        """Generate human-readable report"""

        if not suspicious:
            return "✓ No suspicious holders detected."

        report = []
        report.append(f"=== Suspicious Holders Detected: {len(suspicious)} ===\n")

        for i, holder in enumerate(suspicious, 1):
            report.append(f"\n{i}. {holder.address[:10]}...{holder.address[-8:]}")
            report.append(f"   Balance: {holder.balance:,.0f} ({holder.balance_pct:.2f}%)")
            report.append(f"   Tx Count: {holder.tx_count}")
            report.append(f"   BNB: {holder.bnb_balance:.6f}")
            report.append(f"   Risk Score: {holder.risk_score}/100")
            report.append(f"   Recommendation: {holder.recommendation}")
            report.append(f"\n   Flags:")

            for flag in holder.flags:
                severity_icon = {
                    SuspicionLevel.CRITICAL: "🔴",
                    SuspicionLevel.HIGH: "🟠",
                    SuspicionLevel.MEDIUM: "🟡",
                    SuspicionLevel.LOW: "🟢"
                }[flag.severity]

                report.append(f"   {severity_icon} [{flag.type}] {flag.description}")

        # Summary statistics
        report.append(f"\n=== Summary ===")
        report.append(f"Total suspicious holders: {len(suspicious)}")

        critical = sum(1 for h in suspicious if h.risk_score >= 70)
        high = sum(1 for h in suspicious if 50 <= h.risk_score < 70)
        medium = sum(1 for h in suspicious if 30 <= h.risk_score < 50)

        if critical > 0:
            report.append(f"🔴 Critical risk: {critical}")
        if high > 0:
            report.append(f"🟠 High risk: {high}")
        if medium > 0:
            report.append(f"🟡 Medium risk: {medium}")

        total_suspicious_pct = sum(h.balance_pct for h in suspicious)
        report.append(f"\nTotal suspicious holdings: {total_suspicious_pct:.2f}% of supply")

        return "\n".join(report)


# CLI
if __name__ == "__main__":
    import json
    import sys

    # Test with HODLAI data
    test_holders = [
        {
            "address": "0x93080361bcb336f7384babcbe17c2c5ffa29691b",
            "balance": 22042661,
            "balance_pct": 2.20,
            "tx_count": 245,
            "bnb_balance": 0.035501
        },
        {
            "address": "0x9a720bc486276322b5dac5b256dbc3e76707881b",
            "balance": 19763077,
            "balance_pct": 1.98,
            "tx_count": 14,
            "bnb_balance": 0.005177
        },
        {
            "address": "0x4ffbfcd476a816b04bf5fb6a19f60c992f54f604",
            "balance": 16261693,
            "balance_pct": 1.63,
            "tx_count": 1,
            "bnb_balance": 0.001984
        },
        {
            "address": "0xa5a23bb97cad1384b7c203dc76796acecf7a748e",
            "balance": 16023952,
            "balance_pct": 1.60,
            "tx_count": 5,
            "bnb_balance": 0.009912
        },
        {
            "address": "0x6e15da986db37eb612d13925fadf563b3cfff51e",
            "balance": 15463053,
            "balance_pct": 1.55,
            "tx_count": 11,
            "bnb_balance": 0.008947
        },
        {
            "address": "0x76075401bbbb958daa6aeb1811941cf223d4deb9",
            "balance": 12186354,
            "balance_pct": 1.22,
            "tx_count": 0,
            "bnb_balance": 0.001000
        }
    ]

    detector = SuspiciousDetector()
    suspicious = detector.detect(test_holders)

    print(detector.generate_report(suspicious))

    # JSON output
    if "--json" in sys.argv:
        print("\n=== JSON Output ===")
        output = []
        for holder in suspicious:
            output.append({
                "address": holder.address,
                "balance_pct": holder.balance_pct,
                "tx_count": holder.tx_count,
                "bnb_balance": holder.bnb_balance,
                "risk_score": holder.risk_score,
                "flags": [
                    {
                        "type": f.type,
                        "description": f.description,
                        "severity": f.severity.value,
                        "score": f.score
                    }
                    for f in holder.flags
                ]
            })
        print(json.dumps(output, indent=2))
