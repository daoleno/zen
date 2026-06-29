type SyntaxLanguage =
  | "c"
  | "css"
  | "go"
  | "html"
  | "java"
  | "javascript"
  | "json"
  | "kotlin"
  | "markdown"
  | "php"
  | "python"
  | "ruby"
  | "rust"
  | "shell"
  | "sql"
  | "swift"
  | "toml"
  | "typescript"
  | "yaml"
  | "plain";

export type HighlightTokenKind =
  | "attribute"
  | "comment"
  | "constant"
  | "function"
  | "keyword"
  | "number"
  | "operator"
  | "plain"
  | "property"
  | "punctuation"
  | "string"
  | "tag";

export interface HighlightSegment {
  text: string;
  kind: HighlightTokenKind;
}

export function highlightCodeLine(line: string, path: string): HighlightSegment[] {
  const language = detectSyntaxLanguage(path);
  return highlightCodeLineWithSyntaxLanguage(line, language);
}

export function highlightCodeLineForLanguage(
  line: string,
  language: string | undefined,
): HighlightSegment[] {
  return highlightCodeLineWithSyntaxLanguage(line, detectSyntaxLanguageForFence(language));
}

function highlightCodeLineWithSyntaxLanguage(
  line: string,
  language: SyntaxLanguage,
): HighlightSegment[] {
  if (language === "plain") {
    return [{ text: line, kind: "plain" }];
  }
  if (
    (language === "javascript" || language === "typescript")
    && /^\s*<\/?[A-Za-z]/.test(line)
  ) {
    return highlightMarkupLine(line);
  }
  if (language === "html") {
    return highlightMarkupLine(line);
  }
  if (language === "markdown") {
    return highlightMarkdownLine(line);
  }
  if (language === "yaml" || language === "toml") {
    return highlightConfigLine(line, language);
  }
  return highlightGenericLine(line, language);
}

function detectSyntaxLanguageForFence(language: string | undefined): SyntaxLanguage {
  const normalized = (language || "").trim().toLowerCase();
  if (!normalized) {
    return "plain";
  }

  switch (normalized) {
    case "bash":
    case "console":
    case "fish":
    case "sh":
    case "shell":
    case "terminal":
    case "zsh":
      return "shell";
    case "c++":
    case "cpp":
    case "cxx":
      return "c";
    case "golang":
      return "go";
    case "htm":
    case "svg":
    case "xml":
      return "html";
    case "javascript":
    case "jsx":
      return "javascript";
    case "jsonc":
      return "json";
    case "kt":
    case "kts":
      return "kotlin";
    case "md":
    case "mdx":
      return "markdown";
    case "py":
      return "python";
    case "rb":
      return "ruby";
    case "rs":
      return "rust";
    case "tsx":
    case "typescript":
      return "typescript";
    case "yml":
      return "yaml";
    case "ascii":
    case "art":
    case "plain":
    case "plaintext":
    case "text":
    case "txt":
      return "plain";
    default:
      return detectSyntaxLanguage(`snippet.${normalized}`);
  }
}

