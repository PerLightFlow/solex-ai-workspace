# 共享空间按用户列表邀请

“邀请用户”显示当前可邀请的有效账号，支持按用户名、邮箱搜索和分页，每页 20 人。选择一个账号后发送七天有效的定向邀请，对方接受后以“使用者”加入。发送成功后刷新空间的待接受邀请和动态。

只有该共享空间的所有者、管理员可查看候选及邀请；个人空间所有者或系统管理员身份本身不能越过共享空间成员权限。列表只返回账号 ID、用户名、邮箱，不包含个人空间、私有资源、配置等信息。停用/删除账号、现有成员、已有有效待接受邀请的账号不在列表中。

新增 `GET /api/v1/spaces/:space/invite-candidates?q=&offset=0&limit=20`，最大每页 50 人。搜索采用参数化查询，按字面匹配通配字符，结果稳定排序。`POST /api/v1/spaces/:space/invitations` 支持 `user_id`；发出时重新验证账号状态、成员关系和有效邀请，过时选择返回 409。旧的邮箱请求仍兼容，但不能同时传两个身份字段。归档空间不能发出邀请。

无需数据库迁移，沿用 schema 137 的邀请记录、接受流程和活动记录。既有账号及空间权限不改变。

## 验证

- 前端 `npm run type-check`、`npm run build` 通过；构建存在既有 bundle 体积提示。
- `go test -tags anydoc ./internal/spaces ./internal/handler -skip '^TestPutTenantParserConfigAdminPreservesRedactedSecrets$'` 通过。跳过项是在本次修改之前已确认的解析器设置脱敏测试失败，与邀请无关。
- 新增服务测试覆盖空间角色、归档状态、失效选择、默认使用者、列表字段边界、停用/删除/成员/邀请过滤、大小写与字面搜索、分页及邀请过期/撤回。
- `deploy/solex-runtime/invite_picker_check.py` 用于隔离数据库的真实 HTTP 验收，不调用模型，不向线上用户发送邀请。

部署及页面验收结果在发布完成后记录。
