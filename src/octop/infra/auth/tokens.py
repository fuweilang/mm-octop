"""Access-token JWT helpers shared by HTTP auth and infra (Bridge)."""

from __future__ import annotations

import time
from typing import Any

import jwt


class InvalidToken(Exception): ...


class TokenExpired(InvalidToken): ...


def sign_token(
    secret: bytes,
    *,
    sub: int,
    uname: str,
    role: str,
    ttl_seconds: int = 86400,
) -> str:
    now = int(time.time())
    payload = {
        "sub": str(sub),
        "uname": uname,
        "role": role,
        "iat": now,
        "exp": now + ttl_seconds,
    }
    return jwt.encode(payload, secret, algorithm="HS256")


def decode_token(secret: bytes, token: str) -> dict[str, Any]:
    try:
        payload = jwt.decode(token, secret, algorithms=["HS256"])
        if "sub" in payload:
            payload["sub"] = int(payload["sub"])
        return payload
    except jwt.ExpiredSignatureError as exc:
        raise TokenExpired() from exc
    except jwt.InvalidTokenError as exc:
        raise InvalidToken(str(exc)) from exc


def decode_expired_token_within_grace(
    secret: bytes, token: str, grace_seconds: int
) -> dict[str, Any]:
    """Decode a token that may already be expired, as long as it expired recently.

    Desktop clients keep the SPA open for days on a single token; ``exp`` alone
    would force a re-login (and break the 中台 handoff) after TTL. The grace
    window bounds how long an expired token may still be exchanged for a fresh
    one, so a leaked token does not stay useful indefinitely.
    """
    try:
        payload = jwt.decode(
            token,
            secret,
            algorithms=["HS256"],
            options={"verify_exp": False},
        )
    except jwt.InvalidTokenError as exc:
        raise InvalidToken(str(exc)) from exc
    exp = payload.get("exp")
    if not isinstance(exp, int):
        raise InvalidToken("missing exp")
    grace = max(0, int(grace_seconds))
    if int(time.time()) > exp + grace:
        raise TokenExpired()
    if "sub" in payload:
        payload["sub"] = int(payload["sub"])
    return payload
