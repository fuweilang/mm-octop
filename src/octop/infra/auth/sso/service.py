"""SSO service orchestration for OIDC and pluggable OAuth providers."""

from __future__ import annotations

import asyncio
import logging
import os
import secrets
import time
from collections.abc import Mapping
from dataclasses import dataclass, replace
from functools import partial
from typing import Any
from urllib.parse import parse_qs, quote, urlencode, urlsplit

import httpx
import jwt

from octop.infra.auth.sso.crypto import decrypt_secret, encrypt_secret
from octop.infra.auth.sso.discovery import DiscoveryCache
from octop.infra.auth.sso.id_token import verify_id_token
from octop.infra.auth.sso.pkce import new_pkce_pair
from octop.infra.auth.sso.providers import build_adapters
from octop.infra.auth.sso.providers.base import SSO_KINDS, IdentityProvider
from octop.infra.auth.sso.public_base import build_redirect_uri, parse_strict_origin
from octop.infra.auth.sso.redirect_after import sanitize_redirect_after
from octop.infra.db.repos.secrets import SecretRepo
from octop.infra.db.repos.sso import SsoProviderRow
from octop.infra.db.services import SharedServices
from octop.infra.errors import ErrorCode, OctopError
from octop.infra.users.identity import User
from octop.infra.users.manager import UserManager

_LOGIN_STATE_TTL_SECONDS = 600
_LOGIN_CODE_TTL_SECONDS = 60
_CONSOLE_HANDOFF_CLAIM = "_mammoth_console_handoff"
_CONSOLE_HANDOFF_SETTING = "mammoth.console_handoff.{user_id}"
_CONSOLE_NEXT_ALLOWED = frozenset({"/admin", "/console/", "/console"})
_HTTP_TIMEOUT = httpx.Timeout(10.0, connect=5.0)
_DESKTOP_OIDC_CLIENT_ID = "mammoth-octop"
_PLATFORM_LOGIN_PATH = "/console/api/auth/login"
_PLATFORM_REGISTER_PATH = "/console/api/auth/register"

logger = logging.getLogger(__name__)

# Installed desktop must log in against production, not a dev issuer left in ~/.octop.
_DESKTOP_OIDC_ISSUER_ENV = "OCTOP_MAMMOTH_OIDC_ISSUER"


def desktop_oidc_issuer() -> str:
    return os.environ.get(_DESKTOP_OIDC_ISSUER_ENV, "").strip().rstrip("/")


def desktop_platform_origin() -> str:
    """Origin of the 猛犸中台. Empty in local gateway dev, so the page origin is used."""
    issuer = desktop_oidc_issuer()
    if not issuer:
        return ""
    parts = urlsplit(issuer)
    if not parts.scheme or not parts.netloc:
        return ""
    return f"{parts.scheme}://{parts.netloc}"


def provider_for_desktop(provider: SsoProviderRow) -> SsoProviderRow:
    issuer = desktop_oidc_issuer()
    if not issuer or provider.kind != "oidc":
        return provider
    return replace(provider, issuer=issuer, client_id=_DESKTOP_OIDC_CLIENT_ID)


@dataclass(frozen=True)
class RedirectResult:
    """A browser redirect produced by an OIDC callback."""

    url: str


def _login_code_from_redirect(url: str) -> str:
    code = parse_qs(urlsplit(url).fragment).get("code", [""])[0]
    if not code:
        raise ValueError("exchange")
    return code


def _code_state_from_authorize(response: httpx.Response) -> tuple[str, str]:
    if response.status_code not in {302, 303, 307}:
        raise ValueError("exchange")
    location = response.headers.get("location") or ""
    parts = urlsplit(location)
    if parts.path.rstrip("/").endswith("/login"):
        raise ValueError("exchange")
    query = parse_qs(parts.query)
    code = (query.get("code") or [""])[0]
    state = (query.get("state") or [""])[0]
    if not code or not state:
        raise ValueError("exchange")
    return code, state


