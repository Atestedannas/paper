# 模板骨架移植第一切片实现计划

> **给后续执行者说明：** 按任务逐步执行本计划。每个步骤使用复选框（`- [ ]`）跟踪进度。实现时优先使用 TDD：先写失败测试，再写最小实现，再跑验证。

**目标：** 在 v2 DOCX 主流程中加入第一条可运行的“模板骨架移植”路径。

**架构：** 保留现有 v2 上传、运行、复检、下载流程，但当配置了 `CQRWST_TEMPLATE_PATH` 时，不再直接复制原稿作为最终稿，而是先解析用户原稿、把内容块映射到已编译模板槽位、从模板骨架生成最终 DOCX，然后继续执行现有 CQRWST 自动修复和独立复检。

**技术栈：** Go、OOXML zip/XML、`internal/core/paperparse`、`internal/core/blockmap`、`internal/core/templatecompile`、`internal/core/transplant`、`internal/core/cqrwst`、`internal/core/verify`、GORM 工作流服务。

---

## 文件结构

- 修改：`internal/core/templatecompile/compiler.go`
  - 让编译出的 block ID 与模板占位符 token 对齐，并加入可重复正文块。
- 修改：`internal/core/blockmap/mapper.go`
  - 将解析出的正文段落映射为 `body` 绑定。
- 修改：`internal/core/transplant/transplanter.go`
  - 自动创建输出目录，并支持模板映射契约中的占位符。
- 修改：`internal/service/paper_workflow_service.go`
  - 如果设置了 `CQRWST_TEMPLATE_PATH`，先从编译后的模板骨架生成最终 DOCX，再执行 CQRWST 修复和复检。
- 修改测试：
  - `internal/core/transplant/transplanter_test.go`
  - `internal/core/blockmap/mapper_test.go`
  - `internal/service/paper_workflow_service_test.go`

## 任务 1：打通模板槽位端到端可用性

- [x] 写一个失败的移植测试，证明编译器风格的 block ID 可以替换 `{{body}}` 和 `{{heading_1}}` 占位符。
- [x] 更新 `templatecompile.Compiler`，让 block ID 使用稳定槽位键：`cover_title`、`abstract_cn_body`、`keywords_cn`、`heading_1`、`body`、`references`、`acknowledgement`。
- [x] 更新 `blockmap.Mapper`，让 `body` 将每个解析出的正文段落映射为可重复绑定。
- [x] 更新 `transplant.Transplanter`，优先使用 `MappingContract.BlockBindings` 中定义的占位符；没有配置时再回退到 `{{block_id}}`。
- [x] 运行验证：

```powershell
go test -count=1 ./internal/core/templatecompile ./internal/core/blockmap ./internal/core/transplant -v
```

## 任务 2：将可选模板骨架移植接入 RunJob

- [x] 写一个失败的服务测试：设置 `CQRWST_TEMPLATE_PATH`，让它指向一个包含 `{{heading_1}}` 和 `{{body}}` 的 DOCX 模板。
- [x] 在 `paperWorkflowService` 中新增 `buildWorkflowOutput(ctx, sourcePath, outputPath)`。
- [x] 如果 `CQRWST_TEMPLATE_PATH` 为空，保持现有 `copyFile` 行为不变。
- [x] 如果 `CQRWST_TEMPLATE_PATH` 已设置，则把模板编译到输出根目录下的缓存目录，解析用户原稿，映射内容块，并从模板骨架生成 `final.docx`。
- [x] 生成 `final.docx` 后，继续使用现有 `cqrwst.FixDOCX` 和 `verify.NewVerifier`，不改变复检闭环。
- [x] 运行验证：

```powershell
go test -count=1 .\internal\service\paper_workflow_service.go .\internal\service\paper_workflow_service_test.go -v
```

## 任务 3：完整验证

- [x] 运行核心包验证：

```powershell
go test -count=1 ./internal/core/paperparse ./internal/core/templatecompile ./internal/core/blockmap ./internal/core/transplant ./internal/core/cqrwst ./internal/core/verify ./internal/core/workflow
```

- [x] 运行 handler 下载链路验证：

```powershell
go test -count=1 ./internal/handler -run TestPaperWorkflowHandler -v
```

- [x] 运行 server 编译验证：

```powershell
go test -count=1 ./cmd/server
```

## 手动使用方式

启动后端前指定标准模板路径：

```powershell
$env:CQRWST_TEMPLATE_PATH="D:\path\to\cqrwst-template.docx"
go run .\cmd\server
```

如果不设置 `CQRWST_TEMPLATE_PATH`，系统继续使用当前“复制原稿 + CQRWST 修复 + 独立复检”的路线。
