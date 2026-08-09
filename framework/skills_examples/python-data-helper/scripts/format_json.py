#!/usr/bin/env python3
"""从 stdin 读取 JSON，格式化后输出。用于校验与阅读。"""
import json
import sys

def main():
    try:
        data = json.load(sys.stdin)
        print(json.dumps(data, ensure_ascii=False, indent=2))
    except json.JSONDecodeError as e:
        print(f"JSON 解析错误: {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
