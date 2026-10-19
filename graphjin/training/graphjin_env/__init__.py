"""Client for a GraphJin environment server.

GraphJin ships the environment; this package is the thin client a training or
evaluation loop uses to drive it. There is deliberately no trainer here: the
loop, the optimizer and the GPUs stay on your side.
"""

from .client import Environment, Episode, Task
from .step import (
    EpisodeGone,
    EpisodeTimeout,
    NothingAwaiting,
    StepEnvironment,
    StepError,
    StepState,
    group_advantages,
    group_rollout,
    run_episode,
)

__all__ = [
    "Environment",
    "Episode",
    "Task",
    "StepEnvironment",
    "StepState",
    "StepError",
    "EpisodeGone",
    "EpisodeTimeout",
    "NothingAwaiting",
    "run_episode",
    "group_rollout",
    "group_advantages",
]
