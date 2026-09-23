import sax from "sax";

export const MAX_SVG_BYTES = 2 << 20;
const DRAWING_TAGS = new Set([
  "svg", "g", "defs", "symbol", "use", "path", "rect", "circle", "ellipse",
  "line", "polyline", "polygon", "text", "tspan", "textPath", "clipPath",
  "mask", "pattern", "linearGradient", "radialGradient", "stop", "marker",
  "filter", "feBlend", "feColorMatrix", "feComponentTransfer", "feComposite",
  "feConvolveMatrix", "feDiffuseLighting", "feDisplacementMap", "feDistantLight",
  "feDropShadow", "feFlood", "feGaussianBlur", "feMerge", "feMergeNode",
  "feMorphology", "feOffset", "fePointLight", "feSpecularLighting", "feTile",
  "feTurbulence",
]);

export function validateSvgPreview(xml: string): { xml: string; ratio: number } {
  if (new TextEncoder().encode(xml).length > MAX_SVG_BYTES) throw new Error("SVG exceeds the preview limit.");
  let count = 0;
  let depth = 0;
  let width = 0;
  let height = 0;
  let roots = 0;
  const parser = sax.parser(true, { xmlns: true, strictEntities: true, maxEntityCount: 100 });
  parser.ondoctype = () => { throw new Error("SVG document declarations are unavailable."); };
  parser.onprocessinginstruction = (instruction) => {
    if (instruction.name.toLowerCase() !== "xml" || count || roots) throw new Error("Unsupported SVG content.");
  };
  parser.onopentag = (node) => {
    if (++count > 10000 || ++depth > 64 || !DRAWING_TAGS.has(node.local) || node.uri !== "http://www.w3.org/2000/svg") throw new Error("Unsupported SVG content.");
    if (depth === 1) {
      if (++roots !== 1 || node.local !== "svg") throw new Error("Unsupported SVG content.");
      const box = node.attributes.viewBox?.value.trim().split(/[\s,]+/).map(Number);
      width = box?.length === 4 ? box[2] : Number(node.attributes.width?.value);
      height = box?.length === 4 ? box[3] : Number(node.attributes.height?.value);
    }
    for (const attribute of Object.values(node.attributes)) {
      const { name, local, value } = attribute;
      if (/^on[a-z]/i.test(local) || local === "dangerouslySetInnerHTML" || local === "style" || (attribute.uri && attribute.uri !== "http://www.w3.org/1999/xlink" && attribute.uri !== "http://www.w3.org/2000/svg" && attribute.uri !== "http://www.w3.org/2000/xmlns/")) throw new Error("Unsupported SVG content.");
      if ((local === "href" || name === "xlink:href") && !/^#[\w.-]+$/.test(value)) throw new Error("External SVG references are unavailable.");
      if (/url\s*\(/i.test(value)) {
        const refs = [...value.matchAll(/url\s*\(\s*(['"]?)(.*?)\1\s*\)/gi)];
        if (!refs.length || refs.some((match) => !/^#[\w.-]+$/.test(match[2]))) throw new Error("External SVG references are unavailable.");
      }
      if (/(?:https?:|file:|content:|data:|javascript:)/i.test(value) && name !== "xmlns" && !name.startsWith("xmlns:")) throw new Error("External SVG references are unavailable.");
    }
  };
  parser.onclosetag = () => { depth--; };
  parser.write(xml).close();
  if (!count || !Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0 || width * height > 40_000_000) throw new Error("SVG dimensions are unavailable or exceed the preview limit.");
  return { xml, ratio: width / height };
}
