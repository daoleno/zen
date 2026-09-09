export type MermaidViewBox = {
  width: number;
  height: number;
};

export function parseMermaidSvgSize(
  svg: string,
  fallback: MermaidViewBox,
): MermaidViewBox {
  const viewBox = /\bviewBox\s*=\s*["']\s*([0-9.+-eE]+)\s+([0-9.+-eE]+)\s+([0-9.+-eE]+)\s+([0-9.+-eE]+)\s*["']/i.exec(
    svg,
  );
  if (viewBox) {
    const width = Number(viewBox[3]);
    const height = Number(viewBox[4]);
    if (width > 0 && height > 0 && Number.isFinite(width) && Number.isFinite(height)) {
      return { width, height };
    }
  }
  const widthMatch = /\bwidth\s*=\s*["']([0-9.+-eE]+)(?:px)?["']/i.exec(svg);
  const heightMatch = /\bheight\s*=\s*["']([0-9.+-eE]+)(?:px)?["']/i.exec(svg);
  const width = widthMatch ? Number(widthMatch[1]) : fallback.width;
  const height = heightMatch ? Number(heightMatch[1]) : fallback.height;
  if (width > 0 && height > 0 && Number.isFinite(width) && Number.isFinite(height)) {
    return { width, height };
  }
  return fallback;
}

export function mermaidPreviewLayout(
  size: MermaidViewBox,
  containerWidth: number,
  maxHeight: number,
) {
  const width = Math.max(1, containerWidth);
  const scale = width / Math.max(size.width, 1);
  const fittedHeight = size.height * scale;
  const height = Math.min(Math.max(48, fittedHeight), maxHeight);
  const overflow = fittedHeight > maxHeight + 1;
  return {
    width,
    height,
    overflow,
    scale: height / Math.max(size.height, 1),
  };
}
