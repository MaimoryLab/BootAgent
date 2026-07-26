#!/usr/bin/env python3
from __future__ import annotations

import sys
from pathlib import Path


if sys.version_info < (3, 12):
    raise SystemExit("Python 3.12+ is required to run the OneAgent source GUI.")


ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from oneagent.server import main


if __name__ == "__main__":
    main()
