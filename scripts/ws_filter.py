#!/usr/bin/env python3
"""Stream and filter chat frames from stdin."""

import json
import os
import sys

PLATFORM = os.environ.get("PLATFORM", "").strip().lower()

def emit(payload: dict) -> None:
    source = str(payload.get("source") or payload.get("Source") or "").strip()
    author = str(payload.get("author") or payload.get("Author") or "").strip()
    message = str(payload.get("message") or payload.get("Message") or "").strip()
    print(f"[{source or '?'}] {author}: {message}")

for raw in sys.stdin:
    raw = raw.strip()
    if not raw or raw == "__keepalive__":
        continue
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError:
        continue
    source = str(payload.get("source") or payload.get("Source") or "").strip().lower()
    if PLATFORM and source != PLATFORM:
        continue
    emit(payload)
