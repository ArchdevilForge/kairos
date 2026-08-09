#!/usr/bin/env python3
"""
Comprehensive Test Suite for Chain-Trace

Tests all major modules:
- Config management
- Cache management
- Suspicious detector
- Visualizer
- Twitter fetcher
"""

import sys
import json
import tempfile
from pathlib import Path
from typing import Dict, Any

# Add parent directory to path
sys.path.insert(0, str(Path(__file__).parent.parent))

# Test results
test_results = []


def test_config_management():
    """Test configuration management"""
    print("\n=== Testing Config Management ===")

    try:
        from scripts.config import ConfigManager, Config

        # Create temp config
        with tempfile.NamedTemporaryFile(mode='w', suffix='.json', delete=False) as f:
            config_path = Path(f.name)

        manager = ConfigManager(config_path)

        # Test default config
        config = manager.load()
        assert config.cache.enabled == True
        assert config.cache.ttl == 300  # Default is 300, not 3600
        print("✓ Default config loaded")

        # Test save
        config.cache.ttl = 7200
        manager.save(config)
        print("✓ Config saved")

        # Test reload
        manager2 = ConfigManager(config_path)
        config2 = manager2.load()
        assert config2.cache.ttl == 7200
        print("✓ Config reloaded correctly")

        # Cleanup
        config_path.unlink()

        test_results.append(("Config Management", True, None))
        return True

    except Exception as e:
        print(f"✗ Config management test failed: {e}")
        import traceback
        traceback.print_exc()
        test_results.append(("Config Management", False, str(e)))
        return False


def test_cache_management():
    """Test cache management"""
    print("\n=== Testing Cache Management ===")

    try:
        from scripts.cache_manager import CacheManager

        # Create temp cache
        with tempfile.TemporaryDirectory() as tmpdir:
            cache_path = Path(tmpdir) / "cache.json"
            cache = CacheManager(cache_path)

            # Test set/get
            cache.set('test', 'key1', {'value': 123}, ttl=60)
            result = cache.get('test', 'key1')
            assert result == {'value': 123}
            print("✓ Set/Get works")

            # Test namespace
            cache.set('test2', 'key1', {'value': 456}, ttl=60)
            result2 = cache.get('test2', 'key1')
            assert result2 == {'value': 456}
            assert cache.get('test', 'key1') == {'value': 123}
            print("✓ Namespace isolation works")

            # Test stats
            stats = cache.stats()
            assert stats['entries'] == 2
            print("✓ Stats work")

            # Test clear
            cache.clear('test')
            assert cache.get('test', 'key1') is None
            assert cache.get('test2', 'key1') == {'value': 456}
            print("✓ Clear works")

        test_results.append(("Cache Management", True, None))
        return True

    except Exception as e:
        print(f"✗ Cache management test failed: {e}")
        import traceback
        traceback.print_exc()
        test_results.append(("Cache Management", False, str(e)))
        return False


def test_suspicious_detector():
    """Test suspicious address detector"""
    print("\n=== Testing Suspicious Detector ===")

    try:
        from scripts.suspicious_detector import SuspiciousDetector

        detector = SuspiciousDetector()

        # Test data (simulating HODLAI holders)
        holders = [
            {
                'address': '0x7607...deb9',
                'balance': 1220000000000000000,
                'balance_pct': 1.22,
                'tx_count': 0,
                'bnb_balance': 0.001
            },
            {
                'address': '0x4ffb...f604',
                'balance': 1630000000000000000,
                'balance_pct': 1.63,
                'tx_count': 1,
                'bnb_balance': 0.002
            },
            {
                'address': '0xnormal...addr',
                'balance': 500000000000000000,
                'balance_pct': 0.5,
                'tx_count': 50,
                'bnb_balance': 1.5
            }
        ]

        # Detect suspicious
        suspicious = detector.detect(holders)

        # Should detect 2 suspicious holders
        assert len(suspicious) == 2
        print(f"✓ Detected {len(suspicious)} suspicious holders")

        # Check Holder6 (0 tx)
        holder6 = next((s for s in suspicious if s.address == '0x7607...deb9'), None)
        assert holder6 is not None
        assert holder6.risk_score >= 80  # Should be high risk
        print(f"✓ Holder6 detected with risk score {holder6.risk_score}")

        # Check Holder3 (1 tx)
        holder3 = next((s for s in suspicious if s.address == '0x4ffb...f604'), None)
        assert holder3 is not None
        assert holder3.risk_score >= 70  # Should be high risk
        print(f"✓ Holder3 detected with risk score {holder3.risk_score}")

        # Generate report
        report = detector.generate_report(suspicious)
        assert 'Suspicious Holders Detected' in report
        print("✓ Report generation works")

        test_results.append(("Suspicious Detector", True, None))
        return True

    except Exception as e:
        print(f"✗ Suspicious detector test failed: {e}")
        import traceback
        traceback.print_exc()
        test_results.append(("Suspicious Detector", False, str(e)))
        return False




