"""
Cache manager with TTL-based caching for RPC calls.

Features:
- TTL-based expiration
- Size-based eviction (LRU)
- Persistent storage
- Thread-safe operations
"""

import json
import time
import hashlib
from pathlib import Path
from typing import Optional, Any, Dict
from dataclasses import dataclass, asdict
from threading import Lock
import os


@dataclass
class CacheEntry:
    """Cache entry with metadata"""
    key: str
    namespace: str  # Added to support namespace-based clearing
    value: Any
    timestamp: float
    ttl: int
    size_bytes: int


class CacheManager:
    """
    TTL-based cache manager with size limits.
    
    Features:
    - Automatic expiration
    - LRU eviction when size limit exceeded
    - Persistent storage
    """
    
    def __init__(
        self,
        cache_dir: Optional[Path] = None,
        ttl: int = 300,
        max_size_mb: int = 100
    ):
        """
        Initialize cache manager.
        
        Args:
            cache_dir: Cache directory (default: ~/.chain-trace/cache)
            ttl: Default TTL in seconds
            max_size_mb: Maximum cache size in MB
        """
        self.cache_dir = cache_dir or Path.home() / ".chain-trace" / "cache"
        self.cache_dir.mkdir(parents=True, exist_ok=True)
        
        self.ttl = ttl
        self.max_size_bytes = max_size_mb * 1024 * 1024
        self.lock = Lock()
        
        # In-memory index
        self.index: Dict[str, CacheEntry] = {}
        self._load_index()
    
    def _load_index(self):
        """Load cache index from disk"""
        index_file = self.cache_dir / "index.json"
        
        if not index_file.exists():
            return
        
        try:
            with open(index_file, 'r') as f:
                data = json.load(f)
            
            for entry_data in data.get('entries', []):
                entry = CacheEntry(**entry_data)
                self.index[entry.key] = entry
        except Exception as e:
            print(f"[CacheManager] Error loading index: {e}")
    
    def _save_index(self):
        """Save cache index to disk"""
        index_file = self.cache_dir / "index.json"
        
        data = {
            'entries': [asdict(entry) for entry in self.index.values()]
        }
        
        with open(index_file, 'w') as f:
            json.dump(data, f)
    
    def _make_key(self, namespace: str, key: str) -> str:
        """Generate cache key"""
        combined = f"{namespace}:{key}"
        return hashlib.md5(combined.encode()).hexdigest()
    
    def _get_file_path(self, cache_key: str) -> Path:
        """Get file path for cache key"""
        return self.cache_dir / f"{cache_key}.json"
    
    def _is_expired(self, entry: CacheEntry) -> bool:
        """Check if entry is expired"""
        return time.time() - entry.timestamp > entry.ttl
    
    def _evict_if_needed(self):
        """Evict entries if cache size exceeds limit"""
        total_size = sum(e.size_bytes for e in self.index.values())
        
        if total_size <= self.max_size_bytes:
            return
        
        # Sort by timestamp (LRU)
        sorted_entries = sorted(
            self.index.values(),
            key=lambda e: e.timestamp
        )
        
        # Evict oldest entries
        for entry in sorted_entries:
            if total_size <= self.max_size_bytes * 0.8:  # Target 80%
                break
            
            self._delete_entry(entry.key)
            total_size -= entry.size_bytes
    
    def _delete_entry(self, cache_key: str):
        """Delete cache entry"""
        if cache_key in self.index:
            file_path = self._get_file_path(cache_key)
            if file_path.exists():
                file_path.unlink()
            del self.index[cache_key]
    
    def get(
        self,
        namespace: str,
        key: str
    ) -> Optional[Any]:
        """
        Get value from cache.
        
        Args:
            namespace: Cache namespace (e.g., "rpc", "api")
            key: Cache key
        
        Returns:
            Cached value or None if not found/expired
        """
        cache_key = self._make_key(namespace, key)
        
        with self.lock:
            entry = self.index.get(cache_key)
            
            if entry is None:
                return None
            
            # Check expiration
            if self._is_expired(entry):
                self._delete_entry(cache_key)
                return None
            
            # Load value from disk
            file_path = self._get_file_path(cache_key)
            
            if not file_path.exists():
                self._delete_entry(cache_key)
                return None
            
            try:
                with open(file_path, 'r') as f:
                    data = json.load(f)
                return data['value']
            except Exception as e:
                print(f"[CacheManager] Error reading cache: {e}")
                self._delete_entry(cache_key)
                return None
    
    def set(
        self,
        namespace: str,
        key: str,
        value: Any,
        ttl: Optional[int] = None
    ):
        """
        Set value in cache.
        
        Args:
            namespace: Cache namespace
            key: Cache key
            value: Value to cache
            ttl: TTL in seconds (default: use manager TTL)
        """
        cache_key = self._make_key(namespace, key)
        ttl = ttl or self.ttl
        
        # Serialize value
        data = {'value': value}
        serialized = json.dumps(data, default=str)
        size_bytes = len(serialized.encode())
        
        with self.lock:
            # Create entry
            entry = CacheEntry(
                key=cache_key,
                namespace=namespace,  # Store namespace for clearing
                value=value,
                timestamp=time.time(),
                ttl=ttl,
                size_bytes=size_bytes
            )
            
            # Save to disk
            file_path = self._get_file_path(cache_key)
            with open(file_path, 'w') as f:
                f.write(serialized)
            
            # Update index
            self.index[cache_key] = entry
            
            # Evict if needed
            self._evict_if_needed()
            
            # Save index
            self._save_index()
    
    def clear(self, namespace: Optional[str] = None):
        """
        Clear cache.

        Args:
            namespace: Clear specific namespace (default: clear all)
        """
        with self.lock:
            if namespace is None:
                # Clear all
                for entry in list(self.index.values()):
                    self._delete_entry(entry.key)
            else:
                # Clear namespace - check entry.namespace instead of key prefix
                for entry in list(self.index.values()):
                    if entry.namespace == namespace:
                        self._delete_entry(entry.key)

            self._save_index()
    
    def stats(self) -> Dict[str, Any]:
        """Get cache statistics"""
        with self.lock:
            total_size = sum(e.size_bytes for e in self.index.values())
            
            return {
                'entries': len(self.index),
                'size_bytes': total_size,
                'size_mb': total_size / (1024 * 1024),
                'max_size_mb': self.max_size_bytes / (1024 * 1024),
                'utilization': total_size / self.max_size_bytes if self.max_size_bytes > 0 else 0
            }


