import React, { useMemo, useState } from "react";
import { Alert } from "react-native";
import type {
  AvailablePlugin,
  InstalledPluginRow,
  PluginInventory,
} from "../../services/pluginsManagement";
import { pluginsUnifiedView } from "../../services/pluginsScreenModel";
import type {
  CatalogSkill,
  InstalledSkill,
  ManagedSkillAgent,
  PackageDetail,
  SkillsLeaderboards,
  SkillsRequestState,
} from "../../services/skillsManagement";
import { skillsAgentCounts } from "../../services/skillsScreenModel";
import type { SkillsSurfaceSection } from "../../services/skillsSurfaceModel";
import {
  SkillsPresentation,
  type SurfaceMutationNotice,
} from "./SkillsPresentation";

const mutationOperations = [
  "import",
  "migrate",
  "bind",
  "unbind",
  "enable",
  "disable",
  "uninstall",
  "forget",
  "adopt",
  "update",
] as const;

const installedSkills: InstalledSkill[] = [
  {
    id: "111111111111111111111111",
    name: "release-checklist",
    description: "Audits mobile release evidence and store readiness.",
    manager: "zen",
    owned: true,
    tracked: true,
    enabled: true,
    canonicalPath: "/fixture/.zen/skills/store/release-checklist",
    sourcePath: "/fixture/.zen/skills/store/release-checklist",
    scope: "mixed",
    agents: ["codex", "cursor"],
    bindings: [
      {
        agent: "codex",
        scope: "global",
        mode: "symlink",
        targetPath: "/fixture/.codex/skills/release-checklist",
        sourcePath: "/fixture/.zen/skills/store/release-checklist",
        enabled: true,
        boundAt: "2026-08-17T04:00:00Z",
        operations: ["unbind", "disable"],
      },
      {
        agent: "cursor",
        scope: "project",
        mode: "copy",
        targetPath: "/fixture/project/.cursor/skills/release-checklist",
        sourcePath: "/fixture/.zen/skills/store/release-checklist",
        enabled: false,
        boundAt: "2026-08-17T04:00:00Z",
        driftHash: "a47d98b4",
        operations: ["unbind", "enable"],
      },
    ],
    provenance: "Zen canonical store",
    source: "zen-fixtures/mobile-skills",
    sourceType: "catalog",
    ref: "9f47b3a",
    contentHash: "73e0d9a973e0d9a973e0d9a973e0d9a9",
    updatedAt: "2026-08-17T04:10:00Z",
    warnings: ["Cursor's project copy drifted from the canonical package."],
    capability: {
      canManage: true,
      operations: ["bind", "uninstall", "update"],
    },
  },
  {
    id: "222222222222222222222222",
    name: "external-notes",
    description: "An installation discovered in Claude Code.",
    manager: "external",
    owned: false,
    tracked: true,
    enabled: true,
    canonicalPath: "/fixture/.claude/skills/external-notes",
    sourcePath: "/fixture/.claude/skills/external-notes",
    scope: "global",
    agents: ["claude-code"],
    bindings: [],
    provenance: "Tracked external installation",
    source: "/fixture/.claude/skills/external-notes",
    sourceType: "external",
    contentHash: "4b23c134b23c134b23c134b23c134b23",
    capability: { canManage: true, operations: ["adopt", "forget"] },
  },
];

const catalogSkills: CatalogSkill[] = [
  {
    id: "mobile-labs/skills/accessibility-audit",
    skillId: "accessibility-audit",
    name: "Accessibility Audit",
    installs: 18420,
    source: "mobile-labs/skills",
    installable: true,
  },
  {
    id: "community/tools/legacy-entry",
    skillId: "legacy-entry",
    name: "Legacy Entry",
    installs: 326,
    source: "community/tools",
    installable: false,
  },
];

const availablePlugin: AvailablePlugin = {
  pluginId: "mobile-workflows@official-market",
  name: "mobile-workflows",
  marketplaceName: "official-market",
  description: "Release and simulator workflows for mobile teams.",
  sourceUrl: "https://example.invalid/mobile-workflows",
  sourceRef: "v2.4.0",
  installable: true,
};

