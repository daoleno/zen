/**
 * Pure render-semantic equality for timeline row stabilization.
 * Allocation-free: no serialize, hash, clone, normalize, mutate, or retain.
 * No React runtime dependency — safe for Bun tests and microbenchmarks.
 */

import type { BrainWorkResultEvent } from "../brain/brainWorkEvent";
import type { ToolDeveloperDetails } from "../../services/toolCallDetails";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import type {
  PatchFileSummary,
  ZenActivityChild,
  ZenActivityTimelineItem,
} from "./InterfaceTimelineActivityTypes";
import type { DisplayAttachment } from "./InterfaceTimelineMessage";
import type { ZenPlanTimelineItem } from "./InterfaceTimelinePlanTypes";

/**
 * Returns true only when every field that can affect the rendered row,
 * accessibility, expansion details, action, or lifecycle is semantically equal.
 * Identity fast path is checked first.
 */
export function timelineItemsSemanticEqual(
  left: ZenTimelineItem,
  right: ZenTimelineItem,
): boolean {
  if (left === right) {
    return true;
  }
  if (
    left.type !== right.type ||
    left.id !== right.id ||
    left.timestamp !== right.timestamp
  ) {
    return false;
  }
  if (left.type === "message" && right.type === "message") {
    return (
      left.role === right.role &&
      left.body === right.body &&
      left.pending === right.pending &&
      left.pendingLifecycle === right.pendingLifecycle &&
      left.pendingLifecycleLabel === right.pendingLifecycleLabel &&
      left.pendingFailureMessage === right.pendingFailureMessage &&
      left.onRetryPending === right.onRetryPending &&
      left.streaming === right.streaming &&
      left.turnFocusAnchorId === right.turnFocusAnchorId &&
      attachmentsEqual(left.attachments, right.attachments) &&
      // Conservative identity: HeartbeatWakeEvent schema is legacy and not
      // field-walked here; distinct objects with identical fields do not reuse.
      left.heartbeatWake === right.heartbeatWake
    );
  }
  if (left.type === "plan" && right.type === "plan") {
    return planItemsEqual(left, right);
  }
  if (left.type === "activity" && right.type === "activity") {
    return activityItemsEqual(left, right);
  }
  if (
    left.type === "brain-work-event" &&
    right.type === "brain-work-event"
  ) {
    return (
      brainWorkResultEventsEqual(left.event, right.event) &&
      left.onPress === right.onPress
    );
  }
  return false;
}

function planItemsEqual(
  left: ZenPlanTimelineItem,
  right: ZenPlanTimelineItem,
): boolean {
  if (left.explanation !== right.explanation) {
    return false;
  }
  if (left.steps.length !== right.steps.length) {
    return false;
  }
  for (let index = 0; index < left.steps.length; index += 1) {
    const leftStep = left.steps[index];
    const rightStep = right.steps[index];
    if (
      leftStep?.step !== rightStep?.step ||
      leftStep?.status !== rightStep?.status
    ) {
      return false;
    }
  }
  return true;
}

function activityItemsEqual(
  left: ZenActivityTimelineItem,
  right: ZenActivityTimelineItem,
): boolean {
  return (
    left.statusKey === right.statusKey &&
    left.title === right.title &&
    left.tone === right.tone &&
    left.icon === right.icon &&
    left.activityKind === right.activityKind &&
    left.streaming === right.streaming &&
    left.detail === right.detail &&
    left.body === right.body &&
    left.bodyKind === right.bodyKind &&
    left.commandText === right.commandText &&
    left.queryText === right.queryText &&
    left.statusLine === right.statusLine &&
    left.previewPath === right.previewPath &&
    left.defaultExpanded === right.defaultExpanded &&
    left.accessibilityLabel === right.accessibilityLabel &&
    left.providerToolId === right.providerToolId &&
    stringArraysEqual(left.files, right.files) &&
    patchSummariesEqual(left.fileSummaries, right.fileSummaries) &&
    toolDeveloperDetailsEqual(left.developerDetails, right.developerDetails) &&
    activityChildrenEqual(left.children, right.children)
  );
}

