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

export interface ManagementPluginStorePlatform {
  goos: string;
  goarch: string;
}

export interface ManagementPluginStoreSource {
  id: string;
  name: string;
  url: string;
}

export interface ManagementPluginStoreSourceError {
  source_id: string;
  source_name: string;
  source_url: string;
  message: string;
}

export interface ManagementPluginStoreEntry {
  store_id: string;
  source_id: string;
  source_name: string;
  source_url: string;
  id: string;
  name: string;
  description: string;
  author: string;
  version: string;
  repository: string;
  install_type: string;
  auth_required: boolean;
  auth_configured: boolean;
  platforms: ManagementPluginStorePlatform[];
  logo?: string;
  homepage?: string;
  license?: string;
  tags: string[];
  installed: boolean;
  installed_version: string;
  installed_source_id?: string;
  install_source_status?: string;
  path?: string;
  configured: boolean;
  registered: boolean;
  enabled: boolean;
  effective_enabled: boolean;
  update_available: boolean;
}

export interface ManagementPluginStoreListResponse {
  plugins_enabled: boolean;
  plugins_dir: string;
  sources: ManagementPluginStoreSource[];
  source_errors?: ManagementPluginStoreSourceError[];
  plugins: ManagementPluginStoreEntry[];
}

export interface ManagementPluginInstallResponse {
  status?: string;
  source_id?: string;
  source_name?: string;
  source_url?: string;
  id?: string;
  version?: string;
  install_type?: string;
  path?: string;
  plugins_enabled?: boolean;
  restart_required?: boolean;
}
