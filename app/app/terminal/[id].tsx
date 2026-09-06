import React, { useEffect } from 'react';
import { useIsFocused, useLocalSearchParams, useRouter } from 'expo-router';
import { useCurrentServer } from '../../store/currentServer';

let TerminalScreenImpl: React.ComponentType | null = null;

function getTerminalScreenImpl(): React.ComponentType {
  if (!TerminalScreenImpl) {
    TerminalScreenImpl = require('../../components/terminal/screen/TerminalScreenImpl').default as React.ComponentType;
  }
  return TerminalScreenImpl;
}

export default function TerminalScreenRoute() {
  const { hydrated, currentServerId, isCurrentServer } = useCurrentServer();
  const focused = useIsFocused();
  const router = useRouter();
  const params = useLocalSearchParams<{ serverId?: string | string[] }>();
  const serverId = Array.isArray(params.serverId) ? params.serverId[0] : params.serverId;
  useEffect(() => {
    if (focused && hydrated && !isCurrentServer(serverId)) router.replace('/(primary)/list');
  }, [focused, hydrated, currentServerId, isCurrentServer, router, serverId]);
  if (!hydrated || !isCurrentServer(serverId)) return null;
  const Screen = getTerminalScreenImpl();
  return <Screen />;
}
