import React, { useEffect, useRef, useState } from "react";
import { Pressable, ScrollView, Text, TextInput, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { SessionFileRequest } from "../../services/sessionFilePreview";
import type { DSHAnswer, DSHInteraction, DSHInteractionSnapshot } from "../../services/dshInteractions";
import { wsClient } from "../../services/websocket";
import { useCurrentServer } from "../../store/currentServer";
import { BottomSheetFrame } from "../ui";

export function DSHInteractionPanel({ serverId, request, chrome }: { serverId: string; request: SessionFileRequest; chrome: TerminalThemeChrome }) {
  const { currentServerId, isCurrentServer } = useCurrentServer();
  const scope = JSON.stringify([serverId, request.workerId, request.processId, request.startedAt]);
  const owner = useRef(scope); owner.current = scope;
  const [snapshot, setSnapshot] = useState<{ scope: string; value: DSHInteractionSnapshot }>();
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const current = currentServerId === serverId && snapshot?.scope === scope ? snapshot.value : undefined;
  const first = current?.items[0];
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    owner.current = scope; setOpen(false); setError(""); setSnapshot(undefined); inFlight.current = false; setBusy(false);
    async function poll() {
      if (!isCurrentServer(serverId)) {
        setSnapshot(undefined);
        if (!cancelled) timer = setTimeout(poll, 1200);
        return;
      }
      try {
        const next = await wsClient.dshInteraction(serverId, request);
        if (!cancelled && isCurrentServer(serverId) && "items" in next) { setSnapshot({ scope, value: next }); setError(""); }
      } catch (error) { if (!cancelled && isCurrentServer(serverId)) { setSnapshot(undefined); setError(error instanceof Error ? error.message : "Requests unavailable"); } }
      if (!cancelled) timer = setTimeout(poll, 1200);
    }
    void poll();
    return () => { cancelled = true; clearTimeout(timer); if (owner.current === scope) owner.current = ""; };
  }, [scope, isCurrentServer, serverId]);
  const respond = async (answer: Omit<DSHAnswer, "epoch" | "rpcId">) => {
    if (!first || !current?.connected || !isCurrentServer(serverId) || inFlight.current) return;
    inFlight.current = true; setBusy(true); setError("");
    const rpcId = first.rpcId;
    try {
      await wsClient.dshInteraction(serverId, request, { ...answer, rpcId, epoch: current.epoch });
      if (owner.current === scope && isCurrentServer(serverId)) { setOpen(false); setSnapshot((previous) => previous?.scope === scope && previous.value.epoch === current.epoch ? { scope, value: { ...previous.value, items: previous.value.items.filter((item) => item.rpcId !== rpcId) } } : previous); }
    } catch (failure) { if (owner.current === scope && isCurrentServer(serverId)) setError(failure instanceof Error ? failure.message : "Answer failed. Refresh before retrying."); }
    finally { inFlight.current = false; if (owner.current === scope && isCurrentServer(serverId)) setBusy(false); }
  };
  if (currentServerId !== serverId) return null;
  if (!first && !error) return null;
  return <View style={{ padding: 10, backgroundColor: chrome.surfaceMuted }}>
    <Pressable accessibilityRole="button" onPress={() => setOpen(true)} disabled={!first} style={{ minHeight: 44, justifyContent: "center" }}>
      <Text style={{ color: chrome.text }}>{first ? `${first.payload.type === "approval/requested" ? "Permission requested" : "Answer requested"} · Review` : "DSH requests unavailable"}</Text>
    </Pressable>
    {error ? <Text selectable style={{ color: chrome.danger }}>{error}</Text> : null}
    <BottomSheetFrame visible={open && Boolean(first)} onClose={() => setOpen(false)} maxHeight="85%">
      {first ? <InteractionForm key={`${scope}:${current?.epoch}:${first.rpcId}`} interaction={first} chrome={chrome} busy={busy} respond={respond} /> : null}
      {error ? <Text selectable style={{ color: chrome.danger }}>{error}</Text> : null}
    </BottomSheetFrame>
  </View>;
}

