# -*- coding: utf-8 -*-
"""Rewrite portal issue #7 body: root-cause first, web fail-closed as companion."""
from __future__ import annotations

import json
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(r"d:\workspace\github\sixath\_neo4j_q")
SRC = ROOT / "issue_web_search_topic_drift.md"
PATCH = ROOT / "issue7_patch.json"


def main() -> int:
    body = SRC.read_text(encoding="utf-8").rstrip() + "\n"
    print("src_has_cn", "治本" in body and "对话中完成" in body)
    PATCH.write_text(json.dumps({"body": body}, ensure_ascii=False), encoding="utf-8")
    r = subprocess.run(
        [
            "gh",
            "api",
            "--method",
            "PATCH",
            "repos/sixath/portal/issues/7",
            "--input",
            str(PATCH),
        ],
        capture_output=True,
    )
    if r.returncode != 0:
        sys.stderr.buffer.write(r.stderr)
        return r.returncode

    v = subprocess.run(
        ["gh", "api", "repos/sixath/portal/issues/7", "--jq", ".body"],
        capture_output=True,
    )
    live_path = ROOT / "issue7_body_live.md"
    live_path.write_bytes(v.stdout)
    live = live_path.read_text(encoding="utf-8")
    checks = {
        "cn": "对话中完成" in live,
        "root_first": "（治本）话题漂移" in live,
        "companion": "（治标 / 配套）注册旁路" in live,
        "p0": "P0 — 治本" in live,
        "p1": "P1 — 配套" in live,
        "accept_any_tool": "任意无关工具调用" in live,
    }
    print(checks)
    return 0 if all(checks.values()) else 2


if __name__ == "__main__":
    raise SystemExit(main())
