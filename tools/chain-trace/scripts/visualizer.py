"""
Visualizer - Generate charts and diagrams for analysis results.

Features:
- Holder distribution pie chart
- Risk score visualization
- Timeline diagrams
- Cluster visualization
"""

import json
from pathlib import Path
from typing import Dict, List, Optional, Any
from datetime import datetime


class Visualizer:
    """
    Generate ASCII-based visualizations for terminal output.

    Note: For production use with matplotlib/plotly, install:
    - pip install matplotlib plotly

    This implementation uses ASCII art for zero-dependency visualization.
    """

    def __init__(self, width: int = 60):
        """
        Initialize visualizer.

        Args:
            width: Chart width in characters
        """
        self.width = width

    def generate_holder_distribution(
        self,
        holders: List[Dict[str, Any]],
        top_n: int = 10
    ) -> str:
        """
        Generate holder distribution bar chart.

        Args:
            holders: List of holder dicts with 'address' and 'balance_pct'
            top_n: Number of top holders to show

        Returns:
            ASCII bar chart
        """
        if not holders:
            return "No holder data available"

        # Sort by percentage
        sorted_holders = sorted(
            holders,
            key=lambda h: h.get('balance_pct', 0),
            reverse=True
        )[:top_n]

        lines = []
        lines.append("=" * self.width)
        lines.append("Holder Distribution (Top {})".format(top_n))
        lines.append("=" * self.width)
        lines.append("")

        max_pct = max(h.get('balance_pct', 0) for h in sorted_holders)

        for i, holder in enumerate(sorted_holders, 1):
            addr = holder.get('address', 'Unknown')
            pct = holder.get('balance_pct', 0)

            # Truncate address
            addr_short = f"{addr[:6]}...{addr[-4:]}" if len(addr) > 10 else addr

            # Calculate bar length
            bar_width = int((pct / max_pct) * (self.width - 25)) if max_pct > 0 else 0
            bar = "█" * bar_width

            lines.append(f"{i:2d}. {addr_short:12s} {bar} {pct:5.2f}%")

        lines.append("")
        lines.append("=" * self.width)

        return "\n".join(lines)

    def generate_risk_gauge(
        self,
        risk_score: float,
        confidence_score: float
    ) -> str:
        """
        Generate risk score gauge.

        Args:
            risk_score: Risk score 0-100
            confidence_score: Confidence score 0-100

        Returns:
            ASCII gauge
        """
        lines = []
        lines.append("=" * self.width)
        lines.append("Risk Assessment")
        lines.append("=" * self.width)
        lines.append("")

        # Risk gauge
        risk_bar_len = int((risk_score / 100) * (self.width - 20))
        risk_bar = "█" * risk_bar_len

        if risk_score >= 70:
            risk_label = "🔴 CRITICAL"
        elif risk_score >= 50:
            risk_label = "🟠 HIGH"
        elif risk_score >= 30:
            risk_label = "🟡 MEDIUM"
        else:
            risk_label = "🟢 LOW"

        lines.append(f"Risk Score:       {risk_bar} {risk_score:.1f}/100")
        lines.append(f"Risk Level:       {risk_label}")
        lines.append("")

        # Confidence gauge
        conf_bar_len = int((confidence_score / 100) * (self.width - 20))
        conf_bar = "█" * conf_bar_len

        lines.append(f"Confidence:       {conf_bar} {confidence_score:.1f}/100")
        lines.append("")
        lines.append("=" * self.width)

        return "\n".join(lines)

    def generate_suspicious_summary(
        self,
        suspicious: List[Dict[str, Any]]
    ) -> str:
        """
        Generate suspicious holders summary.

        Args:
            suspicious: List of suspicious holder dicts

        Returns:
            ASCII table
        """
        if not suspicious:
            return "✓ No suspicious holders detected"

        lines = []
        lines.append("=" * self.width)
        lines.append(f"Suspicious Holders ({len(suspicious)})")
        lines.append("=" * self.width)
        lines.append("")

        for i, holder in enumerate(suspicious[:10], 1):
            addr = holder.get('address', 'Unknown')
            risk = holder.get('risk_score', 0)
            flags = holder.get('flags', [])

            addr_short = f"{addr[:8]}...{addr[-6:]}"

            if risk >= 70:
                icon = "🔴"
            elif risk >= 50:
                icon = "🟠"
            else:
                icon = "🟡"

            lines.append(f"{i:2d}. {icon} {addr_short:18s} Risk: {risk:3d}/100")

            if flags:
                flag_str = ", ".join(flags[:3])
                if len(flag_str) > self.width - 8:
                    flag_str = flag_str[:self.width - 11] + "..."
                lines.append(f"    Flags: {flag_str}")

            lines.append("")

        lines.append("=" * self.width)

        return "\n".join(lines)

    def generate_cluster_summary(
        self,
        clusters: List[Dict[str, Any]]
    ) -> str:
        """
        Generate cluster analysis summary.

        Args:
            clusters: List of cluster dicts

        Returns:
            ASCII summary
        """
        if not clusters:
            return "No clusters detected"

        lines = []
        lines.append("=" * self.width)
        lines.append(f"Holder Clusters ({len(clusters)})")
        lines.append("=" * self.width)
        lines.append("")

        for i, cluster in enumerate(clusters[:5], 1):
            cluster_id = cluster.get('id', f'Cluster {i}')
            members = cluster.get('members', [])
            total_pct = cluster.get('total_percentage', 0)
            risk = cluster.get('risk_score', 0)
            signals = cluster.get('signals', [])

            lines.append(f"{i}. {cluster_id}")
            lines.append(f"   Members: {len(members)}")
            lines.append(f"   Holdings: {total_pct:.2f}%")
            lines.append(f"   Risk: {risk:.1f}/100")

            if signals:
                signal_str = ", ".join(signals[:2])
                lines.append(f"   Signals: {signal_str}")

            lines.append("")

        lines.append("=" * self.width)

        return "\n".join(lines)

    def generate_timeline(
        self,
        events: List[Dict[str, Any]]
    ) -> str:
        """
        Generate timeline visualization.

        Args:
            events: List of event dicts with 'timestamp', 'type', 'description'

        Returns:
            ASCII timeline
        """
        if not events:
            return "No timeline events"

        lines = []
        lines.append("=" * self.width)
        lines.append("Timeline")
        lines.append("=" * self.width)
        lines.append("")

        for event in events:
            timestamp = event.get('timestamp', 'Unknown')
            event_type = event.get('type', 'Event')
            description = event.get('description', '')

            # Format timestamp
            if isinstance(timestamp, str):
                try:
                    dt = datetime.fromisoformat(timestamp.replace('Z', '+00:00'))
                    time_str = dt.strftime('%Y-%m-%d %H:%M')
                except:
                    time_str = timestamp[:16]
            else:
                time_str = str(timestamp)[:16]

            # Event icon
            icon_map = {
                'deploy': '🚀',
                'launch': '🎯',
                'suspicious': '⚠️',
                'cluster': '🔗',
                'alert': '🚨',
                'info': 'ℹ️'
            }
            icon = icon_map.get(event_type.lower(), '•')

            lines.append(f"{time_str} {icon} {event_type}")

            if description:
                # Wrap description
                desc_lines = self._wrap_text(description, self.width - 4)
                for desc_line in desc_lines:
                    lines.append(f"  {desc_line}")

            lines.append("")

        lines.append("=" * self.width)

        return "\n".join(lines)

    def _wrap_text(self, text: str, width: int) -> List[str]:
        """Wrap text to specified width"""
        words = text.split()
        lines = []
        current_line = []
        current_length = 0

        for word in words:
            if current_length + len(word) + 1 <= width:
                current_line.append(word)
                current_length += len(word) + 1
            else:
                if current_line:
                    lines.append(" ".join(current_line))
                current_line = [word]
                current_length = len(word)

        if current_line:
            lines.append(" ".join(current_line))

        return lines

    def generate_full_report(
        self,
        analysis_results: Dict[str, Any]
    ) -> str:
        """
        Generate complete visual report.

        Args:
            analysis_results: Complete analysis results dict

        Returns:
            Full ASCII report
        """
        sections = []

        # Risk gauge
        if 'risk_scores' in analysis_results:
            scores = analysis_results['risk_scores']
            sections.append(self.generate_risk_gauge(
                scores.get('risk_score', 0),
                scores.get('confidence_score', 0)
            ))
            sections.append("")

        # Suspicious holders
        if 'suspicious' in analysis_results:
            susp_data = analysis_results['suspicious']
            if isinstance(susp_data, dict) and 'holders' in susp_data:
                sections.append(self.generate_suspicious_summary(
                    susp_data['holders']
                ))
                sections.append("")

        # Holder distribution
        if 'holders' in analysis_results:
            holders = analysis_results['holders']
            if holders:
                sections.append(self.generate_holder_distribution(holders))
                sections.append("")

        # Clusters
        if 'clusters' in analysis_results:
            cluster_data = analysis_results['clusters']
            if isinstance(cluster_data, dict) and 'clusters' in cluster_data:
                sections.append(self.generate_cluster_summary(
                    cluster_data['clusters']
                ))
                sections.append("")

        # Timeline
        if 'timeline' in analysis_results:
            sections.append(self.generate_timeline(
                analysis_results['timeline']
            ))

        return "\n".join(sections)


# CLI
if __name__ == "__main__":
    import sys

    if len(sys.argv) < 2:
        print("Usage: python visualizer.py <results.json>")
        sys.exit(1)

    results_file = Path(sys.argv[1])

    if not results_file.exists():
        print(f"Error: {results_file} not found")
        sys.exit(1)

    with open(results_file, 'r') as f:
        results = json.load(f)

    visualizer = Visualizer()
    report = visualizer.generate_full_report(results)

    print(report)
