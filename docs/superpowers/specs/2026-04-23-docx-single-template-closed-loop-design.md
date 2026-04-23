# DOCX 单模板闭环重构设计

日期：2026-04-23

## 目标

将当前论文格式系统重构为一条唯一、确定、可复检的生产主链，服务于以下场景：

- `DOCX`
- `单学校`
- `单模板`
- `自动检查`
- `自动修复`
- `自动复检`
- 浏览器直接下载最终修正稿

目标结果：

- 生产环境只有一条主链
- 不再存在多引擎回退
- 不生成预览版文件
- 不生成“尽力版”文件
- 浏览器只能下载最终通过验证的 `docx`

## 非目标

本次重构不覆盖以下范围：

- 多模板自动匹配
- PDF 作为主输入
- AI 优先的运行时修复
- 向量数据库驱动的生产决策
- 人工 diff 审核作为主生产流程
- 浏览器内文档预览作为交付路径

## 核心产品决策

系统不再“原地修学生稿”。

系统改为：

`解析学生稿 docx -> 映射到模板槽位 -> 基于模板骨架生成最终稿 -> 进行白名单 OOXML 补丁 -> 独立复检 -> 通过后下载`

核心约束：

- 模板顺序是唯一结构真相
- 学生内容允许被重排进模板槽位
- 不能识别的内容不得强行塞入正常块

## 当前系统为什么效果差

当前系统在生产路径上同时存在多条重叠链路：

- 上传路径混入了解析、AI、修复、实验流程和生成流程
- 服务层同时承担检查、修复、导出和引擎回退
- 对比链路以半生产状态并行存在
- 多个修复引擎会输出彼此不兼容的结果

这会导致四类系统性问题：

1. 最终格式没有单一真相来源
2. 检查结果无法稳定映射为可执行修复动作
3. 引擎切换使输出行为不可预测
4. 前端交互面对的是碎片化流程，而不是单一状态机

## 最终技术路线

最终确定的技术路线为：

`Go + 单模板预编译 + 模板骨架移植 + 块级内容映射 + 原生 OOXML 定点回写 + 独立复检器`

### 技术选型

- 服务与 API：`Go + Gin`
- DOCX 读写真相层：`DOCX zip 包 + 原生 OOXML XML 操作`
- 只读结构辅助：`unioffice`
- XML 编辑方式：DOM 风格 OOXML 编辑库，或等价低层 XML 处理方案
- 运行时分类：纯 Go 确定性规则引擎
- 状态管理：数据库驱动的显式任务状态机
- 输出产物：仅最终通过验证的 `docx`

### 明确排除出生产主链的方案

- Python + OOXML 作为主修复引擎
- Word COM 作为主修复引擎
- 向量数据库 / RAG 进入运行时决策回路
- LLM 作为最终块映射器或最终修复权威
- 多引擎运行时回退链
- 通用样式修补器作为主修复模型

## 总体架构

新的生产主链由七个核心模块组成：

1. `TemplateCompiler`
2. `PaperParser`
3. `BlockMapper`
4. `Transplanter`
5. `OOXMLPatchWriter`
6. `Verifier`
7. `LoopController`

### 主流程

`uploaded -> parsed -> mapped -> transplanted -> patched -> verified_pass`

失败路径：

- 若仅存在白名单内可修复问题，则允许再进行一次补丁重试
- 其他情况直接进入 `manual_review`

主链内部不允许切换修复引擎。

## 模块设计

### 1. TemplateCompiler

职责：

- 将一份官方学校模板编译为版本化模板资产包
- 将原始模板转化为稳定的排版合同

输入：

- 官方模板 `docx`

输出：

- 编译后的模板资产包

模板资产包至少包含：

1. `manifest`
2. `skeleton`
3. `block_catalog`
4. `style_profiles`
5. `mapping_contract`
6. `verification_rules`
7. `patch_targets`

### 2. PaperParser

职责：

- 只解析学生稿
- 构建内容结构树
- 不做修复
- 不在生产路径上做格式落地

输出内容类别：

- 封面字段
- 中文摘要
- 中文关键词
- 英文摘要
- 英文关键词
- 标题树
- 正文段落
- 图题
- 表题
- 正文表格
- 参考文献
- 致谢
- 异常块

### 3. BlockMapper

职责：

- 将解析后的学生内容映射到模板槽位
- 严格服从模板顺序这一唯一结构真相

映射规则分五层：

1. 强锚点规则
2. 文档状态机规则
3. 局部上下文规则
4. 块容量规则
5. 异常兜底规则

输出：

- `block_bindings`
- `generated_blocks`
- `unmapped_blocks`
- `ambiguous_blocks`
- `verifier_hints`

### 4. Transplanter

职责：

- 基于编译好的模板骨架生成最终论文
- 按模板顺序把学生内容移植进模板槽位

它不负责：

- 开放式文档修复
- 重新分类
- 对其职责范围之外的节点做补丁

### 5. OOXMLPatchWriter

职责：

- 仅做小范围白名单后处理修正

允许的修正类别：

