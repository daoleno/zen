import React, { useEffect, useRef, useState } from 'react';
import {
  View,
  Text,
  TouchableOpacity,
  StyleSheet,
  SafeAreaView,
  ScrollView,
  Alert,
} from 'react-native';
import * as Haptics from 'expo-haptics';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { Colors, Spacing, Typography, statusColor } from '../../constants/tokens';
import { DefaultTerminalThemeName, TerminalThemeName } from '../../constants/terminalThemes';
import { useAgents } from '../../store/agents';
import { getTerminalTheme } from '../../services/storage';
import { wsClient } from '../../services/websocket';
import { TerminalSurface, TerminalSurfaceHandle } from '../../components/terminal/TerminalSurface';

const QUICK_ACTIONS = ['yes', 'no', 'show diff', 'pause', 'run tests', 'git status'];
const TERMINAL_KEYS = [
  { label: 'Focus', sequence: '__focus__' },
  { label: 'Space', sequence: ' ' },
  { label: 'Esc', sequence: '\x1b' },
  { label: 'Tab', sequence: '\t' },
  { label: 'Ctrl-C', sequence: '\x03' },
  { label: 'Ctrl-D', sequence: '\x04' },
  { label: 'Ctrl-L', sequence: '\x0c' },
  { label: '⌫', sequence: '\x7f' },
  { label: 'Enter', sequence: '\r' },
  { label: '←', sequence: '\x1b[D' },
  { label: '↑', sequence: '\x1b[A' },
  { label: '↓', sequence: '\x1b[B' },
  { label: '→', sequence: '\x1b[C' },
  { label: 'Home', sequence: '\x1b[H' },
  { label: 'End', sequence: '\x1b[F' },
  { label: 'PgUp', sequence: '\x1b[5~' },
  { label: 'PgDn', sequence: '\x1b[6~' },
] as const;

