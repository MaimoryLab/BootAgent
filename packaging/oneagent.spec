# -*- mode: python ; coding: utf-8 -*-

from pathlib import Path


ROOT = Path(SPECPATH).resolve().parent
METADATA = ROOT / "build" / "metadata"

datas = [
    (str(ROOT / "agents.lock.json"), "."),
    (str(ROOT / "frontend" / "dist"), "frontend/dist"),
    (str(ROOT / "README.md"), "."),
    (str(METADATA / "THIRD_PARTY_NOTICES.md"), "."),
    (str(METADATA / "licenses"), "licenses"),
]

analysis = Analysis(
    [str(ROOT / "oneagent" / "entrypoint.py")],
    pathex=[str(ROOT)],
    binaries=[],
    datas=datas,
    hiddenimports=[],
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=["tkinter", "unittest", "doctest"],
    noarchive=False,
    optimize=1,
)

pyz = PYZ(analysis.pure)

exe = EXE(
    pyz,
    analysis.scripts,
    [],
    exclude_binaries=True,
    name="OneAgent",
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=False,
    console=True,
    disable_windowed_traceback=False,
    argv_emulation=False,
    target_arch=None,
    codesign_identity=None,
    entitlements_file=None,
)

collection = COLLECT(
    exe,
    analysis.binaries,
    analysis.datas,
    strip=False,
    upx=False,
    upx_exclude=[],
    name="OneAgent",
)
