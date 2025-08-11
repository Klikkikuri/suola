import logging
from pathlib import Path
from typing import Protocol

from ._daemon import ProcessManager
from ._wasm import get_wasi_module

logger = logging.getLogger(__name__)

class SuolaAPI(Protocol):
    """
    Protocol for the Suola API.

    This defines the interface for the Suola class, which hashes URLs using a WASI module.
    """

    def __call__(self, url: str) -> str:
        """
        Hash a URL using the WASI module.

        :param url: The URL to normalize and hash
        :return: The hashed URL
        """
        raise NotImplementedError("Subclasses must implement this method")

class Suola(SuolaAPI):
    """
    WebAssembly-based URL hashing API using WASI module.

    This class uses a WASI module to hash URLs. It initializes the WASI environment and provides
    a callable interface to hash URLs. It uses wasmtime for executing the WASI module.
sage::
    Example usage:
    .. code-block:: python

        from suola.api import Suola
        suola = Suola()
        hashed_url = suola("https://www.example.com/path/to/resource?query=123")
        print(hashed_url)  # Outputs the hashed URL
    """

    _process_manager: ProcessManager

    def __init__(self, wasm_module: Path | str | None = None):
        """
        Initialize the Suola API with the WASI module.

        :param wasm_module: Optional path to the WASI module. If not provided, it will be automatically located.
        """
        if wasm_module is None:
            # Automatically locate the WASI module
            wasm_module = get_wasi_module()
            logger.debug("Using located WASI module at: %r", wasm_module)

        self._process_manager = ProcessManager(["wasmtime", str(wasm_module)])
        # Wait for ready signal from the WASI module
        with self._process_manager._buffer_lock:
            logger.debug("Waiting for WASI module to be ready...")
            while self._process_manager.is_running():
                stdout, stderr = self._process_manager._read_output(self._process_manager.timeout)  # Read output to avoid blocking
                logger.debug("WASI module output: %s", stdout)
                logger.debug("WASI module error: %s", stderr)
                if "Waiting." in "\n".join(stderr):
                    logger.debug("WASI module is waiting for input")
                    break


    def __call__(self, url: str) -> str:
        """
        Hash a URL using the WASI module.
        :param url: The URL to normalize and hash
        :return: The hashed URL
        """
        url = str(url).strip()
        if not url:
            raise ValueError("URL cannot be empty")
        return self._process_manager.__call__(url)

if __name__ == "__main__":
    # Example usage
    logging.basicConfig(level=logging.DEBUG)
    logger.info("Starting Suola API example")
    suola = Suola()
    try:
        result = suola("https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b")
        print(f"Hashed URL: {result}")
        print(suola("http://www.example.com/path/to/resource?query=123"))
        print(suola("https://www.iltalehti.fi/politiikka/a/4427e983-993e-4a4a-aeb4-531f9e9f7d7a"))
    except NotImplementedError as e:
        print(f"Error: {e}")
    except Exception as e:
        print(f"Unexpected error: {e}")
