import React, { useRef, useState, useEffect } from 'react';
import {
  View,
  Text,
  FlatList,
  TextInput,
  TouchableOpacity,
  StyleSheet,
  SafeAreaView,
  ScrollView,
  KeyboardAvoidingView,
  Platform,
  Alert,
} from 'react-native';
import * as Haptics from 'expo-haptics';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { Colors, Spacing, Typography, statusColor } from '../../constants/tokens';
import { useAgents } from '../../store/agents';
import { wsClient } from '../../services/websocket';
import { AnsiLine } from '../../services/ansi';

const QUICK_ACTIONS = ['yes', 'no', 'show diff', 'pause', 'run tests', 'git status'];

export default function TerminalScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { state } = useAgents();
  const router = useRouter();
  const [input, setInput] = useState('');
  const flatListRef = useRef<FlatList>(null);
  const [autoScroll, setAutoScroll] = useState(true);

  const agent = state.agents.find(a => a.id === id);
  const lines = agent?.last_output_lines || [];

  useEffect(() => {
    if (autoScroll && flatListRef.current && lines.length > 0) {
      flatListRef.current.scrollToEnd({ animated: false });
    }
  }, [lines.length, autoScroll]);

  const handleSend = () => {
    if (!input.trim() || !id) return;
    wsClient.sendInput(id, input + '\n');
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setInput('');
  };

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

  const renderLine = ({ item, index }: { item: string; index: number }) => (
    <AnsiLine text={item} key={index} />
  );

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

      <FlatList
        ref={flatListRef}
        data={lines}
        renderItem={renderLine}
        keyExtractor={(_, i) => String(i)}
        style={styles.output}
        onScrollBeginDrag={() => setAutoScroll(false)}
        onEndReached={() => setAutoScroll(true)}
        onEndReachedThreshold={0.1}
        initialNumToRender={50}
        maxToRenderPerBatch={50}
        windowSize={21}
      />

      {!autoScroll && lines.length > 50 && (
        <TouchableOpacity
          style={styles.scrollPill}
          onPress={() => {
            setAutoScroll(true);
            flatListRef.current?.scrollToEnd({ animated: true });
          }}
        >
          <Text style={styles.scrollPillText}>↓ Scroll to bottom</Text>
        </TouchableOpacity>
      )}

      <KeyboardAvoidingView
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        style={styles.inputArea}
      >
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

        <View style={styles.inputRow}>
          <TextInput
            style={styles.textInput}
            value={input}
            onChangeText={setInput}
            placeholder="Tell agent what to do..."
            placeholderTextColor={Colors.textSecondary}
            onSubmitEditing={handleSend}
            returnKeyType="send"
          />
          <TouchableOpacity style={styles.sendBtn} onPress={handleSend}>
            <Text style={styles.sendBtnText}>↑</Text>
          </TouchableOpacity>
        </View>
      </KeyboardAvoidingView>
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
    paddingHorizontal: Spacing.screenMargin,
    paddingTop: 8,
  },
  outputLine: {
    color: Colors.textPrimary,
    fontFamily: Typography.terminalFont,
    fontSize: Typography.terminalSize,
    lineHeight: Typography.terminalSize * 1.6,
  },
  scrollPill: {
    position: 'absolute',
    bottom: 160,
    alignSelf: 'center',
    backgroundColor: Colors.bgElevated,
    paddingHorizontal: 16,
    paddingVertical: 8,
    borderRadius: 20,
    borderWidth: 1,
    borderColor: Colors.textSecondary,
  },
  scrollPillText: { color: Colors.textPrimary, fontSize: 12 },
  inputArea: {
    borderTopWidth: 1,
    borderTopColor: Colors.bgSurface,
    backgroundColor: Colors.bgElevated,
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
  inputRow: {
    flexDirection: 'row',
    paddingHorizontal: 12,
    paddingVertical: 10,
    gap: 8,
    alignItems: 'center',
  },
  textInput: {
    flex: 1,
    backgroundColor: Colors.bgSurface,
    borderRadius: 12,
    paddingHorizontal: 14,
    paddingVertical: 10,
    color: Colors.textPrimary,
    fontFamily: Typography.terminalFont,
    fontSize: 14,
    borderWidth: 1,
    borderColor: Colors.bgElevated,
  },
  sendBtn: {
    width: 40,
    height: 40,
    borderRadius: 10,
    backgroundColor: Colors.accent,
    justifyContent: 'center',
    alignItems: 'center',
  },
  sendBtnText: { color: Colors.bgPrimary, fontSize: 18, fontWeight: '700' },
});