/** Owns providerToolId, rawInput, and string-valued transport (no key order). */
export function toolDeveloperDetailsEqual(
  left?: ToolDeveloperDetails,
  right?: ToolDeveloperDetails,
): boolean {
  if (left === right) {
    return true;
  }
  if (!left || !right) {
    return false;
  }
  return (
    left.providerToolId === right.providerToolId &&
    left.rawInput === right.rawInput &&
    stringRecordsEqual(left.transport, right.transport)
  );
}

/**
 * String-valued transport record equality.
 * Key insertion order is not semantic.
 */
export function stringRecordsEqual(
  left?: Record<string, string>,
  right?: Record<string, string>,
): boolean {
  if (left === right) {
    return true;
  }
  if (!left || !right) {
    return false;
  }
  let leftCount = 0;
  for (const key in left) {
    if (!Object.prototype.hasOwnProperty.call(left, key)) {
      continue;
    }
    leftCount += 1;
    if (
      !Object.prototype.hasOwnProperty.call(right, key) ||
      left[key] !== right[key]
    ) {
      return false;
    }
  }
  let rightCount = 0;
  for (const key in right) {
    if (!Object.prototype.hasOwnProperty.call(right, key)) {
      continue;
    }
    rightCount += 1;
  }
  return leftCount === rightCount;
}

/** Owns id, title, tone, providerToolId in array order. */
export function activityChildrenEqual(
  left?: ZenActivityChild[],
  right?: ZenActivityChild[],
): boolean {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    const leftChild = left[index];
    const rightChild = right[index];
    if (
      leftChild?.id !== rightChild?.id ||
      leftChild?.title !== rightChild?.title ||
      leftChild?.tone !== rightChild?.tone ||
      leftChild?.providerToolId !== rightChild?.providerToolId
    ) {
      return false;
    }
  }
  return true;
}

/** Owns every canonical Brain Work event field explicitly. */
export function brainWorkResultEventsEqual(
  left: BrainWorkResultEvent,
  right: BrainWorkResultEvent,
): boolean {
  if (left === right) {
    return true;
  }
  return (
    left.event_id === right.event_id &&
    left.kind === right.kind &&
    left.work_id === right.work_id &&
    left.work_title === right.work_title &&
    left.summary === right.summary &&
    left.session_id === right.session_id &&
    left.session_name === right.session_name &&
    left.occurred_at === right.occurred_at &&
    left.unread === right.unread &&
    left.review_state === right.review_state &&
    left.session_state === right.session_state &&
    left.current_result === right.current_result
  );
}

/**
 * Owns every DisplayAttachment field the preview renderer consumes:
 * name, path, localUri, mimeType — in array order.
 */
export function attachmentsEqual(
  left: DisplayAttachment[],
  right: DisplayAttachment[],
): boolean {
  if (left === right) {
    return true;
  }
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    const leftAttachment = left[index];
    const rightAttachment = right[index];
    if (
      leftAttachment?.name !== rightAttachment?.name ||
      leftAttachment?.path !== rightAttachment?.path ||
      leftAttachment?.localUri !== rightAttachment?.localUri ||
      leftAttachment?.mimeType !== rightAttachment?.mimeType
    ) {
      return false;
    }
  }
  return true;
}

function stringArraysEqual(left?: string[], right?: string[]) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
      return false;
    }
  }
  return true;
}

function patchSummariesEqual(
  left?: PatchFileSummary[],
  right?: PatchFileSummary[],
) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    const leftFile = left[index];
    const rightFile = right[index];
    if (
      leftFile?.path !== rightFile?.path ||
      leftFile?.movePath !== rightFile?.movePath ||
      leftFile?.operation !== rightFile?.operation ||
      leftFile?.added !== rightFile?.added ||
      leftFile?.removed !== rightFile?.removed
    ) {
      return false;
    }
  }
  return true;
}
