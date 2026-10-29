import { request } from "../request";

export interface VersionInfo {
  product: string;
  version: string;
}

/** 版本号唯一接口：镜像内 pyproject 的版本，不受 .env 影响。 */
export const versionApi = {
  get: () => request<VersionInfo>("/version"),
};