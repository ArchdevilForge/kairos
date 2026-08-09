#!/usr/bin/env python3
"""
Enhanced Twitter Data Fetcher with Auto-Camofox

Features:
- Auto-starts Camofox when needed
- Better error handling for JS-gated pages
- Timeline analysis (frequency, content themes)
- Graceful degradation when Camofox unavailable
- Caching support
"""

import sys
import os
import json
import re
from pathlib import Path
from typing import Optional, Dict, List, Any
from datetime import datetime
from dataclasses import dataclass, asdict

# Add x-tweet-fetcher to path
X_FETCHER_PATH = Path.home() / ".claude/skills/x-tweet-fetcher/scripts"
if not X_FETCHER_PATH.exists():
    X_FETCHER_PATH = Path.home() / ".agents/skills/x-tweet-fetcher/scripts"

if X_FETCHER_PATH.exists():
    sys.path.insert(0, str(X_FETCHER_PATH))

try:
    from camofox_starter import ensure_camofox
except ImportError:
    def ensure_camofox():
        return False


@dataclass
class TweetData:
    """Structured tweet data"""
    tweet_id: str
    author: str
    text: str
    timestamp: Optional[str] = None
    likes: Optional[int] = None
    retweets: Optional[int] = None
    replies: Optional[int] = None
    urls: List[str] = None
    mentions: List[str] = None
    hashtags: List[str] = None

    def __post_init__(self):
        if self.urls is None:
            self.urls = []
        if self.mentions is None:
            self.mentions = []
        if self.hashtags is None:
            self.hashtags = []


@dataclass
class TimelineAnalysis:
    """Timeline analysis results"""
    total_tweets: int
    date_range: Dict[str, str]  # first, last
    avg_tweets_per_day: float
    top_hashtags: List[tuple]  # (hashtag, count)
    top_mentions: List[tuple]  # (mention, count)
    url_domains: List[tuple]  # (domain, count)
    content_themes: List[str]