# Global cache instance
_cache_manager: Optional[CacheManager] = None


def get_cache() -> CacheManager:
    """Get global cache instance"""
    global _cache_manager
    if _cache_manager is None:
        from scripts.config import get_config
        config = get_config()
        
        cache_dir = Path(config.cache.directory).expanduser()
        
        _cache_manager = CacheManager(
            cache_dir=cache_dir,
            ttl=config.cache.ttl,
            max_size_mb=config.cache.max_size_mb
        )
    
    return _cache_manager


# CLI
if __name__ == "__main__":
    import argparse
    
    parser = argparse.ArgumentParser(description="Cache manager")
    parser.add_argument("--stats", action="store_true", help="Show cache stats")
    parser.add_argument("--clear", action="store_true", help="Clear cache")
    parser.add_argument("--namespace", help="Namespace to clear")
    args = parser.parse_args()
    
    cache = get_cache()
    
    if args.stats:
        stats = cache.stats()
        print("=== Cache Statistics ===")
        print(f"Entries: {stats['entries']}")
        print(f"Size: {stats['size_mb']:.2f} MB / {stats['max_size_mb']:.2f} MB")
        print(f"Utilization: {stats['utilization']*100:.1f}%")
    
    elif args.clear:
        cache.clear(namespace=args.namespace)
        print(f"Cache cleared{' (namespace: ' + args.namespace + ')' if args.namespace else ''}")
    
    else:
        parser.print_help()
