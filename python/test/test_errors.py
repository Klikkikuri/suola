"""Every failure surfaces as a SuolaError subclass, and a trapped instance refuses later calls."""

import faulthandler
import os
import shutil
import subprocess
import sys

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
    with pytest.raises(InvalidRulesError, match="The module writes the reason to stderr"):
        Suola(custom_rules=rules)


def test_unreadable_custom_rules_raise_invalid_rules_error(tmp_path):
    with pytest.raises(InvalidRulesError, match="Cannot load custom rules"):
        Suola(custom_rules=tmp_path)


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


MODULE_FAULTS = [
    "invalid pointer 16: not a buffer returned by Malloc",
    "invalid buffer length: 0",
    "rules not loaded",
    'unknown rule set "x"',
]


@pytest.mark.parametrize(
    "message",
    [
        "URL has no host: mailto:x@example.com",
        "no matching rule found for host example.com",
        "URL length 70000 exceeds maximum of 65536 bytes",
    ],
)
def test_url_errors_are_input_errors(message):
    assert type(_wasm._module_error(message, InvalidURLError)) is InvalidURLError


@pytest.mark.parametrize("message", ["parsing rules JSON: unexpected end of JSON input", "rules data is empty"])
def test_rules_errors_are_input_errors(message):
    assert type(_wasm._module_error(message, InvalidRulesError)) is InvalidRulesError


@pytest.mark.parametrize("input_error", [InvalidURLError, InvalidRulesError])
@pytest.mark.parametrize("message", MODULE_FAULTS)
def test_module_faults_are_runtime_errors(message, input_error):
    assert type(_wasm._module_error(message, input_error)) is SuolaRuntimeError


def test_append_rules_reports_bad_json_as_invalid_rules_error():
    with pytest.raises(InvalidRulesError, match="parsing rules JSON"):
        Suola().append_rules(b'{"sites": [')


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


def test_pointer_above_2_gib_is_not_written_before_memory():
    """A negative i32 pointer means an offset of 2 GiB or more, which is outside this module's memory."""
    runtime = WasmRuntime()
    runtime.malloc_fn = lambda store, size: -16
    runtime.free_fn = lambda store, ptr: None

    with pytest.raises(SuolaRuntimeError, match=f"Memory overflow: ptr={2**32 - 16}"):
        runtime.get_signature(ARTICLE_URL)


# An empty custom section named "x". Appended to a module, it changes the content but not the behaviour.
CUSTOM_SECTION = b"\x00\x02\x01x"


def test_module_is_compiled_once_for_each_content(tmp_path, wasm_path):
    module = tmp_path / "suola.wasm"
    shutil.copy2(wasm_path, module)
    copy = tmp_path / "copy.wasm"
    shutil.copy2(wasm_path, copy)

    first = _wasm._compile(module)
    assert _wasm._compile(module) is first
    assert _wasm._compile(copy) is first

    stat = module.stat()
    module.write_bytes(wasm_path.read_bytes() + CUSTOM_SECTION)
    os.utime(module, ns=(stat.st_atime_ns, stat.st_mtime_ns))
    assert _wasm._compile(module) is not first


def test_import_does_not_create_the_engine():
    code = "import suola, suola._wasm as w; print(w._create_engine.cache_info().currsize)"
    result = subprocess.run([sys.executable, "-c", code], capture_output=True, text=True, check=True)
    assert result.stdout.strip() == "0"


COLD_START = """
import sys, threading
from suola import Suola, _wasm

errors = []

def create(barrier):
    barrier.wait()
    try:
        Suola(wasm_module=sys.argv[1])("https://example.com/a")
    except Exception as e:
        errors.append(e)

# The race depends on timing, so start cold several times. Each round empties the engine and module caches.
for _ in range(5):
    _wasm._create_engine.cache_clear()
    _wasm._compile_wasm.cache_clear()
    barrier = threading.Barrier(16)
    threads = [threading.Thread(target=create, args=(barrier,)) for _ in range(16)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()
print(len(errors), errors[:1])
"""


def test_first_instances_from_many_threads_share_one_engine(wasm_path):
    """A new process, so that the engine and the module caches are empty when the threads start."""
    result = subprocess.run(
        [sys.executable, "-c", COLD_START, str(wasm_path)], capture_output=True, text=True, check=True
    )
    assert result.stdout.startswith("0 "), result.stdout


def test_all_errors_share_a_base_class():
    for error in (InvalidURLError, InvalidRulesError, SuolaRuntimeError):
        assert issubclass(error, SuolaError)
