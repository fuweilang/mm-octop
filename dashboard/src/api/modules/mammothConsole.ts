type QuotaSnapshot = {
  exceeded?: boolean;
  message?: string;
};

type SearchHit = {
  documentName: string;
  snippet: string;
};

type SearchResult = {
  hits?: SearchHit[];
};

export async function prepareMammothChatTurn(text: string): Promise<{
  outboundText: string;
  blocked?: string;
}> {
  try {
    const quota = await consoleJson<QuotaSnapshot>("/usage/quota");
    if (quota?.exceeded) {
      return {
        outboundText: text,
        blocked: quota.message || "本月对话次数已用完",
      };
    }
  } catch (error) {
    if (error instanceof Error && error.message.includes("用完")) {
      return { outboundText: text, blocked: error.message };
    }
  }

  let outboundText = text;
  try {
    const search = await consoleJson<SearchResult>(
      `/kb/search?q=${encodeURIComponent(text.slice(0, 200))}`,
    );
    const hits = search?.hits ?? [];
    if (hits.length > 0) {
      const lines = hits.slice(0, 6).map((hit, index) => {
        return `[${index + 1}] 《${hit.documentName}》\n${hit.snippet}`;
      });
      outboundText =
        "以下是企业知识库检索结果，只可根据这些材料回答；没有依据就说知识库没有。不得编造法条、案例、客户或结论。\n\n" +
        `${lines.join("\n\n")}\n\n用户问题：\n${text}`;
    }
  } catch {
    outboundText = text;
  }

  try {
    await consoleJson("/usage/events", {
      method: "POST",
      body: JSON.stringify({ kind: "chat_turn" }),
    });
  } catch (error) {
    if (error instanceof Error && error.message.includes("用完")) {
      return { outboundText: text, blocked: error.message };
    }
  }
  return { outboundText };
}

async function consoleJson<T>(
  path: string,
  init: RequestInit = {},
): Promise<T | null> {
  const { ensurePlatformOrigin, platformUrl } = await import(
    "../../utils/mammothSso"
  );
  const origin = await ensurePlatformOrigin();
  const url = platformUrl(`/console/api${path}`, origin);

  const send = async (token: string): Promise<Response> => {
    const headers = new Headers(init.headers);
    if (init.body && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
    headers.set("Authorization", `Bearer ${token}`);
    return fetch(url, { ...init, headers });
  };

  let res = await send(await fetchConsoleToken());
  if (res.status === 401) {
    // The handoff token has a lifetime; mint a fresh one once and retry.
    res = await send(await fetchConsoleToken(true));
    if (res.status === 401) {
      throw new ConsoleAuthError("中台登录已过期，请重新登录", 401);
    }
  }
  const data = (await res.json().catch(() => null)) as T & {
    message?: string | string[];
  };
  if (!res.ok) {
    const message = Array.isArray(data?.message)
      ? data.message.join("；")
      : data?.message;
    throw new Error(message || "请求失败");
  }
  return data;
}

/** 中台 refused our credentials; the caller decides the UX. */
export class ConsoleAuthError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ConsoleAuthError";
    this.status = status;
  }
}

let consoleToken: string | null = null;

/**
 * Bearer token for www.mmai.vip/console/api.
 *
 * The dashboard is served from 127.0.0.1, so the 中台 cookie (SameSite=Lax) is
 * never sent cross-site and every cookie-only call 401s. The local backend
 * hands out the SSO handoff token instead; only the desktop shell can mint it.
 */
async function fetchConsoleToken(force = false): Promise<string> {
  if (consoleToken && !force) return consoleToken;
  consoleToken = null;

  const { getAuthToken } = await import("../request");
  const { getApiUrl } = await import("../config");
  const local = getAuthToken();
  const headers: Record<string, string> = {};
  if (local) headers.Authorization = `Bearer ${local}`;

  let res: Response;
  try {
    res = await fetch(getApiUrl("/api/auth/mammoth/console-token"), {
      headers,
      cache: "no-store",
    });
  } catch {
    throw new ConsoleAuthError("拿不到中台凭证，请检查网络后重试", 0);
  }
  const data = res.ok
    ? ((await res.json().catch(() => null)) as { token?: string } | null)
    : null;
  if (!data?.token) {
    throw new ConsoleAuthError("中台登录已过期，请重新登录", res.status || 401);
  }
  consoleToken = data.token;
  return consoleToken;
}
