import contextlib
import ctypes
import functools
import importlib.resources
import logging
import threading
from pathlib import Path
from typing import Iterator, Optional, cast

import wasmtime

from .errors import InvalidRulesError, InvalidURLError, SuolaError, SuolaRuntimeError

# Mirrors the limits enforced by the WASM module (see wasi.go and lib.go).
# Checking them here turns an oversized input into a clear Python error instead
# of a round trip that comes back as a packed error string.
MAX_URL_LENGTH = 64 * 1024
MAX_RULES_SIZE = 2 * 1024 * 1024

# Failures inside the module. Its state is unknown after one, so the instance must not be called again.
_TRAPS = (wasmtime.Trap, wasmtime.ExitTrap)

# Module errors that report a host or module fault, not a bad input (ptrToBytes in wasi.go, lib.go).
_MODULE_FAULTS = ("invalid buffer", "invalid pointer", "rules not loaded", "unknown rule set")

logger = logging.getLogger(__name__)


@functools.cache
def _engine() -> wasmtime.Engine:
    """One engine per process: wasmtime shares an Engine and its compiled Modules between threads and Stores.

    - Parallel compilation is off, because its thread pool does not survive ``fork``: a forked child that
      compiles a module hangs. The module is compiled once per process, so the extra time is small.
    - Traps do not use signals. A signal handler that the program installs after wasmtime, for example
      :mod:`faulthandler` in pytest, otherwise gets the trap signal and kills the process. Explicit checks make
      a call about 5% slower.
    """
    config = wasmtime.Config()
    config.parallel_compilation = False
    config.signals_based_traps = False
    return wasmtime.Engine(config)


@contextlib.contextmanager
def _wasm_errors(error: type[SuolaError], context: str) -> Iterator[None]:
    """Raise wasmtime failures (traps, exits, compile and link errors) as ``error``."""
    try:
        yield
    except (wasmtime.Trap, wasmtime.WasmtimeError) as e:
        raise error(f"{context}: {e}") from e


def _compile(path: Path) -> wasmtime.Module:
    """Compile the WASI module at ``path``, once for each content of the file."""
    wasm = path.read_bytes()
    with _wasm_errors(SuolaRuntimeError, f"Cannot compile WASI module {path}"):
        return _compile_wasm(wasm)


@functools.lru_cache(maxsize=8)
def _compile_wasm(wasm: bytes) -> wasmtime.Module:
    """Cached by content, so that a rebuilt file is compiled again also when its size and mtime did not change."""
    return wasmtime.Module(_engine(), wasm)


@functools.cache
def _packaged_module() -> wasmtime.Module:
    """Compile the WASI module that the wheel ships.

    An editable install has no packaged module, because the wheel build copies it from ``build/wasi.wasm``.
    """
    resource = importlib.resources.files(__package__) / "suola.wasm"
    if not resource.is_file():
        raise FileNotFoundError(
            "The suola package has no packaged WASI module 'suola.wasm'. "
            "Pass wasm_module= or install a wheel built with `make build-python`."
        )
    with importlib.resources.as_file(resource) as path:
        return _compile(path)


def _module_error(message: str, input_error: type[SuolaError]) -> SuolaError:
    """Classify an error that the module reports: a fault becomes :class:`SuolaRuntimeError`, else ``input_error``."""
    if message.startswith(_MODULE_FAULTS):
        return SuolaRuntimeError(f"WASM error: {message}")
    return input_error(message)


