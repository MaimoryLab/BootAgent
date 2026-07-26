from __future__ import annotations

import sys

from oneagent import cli, server


def main() -> None:
    if len(sys.argv) > 1:
        if sys.argv[1] == "gui":
            del sys.argv[1]
            server.main()
            return
        cli.main()
        return
    server.main()


if __name__ == "__main__":
    main()
