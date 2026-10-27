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
      return { outboundText: text, blocked: quota.message || "本月对话次数已用完" };
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

async function consoleJson<T>(path: string, init: RequestInit = {}): Promise<T | null> {
  try {
    const headers = new Headers(init.headers);
    if (init.body && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
    const { ensurePlatformOrigin, platformUrl } = await import("../../utils/mammothSso");
    const origin = await ensurePlatformOrigin();
    const res = await fetch(platformUrl(`/console/api${path}`, origin), {
      ...init,
      headers,
      credentials: "include",
    });
    if (res.status === 401) return null;
    const data = (await res.json().catch(() => null)) as T & { message?: string | string[] };
    if (!res.ok) {
      const message = Array.isArray(data?.message) ? data.message.join("；") : data?.message;
      throw new Error(message || "请求失败");
    }
    return data;
  } catch (error) {
    if (error instanceof Error && error.message && error.message !== "Failed to fetch") {
      throw error;
    }
    return null;
  }
}
