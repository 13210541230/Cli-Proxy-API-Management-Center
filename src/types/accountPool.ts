export interface AccountPool {
  id: string;
  name: string;
  provider: string;
  enabled: boolean;
  createdAtMs: number;
  updatedAtMs: number;
}

export interface AccountPoolMember {
  poolId: string;
  authId: string;
  priority: number;
  enabled: boolean;
}

export interface AccountPoolBinding {
  apiKeyHash: string;
  poolId: string;
  updatedAtMs: number;
}

export interface AccountPoolPolicyStatus {
  desiredVersion: number;
  desiredHash: string;
  appliedVersion: number;
  appliedHash: string;
  activeVersion: number;
  activeHash: string;
  exclusiveReady: boolean;
  lastError?: string;
  updatedAtMs: number;
}

export interface AccountPoolPolicy {
  version: number;
  hash: string;
  provider: string;
  pools: AccountPool[];
  members: AccountPoolMember[];
  bindings: AccountPoolBinding[];
}

export interface AccountPoolSnapshot {
  policy: AccountPoolPolicy;
  status: AccountPoolPolicyStatus;
}