export default function TerminalScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { state } = useAgents();
  const router = useRouter();
  const [themeName, setThemeName] = useState<TerminalThemeName>(DefaultTerminalThemeName);
  const [ctrlArmed, setCtrlArmed] = useState(false);
  const [altArmed, setAltArmed] = useState(false);
  const terminalRef = useRef<TerminalSurfaceHandle>(null);

  const agent = state.agents.find(a => a.id === id);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const stored = await getTerminalTheme();
      if (!cancelled) {
        setThemeName(stored);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const handleQuickAction = (action: string) => {
    if (!id) return;
    const needsConfirm = action === 'no' || action === 'pause';
    const doAction = () => {
      if (action === 'yes') wsClient.sendAction(id, 'approve');
      else if (action === 'no') wsClient.sendAction(id, 'reject');
      else if (action === 'pause') wsClient.sendAction(id, 'pause');
      else if (action === 'show diff') wsClient.sendAction(id, 'show_diff');
      else if (action === 'run tests') wsClient.sendAction(id, 'run_tests');
      else if (action === 'git status') wsClient.sendAction(id, 'git_status');
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    };
    if (needsConfirm) {
      Alert.alert('Confirm', `Are you sure you want to ${action}?`, [
        { text: 'Cancel', style: 'cancel' },
        { text: action, style: action === 'no' ? 'destructive' : 'default', onPress: doAction },
      ]);
    } else {
      doAction();
    }
  };

  const applyModifiers = (sequence: string) => {
    let next = sequence;

    if (ctrlArmed && sequence.length === 1) {
      const code = sequence.toUpperCase().charCodeAt(0);
      if (code >= 64 && code <= 95) {
        next = String.fromCharCode(code - 64);
      }
      setCtrlArmed(false);
    }

    if (altArmed) {
      next = '\x1b' + next;
      setAltArmed(false);
    }

    return next;
  };

  const handleTerminalKey = (sequence: string) => {
    if (sequence === '__focus__') {
      terminalRef.current?.focus();
      return;
    }
    terminalRef.current?.sendInput(applyModifiers(sequence));
  };

  const toggleCtrl = () => {
    setCtrlArmed(value => !value);
  };

  const toggleAlt = () => {
    setAltArmed(value => !value);
  };

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.topBar}>
        <TouchableOpacity onPress={() => router.back()} style={styles.backBtn}>
          <Text style={styles.backText}>← inbox</Text>
        </TouchableOpacity>
        <Text style={styles.agentName}>{agent?.name || id}</Text>
        {agent && (
          <View style={[styles.statusBadge, { backgroundColor: statusColor(agent.status) + '33' }]}>
            <Text style={[styles.statusText, { color: statusColor(agent.status) }]}>{agent.status}</Text>
          </View>
        )}
      </View>

      <View style={styles.output}>
        {id ? <TerminalSurface ref={terminalRef} targetId={id} themeName={themeName} /> : null}
      </View>

      <View style={styles.inputArea}>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.terminalKeyRow}>
          <TouchableOpacity
            style={[styles.modifierBtn, ctrlArmed && styles.modifierBtnActive]}
            onPress={toggleCtrl}
          >
            <Text style={[styles.modifierText, ctrlArmed && styles.modifierTextActive]}>Ctrl</Text>
          </TouchableOpacity>
          <TouchableOpacity
            style={[styles.modifierBtn, altArmed && styles.modifierBtnActive]}
            onPress={toggleAlt}
          >
            <Text style={[styles.modifierText, altArmed && styles.modifierTextActive]}>Alt</Text>
          </TouchableOpacity>

          {TERMINAL_KEYS.map(key => (
            <TouchableOpacity
              key={key.label}
              style={styles.terminalKeyBtn}
              onPress={() => handleTerminalKey(key.sequence)}
            >
              <Text style={styles.terminalKeyText}>{key.label}</Text>
            </TouchableOpacity>
          ))}
        </ScrollView>

        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.quickRow}>
          {QUICK_ACTIONS.map(action => (
            <TouchableOpacity
              key={action}
              style={styles.quickBtn}
              onPress={() => handleQuickAction(action)}
            >
              <Text style={styles.quickBtnText}>{action}</Text>
            </TouchableOpacity>
          ))}
        </ScrollView>
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.bgPrimary },
  topBar: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: Spacing.screenMargin,
    paddingVertical: 12,
    borderBottomWidth: 1,
    borderBottomColor: Colors.bgSurface,
  },
  backBtn: { marginRight: 12 },
  backText: { color: Colors.textSecondary, fontSize: 14 },
  agentName: { color: Colors.accent, fontSize: 15, fontWeight: '600', fontFamily: Typography.terminalFont },
  statusBadge: { marginLeft: 8, paddingHorizontal: 8, paddingVertical: 2, borderRadius: 8 },
  statusText: { fontSize: 11, fontWeight: '500' },
  output: {
    flex: 1,
    paddingTop: 8,
  },
  inputArea: {
    borderTopWidth: 1,
    borderTopColor: Colors.bgSurface,
    backgroundColor: Colors.bgElevated,
  },
  terminalKeyRow: {
    paddingHorizontal: 12,
    paddingVertical: 8,
    maxHeight: 50,
  },
  terminalKeyBtn: {
    paddingHorizontal: 12,
    paddingVertical: 7,
    borderRadius: 10,
    backgroundColor: '#121c28',
    marginRight: 6,
    minHeight: 34,
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#27364a',
  },
  terminalKeyText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.terminalFont,
    fontWeight: '600',
  },
  modifierBtn: {
    paddingHorizontal: 12,
    paddingVertical: 7,
    borderRadius: 10,
    backgroundColor: '#1b2634',
    marginRight: 6,
    minHeight: 34,
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#31445d',
  },
  modifierBtnActive: {
    backgroundColor: Colors.accent,
    borderColor: Colors.accent,
  },
  modifierText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.terminalFont,
    fontWeight: '700',
  },
  modifierTextActive: {
    color: Colors.bgPrimary,
  },
  quickRow: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    maxHeight: 40,
  },
  quickBtn: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 8,
    backgroundColor: Colors.bgSurface,
    marginRight: 6,
    minHeight: 32,
    justifyContent: 'center',
  },
  quickBtnText: { color: Colors.textSecondary, fontSize: 12, fontFamily: Typography.terminalFont },
});
