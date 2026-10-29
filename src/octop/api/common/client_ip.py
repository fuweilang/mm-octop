"""Resolve the client address for security-sensitive decisions.

``X-Forwarded-For`` is caller-supplied, so anything that throttles or rate-limits
on it can be reset by varying one header. The rules here keep that header usable
behind a reverse proxy while refusing to trust it from an arbitrary peer:

* The direct peer address is always trusted.
* ``X-Forwarded-For`` is honoured only when the peer is loopback or a private
  address, i.e. a reverse proxy on this host, in the same container network, or
  on the same LAN (nginx on the host reaching a container's bridge address).
* From that header the **right-most** entry is taken. A trusted proxy appends the
  address it observed, so the right-most value is the one it actually saw; values
  to its left may have been supplied by the client itself.
"""

from __future__ import annotations

import ipaddress
from typing import Any

UNKNOWN_CLIENT = "unknown"


def _peer_address(request: Any) -> str:
    client = getattr(request, "client", None)
    host = getattr(client, "host", None) if client is not None else None
    return host.strip() if isinstance(host, str) and host.strip() else ""


# Peers we accept as a reverse proxy: loopback, RFC1918 / ULA, link-local.
# Explicitly listed because ``ipaddress.is_private`` also flags the
# documentation ranges (203.0.113.0/24 …) on newer Pythons.
_TRUSTED_PROXY_NETWORKS = tuple(
    ipaddress.ip_network(cidr)
    for cidr in (
        "127.0.0.0/8",
        "::1/128",
        "10.0.0.0/8",
        "172.16.0.0/12",
        "192.168.0.0/16",
        "169.254.0.0/16",
        "fc00::/7",
        "fe80::/10",
    )
)


def _is_trusted_proxy(address: str) -> bool:
    """Loopback or private peer ⇒ a proxy we control, not the end user."""
    try:
        ip = ipaddress.ip_address(address)
    except ValueError:
        return False
    return any(
        ip in network for network in _TRUSTED_PROXY_NETWORKS if network.version == ip.version
    )


def _forwarded_for_rightmost(request: Any) -> str:
    raw = request.headers.get("x-forwarded-for")
    if not raw:
        return ""
    hops = [part.strip() for part in raw.split(",") if part.strip()]
    return hops[-1] if hops else ""


def resolve_client_ip(request: Any) -> str:
    """The address to key rate limits on.

    Falls back to :data:`UNKNOWN_CLIENT` when the ASGI scope carries no peer
    (e.g. an in-process test client), which collapses those callers into a single
    bucket rather than giving each request a fresh one.

    When the direct peer is loopback or private (local nginx, docker bridge,
    sidecar), prefer ``X-Forwarded-For`` (right-most) then ``X-Real-IP``.
    """
    peer = _peer_address(request)
    if peer and _is_trusted_proxy(peer):
        forwarded = _forwarded_for_rightmost(request)
        if forwarded:
            return forwarded
        real_ip = (request.headers.get("x-real-ip") or "").strip()
        if real_ip:
            return real_ip
    return peer or UNKNOWN_CLIENT