const installedPlugin: InstalledPluginRow = {
  id: "review-tools@official-market",
  name: "review-tools",
  marketplace: "official-market",
  version: "1.8.2",
  scope: "user",
  enabled: true,
  host: "claude",
  mutable: true,
  source: "catalog",
  skillCount: 1,
  skills: [
    {
      name: "review-summary",
      canonicalPath: "/fixture/plugins/review-tools/review-summary",
      sourcePath: "/fixture/plugins/review-tools/review-summary",
    },
  ],
};

const pluginInventory: PluginInventory = {
  generatedAt: "2026-08-17T04:15:00Z",
  catalog: {
    status: "ready",
    available: [availablePlugin],
    installed: [
      {
        id: installedPlugin.id,
        version: installedPlugin.version,
        enabled: true,
      },
    ],
  },
  installed: [installedPlugin],
  warnings: [],
};

const emptyLeaderboards: SkillsLeaderboards = {
  allTime: { view: "all-time", totalSkills: 0, skills: [] },
  trending: { view: "trending", totalSkills: 0, skills: [] },
  hot: { view: "hot", totalSkills: 0, skills: [] },
};

function ready<T>(data: T): SkillsRequestState<T> {
  return { status: "ready", generation: 1, data };
}

function detailFor(name: string, path?: string): PackageDetail {
  const skill = installedSkills.find((candidate) => candidate.name === name)!;
  return {
    skillName: skill.name,
    description: skill.description,
    manager: skill.manager,
    owned: skill.owned,
    tracked: skill.tracked,
    enabled: skill.enabled,
    canonicalPath: skill.canonicalPath,
    sourcePath: skill.sourcePath,
    source: skill.source,
    sourceType: skill.sourceType,
    ref: skill.ref,
    contentHash: skill.contentHash,
    updatedAt: skill.updatedAt,
    scope: skill.scope,
    agents: skill.agents,
    bindings: skill.bindings,
    files: [
      { path: "SKILL.md", size: 782, mode: "0644" },
      { path: "references/release.md", size: 421, mode: "0644" },
    ],
    skillMd: `# ${skill.name}\n\n${skill.description ?? "Fixture package."}\n\n## Workflow\n\nValidate both mobile platforms and retain exact evidence.`,
    filePath: path,
    fileContent: path
      ? `Read-only fixture content for ${path}.\n\nNo installation was accessed.`
      : undefined,
    risk: [
      {
        type: "script",
        severity: "info",
        detail: "Contains a release helper.",
        file: "run.sh",
      },
    ],
    warnings: skill.warnings,
    capability: skill.capability,
  };
}

