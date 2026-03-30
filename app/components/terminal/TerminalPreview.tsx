import React, { useEffect, useMemo, useRef, useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { Asset } from 'expo-asset';
import { WebView } from 'react-native-webview';
import { Colors, Typography } from '../../constants/tokens';
import {
  DefaultTerminalThemeName,
  resolveTerminalTheme,
  TerminalThemeName,
  TerminalThemePalette,
} from '../../constants/terminalThemes';
import { xtermCss, xtermFitAddonJs, xtermJs } from './xtermAssets';

interface TerminalPreviewProps {
  lines: string[];
  themeName?: TerminalThemeName;
}

const PREVIEW_FONT_SIZE = 9;
const PREVIEW_LINE_HEIGHT = 1.2;

let cachedFontUri: string | null = null;

export function TerminalPreview({ lines, themeName = DefaultTerminalThemeName }: TerminalPreviewProps) {
  const webviewRef = useRef<WebView>(null);
  const mountedRef = useRef(true);
  const [fontUri, setFontUri] = useState<string | null>(cachedFontUri);
  const [ready, setReady] = useState(false);
  const theme = useMemo(() => resolveTerminalTheme(themeName), [themeName]);

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  useEffect(() => {
    if (cachedFontUri) {
      setFontUri(cachedFontUri);
      return;
    }
    let cancelled = false;
    (async () => {
      const asset = Asset.fromModule(require('../../assets/fonts/MapleMono-CN-Regular.ttf'));
      if (!asset.localUri) await asset.downloadAsync();
      if (!cancelled && mountedRef.current) {
        cachedFontUri = asset.localUri ?? asset.uri;
        setFontUri(cachedFontUri);
      }
    })();
    return () => { cancelled = true; };
  }, []);

  const html = useMemo(() => buildPreviewHtml(theme, fontUri), [theme, fontUri]);

  useEffect(() => {
    if (!ready || !mountedRef.current || !webviewRef.current) return;
    const data = lines.join('\r\n');
    const script = `window.__zenPreviewWrite(${JSON.stringify(data)}); true;`;
    webviewRef.current.injectJavaScript(script);
  }, [ready, lines]);

  return (
    <View style={[styles.container, { backgroundColor: theme.background }]}>
      <WebView
        ref={webviewRef}
        originWhitelist={['*']}
        source={{ html, baseUrl: 'https://zen.local/' }}
        onMessage={(event) => {
          try {
            const msg = JSON.parse(event.nativeEvent.data);
            if (msg.type === 'ready') setReady(true);
          } catch {}
        }}
        javaScriptEnabled
        scrollEnabled={false}
        bounces={false}
        overScrollMode="never"
        style={[styles.webview, { backgroundColor: theme.background }]}
        pointerEvents="none"
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    borderRadius: 8,
    overflow: 'hidden',
  },
  webview: {
    flex: 1,
    opacity: 1,
  },
});

function buildPreviewHtml(theme: TerminalThemePalette, fontUri: string | null) {
  const fontFace = fontUri
    ? `@font-face {
        font-family: 'ZenTerm';
        src: url('${fontUri}') format('truetype');
        font-display: swap;
      }`
    : '';
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no"/>
  <style>
    ${xtermCss}
    ${fontFace}
    html,body{margin:0;padding:0;height:100%;width:100%;background:${theme.background};overflow:hidden;overscroll-behavior:none}
    #terminal{height:100%;width:100%;box-sizing:border-box}
    .xterm-viewport{overflow-y:hidden!important}
    .xterm-viewport::-webkit-scrollbar{display:none}
    .xterm-cursor-layer{display:none!important}
  </style>
</head>
<body>
  <div id="terminal"></div>
  <script>${xtermJs}</script>
  <script>${xtermFitAddonJs}</script>
  <script>
    const terminal = new Terminal({
      convertEol: false,
      cursorBlink: false,
      cursorStyle: 'bar',
      cursorWidth: 0,
      disableStdin: true,
      allowTransparency: false,
      fontFamily: ${JSON.stringify(fontUri ? 'ZenTerm, monospace' : 'monospace')},
      fontSize: ${PREVIEW_FONT_SIZE},
      lineHeight: ${PREVIEW_LINE_HEIGHT},
      letterSpacing: 0,
      theme: ${JSON.stringify(theme)},
      scrollback: 0
    });
    const fitAddon = new FitAddon.FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(document.getElementById('terminal'));

    window.__zenPreviewWrite = function(data) {
      terminal.clear();
      terminal.write(data);
    };

    setTimeout(function() {
      fitAddon.fit();
      window.ReactNativeWebView.postMessage(JSON.stringify({type:'ready'}));
    }, 0);
    window.addEventListener('resize', function() { fitAddon.fit(); });
  </script>
</body>
</html>`;
}
