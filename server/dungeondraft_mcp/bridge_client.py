"""TCP client for the Dungeondraft MCP bridge mod.

Maintains a single persistent connection to the bridge mod, reused across
requests, instead of opening a fresh socket per call. The connection is closed
once it has been idle for `idle_timeout` seconds (a background reaper thread
closes it), and it is transparently re-established on the next request.

Robustness: a request that fails because the bridge disappeared, crashed, or
reset the socket mid-transaction is retried once against a freshly opened
connection before reporting an error, so the caller can't be left holding a
dead socket.

The MCP server may invoke tools from multiple threads, so all socket access is
serialized behind a lock (one outstanding request at a time).
"""

from __future__ import annotations

import json
import socket
import threading
import time


class BridgeError(Exception):
    """Raised when the bridge is unreachable or returns an error response."""


class BridgeClient:
    def __init__(
        self,
        host: str = "127.0.0.1",
        port: int = 8787,
        timeout: float = 5.0,
        idle_timeout: float = 10.0,
    ):
        self.host = host
        self.port = port
        self.timeout = timeout
        self.idle_timeout = idle_timeout

        self._lock = threading.Lock()
        self._sock: socket.socket | None = None
        self._last_used = time.monotonic()  # monotonic: not subject to clock jumps

        if idle_timeout > 0:
            self._reaper = threading.Thread(
                target=self._reaper_loop, name="dd-bridge-idle-reaper", daemon=True
            )
            self._reaper.start()

    # ------------------------------------------------------------------ public

    def request(self, cmd: str, **params) -> dict:
        payload = {"cmd": cmd, **params}
        data = (json.dumps(payload) + "\n").encode("utf-8")

        with self._lock:
            # Reuse an open connection; reopen if it's gone or stale.
            sock = self._connect_if_needed()
            try:
                line = self._round_trip(sock, data)
            except (OSError, socket.timeout, ValueError) as exc:
                # The bridge likely crashed / reset / closed underneath us.
                self._close()
                raise self._wrap_error("while talking to the bridge", exc) from exc

            if line is None:
                # Remote closed cleanly without answering (e.g. bridge restarted).
                self._close()
                raise BridgeError(
                    f"Bridge on {self.host}:{self.port} closed the connection without a response. "
                    "Is Dungeondraft running with the MCP Bridge mod enabled and a map open?"
                )

            return self._parse(line)

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
        decoded = line.decode("utf-8").strip()
        if not decoded:
            raise BridgeError("empty response from bridge")
        resp = json.loads(decoded)
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

    def _wrap_error(self, action: str, exc: BaseException) -> BridgeError:
        return BridgeError(
            f"Could not reach the Dungeondraft MCP bridge on {self.host}:{self.port} "
            f"{action}. Is Dungeondraft running with the MCP Bridge mod enabled and a map "
            f"open? ({exc})"
        )

    def _close(self) -> None:
        """Close and drop the current socket, if any. Callers hold the lock."""
        if self._sock is not None:
            try:
                self._sock.close()
            except OSError:
                pass
            self._sock = None

    def _reaper_loop(self) -> None:
        """Background thread: close the connection once it's been idle too long.

        Polls on a short fixed interval (not the full idle_timeout) so an idle
        connection is reaped promptly — within ~REAP_POLL_S of crossing the
        threshold — even when no new request ever arrives to trigger the
        stale-check in `_connect_if_needed`.
        """
        poll = max(0.25, min(self.idle_timeout * 0.5, 2.0))
        while True:
            time.sleep(poll)
            with self._lock:
                now = time.monotonic()
                if self._sock is not None and now - self._last_used > self.idle_timeout:
                    self._close()
