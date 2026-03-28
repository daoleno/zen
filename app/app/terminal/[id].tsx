import React, { useEffect, useRef, useState } from 'react';
import {
  Keyboard,
  KeyboardEvent,
  View,
  Text,
  TouchableOpacity,
  StyleSheet,
  SafeAreaView,
} from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { Colors, Spacing, Typography, statusColor } from '../../constants/tokens';
import { DefaultTerminalThemeName, TerminalThemeName } from '../../constants/terminalThemes';
import { useAgents } from '../../store/agents';
import { getTerminalTheme } from '../../services/storage';
import { TerminalSurface, TerminalSurfaceHandle } from '../../components/terminal/TerminalSurface';
import { InputBar, InputBarHandle } from '../../components/terminal/InputBar';

export default function TerminalScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { state } = useAgents();
  const router = useRouter();
  const [themeName, setThemeName] = useState<TerminalThemeName>(DefaultTerminalThemeName);
  const [keyboardVisible, setKeyboardVisible] = useState(false);
  const [keyboardHeight, setKeyboardHeight] = useState(0);
  const [inputBarHeight, setInputBarHeight] = useState(54);
  const terminalRef = useRef<TerminalSurfaceHandle>(null);
  const inputBarRef = useRef<InputBarHandle>(null);

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

  useEffect(() => {
    const handleShow = (event: KeyboardEvent) => {
      setKeyboardVisible(true);
      setKeyboardHeight(event.endCoordinates?.height ?? 0);
    };
    const handleHide = () => {
      setKeyboardVisible(false);
      setKeyboardHeight(0);
    };

    const showSub = Keyboard.addListener('keyboardDidShow', handleShow);
    const hideSub = Keyboard.addListener('keyboardDidHide', handleHide);

    return () => {
      showSub.remove();
      hideSub.remove();
    };
  }, []);

  const toolbarBottom = keyboardVisible ? keyboardHeight + 18 : 14;
  const outputBottomInset = inputBarHeight + toolbarBottom + 8;

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

      <View style={[styles.output, { paddingBottom: outputBottomInset }]}>
        {id ? (
          <TerminalSurface
            ref={terminalRef}
            targetId={id}
            themeName={themeName}
            onTap={() => inputBarRef.current?.focus()}
          />
        ) : null}
      </View>

      {id ? (
        <View
          style={[styles.inputShell, { bottom: toolbarBottom }]}
          onLayout={(event) => {
            setInputBarHeight(event.nativeEvent.layout.height);
          }}
        >
          <InputBar ref={inputBarRef} terminalRef={terminalRef} />
        </View>
      ) : null}
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
  agentName: { color: Colors.accent, fontSize: 15, fontFamily: Typography.uiFontMedium },
  statusBadge: { marginLeft: 8, paddingHorizontal: 8, paddingVertical: 2, borderRadius: 8 },
  statusText: { fontSize: 11, fontWeight: '500' },
  output: {
    flex: 1,
    paddingTop: 8,
  },
  inputShell: {
    position: 'absolute',
    left: 0,
    right: 0,
    paddingHorizontal: 12,
    backgroundColor: 'transparent',
  },
});
