# ModelTrace development notes

- Codex quality checks are limited to the quota page's per-file ModelTrace action and independent status area. Do not add enterprise-audit integration, other evaluation methods, or automatic credential disabling/priority changes.
- CPA executes the three challenges with the selected AuthID and Codex provider. Never fall back to another credential. Model attribution is not proof of degradation; incomplete/low-confidence results remain inconclusive, and upstream failures are not degradation.
- The frontend API contract, scope decisions, and plan are under `.ccg/plan/codex-quality-detection/`. The end-user guide is `docs/modeltrace-detection_CN.md`.
- Targeted frontend verification: `powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/windows/verify-modeltrace.ps1 -Mode All`. Isolation-generator tests: `python -m unittest scripts/test_prepare_modeltrace_isolation.py`.
- Never run browser or service validation on production port **18317**. Isolated profiles live under `D:/C_projects/CLIProxyAPI-Suite_7.3.12_windows_amd64/modeltrace-validation/`, with CPA on **18327** and the local synthetic upstream on **18328**. The `7.3.32` Suite config is the real-upstream source, not a deployment target to overwrite.
- Local runtime regression tools: `node scripts/verify-modeltrace-local-scenarios.mjs` starts only verified synthetic-profile jobs; `node scripts/verify-modeltrace-runtime.mjs local completed a` is read-only and checks persisted evidence. Never use the local driver with live credentials.
- Real-upstream checks must be single-account and bounded. Copy only the selected test credential into an owned profile; do not modify source Suite configuration/auth files or share them in logs/Git. The preparation script restricts profile ACLs and generates a private management key.

- Verdicts and fingerprint matching are independent: only exact `gpt-5.6-luna` is suspected degradation; `gpt-6-luna` and other candidates are not. Both decisive judgments require three valid challenges and probability >= 0.8. Reclassify stored evidence without paid re-execution.

## Change record

- 2026-10-08: Added the ModelTrace quota-card workflow, typed API/state handling, scoped tests, and isolated validation tooling. Backend lives in `D:/C_projects/CLIProxyAPI`; production instances and plugin workspaces are out of scope.
- 2026-10-08: Dual release uses manager v1.24.6 and CPA v7.3.34 with an immutable manager commit in CPA's suite workflow. Rebuild and smoke-test the embedded manager before tagging. The inherited DockerHub publishing job targets `seakee/cpa-manager` and runs only in `seakee/CPA-Manager`; fork release archives and checksums remain enabled.
- 2026-10-08: Adjacent CPA path tests must create the platform-native executable (`cli-proxy-api.exe` only on Windows, `cli-proxy-api` otherwise). Keep the Linux custom-runtime test reaching its actual preservation branch rather than exiting for a missing adjacent fixture. This test-only correction does not change the published runtime or tags.

## Hidden plugin workspace
- The enterprise audit exemption workspace is `/management.html#/plugin-pages/enterprise-access-audit/0/account-pool-exemptions`, with no normal navigation entry. A fixed route supplies the exact allowlisted view to the existing iframe and authenticated API bridge. The URL is not a permission bypass; ordinary pages remain neutral.
- Use a dedicated pathname, not a query-only selector: `PageTransition` keeps a frozen route Location when pathname is unchanged. Do not broaden global animation/routing behavior to support this hidden view.
- Verify from the CPA repository with `scripts/windows/verify-account-pool-hidden-workspace.ps1`. The owned staged Manager is `CLIProxyAPI/build/cpa-manager-account-pool.exe`; Go's embed overlay avoids overwriting another session's binary or tracked embedded HTML. Upgrade the plugin DLL alongside it.
