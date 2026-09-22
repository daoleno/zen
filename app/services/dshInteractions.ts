export type DSHQuestion = { id: string; question: string; detail?: string; header?: string; multiSelect?: boolean; options?: { label: string; description?: string }[] };
export type DSHInteraction = { rpcId: string; payload: { type: "approval/requested" | "question/requested"; sessionId: string; approvalId?: string; toolName?: string; callId?: string; reason?: string; questions?: DSHQuestion[] } };
export type DSHInteractionSnapshot = { epoch: string; connected: boolean; items: DSHInteraction[] };
export type DSHAnswer = { epoch: string; rpcId: string; outcome?: "allowed-once" | "rejected"; cancel?: boolean; answer?: { answers: { id: string; selected: string[]; custom?: string }[] } };