export function SkillsScreenshotDemo() {
  const [section, setSection] = useState<SkillsSurfaceSection>("skills");
  const [selectedAgent, setSelectedAgent] =
    useState<ManagedSkillAgent>("codex");
  const [query, setQuery] = useState("");
  const [inspectedName, setInspectedName] = useState<string | null>(null);
  const [inspectState, setInspectState] = useState<
    SkillsRequestState<PackageDetail>
  >({ status: "idle", generation: 0 });
  const [notice, setNotice] = useState<SurfaceMutationNotice | null>(null);
  const inventory = useMemo(
    () => ({
      generatedAt: "2026-08-17T04:15:00Z",
      cwd: "/fixture/project",
      skills: installedSkills,
      agents: [],
      warnings: [],
      mutationOperations: [...mutationOperations],
      migration: {
        owned: 1,
        external: 1,
        duplicate: 0,
        conflict: 0,
        tracked: 2,
      },
    }),
    [],
  );
  const showMutation = (message: string) =>
    setNotice({ kind: "success", message });
  const confirm = (title: string, message: string, action: string) =>
    Alert.alert(title, message, [
      { text: "Cancel", style: "cancel" },
      {
        text: action,
        style: "destructive",
        onPress: () =>
          showMutation(
            `${action} was simulated; fixture state was not changed.`,
          ),
      },
    ]);

  return (
    <SkillsPresentation
      section={section}
      selectedAgent={selectedAgent}
      agentCounts={skillsAgentCounts(inventory)}
      inventoryState={ready(inventory)}
      installedSkills={installedSkills.filter(
        (skill) =>
          skill.agents.includes(selectedAgent) || skill.bindings.length === 0,
      )}
      catalogSkills={catalogSkills}
      catalogInstalledElsewhere={{}}
      browsing
      catalogState={ready(emptyLeaderboards)}
      leaderboard={emptyLeaderboards.allTime}
      searchState={{ status: "idle", generation: 0 }}
      query={query}
      submittedQuery=""
      leaderboardView="all-time"
      pluginsState={ready(pluginInventory)}
      pluginsView={pluginsUnifiedView(pluginInventory)}
      mutationOperations={mutationOperations}
      hasProjectCwd
      preparingMutation=""
      mutationNotice={notice}
      currentServerAvailable
      inspectedName={inspectedName}
      inspectState={inspectState}
      onSelectSection={setSection}
      onSelectAgent={setSelectedAgent}
      onOpenSettings={() => undefined}
      onRefreshSkills={() => showMutation("Skills refreshed in place.")}
      onRetryPlugins={() => showMutation("Plugins refreshed in place.")}
      onInspectSkill={(name, path) => {
        setInspectedName(name);
        setInspectState(ready(detailFor(name, path)));
      }}
      onDismissInspector={() => setInspectedName(null)}
      onImport={(skill) =>
        confirm(
          `Import ${skill.name}?`,
          `Import ${skill.name} from ${skill.source} at its pinned ref and bind it to ${selectedAgent}.`,
          "Import",
        )
      }
      onMigrate={() =>
        showMutation("External scan completed without changing files.")
      }
      onBinding={(skill, operation, agent, scope) =>
        confirm(
          `${operation} ${skill.name}?`,
          `${operation} ${skill.name} for ${agent} at ${scope} scope. Package content remains in /fixture/.zen/skills/store/${skill.name}.`,
          operation,
        )
      }
      onUninstall={(skill) =>
        confirm(
          `Uninstall ${skill.name}?`,
          `Remove /fixture/.zen/skills/store/${skill.name} and its two managed bindings. The pinned source remains available.`,
          "Uninstall",
        )
      }
      onForget={(skill) =>
        confirm(
          `Forget ${skill.name}?`,
          `Remove only Zen inventory for ${skill.sourcePath}. External files remain untouched.`,
          "Forget",
        )
      }
      onAdopt={(skill) =>
        confirm(
          `Adopt ${skill.name}?`,
          `Copy ${skill.sourcePath} into Zen's canonical store. The external source remains untouched.`,
          "Adopt",
        )
      }
      onUpdate={(skill) =>
        confirm(
          `Update ${skill.name}?`,
          `Replace the canonical package and managed copies from pinned ref ${skill.ref}. Roll back all paths if commit fails.`,
          "Update",
        )
      }
      onInstallPlugin={(plugin) =>
        confirm(
          `Install ${plugin.name}?`,
          `Install ${plugin.pluginId} through its Claude Code catalog owner.`,
          "Install",
        )
      }
      onUpdatePlugin={(plugin) =>
        confirm(
          `Update ${plugin.name}?`,
          `Update ${plugin.id} through its owning client.`,
          "Update",
        )
      }
      onUninstallPlugin={(plugin) =>
        confirm(
          `Uninstall ${plugin.name}?`,
          `Remove ${plugin.id}; included Skills will no longer be available.`,
          "Uninstall",
        )
      }
      onChangeQuery={setQuery}
      onSubmitSearch={() => undefined}
      onClearSearch={() => setQuery("")}
      onSelectLeaderboard={() => undefined}
      onRetryCatalog={() => undefined}
      onRetrySearch={() => undefined}
      onDismissNotice={() => setNotice(null)}
    />
  );
}