function detectSyntaxLanguage(path: string): SyntaxLanguage {
  const normalized = path.toLowerCase();
  const fileName = pathBaseName(normalized);
  const extension = fileName.includes(".") ? fileName.split(".").pop() || "" : "";

  if (fileName === "dockerfile" || fileName.endsWith(".dockerfile")) {
    return "shell";
  }
  if (fileName === "makefile" || fileName === "justfile") {
    return "shell";
  }
  if (fileName === "rakefile" || fileName === "gemfile") {
    return "ruby";
  }

  switch (extension) {
    case "c":
    case "cc":
    case "cpp":
    case "cxx":
    case "h":
    case "hpp":
    case "m":
    case "mm":
      return "c";
    case "css":
    case "less":
    case "scss":
      return "css";
    case "go":
      return "go";
    case "htm":
    case "html":
    case "svg":
    case "svelte":
    case "vue":
    case "xml":
      return "html";
    case "java":
      return "java";
    case "cjs":
    case "js":
    case "jsx":
    case "mjs":
      return "javascript";
    case "json":
    case "jsonc":
      return "json";
    case "kt":
    case "kts":
      return "kotlin";
    case "md":
    case "mdx":
      return "markdown";
    case "php":
      return "php";
    case "py":
    case "pyw":
      return "python";
    case "rb":
      return "ruby";
    case "rs":
      return "rust";
    case "bash":
    case "fish":
    case "sh":
    case "zsh":
      return "shell";
    case "sql":
      return "sql";
    case "swift":
      return "swift";
    case "toml":
      return "toml";
    case "ts":
    case "tsx":
      return "typescript";
    case "yaml":
    case "yml":
      return "yaml";
    default:
      return "plain";
  }
}

function highlightMarkupLine(line: string): HighlightSegment[] {
  if (line.trimStart().startsWith("<!--")) {
    return [{ text: line, kind: "comment" }];
  }

  const segments: HighlightSegment[] = [];
  const pattern = /(<!--.*?-->|<\/?[A-Za-z][\w:-]*|\s+[A-Za-z_:][\w:.-]*(?=\=)|"[^"]*"|'[^']*'|\/?>)/g;
  pushPatternSegments(line, pattern, segments, (token) => {
    if (token.startsWith("<!--")) return "comment";
    if (token.startsWith("<")) return "tag";
    if (token === ">" || token === "/>") return "punctuation";
    if (token.startsWith('"') || token.startsWith("'")) return "string";
    if (/^\s+[A-Za-z_:]/.test(token)) return "attribute";
    return "plain";
  });
  return segments;
}

function highlightMarkdownLine(line: string): HighlightSegment[] {
  if (/^\s*#{1,6}\s/.test(line)) {
    return [{ text: line, kind: "keyword" }];
  }
  if (/^\s*>/.test(line)) {
    return [{ text: line, kind: "comment" }];
  }
  if (/^\s*(```|~~~)/.test(line)) {
    return [{ text: line, kind: "string" }];
  }

  const segments: HighlightSegment[] = [];
  const pattern = /(`[^`]+`|\[[^\]]+\]\([^)]+\)|\*\*[^*]+\*\*|__[^_]+__)/g;
  pushPatternSegments(line, pattern, segments, (token) => {
    if (token.startsWith("`")) return "string";
    if (token.startsWith("[")) return "property";
    return "keyword";
  });
  return segments;
}

function highlightConfigLine(line: string, language: SyntaxLanguage): HighlightSegment[] {
  const keyMatch = line.match(/^(\s*)([A-Za-z0-9_.-]+)(\s*[:=])/);
  if (keyMatch) {
    const [, leading, key, delimiter] = keyMatch;
    return [
      ...(leading ? [{ text: leading, kind: "plain" as const }] : []),
      { text: key, kind: "property" },
      { text: delimiter, kind: "punctuation" },
      ...highlightGenericLine(line.slice(keyMatch[0].length), language),
    ];
  }
  return highlightGenericLine(line, language);
}

