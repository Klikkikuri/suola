"""Checks the ABI surface of the WASI module, build/wasi.wasm.

Every other test drives the module through WasmRuntime, so a change to its shape
shows up only indirectly -- as a pile of confusing runtime failures rather than
one clear message. These assert the exports themselves, and deliberately do not
instantiate the module: this is about the contract hosts compile against, not
about behaviour.
"""
import sys
from pathlib import Path

import pytest
import wasmtime

# Add parent directory to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent / "src"))

from suola._wasm import get_wasi_module

# The exports a host needs, with their exact signatures. Types are spelled out
# because a changed arity or width is an ABI break that would otherwise surface
# as an opaque trap at call time.
EXPECTED_FUNCTIONS = {
    "GetSignature": (["i32", "i32"], ["i64"]),
    "AppendRules": (["i32", "i32"], ["i64"]),
    "Malloc": (["i32"], ["i32"]),
    "Free": (["i32"], []),
}


@pytest.fixture(scope="module")
def exports():
    """Exports of the WASI module, by name. No instantiation."""
    engine = wasmtime.Engine()
    module = wasmtime.Module.from_file(engine, str(get_wasi_module()))
    return {export.name: export.type for export in module.exports}


class TestModuleExports:
    """Test suite for the WASI module's export surface."""

    @pytest.mark.parametrize("name", sorted(EXPECTED_FUNCTIONS))
    def test_exports_function(self, exports, name):
        """Each documented export exists with the documented signature."""
        assert name in exports, f"{name} is not exported; hosts call it by name"

        func_type = exports[name]
        assert isinstance(func_type, wasmtime.FuncType), f"{name} is exported but is not a function"

        params, results = EXPECTED_FUNCTIONS[name]
        assert [str(p) for p in func_type.params] == params, f"{name} parameters changed"
        assert [str(r) for r in func_type.results] == results, f"{name} results changed"

    def test_exports_memory(self, exports):
        """Hosts read results straight out of linear memory, so it must be exported."""
        assert "memory" in exports, "memory is not exported; hosts cannot read results"
        assert isinstance(exports["memory"], wasmtime.MemoryType)

    def test_is_a_reactor_module(self, exports):
        """The module must be a shared library, not a command.

        TinyGo only keeps //go:wasmexport functions callable when built with
        -buildmode=c-shared. Without it the module exports _start instead, and
        every export traps in runtime.wasmExportCheckRun once main returns --
        so this is the check that pins the build mode.
        """
        assert "_initialize" in exports, (
            "_initialize is missing: build the module with -buildmode=c-shared, "
            "otherwise its exports trap once main returns"
        )
        assert "_start" not in exports, (
            "_start is exported, so the module was built as a command rather than "
            "a shared library"
        )
        assert isinstance(exports["_initialize"], wasmtime.FuncType)
