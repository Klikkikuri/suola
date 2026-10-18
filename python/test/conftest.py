"""Point the tests at the module that ``make wasi`` builds.

The tests run against an editable install, which has no packaged ``suola.wasm``: the wheel build copies it from
``build/wasi.wasm``. Every test therefore uses ``build/wasi.wasm`` as the default module. The CI wheel smoke test
covers the packaged module.
"""

from pathlib import Path

import pytest

from suola import _wasm

BUILD_WASI = Path(__file__).resolve().parents[2] / "build" / "wasi.wasm"


@pytest.fixture(scope="session")
def wasm_path() -> Path:
    """The WASI module under test."""
    if not BUILD_WASI.is_file():
        pytest.exit(f"{BUILD_WASI} not found -- run `make wasi`", returncode=1)
    return BUILD_WASI


@pytest.fixture(scope="session", autouse=True)
def default_module(wasm_path):
    """Make the build output the default module of WasmRuntime and Suola."""
    with pytest.MonkeyPatch.context() as mp:
        mp.setattr(_wasm, "_packaged_module", lambda: _wasm._compile(wasm_path))
        yield
