#!/usr/bin/env python3
"""Offline tests for the TCP client's reconnect/retry behavior — no Dungeondraft needed.

Runs a fake socket server locally and checks that BridgeClient:
  - reuses a single persistent connection across requests,
  - retries on freshly opened connections (up to 3 attempts) when a stale
    socket dies remotely (closed without answering, or reset mid-response), and
  - surfaces a clean BridgeError when the bridge is unreachable or every
    attempt fails, without hanging or looping.

    python test_client.py
"""

import json
import socket
import threading

from dungeondraft_mcp.bridge_client import BridgeClient, BridgeError, BridgeUnreachableError

RECEIVED: list[dict] = []


def read_line(conn: socket.socket) -> bytes | None:
    buf = b""
    while b"\n" not in buf:
        chunk = conn.recv(4096)
        if not chunk:
            return None
        buf += chunk
    return buf.partition(b"\n")[0]


def _ok(conn: socket.socket, **kw) -> None:
    conn.sendall((json.dumps({"ok": True, "result": kw}) + "\n").encode())


def _reply_to_each(conn: socket.socket) -> None:
    while True:
        line = read_line(conn)
        if line is None:
            return
        RECEIVED.append(json.loads(line.decode()))
        _ok(conn, pong="pong")


def _close_without_reply(conn: socket.socket) -> None:
    line = read_line(conn)
    if line:
        RECEIVED.append(json.loads(line.decode()))
    conn.close()  # like the mod's idle reaper closing a socket under us


def _reset_mid_response(conn: socket.socket) -> None:
    line = read_line(conn)
    if line:
        RECEIVED.append(json.loads(line.decode()))
    try:
        conn.sendall(b"par")  # partial, no newline, then die
    except OSError:
        pass
    conn.close()


def _reject_no_map(conn: socket.socket) -> None:
    line = read_line(conn)
    if line:
        RECEIVED.append(json.loads(line.decode()))
    conn.sendall((json.dumps({"ok": False, "error": "no map open"}) + "\n").encode())


class FakeBridge:
    """Serve `handlers` on sequential connections (one handler per connection)."""

    def __init__(self, handlers):
        self.srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.srv.bind(("127.0.0.1", 0))
        self.srv.listen(4)
        self.port = self.srv.getsockname()[1]
        threading.Thread(target=self._serve, args=(list(handlers),), daemon=True).start()

    def _serve(self, handlers):
        for handler in handlers:
            conn, _ = self.srv.accept()
            try:
                handler(conn)
            finally:
                conn.close()
        self.srv.close()


def main() -> int:
    failures = 0

    def case(label, fn):
        nonlocal failures
        try:
            fn()
            print(f"  PASS  {label}")
        except AssertionError as exc:
            print(f"  FAIL  {label} -> {exc}")
            failures += 1

    def reset_received():
        RECEIVED.clear()

    def client(port, **kwargs) -> BridgeClient:
        return BridgeClient(
            "127.0.0.1", port, timeout=2.0, idle_timeout=60, watchdog=False, **kwargs
        )

    # Single persistent connection reused across calls (no reconnect churn).
    def c1():
        reset_received()
        bridge = FakeBridge([_reply_to_each])
        c = client(bridge.port)
        for _ in range(3):
            assert c.request("ping") == {"pong": "pong"}
        assert len(RECEIVED) == 3 and all(r["cmd"] == "ping" for r in RECEIVED)
    case("reuse one socket across 3 calls", c1)

    # Remote closes a still-open socket without replying (mod reaper won the
    # race) -> retried once against a fresh connection.
    def c2():
        reset_received()
        bridge = FakeBridge([_close_without_reply, _reply_to_each])
        c = client(bridge.port)
        assert c.request("get_status") == {"pong": "pong"}
        assert len(RECEIVED) == 2
    case("retry after closed-without-reply", c2)

    # Connection reset mid-response (partial bytes, then dead) -> retried once.
    def c3():
        reset_received()
        bridge = FakeBridge([_reset_mid_response, _reply_to_each])
        c = client(bridge.port)
        assert c.request("ping") == {"pong": "pong"}
        assert len(RECEIVED) == 2
    case("retry after reset mid-response", c3)

    # Both attempts dead -> single clean BridgeError, no hang, two sends max.
    def c4():
        reset_received()
        bridge = FakeBridge([_close_without_reply, _close_without_reply])
        c = client(bridge.port)
        try:
            c.request("ping")
        except BridgeError as exc:
            assert "closed the connection without a response" in str(exc)
            assert len(RECEIVED) == 2
        else:
            raise AssertionError("expected BridgeError")
    case("both attempts dead -> BridgeError", c4)

    # Bridge completely unreachable -> clean BridgeUnreachableError, no hang.
    def c5():
        c = client(1)  # nothing listens on port 1, connect fails fast here
        try:
            c.request("ping")
        except BridgeUnreachableError as exc:
            assert "Could not reach" in str(exc)
        else:
            raise AssertionError("expected BridgeUnreachableError")
    case("bridge down -> BridgeUnreachableError", c5)

    # A bridge rejection ("no map open") is a real answer, not a transport
    # failure: it must surface immediately and never be re-sent.
    def c7():
        reset_received()
        bridge = FakeBridge([_reject_no_map])
        c = client(bridge.port)
        try:
            c.request("get_status")
        except BridgeError as exc:
            assert "no map open" in str(exc)
            assert not isinstance(exc, BridgeUnreachableError)
        else:
            raise AssertionError("expected BridgeError")
        assert len(RECEIVED) == 1, RECEIVED  # exactly one send, no retry
    case("app rejection not retried", c7)

    print(f"\n{6 - failures} passed, {failures} failed.")
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())