import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Keyboard,
  KeyboardEvent,
  Modal,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
  useWindowDimensions,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { useFocusEffect, useLocalSearchParams, useRouter } from 'expo-router';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';
import { Agent, useAgents } from '../../store/agents';
import { AgentStatus, Colors, Typography, statusColor } from '../../constants/tokens';
import { DefaultTerminalThemeName, TerminalThemeName } from '../../constants/terminalThemes';
import {
  closeOtherTerminalTabs,
  closeTerminalTab,
  getAgentAliases,
  getRecentAgentOpens,
  getServerById,
  getTerminalTabs,
  getTerminalTheme,
  markAgentOpened,
  setAgentAlias,
  setTerminalTabPinned,
  StoredAgentAliases,
  StoredRecentAgentOpens,
  StoredTerminalTabs,
  touchTerminalTab,
} from '../../services/storage';
import { makeSessionKey, parseSessionKey } from '../../services/sessionKeys';
import { TerminalSurface, TerminalSurfaceHandle } from '../../components/terminal/TerminalSurface';
import { InputBar, InputBarHandle } from '../../components/terminal/InputBar';

const EMPTY_TABS: StoredTerminalTabs = { order: [], pinned: [] };
const MENU_POPOVER_WIDTH = 168;

const STATUS_PRIORITY: Record<AgentStatus, number> = {
  failed: 0,
  blocked: 1,
  unknown: 2,
  running: 3,
  done: 4,
};

interface TerminalTabDescriptor {
  id: string;
  name: string;
  status: AgentStatus;
  pinned: boolean;
  active: boolean;
}

