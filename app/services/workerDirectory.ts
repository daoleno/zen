import type { Worker } from "../store/workers";
import { compactPathLabel } from "./pathDisplay";

export interface WorkerDirectorySection {
  key: string;
  title: string;
  subtitle: string;
  data: Worker[];
}


export function groupWorkersByDirectory(
  agents: Worker[],
): WorkerDirectorySection[] {
  const sections: WorkerDirectorySection[] = [];
  const sectionByKey = new Map<string, WorkerDirectorySection>();

  for (const agent of agents) {
    const directory = normalizeDirectory(agent.cwd);
    const project = normalize(agent.project);
    const serverName = normalize(agent.serverName);

    let key = "";
    let title = "";
    let subtitle = "";

    if (directory) {
      key = `${agent.serverId}::cwd::${directory}`;
      title = basename(directory);
      const directoryLabel = compactPathLabel(directory);
      subtitle = directoryLabel;
    } else if (project) {
      key = `${agent.serverId}::project::${project}`;
      title = project;
      subtitle = "Project";
    } else {
      key = `${agent.serverId}::fallback`;
      title = serverName || "Unknown workspace";
      subtitle = "No directory";
    }

    const existing = sectionByKey.get(key);
    if (existing) {
      existing.data.push(agent);
      continue;
    }

    const nextSection: WorkerDirectorySection = {
      key,
      title,
      subtitle,
      data: [agent],
    };
    sectionByKey.set(key, nextSection);
    sections.push(nextSection);
  }

  return sections;
}

function normalize(value?: string): string {
  return value?.trim() || "";
}

function normalizeDirectory(value?: string): string {
  const trimmed = normalize(value);
  if (!trimmed || trimmed === "/") {
    return trimmed;
  }

  const normalized = trimmed.replace(/\/+$/, "");
  return normalized || "/";
}

function basename(value: string): string {
  if (!value || value === "/") {
    return value || "/";
  }

  const parts = value.split("/").filter(Boolean);
  return parts[parts.length - 1] || value;
}
