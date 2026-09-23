import React, { useEffect, useState } from "react";
import { Image, StyleSheet, View } from "react-native";
import { SvgXml } from "react-native-svg";
import type { SessionFileBinarySource } from "../../services/sessionFilePreview";
import { loadSvgPreview } from "../../services/svgPreview";

export function ZenImageContent({ source, svg, onLoad, onError }: {
  source: SessionFileBinarySource;
  svg: boolean;
  onLoad?(ratio: number): void;
  onError(): void;
}) {
  const [vector, setVector] = useState<{ key: string; xml: string; ratio: number } | null>(null);
  const headers = JSON.stringify(source.headers);
  const sourceKey = JSON.stringify([source.uri, headers]);
  useEffect(() => {
    if (!svg) return;
    const controller = new AbortController();
    setVector(null);
    void loadSvgPreview(source, controller.signal).then(
      (result) => { if (!controller.signal.aborted) setVector({ ...result, key: sourceKey }); },
      () => { if (!controller.signal.aborted) onError(); },
    );
    return () => controller.abort();
  }, [sourceKey, svg]);
  const currentVector = svg && vector?.key === sourceKey ? vector : null;
  useEffect(() => { if (currentVector) onLoad?.(currentVector.ratio); }, [currentVector]);
  if (svg) return currentVector ? <View style={styles.canvas}><SvgXml xml={currentVector.xml} width="100%" height="100%" onError={onError} /></View> : null;
  return <Image source={source} resizeMode="contain" resizeMethod="resize" style={StyleSheet.absoluteFill}
    onLoad={(event) => { const { width, height } = event.nativeEvent.source; onLoad?.(width > 0 && height > 0 ? width / height : 1); }} onError={onError} />;
}

const styles = StyleSheet.create({ canvas: { position: "absolute", top: 0, left: 0, right: 0, bottom: 0, alignItems: "center", justifyContent: "center", backgroundColor: "#454545" } });
