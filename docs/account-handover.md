# 账号停用与共享内容交接

系统管理员在「系统管理 → 用户管理」点击有效账号的「停用」，先查看该账号拥有的共享空间和已共享的个人原件，再确认停用。停用成功后原有登录会话立即失效，在同一窗口继续指定接任人或原件归属空间。尚未完成的交接可通过停用账号行的「交接」继续。仅有待交接空间或已共享原件的停用账号显示此按钮；无待办或交接完成后自动隐藏。列表仅检查当前页停用账号，并将并发请求限制为 4；读取失败提示刷新，不把失败当成空清单。

- 共享空间首页不再显示「离职空间交接」。成员管理的「转交所有权」保留，正常交接仍由空间所有者完成。
- 停用和交接是明确的两个阶段。交接失败不会重新启用账号；管理员可以先停用，再稍后处理。无待交接内容时直接完成停用。
- 新接口 `GET /api/v1/spaces/admin/users/:user/handover` 仅系统管理员可用，按账号 ID 查询，包含启用状态、拥有的共享空间、已经共享的个人原件及现有共享目标。私人聊天、未共享资料不进入清单。
- 同一原件共享到多个空间时合并显示，仅移交一次；归属只能选择它已有授权的空间，其他授权不删除。知识库优先于引用它的智能体显示，智能体移交仍校验知识库范围。
- 交接请求携带预期所有者 ID，服务端在持有资源/空间锁时检查，拒绝过期清单对新所有者资源的操作。停用状态使用账号行锁核实。
- 接任人列表遍历用户分页，仅显示有效账号。交接成功写入现有空间动态；原有账号停用、当前账号及最后管理员保护机制保持不变。
- 本次没有数据库结构变更，沿用 PostgreSQL schema 137。发布只替换应用和前端镜像，保留现有数据及配置；故障恢复不还原旧数据库。

验证：前端类型检查、生产构建及 10 项定向测试；后端空间、账号状态/列表及仓储测试；`deploy/solex-runtime/account_handover_check.py` 在隔离数据库中验证真实 API 流程，使用合成账号/知识库/智能体，不调用模型。

后端完整 handler 测试中的 `TestPutTenantParserConfigAdminPreservesRedactedSecrets` 返回 400 而预期 200，已在未修改的生产源版本 `00f3bdfb5338` 复现；与本次交接接口无关。其余上述包测试单独排除此项后执行，发布证据另行记录。

## 2026-11-06 发布结果

- 应用与前端源码版本：`81119d140d4efab230b5f5b275cf51a2b54656e2`。
- 应用镜像：`solex/solex-app:81119d140d4e`；前端镜像：`solex/solex-ui:managed-81119d140d4e`。
- 生产入口：`http://192.168.85.199:8080`，系统用户管理路径 `/platform/settings?section=system-global&panel=users`。
- PostgreSQL 仍为 schema 137、dirty=false；数据库、Redis、解析、向量和搜索的五个服务容器未更换。
- 前端类型检查、生产构建和 10 项定向测试通过。后端 `internal/spaces`、`internal/handler`、`internal/application/repository` 包测试通过（仅排除上文已复现的原有解析配置失败项）。
- 隔离真实 API 验收的 10 项检查通过：按账号预览、普通用户拒绝、停用撤销会话、部分交接恢复、过期所有者拒绝、目标仅限已有共享空间、知识库及智能体交接、保留其他共享授权、排除私有内容、保留普通成员交接。
- 生产浏览器验证：首页入口消失；成员页保留转交所有权；已停用账号行显示交接；有效账号停用按钮打开预览，取消和 Escape 正常。仅查看现有业务账号预览，没有停用它们。
- 隔离应用/前端/Redis 容器和隔离数据库已清理。未在生产创建本次验收账号或资料。
- 服务器验收记录、配置备份和数据库快照：`/data/solexai/yongjun/build-cache/account-handover-state-81119d1`（受限权限）；运行目录内发布记录：`/data/solexai/yongjun/weknora-native/runtime-releases/solex-runtime-81119d140d4e`。
- 本机 UI 截图：`/home/s5376/yongjun/work/proj/SLSelectChipAgent/work/acceptance/account-handover-20261106/disable-preview.png`。本机同结构发布脚本：`/home/s5376/yongjun/work/proj/SLSelectChipAgent/work/account-handover-ops.py`，仅针对该已验收版本，配置与凭据不进入 Git。

### 同日界面修正：隐藏空交接入口

前端源码 `41301e358a512a114f2ada86afa256a5aab68600` 已发布为 `solex/solex-ui:2fcfef6624a5b186`。前端类型检查与生产构建通过；发布前后其他六个服务容器保持不变。只读查询确认当前 24 个停用账号均无待交接内容，生产浏览器已验证两页用户列表的交接按钮全部隐藏，包括用户批注所指账号。未修改账号状态、资料或共享关系。完成交接后，弹窗读取到的新清单同步回用户列表，按钮随待办清空而隐藏。

发布记录：`/data/solexai/yongjun/build-cache/handover-button-ui-41301e3-state/acceptance.json`。页面截图：`/home/s5376/yongjun/work/proj/SLSelectChipAgent/work/acceptance/handover-button-20261106/users.png`。
