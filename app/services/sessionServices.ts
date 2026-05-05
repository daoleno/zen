export type SessionServiceURL = {
  label: string;
  url: string;
  address: string;
  kind: "lan" | "tailscale" | string;
};

export type SessionServiceInterface = {
  name: string;
  address: string;
  kind: "lan" | "tailscale" | string;
};

export type SessionService = {
  id: string;
  agent_id: string;
  agent_name: string;
  project?: string;
  cwd?: string;
  command?: string;
  process?: string;
  pid: number;
  port: number;
  protocol: string;
  binds: string[];
  urls: SessionServiceURL[];
  local_only: boolean;
};

export type SessionServiceSnapshot = {
  generated_at?: string;
  interfaces: SessionServiceInterface[];
  services: SessionService[];
};