function InteractionForm({ interaction, chrome, busy, respond }: { interaction: DSHInteraction; chrome: TerminalThemeChrome; busy: boolean; respond(answer: Omit<DSHAnswer, "epoch" | "rpcId">): Promise<void> }) {
  const [values, setValues] = useState<Record<string, { selected: string[]; custom?: string }>>({});
  const payload = interaction.payload;
  const questions = payload.questions ?? [];
  const action = (label: string, answer: Omit<DSHAnswer, "epoch" | "rpcId">, disabled = false) => <Pressable accessibilityRole="button" accessibilityLabel={label} disabled={busy || disabled} onPress={() => void respond(answer)} style={{ padding: 14, opacity: busy || disabled ? 0.45 : 1 }}><Text style={{ color: chrome.accent }}>{label}</Text></Pressable>;
  return <ScrollView keyboardShouldPersistTaps="handled" style={{ maxHeight: 520 }}>
    {payload.type === "approval/requested" ? <>
      <Text selectable style={{ color: chrome.text, fontSize: 18 }}>{payload.toolName || "Permission requested"}</Text>
      {payload.reason ? <Text selectable style={{ color: chrome.text, marginVertical: 12 }}>{payload.reason}</Text> : null}
      {payload.callId ? <Text selectable style={{ color: chrome.textMuted }}>{payload.callId}</Text> : null}
      <View style={{ flexDirection: "row" }}>{action("Reject", { outcome: "rejected" })}{action("Allow once", { outcome: "allowed-once" })}</View>
    </> : <>
      {questions.map((question) => <View key={question.id} style={{ gap: 8, marginBottom: 18 }}>
        <Text selectable style={{ color: chrome.text, fontSize: 17 }}>{question.question}</Text>
        {question.detail ? <Text selectable style={{ color: chrome.textMuted }}>{question.detail}</Text> : null}
        {(question.options ?? []).map((option) => {
          const selected = values[question.id]?.selected.includes(option.label) ?? false;
          return <Pressable key={option.label} accessibilityRole="checkbox" accessibilityState={{ checked: selected }} disabled={busy} onPress={() => setValues((previous) => {
            const value = previous[question.id] ?? { selected: [] };
            return { ...previous, [question.id]: { selected: question.multiSelect ? selected ? value.selected.filter((label) => label !== option.label) : [...value.selected, option.label] : selected ? [] : [option.label], custom: question.multiSelect ? value.custom : undefined } };
          })} style={{ padding: 12, borderWidth: 1, borderColor: selected ? chrome.accent : chrome.border, borderRadius: 8 }}>
            <Text style={{ color: chrome.text }}>{selected ? "✓ " : ""}{option.label}</Text>
            {option.description ? <Text style={{ color: chrome.textMuted }}>{option.description}</Text> : null}
          </Pressable>;
        })}
        <TextInput accessibilityLabel={`Custom answer: ${question.question}`} placeholder="Your answer" placeholderTextColor={chrome.textSubtle} multiline editable={!busy} value={values[question.id]?.custom ?? ""} onChangeText={(custom) => setValues((previous) => ({ ...previous, [question.id]: { selected: question.multiSelect || !custom ? previous[question.id]?.selected ?? [] : [], custom } }))} style={{ color: chrome.text, borderWidth: 1, borderColor: chrome.border, padding: 12, minHeight: 48 }} />
      </View>)}
      <View style={{ flexDirection: "row" }}>{action("Cancel request", { cancel: true })}{action("Send answer", { answer: { answers: questions.map((question) => ({ id: question.id, selected: values[question.id]?.selected ?? [], ...(values[question.id]?.custom?.trim() ? { custom: values[question.id]?.custom?.trim() } : {}) })) } }, !questions.length || questions.some((question) => !values[question.id]?.selected.length && !values[question.id]?.custom?.trim()))}</View>
    </>}
  </ScrollView>;
}
