import type {
  InstalledSkill,
  ManagedSkillAgent,
  PackageFile,
  SkillsInventory,
} from "./skillsManagement";
import { skillAgentLabel } from "./skillsManagement";

export const MANAGED_SKILL_AGENTS: readonly ManagedSkillAgent[] = [
  "codex",
  "claude-code",
  "cursor",
  "grok",
  "opencode",
  "pi",
];

export type SkillsAgentCounts = Record<ManagedSkillAgent, number>;
export type SkillStatusFilter = "all" | "enabled" | "disabled";
export type SkillScopeFilter = "all" | "global" | "project";

export interface SkillFilters {
  agents: ManagedSkillAgent[];
  status: SkillStatusFilter;
  scope: SkillScopeFilter;
}

export interface LogicalSkill {
  key: string;
  name: string;
  description?: string;
  copies: InstalledSkill[];
  primaryCopy: InstalledSkill;
  agents: ManagedSkillAgent[];
  enabled: boolean;
}

export interface SkillTreeNode {
  name: string;
  path: string;
  kind: "directory" | "file";
  file?: PackageFile;
  children: SkillTreeNode[];
}

interface SkillsProjectCandidate {
  serverId: string;
  cwd?: string;
  updated_at?: number;
}

/**
 * Keep the Skills query context stable while Agent activity streams in.
 * `updated_at` is runtime activity, so using its latest value as a live query
 * key makes the selected cwd oscillate between active Sessions. We only use it
 * to choose an initial/fallback cwd; an existing cwd remains selected while it
 * is still represented on the current server.
 */
export function selectStableSkillsProjectCwd(
  agents: readonly SkillsProjectCandidate[],
  serverId: string | null | undefined,
  previousCwd: string,
): string {
  if (!serverId) return "";
  const candidates = agents.filter(
    (agent) => agent.serverId === serverId && agent.cwd?.trim(),
  );
  const retained = previousCwd.trim();
  if (
    retained &&
    candidates.some((agent) => agent.cwd?.trim() === retained)
  ) {
    return retained;
  }
  return (
    [...candidates].sort(
      (a, b) =>
        (b.updated_at || 0) - (a.updated_at || 0) ||
        (a.cwd?.trim() || "").localeCompare(b.cwd?.trim() || ""),
    )[0]?.cwd?.trim() || ""
  );
}

export function skillsSectionAgentCounts(
  inventory?: SkillsInventory,
): SkillsAgentCounts {
  const counts: SkillsAgentCounts = {
    codex: 0,
    "claude-code": 0,
    cursor: 0,
    grok: 0,
    opencode: 0,
    pi: 0,
  };
  for (const skill of inventory?.skills ?? []) {
    for (const agent of skill.agents) counts[agent] += 1;
  }
  return counts;
}

export function skillsSectionProjection(
  inventory: SkillsInventory | undefined,
  agent: ManagedSkillAgent,
): { agent: ManagedSkillAgent; count: number; skills: InstalledSkill[] } {
  const skills = (inventory?.skills ?? []).filter(
    (skill) => skill.agents.includes(agent),
  );
  return { agent, count: skills.length, skills };
}

const agentOrder = new Map(
  MANAGED_SKILL_AGENTS.map((agent, index) => [agent, index]),
);

/**
 * Inventory entries are physical copies. The product surface is logical:
 * one name with every daemon-reported copy retained. This projection never
 * chooses a runtime winner; providers have different duplicate-name rules.
 */
