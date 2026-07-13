# Real PTY + Vision LLM

**Goal:** `terminal(pty=true)` uses a real pseudo-TTY; `browser_vision` / `vision_analyze` call a multimodal LLM.

## PTY

- Library: `github.com/aymanbagabas/go-pty` (Unix + Windows ConPTY)
- Foreground + background (`ProcessRegistry.Start` with `PTY: true`)
- Windows: resolve shell via `LookPath` before setting `Dir` (ConPTY path join quirk)
- `process` close on PTY sends EOT (does not tear down master until exit/kill)

## Vision

- `tool.VisionAnalyzer` + `model.NewVisionAnalyzer`
- Portal wires agent model (or `SATH_VISION_*`); disable with `SATH_VISION_ENABLED=0`
- `browser_vision(question|analyze)` → `analysis`; registers `vision_analyze` when analyzer present
