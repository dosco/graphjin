"""Driving a graded episode one model call at a time.

The usual arrangement points GraphJin at an inference endpoint and lets it call
out. A training loop often cannot work that way: the weights it is updating live
inside its own process, and standing up an HTTP server in front of them just to
be called back is a lot of machinery to get a completion across a function
boundary.

So this inverts it. The episode runs exactly as it always does — same reset,
same setup, same grading — but when the agent needs a completion the call is
parked and handed over as an observation. You send the completion back and the
episode resumes. Nothing about the scoring changes, which is the point: a policy
trained this way is measured by the same contract as one measured over the
network.

For GRPO the unit that matters is a group: several attempts at ONE task, whose
rewards are compared against each other. `group_rollout` collects one.
"""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from typing import Any, Callable

from .client import Task


class StepError(RuntimeError):
    """Raised when the environment refuses a step request."""


class EpisodeGone(StepError):
    """The episode has ended or was reclaimed after sitting idle.

    A parked episode holds one of the environment's worlds, so the server takes
    it back if nobody drives it. Seeing this means the loop was too slow or
    stopped; the world is already back in the pool.
    """


class NothingAwaiting(StepError):
    """A completion was sent while the episode was not asking for one."""


class EpisodeTimeout(StepError):
    """The episode neither asked for a completion nor finished in time."""


@dataclass
class StepState:
    """Where an episode is: what it is asking, or how it ended."""

    episode_id: str
    done: bool
    task_id: str = ""
    slug: str = ""
    observation: dict[str, Any] | None = None
    status: str = ""
    answer: str = ""
    passed: bool = False
    reward: float = 0.0
    score: dict[str, Any] = field(default_factory=dict)

    @staticmethod
    def from_json(payload: dict[str, Any]) -> "StepState":
        return StepState(
            episode_id=payload.get("episode_id", ""),
            done=bool(payload.get("done", False)),
            task_id=payload.get("task_id", ""),
            slug=payload.get("slug", ""),
            observation=payload.get("observation"),
            status=payload.get("status", ""),
            answer=payload.get("answer", ""),
            passed=bool(payload.get("pass", False)),
            reward=float(payload.get("reward", 0.0)),
            score=payload.get("score", {}) or {},
        )

    @property
    def messages(self) -> list[dict[str, str]]:
        """The rendered conversation, ready to hand to a tokenizer."""
        if not self.observation:
            return []
        return self.observation.get("messages", [])

    @property
    def stage(self) -> str:
        """Which of the run's policies is being asked.

        An agent run is several calls with different jobs. A loop training only
        the executor answers those and lets a support model take the rest —
        `env serve --step --support-model` arranges that server-side, and this
        says which one arrived anyway.
        """
        if not self.observation:
            return ""
        return self.observation.get("stage", "")


class StepEnvironment:
    """A client for `graphjin env serve --step`."""

    def __init__(self, base_url: str = "http://127.0.0.1:8090", timeout: float = 600.0):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout

    def _post(self, path: str, body: dict[str, Any]) -> dict[str, Any]:
        request = urllib.request.Request(
            f"{self.base_url}{path}", data=json.dumps(body).encode(), method="POST"
        )
        request.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                return json.loads(response.read().decode())
        except urllib.error.HTTPError as error:
            detail = error.read().decode(errors="replace")
            # The server distinguishes these deliberately, and a loop needs to
            # tell them apart: one means retry the episode, one means a bug in
            # the loop's own bookkeeping.
            if error.code == 410:
                raise EpisodeGone(detail) from error
            if error.code == 409:
                raise NothingAwaiting(detail) from error
            if error.code == 504:
                raise EpisodeTimeout(detail) from error
            raise StepError(f"{path} failed with {error.code}: {detail}") from error

    def reset(self, task: Task | str, *, reward_profile: str | None = None) -> StepState:
        """Start an episode and return the first thing the model is asked."""
        body: dict[str, Any] = {}
        if isinstance(task, Task):
            body["task_id"] = task.task_id
        else:
            body["slug"] = task
        if reward_profile:
            body["reward_profile"] = reward_profile
        return StepState.from_json(self._post("/step/reset", body))

    def step(
        self,
        episode_id: str,
        completion: str,
        *,
        prompt_tokens: int = 0,
        completion_tokens: int = 0,
    ) -> StepState:
        """Send one completion and get whatever comes next.

        The token counts are reported rather than measured: efficiency is a term
        in the reward, so a loop that omits them scores as though its policy were
        free.
        """
        return StepState.from_json(
            self._post(
                "/step",
                {
                    "episode_id": episode_id,
                    "completion": completion,
                    "prompt_tokens": prompt_tokens,
                    "completion_tokens": completion_tokens,
                },
            )
        )

    def abandon(self, episode_id: str) -> None:
        """End an episode early and give its world back."""
        request = urllib.request.Request(
            f"{self.base_url}/step/{episode_id}", method="DELETE"
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout):
                return
        except urllib.error.HTTPError as error:
            if error.code == 410:
                return  # Already gone, which is what was wanted.
            raise StepError(f"abandon failed with {error.code}") from error