class TwitterFetcher:
    """Enhanced Twitter data fetcher"""

    def __init__(self, cache_enabled: bool = True):
        self.cache_enabled = cache_enabled
        self.cache = None

        if cache_enabled:
            try:
                from cache_manager import get_cache
                self.cache = get_cache()
            except ImportError:
                self.cache_enabled = False

    def fetch_tweet(self, url: str) -> Optional[TweetData]:
        """
        Fetch single tweet data.

        Args:
            url: Tweet URL

        Returns:
            TweetData or None
        """
        # Check cache
        if self.cache_enabled and self.cache:
            cached = self.cache.get('twitter', url)
            if cached:
                return TweetData(**cached)

        # Extract tweet ID
        tweet_id = self._extract_tweet_id(url)
        if not tweet_id:
            return None

        # Try x-tweet-fetcher
        try:
            import subprocess
            fetch_script = X_FETCHER_PATH / "fetch_tweet.py"

            result = subprocess.run(
                [sys.executable, str(fetch_script), '--url', url, '--json'],
                capture_output=True,
                text=True,
                check=False
            )

            if result.returncode == 0 and result.stdout:
                data = json.loads(result.stdout)
                tweet = self._parse_tweet_data(data, tweet_id)

                # Cache result
                if self.cache_enabled and self.cache and tweet:
                    self.cache.set('twitter', url, asdict(tweet), ttl=3600)

                return tweet

        except Exception as e:
            print(f"[Twitter] Error fetching tweet: {e}", file=sys.stderr)

        return None

    def fetch_user_timeline(
        self,
        username: str,
        limit: int = 50
    ) -> List[TweetData]:
        """
        Fetch user timeline.

        Args:
            username: Twitter username
            limit: Max tweets to fetch

        Returns:
            List of TweetData
        """
        # Check cache
        cache_key = f"{username}:{limit}"
        if self.cache_enabled and self.cache:
            cached = self.cache.get('twitter_timeline', cache_key)
            if cached:
                return [TweetData(**t) for t in cached]

        # Ensure Camofox is running
        if not ensure_camofox():
            print("[Twitter] Camofox required for timeline fetching", file=sys.stderr)
            return []

        # Fetch timeline
        try:
            import subprocess
            fetch_script = X_FETCHER_PATH / "fetch_tweet.py"

            result = subprocess.run(
                [sys.executable, str(fetch_script), '--user', username, '--limit', str(limit), '--json'],
                capture_output=True,
                text=True,
                check=False
            )

            if result.returncode == 0 and result.stdout:
                data = json.loads(result.stdout)
                tweets = []

                for item in data.get('tweets', []):
                    tweet = self._parse_tweet_data(item, item.get('id', ''))
                    if tweet:
                        tweets.append(tweet)

                # Cache result
                if self.cache_enabled and self.cache and tweets:
                    self.cache.set(
                        'twitter_timeline',
                        cache_key,
                        [asdict(t) for t in tweets],
                        ttl=1800  # 30 min
                    )

                return tweets

        except Exception as e:
            print(f"[Twitter] Error fetching timeline: {e}", file=sys.stderr)

        return []

    def analyze_timeline(self, tweets: List[TweetData]) -> TimelineAnalysis:
        """
        Analyze timeline for patterns.

        Args:
            tweets: List of TweetData

        Returns:
            TimelineAnalysis
        """
        if not tweets:
            return TimelineAnalysis(
                total_tweets=0,
                date_range={},
                avg_tweets_per_day=0.0,
                top_hashtags=[],
                top_mentions=[],
                url_domains=[],
                content_themes=[]
            )

        # Extract timestamps
        timestamps = [t.timestamp for t in tweets if t.timestamp]
        timestamps.sort()

        date_range = {}
        avg_per_day = 0.0

        if timestamps:
            date_range = {
                'first': timestamps[0],
                'last': timestamps[-1]
            }

            # Calculate avg tweets per day
            try:
                first_dt = datetime.fromisoformat(timestamps[0].replace('Z', '+00:00'))
                last_dt = datetime.fromisoformat(timestamps[-1].replace('Z', '+00:00'))
                days = (last_dt - first_dt).days + 1
                avg_per_day = len(tweets) / max(days, 1)
            except:
                pass

        # Count hashtags
        hashtag_counts = {}
        for tweet in tweets:
            for tag in tweet.hashtags:
                hashtag_counts[tag] = hashtag_counts.get(tag, 0) + 1

        top_hashtags = sorted(hashtag_counts.items(), key=lambda x: x[1], reverse=True)[:10]

        # Count mentions
        mention_counts = {}
        for tweet in tweets:
            for mention in tweet.mentions:
                mention_counts[mention] = mention_counts.get(mention, 0) + 1

        top_mentions = sorted(mention_counts.items(), key=lambda x: x[1], reverse=True)[:10]

        # Extract URL domains
        domain_counts = {}
        for tweet in tweets:
            for url in tweet.urls:
                domain = self._extract_domain(url)
                if domain:
                    domain_counts[domain] = domain_counts.get(domain, 0) + 1

        url_domains = sorted(domain_counts.items(), key=lambda x: x[1], reverse=True)[:10]

        # Detect content themes (simple keyword analysis)
        content_themes = self._detect_themes(tweets)

        return TimelineAnalysis(
            total_tweets=len(tweets),
            date_range=date_range,
            avg_tweets_per_day=round(avg_per_day, 2),
            top_hashtags=top_hashtags,
            top_mentions=top_mentions,
            url_domains=url_domains,
            content_themes=content_themes
        )

    def _extract_tweet_id(self, url: str) -> Optional[str]:
        """Extract tweet ID from URL"""
        match = re.search(r'/status/(\d+)', url)
        return match.group(1) if match else None

    def _extract_domain(self, url: str) -> Optional[str]:
        """Extract domain from URL"""
        match = re.search(r'https?://([^/]+)', url)
        return match.group(1) if match else None

    def _parse_tweet_data(self, data: Dict, tweet_id: str) -> Optional[TweetData]:
        """Parse raw tweet data into TweetData"""
        try:
            # Extract text
            text = data.get('text', '')

            # Extract URLs
            urls = re.findall(r'https?://[^\s]+', text)

            # Extract mentions
            mentions = re.findall(r'@(\w+)', text)

            # Extract hashtags
            hashtags = re.findall(r'#(\w+)', text)

            return TweetData(
                tweet_id=tweet_id,
                author=data.get('author', data.get('username', 'Unknown')),
                text=text,
                timestamp=data.get('timestamp', data.get('created_at')),
                likes=data.get('likes', data.get('favorite_count')),
                retweets=data.get('retweets', data.get('retweet_count')),
                replies=data.get('replies', data.get('reply_count')),
                urls=urls,
                mentions=mentions,
                hashtags=hashtags
            )
        except Exception as e:
            print(f"[Twitter] Error parsing tweet data: {e}", file=sys.stderr)
            return None

    def _detect_themes(self, tweets: List[TweetData]) -> List[str]:
        """Detect content themes from tweets"""
        # Simple keyword-based theme detection
        themes = []

        # Combine all text
        all_text = ' '.join(t.text.lower() for t in tweets)

        # Theme keywords
        theme_keywords = {
            'crypto': ['crypto', 'bitcoin', 'eth', 'token', 'defi', 'nft', 'blockchain'],
            'trading': ['trade', 'buy', 'sell', 'pump', 'moon', 'hodl', 'dip'],
            'launch': ['launch', 'presale', 'airdrop', 'mint', 'whitelist', 'ido', 'ico'],
            'community': ['community', 'join', 'discord', 'telegram', 'follow', 'giveaway'],
            'technical': ['audit', 'contract', 'liquidity', 'staking', 'yield', 'apr']
        }

        for theme, keywords in theme_keywords.items():
            if any(kw in all_text for kw in keywords):
                themes.append(theme)

        return themes


def needs_camofox(args: list) -> bool:
    """Check if the command needs Camofox"""
    return any(arg in args for arg in ['--user', '--replies', '-r', '--article', '-a'])


