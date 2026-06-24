#!/usr/bin/env python3
"""Hook PostToolUse: roda `gofmt -w` no arquivo .go recem-editado.

Automatiza a convencao do CLAUDE.md ("sempre gofmt -w os arquivos tocados").
DEFENSIVO POR DESIGN: qualquer condicao inesperada -> no-op silencioso (exit 0),
pra NUNCA atrapalhar o fluxo de edicao. Nao formata nada que nao seja .go.
"""
import json
import shutil
import subprocess
import sys


def main() -> None:
    try:
        data = json.load(sys.stdin)
    except Exception:
        return  # sem JSON valido na stdin -> nao faz nada

    tool_input = data.get("tool_input") or {}
    path = tool_input.get("file_path") or tool_input.get("path") or ""
    if not isinstance(path, str) or not path.endswith(".go"):
        return

    gofmt = shutil.which("gofmt")
    if not gofmt:
        return  # toolchain Go ausente -> no-op

    try:
        subprocess.run(
            [gofmt, "-w", path],
            timeout=15,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
    except Exception:
        return  # qualquer erro do gofmt nao deve quebrar a edicao


if __name__ == "__main__":
    main()
