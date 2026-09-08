// Execute the actual TypeScript model/tests without Node's WASM-based TS loader.
const fs = require("node:fs");
const path = require("node:path");
const ts = require("typescript");
const root = path.resolve(__dirname, "..");
const files = new Set([
  path.join(root, "app/services/remoteDesktopCommands.ts"),
  path.join(root, "app/services/remoteDesktopCommands.test.ts"),
]);
require.extensions[".ts"] = (module, filename) => {
  if (!files.has(filename)) throw new Error(`Unexpected test dependency: ${filename}`);
  const result = ts.transpileModule(fs.readFileSync(filename, "utf8"), {
    fileName: filename,
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS, esModuleInterop: true },
    reportDiagnostics: true,
  });
  if (result.diagnostics?.some((diagnostic) => diagnostic.category === ts.DiagnosticCategory.Error)) {
    throw new Error(ts.formatDiagnosticsWithColorAndContext(result.diagnostics, {
      getCanonicalFileName: (file) => file, getCurrentDirectory: () => root, getNewLine: () => "\n",
    }));
  }
  module._compile(result.outputText, filename);
};
process.on("exit", () => console.log(`# peakRSSKiB=${process.resourceUsage().maxRSS}; single-process JITless behavior runner`));
require(path.join(root, "app/services/remoteDesktopCommands.test.ts"));