def main():
    args = sys.argv[1:]

    # Parse arguments
    if '--help' in args or '-h' in args:
        print("""
Enhanced Twitter Fetcher

Usage:
  fetch_twitter.py --url <tweet_url> [--json]
  fetch_twitter.py --user <username> [--limit N] [--analyze] [--json]

Options:
  --url URL         Fetch single tweet
  --user USERNAME   Fetch user timeline (requires Camofox)
  --limit N         Max tweets to fetch (default: 50)
  --analyze         Analyze timeline patterns
  --json            Output JSON format
  --no-cache        Disable caching
  -h, --help        Show this help

Examples:
  # Single tweet
  fetch_twitter.py --url https://x.com/user/status/123456

  # User timeline
  fetch_twitter.py --user elonmusk --limit 100

  # Timeline analysis
  fetch_twitter.py --user elonmusk --limit 200 --analyze
""")
        sys.exit(0)

    # Check if Camofox is needed
    if needs_camofox(args):
        print("[Twitter Fetcher] This operation requires Camofox browser service")

        if not ensure_camofox():
            print("\n❌ Failed to start Camofox. Advanced features unavailable.")
            print("\nAlternatives:")
            print("  1. Install Node.js: https://nodejs.org/")
            print("  2. Use basic features only (single tweet without --replies)")
            sys.exit(1)

        print()  # Blank line for readability

    # Initialize fetcher
    cache_enabled = '--no-cache' not in args
    fetcher = TwitterFetcher(cache_enabled=cache_enabled)

    # Handle commands
    try:
        if '--url' in args:
            # Single tweet
            url_idx = args.index('--url') + 1
            url = args[url_idx]

            tweet = fetcher.fetch_tweet(url)

            if tweet:
                if '--json' in args:
                    print(json.dumps(asdict(tweet), indent=2))
                else:
                    print(f"Tweet ID: {tweet.tweet_id}")
                    print(f"Author: @{tweet.author}")
                    print(f"Text: {tweet.text}")
                    if tweet.timestamp:
                        print(f"Time: {tweet.timestamp}")
                    if tweet.likes is not None:
                        print(f"Likes: {tweet.likes}")
                    if tweet.retweets is not None:
                        print(f"Retweets: {tweet.retweets}")
                    if tweet.urls:
                        print(f"URLs: {', '.join(tweet.urls)}")
            else:
                print("❌ Failed to fetch tweet")
                sys.exit(1)

        elif '--user' in args:
            # User timeline
            user_idx = args.index('--user') + 1
            username = args[user_idx]

            limit = 50
            if '--limit' in args:
                limit_idx = args.index('--limit') + 1
                limit = int(args[limit_idx])

            tweets = fetcher.fetch_user_timeline(username, limit)

            if tweets:
                if '--analyze' in args:
                    # Analyze timeline
                    analysis = fetcher.analyze_timeline(tweets)

                    if '--json' in args:
                        print(json.dumps(asdict(analysis), indent=2))
                    else:
                        print(f"\n=== Timeline Analysis: @{username} ===\n")
                        print(f"Total Tweets: {analysis.total_tweets}")
                        if analysis.date_range:
                            print(f"Date Range: {analysis.date_range.get('first', 'N/A')} to {analysis.date_range.get('last', 'N/A')}")
                        print(f"Avg Tweets/Day: {analysis.avg_tweets_per_day}")

                        if analysis.top_hashtags:
                            print(f"\nTop Hashtags:")
                            for tag, count in analysis.top_hashtags[:5]:
                                print(f"  #{tag}: {count}")

                        if analysis.top_mentions:
                            print(f"\nTop Mentions:")
                            for mention, count in analysis.top_mentions[:5]:
                                print(f"  @{mention}: {count}")

                        if analysis.url_domains:
                            print(f"\nTop Domains:")
                            for domain, count in analysis.url_domains[:5]:
                                print(f"  {domain}: {count}")

                        if analysis.content_themes:
                            print(f"\nContent Themes: {', '.join(analysis.content_themes)}")

                elif '--json' in args:
                    print(json.dumps([asdict(t) for t in tweets], indent=2))
                else:
                    print(f"\n=== Timeline: @{username} ({len(tweets)} tweets) ===\n")
                    for i, tweet in enumerate(tweets[:10], 1):
                        print(f"{i}. [{tweet.timestamp or 'N/A'}] {tweet.text[:100]}...")
                        if i < len(tweets):
                            print()
            else:
                print(f"❌ Failed to fetch timeline for @{username}")
                sys.exit(1)

        else:
            # Fallback to original x-tweet-fetcher
            if not X_FETCHER_PATH.exists():
                print("❌ x-tweet-fetcher not found")
                print("Install: https://github.com/ythx-101/x-tweet-fetcher")
                sys.exit(1)

            import subprocess
            fetch_script = X_FETCHER_PATH / "fetch_tweet.py"

            result = subprocess.run(
                [sys.executable, str(fetch_script)] + args,
                check=False
            )

            sys.exit(result.returncode)

    except Exception as e:
        print(f"❌ Error: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    main()
