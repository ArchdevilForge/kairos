"""Tests for performance_monitor utility."""

import pytest
from unittest.mock import MagicMock, patch
from kairos.utils.performance_monitor import (
    MetricType, Metric, PerformanceSnapshot, PerformanceMonitor
)


class TestMetricType:
    """Test MetricType enum."""
    
    def test_metric_type_values(self):
        """Test MetricType enum values."""
        assert MetricType.COUNTER.value == "counter"
        assert MetricType.GAUGE.value == "gauge"
        assert MetricType.HISTOGRAM.value == "histogram"
        assert MetricType.TIMER.value == "timer"


class TestMetric:
    """Test Metric dataclass."""
    
    def test_init(self):
        """Test Metric initialization."""
        metric = Metric(
            name="test_metric",
            type=MetricType.COUNTER,
            value=42.0
        )
        assert metric.name == "test_metric"
        assert metric.type == MetricType.COUNTER
        assert metric.value == 42.0
        assert metric.tags == {}
    
    def test_to_dict(self):
        """Test Metric to_dict method."""
        metric = Metric(
            name="test_metric",
            type=MetricType.COUNTER,
            value=42.0,
            tags={"env": "test"}
        )
        result = metric.to_dict()
        assert result["name"] == "test_metric"
        assert result["type"] == "counter"
        assert result["value"] == 42.0
        assert result["tags"] == {"env": "test"}


class TestPerformanceSnapshot:
    """Test PerformanceSnapshot dataclass."""
    
    def test_init(self):
        """Test PerformanceSnapshot initialization."""
        snapshot = PerformanceSnapshot(
            timestamp=1234567890.0,
            cpu_percent=50.0,
            memory_percent=60.0,
            memory_used_mb=1000.0,
            memory_available_mb=2000.0,
            disk_usage_percent=30.0,
            thread_count=5,
            open_files=10,
            network_connections=3
        )
        assert snapshot.timestamp == 1234567890.0
        assert snapshot.cpu_percent == 50.0
        assert snapshot.memory_percent == 60.0
        assert snapshot.memory_used_mb == 1000.0
        assert snapshot.thread_count == 5


class TestPerformanceMonitor:
    """Test PerformanceMonitor class."""
    
    def test_init(self):
        """Test PerformanceMonitor initialization."""
        monitor = PerformanceMonitor()
        assert monitor is not None