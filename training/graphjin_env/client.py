"""Minimal client for `graphjin env serve`.

The server grades episodes with the same function GraphJin's own benchmark
uses, so a reward collected here and a number on the public board mean the same
thing. Nothing in this file computes a reward itself, on purpose: a second
implementation of the contract would drift from the first, and the drift shows
up as a model that improves on one number and not the other.
"""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Iterator


@dataclass(frozen=True)
class Task:
    task_id: str
    slug: str
    prompt: str
    category: str = ""
    difficulty: str = ""
    writes: bool = False
    family: str = ""

    @staticmethod
    def from_json(payload: dict[str, Any]) -> "Task":
        return Task(
            task_id=payload["task_id"],
            slug=payload.get("slug", ""),
            prompt=payload.get("prompt", ""),
            category=payload.get("category", ""),
            difficulty=payload.get("difficulty", ""),
            writes=bool(payload.get("writes", False)),
            family=payload.get("family", ""),
        )


@dataclass
class Episode:
    task_id: str
    slug: str
    status: str
    answer: str
    passed: bool
    reward: float
    latency_ms: int = 0
    score: dict[str, Any] = field(default_factory=dict)
    trajectory: dict[str, Any] | None = None

    @staticmethod
    def from_json(payload: dict[str, Any]) -> "Episode":
        return Episode(
            task_id=payload.get("task_id", ""),
            slug=payload.get("slug", ""),
            status=payload.get("status", ""),
            answer=payload.get("answer", ""),
            passed=bool(payload.get("pass", False)),
            reward=float(payload.get("reward", 0.0)),
            latency_ms=int(payload.get("latency_ms", 0)),
            score=payload.get("score", {}) or {},
            trajectory=payload.get("trajectory"),
        )

    @property
    def steps(self) -> list[dict[str, Any]]:
        if not self.trajectory:
            return []
        return self.trajectory.get("steps", [])


class EnvironmentError_(RuntimeError):
    """Raised when the environment refuses or fails a request."""


class Environment:
    def __init__(self, base_url: str = "http://127.0.0.1:8090", timeout: float = 600.0):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout

    def _request(self, path: str, body: dict[str, Any] | None = None) -> dict[str, Any]:
        url = f"{self.base_url}{path}"
        data = None
        if body is not None:
            data = json.dumps(body).encode()
        request = urllib.request.Request(url, data=data, method="POST" if data else "GET")
        request.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                return json.loads(response.read().decode())
        except urllib.error.HTTPError as error:  # pragma: no cover - network shape
            detail = error.read().decode(errors="replace")
            raise EnvironmentError_(f"{path} failed with {error.code}: {detail}") from error

    def health(self) -> dict[str, Any]:
        """Return what the environment is serving.

        Worth recording alongside any result: the dataset fingerprint and reward
        version are what make two runs comparable, and a run that cannot name
        them cannot be compared with anything.
        """
        return self._request("/health")

    def tasks(self) -> list[Task]:
        payload = self._request("/tasks")
        return [Task.from_json(item) for item in payload.get("tasks", [])]

    def run(
        self,
        task: Task | str | None = None,
        *,
        include_trajectory: bool = False,
        reward_profile: str | None = None,
        repeat: int = 0,
    ) -> Episode:
        """Run one graded episode.

        The environment leases one isolated world for the episode's duration, so
        a task that writes changes only the world it was given.
        """
        body: dict[str, Any] = {"include_trajectory": include_trajectory, "repeat": repeat}
        if isinstance(task, Task):
            body["task_id"] = task.task_id
        elif isinstance(task, str):
            body["slug"] = task
        if reward_profile:
            body["reward_profile"] = reward_profile
        return Episode.from_json(self._request("/episodes", body))

    def rollout(self, tasks: list[Task], **kwargs: Any) -> Iterator[Episode]:
        for task in tasks:
            yield self.run(task, **kwargs)
