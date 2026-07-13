#!/usr/bin/env python3
"""Batch translate ai-agent-classics papers via arxiv-translate."""

from __future__ import annotations

import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PDF_DIR = ROOT / "docs" / "papers" / "ai-agent-classics"
OUT_DIR = PDF_DIR / "zh"
ARXIV_ID_RE = re.compile(r"arxiv[:\s]*(\d{4}\.\d{4,5})", re.I)


def extract_arxiv_id(pdf: Path) -> str:
    try:
        import pypdf
    except ImportError:
        subprocess.check_call([sys.executable, "-m", "pip", "install", "pypdf", "-q"])
        import pypdf

    reader = pypdf.PdfReader(str(pdf))
    meta = reader.metadata or {}
    text = ""
    for i in range(min(3, len(reader.pages))):
        text += reader.pages[i].extract_text() or ""
    matches = ARXIV_ID_RE.findall(str(meta) + " " + text)
    if not matches:
        raise ValueError(f"Cannot find arXiv ID in {pdf.name}")
    return matches[0]


def build_jobs() -> list[tuple[str, str, Path]]:
    jobs: list[tuple[str, str, Path]] = []
    for pdf in sorted(PDF_DIR.glob("*.pdf")):
        arxiv_id = extract_arxiv_id(pdf)
        slug = pdf.stem
        target = OUT_DIR / slug
        jobs.append((arxiv_id, slug, target))
    return jobs


def find_done_marker(target: Path, arxiv_id: str) -> Path | None:
    root = target / arxiv_id
    if not root.exists():
        return None
    matches = list(root.rglob("main_translated.tex"))
    return matches[0] if matches else None


def translate_one(arxiv_id: str, slug: str, target: Path) -> int:
    target.mkdir(parents=True, exist_ok=True)
    if find_done_marker(target, arxiv_id):
        print(f"[skip] {slug} already translated")
        return 0

    key = os.environ.get("OPENAI_API_KEY", "")
    endpoint = os.environ.get(
        "OPENAI_BASE_URL", "http://10.86.3.248:3000/v1"
    ).rstrip("/") + "/chat/completions"
    model = os.environ.get("ARXIV_TRANSLATE_MODEL", "Qwen3.6-35B-A3B-FP8-Instruct")

    env = os.environ.copy()
    env.setdefault("PYTHONUTF8", "1")
    env.setdefault("PYTHONIOENCODING", "utf-8")

    cmd = [
        "arxiv-translate",
        "translate",
        arxiv_id,
        "-o",
        str(target),
        "--no-compile",
        "--sdk",
        "openai",
        "--model",
        model,
        "--endpoint",
        endpoint,
        "--key",
        key,
        "-c",
        os.environ.get("ARXIV_TRANSLATE_CONCURRENCY", "8"),
    ]
    print(f"[run] {slug} ({arxiv_id})")
    return subprocess.call(cmd, env=env)


def main() -> int:
    if not PDF_DIR.exists():
        print(f"Missing directory: {PDF_DIR}", file=sys.stderr)
        return 1
    if not os.environ.get("OPENAI_API_KEY"):
        print("OPENAI_API_KEY is required", file=sys.stderr)
        return 1

    jobs = build_jobs()
    print(f"Found {len(jobs)} papers")
    for arxiv_id, slug, target in jobs:
        code = translate_one(arxiv_id, slug, target)
        if code != 0:
            print(f"[fail] {slug} exit={code}", file=sys.stderr)
            return code
    print("All done")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
