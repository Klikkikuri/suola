import logging
from pathlib import Path
from typing import Protocol

from ._wasm import WasmRuntime

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
    URL hashing API using WASM runtime.

    Provides a simple callable interface for URL hashing with the Suola algorithm.

    Example usage:
    .. code-block:: python

        from suola.api import Suola
        suola = Suola()
        hashed_url = suola("https://www.example.com/path/to/resource?query=123")
        print(hashed_url)  # Outputs the hash signature of the normalized URL
    """

    _runtime: WasmRuntime

    def __init__(self, wasm_module: Path | str | None = None, custom_rules: Path | str | None = None):
        """
        Initialize the Suola API with WASM runtime.

        :param wasm_module: Optional path to the WASI module. If not provided, it will be automatically located.
        :param custom_rules: Optional path to a custom YAML rules file. If not provided, uses embedded default rules.
        """
        if wasm_module is not None:
            wasm_module = Path(wasm_module)
        
        if custom_rules is not None:
            custom_rules = Path(custom_rules)
        
        self._runtime = WasmRuntime(wasm_module, custom_rules)
        if custom_rules:
            logger.debug("Suola API initialized with custom rules: %s", custom_rules)
        else:
            logger.debug("Suola API initialized with default embedded rules")

    def __call__(self, url: str) -> str | None:
        """
        Hash a URL using the WASM runtime.

        :param url: The URL to normalize and hash
        :return: The hashed URL or None if an error occurs
        """
        url = str(url).strip()
        if not url:
            raise ValueError("URL cannot be empty")
        try:
            hash = self._runtime.get_signature(url)
            return hash
        except Exception as e:
            logger.error(f"Error hashing URL: {e}")
        return None

if __name__ == "__main__":
    # Example usage
    logging.basicConfig(level=logging.DEBUG)
    logger.info("Starting Suola API example")
    
    # Example 1: Using default embedded rules
    print("\n=== Example 1: Using Default Embedded Rules ===")
    suola = Suola()
    
    test_urls = [
        "https://www.iltalehti.fi/ulkomaat/a/51495a62-a494-4474-a234-ddedae3e112b",
        "https://www.iltalehti.fi/politiikka/a/4427e983-993e-4a4a-aeb4-531f9e9f7d7a",
        "http://www.example.com/path/to/resource?query=123",
        "https://www.iltalehti.fi/kotimaa/a/7d3c5ba2-66bd-473e-9c0b-fc3ec26abe80",
    ]
    
    print("\nProcessing URLs:")
    for i, url in enumerate(test_urls, 1):
        try:
            result = suola(url)
            print(f"{i}. {result}")
        except Exception as e:
            print(f"{i}. Error: {e}")
    
    # Example 2: Using custom rules
    print("\n=== Example 2: Using Custom Rules ===")
    from pathlib import Path
    custom_rules_path = Path(__file__).parent.parent.parent.parent / "test_custom_rules.yaml"
    if custom_rules_path.exists():
        suola_custom = Suola(custom_rules=custom_rules_path)
        print(f"Initialized with custom rules: {custom_rules_path}")
        try:
            result = suola_custom("https://example.com/article/12345?utm_source=test")
            print(f"Custom rules result: {result}")
        except Exception as e:
            print(f"Error with custom rules: {e}")
    else:
        print(f"Custom rules file not found at: {custom_rules_path}")
    
    print("\nTesting error handling:")
    try:
        suola("")
    except ValueError as e:
        print(f"✓ Empty URL rejected: {e}")
