"""Client for a GraphJin environment server.

GraphJin ships the environment; this package is the thin client a training or
evaluation loop uses to drive it. There is deliberately no trainer here: the
loop, the optimizer and the GPUs stay on your side.
"""

from .client import Environment, Episode, Task

__all__ = ["Environment", "Episode", "Task"]
