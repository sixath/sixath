# -*- coding: utf-8 -*-
"""Update issue #7 screenshots section + comment with release asset links."""
from __future__ import annotations

import json
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(r"d:\workspace\github\sixath\_neo4j_q")
SRC = ROOT / "issue_web_search_topic_drift.md"
PATCH = ROOT / "issue7_patch.json"

FULL = "https://github.com/sixath/portal/releases/download/untagged-edd8f1089578a956e957/topic-drift-full.png"
SPLICE = "https://github.com/sixath/portal/releases/download/untagged-edd8f1089578a956e957/topic-drift-splice.png"

screenshots = f"""

## Screenshots

代码分析收束后硬接「7天无理由退货」：

- 全貌：[{FULL}]({FULL})
- 拼接处特写：[{SPLICE}]({SPLICE})

> 私有仓库的 Release 资源无法被 GitHub camo 内嵌渲染；原图已上传到 draft release `issue-7-screenshots`。若需 issue 内联预览，请在网页评论框拖入本地文件：
> `d:\\workspace\\github\\sixath\\_neo4j_q\\issue7_assets\\`
"""

comment = f"""## 截图附件

原图已挂到 draft release [`issue-7-screenshots`](https://github.com/sixath/portal/releases/tag/untagged-edd8f1089578a956e957)：

1. [topic-drift-full.png]({FULL}) — 代码分析后硬接退货政策全貌
2. [topic-drift-splice.png]({SPLICE}) — `# 7天无理由退货政策` 拼接处特写

本地副本：`_neo4j_q/issue7_assets/`
"""


def gh_json(args: list[str], input_path: pathlib.Path | None = None) -> subprocess.CompletedProcess:
    cmd = ["gh", "api", *args]
    if input_path is not None:
        cmd.extend(["--input", str(input_path)])
    return subprocess.run(cmd, capture_output=True)


def main() -> int:
    body = SRC.read_text(encoding="utf-8").rstrip() + screenshots
    PATCH.write_text(json.dumps({"body": body}, ensure_ascii=False), encoding="utf-8")
    r = gh_json(["--method", "PATCH", "repos/sixath/portal/issues/7"], PATCH)
    if r.returncode != 0:
        print(r.stderr.decode("utf-8", errors="replace"), file=sys.stderr)
        return r.returncode

    cpath = ROOT / "issue7_comment.json"
    cpath.write_text(json.dumps({"body": comment}, ensure_ascii=False), encoding="utf-8")
    r2 = gh_json(["--method", "POST", "repos/sixath/portal/issues/7/comments"], cpath)
    if r2.returncode != 0:
        print(r2.stderr.decode("utf-8", errors="replace"), file=sys.stderr)
        return r2.returncode

    data = json.loads(r2.stdout.decode("utf-8"))
    print("comment_url", data.get("html_url"))
    print("issue", "https://github.com/sixath/portal/issues/7")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
