$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath 'D:\C_projects\Cli-Proxy-API-Management-Center'
npm exec prettier -- --write src/pages/MonitoringCenterPage.tsx src/pages/MonitoringCenterPage.test.tsx src/pages/MonitoringCenterPage.module.scss src/i18n/locales/en.json src/i18n/locales/ru.json src/i18n/locales/zh-CN.json src/i18n/locales/zh-TW.json
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
npm run test -- --run src/pages/MonitoringCenterPage.test.tsx
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
npm run type-check
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
npm run build
exit $LASTEXITCODE
