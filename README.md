# SOLEX 企业知识工作台

源码仓库：[PerLightFlow/solex-ai-workspace](https://github.com/PerLightFlow/solex-ai-workspace)。

本仓库维护当前部署的 SOLEX：基于完整 WeKnora v0.9.0 源码，提供智能体对话、企业知识库、文档检索与 Wiki。模型、工具连接和运行参数由系统管理员统一配置，普通用户直接使用。

## 当前代码入口

| 路径 | 用途 |
| --- | --- |
| [`weknora/`](weknora/) | 完整应用源码，含 SOLEX 定制、测试、迁移、第三方代码和许可证 |
| [`weknora/frontend/`](weknora/frontend/) | Vue 3 / TypeScript 前端，界面修改入口 |
| [`weknora/internal/`](weknora/internal/) | Go 后端业务、接口和权限检查 |
| [`deploy/solex-runtime/`](deploy/solex-runtime/README.md) | 后端构建、完整版本打包、发布、验收及回退 |
| [`deploy/solex-ui/`](deploy/solex-ui/README.md) | 仅前端修改时的独立发布及回退 |
| [`deploy/solex-infra/`](deploy/solex-infra/README.md) | 当前 Docker Compose 拓扑及搜索配置模板 |
| [`SOLEX.md`](SOLEX.md) | 上游来源、完整源码校验、开发位置和升级方式 |
| [`docs/ui-design.md`](docs/ui-design.md) | SOLEX 品牌、界面和协议兼容约束 |

旧 Django 后端、旧自研前端、芯片规则库、项目卡片和过渡知识库适配服务已退役，不再作为第二套应用维护。其代码、旧发布记录和一次性迁移脚本保留在 Git 历史中，可从清理前提交 `40b9cd8df4b6ea00c42edc00ab63e57da023af80` 查询。

## 已部署功能

- 管理员统一管理模型、MCP、联网搜索和运行配置；普通用户保留智能体提示词、知识库与已开放工具的业务选择。
- 系统管理员可添加、修改和移除全局内置智能体，所有工作空间使用同一份定义；个人智能体和私有聊天保持原权限。
- 企业资料通过知识库成员或组织共享，结合原文检索和 Wiki 回答；注册账户本身不等于获得所有知识库权限。
- 文件展示上传人、上传时间、最后修改人和人工修改时间；有读取权限的用户可查看文件操作记录。后台解析不覆盖人工归属，历史来源不明时显示未知。

详细行为及权限边界见 [运行版本说明](deploy/solex-runtime/README.md)。

## 本地验证

```bash
cd weknora/frontend
npm ci --no-audit --no-fund
npm run type-check
npm run test -- --run
npm run build
```

在仓库根目录运行离线部署工具测试：

```bash
python3 -m unittest discover -s deploy/solex-runtime -p 'test_*.py'
python3 -m unittest discover -s deploy/solex-ui -p 'test_*.py'
```

实际生效的 CI 在 [`.github/workflows/ci.yml`](.github/workflows/ci.yml)，验证当前 Go 业务包、SOLEX 前端及部署工具。`weknora/.github/` 随完整上游源码保留，不是本仓库的 Actions 入口。Go 单元测试不替代生产 AnyDoc/CGO 构建；后端生产构建使用 [`build-backend.sh`](deploy/solex-runtime/build-backend.sh)。

## 部署与版本

现有入口为 `http://192.168.85.199:8080`，需从能访问该内网的设备打开。服务与数据位于 `/data/solexai/yongjun/weknora-native`；服务器发布助手仍使用 `/data/solexai/yongjun/select-chip-agent/releases/`。

2026-11-03 只读核对：运行中的 app 与 frontend 镜像均标记源码提交 `40b9cd8df4b6ea00c42edc00ab63e57da023af80`。本次仓库整理保留该版本的完整应用源码，更新目录、文档和 CI，不重新发布容器。后续版本应以实际容器的 `org.opencontainers.image.revision` 和对应发布记录为准。

维护既有实例请使用 [基础设施说明](deploy/solex-infra/README.md) 与 [完整发布流程](deploy/solex-runtime/README.md)。数据库、上传文档、模型文件、密钥、运行日志和发布产物不进入 Git。完整源码与单元测试通过不等于 50 人并发或万份文档容量验收。
