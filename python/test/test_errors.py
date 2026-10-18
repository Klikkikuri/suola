"""Every failure surfaces as a SuolaError subclass, and a trapped instance refuses later calls."""

import faulthandler
import os
import shutil

import pytest
import wasmtime

from suola import InvalidRulesError, InvalidURLError, Suola, SuolaError, SuolaRuntimeError
from suola import _wasm
from suola._wasm import WasmRuntime

ARTICLE_URL = "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b"


BAD_REGEX_RULES = '{"sites": [{"domain": "x.fi", "templates": [{"pattern": "(", "template": "x"}]}]}'


@pytest.mark.parametrize("content", ['{"bad": ', BAD_REGEX_RULES])
def test_bad_custom_rules_raise_invalid_rules_error(tmp_path, content):
    rules = tmp_path / "rules.json"
    rules.write_text(content)
    with pytest.raises(InvalidRulesError, match="exited with code 1. The module writes the reason to stderr"):
        Suola(custom_rules=rules)


def test_trap_raises_when_faulthandler_is_enabled(tmp_path):
    """A real trap: the module panics because the rules path is a directory.

    With signal-based traps, faulthandler gets the trap signal first and kills the process.
    """
    faulthandler.enable()
    with pytest.raises(SuolaError):
        Suola(custom_rules=tmp_path)


def test_module_that_is_not_wasm_raises_runtime_error(tmp_path):
    module = tmp_path / "suola.wasm"
    module.write_bytes(b"junk")
    with pytest.raises(SuolaRuntimeError, match="Cannot compile"):
        Suola(wasm_module=module)


def test_url_that_is_not_valid_unicode_raises_invalid_url_error():
    with pytest.raises(InvalidURLError, match="not valid Unicode"):
        Suola()("https://example.com/\udcff")


@pytest.mark.parametrize(
    ("message", "error"),
    [
        ("URL has no host: mailto:x@example.com", InvalidURLError),
        ("no matching rule found for host example.com", InvalidURLError),
        ("URL length 70000 exceeds maximum of 65536 bytes", InvalidURLError),
        ("invalid pointer 16: not a buffer returned by Malloc", SuolaRuntimeError),
        ("invalid buffer length: 0", SuolaRuntimeError),
        ("rules not loaded", SuolaRuntimeError),
        ('unknown rule set "x"', SuolaRuntimeError),
    ],
)
def test_signature_errors_are_classified(message, error):
    assert type(_wasm._signature_error(message)) is error


def test_trap_disables_the_instance():
    runtime = WasmRuntime()
    freed = []
    free_fn = runtime.free_fn
    runtime.free_fn = lambda store, ptr: (freed.append(ptr), free_fn(store, ptr))

    def trap(store, ptr, length):
        raise wasmtime.Trap("boom")

    runtime.get_signature_fn = trap
    with pytest.raises(SuolaRuntimeError, match="trapped: .*boom"):
        runtime.get_signature(ARTICLE_URL)
    assert freed == [], "a trapped module must not be called again, not even to free"

    with pytest.raises(SuolaRuntimeError, match="earlier call"):
        runtime.get_signature(ARTICLE_URL)


def test_host_error_does_not_disable_the_instance():
    runtime = WasmRuntime()

    def bad_call(store, ptr, length):
        raise wasmtime.WasmtimeError("wrong argument types")

    get_signature_fn, runtime.get_signature_fn = runtime.get_signature_fn, bad_call
    with pytest.raises(SuolaRuntimeError, match="call failed"):
        runtime.get_signature(ARTICLE_URL)

    runtime.get_signature_fn = get_signature_fn
    assert len(runtime.get_signature(ARTICLE_URL)) == 64


def test_rebuilt_module_is_compiled_again(tmp_path, wasm_path):
    module = tmp_path / "suola.wasm"
    shutil.copy(wasm_path, module)

    first = _wasm._compile(module)
    assert _wasm._compile(module) is first

    stat = module.stat()
    os.utime(module, ns=(stat.st_atime_ns, stat.st_mtime_ns + 1_000_000_000))
    assert _wasm._compile(module) is not first


def test_all_errors_share_a_base_class():
    for error in (InvalidURLError, InvalidRulesError, SuolaRuntimeError):
        assert issubclass(error, SuolaError)
