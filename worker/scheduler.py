"""Per-session bounded, stable priority queue; completed IDs remain deduplicated."""

import heapq
import time
from datetime import datetime

PRIORITY = {
    kind: i
    for i, kind in enumerate(("manual", "gift", "like", "comment", "welcome", "host"))
}


def timestamp(value):
    if isinstance(value, (float, int)):
        return float(value)
    return datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()


class Scheduler:
    def __init__(self, clock=time.time):
        self.clock = clock
        self.items = []
        self.seen = set()
        self.sequence = 0

    def _purge(self):
        self.items = [item for item in self.items if item[2]["_expires"] > self.clock()]
        heapq.heapify(self.items)

    def add(self, task):
        now = self.clock()
        created = timestamp(task["createdAt"])
        expires = min(timestamp(task["expiresAt"]), created + 120)
        if (
            task["kind"] not in PRIORITY
            or not isinstance(task["text"], str)
            or not 1 <= len(task["text"].strip()) <= 240
        ):
            raise ValueError("invalid task")
        if created > now + 5 or not isinstance(task["id"], str) or not task["id"]:
            raise ValueError("invalid task identity/time")
        self._purge()
        if expires <= now or task["id"] in self.seen:
            return False
        if len(self.items) >= 100:
            raise OverflowError("task queue full")
        # Bound replay memory as well; refuse new work rather than forgetting old IDs.
        if len(self.seen) >= 100000:
            raise OverflowError("session task limit reached")
        self.seen.add(task["id"])
        self.sequence += 1
        heapq.heappush(
            self.items,
            (PRIORITY[task["kind"]], self.sequence, dict(task, _expires=expires)),
        )
        return True

    def pop(self):
        self._purge()
        return heapq.heappop(self.items)[2] if self.items else None

    def __len__(self):
        self._purge()
        return len(self.items)
