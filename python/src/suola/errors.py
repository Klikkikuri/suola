"""Exceptions that Suola raises.

Catch :class:`SuolaError` for all of them. :class:`InvalidURLError` and :class:`InvalidRulesError` are also
:class:`ValueError`, because the caller gave a bad input. :class:`SuolaRuntimeError` is a :class:`RuntimeError`,
because the module or the host failed.
"""


class SuolaError(Exception):
    """Base class of all Suola errors."""


class InvalidURLError(SuolaError, ValueError):
    """The URL cannot be signed, for example because it is empty, too long or has no host."""


class InvalidRulesError(SuolaError, ValueError):
    """The rules cannot be loaded, for example because they are empty, too large or not valid JSON rules."""


class SuolaRuntimeError(SuolaError, RuntimeError):
    """The WASI module or the host failed: no entry point, a trap, a failed allocation or a bad result."""