def _with_agent_prefix(frontend_base: str) -> str:
    base = frontend_base.rstrip("/")
    return base if base.endswith("/agent") else f"{base}/agent"


class SsoService:
    """Coordinates SSO configuration and browser login / bind flows."""

    def __init__(self, services: SharedServices, user_manager: UserManager) -> None:
        self._services = services
        self._user_manager = user_manager
        self._discovery = DiscoveryCache()
        self._adapters: dict[str, IdentityProvider] = build_adapters(self)

    def status(self) -> dict[str, bool | str]:
        provider = self._services.sso_repo.get_provider()
        adapter = self._adapters["oidc"]
        enabled = bool(
            provider
            and provider.enabled
            and adapter.is_configured(provider)
            and self._user_manager.count() > 0
        )
        return {
            "enabled": enabled,
            "display_name": provider.display_name if provider is not None else "",
        }

    def providers_status(self) -> dict[str, list[dict[str, bool | str]]]:
        rows = {row.kind: row for row in self._services.sso_repo.list()}
        users_exist = self._user_manager.count() > 0
        providers: list[dict[str, bool | str]] = []
        for kind in SSO_KINDS:
            adapter = self._adapters[kind]
            row = rows.get(kind)
            enabled = bool(
                row is not None and row.enabled and adapter.is_configured(row) and users_exist
            )
            providers.append(
                {
                    "kind": kind,
                    "display_name": row.display_name if row is not None else "",
                    "enabled": enabled,
                }
            )
        return {
            "providers": providers,
            "platform_origin": desktop_platform_origin(),
        }

    def get_config_for_admin(self, *, public_base: str) -> dict[str, Any]:
        config = self.get_config_for_kind("oidc", public_base=public_base)
        config.pop("kind", None)
        config.pop("extra", None)
        return config

    def get_config_for_kind(self, kind: str, *, public_base: str) -> dict[str, Any]:
        adapter = self._adapter(kind)
        provider = self._services.sso_repo.get_by_kind(kind)
        redirect_uri = build_redirect_uri(public_base, adapter.callback_path)
        if provider is None:
            config: dict[str, Any] = {
                "kind": kind,
                "enabled": False,
                "display_name": "",
                "issuer": "",
                "client_id": "",
                "scopes": adapter.default_scopes,
                "dashboard_origin": None,
                "has_client_secret": False,
                "redirect_uri": redirect_uri,
                "extra": {},
            }
            return config
        return {
            "kind": kind,
            "enabled": bool(provider.enabled),
            "display_name": provider.display_name,
            "issuer": provider.issuer,
            "client_id": provider.client_id,
            "scopes": provider.scopes,
            "dashboard_origin": provider.dashboard_origin,
            "has_client_secret": provider.client_secret_enc is not None,
            "redirect_uri": redirect_uri,
            "extra": dict(provider.extra),
        }

    def put_config(
        self,
        body: Mapping[str, object],
        *,
        secret_repo: SecretRepo | None = None,
        public_base: str | None = None,
    ) -> dict[str, Any]:
        result = self.put_config_for_kind(
            "oidc", body, secret_repo=secret_repo, public_base=public_base
        )
        result.pop("kind", None)
        result.pop("extra", None)
        return result

    def put_config_for_kind(
        self,
        kind: str,
        body: Mapping[str, object],
        *,
        secret_repo: SecretRepo | None = None,
        public_base: str | None = None,
    ) -> dict[str, Any]:
        adapter = self._adapter(kind)
        current = self._services.sso_repo.get_by_kind(kind)
        secret = body.get("client_secret")
        encrypted_secret: bytes | None = None
        if isinstance(secret, str) and secret:
            encrypted_secret = encrypt_secret(secret_repo or self._services.secret_repo, secret)

        extra = dict(current.extra) if current is not None else {}
        raw_extra = body.get("extra")
        if isinstance(raw_extra, Mapping):
            extra.update({str(key): value for key, value in raw_extra.items()})
        if kind == "feishu":
            region = extra.get("region", "feishu")
            extra = {"region": "lark" if region == "lark" else "feishu"}
        elif kind == "wecom":
            agent_raw = extra.get("agent_id", "")
            agent_id = str(agent_raw).strip() if agent_raw is not None else ""
            extra = {"agent_id": agent_id}
        elif kind == "dingtalk":
            extra = {}

        dashboard_origin = self._nullable_string(body, "dashboard_origin", current)
        if kind in {"feishu", "dingtalk", "wecom"}:
            dashboard_origin = parse_strict_origin(dashboard_origin)

        issuer = self._string(body, "issuer", current, "")
        if kind in {"feishu", "dingtalk", "wecom"}:
            issuer = ""

        default_names = {
            "feishu": "Feishu",
            "dingtalk": "DingTalk",
            "wecom": "WeCom",
        }
        provider = self._services.sso_repo.upsert_by_kind(
            kind,
            enabled=bool(body.get("enabled", current.enabled if current else False)),
            display_name=self._string(
                body,
                "display_name",
                current,
                default_names.get(kind, "Octop SSO"),
            ),
            issuer=issuer,
            client_id=self._string(body, "client_id", current, ""),
            client_secret_enc=encrypted_secret,
            scopes=self._string(body, "scopes", current, adapter.default_scopes),
            dashboard_origin=dashboard_origin,
            extra=extra,
        )
        if provider.issuer.strip():
            self._discovery.invalidate(provider.issuer)
        if public_base is not None:
            return self.get_config_for_kind(kind, public_base=public_base)
        return self._provider_config(provider)

    def test_connection(self) -> dict[str, bool | str]:
        return self.test_connection_for_kind("oidc")

    def test_connection_for_kind(self, kind: str) -> dict[str, bool | str]:
        adapter = self._adapter(kind)
        provider = self._services.sso_repo.get_by_kind(kind)
        if provider is None or not adapter.is_configured(provider):
            raise ValueError("SSO is misconfigured")
        return adapter.test_connection(provider)

    def start_login(self, *, redirect_after: str | None, public_base: str) -> dict[str, str]:
        return self.start_login_for_kind(
            "oidc", redirect_after=redirect_after, public_base=public_base
        )

    def start_login_for_kind(
        self,
        kind: str,
        *,
        redirect_after: str | None,
        public_base: str,
        bind_user_id: int | None = None,
    ) -> dict[str, str]:
        self._services.sso_repo.delete_expired()
        adapter = self._adapter(kind)
        provider = self._enabled_provider_for_kind(kind)
        nonce = secrets.token_urlsafe(32)
        state = secrets.token_urlsafe(32)
        verifier, challenge = new_pkce_pair()
        redirect_uri = build_redirect_uri(public_base, adapter.callback_path)
        authorization_url = adapter.authorize_url(
            row=provider,
            state=state,
            nonce=nonce,
            code_challenge=challenge,
            redirect_uri=redirect_uri,
        )
        self._services.sso_repo.create_login_state(
            state=state,
            provider_id=provider.id,
            nonce=nonce,
            code_verifier=verifier,
            redirect_after=sanitize_redirect_after(redirect_after),
            expires_at=int(time.time()) + _LOGIN_STATE_TTL_SECONDS,
            user_id=bind_user_id,
        )
        return {
            "authorization_url": authorization_url,
            "state": state,
        }

    async def handle_callback(
        self,
        *,
        code: str | None,
        state: str | None,
        error: str | None,
        public_base: str,
    ) -> RedirectResult:
        self._services.sso_repo.delete_expired()
        frontend = self._frontend_base(public_base)
        if error is not None:
            return self._error_redirect(
                frontend, "denied" if error == "access_denied" else "generic"
            )
        if not state:
            return self._error_redirect(frontend, "state")

        login_state = self._services.sso_repo.take_login_state(state)
        if login_state is None:
            return self._error_redirect(frontend, "state")
        provider = self._services.sso_repo.get_by_id(login_state.provider_id)
        if provider is None or not provider.enabled:
            return self._error_redirect(frontend, "disabled")
        provider = provider_for_desktop(provider)
        try:
            adapter = self._adapter(provider.kind)
        except ValueError:
            return self._error_redirect(frontend, "misconfigured")
        if not adapter.is_configured(provider) or not code:
            return self._error_redirect(frontend, "misconfigured")

        frontend = self._frontend_base(public_base, provider=provider)
        redirect_uri = build_redirect_uri(public_base, adapter.callback_path)

        try:
            subject, claims = await asyncio.get_running_loop().run_in_executor(
                None,
                partial(
                    adapter.complete_login,
                    code,
                    row=provider,
                    login_state=login_state,
                    redirect_uri=redirect_uri,
                ),
            )
        except jwt.InvalidTokenError:
            logger.exception("SSO callback returned an invalid token (kind=%s)", provider.kind)
            return self._error_redirect(frontend, "invalid_token")
        except (httpx.HTTPError, ValueError):
            logger.exception("SSO callback exchange failed (kind=%s)", provider.kind)
            return self._error_redirect(frontend, "exchange")

        if not subject:
            return self._error_redirect(frontend, "invalid_token")
        try:
            if login_state.user_id is not None:
                user = await self._user_manager.bind_sso_identity(
                    user_id=login_state.user_id,
                    provider_id=provider.id,
                    subject=subject,
                    claims=claims,
                )
            else:
                user = await self._user_manager.resolve_or_create_sso_user(
                    provider_id=provider.id, subject=subject, claims=claims
                )
        except OctopError as exc:
            if exc.code is ErrorCode.USER_DISABLED:
                return self._error_redirect(frontend, "disabled")
            if exc.code is ErrorCode.SSO_IDENTITY_TAKEN:
                return self._error_redirect(frontend, "identity_taken")
            return self._error_redirect(frontend, "generic")
        except Exception:
            return self._error_redirect(frontend, "generic")

        self._store_console_handoff(user.id, claims)

        login_code = secrets.token_urlsafe(32)
        self._services.sso_repo.attach_login_code(
            state,
            login_code=login_code,
            user_id=user.id,
            expires_at=int(time.time()) + _LOGIN_CODE_TTL_SECONDS,
        )
        params = {"code": login_code, "redirect": login_state.redirect_after}
        if login_state.user_id is not None:
            params["bind"] = "1"
        return RedirectResult(f"{_with_agent_prefix(frontend)}/login/oidc/complete#{urlencode(params)}")

    async def exchange_login_code(self, code: str) -> User:
        consumed = self._services.sso_repo.consume_login_code(code)
        if consumed is None:
            raise ValueError("invalid or expired SSO login code")
        user = self._user_manager.get_by_id(consumed["user_id"])
        if user is None:
            raise ValueError("SSO user is unavailable")
        self._services.audit_repo.write(
            actor=user.username, action="auth.oidc_login", target=user.username
        )
        return user

    def _adapter(self, kind: str) -> IdentityProvider:
        adapter = self._adapters.get(kind)
        if adapter is None:
            raise ValueError("unknown SSO provider kind")
        return adapter

    def _enabled_provider(self) -> SsoProviderRow:
        return self._enabled_provider_for_kind("oidc")

    def _enabled_provider_for_kind(self, kind: str) -> SsoProviderRow:
        adapter = self._adapter(kind)
        provider = self._services.sso_repo.get_by_kind(kind)
        if provider is None or not provider.enabled or self._user_manager.count() == 0:
            raise ValueError("SSO is disabled")
        if not adapter.is_configured(provider):
            raise ValueError("SSO is misconfigured")
        return provider_for_desktop(provider)

    def _configured_provider(self) -> SsoProviderRow:
        provider = self._services.sso_repo.get_provider()
        adapter = self._adapters["oidc"]
        if provider is None or not adapter.is_configured(provider):
            raise ValueError("SSO is misconfigured")
        return provider

    def _frontend_base(self, public_base: str, *, provider: SsoProviderRow | None = None) -> str:
        if provider is not None and provider.dashboard_origin:
            return provider.dashboard_origin.rstrip("/")
        oidc = self._services.sso_repo.get_by_kind("oidc")
        if oidc is not None and oidc.dashboard_origin:
            return oidc.dashboard_origin.rstrip("/")
        for row in self._services.sso_repo.list():
            if row.enabled and row.dashboard_origin:
                return row.dashboard_origin.rstrip("/")
        return public_base.rstrip("/")

    def login_error_frontend(self, public_base: str) -> str:
        """Resolve where OIDC error redirects should land (dashboard_origin when set)."""
        return self._frontend_base(public_base)

    @staticmethod
    def _http_client() -> httpx.Client:
        return httpx.Client(timeout=_HTTP_TIMEOUT)

    @staticmethod
    def _endpoint(discovery: Mapping[str, object], name: str) -> str:
        endpoint = discovery.get(name)
        if not isinstance(endpoint, str) or not endpoint:
            raise ValueError(f"OIDC discovery has no {name}")
        return endpoint

    def _token_request_data(
        self,
        provider: SsoProviderRow,
        code: str,
        verifier: str,
        public_base: str,
        redirect_uri: str | None = None,
    ) -> dict[str, str]:
        data = {
            "grant_type": "authorization_code",
            "code": code,
            "redirect_uri": redirect_uri or build_redirect_uri(public_base),
            "client_id": provider.client_id,
            "code_verifier": verifier,
        }
        if provider.client_secret_enc is not None:
            data["client_secret"] = decrypt_secret(
                self._services.secret_repo, provider.client_secret_enc
            )
        return data

    def _exchange_claims(
        self,
        provider: SsoProviderRow,
        code: str,
        code_verifier: str,
        nonce: str,
        public_base: str | None = None,
        redirect_uri: str | None = None,
    ) -> dict[str, Any]:
        uri = redirect_uri or build_redirect_uri(public_base or "")
        with self._http_client() as client:
            discovery = self._discovery.get(provider.issuer, httpx_client=client)
            token_endpoint = self._endpoint(discovery, "token_endpoint")
            jwks_uri = self._endpoint(discovery, "jwks_uri")
            token_response = client.post(
                token_endpoint,
                data=self._token_request_data(
                    provider, code, code_verifier, public_base or "", redirect_uri=uri
                ),
            )
            token_response.raise_for_status()
            token = token_response.json()
            id_token = token.get("id_token") if isinstance(token, dict) else None
            if not isinstance(id_token, str):
                raise jwt.InvalidTokenError("token response has no ID token")
            claims = verify_id_token(
                id_token,
                jwks_uri=jwks_uri,
                issuer=self._endpoint(discovery, "issuer"),
                client_id=provider.client_id,
                nonce=nonce,
                httpx=client,
            )
            handoff = token.get("console_handoff_token") if isinstance(token, dict) else None
            if isinstance(handoff, str) and handoff.strip():
                claims[_CONSOLE_HANDOFF_CLAIM] = handoff.strip()
            return claims

    def _store_console_handoff(self, user_id: int, claims: Mapping[str, Any]) -> None:
        token = claims.get(_CONSOLE_HANDOFF_CLAIM)
        if not isinstance(token, str) or not token:
            return
        self._services.settings_repo.set(_CONSOLE_HANDOFF_SETTING.format(user_id=user_id), token)

    def console_handoff_url(self, user_id: int) -> str:
        """Mint a Chrome login URL for the current Octop user. Client identity wins."""
        origin = desktop_platform_origin()
        if not origin:
            raise ValueError("platform origin is not configured")
        token = self._services.settings_repo.get(_CONSOLE_HANDOFF_SETTING.format(user_id=user_id))
        if not token:
            raise ValueError("console handoff token is missing")
        try:
            with self._http_client() as client:
                response = client.post(
                    f"{origin}/console/api/auth/browser-sso-ticket",
                    headers={"Authorization": f"Bearer {token}"},
                )
                response.raise_for_status()
                payload = response.json()
        except (httpx.HTTPError, ValueError) as exc:
            raise ValueError("console handoff request failed") from exc
        ticket = payload.get("ticket") if isinstance(payload, dict) else None
        if not isinstance(ticket, str) or not ticket.strip():
            raise ValueError("console handoff ticket is missing")
        next_path = payload.get("next") if isinstance(payload, dict) else None
        if next_path not in _CONSOLE_NEXT_ALLOWED:
            next_path = "/console/"
        if next_path == "/console":
            next_path = "/console/"
        return (
            f"{origin}/login?sso={quote(ticket.strip(), safe='')}"
            f"&next={quote(next_path, safe='')}"
        )

    async def login_with_platform_password(
        self,
        phone: str,
        password: str,
        *,
        public_base: str,
    ) -> str:
        """Sign in against 中台 and finish OIDC without leaving the desktop WebView.

        MAMMOTH-PATCH: B类-SSO中台胶水. Does not write a product password into Octop.
        """
        origin = desktop_platform_origin()
        if not origin:
            raise ValueError("unreachable")
        cleaned = phone.strip()
        if not cleaned or not password:
            raise ValueError("bad_credentials")
        provider = self._desktop_oidc_provider()
        code, state = await asyncio.get_running_loop().run_in_executor(
            None,
            partial(
                self._authorize_with_platform_password,
                origin,
                cleaned,
                password,
                provider,
                public_base,
            ),
        )
        result = await self.handle_callback(
            code=code,
            state=state,
            error=None,
            public_base=public_base,
        )
        return _login_code_from_redirect(result.url)

    async def register_with_platform(
        self,
        name: str,
        phone: str,
        password: str,
        enterprise_name: str,
        *,
        public_base: str,
    ) -> str:
        """Create the 中台 account, then finish OIDC so the Octop shadow user exists."""
        origin = desktop_platform_origin()
        if not origin:
            raise ValueError("unreachable")
        cleaned_phone = phone.strip()
        cleaned_name = name.strip()
        cleaned_enterprise = enterprise_name.strip()
        if not cleaned_phone or not password or not cleaned_name or not cleaned_enterprise:
            raise ValueError("bad_credentials")
        provider = self._desktop_oidc_provider()
        code, state = await asyncio.get_running_loop().run_in_executor(
            None,
            partial(
                self._authorize_with_platform_register,
                origin,
                cleaned_name,
                cleaned_phone,
                password,
                cleaned_enterprise,
                provider,
                public_base,
            ),
        )
        result = await self.handle_callback(
            code=code,
            state=state,
            error=None,
            public_base=public_base,
        )
        return _login_code_from_redirect(result.url)

    def _desktop_oidc_provider(self) -> SsoProviderRow:
        issuer = desktop_oidc_issuer()
        if not issuer:
            raise ValueError("unreachable")
        current = self._services.sso_repo.get_by_kind("oidc")
        if current is not None and current.client_id.strip():
            return provider_for_desktop(current)
        # MAMMOTH-PATCH: 全新客户端没有本地 OIDC 行，用中台固定 client_id 补一行。
        return provider_for_desktop(
            self._services.sso_repo.upsert_provider(
                enabled=True,
                display_name="猛犸",
                issuer=issuer,
                client_id=_DESKTOP_OIDC_CLIENT_ID,
                client_secret_enc=None,
                scopes="openid profile email",
                dashboard_origin=None,
            )
        )

    def _begin_oidc_login(
        self,
        provider: SsoProviderRow,
        *,
        redirect_after: str,
        public_base: str,
    ) -> dict[str, str]:
        """Store PKCE state without the 'no users yet' gate used by browser SSO."""
        self._services.sso_repo.delete_expired()
        adapter = self._adapter(provider.kind)
        nonce = secrets.token_urlsafe(32)
        state = secrets.token_urlsafe(32)
        verifier, challenge = new_pkce_pair()
        redirect_uri = build_redirect_uri(public_base, adapter.callback_path)
        authorization_url = adapter.authorize_url(
            row=provider,
            state=state,
            nonce=nonce,
            code_challenge=challenge,
            redirect_uri=redirect_uri,
        )
        self._services.sso_repo.create_login_state(
            state=state,
            provider_id=provider.id,
            nonce=nonce,
            code_verifier=verifier,
            redirect_after=sanitize_redirect_after(redirect_after),
            expires_at=int(time.time()) + _LOGIN_STATE_TTL_SECONDS,
        )
        return {"authorization_url": authorization_url, "state": state}

    def _authorize_with_platform_password(
        self,
        origin: str,
        phone: str,
        password: str,
        provider: SsoProviderRow,
        public_base: str,
    ) -> tuple[str, str]:
        try:
            with self._http_client() as client:
                login = client.post(
                    f"{origin}{_PLATFORM_LOGIN_PATH}",
                    json={"phone": phone, "password": password},
                )
                if login.status_code in {400, 401, 403}:
                    raise ValueError("bad_credentials")
                if login.status_code >= 500:
                    raise ValueError("unreachable")
                if login.status_code != 200 or not client.cookies:
                    raise ValueError("exchange")
                return self._authorize_session(client, provider, public_base)
        except httpx.HTTPError as exc:
            raise ValueError("unreachable") from exc

    def _authorize_with_platform_register(
        self,
        origin: str,
        name: str,
        phone: str,
        password: str,
        enterprise_name: str,
        provider: SsoProviderRow,
        public_base: str,
    ) -> tuple[str, str]:
        try:
            with self._http_client() as client:
                created = client.post(
                    f"{origin}{_PLATFORM_REGISTER_PATH}",
                    json={
                        "name": name,
                        "phone": phone,
                        "password": password,
                        "enterpriseName": enterprise_name,
                    },
                )
                if created.status_code == 409:
                    raise ValueError("phone_taken")
                if created.status_code in {400, 401, 403}:
                    raise ValueError("bad_credentials")
                if created.status_code >= 500:
                    raise ValueError("unreachable")
                if created.status_code != 200 or not client.cookies:
                    raise ValueError("exchange")
                return self._authorize_session(client, provider, public_base)
        except httpx.HTTPError as exc:
            raise ValueError("unreachable") from exc

    def _authorize_session(
        self,
        client: httpx.Client,
        provider: SsoProviderRow,
        public_base: str,
    ) -> tuple[str, str]:
        started = self._begin_oidc_login(
            provider,
            redirect_after="/chat",
            public_base=public_base,
        )
        authorize = client.get(started["authorization_url"], follow_redirects=False)
        return _code_state_from_authorize(authorize)

    @staticmethod
    def _string(
        body: Mapping[str, object],
        name: str,
        current: SsoProviderRow | None,
        default: str,
    ) -> str:
        value = body.get(name)
        if isinstance(value, str):
            return value.strip()
        return str(getattr(current, name)) if current is not None else default

    @staticmethod
    def _nullable_string(
        body: Mapping[str, object], name: str, current: SsoProviderRow | None
    ) -> str | None:
        if name in body:
            value = body[name]
            return value.strip() if isinstance(value, str) and value.strip() else None
        return getattr(current, name) if current is not None else None

    @staticmethod
    def _provider_config(provider: SsoProviderRow) -> dict[str, Any]:
        return {
            "enabled": bool(provider.enabled),
            "display_name": provider.display_name,
            "issuer": provider.issuer,
            "client_id": provider.client_id,
            "scopes": provider.scopes,
            "dashboard_origin": provider.dashboard_origin,
            "has_client_secret": provider.client_secret_enc is not None,
        }

    @staticmethod
    def _error_redirect(frontend_base: str, oidc_error: str) -> RedirectResult:
        return RedirectResult(
            f"{_with_agent_prefix(frontend_base)}/login?{urlencode({'oidc_error': oidc_error})}"
        )
