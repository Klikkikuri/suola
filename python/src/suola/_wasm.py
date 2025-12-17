import ctypes
import importlib.resources
import logging
from pathlib import Path
from typing import Optional, cast

import wasmtime
from .util import get_data_dir

# Prevent excessively large URLs
MAX_URL_LENGTH = 64 * 1024

logger = logging.getLogger(__name__)

def get_wasi_module(pkg_name = __package__) -> Path:
    pkg_name = str(pkg_name or __package__)

    module_locations = [
        Path.cwd() / "suola.wasm",
        Path.cwd() / ".." / "build" / "wasi.wasm",
        Path(get_data_dir()) / "suola.wasm",
        importlib.resources.files(pkg_name) / "suola.wasm",
    ]
    if Path("/.dockerenv").exists():
        # Check the default build location in Docker
        module_locations.append(Path("/app/build/wasi.wasm"))
        # In meri workspace, the working directory is /app
        module_locations.append(Path("/app/packages/suola/build/wasi.wasm"))
 
    for location in module_locations:
        if location.exists():
            logger.debug("Found WASI module at: %s", location)
            return location
    else:
        raise FileNotFoundError(
            f"WASI module 'suola.wasm' could not be found. Searched paths: {', '.join(map(str, module_locations))}"
        )


class WasmRuntime:
    """Direct WASM runtime using wasmtime-py for in-process execution."""

    get_signature_fn: wasmtime.Func
    malloc_fn: wasmtime.Func
    free_fn: wasmtime.Func

    def __init__(self, wasm_path: Optional[Path] = None, custom_rules_path: Optional[Path] = None):
        if wasm_path is None:
            wasm_path = get_wasi_module()

        # Initialize wasmtime engine and module
        self.engine = wasmtime.Engine()
        self.module = wasmtime.Module.from_file(self.engine, str(wasm_path))
        
        # Create linker with WASI imports
        self.linker = wasmtime.Linker(self.engine)
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
        
        self.store = wasmtime.Store(self.engine)
        self.store.set_wasi(wasi)
        
        # Instantiate the module
        self.instance = self.linker.instantiate(self.store, self.module)

        # Get exported functions
        exports = self.instance.exports(self.store)
        # Print available exports for debugging
        logger.debug("WASM module exports: %s", [export for export in exports])

        self.get_signature_fn = cast(wasmtime.Func, exports["GetSignature"])
        self.malloc_fn = cast(wasmtime.Func, exports["Malloc"])
        self.free_fn = cast(wasmtime.Func, exports["Free"])
        self.memory = cast(wasmtime.Memory, exports["memory"])

        # Run main or _start function if present
        if "_start" in exports:
            start_fn = cast(wasmtime.Func, exports["_start"])
            try:
                start_fn(self.store)
            except wasmtime.ExitTrap as e:
                # Start function may exit; ignore normal exit
                if e.code not in (0,):
                    logger.error("WASM _start function trapped: %s", e)
                else:
                    logger.debug("WASM _start function completed with exit code %d", e.code)

    def get_signature(self, url: str) -> str:
        """Call GetSignature function directly in WASM."""
        if not url:
            raise ValueError("URL cannot be empty")
        
        url_bytes = url.encode('utf-8')
        url_len = len(url_bytes)

        if url_len > MAX_URL_LENGTH:
            raise ValueError(f"URL too long: {url_len} bytes (max {MAX_URL_LENGTH})")
        
        # Allocate memory for URL in WASM
        url_ptr = self.malloc_fn(self.store, url_len)
        if url_ptr == 0:
            raise RuntimeError("Failed to allocate memory in WASM")
        
        try:
            # Get memory size to validate bounds
            memory_size = self.memory.data_len(self.store)
            if url_ptr + url_len > memory_size:
                raise RuntimeError(f"Memory overflow: ptr={url_ptr}, len={url_len}, memory_size={memory_size}")
            
            # Write URL to WASM memory using ctypes.memmove
            memory_data = self.memory.data_ptr(self.store)
            ptr_type = ctypes.c_ubyte * url_len
            src_ptr = ptr_type.from_buffer_copy(url_bytes)
            ctypes.memmove(ctypes.addressof(memory_data.contents) + url_ptr, src_ptr, url_len)
            
            # Call GetSignature
            result = self.get_signature_fn(self.store, url_ptr, url_len)
            
            # Unpack result: high 32 bits = pointer, low 32 bits = length
            sig_ptr = (result >> 32) & 0xFFFFFFFF
            sig_len = result & 0x7FFFFFFF  # Mask out error bit
            is_error = (result & 0x80000000) != 0

            # Validate result bounds before reading
            if sig_ptr != 0 and sig_len > 0:
                if sig_ptr + sig_len > memory_size:
                    raise RuntimeError(f"Result overflow: ptr={sig_ptr}, len={sig_len}, memory_size={memory_size}")
            
            # Read result from WASM memory using bytes() constructor directly on slice
            result_str = bytes(memory_data[sig_ptr:sig_ptr + sig_len]).decode('utf-8')
            
            if is_error:
                raise RuntimeError(f"WASM error: {result_str}")
            
            return result_str
        finally:
            # Free only the input buffer allocated by Malloc
            # Note: sig_ptr is managed by Go's memory pool and should NOT be freed here
            self.free_fn(self.store, url_ptr)


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
