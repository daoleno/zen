import type { Agent } from '../store/agents';
import { commandBinary } from './agentCommands';

export type TerminalFlavor =
  | 'shell'
  | 'ssh'
  | 'go'
  | 'rust'
  | 'python'
  | 'node'
  | 'typescript'
  | 'bun'
  | 'docker'
  | 'kubernetes'
  | 'postgres'
  | 'redis'
  | 'nginx'
  | 'git'
  | 'java'
  | 'ruby'
  | 'php'
  | 'terraform'
  | 'aws'
  | 'bash'
  | 'linux';

type FlavorSignal = {
  flavor: TerminalFlavor;
  weight: number;
};

type AgentFlavorSource = Pick<
  Agent,
  'name' | 'project' | 'cwd' | 'command' | 'summary' | 'last_output_lines'
>;

export function detectTerminalFlavor(
  agent: AgentFlavorSource,
): TerminalFlavor {
  const signals: FlavorSignal[] = [];
  const command = normalize(agent.command);
  const name = normalize(agent.name);
  const project = normalize(agent.project);
  const cwd = normalize(agent.cwd);
  const binary = commandBinary(command);
  const recentOutput = (agent.last_output_lines || []).slice(-6).join('\n').toLowerCase();
  const context = [
    command,
    name,
    project,
    cwd,
    normalize(agent.summary),
    recentOutput,
  ]
    .join(' ')
    .toLowerCase();

  if (binary === 'ssh' || /\bssh\b/.test(command) || /^ssh[- ]/.test(name)) {
    signals.push({ flavor: 'ssh', weight: 12 });
  }
  if (
    binary === 'go' ||
    /\bgo (run|test|build|mod|install|fmt|vet)\b/.test(command) ||
    /\bgo\.mod\b/.test(context) ||
    /\bFAIL\s+\S+\s+\[/i.test(recentOutput) ||
    /\bok\s+\S+\s+\d/i.test(recentOutput)
  ) {
    signals.push({ flavor: 'go', weight: 11 });
  }
  if (
    binary === 'cargo' ||
    binary === 'rustc' ||
    /\brustc?\b/.test(context) ||
    /\bcargo (run|test|build|check)\b/.test(command) ||
    /\bCompiling\s+\S+\s+v[\d.]+/i.test(recentOutput)
  ) {
    signals.push({ flavor: 'rust', weight: 11 });
  }
  if (
    binary === 'python' ||
    binary === 'python3' ||
    binary === 'pip' ||
    binary === 'pip3' ||
    binary === 'poetry' ||
    /\bpy(?:thon)?3?\b/.test(command) ||
    /\bTraceback \(most recent call last\):/i.test(recentOutput)
  ) {
    signals.push({ flavor: 'python', weight: 10 });
  }
  if (binary === 'uv' && /\b(py|python|pip|venv)\b/.test(context)) {
    signals.push({ flavor: 'python', weight: 9 });
  }
  if (
    binary === 'tsc' ||
    binary === 'vite' ||
    binary === 'vitest' ||
    binary === 'typescript' ||
    /\btsx?\b/.test(command) && /\b(ts|typescript|vite|vitest)\b/.test(context)
  ) {
    signals.push({ flavor: 'typescript', weight: 10 });
  }
  if (
    binary === 'node' ||
    binary === 'npm' ||
    binary === 'npx' ||
    binary === 'yarn' ||
    binary === 'pnpm'
  ) {
    signals.push({ flavor: 'node', weight: 10 });
  }
  if (binary === 'bun' || binary === 'bunx' || /\bbun (run|test|install)\b/.test(command)) {
    signals.push({ flavor: 'bun', weight: 11 });
  }
  if (
    binary === 'docker' ||
    binary === 'podman' ||
    binary === 'docker-compose' ||
    (binary === 'compose' && /\b(docker|podman|container)\b/.test(context)) ||
    /\bdocker\b/.test(command)
  ) {
    signals.push({ flavor: 'docker', weight: 10 });
  }
  if (
    binary === 'kubectl' ||
    binary === 'k9s' ||
    binary === 'helm' ||
    binary === 'kubectx' ||
    /\bk8s\b/.test(context)
  ) {
    signals.push({ flavor: 'kubernetes', weight: 10 });
  }
  if (binary === 'terraform' || binary === 'tofu' || /\bterraform\b/.test(command)) {
    signals.push({ flavor: 'terraform', weight: 10 });
  }
  if (
    binary === 'aws' ||
    binary === 'sam' ||
    /\baws\s+(s3|ec2|lambda|sts|cloudformation)\b/.test(command)
  ) {
    signals.push({ flavor: 'aws', weight: 10 });
  }
  if (binary === 'psql' || /\bpostgres(?:ql)?\b/.test(context)) {
    signals.push({ flavor: 'postgres', weight: 9 });
  }
  if (
    binary === 'redis-cli' ||
    binary === 'redis-server' ||
    /\bredis-cli\b/.test(command) ||
    /^redis\b/.test(binary)
  ) {
    signals.push({ flavor: 'redis', weight: 9 });
  }
  if (binary === 'nginx' || /\bnginx\b/.test(context)) {
    signals.push({ flavor: 'nginx', weight: 9 });
  }
  if (
    binary === 'git' ||
    /\bgit (status|commit|push|pull|checkout|diff|log|rebase|merge)\b/.test(command)
  ) {
    signals.push({ flavor: 'git', weight: 8 });
  }
  if (
    binary === 'java' ||
    binary === 'mvn' ||
    binary === 'gradle' ||
    binary === 'javac' ||
    /\bjava\b/.test(command)
  ) {
    signals.push({ flavor: 'java', weight: 9 });
  }
  if (
    binary === 'ruby' ||
    binary === 'rails' ||
    binary === 'bundle' ||
    binary === 'rake' ||
    /\bruby\b/.test(command)
  ) {
    signals.push({ flavor: 'ruby', weight: 9 });
  }
  if (binary === 'php' || binary === 'artisan' || /\bphp\b/.test(command)) {
    signals.push({ flavor: 'php', weight: 9 });
  }
  if (
    binary === 'bash' ||
    binary === 'zsh' ||
    binary === 'fish' ||
    binary === 'sh' ||
    /\b(bash|zsh|fish)\b/.test(name)
  ) {
    signals.push({ flavor: 'bash', weight: 4 });
  }
  if (binary === 'linux' || /\b(ubuntu|debian|arch|fedora|nixos)\b/.test(context)) {
    signals.push({ flavor: 'linux', weight: 3 });
  }

  const best = signals.sort((left, right) => right.weight - left.weight)[0];
  return best?.flavor ?? 'shell';
}

export function terminalFlavorLabel(flavor: TerminalFlavor): string {
  switch (flavor) {
    case 'ssh':
      return 'SSH session';
    case 'go':
      return 'Go';
    case 'rust':
      return 'Rust';
    case 'python':
      return 'Python';
    case 'node':
      return 'Node.js';
    case 'typescript':
      return 'TypeScript';
    case 'bun':
      return 'Bun';
    case 'docker':
      return 'Docker';
    case 'kubernetes':
      return 'Kubernetes';
    case 'postgres':
      return 'PostgreSQL';
    case 'redis':
      return 'Redis';
    case 'nginx':
      return 'Nginx';
    case 'git':
      return 'Git';
    case 'java':
      return 'Java';
    case 'ruby':
      return 'Ruby';
    case 'php':
      return 'PHP';
    case 'terraform':
      return 'Terraform';
    case 'aws':
      return 'AWS';
    case 'bash':
      return 'Shell';
    case 'linux':
      return 'Linux';
    default:
      return 'Shell terminal';
  }
}

function normalize(value?: string): string {
  return value?.trim() || '';
}