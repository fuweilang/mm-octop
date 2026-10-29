"""Argon2id password hashing and change-password policy checks."""

from __future__ import annotations

from argon2 import PasswordHasher
from argon2.exceptions import InvalidHashError, VerificationError, VerifyMismatchError

from octop.infra.errors import ErrorCode, OctopError

_HASHER = PasswordHasher()

MIN_PASSWORD_LENGTH = 6

# Kept for docker-entrypoint parity tests. Product policy no longer denylists.
_COMMON_PASSWORDS: frozenset[str] = frozenset()


def hash_password(plain: str) -> str:
    if not plain:
        raise ValueError("password must not be empty")
    return _HASHER.hash(plain)


def verify_password(plain: str, hashed: str) -> bool:
    try:
        return _HASHER.verify(hashed, plain)
    except (VerifyMismatchError, VerificationError, InvalidHashError):
        return False


def validate_password_policy(password: str, *, old_password: str | None = None) -> None:
    """Raise ``OctopError`` when ``password`` is empty/too short or equals ``old_password``.

    Floor only: at least ``MIN_PASSWORD_LENGTH`` characters. No letter/digit mix
    and no common-password denylist.
    """
    if old_password is not None and password == old_password:
        raise OctopError(
            ErrorCode.PASSWORD_SAME_AS_OLD,
            "new password must differ from current password",
        )
    if len(password) < MIN_PASSWORD_LENGTH:
        raise OctopError(ErrorCode.PASSWORD_TOO_WEAK, "password too short")
