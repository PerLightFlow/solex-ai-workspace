# SOLEX 源码与开发入口

当前原生平台的完整源码位于 [`weknora/`](weknora/)，产品界面名称为 **SOLEX**。

这是直接纳入本仓库 Git 管理的完整上游源码，不是子模块、嵌套仓库或仅前端副本。全部应用修改均在 `weknora/` 中进行，前端位于 `weknora/frontend/`。旧自研系统与一次性迁移工具已从当前分支移除，历史版本仍可从 Git 查询。

## 上游基线

| 项目 | 值 |
| --- | --- |
| 上游 | https://github.com/Tencent/WeKnora.git |
| 版本 | v0.9.0 |
| 上游提交 | `f17ee8a8da901bd658a4cb25f40b5fd41adbfdd7` |
| 完整源码树 | `de9e8951de64824a2f654d8ff5ace37fe5672b23` |
| 纳入文件 | 5,870 个上游跟踪文件；逐文件内容、可执行位与符号链接已校验 |
| 本仓库基线提交 | `33001f3` |
| 发布归档 SHA-256 | `b6870da4beee43313e0cbead789d8ea49680c295e3e341938ac542a444631686` |

可通过 `git rev-parse 33001f3:weknora` 核对源码树。上游的许可证、第三方声明、后端、CLI、解析服务、测试、文档和构建文件均保留。产品定制放在后续独立提交中，便于比较与回退。

## 开发

```bash
cd weknora/frontend
npm ci
npm run dev
```

发布前执行 `npm run type-check`、相关测试和 `npm run build`，并验证桌面与手机布局。依赖锁文件使用上游版本，`node_modules/` 和 `dist/` 不提交。

常用位置：

- `weknora/frontend/src/views/auth/`：登录、邀请与空间初始化。
- `weknora/frontend/src/components/menu.vue`：工作台导航。
- `weknora/frontend/src/views/creatChat/`：新对话首页。
- `weknora/frontend/src/assets/solex/`、`assets/theme/solex.css`：品牌资源与全局主题。
- `weknora/frontend/src/views/settings/SolexHelp.vue`：站内帮助。
- `weknora/internal/`、`weknora/cmd/`：Go 后端。
- `weknora/docreader/`：文档解析服务。
- `deploy/solex-runtime/`：当前后端与前端的完整构建、发布、验收和回退。
- `deploy/solex-ui/`：仅修改前端时的独立发布与回退。
- `deploy/solex-infra/`：当前七服务 Compose 拓扑及搜索配置模板。
- `.github/workflows/ci.yml`：本仓库生效的 CI；`weknora/.github/` 是上游源码的一部分，不会在本仓库自动运行。

## Git 维护

`origin` 保持为 `PerLightFlow/select-chip-agent`，`weknora-upstream` 指向官方仓库。克隆本仓库即可取得完整源文件，不需要额外初始化子模块。Git remote 配置不会随克隆传播，后续需要追踪上游时执行：

```bash
git remote add weknora-upstream https://github.com/Tencent/WeKnora.git
git fetch --no-tags weknora-upstream refs/tags/v0.9.0:refs/remotes/weknora-upstream/v0.9.0
git diff 33001f3..HEAD -- weknora/frontend
```

升级上游应单开分支，先核对完整新版源码，再对照 SOLEX 定制提交处理冲突和验证。不要把上游根目录直接合并到当前仓库根目录。业务代码、部署配置、数据与运行凭据分开维护，服务端 `.env`、数据库、模型文件和账号凭据不加入源码。

## 品牌与兼容

界面统一为 SOLEX；存储键、API 字段、内置智能体 ID、消息协议及既有嵌入接口保留兼容。新嵌入代码使用 `solex-widget.js`，旧接口仍可工作。用户文档、模型回答和自定义名称不做全局文本替换。

现有 Docker 部署位于 `/data/solexai/yongjun/weknora-native`。仅改界面时可只替换前端；涉及后端时使用完整运行版本发布。模型配置、账号、知识库和 Wiki 保存在服务器数据目录中，具体维护步骤见 [当前部署说明](deploy/solex-infra/README.md)。源码导入、构建与单元测试通过不等同于生产并发验收。
