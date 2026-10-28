import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Button, Result, Spin } from "antd";
import { useTranslation } from "react-i18next";
import { clearAuthToken, getAuthToken } from "../api/request";
import { authApi, type OctopUser } from "../api/modules/auth";
import { applyUserLocale } from "../utils/locale";
import { isNetworkFetchError } from "../utils/networkError";
import {
  collectBootDiagnostics,
  type BootDiagnostics,
} from "../utils/bootDiagnostics";
import {
  MammothLoginUnreachableError,
  PlatformOriginInitializingError,
  readPlatformOrigin,
  shouldUseLocalLogin,
  startMammothSession,
} from "../utils/mammothSso";
import { CurrentUserProvider } from "../hooks/useCurrentUser";
import BootOfflinePanel from "./BootOfflinePanel";

interface AuthGuardProps {
  children: React.ReactNode;
}

/**
 * Gate every protected route on (a) the initial admin existing and
 * (b) a valid JWT in localStorage. Octop always requires auth — there is
 * no "password protection disabled" mode like finnie had.
 *
 * Always wait for ``/api/setup/status`` (and ``/auth/me`` when a token
 * exists) before mounting children. Optimistic shell render with a stale
 * token would fire ``/api/agents`` / capabilities / update probes and get
 * 503'd by setup lockdown on first boot.
 *
 * When unauthenticated we must NOT render children: MainLayout / AgentProvider
 * would fire authenticated APIs, trip the 401 interceptor, and race the
 * navigate back to ``/login``.
 *
 * When the backend is unreachable (offline / Failed to fetch), show an
 * explicit offline panel with Retry instead of mounting a blank shell.
 * When health is up but oauth/status reports initializing (503 / ready:false),
 * show the starting-local panel — not a fake network failure.
 *
 * Auth is checked on mount and on explicit offline retry only. Do not put
 * ``navigate`` in the effect deps: under ``BrowserRouter``, React Router's
 * navigate identity changes with the location, which would re-run the gate
 * on every sidebar click and flash a full-page spinner.
 */
