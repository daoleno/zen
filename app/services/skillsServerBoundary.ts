export type SkillsRequestChannel =
  "inventory" | "catalog" | "search" | "mutation" | "handoff";

export interface SkillsServerRequestToken {
  channel: SkillsRequestChannel;
  generation: number;
  serverId: string | null;
}

const channels: SkillsRequestChannel[] = [
  "inventory",
  "catalog",
  "search",
  "mutation",
  "handoff",
];

/**
 * One mounted Skills surface can have independent requests in flight, but all
 * of them belong to one current-server generation. Rebinding invalidates every
 * channel before React effects have a chance to clear presentation state.
 */
export class SkillsServerRequestOwner {
  private serverId: string | null = null;
  private generations: Record<SkillsRequestChannel, number> = {
    inventory: 0,
    catalog: 0,
    search: 0,
    mutation: 0,
    handoff: 0,
  };

  rebind(serverId: string | null | undefined): boolean {
    const normalizedId = serverId?.trim() || null;
    if (normalizedId === this.serverId) {
      return false;
    }
    this.serverId = normalizedId;
    this.invalidateAll();
    return true;
  }

  issue(channel: SkillsRequestChannel): SkillsServerRequestToken {
    const generation = ++this.generations[channel];
    return { channel, generation, serverId: this.serverId };
  }

  invalidate(channel: SkillsRequestChannel): number {
    return ++this.generations[channel];
  }

  invalidateAll(): void {
    for (const channel of channels) {
      this.invalidate(channel);
    }
  }

  isCurrent(token: SkillsServerRequestToken): boolean {
    return (
      token.serverId === this.serverId &&
      token.generation === this.generations[token.channel]
    );
  }
}