function highlightGenericLine(line: string, language: SyntaxLanguage): HighlightSegment[] {
  const segments: HighlightSegment[] = [];
  let index = 0;

  while (index < line.length) {
    const commentMarker = commentMarkerAt(line, index, language);
    if (commentMarker) {
      segments.push({ text: line.slice(index), kind: "comment" });
      break;
    }

    const char = line[index];

    if (char === '"' || char === "'" || char === "`") {
      const end = readQuotedString(line, index, char);
      const token = line.slice(index, end);
      const kind =
        language === "json" && nextNonSpaceChar(line, end) === ":"
          ? "property"
          : "string";
      segments.push({ text: token, kind });
      index = end;
      continue;
    }

    if (isNumberStart(line, index)) {
      const end = readNumber(line, index);
      segments.push({ text: line.slice(index, end), kind: "number" });
      index = end;
      continue;
    }

    if (isIdentifierStart(char)) {
      const end = readIdentifier(line, index);
      const token = line.slice(index, end);
      const previous = previousNonSpaceChar(line, index);
      const next = nextNonSpaceChar(line, end);
      const keywords = keywordSetForLanguage(language);
      let kind: HighlightTokenKind = "plain";
      if (keywords.has(token) || keywords.has(token.toLowerCase()) || keywords.has(token.toUpperCase())) {
        kind = "keyword";
      } else if (CONSTANT_WORDS.has(token)) {
        kind = "constant";
      } else if (next === "(") {
        kind = "function";
      } else if (previous === "." || next === ":") {
        kind = "property";
      }
      segments.push({ text: token, kind });
      index = end;
      continue;
    }

    if (isOperatorChar(char)) {
      segments.push({ text: char, kind: "operator" });
      index += 1;
      continue;
    }

    if (isPunctuationChar(char)) {
      segments.push({ text: char, kind: "punctuation" });
      index += 1;
      continue;
    }

    const nextSpecial = findNextTokenStart(line, index + 1, language);
    segments.push({ text: line.slice(index, nextSpecial), kind: "plain" });
    index = nextSpecial;
  }

  return segments.length ? segments : [{ text: line || " ", kind: "plain" }];
}

function pushPatternSegments(
  line: string,
  pattern: RegExp,
  segments: HighlightSegment[],
  kindForToken: (token: string) => HighlightTokenKind,
) {
  let lastIndex = 0;
  for (const match of line.matchAll(pattern)) {
    const index = match.index ?? 0;
    if (index > lastIndex) {
      segments.push({ text: line.slice(lastIndex, index), kind: "plain" });
    }
    segments.push({ text: match[0], kind: kindForToken(match[0]) });
    lastIndex = index + match[0].length;
  }
  if (lastIndex < line.length) {
    segments.push({ text: line.slice(lastIndex), kind: "plain" });
  }
  if (segments.length === 0) {
    segments.push({ text: line || " ", kind: "plain" });
  }
}

function commentMarkerAt(line: string, index: number, language: SyntaxLanguage): string | null {
  const rest = line.slice(index);
  if (rest.startsWith("/*")) return "/*";
  if (["c", "css", "go", "java", "javascript", "kotlin", "php", "rust", "swift", "typescript"].includes(language) && rest.startsWith("//")) {
    return "//";
  }
  if (["python", "ruby", "shell", "toml", "yaml"].includes(language) && rest.startsWith("#")) {
    return "#";
  }
  if (language === "sql" && rest.startsWith("--")) {
    return "--";
  }
  return null;
}

function readQuotedString(line: string, start: number, quote: string): number {
  let index = start + 1;
  while (index < line.length) {
    if (line[index] === "\\") {
      index += 2;
      continue;
    }
    if (line[index] === quote) {
      return index + 1;
    }
    index += 1;
  }
  return line.length;
}

function isNumberStart(line: string, index: number): boolean {
  const char = line[index];
  const next = line[index + 1];
  return /[0-9]/.test(char) || (char === "." && /[0-9]/.test(next || ""));
}

function readNumber(line: string, start: number): number {
  let index = start;
  while (index < line.length && /[0-9A-Fa-f_xXobOB.]/.test(line[index])) {
    index += 1;
  }
  return index;
}

function isIdentifierStart(char: string): boolean {
  return /[A-Za-z_$]/.test(char);
}

function readIdentifier(line: string, start: number): number {
  let index = start;
  while (index < line.length && /[A-Za-z0-9_$-]/.test(line[index])) {
    index += 1;
  }
  return index;
}

function previousNonSpaceChar(line: string, index: number): string {
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    if (!/\s/.test(line[cursor])) {
      return line[cursor];
    }
  }
  return "";
}

