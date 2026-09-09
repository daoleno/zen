/** Exact user Perpetuo single-domain architecture diagram, plus small relatives. */

export const PERPETUO_SINGLE_DOMAIN_FLOWCHART = `flowchart TB
    subgraph Browser["用户浏览器 · 地址栏始终是 perpetuo.freeride-ai.io"]
        Shell["可信主界面<br/>Chat / Workspace / 应用页面"]
        Frame["隔离的 Mini App iframe<br/>通过 srcdoc 装入完整应用<br/>无 allow-same-origin：不继承主站身份<br/>CSP 禁止外部请求、表单提交等能力"]
        Shell -->|"⑤ 装入新版本；就绪后替换旧预览"| Frame
        Frame -.->|"仅回报 ready / error<br/>校验窗口、应用、版本和本次加载标识"| Shell
    end

    subgraph Entry["唯一公网入口 · HTTPS 主站"]
        Proxy["现有 Cloudflare / Caddy<br/>网页、API、WebSocket"]
        Server["Perpetuo Server<br/>登录认证 / 应用归属 / 版本管理"]
        Compose["应用文档组装器<br/>HTML + JS + CSS + 图片等内置资源<br/>加入 CSP 与运行状态通信"]
        Proxy --> Server
        Server -->|"④ 读取已成功构建的指定版本"| Compose
    end

    subgraph Backend["服务端内部 · 不直接暴露给浏览器"]
        Harness["Agent / Harness<br/>生成和修改源码"]
        Build["隔离构建容器<br/>完整文件阶段触发构建<br/>不是执行未完成的代码片段"]
        Files[("持久文件存储<br/>源码 / 不可变版本 / 构建产物")]
        DB[("数据库<br/>用户与应用归属<br/>预览版本 / 已发布版本")]

        Harness -->|"② 写入源码"| Files
        Harness -->|"阶段性构建"| Build
        Build -->|"③ 成功后保存新版本"| Files
        Build -->|"更新构建状态与预览版本"| DB
    end

    Shell -->|"① 聊天生成，或点击已有应用"| Proxy
    Server -->|"发起生成任务"| Harness
    Server <-->|"读取 / 更新元数据"| DB
    Compose <-->|"读取版本文件"| Files
    Compose -->|"通过主站 API 返回完整文档 JSON<br/>不返回另一个域名的网页地址"| Shell
    Server -.->|"构建状态通知 / 状态同步<br/>触发下一次预览加载"| Shell

    Shell -->|"⑥ Publish / Update<br/>将选定版本设为已发布版本"| Proxy
`;

export const SIMPLE_FLOWCHART_TD = `flowchart TD
  Start[开始] --> Decision{Ready?}
  Decision -->|Yes| Done[完成]
  Decision -->|No| Wait[等待]
`;

export const SIMPLE_FLOWCHART_LR = `flowchart LR
  Input --> Process --> Output
`;

export const MERMAID_MARKDOWN_FIXTURE = `Architecture notes.

\`\`\`mermaid
${PERPETUO_SINGLE_DOMAIN_FLOWCHART.trim()}
\`\`\`

A second diagram:

\`\`\`mermaid
${SIMPLE_FLOWCHART_TD.trim()}
\`\`\`

Plain \`\`\`ts code stays code:

\`\`\`ts
const fence = "\`\`\`mermaid";
export const ok = true;
\`\`\`
`;
