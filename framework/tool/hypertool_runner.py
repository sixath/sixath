"""HyperTool block runner — JSON-line protocol with Go host.

Host → runner (stdin):  {"type":"run","code":"..."}
Runner → host (stdout): {"type":"call","name":"...","arguments":{...}}
Host → runner (stdin):  {"type":"result","result":...} | {"type":"error","message":"..."}
Runner → host (stdout): {"type":"done","result":...} | {"type":"error","message":"..."}
"""
from __future__ import annotations

import json
import sys
from typing import Any


def _write(obj: dict[str, Any]) -> None:
    sys.stdout.write(json.dumps(obj, ensure_ascii=False) + "\n")
    sys.stdout.flush()


def _read() -> dict[str, Any] | None:
    line = sys.stdin.readline()
    if not line:
        return None
    line = line.strip()
    if not line:
        return None
    return json.loads(line)


def call_tool(name: str, arguments: dict[str, Any] | None = None) -> Any:
    _write({"type": "call", "name": name, "arguments": arguments or {}})
    resp = _read()
    if resp is None:
        raise RuntimeError("hypertool: host closed connection")
    if resp.get("type") == "error":
        raise RuntimeError(resp.get("message") or "tool call failed")
    if resp.get("type") != "result":
        raise RuntimeError("hypertool: unexpected host response")
    return resp.get("result")


def main() -> None:
    msg = _read()
    if not msg or msg.get("type") != "run":
        _write({"type": "error", "message": "expected run message"})
        return
    code = msg.get("code")
    if not isinstance(code, str) or not code.strip():
        _write({"type": "error", "message": "code must be a non-empty string"})
        return

    ns: dict[str, Any] = {"call_tool": call_tool, "__builtins__": __builtins__}
    try:
        exec(code, ns, ns)
    except Exception as exc:  # noqa: BLE001 — surface block errors to host
        _write({"type": "error", "message": str(exc)})
        return

    if "result" not in ns:
        _write({"type": "error", "message": 'code must assign variable "result"'})
        return

    try:
        _write({"type": "done", "result": ns["result"]})
    except TypeError as exc:
        _write({"type": "error", "message": f"result is not JSON-serializable: {exc}"})


if __name__ == "__main__":
    main()
