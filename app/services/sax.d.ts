declare module "sax" {
  interface Attribute { name: string; local: string; uri: string; value: string }
  interface Element { local: string; uri: string; attributes: Record<string, Attribute> }
  interface Parser {
    ondoctype: (() => void) | null;
    onprocessinginstruction: ((instruction: { name: string }) => void) | null;
    onopentag: ((node: Element) => void) | null;
    onclosetag: (() => void) | null;
    write(xml: string): Parser;
    close(): Parser;
  }
  const sax: { parser(strict: boolean, options: { xmlns: boolean; strictEntities: boolean; maxEntityCount: number }): Parser };
  export default sax;
}
