import { useEffect, useState } from "react";

import { versionApi } from "../api/modules/version";

/**
 * 产品版本号：只读 GET /api/version（镜像内 pyproject 版本）。
 *
 * 以前角标读 update status 的 current_version，那条链路会先看
 * MAMMOTH_APP_VERSION 环境变量，线上 .env 写死后就漂成旧版本。
 */
export function useAppVersion(): string | null {
  const [version, setVersion] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    versionApi
      .get()
      .then((info) => {
        if (alive) setVersion(info.version);
      })
      .catch(() => {
        // 版本号是装饰信息，取不到不打断页面。
      });
    return () => {
      alive = false;
    };
  }, []);

  return version;
}