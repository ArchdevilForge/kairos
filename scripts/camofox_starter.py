#!/usr/bin/env python3
"""
Camofox Auto-Starter for chain-trace

Automatically starts Camofox browser service if needed for Twitter data collection.
"""

import subprocess
import time
import urllib.request
import urllib.error
import sys
import os
import signal
import atexit

CAMOFOX_PORT = 9377
CAMOFOX_URL = f"http://localhost:{CAMOFOX_PORT}/health"
CAMOFOX_PROCESS = None


def is_camofox_running() -> bool:
    """Check if Camofox is already running"""
    try:
        req = urllib.request.Request(CAMOFOX_URL, method='GET')
        with urllib.request.urlopen(req, timeout=2) as resp:
            return resp.status == 200
    except (urllib.error.URLError, ConnectionRefusedError, TimeoutError):
        return False


def start_camofox_background() -> subprocess.Popen:
    """Start Camofox in background using npx"""
    global CAMOFOX_PROCESS

    print(f"[Camofox] Starting browser service on port {CAMOFOX_PORT}...")

    # Use npx to run camofox-browser (no installation needed)
    CAMOFOX_PROCESS = subprocess.Popen(
        ["npx", "camofox-browser"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        preexec_fn=os.setsid if sys.platform != 'win32' else None
    )

    # Wait for service to be ready (max 30 seconds)
    for i in range(30):
        time.sleep(1)
        if is_camofox_running():
            print(f"[Camofox] ✅ Service ready on port {CAMOFOX_PORT}")
            return CAMOFOX_PROCESS

        # Check if process died
        if CAMOFOX_PROCESS.poll() is not None:
            stdout, stderr = CAMOFOX_PROCESS.communicate()
            print(f"[Camofox] ❌ Failed to start")
            print(f"[Camofox] stdout: {stdout.decode()}")
            print(f"[Camofox] stderr: {stderr.decode()}")
            return None

    print(f"[Camofox] ⚠️  Timeout waiting for service")
    return CAMOFOX_PROCESS


def stop_camofox():
    """Stop Camofox process"""
    global CAMOFOX_PROCESS

    if CAMOFOX_PROCESS and CAMOFOX_PROCESS.poll() is None:
        print("[Camofox] Stopping service...")

        if sys.platform == 'win32':
            CAMOFOX_PROCESS.terminate()
        else:
            # Kill entire process group
            os.killpg(os.getpgid(CAMOFOX_PROCESS.pid), signal.SIGTERM)

        try:
            CAMOFOX_PROCESS.wait(timeout=5)
            print("[Camofox] ✅ Service stopped")
        except subprocess.TimeoutExpired:
            CAMOFOX_PROCESS.kill()
            print("[Camofox] ⚠️  Force killed")


def ensure_camofox() -> bool:
    """Ensure Camofox is running, start if needed"""
    if is_camofox_running():
        print(f"[Camofox] ✅ Already running on port {CAMOFOX_PORT}")
        return True

    # Check if npx is available
    try:
        subprocess.run(["npx", "--version"], capture_output=True, check=True)
    except (subprocess.CalledProcessError, FileNotFoundError):
        print("[Camofox] ❌ npx not found. Please install Node.js:")
        print("  https://nodejs.org/")
        return False

    # Start Camofox
    process = start_camofox_background()

    if process and is_camofox_running():
        # Register cleanup on exit
        atexit.register(stop_camofox)
        return True

    return False


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="Camofox Auto-Starter")
    parser.add_argument("--check", action="store_true", help="Check if Camofox is running")
    parser.add_argument("--start", action="store_true", help="Start Camofox if not running")
    parser.add_argument("--stop", action="store_true", help="Stop Camofox")
    args = parser.parse_args()

    if args.check:
        if is_camofox_running():
            print(f"✅ Camofox is running on port {CAMOFOX_PORT}")
            sys.exit(0)
        else:
            print(f"❌ Camofox is not running")
            sys.exit(1)

    elif args.stop:
        stop_camofox()

    elif args.start:
        if ensure_camofox():
            print("✅ Camofox is ready")
            print("Press Ctrl+C to stop...")
            try:
                while True:
                    time.sleep(1)
            except KeyboardInterrupt:
                print("\n[Camofox] Shutting down...")
                stop_camofox()
        else:
            print("❌ Failed to start Camofox")
            sys.exit(1)

    else:
        parser.print_help()