def test_visualizer():
    """Test visualizer"""
    print("\n=== Testing Visualizer ===")

    try:
        from scripts.visualizer import Visualizer

        visualizer = Visualizer(width=60)

        # Test holder distribution
        holders = [
            {'address': '0xaaa', 'balance_pct': 15.5},
            {'address': '0xbbb', 'balance_pct': 10.2},
            {'address': '0xccc', 'balance_pct': 8.7}
        ]

        chart = visualizer.generate_holder_distribution(holders)
        assert 'Holder Distribution' in chart
        assert '0xaaa' in chart
        print("✓ Holder distribution chart works")

        # Test risk gauge
        gauge = visualizer.generate_risk_gauge(75.0, 85.0)
        assert 'Risk Assessment' in gauge
        assert '75.0' in gauge
        print("✓ Risk gauge works")

        # Test suspicious summary
        suspicious = [
            {'address': '0xsus1', 'risk_score': 90, 'flags': ['zero_tx', 'insufficient_gas']},
            {'address': '0xsus2', 'risk_score': 75, 'flags': ['single_tx']}
        ]

        summary = visualizer.generate_suspicious_summary(suspicious)
        assert 'Suspicious Holders' in summary
        assert '0xsus1' in summary
        print("✓ Suspicious summary works")

        # Test timeline
        events = [
            {'timestamp': '2024-01-01T00:00:00Z', 'type': 'deploy', 'description': 'Contract deployed'},
            {'timestamp': '2024-01-02T00:00:00Z', 'type': 'launch', 'description': 'Token launched'}
        ]

        timeline = visualizer.generate_timeline(events)
        assert 'Timeline' in timeline
        assert 'deploy' in timeline
        print("✓ Timeline visualization works")

        # Test full report
        analysis_results = {
            'risk_scores': {'risk_score': 65.0, 'confidence_score': 80.0},
            'suspicious': {'holders': suspicious},
            'holders': holders
        }

        report = visualizer.generate_full_report(analysis_results)
        assert 'Risk Assessment' in report
        print("✓ Full report generation works")

        test_results.append(("Visualizer", True, None))
        return True

    except Exception as e:
        print(f"✗ Visualizer test failed: {e}")
        import traceback
        traceback.print_exc()
        test_results.append(("Visualizer", False, str(e)))
        return False




def test_twitter_fetcher():
    """Test Twitter fetcher (basic functionality only)"""
    print("\n=== Testing Twitter Fetcher ===")

    try:
        from scripts.fetch_twitter import TwitterFetcher, TweetData

        fetcher = TwitterFetcher(cache_enabled=False)

        # Test tweet data parsing
        raw_data = {
            'text': 'Test tweet with #hashtag and @mention https://example.com',
            'author': 'testuser',
            'timestamp': '2024-01-01T00:00:00Z',
            'likes': 10,
            'retweets': 5
        }

        tweet = fetcher._parse_tweet_data(raw_data, '123456')
        assert tweet is not None
        assert tweet.tweet_id == '123456'
        assert tweet.author == 'testuser'
        assert len(tweet.hashtags) == 1
        assert len(tweet.mentions) == 1
        assert len(tweet.urls) == 1
        print("✓ Tweet data parsing works")

        # Test timeline analysis
        tweets = [
            TweetData(
                tweet_id='1',
                author='user1',
                text='Test #crypto tweet',
                hashtags=['crypto'],
                mentions=[],
                urls=[]
            ),
            TweetData(
                tweet_id='2',
                author='user1',
                text='Another #crypto #defi tweet @someone',
                hashtags=['crypto', 'defi'],
                mentions=['someone'],
                urls=['https://example.com']
            )
        ]

        analysis = fetcher.analyze_timeline(tweets)
        assert analysis.total_tweets == 2
        assert len(analysis.top_hashtags) > 0
        assert analysis.top_hashtags[0][0] == 'crypto'
        assert analysis.top_hashtags[0][1] == 2
        print("✓ Timeline analysis works")

        test_results.append(("Twitter Fetcher", True, None))
        return True

    except Exception as e:
        print(f"✗ Twitter fetcher test failed: {e}")
        import traceback
        traceback.print_exc()
        test_results.append(("Twitter Fetcher", False, str(e)))
        return False


def print_summary():
    """Print test summary"""
    print("\n" + "=" * 60)
    print("TEST SUMMARY")
    print("=" * 60)

    passed = sum(1 for _, success, _ in test_results if success)
    failed = sum(1 for _, success, _ in test_results if not success)
    total = len(test_results)

    for name, success, error in test_results:
        status = "✓ PASS" if success else "✗ FAIL"
        print(f"{status:8s} {name}")
        if error:
            print(f"         Error: {error}")

    print("\n" + "-" * 60)
    print(f"Total: {total} | Passed: {passed} | Failed: {failed}")
    print(f"Success Rate: {passed/total*100:.1f}%")
    print("=" * 60)

    return failed == 0


def main():
    print("=" * 60)
    print("CHAIN-TRACE COMPREHENSIVE TEST SUITE")
    print("=" * 60)

    # Run all tests
    test_config_management()
    test_cache_management()
    test_suspicious_detector()
    test_visualizer()
    test_twitter_fetcher()

    # Print summary
    all_passed = print_summary()

    sys.exit(0 if all_passed else 1)


if __name__ == "__main__":
    main()
