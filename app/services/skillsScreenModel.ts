import type {
  InstalledSkill,
  ManagedSkillAgent,
  PackageFile,
  SkillsInventory,
} from "./skillsManagement";

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

export function filterInstalledSkills(
  skills: readonly InstalledSkill[],
  query: string,
  status: SkillStatusFilter,
  scope: SkillScopeFilter,
): InstalledSkill[] {
  const needle = query.trim().toLocaleLowerCase();
  return skills
    .filter((skill) => {
      if (status === "enabled" && !skill.enabled) return false;
      if (status === "disabled" && skill.enabled) return false;
      if (scope !== "all" && skill.scope !== scope && skill.scope !== "mixed")
        return false;
      if (!needle) return true;
      return [
        skill.name,
        skill.description,
        skill.source,
        skill.provenance,
      ].some((value) => value?.toLocaleLowerCase().includes(needle));
    })
    .sort((a, b) => a.name.localeCompare(b.name));
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