function nextNonSpaceChar(line: string, index: number): string {
  for (let cursor = index; cursor < line.length; cursor += 1) {
    if (!/\s/.test(line[cursor])) {
      return line[cursor];
    }
  }
  return "";
}

function findNextTokenStart(line: string, start: number, language: SyntaxLanguage): number {
  for (let index = start; index < line.length; index += 1) {
    const char = line[index];
    if (
      commentMarkerAt(line, index, language)
      || char === '"'
      || char === "'"
      || char === "`"
      || isNumberStart(line, index)
      || isIdentifierStart(char)
      || isOperatorChar(char)
      || isPunctuationChar(char)
    ) {
      return index;
    }
  }
  return line.length;
}

function isOperatorChar(char: string): boolean {
  return /[+\-*\/%=!<>|&^~?]/.test(char);
}

function isPunctuationChar(char: string): boolean {
  return /[{}[\]();,.]/.test(char);
}

function keywordSetForLanguage(language: SyntaxLanguage): Set<string> {
  switch (language) {
    case "c":
      return C_KEYWORDS;
    case "css":
      return CSS_KEYWORDS;
    case "go":
      return GO_KEYWORDS;
    case "java":
      return JAVA_KEYWORDS;
    case "javascript":
    case "json":
      return JS_KEYWORDS;
    case "kotlin":
      return KOTLIN_KEYWORDS;
    case "php":
      return PHP_KEYWORDS;
    case "python":
      return PYTHON_KEYWORDS;
    case "ruby":
      return RUBY_KEYWORDS;
    case "rust":
      return RUST_KEYWORDS;
    case "shell":
      return SHELL_KEYWORDS;
    case "sql":
      return SQL_KEYWORDS;
    case "swift":
      return SWIFT_KEYWORDS;
    case "typescript":
      return TS_KEYWORDS;
    default:
      return EMPTY_KEYWORDS;
  }
}

function pathBaseName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
}

