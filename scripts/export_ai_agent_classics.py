#!/usr/bin/env python3
"""Export translated ai-agent-classics papers to Markdown and PDF."""

from __future__ import annotations

import re
import sys
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ZH_DIR = ROOT / "docs" / "papers" / "ai-agent-classics" / "zh"

# Windows fallback fonts when Source Han is unavailable
WIN_CJK_FONTS = {
    "Source Han Serif SC": "SimSun",
    "Source Han Sans SC": "Microsoft YaHei",
    "Source Han Mono SC": "FangSong",
}


def find_translated_tex() -> list[tuple[str, Path, Path]]:
    items: list[tuple[str, Path, Path]] = []
    for paper_dir in sorted(p for p in ZH_DIR.iterdir() if p.is_dir()):
        matches = list(paper_dir.rglob("main_translated.tex"))
        if not matches:
            continue
        tex = matches[0]
        work_dir = tex.parent
        items.append((paper_dir.name, tex, work_dir))
    return items


def patch_fonts_for_windows(tex: str) -> str:
    for src, dst in WIN_CJK_FONTS.items():
        tex = tex.replace(src, dst)
    return tex


def extract_document_body(tex: str) -> str:
    match = re.search(r"\\begin\{document\}(.*?)\\end\{document\}", tex, re.S)
    if not match:
        return tex
    return match.group(1).strip()


def clean_markdown(text: str) -> str:
    text = re.sub(r"\\(?:model|act|reason|palm|palmflan|myparagraph)\b", "", text)
    text = re.sub(r"\\(?:citet|citep|cite)\*?\{[^}]*\}", "", text)
    text = re.sub(r"<cit\.>", "", text)
    text = re.sub(r"<ref>", "", text)
    text = re.sub(r"\[NO \\(?:title|author) GIVEN\]\s*", "", text)
    text = re.sub(r"\\[a-zA-Z@]+\*?", "", text)
    text = re.sub(r"[ \t]+\n", "\n", text)
    text = re.sub(r"\n{3,}", "\n\n", text)
    return text.strip()


def extract_title(tex: str) -> str:
    m = re.search(r"\\title\{([^}]*)\}", tex, re.S)
    if not m:
        return ""
    raw = m.group(1)
    try:
        from pylatexenc.latex2text import LatexNodes2Text

        title = LatexNodes2Text().latex_to_text(raw)
    except Exception:
        title = _fallback_latex_to_text(raw)
    return clean_markdown(title)


def latex_to_markdown(body: str) -> str:
    from pylatexenc.latex2text import LatexNodes2Text

    try:
        text = LatexNodes2Text().latex_to_text(body)
    except Exception:
        text = _fallback_latex_to_text(body)

    text = re.sub(r"\[\[(?:REF|LABEL|CITE)_\d+\]\]", "", text)
    text = re.sub(r"\n{3,}", "\n\n", text)
    return text.strip()


def _fallback_latex_to_text(body: str) -> str:
    text = body
    for level, cmd in [
        (1, "section"),
        (2, "subsection"),
        (3, "subsubsection"),
        (4, "paragraph"),
    ]:
        prefix = "#" * level
        text = re.sub(
            rf"\\{cmd}\*?\{{([^{{}}]*)\}}",
            lambda m, p=prefix: f"\n{p} {m.group(1).strip()}\n",
            text,
        )
    text = re.sub(r"\\(?:textbf|emph|textit)\{([^{}]*)\}", r"\1", text)
    text = re.sub(r"\\[a-zA-Z@]+\*?(?:\[[^\]]*\])?(?:\{[^{}]*\})*", " ", text)
    text = re.sub(r"[{}]", "", text)
    text = re.sub(r"[ \t]+", " ", text)
    text = re.sub(r"\n{3,}", "\n\n", text)
    return text


def export_markdown(slug: str, tex_path: Path, out_path: Path) -> None:
    tex = tex_path.read_text(encoding="utf-8")
    title = extract_title(tex) or slug
    body = extract_document_body(tex)
    md_body = clean_markdown(latex_to_markdown(body))
    content = f"# {title}\n\n{md_body}\n"
    out_path.write_text(content, encoding="utf-8")
    print(f"[md] {out_path.name} ({out_path.stat().st_size // 1024} KB)")


def sanitize_for_pdf(md: str) -> str:
    text = md
    text = text.replace("\\&", " 和 ")
    text = re.sub(r"\\\\", "\n", text)
    text = re.sub(r"\\[a-zA-Z@]+\*?", "", text)
    text = re.sub(r"\$([^$]*)\$", r"\1", text)
    text = re.sub(r"\\\(([^)]*)\\\)", r"\1", text)
    text = re.sub(r"\\\[(.*?)\]", r"\1", text, flags=re.S)
    text = re.sub(r"(?<!\w)_+(?!\w)", "", text)
    return text


def export_pdf_from_markdown(md_path: Path, pdf_path: Path) -> bool:
    import shutil
    import subprocess
    import tempfile

    pandoc = shutil.which("pandoc")
    if not pandoc:
        print("[pdf-fail] pandoc not found", file=sys.stderr)
        return False

    source = md_path.read_text(encoding="utf-8")
    with tempfile.NamedTemporaryFile(
        "w", suffix=".md", delete=False, encoding="utf-8"
    ) as tmp:
        tmp.write(sanitize_for_pdf(source))
        tmp_path = Path(tmp.name)

    cmd = [
        pandoc,
        str(tmp_path),
        "-o",
        str(pdf_path),
        "--pdf-engine=xelatex",
        "-V",
        "CJKmainfont=SimSun",
        "-V",
        "geometry:margin=1in",
        "-V",
        "fontsize=11pt",
        "--metadata",
        "lang=zh-CN",
    ]
    try:
        proc = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=300,
            encoding="utf-8",
            errors="replace",
            env={**os.environ, "MIKTEX_NO_UPDATE_CHECK": "1"},
        )
    finally:
        tmp_path.unlink(missing_ok=True)

    if proc.returncode == 0 and pdf_path.exists():
        print(f"[pdf] {pdf_path.name} ({pdf_path.stat().st_size // 1024} KB)")
        return True

    err = (proc.stderr or proc.stdout or "pandoc failed")[:300]
    print(f"[pdf-fail] {md_path.stem}: {err}", file=sys.stderr)
    return False


def main() -> int:
    if not ZH_DIR.exists():
        print(f"Missing: {ZH_DIR}", file=sys.stderr)
        return 1

    items = find_translated_tex()
    if not items:
        print("No translated tex files found", file=sys.stderr)
        return 1

    md_ok = 0
    pdf_ok = 0
    for slug, tex_path, work_dir in items:
        md_path = ZH_DIR / f"{slug}.md"
        pdf_path = ZH_DIR / f"{slug}.pdf"
        try:
            export_markdown(slug, tex_path, md_path)
            md_ok += 1
        except Exception as exc:
            print(f"[md-fail] {slug}: {exc}", file=sys.stderr)

        if export_pdf_from_markdown(md_path, pdf_path):
            pdf_ok += 1

    print(f"Done: markdown={md_ok}/{len(items)}, pdf={pdf_ok}/{len(items)}")
    return 0 if md_ok == len(items) else 1


if __name__ == "__main__":
    raise SystemExit(main())