export default function AuthGuard({ children }: AuthGuardProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const navigateRef = useRef(navigate);
  navigateRef.current = navigate;

  const [checking, setChecking] = useState(true);
  const [authed, setAuthed] = useState(false);
  const [user, setUser] = useState<OctopUser | null>(null);
  const [offline, setOffline] = useState(false);
  const [diagnostics, setDiagnostics] = useState<BootDiagnostics | null>(null);
  const [loginOrigin, setLoginOrigin] = useState<string | null>(null);
  const [retryKey, setRetryKey] = useState(0);

  useEffect(() => {
    let cancelled = false;

    const showUnreachable = async (lastError: string) => {
      const diag = await collectBootDiagnostics(lastError);
      if (cancelled) return;
      setDiagnostics(diag);
      setOffline(true);
      setChecking(false);
      setAuthed(false);
    };

    const openLogin = async () => {
      try {
        const origin = await readPlatformOrigin();
        if (origin === null) {
          if (!cancelled) {
            await showUnreachable("oauth/status probe failed");
          }
          return "offline" as const;
        }
        if (shouldUseLocalLogin(origin, window.location.origin)) {
          if (!cancelled) navigateRef.current("/login", { replace: true });
          return "local" as const;
        }
        return "browser" as const;
      } catch (error) {
        if (cancelled) return "offline" as const;
        if (error instanceof PlatformOriginInitializingError) {
          await showUnreachable(error.message || "local backend initializing");
          return "offline" as const;
        }
        await showUnreachable(
          error instanceof Error ? error.message : "oauth/status failed",
        );
        return "offline" as const;
      }
    };

    const check = async () => {
      setOffline(false);
      setLoginOrigin(null);
      setDiagnostics(null);
      try {
        const status = await authApi.getAuthStatus();

        // No admin yet. Desktop must not open the local password wizard.
        if (status.setup_required) {
          clearAuthToken();
          const where = await openLogin();
          if (where === "browser" && !cancelled) {
            navigateRef.current("/setup", { replace: true });
          }
          return;
        }

        // Setup done. Need a token.
        const token = getAuthToken();
        if (!token) {
          if (!cancelled) {
            setAuthed(false);
            // Stay on the spinner until navigation away completes — do not
            // flip ``checking`` off or children would mount and 401→/login.
            const where = await openLogin();
            if (where !== "browser" || cancelled) return;
            try {
              await startMammothSession();
            } catch (error) {
              if (cancelled) return;
              if (error instanceof MammothLoginUnreachableError) {
                setLoginOrigin(error.origin);
                setChecking(false);
                return;
              }
              if (error instanceof PlatformOriginInitializingError) {
                await showUnreachable(error.message);
                return;
              }
              await showUnreachable(
                error instanceof Error ? error.message : "login start failed",
              );
            }
          }
          return;
        }

        // Validate the token by hitting /auth/me. On 401 the request.ts
        // interceptor already starts the SSO round-trip, so we just need to
        // swallow the throw here.
        try {
          const me = await authApi.me();
          await applyUserLocale(me.locale);
          if (!cancelled) {
            setUser(me);
            setAuthed(true);
            setChecking(false);
          }
        } catch (err) {
          if (cancelled) return;
          if (isNetworkFetchError(err)) {
            await showUnreachable(
              err instanceof Error ? err.message : "auth/me network error",
            );
            return;
          }
          setAuthed(false);
          const where = await openLogin();
          if (where !== "browser" || cancelled) return;
          try {
            await startMammothSession();
          } catch (error) {
            if (cancelled) return;
            if (error instanceof MammothLoginUnreachableError) {
              setLoginOrigin(error.origin);
              setChecking(false);
              return;
            }
            if (error instanceof PlatformOriginInitializingError) {
              await showUnreachable(error.message);
              return;
            }
            await showUnreachable(
              error instanceof Error ? error.message : "login start failed",
            );
          }
        }
      } catch (err) {
        if (cancelled) return;
        // Backend unreachable, still starting, or setup/status failed.
        const message =
          err instanceof Error ? err.message : "setup/status failed";
        await showUnreachable(message);
      }
    };

    void check();
    return () => {
      cancelled = true;
    };
  }, [retryKey]);

  // Auto-retry while local backend is initializing (first-boot race).
  useEffect(() => {
    if (!offline || diagnostics?.phase !== "initializing") return;
    const timer = window.setTimeout(() => {
      setChecking(true);
      setAuthed(false);
      setOffline(false);
      setRetryKey((k) => k + 1);
    }, 1500);
    return () => window.clearTimeout(timer);
  }, [offline, diagnostics?.phase, retryKey]);

  if (offline) {
    return (
      <BootOfflinePanel
        diagnostics={diagnostics}
        onRetry={() => {
          setChecking(true);
          setAuthed(false);
          setOffline(false);
          setDiagnostics(null);
          setRetryKey((k) => k + 1);
        }}
      />
    );
  }

  if (loginOrigin) {
    return (
      <div
        style={{
          height: "100dvh",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          background: "var(--fn-bg-layout)",
        }}
      >
        <Result
          status="warning"
          title={t("errors.loginUnreachableTitle")}
          subTitle={t("errors.loginUnreachableSubtitle", { origin: loginOrigin })}
          extra={
            <Button
              type="primary"
              onClick={() => {
                setLoginOrigin(null);
                setChecking(true);
                setRetryKey((k) => k + 1);
              }}
            >
              {t("errors.retry")}
            </Button>
          }
        />
      </div>
    );
  }

  if (checking || !authed) {
    return (
      <div
        style={{
          height: "100dvh",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          background: "var(--fn-bg-layout)",
        }}
      >
        <Spin size="large" />
      </div>
    );
  }

  return (
    <CurrentUserProvider user={user} setUser={setUser}>
      {children}
    </CurrentUserProvider>
  );
}
