# Generic plugin host

The Management Center treats CPA plugins as external extensions instead of adding a React page for every plugin.

## Runtime model

```text
/v0/management/plugins                         CPA plugin metadata/lifecycle API
/v0/management/plugins/:id/...                  generic management proxy
/v0/resource/plugins/:id/...                    plugin UI/resource proxy
/plugin-pages/:pluginId/:menuIndex             one generic iframe host route
```

The plugin API returns optional metadata, configuration fields, and menu resources. The frontend normalizes both snake_case CPA responses and camelCase compatibility responses. Enabled plugin menus are converted into navigation entries at runtime. All entries render through `PluginResourcePage`; the plugin owns its HTML, JavaScript, CSS, and business UI. For same-origin resources, the host exposes a narrow `postMessage` bridge that executes only authenticated requests under that plugin's `/v0/management/{plugin-id}/...` namespace with the Management Center's existing session; plugin pages never receive the management key.

The built-in `/plugins` page only manages lifecycle and configuration using generic controls:

- installed plugin overview;
- enable/disable;
- configuration fields declared by the plugin;
- declared workspace links;
- delete and refresh.

A plugin without `menus` does not add a navigation page. A CPA version without the plugin API leaves the official Management Center usable and hides the optional plugin navigation.

## Compatibility and security boundaries

- Existing core pages remain built in; only cross-plugin or platform-owned functionality belongs in the Management Center.
- Plugin-specific pages and policy controls are not compiled into core pages; Enterprise Access Audit is delivered through its declared plugin resource menu.
- The Usage Service proxies only `/v0/resource/plugins/` without accepting browser-supplied management credentials; it injects the configured CPA management key server-side.
- Plugin IDs are URL-encoded in frontend routes and management requests.
- Plugin resources are isolated in an iframe and loaded only from a plugin-declared resource path or an explicitly absolute resource URL. The API bridge is enabled only for same-origin resources and rejects paths outside the current plugin namespace, unsupported methods, and cross-origin messages.
- Missing metadata, menus, configuration fields, and newer response fields are treated as optional so older CPA versions remain compatible.
