import * as DocumentPicker from "expo-document-picker";
import { buildAuthorizationHeader } from "./auth";
import { getServerById } from "./storage";

export type UploadedAttachment = {
  name: string;
  path: string;
};

export async function buildUploadHeaders(
  daemonId: string,
): Promise<Record<string, string>> {
  return {
    Authorization: await buildAuthorizationHeader({
      daemonId,
      purpose: "zen-upload",
    }),
  };
}

export function buildUploadUrl(serverUrl: string): string | null {
  if (!serverUrl) {
    return null;
  }

  try {
    const url = new URL(serverUrl);
    if (url.protocol === "ws:") {
      url.protocol = "http:";
    } else if (url.protocol === "wss:") {
      url.protocol = "https:";
    }
    url.pathname = "/upload";
    url.search = "";
    url.hash = "";
    return url.toString();
  } catch {
    return null;
  }
}

export async function uploadDocumentForServer(
  serverId: string,
): Promise<UploadedAttachment | null> {
  const result = await DocumentPicker.getDocumentAsync({
    type: ["*/*"],
    copyToCacheDirectory: true,
  });
  if (result.canceled || !result.assets?.length) {
    return null;
  }

  const server = await getServerById(serverId);
  if (!server) {
    throw new Error("Server not found.");
  }

  const uploadUrl = buildUploadUrl(server.url);
  if (!uploadUrl) {
    throw new Error("Server URL is not configured.");
  }

  const asset = result.assets[0];
  const formData = new FormData();
  formData.append("file", {
    uri: asset.uri,
    name: asset.name || "upload",
    type: asset.mimeType || "application/octet-stream",
  } as any);

  const response = await fetch(uploadUrl, {
    method: "POST",
    headers: await buildUploadHeaders(server.daemonId),
    body: formData,
  });
  if (!response.ok) {
    throw new Error(`Upload failed (${response.status})`);
  }

  const payload = (await response.json()) as {
    path?: string;
    name?: string;
  };
  if (!payload.path) {
    throw new Error("Upload response missing file path.");
  }

  return {
    name: payload.name || asset.name || "upload",
    path: payload.path,
  };
}