const EMPTY_KEYWORDS = new Set<string>();
const CONSTANT_WORDS = new Set(["False", "None", "True", "false", "nil", "null", "true"]);
const C_KEYWORDS = new Set([
  "auto", "break", "case", "catch", "char", "class", "const", "constexpr", "continue", "default",
  "delete", "do", "double", "else", "enum", "extern", "float", "for", "friend", "goto", "if",
  "inline", "int", "long", "namespace", "new", "private", "protected", "public", "register",
  "return", "short", "signed", "sizeof", "static", "struct", "switch", "template", "this",
  "throw", "try", "typedef", "typename", "union", "unsigned", "using", "virtual", "void",
  "volatile", "while",
]);
const CSS_KEYWORDS = new Set(["from", "import", "important", "keyframes", "media", "supports", "to"]);
const GO_KEYWORDS = new Set([
  "break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough", "for",
  "func", "go", "goto", "if", "import", "interface", "map", "package", "range", "return", "select",
  "struct", "switch", "type", "var",
]);
const JAVA_KEYWORDS = new Set([
  "abstract", "assert", "boolean", "break", "byte", "case", "catch", "char", "class", "const",
  "continue", "default", "do", "double", "else", "enum", "extends", "final", "finally", "float",
  "for", "if", "implements", "import", "instanceof", "int", "interface", "long", "native", "new",
  "package", "private", "protected", "public", "return", "short", "static", "strictfp", "super",
  "switch", "synchronized", "this", "throw", "throws", "transient", "try", "void", "volatile",
  "while",
]);
const JS_KEYWORDS = new Set([
  "async", "await", "break", "case", "catch", "class", "const", "continue", "debugger", "default",
  "delete", "do", "else", "export", "extends", "finally", "for", "from", "function", "if",
  "import", "in", "instanceof", "let", "new", "of", "return", "static", "super", "switch", "this",
  "throw", "try", "typeof", "var", "void", "while", "with", "yield",
]);
const KOTLIN_KEYWORDS = new Set([
  "as", "break", "class", "continue", "data", "do", "else", "false", "for", "fun", "if", "in",
  "interface", "is", "null", "object", "override", "package", "private", "protected", "public",
  "return", "super", "this", "throw", "true", "try", "typealias", "val", "var", "when", "while",
]);
const PHP_KEYWORDS = new Set([
  "abstract", "and", "array", "as", "break", "case", "catch", "class", "clone", "const", "continue",
  "declare", "default", "die", "do", "echo", "else", "elseif", "empty", "endfor", "endforeach",
  "endif", "endswitch", "extends", "final", "finally", "fn", "for", "foreach", "function", "global",
  "if", "implements", "include", "interface", "isset", "namespace", "new", "or", "private",
  "protected", "public", "require", "return", "static", "switch", "throw", "trait", "try", "use",
  "var", "while", "xor", "yield",
]);
const PYTHON_KEYWORDS = new Set([
  "False", "None", "True", "and", "as", "assert", "async", "await", "break", "class", "continue",
  "def", "del", "elif", "else", "except", "finally", "for", "from", "global", "if", "import", "in",
  "is", "lambda", "nonlocal", "not", "or", "pass", "raise", "return", "try", "while", "with", "yield",
]);
const RUBY_KEYWORDS = new Set([
  "BEGIN", "END", "alias", "and", "begin", "break", "case", "class", "def", "defined", "do", "else",
  "elsif", "end", "ensure", "false", "for", "if", "in", "module", "next", "nil", "not", "or", "redo",
  "rescue", "retry", "return", "self", "super", "then", "true", "undef", "unless", "until", "when",
  "while", "yield",
]);
const RUST_KEYWORDS = new Set([
  "as", "async", "await", "break", "const", "continue", "crate", "dyn", "else", "enum", "extern",
  "false", "fn", "for", "if", "impl", "in", "let", "loop", "match", "mod", "move", "mut", "pub",
  "ref", "return", "self", "static", "struct", "super", "trait", "true", "type", "unsafe", "use",
  "where", "while",
]);
const SHELL_KEYWORDS = new Set([
  "case", "do", "done", "elif", "else", "esac", "fi", "for", "function", "if", "in", "select",
  "then", "until", "while",
]);
const SQL_KEYWORDS = new Set([
  "ADD", "ALTER", "AND", "AS", "ASC", "BEGIN", "BY", "CASE", "CREATE", "DELETE", "DESC", "DISTINCT",
  "DROP", "ELSE", "END", "EXISTS", "FROM", "GROUP", "HAVING", "IN", "INDEX", "INNER", "INSERT",
  "INTO", "IS", "JOIN", "LEFT", "LIKE", "LIMIT", "NOT", "NULL", "ON", "OR", "ORDER", "OUTER",
  "PRIMARY", "RIGHT", "SELECT", "SET", "TABLE", "THEN", "UPDATE", "VALUES", "WHEN", "WHERE",
]);
const SWIFT_KEYWORDS = new Set([
  "Any", "Self", "Type", "as", "associatedtype", "break", "case", "catch", "class", "continue",
  "default", "defer", "deinit", "do", "else", "enum", "extension", "fallthrough", "false", "fileprivate",
  "final", "for", "func", "guard", "if", "import", "in", "init", "inout", "internal", "is", "let",
  "nil", "open", "operator", "private", "protocol", "public", "repeat", "return", "self", "static",
  "struct", "subscript", "super", "switch", "throw", "throws", "true", "try", "typealias", "var",
  "where", "while",
]);
const TS_KEYWORDS = new Set([
  ...Array.from(JS_KEYWORDS),
  "abstract", "any", "as", "boolean", "declare", "enum", "implements", "interface", "keyof",
  "module", "namespace", "never", "number", "private", "protected", "public", "readonly", "string",
  "type", "unknown",
]);
