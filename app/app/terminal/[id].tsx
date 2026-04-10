import React, { useEffect, useState } from 'react';
import { ActivityIndicator, View } from 'react-native';

let TerminalScreenImpl: React.ComponentType | null = null;

function getTerminalScreenImpl(): React.ComponentType {
  if (!TerminalScreenImpl) {
    TerminalScreenImpl = require('./TerminalScreenImpl').default as React.ComponentType;
  }
  return TerminalScreenImpl;
}

export default function TerminalScreenRoute() {
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
          backgroundColor: '#0F0F14',
        }}
      >
        <ActivityIndicator color="#5B9DFF" />
      </View>
    );
  }

  const Screen = getTerminalScreenImpl();
  return <Screen />;
}
