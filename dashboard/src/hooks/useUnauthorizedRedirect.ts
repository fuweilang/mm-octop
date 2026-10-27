import { useEffect } from "react";
import { UNAUTHORIZED_EVENT } from "../api/request";
import {
  ensurePlatformOrigin,
  localLoginUrl,
  shouldUseLocalLogin,
  startMammothSession,
} from "../utils/mammothSso";

export function useUnauthorizedRedirect(): void {
  useEffect(() => {
    const handler = (event: Event) => {
      event.preventDefault();
      void (async () => {
        const origin = await ensurePlatformOrigin();
        if (shouldUseLocalLogin(origin, window.location.origin)) {
          window.location.assign(localLoginUrl());
          return;
        }
        await startMammothSession();
      })();
    };
    window.addEventListener(UNAUTHORIZED_EVENT, handler);
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, handler);
  }, []);
}