export default function TerminalScreen() {
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  const agentId = typeof params.id === 'string' ? params.id : '';
  const serverId = typeof params.serverId === 'string' ? params.serverId : '';
  const sessionKey = agentId && serverId ? makeSessionKey(serverId, agentId) : null;
  const { state, dispatch } = useAgents();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { width: windowWidth } = useWindowDimensions();
  const [themeName, setThemeName] = useState<TerminalThemeName>(DefaultTerminalThemeName);
  const [agentAliases, setAgentAliases] = useState<StoredAgentAliases>({});
  const [recentAgentOpens, setRecentAgentOpens] = useState<StoredRecentAgentOpens>({});
  const [terminalTabs, setTerminalTabs] = useState<StoredTerminalTabs>(EMPTY_TABS);
  const [serverUrl, setServerUrl] = useState('');
  const [pickerVisible, setPickerVisible] = useState(false);
  const [menuVisible, setMenuVisible] = useState(false);
  const [menuAnchor, setMenuAnchor] = useState<{ x: number; y: number; width: number; height: number } | null>(null);
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameDraft, setRenameDraft] = useState('');
  const [keyboardVisible, setKeyboardVisible] = useState(false);
  const [keyboardHeight, setKeyboardHeight] = useState(0);
  const [inputBarHeight, setInputBarHeight] = useState(54);
  const terminalRef = useRef<TerminalSurfaceHandle>(null);
  const inputBarRef = useRef<InputBarHandle>(null);
  const menuAnchorRef = useRef<View | null>(null);

  const agentByKey = useMemo(
    () => new Map(state.agents.map(agent => [agent.key, agent])),
    [state.agents],
  );
  const agent = sessionKey ? agentByKey.get(sessionKey) : undefined;
  const activePinned = sessionKey ? terminalTabs.pinned.includes(sessionKey) : false;
  const displayName = useMemo(
    () => resolveAgentName(agent, sessionKey, agentAliases),
    [agent, agentAliases, sessionKey],
  );

  useFocusEffect(
    React.useCallback(() => {
      dispatch({ type: 'SELECT_AGENT', key: sessionKey });
      return () => {
        dispatch({ type: 'SELECT_AGENT', key: null });
      };
    }, [dispatch, sessionKey]),
  );

  useEffect(() => {
    let cancelled = false;

    (async () => {
      const [storedTheme, storedRecentOpens, storedAliases] = await Promise.all([
        getTerminalTheme(),
        getRecentAgentOpens(),
        getAgentAliases(),
      ]);
      const storedServer = serverId ? await getServerById(serverId) : null;

      if (!sessionKey) {
        const storedTabs = await getTerminalTabs();
        if (!cancelled) {
          setThemeName(storedTheme);
          setAgentAliases(storedAliases);
          setRecentAgentOpens(storedRecentOpens);
          setTerminalTabs(storedTabs);
          setServerUrl(storedServer?.url || '');
        }
        return;
      }

      const openedAt = Date.now();
      const nextTabs = await touchTerminalTab(sessionKey);
      void markAgentOpened(sessionKey, openedAt);

      if (!cancelled) {
        setThemeName(storedTheme);
        setAgentAliases(storedAliases);
        setRecentAgentOpens({
          ...storedRecentOpens,
          [sessionKey]: openedAt,
        });
        setTerminalTabs(nextTabs);
        setServerUrl(storedServer?.url || '');
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [sessionKey]);

  useEffect(() => {
    setMenuVisible(false);
    setMenuAnchor(null);
  }, [sessionKey]);

  useEffect(() => {
    setRenameVisible(false);
    setRenameDraft('');
  }, [sessionKey]);

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

  const tabs = useMemo(() => {
    const order = buildDisplayTabOrder(sessionKey, terminalTabs);
    return order.map(currentSessionKey => {
      const tabAgent = agentByKey.get(currentSessionKey);
      const parsed = parseSessionKey(currentSessionKey);
      const serverLabel = tabAgent?.serverName || parsed?.serverId || 'server';
      return {
        id: currentSessionKey,
        name: formatTabLabel(serverLabel, resolveAgentName(tabAgent, currentSessionKey, agentAliases)),
        status: tabAgent?.status || 'unknown',
        pinned: terminalTabs.pinned.includes(currentSessionKey),
        active: currentSessionKey === sessionKey,
      } satisfies TerminalTabDescriptor;
    });
  }, [agentAliases, agentByKey, sessionKey, terminalTabs]);

  const sortedAgents = useMemo(() => {
    const openTabs = new Set(terminalTabs.order);
    const pinnedTabs = new Set(terminalTabs.pinned);

    return [...state.agents].sort((left, right) => {
      const leftPinned = pinnedTabs.has(left.key) ? 0 : 1;
      const rightPinned = pinnedTabs.has(right.key) ? 0 : 1;
      if (leftPinned !== rightPinned) return leftPinned - rightPinned;

      const leftOpen = openTabs.has(left.key) ? 0 : 1;
      const rightOpen = openTabs.has(right.key) ? 0 : 1;
      if (leftOpen !== rightOpen) return leftOpen - rightOpen;

      const leftOpenedAt = recentAgentOpens[left.key] ?? 0;
      const rightOpenedAt = recentAgentOpens[right.key] ?? 0;
      if (leftOpenedAt !== rightOpenedAt) return rightOpenedAt - leftOpenedAt;

      const leftPriority = STATUS_PRIORITY[left.status] ?? 5;
      const rightPriority = STATUS_PRIORITY[right.status] ?? 5;
      if (leftPriority !== rightPriority) return leftPriority - rightPriority;

      return (right.updated_at || 0) - (left.updated_at || 0);
    });
  }, [recentAgentOpens, state.agents, terminalTabs]);

  const menuPosition = useMemo(
    () => buildMenuPosition(menuAnchor, windowWidth),
    [menuAnchor, windowWidth],
  );

  const toolbarBottom = keyboardVisible ? keyboardHeight + 18 : Math.max(insets.bottom + 10, 14);
  const outputBottomInset = inputBarHeight + toolbarBottom + 8;

  const closeMenu = () => {
    setMenuVisible(false);
    setMenuAnchor(null);
  };

  const openRenameModal = () => {
    closeMenu();
    setRenameDraft(displayName);
    setRenameVisible(true);
  };

  const openAgentTab = (agentId: string) => {
    setPickerVisible(false);
    closeMenu();

    if (!agentId || agentId === sessionKey) return;
    const parsed = parseSessionKey(agentId);
    if (!parsed) return;
    router.replace({
      pathname: '/terminal/[id]',
      params: { id: parsed.agentId, serverId: parsed.serverId },
    });
  };

  const goToInbox = () => {
    setPickerVisible(false);
    closeMenu();
    router.replace('/');
  };

  const openMenu = () => {
    const anchor = menuAnchorRef.current;
    if (!anchor) {
      setMenuAnchor(null);
      setMenuVisible(true);
      return;
    }

    anchor.measureInWindow((x, y, width, height) => {
      setMenuAnchor({ x, y, width, height });
      setMenuVisible(true);
    });
  };

  const handleTogglePinned = async () => {
    if (!sessionKey) return;
    const nextTabs = await setTerminalTabPinned(sessionKey, !activePinned);
    setTerminalTabs(nextTabs);
    closeMenu();
  };

  const handleCloseCurrentTab = async () => {
    if (!sessionKey) return;

    const nextTabs = await closeTerminalTab(sessionKey);
    setTerminalTabs(nextTabs);
    closeMenu();

    const nextSessionKey = pickNextTabAfterClose(sessionKey, terminalTabs, nextTabs);
    if (nextSessionKey) {
      const parsed = parseSessionKey(nextSessionKey);
      if (parsed) {
        router.replace({
          pathname: '/terminal/[id]',
          params: { id: parsed.agentId, serverId: parsed.serverId },
        });
        return;
      }
      return;
    }

    router.replace('/');
  };

  const handleCloseOtherTabs = async () => {
    if (!sessionKey) return;

    const nextTabs = await closeOtherTerminalTabs(sessionKey);
    setTerminalTabs(nextTabs);
    closeMenu();
  };

  const handleSaveRename = async () => {
    if (!sessionKey) return;
    const nextAliases = await setAgentAlias(sessionKey, renameDraft);
    setAgentAliases(nextAliases);
    setRenameVisible(false);
  };

  return (
    <SafeAreaView style={styles.container} edges={['top']}>
      <View style={styles.topBar}>
        <TouchableOpacity
          onPress={goToInbox}
          style={styles.chromeButton}
          activeOpacity={0.84}
        >
          <Ionicons name="chevron-back" size={22} color={Colors.textPrimary} />
        </TouchableOpacity>

        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          style={styles.tabScroller}
          contentContainerStyle={styles.tabScrollerContent}
        >
          {tabs.map(tab => (
            <View
              key={tab.id}
              style={[
                styles.tabPill,
                tab.active && styles.tabPillActive,
                tab.pinned && styles.tabPillPinned,
              ]}
            >
              <TouchableOpacity
                style={styles.tabMainButton}
                onPress={() => openAgentTab(tab.id)}
                activeOpacity={0.84}
              >
                <View
                  style={[
                    styles.tabStatusDot,
                    { backgroundColor: statusColor(tab.status) },
                  ]}
                />
                <Text
                  style={[
                    styles.tabLabel,
                    tab.active && styles.tabLabelActive,
                  ]}
                  numberOfLines={1}
                >
                  {tab.name}
                </Text>
                {tab.pinned ? (
                  <Ionicons
                    name="bookmark"
                    size={12}
                    color={tab.active ? Colors.textPrimary : '#73839A'}
                  />
                ) : null}
              </TouchableOpacity>

              {tab.active ? (
                <View ref={menuAnchorRef} collapsable={false}>
                  <TouchableOpacity
                    style={styles.tabMenuButton}
                    onPress={openMenu}
                    activeOpacity={0.84}
                  >
                    <Ionicons name="ellipsis-vertical" size={17} color={Colors.textPrimary} />
                  </TouchableOpacity>
                </View>
              ) : null}
            </View>
          ))}
        </ScrollView>

        <TouchableOpacity
          onPress={() => setPickerVisible(true)}
          style={styles.chromeButton}
          activeOpacity={0.84}
        >
          <Ionicons name="add" size={22} color={Colors.textPrimary} />
        </TouchableOpacity>
      </View>

      <View style={[styles.output, { paddingBottom: outputBottomInset }]}>
        {sessionKey && serverId && agentId ? (
          <TerminalSurface
            ref={terminalRef}
            serverId={serverId}
            targetId={agentId}
            themeName={themeName}
            onTap={() => inputBarRef.current?.focus()}
          />
        ) : null}
      </View>

      {sessionKey && serverId && agentId && serverUrl ? (
        <View
          style={[styles.inputShell, { bottom: toolbarBottom }]}
          onLayout={(event) => {
            setInputBarHeight(event.nativeEvent.layout.height);
          }}
        >
          <InputBar
            ref={inputBarRef}
            terminalRef={terminalRef}
            serverUrl={serverUrl}
          />
        </View>
      ) : null}

      <Modal
        visible={pickerVisible}
        transparent
        animationType="fade"
        onRequestClose={() => setPickerVisible(false)}
      >
        <View style={styles.modalRoot}>
          <TouchableOpacity
            style={styles.modalBackdrop}
            activeOpacity={1}
            onPress={() => setPickerVisible(false)}
          />

          <View style={styles.sheetCard}>
            <View style={styles.sheetHandle} />
            <Text style={styles.sheetTitle}>Switch Agent</Text>
            <Text style={styles.sheetSubtitle}>
              Keep one live terminal, jump between recent agents instantly.
            </Text>

            <ScrollView
              style={styles.sheetScroll}
              contentContainerStyle={styles.sheetScrollContent}
              showsVerticalScrollIndicator={false}
            >
              {sortedAgents.length === 0 ? (
                <Text style={styles.sheetEmpty}>No agents available right now.</Text>
              ) : (
                sortedAgents.map(item => {
                  const isActive = item.key === sessionKey;
                  const isOpen = terminalTabs.order.includes(item.key);
                  const isPinned = terminalTabs.pinned.includes(item.key);

                  return (
                    <TouchableOpacity
                      key={item.key}
                      style={[
                        styles.agentRow,
                        isActive && styles.agentRowActive,
                      ]}
                      onPress={() => openAgentTab(item.key)}
                      activeOpacity={0.84}
                    >
                      <View style={styles.agentRowCopy}>
                        <View style={styles.agentRowStatus}>
                          <View
                            style={[
                              styles.agentRowStatusDot,
                              { backgroundColor: statusColor(item.status) },
                            ]}
                          />
                          <Text style={styles.agentRowStatusText}>
                            {getStatusLabel(item.status)}
                          </Text>
                        </View>
                        <Text style={styles.agentRowTitle} numberOfLines={1}>
                          {resolveAgentName(item, item.key, agentAliases)}
                        </Text>
                        <Text style={styles.agentRowMeta} numberOfLines={1}>
                          {item.serverName}{item.project ? ` · ${item.project}` : ` · ${item.id}`}
                        </Text>
                      </View>

                      <View style={styles.agentRowBadges}>
                        {isPinned ? <Badge label="Pinned" active={false} /> : null}
                        {isActive ? (
                          <Badge label="Current" active />
                        ) : isOpen ? (
                          <Badge label="Open" active={false} />
                        ) : null}
                      </View>
                    </TouchableOpacity>
                  );
                })
              )}
            </ScrollView>
          </View>
        </View>
      </Modal>

      <Modal
        visible={menuVisible}
        transparent
        animationType="none"
        onRequestClose={closeMenu}
      >
        <View style={styles.popoverRoot}>
          <TouchableOpacity
            style={styles.popoverBackdrop}
            activeOpacity={1}
            onPress={closeMenu}
          />

          <View
            style={[
              styles.menuPopover,
              {
                left: menuPosition.left,
                top: menuPosition.top,
                width: MENU_POPOVER_WIDTH,
              },
            ]}
          >
            <MenuAction
              label="Rename"
              onPress={openRenameModal}
            />
            <MenuAction
              label={activePinned ? 'Unpin Tab' : 'Pin Tab'}
              onPress={handleTogglePinned}
            />
            <MenuAction
              label="Close Other Tabs"
              onPress={handleCloseOtherTabs}
              disabled={tabs.length <= 1}
            />
            <MenuAction
              label="Close Tab"
              onPress={handleCloseCurrentTab}
            />
          </View>
        </View>
      </Modal>

      <Modal
        visible={renameVisible}
        transparent
        animationType="fade"
        onRequestClose={() => setRenameVisible(false)}
      >
        <View style={styles.renameRoot}>
          <TouchableOpacity
            style={styles.modalBackdrop}
            activeOpacity={1}
            onPress={() => setRenameVisible(false)}
          />

          <View style={styles.renameCard}>
            <Text style={styles.renameTitle}>Rename Session</Text>
            <Text style={styles.renameHint}>Only changes the local display name on this device.</Text>
            <TextInput
              style={styles.renameInput}
              value={renameDraft}
              onChangeText={setRenameDraft}
              placeholder={agent?.name || agentId}
              placeholderTextColor="#6E7D90"
              autoFocus
              autoCorrect={false}
              autoCapitalize="none"
              returnKeyType="done"
              onSubmitEditing={handleSaveRename}
            />
            <View style={styles.renameActions}>
              <TouchableOpacity
                style={styles.renameButton}
                onPress={() => setRenameVisible(false)}
                activeOpacity={0.84}
              >
                <Text style={styles.renameButtonText}>Cancel</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={[styles.renameButton, styles.renameButtonPrimary]}
                onPress={handleSaveRename}
                activeOpacity={0.84}
              >
                <Text style={[styles.renameButtonText, styles.renameButtonTextPrimary]}>Save</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

function Badge({ label, active }: { label: string; active: boolean }) {
  return (
    <View style={[styles.badge, active && styles.badgeActive]}>
      <Text style={[styles.badgeText, active && styles.badgeTextActive]}>{label}</Text>
    </View>
  );
}

function MenuAction({
  label,
  onPress,
  disabled = false,
}: {
  label: string;
  onPress: () => void;
  disabled?: boolean;
}) {
  return (
    <TouchableOpacity
      style={[styles.menuAction, disabled && styles.menuActionDisabled]}
      onPress={onPress}
      disabled={disabled}
      activeOpacity={0.84}
    >
      <Text style={[styles.menuActionText, disabled && styles.menuActionTextDisabled]}>
        {label}
      </Text>
    </TouchableOpacity>
  );
}

function buildDisplayTabOrder(currentId: string | null | undefined, tabs: StoredTerminalTabs): string[] {
  if (!currentId) return tabs.order;
  return tabs.order.includes(currentId) ? tabs.order : [...tabs.order, currentId];
}

function buildMenuPosition(
  anchor: { x: number; y: number; width: number; height: number } | null,
  windowWidth: number,
): { left: number; top: number } {
  const top = Math.max(12, (anchor?.y ?? 12) + (anchor?.height ?? 38) + 16);
  const preferredLeft = (anchor?.x ?? windowWidth - 14) + (anchor?.width ?? 0) - MENU_POPOVER_WIDTH;
  const maxLeft = Math.max(12, windowWidth - MENU_POPOVER_WIDTH - 12);

  return {
    left: clamp(preferredLeft, 12, maxLeft),
    top,
  };
}

function getStatusLabel(status: AgentStatus): string {
  switch (status) {
    case 'running':
      return 'Running';
    case 'blocked':
      return 'Needs Input';
    case 'failed':
      return 'Error';
    case 'done':
      return 'Done';
    case 'unknown':
      return 'Waiting';
  }
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function pickNextTabAfterClose(
  closedId: string,
  currentTabs: StoredTerminalTabs,
  nextTabs: StoredTerminalTabs,
): string | null {
  const currentOrder = buildDisplayTabOrder(null, currentTabs);
  const nextOrder = buildDisplayTabOrder(null, nextTabs);
  const closedIndex = currentOrder.indexOf(closedId);

  if (closedIndex === -1) return nextOrder[0] || null;

  return currentOrder[closedIndex + 1] || currentOrder[closedIndex - 1] || null;
}

function resolveAgentName(
  agent: Agent | undefined,
  sessionKey: string | null,
  aliases: StoredAgentAliases,
): string {
  if (sessionKey && aliases[sessionKey]) return aliases[sessionKey];
  if (agent?.name) return agent.name;
  if (sessionKey) {
    const parsed = parseSessionKey(sessionKey);
    if (parsed) return parsed.agentId;
  }
  return '';
}

function formatTabLabel(serverName: string, title: string): string {
  return `${serverName} · ${title}`;
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#0B1118',
  },
  topBar: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 10,
    paddingTop: 4,
    paddingBottom: 8,
    borderBottomWidth: 1,
    borderBottomColor: '#161E2A',
    backgroundColor: '#11161F',
  },
  chromeButton: {
    width: 38,
    height: 38,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: '#1B2230',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.05)',
  },
  tabScroller: {
    flex: 1,
    marginHorizontal: 8,
  },
  tabScrollerContent: {
    paddingRight: 2,
  },
  tabPill: {
    minWidth: 112,
    maxWidth: 176,
    height: 38,
    borderRadius: 13,
    paddingLeft: 10,
    paddingRight: 6,
    marginRight: 6,
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: '#262633',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.04)',
  },
  tabPillActive: {
    backgroundColor: '#5A5A67',
    borderColor: 'rgba(255,255,255,0.08)',
  },
  tabPillPinned: {
    shadowColor: '#5B9DFF',
    shadowOpacity: 0.12,
    shadowRadius: 5,
  },
  tabMainButton: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
  },
  tabStatusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    marginRight: 8,
  },
  tabLabel: {
    flex: 1,
    color: '#C6CDDA',
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
    marginRight: 6,
  },
  tabLabelActive: {
    color: '#F4F6FA',
  },
  tabMenuButton: {
    width: 24,
    height: 24,
    borderRadius: 8,
    alignItems: 'center',
    justifyContent: 'center',
  },
  output: {
    flex: 1,
    paddingTop: 4,
  },
  inputShell: {
    position: 'absolute',
    left: 0,
    right: 0,
    paddingHorizontal: 12,
    backgroundColor: 'transparent',
  },
  modalRoot: {
    flex: 1,
    justifyContent: 'flex-end',
  },
  popoverRoot: {
    flex: 1,
  },
  popoverBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: 'transparent',
  },
  modalBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: 'rgba(6, 8, 12, 0.58)',
  },
  sheetCard: {
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    paddingHorizontal: 18,
    paddingTop: 12,
    paddingBottom: 28,
    backgroundColor: '#121A25',
    borderTopWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
    maxHeight: '82%',
  },
  sheetHandle: {
    alignSelf: 'center',
    width: 42,
    height: 4,
    borderRadius: 2,
    backgroundColor: '#3A475B',
    marginBottom: 14,
  },
  sheetTitle: {
    color: Colors.textPrimary,
    fontSize: 20,
    fontFamily: Typography.uiFontMedium,
  },
  sheetSubtitle: {
    marginTop: 6,
    color: '#7D8CA0',
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  sheetScroll: {
    marginTop: 18,
  },
  sheetScrollContent: {
    paddingBottom: 8,
  },
  sheetEmpty: {
    color: '#7D8CA0',
    fontSize: 13,
    fontFamily: Typography.uiFont,
    paddingVertical: 12,
  },
  agentRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 14,
    paddingVertical: 14,
    marginBottom: 10,
    borderRadius: 18,
    backgroundColor: '#18222F',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.05)',
  },
  agentRowActive: {
    borderColor: 'rgba(91,157,255,0.38)',
    backgroundColor: '#1B2735',
  },
  agentRowCopy: {
    flex: 1,
    paddingRight: 12,
  },
  agentRowStatus: {
    flexDirection: 'row',
    alignItems: 'center',
    marginBottom: 8,
  },
  agentRowStatusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
    marginRight: 6,
  },
  agentRowStatusText: {
    color: '#8A98AA',
    fontSize: 10,
    fontFamily: Typography.uiFontMedium,
    textTransform: 'uppercase',
    letterSpacing: 0.5,
  },
  agentRowTitle: {
    color: Colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
  },
  agentRowMeta: {
    marginTop: 4,
    color: '#7D8CA0',
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  agentRowBadges: {
    alignItems: 'flex-end',
  },
  badge: {
    paddingHorizontal: 10,
    paddingVertical: 5,
    borderRadius: 999,
    backgroundColor: '#202A38',
    marginTop: 6,
  },
  badgeActive: {
    backgroundColor: 'rgba(91,157,255,0.18)',
    borderWidth: 1,
    borderColor: 'rgba(91,157,255,0.35)',
  },
  badgeText: {
    color: '#A4B0C2',
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
  },
  badgeTextActive: {
    color: '#B9D6FF',
  },
  menuPopover: {
    position: 'absolute',
    borderRadius: 14,
    paddingVertical: 4,
    backgroundColor: '#161F2B',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
    shadowColor: '#000',
    shadowOpacity: 0.2,
    shadowRadius: 8,
    elevation: 8,
  },
  menuAction: {
    minHeight: 38,
    justifyContent: 'center',
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  menuActionDisabled: {
    opacity: 0.52,
  },
  menuActionText: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
  menuActionTextDisabled: {
    color: '#556176',
  },
  renameRoot: {
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: 20,
  },
  renameCard: {
    borderRadius: 18,
    padding: 16,
    backgroundColor: '#161F2B',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
  },
  renameTitle: {
    color: Colors.textPrimary,
    fontSize: 17,
    fontFamily: Typography.uiFontMedium,
  },
  renameHint: {
    marginTop: 4,
    color: '#7D8CA0',
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  renameInput: {
    marginTop: 14,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: '#263345',
    backgroundColor: '#111923',
    color: Colors.textPrimary,
    paddingHorizontal: 12,
    paddingVertical: 11,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
  renameActions: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    marginTop: 14,
    gap: 10,
  },
  renameButton: {
    minWidth: 72,
    height: 38,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: '#202A38',
  },
  renameButtonPrimary: {
    backgroundColor: Colors.accent,
  },
  renameButtonText: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  renameButtonTextPrimary: {
    color: '#07111E',
  },
});
