import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import ts from "typescript";

const source = readFileSync(new URL("./PrimaryDrawerPanel.tsx", import.meta.url), "utf8");
const file = ts.createSourceFile("PrimaryDrawerPanel.tsx", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);

function descendants(node: ts.Node): ts.Node[] {
  const children: ts.Node[] = [];
  node.forEachChild((child) => { children.push(child, ...descendants(child)); });
  return children;
}

const nodes = descendants(file);
const scroll = nodes.find((node): node is ts.JsxElement =>
  ts.isJsxElement(node) && node.openingElement.tagName.getText(file) === "ScrollView",
)!;
const footer = nodes.find((node): node is ts.JsxElement =>
  ts.isJsxElement(node) && node.openingElement.attributes.getText(file).includes("styles.drawerFooter"),
)!;

function rows(node: ts.Node) {
  return descendants(node).filter((child): child is ts.JsxSelfClosingElement =>
    ts.isJsxSelfClosingElement(child) && child.tagName.getText(file) === "DrawerRow",
  );
}

function label(node: ts.JsxSelfClosingElement) {
  const prop = node.attributes.properties.find((attr): attr is ts.JsxAttribute =>
    ts.isJsxAttribute(attr) && attr.name.getText(file) === "label",
  );
  return prop?.initializer && ts.isStringLiteral(prop.initializer) ? prop.initializer.text : null;
}

describe("primary drawer Settings footer", () => {
  test("keeps feature order and places only Settings outside the scroll region", () => {
    expect(scroll).toBeDefined();
    expect(footer).toBeDefined();
    expect(rows(scroll).map(label)).toEqual(["Skills", "Stats", "Remote Desktop"]);
    expect(rows(footer).map(label)).toEqual(["Settings"]);
    expect(rows(file).map(label)).toEqual(["Skills", "Stats", "Remote Desktop", "Settings"]);
    expect(scroll.end).toBeLessThan(footer.pos);
    expect(footer.getText(file)).toContain("Zen v{appVersion}");
  });

  test("preserves Settings navigation and closed-drawer keyboard semantics", () => {
    const settings = rows(footer)[0].getText(file);
    expect(settings).toContain('onPress={() => openRoute("/settings")}');
    expect(settings).toContain("drawerVisible={drawerVisible}");
    expect(source).toContain("onNavigateAway();\n      router.push(pathname);");
    expect(source).toContain('accessibilityRole="button"');
    expect(source).toContain("accessibilityLabel={label}");
    expect(source).toContain("tabIndex={drawerVisible ? 0 : -1}");
    expect(source).toContain('name="settings-outline"');
  });

  test("reserves footer space within safe area instead of overlaying scroll content", () => {
    const stylesCall = nodes.find((node): node is ts.CallExpression =>
      ts.isCallExpression(node) && node.expression.getText(file) === "StyleSheet.create",
    )!;
    const styles = stylesCall.arguments[0] as ts.ObjectLiteralExpression;
    const style = (name: string) => (styles.properties.find((node) => node.name?.getText(file) === name) as ts.PropertyAssignment).initializer.getText(file);
    expect(style("drawerScroll")).toContain("flex: 1");
    expect(style("drawerScroll")).toContain("minHeight: 0");
    expect(style("drawerFooter")).toContain("flexShrink: 0");
    expect(style("drawerFooter")).not.toContain("absolute");
    expect(style("drawerRow")).toContain("minHeight: 58");
    expect(source).toContain('edges={["top", "bottom"]}');
    expect(footer.openingElement.getText(file)).toContain("colors.borderSubtle");
  });
});