class WasmRuntime:
    """Direct WASM runtime using wasmtime-py for in-process execution.

    An instance is safe to share between threads. Calls into the module are serialized, because a wasmtime
    ``Store`` cannot be used by more than one thread at a time and the WASI module itself is single-threaded.
    """

    get_signature_fn: wasmtime.Func
    append_rules_fn: Optional[wasmtime.Func]
    malloc_fn: wasmtime.Func
    free_fn: wasmtime.Func

    def __init__(self, wasm_path: Optional[Path] = None, custom_rules_path: Optional[Path] = None):
        """
        :param wasm_path: The WASI module to load. If not given, the module packaged in the wheel is used.
        :param custom_rules_path: A compiled JSON rules file that replaces the embedded default rules.
        """
        self._lock = threading.Lock()
        self._failure: Optional[SuolaRuntimeError] = None
        self.module = _packaged_module() if wasm_path is None else _compile(Path(wasm_path).resolve())

        # Create linker with WASI imports
        self.linker = wasmtime.Linker(_engine())
        self.linker.define_wasi()

        # Create store with WASI context
        wasi = wasmtime.WasiConfig()
        wasi.inherit_stderr()  # For debug messages

        # If custom rules path is provided, pass it as argv to the WASM module
        if custom_rules_path is not None:
            custom_rules_path = Path(custom_rules_path).resolve()
            if not custom_rules_path.exists():
                raise FileNotFoundError(f"Custom rules file not found: {custom_rules_path}")

            # Preopen the directory containing the custom rules file so WASI can access it
            # This is required because WASI has a capability-based security model
            # TODO: Make this more secure by only preopening the specific file if possible
            rules_dir = custom_rules_path.parent
            wasi.preopen_dir(str(rules_dir), str(rules_dir))
            logger.debug("Preopened directory: %s", rules_dir)

            # Set argv with program name (argv[0]) and custom rules path (argv[1])
            # The WASM module's main() will receive this as os.Args
            wasi.argv = ["suola.wasm", str(custom_rules_path)]
            logger.debug("Custom rules path set to: %s", custom_rules_path)

        self.store = wasmtime.Store(_engine())
        self.store.set_wasi(wasi)

        with _wasm_errors(SuolaRuntimeError, "Cannot instantiate WASI module"):
            self.instance = self.linker.instantiate(self.store, self.module)
        exports = self.instance.exports(self.store)
        logger.debug("WASM module exports: %s", [export for export in exports])

        self.get_signature_fn = cast(wasmtime.Func, exports["GetSignature"])
        self.append_rules_fn = cast(Optional[wasmtime.Func], exports.get("AppendRules"))
        self.malloc_fn = cast(wasmtime.Func, exports["Malloc"])
        self.free_fn = cast(wasmtime.Func, exports["Free"])
        self.memory = cast(wasmtime.Memory, exports["memory"])

        try:
            with _wasm_errors(SuolaRuntimeError, "WASI module failed to initialize"):
                self._initialize()
        except SuolaRuntimeError as e:
            # The module loads its rules when it starts. It exits for rules that are not valid and panics for a
            # file that it cannot read. A TinyGo panic is a trap and a Go panic is an exit, so both mean bad rules.
            if custom_rules_path is not None and e.__cause__ is not None:
                raise InvalidRulesError(
                    f"Cannot load custom rules {custom_rules_path}: {e.__cause__}. "
                    "The module writes the reason to stderr."
                ) from e.__cause__
            raise

    def _initialize(self) -> None:
        """Run the module's entry point, which loads the rules (and reads argv[1], if a custom rules path is set).

        Which entry point exists depends on the toolchain that built the module:

        - ``_initialize``: a reactor module, i.e. a shared library. The Makefile builds this with
          ``-buildmode=c-shared``. It runs the Go init functions and returns normally; main is never called.
        - ``_start``: a command. It runs init and then main, and may exit when main returns.

        Prefer ``_initialize``: a module exporting it is a library and must not be started as a command.
        """
        exports = self.instance.exports(self.store)
        if "_initialize" in exports:
            entry = cast(wasmtime.Func, exports["_initialize"])
        elif "_start" in exports:
            entry = cast(wasmtime.Func, exports["_start"])
        else:
            # Without an entry point the rules are never loaded, and every
            # GetSignature call would fail with "rules not loaded".
            raise SuolaRuntimeError("WASM module exports neither _initialize nor _start; cannot initialize rules")

        try:
            entry(self.store)
        except wasmtime.ExitTrap as e:
            # A command exits when main returns; only a non-zero code is a failure.
            if e.code != 0:
                raise

    def _unpack_result(self, result: int) -> tuple[bool, str]:
        """Decode an export's packed uint64 return value.

        Layout is [pointer:32][length:31|error_bit:1], as documented in wasi.go.
        Returns (is_error, message); the message is the signature on success and
        the error text otherwise.

        The memory base is read *after* the call rather than reused from before
        it: an export can grow linear memory, which invalidates any host pointer
        taken earlier.
        """
        ptr = (result >> 32) & 0xFFFFFFFF
        length = result & 0x7FFFFFFF  # Mask out error bit
        is_error = (result & 0x80000000) != 0

        if ptr == 0 or length == 0:
            return is_error, ""

        memory_size = self.memory.data_len(self.store)
        if ptr + length > memory_size:
            raise SuolaRuntimeError(f"Result overflow: ptr={ptr}, len={length}, memory_size={memory_size}")

        memory_data = self.memory.data_ptr(self.store)
        try:
            return is_error, bytes(memory_data[ptr:ptr + length]).decode('utf-8')
        except UnicodeDecodeError as e:
            raise SuolaRuntimeError(f"WASI module returned a result that is not UTF-8: {e}") from e

    def _write(self, ptr: int, data: bytes) -> None:
        """Copy ``data`` into module memory at ``ptr``."""
        # wasmtime gives an i32 as a signed int. A pointer at 2 GiB or more is negative, and would pass the bounds
        # check and write before the start of linear memory.
        offset = ptr & 0xFFFFFFFF
        memory_size = self.memory.data_len(self.store)
        if offset + len(data) > memory_size:
            raise SuolaRuntimeError(f"Memory overflow: ptr={offset}, len={len(data)}, memory_size={memory_size}")

        memory_data = self.memory.data_ptr(self.store)
        src = (ctypes.c_ubyte * len(data)).from_buffer_copy(data)
        ctypes.memmove(ctypes.addressof(memory_data.contents) + offset, src, len(data))

    def _call(self, fn: wasmtime.Func, data: bytes) -> tuple[bool, str]:
        """Copy ``data`` into module memory, call ``fn`` with it and return the unpacked result.

        The lock covers the whole sequence: a second thread must not enter the module between the allocation,
        the call and the read of the result. After a trap the state of the module is unknown, so the instance
        refuses all later calls.

        :raises SuolaRuntimeError: If the module traps or the instance trapped before.
        """
        with self._lock:
            if self._failure is not None:
                raise SuolaRuntimeError("The WASI module trapped in an earlier call") from self._failure
            try:
                return self._call_unlocked(fn, data)
            except _TRAPS as e:
                self._failure = SuolaRuntimeError(f"WASI module trapped: {e}")
                raise self._failure from e
            except wasmtime.WasmtimeError as e:
                raise SuolaRuntimeError(f"WASI call failed: {e}") from e

    def _call_unlocked(self, fn: wasmtime.Func, data: bytes) -> tuple[bool, str]:
        """The body of :meth:`_call`. The caller holds the lock."""
        ptr = self.malloc_fn(self.store, len(data))
        if ptr == 0:
            raise SuolaRuntimeError("Failed to allocate memory in WASM")

        try:
            self._write(ptr, data)
            result = fn(self.store, ptr, len(data))
        except _TRAPS:
            # Do not call into a module that trapped, not even to free the buffer.
            raise
        except BaseException:
            self.free_fn(self.store, ptr)
            raise

        try:
            return self._unpack_result(result)
        finally:
            # The result buffer belongs to the module, so only the input buffer is freed here.
            self.free_fn(self.store, ptr)

    def get_signature(self, url: str) -> str:
        """Return the signature of ``url``.

        :raises InvalidURLError: If the URL is empty, too long, not valid Unicode or rejected by the rules.
        :raises SuolaRuntimeError: If the module fails.
        """
        if not url:
            raise InvalidURLError("URL cannot be empty")

        try:
            url_bytes = url.encode('utf-8')
        except UnicodeEncodeError as e:
            raise InvalidURLError(f"URL is not valid Unicode: {e}") from e
        if len(url_bytes) > MAX_URL_LENGTH:
            raise InvalidURLError(f"URL too long: {len(url_bytes)} bytes (max {MAX_URL_LENGTH})")

        is_error, result = self._call(self.get_signature_fn, url_bytes)
        if is_error:
            raise _module_error(result, InvalidURLError)
        return result

    def append_rules(self, rules: str | bytes | Path) -> None:
        """
        Append additional JSON rules to the already-initialized runtime.

        Rules are authored as YAML in the repository but the module parses JSON only; compile
        them with ``make rules`` (see ``cmd/rules-compile``) before passing them here.

        Note on string resolution:
        If a `str` is passed, it is treated as literal JSON content if it starts with '{'.
        Otherwise, if it corresponds to an existing file path on disk, it will be read from that
        file. To guarantee that a file path is never misinterpreted as literal JSON, pass a
        `pathlib.Path` instance.

        :raises InvalidRulesError: If the rules are empty, too large or rejected by the module.
        :raises SuolaRuntimeError: If the module has no ``AppendRules`` export or fails.
        """
        if self.append_rules_fn is None:
            raise SuolaRuntimeError("AppendRules export not available in WASM module")

        if isinstance(rules, Path):
            rules_bytes = rules.read_bytes()
        elif isinstance(rules, str):
            if rules.lstrip().startswith("{"):
                rules_bytes = rules.encode('utf-8')
            else:
                p = Path(rules)
                if p.exists() and p.is_file():
                    rules_bytes = p.read_bytes()
                else:
                    rules_bytes = rules.encode('utf-8')
        elif isinstance(rules, bytes):
            rules_bytes = rules
        else:
            raise TypeError(f"Invalid type for rules: {type(rules)}")

        if not rules_bytes:
            raise InvalidRulesError("Rules data cannot be empty")

        if len(rules_bytes) > MAX_RULES_SIZE:
            raise InvalidRulesError(f"Rules too large: {len(rules_bytes)} bytes (max {MAX_RULES_SIZE})")

        is_error, err_msg = self._call(self.append_rules_fn, rules_bytes)
        if is_error:
            raise _module_error(err_msg or "Unknown WASM error", InvalidRulesError)


if __name__ == "__main__":
    # Example usage
    logging.basicConfig(level=logging.DEBUG)
    logger.info("Starting WASM runtime example")
    wasm_runtime = WasmRuntime()
    try:
        result = wasm_runtime.get_signature("https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b")
        print(f"Hashed URL: {result}")
        print(wasm_runtime.get_signature("http://www.example.com/path/to/resource?query=123"))
        print(wasm_runtime.get_signature("https://www.iltalehti.fi/politiikka/a/4427e983-993e-4a4a-aeb4-531f9e9f7d7a"))
    except Exception as e:
        print(f"Error: {e}")
