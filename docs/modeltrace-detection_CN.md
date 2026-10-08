# Codex 配额页 ModelTrace 检测

## 功能范围

配额管理页的 Codex 认证文件卡片提供 ModelTrace 检测、取消和最新结果证据。检测引擎原生运行在 CPA 后端，不需要安装参考插件或 Usage Service。

本功能不集成到 enterprise-audit，不提供糖果、传统指纹或鹈鹕测试，不修改账号的启用状态或 priority，不新增独立工作区或定时巡检。

## 使用

1. 打开配额管理页，在目标 Codex 文件卡片点击“ModelTrace 检测”。禁用/不可用认证不允许启动。
2. 选择该认证可用且候选库覆盖的目标模型，确认额度提示后点击“开始检测”。不接受自由输入模型别名。
3. 后端固定使用该认证执行三条挑战；前端展示完成进度。关闭弹窗或页面不会停止检测，可以使用“取消检测”。
4. 检测结束后，在卡片状态区查看结果、目标/归因模型和检测时间；“查看证据”展示候选概率、有效挑战数、Token、耗时和原始数字回答。
5. 页面刷新后从 CPA 恢复结果。旧 CPA 没有相关接口时提示升级，原有配额功能仍可使用。

## 结果语义

| 显示 | 含义 |
| --- | --- |
| 未发现降智 | 三条挑战均有效、候选归因概率达到展示门槛，第一候选不是 `gpt-5.6-luna` |
| 疑似降智 | 三条挑战均有效、概率达到门槛，第一候选为 `gpt-5.6-luna`；是指纹规则判断，不是确定证明 |
| 归因未确定 | 不完整挑战或低置信归因，仅保留候选证据，不给强结论 |
| 检测失败 | 请求、响应或分析失败；不代表账号降智 |
| 已取消 / 检测已中断 | 用户取消或 CPA 重启中断；不会自动重发已计费的检测 |

可选择目标模型，证据页同时展示“指纹匹配 / 指纹不匹配 / 匹配未确定”：对比归因模型与所选目标，独立于降智结论。不匹配不自动等于降智。例如选择 `gpt-5.4` 而归因为 `gpt-6-luna`，显示“指纹不匹配”和“未发现降智”。`gpt-5.6-luna` 与 `gpt-6-luna` 是候选库内不同指纹，不能混同。保存的历史结果基于原证据更新规则，不重发检测。

当前展示拒判门槛为候选归因概率 0.8，属于保守的显示规则，**不是已验证的 80% 真实准确率，也不是降智概率**。未在生产账号上完成正常/异常对照校准。ModelTrace 的概率只是在有限候选库内归一；未知新模型、别名或模型版本变化需要基线校准，不能根据旧基线直接判断确定降智。

## 成本与执行

- 每次三条挑战，参考实现使用完整 Codex CLI 上下文，文档估算约 4.2 万输入 Token，尚未计入输出或 CPA 重试。这不是本部署实测值。
- 相比传统指纹的 60/200/400 次采集，请求数更少；不应据此宣称比任何单次短测试都便宜。
- 结果展示实际响应返回的输入、输出、推理 Token；不要用预估冒充实测。
- 同认证最多一个任务，挑战按原方式并行，全局最多六条挑战；超容量不会无限排队。
- 不新加连接建立后的网络超时，沿用 CPA 执行链，并支持显式取消。
- 认证由后端 AuthID + provider 锁定，不通过普通轮询接口选账号，不将 OAuth Token 下发浏览器。
- 结果保存最多每认证 20 条，状态文件位于配置目录而非 auth-dir；保存/读取失败明确提示。重启只恢复记录并标记中断，不自动重放检测。

## 原生管理接口

接口在 `/v0/management` 下，使用既有管理鉴权。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/auth-files/modeltrace` | 候选库与全部认证最新摘要、运行进度（批量一次读取） |
| POST | `/auth-files/modeltrace` | `{auth_index, model}` 启动单认证任务，202 返回进度 |
| DELETE | `/auth-files/modeltrace?auth_index=...` | 取消单认证任务 |
| GET | `/auth-files/modeltrace/record?auth_index=...&id=...` | 读取指定检测证据，省略 id 时读取最新记录 |

前端仅在存在运行任务或提交结果待确认时进行状态轮询，普通刷新不会中止启动/取消请求；离开页面、切换 CPA 地址/管理凭证时取消客户端请求并隔离旧结果，但不会自动重发已提交的付费检测。没有按卡片单独轮询。

## 验证与许可

- 前端验证：`powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/windows/verify-modeltrace.ps1`。
- 所有运行验证必须使用隔离部署，禁止使用生产 18317 端口。测试根目录为 `D:/C_projects/CLIProxyAPI-Suite_7.3.12_windows_amd64`，仅在其独立 `modeltrace-validation/` 子目录创建独立配置、认证和数据；原有 Suite 文件保持不变。CPA 使用独立端口 18327；局部模拟上游/页面夹具使用 18328，不转发到真实模型。
- `D:/C_projects/CLIProxyAPI-Suite_7.3.32_windows_amd64/config.yaml` 是真实上游测试配置来源。`live` 隔离配置仅复制唯一启用的 Codex OAuth 认证文件和必要执行选项，不复制整池认证、其他 API Key、生产管理密钥或插件状态；设置零请求重试，并记录源配置/认证的校验值。副本移除 `refresh_token`，只使用当前有效的访问凭据，防止测试实例刷新并轮换共享上游认证；凭据有效期不足 15 分钟时拒绝准备。若有多个启用账号，脚本拒绝自动选择。
- 隔离脚本需要 Python + PyYAML、已构建的 `dist/index.html`，以及后端 `build/cpa-modeltrace-verify.exe`。`local` 和 `live` 不能同时占用 18327；脚本拒绝覆盖活动实例。在复制凭据之前，对整个隔离配置目录设置 ACL，仅授予当前用户和 SYSTEM。每个配置目录生成独立随机管理密钥，不复用公开测试密钥。

```powershell
# 定向验证隔离配置生成器；不读取真实配置或凭据。
python -m unittest scripts/test_prepare_modeltrace_isolation.py
# local：先启动 18328 的局部上游，再启动 18327 的真实 CPA。
powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/windows/run-modeltrace-fixture.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/windows/run-modeltrace-isolated.ps1 -Mode local
# 本地原生 CPA 回归：仅允许合成凭据，验证部分成功及全调用失败。
node scripts/verify-modeltrace-local-scenarios.mjs
# 只读验证已完成结果，不发起模型调用。
node scripts/verify-modeltrace-runtime.mjs local completed a
# 停止 local 实例后才可切换 live。仅做已授权的单账号真实测试，不批量执行。
powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/windows/run-modeltrace-isolated.ps1 -Mode live
```

页面地址为 `http://127.0.0.1:18327/management.html#/quota`，隔离管理密钥保存在对应 `local/management-key.txt` 或 `live/management-key.txt`，不进入日志或报告。用完停止测试服务；真实认证副本不得进入 Git 或报告。
- 后端单元测试使用模拟执行器，不能代替生产账号的归因准确率验证。
- 算法和候选库来自 xqy2006/ModelTrace（MIT），请求构造及适配参考 Hao Wang 的 cpa-plugin-codex-candy-eval（MIT）；移植保留原版权与许可声明。
