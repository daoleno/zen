import type { CodexPlanStep } from "../../services/codexConversation";

export interface ZenPlanTimelineItem {
  type: "plan";
  id: string;
  timestamp?: string;
  explanation?: string;
  steps: CodexPlanStep[];
}
