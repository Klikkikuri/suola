import importlib.resources
import logging
from pathlib import Path
from .util import get_data_dir

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
 
    for location in module_locations:
        if location.exists():
            logger.debug("Found WASI module at: %s", location)
            return location
    else:
        raise FileNotFoundError(
            f"WASI module 'suola.wasm' could not be found. Searched paths: {', '.join(map(str, module_locations))}"
        )