export function groupLogicalSkills(
  skills: readonly InstalledSkill[],
): LogicalSkill[] {
  const groups = new Map<string, InstalledSkill[]>();
  for (const skill of skills) {
    const key = skill.name.toLocaleLowerCase();
    groups.set(key, [...(groups.get(key) ?? []), skill]);
  }
  return [...groups.entries()]
    .map(([key, unsortedCopies]) => {
      const copies = [...unsortedCopies].sort(
        (a, b) =>
          a.rootPath.localeCompare(b.rootPath) || a.id.localeCompare(b.id),
      );
      const primaryCopy = copies[0]!;
      const agents = MANAGED_SKILL_AGENTS.filter((agent) =>
        copies.some((copy) => copy.agents.includes(agent)),
      );
      agents.sort(
        (a, b) => (agentOrder.get(a) ?? 99) - (agentOrder.get(b) ?? 99),
      );
      return {
        key,
        name: primaryCopy.name,
        description:
          primaryCopy.description ||
          copies.find((copy) => copy.description)?.description,
        copies,
        primaryCopy,
        agents,
        enabled: copies.some((copy) => copy.enabled),
      };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function filterLogicalSkills(
  skills: readonly LogicalSkill[],
  query: string,
  filters: SkillFilters,
): LogicalSkill[] {
  const needle = query.trim().toLocaleLowerCase();
  return skills
    .filter((skill) => {
      if (filters.status === "enabled" && !skill.enabled) return false;
      if (filters.status === "disabled" && skill.enabled) return false;
      if (
        filters.scope !== "all" &&
        !skill.copies.some((copy) => copy.scope === filters.scope)
      )
        return false;
      if (
        filters.agents.length > 0 &&
        !filters.agents.some((agent) => skill.agents.includes(agent))
      )
        return false;
      if (!needle) return true;
      return [
        skill.name,
        skill.description,
        ...skill.copies.flatMap((copy) => [copy.location, copy.rootPath]),
      ].some((value) => value?.toLocaleLowerCase().includes(needle));
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

/** Kept for non-screen callers while the screen consumes logical rows. */
export function filterInstalledSkills(
  skills: readonly InstalledSkill[],
  query: string,
  status: SkillStatusFilter,
  scope: SkillScopeFilter,
): InstalledSkill[] {
  return skills.filter((skill) => {
    if (status === "enabled" && !skill.enabled) return false;
    if (status === "disabled" && skill.enabled) return false;
    if (scope !== "all" && skill.scope !== scope)
      return false;
    const needle = query.trim().toLocaleLowerCase();
    return (
      !needle ||
      [skill.name, skill.description, skill.location, skill.rootPath].some(
        (value) => value?.toLocaleLowerCase().includes(needle),
      )
    );
  });
}

export function skillCopyLocation(copy: InstalledSkill): {
  label: string;
  path: string;
} {
  const agents = copy.agents.map(skillAgentLabel).join(", ") || "Local copy";
  const parts = copy.rootPath.replace(/\\/g, "/").split("/").filter(Boolean);
  const path =
    parts.length > 3 ? `.../${parts.slice(-3).join("/")}` : parts.join("/");
  return { label: `${copy.location} · ${agents}`, path };
}

export function buildSkillFileTree(
  files: readonly PackageFile[],
): SkillTreeNode[] {
  const root: SkillTreeNode[] = [];
  for (const file of [...files].sort((a, b) => a.path.localeCompare(b.path))) {
    const parts = file.path.split("/").filter(Boolean);
    let level = root;
    let path = "";
    parts.forEach((part, index) => {
      path = path ? `${path}/${part}` : part;
      const isFile = index === parts.length - 1;
      let node = level.find((candidate) => candidate.name === part);
      if (!node) {
        node = {
          name: part,
          path,
          kind: isFile ? "file" : "directory",
          file: isFile ? file : undefined,
          children: [],
        };
        level.push(node);
      }
      level = node.children;
    });
  }
  const sort = (nodes: SkillTreeNode[]) => {
    nodes.sort((a, b) =>
      a.kind === b.kind
        ? a.name.localeCompare(b.name)
        : a.kind === "directory"
          ? -1
          : 1,
    );
    nodes.forEach((node) => sort(node.children));
  };
  sort(root);
  return root;
}

export function defaultSkillFile(
  files: readonly PackageFile[],
): string | undefined {
  return (
    files.find((file) => file.path === "SKILL.md")?.path ??
    files.find((file) => file.previewStatus !== "binary")?.path
  );
}

export function skillRenderer(
  kind: PackageFile["kind"],
  content?: string,
): "markdown" | "json" | "text" | "binary" | "invalid-json" {
  if (kind === "binary") return "binary";
  if (kind === "markdown") return "markdown";
  if (kind === "json") {
    try {
      JSON.parse(content ?? "");
      return "json";
    } catch {
      return "invalid-json";
    }
  }
  return "text";
}

export class SkillsAutomaticInventoryOwner {
  private identity = "";
  shouldRefresh(
    focusGeneration: number,
    serverId: string | null | undefined,
  ): boolean {
    if (focusGeneration < 1) return false;
    const identity = `${focusGeneration}:${serverId?.trim() || "none"}`;
    if (identity === this.identity) return false;
    this.identity = identity;
    return true;
  }
}