- 段落间距、缩进、对齐、分页控制
- run 级字体、字号、粗斜体、颜色、上下标
- 编号引用挂接到模板编号体系
- 白名单表格单元格内 run 和段落修正
- 目录字段设置与刷新相关元数据
- 必要的媒体或超链接关系补丁
- 白名单节属性补齐

它不能演变成第二个修复引擎。

### 6. Verifier

职责：

- 独立验证输出质量
- 不能把映射决策和修复决策当成真相复用

验证分四层：

1. 块级验证
2. 样式级验证
3. 包级验证
4. 安全级验证

### 7. LoopController

职责：

- 编排确定性的自动闭环
- 首次验证失败后，最多允许一次补丁重试

自动闭环预算：

- 一次完整生成
- 一次补丁重试

超出预算直接进入 `manual_review`。

## 模板资产包设计

### Manifest

字段：

- `template_id`
- `template_version`
- `school_id`
- `docx_hash`
- `compiled_at`
- `compiler_version`

### Block Catalog

每个模板块至少包含：

- `block_id`
- `kind`
- `slot_type`
- `order_index`
- `parent_block_id`
- `style_profile_id`
- `anchor`
- `source_region`
- `capacity`
- `required`
- `accepts`
- `patch_policy`
- `verify_policy`

推荐的 `slot_type`：

- `fixed`
- `single`
- `repeatable`
- `generated`

第一版推荐的 `block kind`：

- `cover_title`
- `cover_meta_label`
- `cover_meta_value`
- `cover_date`
- `abstract_cn_title`
- `abstract_cn_body`
- `keywords_cn`
- `abstract_en_title`
- `abstract_en_body`
- `keywords_en`
- `toc_title`
- `toc_entry_container`
- `heading_1`
- `heading_2`
- `heading_3`
- `body_para`
- `figure_caption`
- `table_caption`
- `reference_title`
- `reference_item`
- `ack_title`
- `ack_body`

### Style Profiles

每个样式档案包含：

- paragraph spec
- run spec
- numbering spec
- 需要时包含 table / cell spec
- section 约束
- forbidden mutations

### Mapping Contract

定义：

- 允许接收的输入块类型
- 单值或多值约束
- 是否允许拆分
- 是否允许合并
- 是否允许为空
- 溢出处理方式
- 歧义处理方式

### Verification Rules

定义：

- 必须存在约束
- 数量约束
- 顺序约束
- 样式约束
- 锚点约束
- 安全约束

### Patch Targets

定义生成后允许补丁写入的唯一 OOXML 目标集合。

## 映射规则

### 结构真相

最终文档顺序永远等于模板顺序。

学生稿不控制最终块顺序。

### 硬规则

- 封面按显式字段映射，不按自由段落映射
- 目录由最终标题树生成，不继承学生稿原始目录
- 标题编号仅使用模板编号体系
- 页眉页脚仅来自模板
- 节属性仅来自模板
- 参考文献与正文段落严格隔离
- 无法识别的内容进入异常桶，不能进入正常槽位

### 异常桶

映射器必须显式输出：

- `unmapped_blocks`
- `ambiguous_blocks`
- `overflow_blocks`

这些是正式输出，不是调试信息。

## 生成规则

### 总原则

模板骨架是输出基底。
学生稿是内容来源。

### 封面

- 不得重建封面表格几何结构
- 仅允许替换指定槽位或指定单元格的文本内容
- 必须保留模板表格布局、合并、宽度、边框和定位

### 摘要与关键词

- 使用模板拥有的壳段落承载内容
- 只移植内容
- 标签格式始终保留模板原生样式

### 标题

- 先从学生稿建立标题树
- 再把标题文本写入模板标题原型段落
- 编号体系只使用模板编号体系

### 正文段落

正文以“内容原子”移植，而不是只按纯文本移植。

推荐的内容原子：

- `text_run`
- `inline_image`
- `inline_formula`
- `footnote_ref`
- `hyperlink`
- `inline_break`

### 题注

- 图题和表题是独立块类型
- 不能悄悄混入正文段落

### 表格

- 保留学生表格内容
- 只允许对白名单内表格文本格式做更新
- 第一版生产链不重建表格几何结构

### 参考文献

- 每条参考文献都克隆模板参考文献原型块
- 格式由模板档案决定，而不是学生稿格式

### 目录

- 模板拥有目录容器
- 第一版服务端不强求目录页码完全精确
- 必须生成结构正确、可在打开文档后刷新的目录字段

## PatchWriter 边界

### 允许修改

- `w:t`
- `w:r`
- `w:rPr`
- `w:pPr`
- `w:numPr`
- `w:br`
- `w:tab`
- 白名单 `w:tc` 文本承载后代节点
- 必要的关系项
- 白名单目录元数据

### 禁止修改

- 代替生产主修复引擎
- 改写模板块顺序
- 修改模板封面表格几何结构
- 重建第二套 `styles.xml`
- 开放式重写节结构
- 生成后任意重新分类

## Verifier 设计

### 验证层次

1. `块级`
2. `样式级`
3. `包级`
4. `安全级`

### Verify Result

