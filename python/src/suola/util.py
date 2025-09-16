from collections import namedtuple
import importlib.metadata
import logging
from packaging.version import parse as parse_version
from pathlib import Path
import threading
from typing import Optional

from platformdirs import PlatformDirs

# Default package metadata
_DEFAULT_MAINTAINER = "Klikkikuri"
_DEFAULT_VERSION = "0.0.0-dev0"

PkgInfo = namedtuple("PkgInfo", ["appname", "appauthor", "version"])
logger = logging.getLogger(__name__)

# Global singleton for platform directories (with thread safety)
_platform_dirs: Optional[PlatformDirs] = None
_platform_dirs_lock = threading.Lock()

def get_pkg_info(pkg_name: str | None = __package__) -> PkgInfo:
    """Retrieve metadata for a given package.

    :param pkg_name: Name of the package to get metadata for
    :type pkg_name: str
    :returns: PkgInfo namedtuple containing appname, appauthor, and version
    :raises ValueError: If the package is not found or name is empty
    """

    if not pkg_name:
        raise ValueError("Package name cannot be empty")
        
    # Extract the app name from the package name (use first part before any dots)
    appname = pkg_name.split('.')[0]
    appauthor = _DEFAULT_MAINTAINER
    version = _DEFAULT_VERSION

    try:
        metadata = importlib.metadata.metadata(pkg_name)
        
        # Try to get maintainer information in order of preference
        if "Maintainer" in metadata:
            appauthor = metadata["Maintainer"]
        elif "Maintainer-email" in metadata:
            maintainer_email = metadata["Maintainer-email"]
            # Extract name from "Name <email>" format
            if "<" in maintainer_email:
                appauthor = maintainer_email.split("<")[0].strip()
        elif "Author" in metadata:
            appauthor = metadata["Author"]
        elif "Author-email" in metadata:
            author_email = metadata["Author-email"]
            if "<" in author_email:
                appauthor = author_email.split("<")[0].strip()

        # Get version if available
        if "Version" in metadata:
            version = metadata["Version"]

    except importlib.metadata.PackageNotFoundError:
        raise ValueError(f"Package '{pkg_name}' not found")

    result = PkgInfo(appname=appname, appauthor=appauthor, version=version)

    return result


def _parse_major_minor_version(version: str) -> str:
    """Extract major.minor version from a version string.
    
    :param version: Version string (e.g., "1.2.3-dev0")
    :returns: Major.minor version string (e.g., "1.2")
    :raises ValueError: If version string is invalid
    """

    parsed_version = parse_version(version)
    return f"{parsed_version.major}.{parsed_version.minor}"


def init_platform_dirs() -> PlatformDirs:
    """Initialize platform directories for the package.

    :returns: PlatformDirs instance configured for this package
    :raises ValueError: If package information cannot be retrieved
    """
    try:
        pkg_info = get_pkg_info()
        version = _parse_major_minor_version(pkg_info.version)

        dirs = PlatformDirs(
            appname=pkg_info.appname,
            appauthor=pkg_info.appauthor,
            version=version
        )

        return dirs
        
    except Exception as e:
        raise ValueError(f"Failed to initialize platform directories: {e}") from e


def get_platform_dirs() -> PlatformDirs:
    """Get the platform directories for the package.
    
    Initializes platform directories if not already done.

    :returns: PlatformDirs instance for this package
    """
    global _platform_dirs
    if _platform_dirs is None:
        with _platform_dirs_lock:
            logger.debug("Initializing platform directories")
            _platform_dirs = init_platform_dirs()
    return _platform_dirs


def get_data_dir() -> Path:
    """Get the user data directory for the package.
    
    Creates the directory if it doesn't exist.
    
    :returns: Path to the user data directory
    """
    dirs = get_platform_dirs()
    data_dir = Path(dirs.user_data_dir)
    data_dir.mkdir(parents=True, exist_ok=True)
    return data_dir


def get_config_dir() -> Path:
    """Get the user configuration directory for the package.
    
    Creates the directory if it doesn't exist.
    
    :returns: Path to the user configuration directory
    :rtype: Path
    """
    dirs = get_platform_dirs()
    config_dir = Path(dirs.user_config_dir)
    config_dir.mkdir(parents=True, exist_ok=True)
    return config_dir


def get_cache_dir() -> Path:
    """Get the user cache directory for the package.
    
    Creates the directory if it doesn't exist.
    
    :returns: Path to the user cache directory
    :rtype: Path
    """
    dirs = get_platform_dirs()
    cache_dir = Path(dirs.user_cache_dir)
    cache_dir.mkdir(parents=True, exist_ok=True)
    return cache_dir
