#!/usr/bin/env python3
"""Smoke: turn extract observability (log + Prometheus)."""
from __future__ import annotations

import json
import os
import re
import time
import urllib.request
from pathlib import Path

BASE = os.environ.get("PORTAL_BASE", "http://127.0.0.1:8000")
TOKEN = os.environ.get("PORTAL_TOKEN", "dev-bootstrap-token")
ORG = os.environ.get("PORTAL_ORG", "default")
OPS_AGENT = "b880051a-a7de-4d91-afea-2ad41269191c"
LOG = Path(r"d:\workspace\github\sixath\portal\bin\backend_e2e.err.log")
# extract uses std log -> often stderr; also check out
LOGS = [
    Path(r"d:\workspace\github\sixath\portal\bin\backend_e2e.err.log"),
    Path(r"d:\workspace\github\sixath\portal\bin\backend_e2e.out.log"),
]


def api(method: str, path: str, body=None, timeout=240):
    data = None
    headers = {"Authorization": f"Bearer {TOKEN}", "X-Org-Id": ORG}
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        raw = resp.read().decode("utf-8")
        return json.loads(raw) if raw else {}


def metrics_text() -> str:
    req = urllib.request.Request(BASE + "/metrics")
    with urllib.request.urlopen(req, timeout=15) as resp:
        return resp.read().decode("utf-8")


def parse_extract_metrics(text: str) -> dict:
    out = {"turns": {}, "candidates": 0.0, "written": 0.0, "drops": {}}
    for line in text.splitlines():
        if line.startswith("#") or "memory_extract" not in line:
            continue
        if line.startswith("memory_extract_turns_total{"):
            m = re.search(r'result="([^"]+)".*\s([0-9.]+)$', line)
            if m:
                out["turns"][m.group(1)] = float(m.group(2))
        elif line.startswith("memory_extract_candidates_total "):
            out["candidates"] = float(line.rsplit(" ", 1)[-1])
        elif line.startswith("memory_extract_written_total "):
            out["written"] = float(line.rsplit(" ", 1)[-1])
        elif line.startswith("memory_extract_drop_total{"):
            m = re.search(r'reason="([^"]+)".*\s([0-9.]+)$', line)
            if m:
                out["drops"][m.group(1)] = float(m.group(2))
    return out


def find_log(sid: str) -> str:
    needle = f"memory extract done session_id={sid}"
    for p in LOGS:
        if not p.exists():
            continue
        text = p.read_text(encoding="utf-8", errors="replace").replace("\r\n", "\n")
        for line in text.splitlines():
            if needle in line:
                return line
    return ""


def turns_total(m: dict) -> float:
    return float(sum(m.get("turns", {}).values()))


def main():
    before = parse_extract_metrics(metrics_text())
    print("BEFORE", json.dumps(before, ensure_ascii=False))

    sess = api("POST", f"/api/v1/agents/{OPS_AGENT}/sessions", {})
    sid = sess.get("id")
    if not sid:
        raise SystemExit(f"session create failed: {sess}")
    print("SESSION", sid)

    marker = f"OBS_SMOKE_{int(time.time())}"
    msg = (
        f"请记住：我的代号是 {marker}，我长期偏好用绿茶（不是咖啡）。"
        "只需简短确认即可，不要调用工具。"
    )
    reply = api("POST", f"/api/v1/sessions/{sid}/messages", {"content": msg}, timeout=240)
    content = reply.get("content") or ""
    print("REPLY", content[:200].encode("utf-8", "replace").decode("ascii", "replace"))

    log_line = ""
    after = before
    for _ in range(45):
        time.sleep(1)
        log_line = find_log(sid)
        after = parse_extract_metrics(metrics_text())
        if log_line:
            break

    print("AFTER", json.dumps(after, ensure_ascii=False))
    print("LOG", log_line or "<missing>")

    metrics_ok = turns_total(after) > turns_total(before) or after.get("candidates", 0) > before.get(
        "candidates", 0
    )
    log_ok = bool(log_line) and "result=" in log_line
    ok = log_ok and metrics_ok
    print("SMOKE", "PASS" if ok else "FAIL", f"log_ok={log_ok} metrics_ok={metrics_ok}")
    if not ok:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
