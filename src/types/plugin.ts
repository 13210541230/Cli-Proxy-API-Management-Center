export interface ManagementPluginEntry {
  id: string;
  registered: boolean;
  enabled: boolean;
  effective_enabled: boolean;
}

export interface ManagementPluginListResponse {
  plugins_enabled?: boolean;
  plugins?: ManagementPluginEntry[];
}
