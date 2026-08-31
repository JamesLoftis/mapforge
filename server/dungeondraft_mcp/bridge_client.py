"""TCP client for the Dungeondraft MCP bridge mod.

Maintains a single persistent connection to the bridge mod, reused across
requests. A background watchdog keeps it alive: while idle it probes the mod
with a cheap ping every few seconds and tears down the socket the moment the
probe fails, so a dead or restarted bridge is detected and re-established
before a real request ever runs.

Failures are classified: transport problems (unreachable, reset mid-reply,
closed without responding) surface as BridgeUnreachableError and are
transparently retried — up to `attempts` times, each on a fresh connection —
while application rejections from the bridge itself (e.g. "no map open", a bad
asset path) raise BridgeError immediately and are never retried.

The MCP server may invoke tools from multiple threads, so all socket access is
serialized behind a lock (one outstanding request at a time).
"""

from __future__ import annotations

import json
import socket
import threading
import time


class BridgeError(Exception):
    """Raised when the bridge itself rejects a request (e.g. no map open)."""


class BridgeUnreachableError(BridgeError):
    """Raised when the bridge cannot be talked to at all (transport failure)."""


class BridgeClient:
    def __init__(
        self,
        host: str = "127.0.0.1",
        port: int = 8787,
        timeout: float = 5.0,
        idle_timeout: float = 10.0,
        attempts: int = 3,
        watchdog: bool = True,
    ):
        self.host = host
        self.port = port
        self.timeout = timeout
        self.attempts = max(1, attempts)
        self.idle_timeout = idle_timeout
        self.watchdog = watchdog

        self._lock = threading.Lock()
        self._sock: socket.socket | None = None
        self._last_used = time.monotonic()  # monotonic: not subject to clock jumps

        if idle_timeout > 0 and watchdog:
            self._keeper = threading.Thread(
                target=self._watchdog_loop, name="dd-bridge-watchdog", daemon=True
            )
            self._keeper.start()

    # ------------------------------------------------------------------ public

    def request(self, cmd: str, **params) -> dict:
        payload = {"cmd": cmd, **params}
        data = (json.dumps(payload) + "\n").encode("utf-8")

        with self._lock:
            # Reuse an open connection; reopen if it's gone or stale by our own
            # idle_timeout. A reused socket can still have been closed on the
            # far end, so on any TRANSPORT failure we close it and retry on a
            # fresh connection. Bridge rejections (`ok: false` replies) are
            # returned to the caller as-is and never retried.
            last_err: BridgeUnreachableError | None = None
            saw_closed_without_reply = False
            for attempt in range(self.attempts):
                try:
                    sock = self._connect_if_needed()
                except BridgeUnreachableError as exc:
                    # Dead bridge / port busy / hung backlog. _connect already
                    # wrapped the OS failure in a user-facing message.
                    self._close()
                    last_err = exc
                    time.sleep(self._backoff(attempt))
                    continue
                try:
                    line = self._round_trip(sock, data)
                except (OSError, socket.timeout, ValueError) as exc:
                    # Stale reused socket (remote reaped it) or genuine reset
                    # mid-reply: transport trouble. Drop it and retry fresh.
                    self._close()
                    last_err = self._wrap_error("while talking to the bridge", exc)
                    time.sleep(self._backoff(attempt))
                    continue
                if line is None:
                    # Remote closed cleanly without answering — stale reused
                    # connection (e.g. the mod dropped a peer it never saw
                    # close). Retry fresh; nothing is actually broken.
                    self._close()
                    saw_closed_without_reply = True
                    last_err = BridgeUnreachableError(
                        f"Bridge on {self.host}:{self.port} closed the connection without a response."
                    )
                    time.sleep(self._backoff(attempt))
                    continue
                return self._parse(line)

            if saw_closed_without_reply:
                raise BridgeUnreachableError(
                    f"Bridge on {self.host}:{self.port} closed the connection without a response. "
                    "Is Dungeondraft running with the MCP Bridge mod enabled and a map open?"
                )
            if last_err is not None:
                raise last_err
            raise BridgeUnreachableError(
                f"Bridge on {self.host}:{self.port} was reachable but never answered "
                f"within {self.timeout}s. Is Dungeondraft running with the MCP Bridge mod enabled?"
            )

    # ------------------------------------------------------------- internals

    def _round_trip(self, sock: socket.socket, data: bytes) -> bytes | None:
        """Send one request and read exactly one newline-terminated reply.

        Reads until a newline, so a reply that arrives in multiple TCP segments
        is reassembled. Returns the raw reply bytes, or None if the peer closed
        before delivering one.
        """
        sock.settimeout(self.timeout)
        sock.sendall(data)
        self._last_used = time.monotonic()

        buf = b""
        while b"\n" not in buf:
            chunk = sock.recv(4096)
            if not chunk:
                return None  # peer closed without a response
            buf += chunk
        self._last_used = time.monotonic()
        line, _, _ = buf.partition(b"\n")
        return line

    def _parse(self, line: bytes) -> dict:
        decoded = line.decode("utf-8", "replace").strip()
        if not decoded:
            raise BridgeError("empty response from bridge")
        try:
            resp = json.loads(decoded)
        except ValueError as exc:
            raise BridgeError(f"invalid response from bridge: {exc}") from exc
        if not resp.get("ok"):
            raise BridgeError(resp.get("error", "unknown bridge error"))
        return resp.get("result", {})

    def _connect_if_needed(self) -> socket.socket:
        now = time.monotonic()
        # If the existing socket is stale (idle too long or previously closed),
        # drop it so we reconnect fresh below.
        if self._sock is not None and now - self._last_used > self.idle_timeout:
            self._close()
        if self._sock is None:
            self._sock = self._connect()
        return self._sock

    def _connect(self) -> socket.socket:
        try:
            sock = socket.create_connection((self.host, self.port), timeout=self.timeout)
            sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
            self._last_used = time.monotonic()
            return sock
        except OSError as exc:
            raise self._wrap_error("connecting to the bridge", exc) from exc

    def _wrap_error(self, action: str, exc: BaseException) -> BridgeUnreachableError:
        return BridgeUnreachableError(
            f"Could not reach the Dungeondraft MCP bridge on {self.host}:{self.port} "
            f"{action}. Is Dungeondraft running with the MCP Bridge mod enabled and a map "
            f"open? ({exc})"
        )

    @staticmethod
    def _backoff(attempt: int) -> float:
        """Small linear backoff between retry attempts (fractions of a second)."""
        return 0.05 * (attempt + 1)

    def _close(self) -> None:
        """Close and drop the current socket, if any. Callers hold the lock."""
        if self._sock is not None:
            try:
                self._sock.close()
            except OSError:
                pass
            self._sock = None

    def _watchdog_loop(self) -> None:
        """Background thread: keep the link alive and heal it pre-emptively.

        While the connection is open but idle, probes the mod with a cheap ping
        roughly every `keepalive` seconds and closes the socket the instant a
        probe fails — so a dead/restarted bridge is detected here, in the
        background, and the next real request silently reconnects instead of
        surfacing an error. Also reaps a socket that outlives `idle_timeout`
        (with the probes keeping it alive, that now only happens if idle_timeout
        is configured shorter than keepalive).
        """
        poll = max(0.25, min(self.idle_timeout * 0.5, 2.0))
        keepalive = min(5.0, max(1.0, self.idle_timeout * 0.5))
        while True:
            time.sleep(poll)
            with self._lock:
                now = time.monotonic()
                if self._sock is None:
                    continue
                idle = now - self._last_used
                if idle > self.idle_timeout:
                    self._close()
                elif idle > keepalive:
                    self._watchdog_ping()

    def _watchdog_ping(self) -> None:
        """Probe the mod once; close the socket on failure. Caller holds the lock.

        Uses a short timeout so a dead bridge is detected quickly in the
        background instead of stalling a real request behind the lock.
        """
        sock = self._sock
        if sock is None:
            return
        sock.settimeout(min(self.timeout, 1.5))
        try:
            sock.sendall(b'{"cmd":"ping"}\n')
            buf = b""
            while b"\n" not in buf:
                chunk = sock.recv(4096)
                if not chunk:
                    raise OSError("bridge closed without replying")
                buf += chunk
            self._last_used = time.monotonic()
        except (OSError, socket.timeout, ValueError):
            self._close()