推荐输出结构：

- `passed`
- `score`
- `fatal_issues`
- `repairable_issues`
- `warnings`
- `unmapped_blocks`
- `ambiguous_blocks`
- `output_hash`

### Fatal Issue 示例

- 必填模板块缺失
- 单值槽位出现歧义
- 标题树不合法
- 参考文献区边界不合法
- 检测到禁止性 OOXML 变更
- 第二次验证仍未通过

### Repairable Issue 示例

- 白名单段落间距不一致
- 编号引用不一致
- 目录字段元数据可修复
- 白名单 run 样式不一致

## 闭环策略

闭环规则：

- 一次完整生成
- 最多一次补丁重试
- 不允许运行时引擎切换
- 不允许无限递归重试

状态流转：

- `uploaded`
- `template_compiled`
- `parsed`
- `mapped`
- `transplanted`
- `patched`
- `verified_pass`
- `verified_fail`
- `manual_review`

## 交付策略

生产环境只允许一个输出文件：

- 最终通过验证的 `docx`

规则：

- 不生成预览版
- 不生成尽力版
- 不生成 manual review 导出版
- 浏览器只能下载最终修正稿

当结果为 `verified_pass`：

- 返回下载地址

当结果为 `verified_fail` 或 `manual_review`：

- 只返回问题清单
- 不暴露任何文件下载地址

## API 设计

生产版 `v2` API 收敛为一组工作流接口：

### `POST /api/v2/templates/compile`

编译一份模板。

### `POST /api/v2/papers`

上传学生稿并绑定编译后的模板。

输入：

- `paper.docx`
- `template_id`

输出：

- `job_id`

### `POST /api/v2/jobs/:job_id/run`

启动确定性闭环。

内部固定执行：

- parse
- map
- transplant
- patch
- verify

### `GET /api/v2/jobs/:job_id`

查询任务状态和问题集合。

### `GET /api/v2/jobs/:job_id/download`

下载最终通过验证的修正稿。
仅 `verified_pass` 可用。

## 前端工作流

前端收敛为一条直通工作流：

1. 选择模板
2. 上传学生稿
3. 创建任务
4. 启动任务
5. 轮询状态
6. 成功后直接下载最终文件

前端应停止暴露：

- 多条修复路径
- 手动切换引擎
- 生产态 diff 应用流程
- 预览优先流程

## 旧代码处理策略

当前生产路径必须显著收敛。

### 必须退出生产主链

- 上传时混入 AI / 实验 / 修复的复合路径
- 多引擎回退修复链
- comparison service 作为生产平行链
- 通用样式修补器作为主修复策略
- 向量数据库 / RAG / LLM 进入运行时生产回路

### 可以选择归档或局部复用

- 底层 OOXML 工具函数
- 现有模板块识别工具
- 能适配新模块边界的严格格式逻辑
- 部分只读解析辅助函数

### 代码结构方向

推荐模块目录：

- `internal/core/templatecompile`
- `internal/core/paperparse`
- `internal/core/blockmap`
- `internal/core/transplant`
- `internal/core/ooxmlpatch`
- `internal/core/verify`
- `internal/core/workflow`

`handler` 应收缩为薄适配层。
工作流编排应进入专门的核心服务。

## 迁移计划

### Phase 1：冻结并隔离

- 定义新的 v2 工作流边界
- 停止向旧的混合上传 / 混合 service 路径继续加逻辑
- 将旧多引擎路径标记为 legacy

### Phase 2：实现模板编译器

- 编译单学校官方模板
- 生成版本化模板资产包
- 定义块目录和样式档案

### Phase 3：实现解析器与映射器

- 解析学生内容树
- 实现确定性映射器
- 显式产出异常桶

### Phase 4：实现移植器与补丁器

- 基于模板骨架生成最终文档
- 增加白名单 OOXML 补丁

### Phase 5：实现复检器与闭环控制器

- 独立验证
- 最多一次重试
- 稳定的 pass / fail / manual review 行为

### Phase 6：替换前端路径

- 切换为单任务工作流
- 移除预览优先与多路径交互

### Phase 7：移除旧生产链

- 将旧 handler / service 从生产路由中断开
- 完成切换后归档或删除死代码

## 成功标准

满足以下条件，视为重构成功：

- 一份模板编译一次后可被稳定复用
- 一篇学生稿只有一个最终输出路径
- 主链运行时不切换修复引擎
- 模板顺序始终被保留
- 必填块不会被静默跳过
- 失败行为清晰且安全
- 只有 `verified_pass` 文件可下载

## 最终结论

新系统的本质是：

一个确定性的、模板驱动的文档组装系统，
而不是一个泛化的文档修补系统。

它的真相来源是：

- 内容真相：学生稿
- 格式真相：编译后的官方模板
- 生成真相：模板骨架移植
- 补丁真相：白名单定点修改
- 质量真相：独立复检器

这就是在目标范围内逼近高实现率的架构基础：

`DOCX + 单学校 + 单模板 + 自动检查 + 自动修复 + 自动复检 + 浏览器直接下载最终修正稿`
