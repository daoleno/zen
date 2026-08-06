import React from 'react';

let TerminalScreenImpl: React.ComponentType | null = null;

function getTerminalScreenImpl(): React.ComponentType {
  if (!TerminalScreenImpl) {
    TerminalScreenImpl = require('../../components/terminal/screen/TerminalScreenImpl').default as React.ComponentType;
  }
  return TerminalScreenImpl;
}

export default function TerminalScreenRoute() {
  const Screen = getTerminalScreenImpl();
  return <Screen />;
}
