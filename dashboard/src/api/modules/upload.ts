import { requestUpload } from "../request";

export interface UploadResponse {
  path: string;
  workspace_path: string;
  filename: string;
  media_type: string;
  url: string;
  access_url: string;
}

/**
 * Upload a chat attachment into the agent workspace ``inbound/`` directory.
 *
 * ``relativePath`` (folder uploads / directory drops) keeps the tree:
 * ``site/src/a.ts`` lands at ``inbound/site/src/a.ts`` so the agent can address
 * files by path. Without it the file is flattened into ``inbound/``.
 */
export async function uploadFile(
  agentId: string,
  file: File,
  relativePath?: string,
): Promise<UploadResponse> {
  const formData = new FormData();
  formData.append("file", file);
  if (relativePath) formData.append("relative_path", relativePath);
  return requestUpload<UploadResponse>(`/agents/${agentId}/upload`, formData);
}

export const uploadImage = uploadFile;

export const uploadApi = { uploadFile, uploadImage };