# A completion function is handed the current state and returns the model's
# text plus, optionally, the tokens it cost.
Completer = Callable[[StepState], "str | tuple[str, int, int]"]


def run_episode(
    env: StepEnvironment,
    task: Task | str,
    complete: Completer,
    *,
    reward_profile: str | None = None,
    max_turns: int = 32,
) -> StepState:
    """Drive one episode to its graded end.

    Whatever the agent asks gets answered, however many times it asks. The turn
    cap is a guard against a loop that never terminates, not a model budget —
    the environment enforces the real one.
    """
    state = env.reset(task, reward_profile=reward_profile)
    turns = 0
    try:
        while not state.done:
            if turns >= max_turns:
                env.abandon(state.episode_id)
                raise StepError(f"episode {state.episode_id} asked for more than {max_turns} completions")
            result = complete(state)
            if isinstance(result, tuple):
                text, prompt_tokens, completion_tokens = result
            else:
                text, prompt_tokens, completion_tokens = result, 0, 0
            state = env.step(
                state.episode_id, text,
                prompt_tokens=prompt_tokens, completion_tokens=completion_tokens,
            )
            turns += 1
    except EpisodeGone:
        raise
    except BaseException:
        # A crash mid-episode would otherwise leave a world parked until the
        # server's idle timer reclaims it.
        env.abandon(state.episode_id)
        raise
    return state


def group_rollout(
    base_url: str,
    task: Task | str,
    n: int,
    complete: Completer,
    *,
    concurrency: int = 1,
    reward_profile: str | None = None,
    timeout: float = 600.0,
) -> list[StepState]:
    """Collect one GRPO group: n attempts at a single task.

    The advantage a group produces is each attempt's reward against the group's
    own mean, so the attempts have to be at the same task and they have to be
    able to differ. If every reward comes back identical the policy is sampling
    greedily and the group carries no signal — check the server's temperature
    before looking anywhere else.

    Concurrency is bounded by how many worlds the server pooled: a request for
    one blocks until a world frees, so asking for more parallelism than
    `--pool N` just queues.
    """
    if n < 1:
        raise ValueError(f"a group needs at least one attempt, got {n}")
    workers = max(1, min(n, concurrency))

    def attempt(_: int) -> StepState:
        env = StepEnvironment(base_url, timeout=timeout)
        return run_episode(env, task, complete, reward_profile=reward_profile)

    if workers == 1:
        return [attempt(index) for index in range(n)]
    with ThreadPoolExecutor(max_workers=workers) as pool:
        return list(pool.map(attempt, range(n)))


def group_advantages(group: list[StepState]) -> list[float]:
    """Each attempt's reward relative to the group's mean.

    This is the whole reason a group is the unit: with no baseline model to
    compare against, the other attempts at the same question are the baseline.
    An all-identical group yields all zeros, which is correct and also a signal
    that nothing was learned from it.
    """
    if not group:
        return []
    mean = sum(state.reward for state in group) / len(group)
    return [state.reward - mean for state in group]
