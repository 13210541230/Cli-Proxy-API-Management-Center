export type ManagementPluginConfigFieldType =
  | 'string'
  | 'number'
  | 'integer'
  | 'boolean'
  | 'enum'
  | 'array'
  | 'object'
  | string;

export interface ManagementPluginConfigField {
  name: string;
  type: ManagementPluginConfigFieldType;
  enum_values?: string[];
  description?: string;
}

export interface ManagementPluginMetadata {
  name?: string;
  version?: string;
  author?: string;
  github_repository?: string;
  logo?: string;
  config_fields?: ManagementPluginConfigField[];
}

export interface ManagementPluginMenu {
  path: string;
  menu?: string;
  description?: string;
}

export interface ManagementPluginEntry {
  id: string;
  registered: boolean;
  enabled: boolean;
  effective_enabled: boolean;
  configured?: boolean;
  supports_oauth?: boolean;
  oauth_provider?: string;
  path?: string;
  logo?: string;
  config_fields?: ManagementPluginConfigField[];
  menus?: ManagementPluginMenu[];
  metadata?: ManagementPluginMetadata | null;
}

export interface ManagementPluginListResponse {
  plugins_enabled?: boolean;
  plugins?: ManagementPluginEntry[];
}

export type ManagementPluginConfig = Record<string, unknown>;

export interface ManagementPluginDeleteResponse {
  status?: string;
  id?: string;
  restart_required?: boolean;
}
