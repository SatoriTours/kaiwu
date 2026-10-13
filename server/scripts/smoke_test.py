#!/usr/bin/env python3
"""Smoke-test the real server on loopback; no Agent or model is invoked."""

import argparse
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


def smoke(binary: Path, stop_signal: signal.Signals) -> None:
    # Production configuration forbids port 0. Reserve a candidate, then release
    # it before launch; a bind race is a visible test failure, not an auto-retry.
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    address = f"127.0.0.1:{port}"
    env = os.environ.copy()
    env["KAIWU_LISTEN_ADDR"] = address
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    with tempfile.TemporaryFile() as stderr:
        proc = subprocess.Popen(
            [str(binary)], env=env, stdout=subprocess.DEVNULL, stderr=stderr
        )
        try:
            deadline = time.monotonic() + 8
            while True:
                if proc.poll() is not None:
                    raise RuntimeError(f"server exited before health: {proc.returncode}")
                if time.monotonic() >= deadline:
                    raise RuntimeError("timed out waiting for health")
                try:
                    with opener.open(f"http://{address}/healthz", timeout=0.5) as response:
                        if response.status != 200:
                            raise RuntimeError(f"health status: {response.status}")
                        if response.headers.get("Content-Type") != "application/json":
                            raise RuntimeError("health Content-Type mismatch")
                        if response.headers.get("Cache-Control") != "no-store":
                            raise RuntimeError("health Cache-Control mismatch")
                        body = json.loads(response.read(65536))
                        if body != {"status": "ok", "service": "kaiwu-server"}:
                            raise RuntimeError(f"health body mismatch: {body!r}")
                    break
                except urllib.error.HTTPError:
                    raise
                except (urllib.error.URLError, TimeoutError):
                    time.sleep(0.02)

            proc.send_signal(stop_signal)
            if proc.wait(timeout=6) != 0:
                raise RuntimeError(f"shutdown exit code: {proc.returncode}")
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=0.5):
                    pass
            except OSError:
                pass
            else:
                raise RuntimeError("listener remained open after process exit")

            stderr.seek(0)
            entries = [json.loads(line) for line in stderr.read().splitlines()]
            if not any(e.get("msg") == "server started" and e.get("listen_addr") == address for e in entries):
                raise RuntimeError("missing structured startup log")
            if not any(e.get("msg") == "server stopped" for e in entries):
                raise RuntimeError("missing structured stop log")
            print(f"PASS {stop_signal.name}: health, exit=0, listener released")
        except Exception:
            stderr.seek(0)
            print(stderr.read()[-8192:].decode("utf-8", errors="replace"), file=sys.stderr)
            raise
        finally:
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=6)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait(timeout=3)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    args = parser.parse_args()
    try:
        binary = args.binary.resolve(strict=True)
        smoke(binary, signal.SIGINT)
        smoke(binary, signal.SIGTERM)
    except Exception as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
