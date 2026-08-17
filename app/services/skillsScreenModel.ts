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
  enabledByAgent: Partial<Record<ManagedSkillAgent, InstalledSkill[]>>;
  installedAgents: ManagedSkillAgent[];
  agents: ManagedSkillAgent[];
  enabled: boolean;
  hasConflict: boolean;
  enabledVariantCount: number;
}

export interface SkillTreeNode {
  name: string;
  path: string;
  kind: "directory" | "file";
  file?: PackageFile;
  children: SkillTreeNode[];
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
    if (skill.manager === "plugin") continue;
    for (const agent of skill.agents) counts[agent] += 1;
  }
  return counts;
}

export function skillsSectionProjection(
  inventory: SkillsInventory | undefined,
  agent: ManagedSkillAgent,
): { agent: ManagedSkillAgent; count: number; skills: InstalledSkill[] } {
  const skills = (inventory?.skills ?? []).filter(
    (skill) =>
      skill.manager !== "plugin" &&
      (skill.agents.includes(agent) ||
        (skill.owned && skill.bindings.length === 0)),
  );
  return { agent, count: skills.length, skills };
}

const agentOrder = new Map(
  MANAGED_SKILL_AGENTS.map((agent, index) => [agent, index]),
);

function copyEnabledForAgent(
  copy: InstalledSkill,
  agent: ManagedSkillAgent,
): boolean {
  if (copy.bindings.length > 0)
    return copy.bindings.some(
      (binding) => binding.agent === agent && binding.enabled,
    );
  return copy.enabled && copy.agents.includes(agent);
}

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
    if (skill.manager === "plugin") continue;
    const key = skill.name.toLocaleLowerCase();
    groups.set(key, [...(groups.get(key) ?? []), skill]);
  }
  return [...groups.entries()]
    .map(([key, unsortedCopies]) => {
      const copies = [...unsortedCopies].sort(
        (a, b) =>
          a.sourcePath.localeCompare(b.sourcePath) || a.id.localeCompare(b.id),
      );
      const enabledByAgent: LogicalSkill["enabledByAgent"] = {};
      for (const agent of MANAGED_SKILL_AGENTS) {
        const candidates = copies.filter((copy) =>
          copyEnabledForAgent(copy, agent),
        );
        if (candidates.length) enabledByAgent[agent] = candidates;
      }
      const enabledCopies = [
        ...new Set(
          Object.values(enabledByAgent).flatMap((items) => items ?? []),
        ),
      ];
      const primaryCopy = copies[0]!;
      const installedAgents = MANAGED_SKILL_AGENTS.filter((agent) =>
        copies.some((copy) => copy.agents.includes(agent)),
      );
      const agents = Object.keys(enabledByAgent).sort(
        (a, b) =>
          (agentOrder.get(a as ManagedSkillAgent) ?? 99) -
          (agentOrder.get(b as ManagedSkillAgent) ?? 99),
      ) as ManagedSkillAgent[];
      const contentVariants = new Set(
        enabledCopies
          .map((copy) => copy.contentHash)
          .filter((hash): hash is string => Boolean(hash)),
      );
      return {
        key,
        name: primaryCopy.name,
        description:
          primaryCopy.description ||
          copies.find((copy) => copy.description)?.description,
        copies,
        primaryCopy,
        enabledByAgent,
        installedAgents,
        agents,
        enabled: agents.length > 0,
        hasConflict: copies.some(
          (copy) =>
            copy.migration === "conflict" || copy.migration === "duplicate",
        ),
        enabledVariantCount: contentVariants.size,
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
        !skill.copies.some(
          (copy) => copy.scope === filters.scope || copy.scope === "mixed",
        )
      )
        return false;
      if (
        filters.agents.length > 0 &&
        !filters.agents.some((agent) => skill.installedAgents.includes(agent))
      )
        return false;
      if (!needle) return true;
      return [
        skill.name,
        skill.description,
        ...skill.copies.flatMap((copy) => [copy.source, copy.provenance]),
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
    if (scope !== "all" && skill.scope !== scope && skill.scope !== "mixed")
      return false;
    const needle = query.trim().toLocaleLowerCase();
    return (
      !needle ||
      [skill.name, skill.description, skill.source, skill.provenance].some(
        (value) => value?.toLocaleLowerCase().includes(needle),
      )
    );
  });
}

export function skillCopyLocation(copy: InstalledSkill): {
  label: string;
  path: string;
} {
  const agents = copy.agents.map(skillAgentLabel).join(", ") || "Unbound";
  const ownership =
    copy.manager === "zen"
      ? "Zen managed"
      : copy.manager === "builtin"
        ? "Built in"
        : "Agent location";
  const parts = copy.sourcePath.replace(/\\/g, "/").split("/").filter(Boolean);
  const path =
    parts.length > 3 ? `.../${parts.slice(-3).join("/")}` : parts.join("/");
  return { label: `${ownership} · ${agents} · ${copy.scope}`, path };
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
