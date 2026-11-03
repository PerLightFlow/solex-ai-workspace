# 当前 SOLEX 基础设施

本目录保存现有部署的七服务 Compose 拓扑和 SearXNG 配置模板。`compose.yaml` 于 2026-11-03 与服务器文件逐字核对一致。Compose 项目名、网络名和服务器目录保留 `weknora-native`，避免重建时生成第二套容器或改变数据挂载。

| 服务 | 用途 | 当前运行位置 |
| --- | --- | --- |
| frontend | SOLEX 页面与 API 代理 | `192.168.85.199:8080` |
| app | Go API、智能体和后台任务 | 回环 `127.0.0.1:18083` |
| postgres | 业务数据和检索索引 | Compose 内网 |
| redis | 任务队列与流状态 | Compose 内网 |
| docreader | 文档解析 | Compose 内网 |
| embedding | Ollama 本地向量模型 | Compose 内网 |
| searxng | 联网搜索 | Compose 内网 |

## 服务器文件与配置

- `/data/solexai/yongjun/weknora-native/compose.yaml`：运行中的拓扑。
- 同目录 `.env`：镜像引用、数据库与加密密钥、管理员引导及运行参数；只保留在服务器。恢复必须保留原密钥，不能对已有数据库重新生成。
- 同目录 `admin.json`：自动验收使用的管理员登录信息，权限为 0600；账户信息调整后需同步维护，不提交或打印。
- 同目录 `data/`：数据库、上传文件、解析缓存、Embedding 模型及搜索配置。
- 同目录 `runtime-releases/`、`ui-releases/`：当前发布证据、私有配置备份和回退数据。
- `/data/solexai/yongjun/select-chip-agent/releases/` 与 `docker-client/`：现有受限发布器的工作目录，仍在使用。

`searxng.yml` 是配置模板。保留服务器当前 `data/searxng/settings.yml` 及其运行配置，不在发布应用时覆盖。模型和搜索提供方设置通过管理员界面/API 维护，不把模型密钥写入 Compose。

统一配置模式依赖 `.env` 中的 `SOLEX_MANAGED_RUNTIME=true` 和 `SOLEX_RUNTIME_TENANT_ID=10000`；该空间提供共享模型及运行配置，知识库权限仍由成员与共享规则决定。发布器会维护这两个参数。

当前 Kimi 配置应保留 `supports_temperature=false` 且不设置 `fixed_temperature`，让提供方按思考模式使用默认采样参数；不能从旧部署样例恢复固定温度。该兼容设置保存在模型记录中，应用发布不重建模型记录或改变已有 Embedding 绑定。

## 维护已有实例

1. 在完整源码中修改，运行相关检查并提交 Git。
2. 后端或公共模板变化使用 [`solex-runtime`](../solex-runtime/README.md)；仅 UI 变化可使用 [`solex-ui`](../solex-ui/README.md)。两者均打包有源码 SHA 和校验清单的独立发布目录。
3. 将发布包上传到现有 `select-chip-agent/releases/RELEASE_NAME/`，执行服务器已配置的受限入口：

   ```bash
   sudo -n /usr/local/sbin/chipagent-release apply RELEASE_NAME
   ```

4. 以发布验收结果、实际容器镜像标签和应用行为确认成功。发生失败时使用该版本保存的回退状态，保留新增业务数据。

完整发布工具假定现有实例已经初始化，包含 `active.json`、管理员登录信息和配置好的模型。首次从旧自研系统搬迁、原生账号引导、旧容器切换和一次性品牌迁移已完成，相关脚本仅保留在 Git 历史，不能再作为日常安装或回退入口。

全新环境安装需单独准备镜像、密钥、管理员、模型与知识库共享；本目录不提供未经验证的一键初始化。完整上游安装源码仍在 [`weknora/`](../../weknora/)。不要将现有生产数据目录用于新环境试装。

## 当前版本基线

2026-11-03 核对的两个运行镜像：

```text
app       solex/solex-app:40b9cd8df4b6
frontend  solex/solex-ui:managed-40b9cd8df4b6
revision  40b9cd8df4b6ea00c42edc00ab63e57da023af80
```

应用源码、公共模板和全部 PostgreSQL/SQLite 迁移完整保留。当前完整发布工具明确支持 PostgreSQL 135/136 到 136、SQLite 源码迁移到 53；增加迁移前需重新审核兼容回退逻辑。此次仓库整理没有修改服务器 Compose、运行镜像或数据。
