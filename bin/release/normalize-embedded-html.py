#!/usr/bin/env python3
"""Normalize generated panel HTML before embedding it into a native binary."""

from __future__ import annotations

import sys
from pathlib import Path

HIDDEN_CODE_POINTS = (
    0x0002,
    0x0018,
    0x001F,
    0x007F,
    0x0080,
    0x0085,
    0x009F,
    0x061C,
    0x200B,
    0x200E,
    0x200F,
    0x202D,
    0x202E,
    0x2066,
    0x2067,
    0x2069,
    0xFEFF,
)


def main() -> int:
    if len(sys.argv) != 3:
        print(f"usage: {sys.argv[0]} INPUT OUTPUT", file=sys.stderr)
        return 2

    source = Path(sys.argv[1])
    destination = Path(sys.argv[2])
    content = source.read_text(encoding="utf-8").replace("\r\n", "\n")
    for code_point in HIDDEN_CODE_POINTS:
        content = content.replace(chr(code_point), f"\\u{code_point:04X}")
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(content, encoding="utf-8", newline="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
