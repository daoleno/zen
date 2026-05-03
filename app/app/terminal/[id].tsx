import React, { useEffect, useState } from 'react';
import { ActivityIndicator, View } from 'react-native';
import { useAppColors } from '../../constants/tokens';

let TerminalScreenImpl: React.ComponentType | null = null;

function getTerminalScreenImpl(): React.ComponentType {
  if (!TerminalScreenImpl) {
    TerminalScreenImpl = require('./TerminalScreenImpl').default as React.ComponentType;
  }
  return TerminalScreenImpl;
}

export default function TerminalScreenRoute() {
  const colors = useAppColors();
  const [ready, setReady] = useState(false);

  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      setReady(true);
    });
    return () => {
      cancelAnimationFrame(frame);
    };
  }, []);

  if (!ready) {
    return (
      <View
        style={{
          flex: 1,
          alignItems: 'center',
          justifyContent: 'center',
          backgroundColor: colors.bgPrimary,
        }}
      >
        <ActivityIndicator color={colors.accent} />
      </View>
    );
  }

  const Screen = getTerminalScreenImpl();
  return <Screen />;
}
