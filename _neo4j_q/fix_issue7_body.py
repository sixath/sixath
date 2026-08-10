# -*- coding: utf-8 -*-
"""Restore portal issue #7 body with correct UTF-8 and screenshot section."""
from __future__ import annotations

import json
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(r"d:\workspace\github\sixath\_neo4j_q")
SRC = ROOT / "issue_web_search_topic_drift.md"
PATCH = ROOT / "issue7_patch.json"

screenshots = """

## Screenshots

代码分析收束后硬接「7天无理由退货」全貌与拼接处见下方评论附图。

Draft release（原图文件）: https://github.com/sixath/portal/releases/tag/untagged-edd8f1089578a956e957
"""


def main() -> int:
    src = SRC.read_text(encoding="utf-8").rstrip()
    print("src_has_cn", "对话中完成" in src)
    body = src + screenshots
    PATCH.write_text(
        json.dumps({"body": body}, ensure_ascii=False),
        encoding="utf-8",
    )
    print("wrote", PATCH, "bytes", PATCH.stat().st_size)

    # Use --input so gh sends UTF-8 JSON without PowerShell re-encoding.
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
        text=True,
        encoding="utf-8",
    )
    if r.returncode != 0:
        print("PATCH failed", r.returncode, r.stderr, file=sys.stderr)
        return r.returncode

    # Verify via API write to file
    out = ROOT / "issue7_body_live.md"
    v = subprocess.run(
        ["gh", "api", "repos/sixath/portal/issues/7", "--jq", ".body"],
        capture_output=True,
    )
    out.write_bytes(v.stdout)
    live = out.read_text(encoding="utf-8")
    print("live_has_cn", "对话中完成" in live)
    print("live_has_screenshots", "## Screenshots" in live)
    print("live_has_mojibake", "鍦" in live[:200])
    print(live[:120].replace("\n", "\\n"))
    return 0 if ("对话中完成" in live) else 2


if __name__ == "__main__":
    raise SystemExit(main())
