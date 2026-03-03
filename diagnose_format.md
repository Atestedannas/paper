# 格式修正问题诊断指南

## 当前状态
已添加详细的调试日志，需要重新测试以获取诊断信息。

## 诊断步骤

### 步骤1: 重启后端服务
```bash
# 停止当前运行的后端
# 然后启动新编译的版本
cd backend
./server.exe
```

### 步骤2: 上传论文并触发格式修正
1. 打开前端页面
2. 上传测试论文
3. 选择"重庆工程学院"模板
4. 点击"格式修正"

### 步骤3: 收集日志信息

在后端控制台中查找以下日志：

#### 必需的日志信息：

1. **规范化日志**（确认中文键名被正确转换）
```
[DEBUG] 开始增强格式修正
[DEBUG] 规范化后的规则: map[...]
[DEBUG] 可用的格式规则键: [body headings title ...]
```

2. **段落分类日志**（确认段落被正确分类）
```
[Classify] heading_1: 1 结论
[Classify] heading_2: 1.1 研究背景和意义
[Classify] body: 农业是国民经济的重要组成部分...
[DEBUG] 段落分类结果:
  heading_1: X 个段落
  heading_2: X 个段落
  body: X 个段落
```

3. **格式应用日志**（确认格式被正确应用）
```
[DEBUG] 应用 1 级标题格式到 X 个段落，规则: map[...]
[Format] 处理段落: 1 结论
[Format] 应用规则: alignment=left, font_name=黑体, font_size=三号, ...
[Format] 设置对齐方式: left
[Format] 设置字体: 黑体
[SUCCESS] 成功应用标题1格式到 X 个段落

[DEBUG] 应用正文格式到 X 个段落，规则: map[...]
[Format] 处理段落: 农业是国民经济的重要组成部分...
[Format] 应用规则: alignment=justify, font_name=宋体, font_size=小四号, ...
[SUCCESS] 成功应用正文格式到 X 个段落
```

### 步骤4: 分析问题

根据日志输出，判断问题出在哪个环节：

#### 情况A: 没有看到任何 [DEBUG] 日志
**原因**: 后端没有使用新编译的代码
**解决**: 
1. 确认后端已重启
2. 确认使用的是新编译的 server.exe
3. 检查是否有多个后端进程在运行

#### 情况B: 规范化后的规则仍包含中文键名
**原因**: normalizeFormatRules 函数没有正确工作
**解决**: 
1. 检查数据库中的 format_rules JSON 结构
2. 提供完整的 format_rules JSON 给我分析

#### 情况C: 段落分类错误
**原因**: intelligentClassifyParagraph 函数分类逻辑有问题
**示例**: "1 结论" 被分类为 "body" 而不是 "heading_1"
**解决**: 
1. 提供被错误分类的段落文本
2. 我会调整分类规则

#### 情况D: 格式规则未找到
**日志示例**: `[WARN] 未找到 headings 规则，可用的键: [...]`
**原因**: 规范化后的键名不匹配
**解决**: 
1. 检查 "可用的键" 列表
2. 调整 normalizeFormatRules 中的键名映射

#### 情况E: 格式应用但效果不对
**原因**: 格式参数值解析错误或 UniOffice API 限制
**解决**: 
1. 检查 [Format] 日志中的 twips 值是否正确
2. 可能需要调整解析函数或使用不同的 API

## 需要提供的信息

请提供以下信息以便我诊断：

### 1. 完整的后端日志
从 `[DEBUG] 开始增强格式修正` 到 `[DEBUG] 精确格式修正完成` 的所有日志

### 2. 数据库中的格式规则
```sql
SELECT format_rules FROM format_templates WHERE university_name = '重庆工程学院';
```

### 3. 问题截图
- 修正前的文档格式
- 修正后的文档格式
- 标注哪些地方格式不对

### 4. 具体的格式问题
例如：
- "1 结论" 应该是黑体、三号、加粗、左对齐，但实际是 ___
- 正文应该是宋体、小四号、1.5倍行距、两端对齐、首行缩进2字符，但实际是 ___

## 快速测试命令

### 查看后端日志（实时）
```bash
# Windows PowerShell
Get-Content backend.log -Wait -Tail 50

# 或者直接在运行后端的控制台查看
```

### 过滤特定日志
```bash
# 只看 DEBUG 日志
Select-String -Path backend.log -Pattern "\[DEBUG\]"

# 只看 Format 日志
Select-String -Path backend.log -Pattern "\[Format\]"

# 只看 Classify 日志
Select-String -Path backend.log -Pattern "\[Classify\]"
```

## 临时解决方案

如果格式修正仍然不工作，可以尝试：

### 方案1: 使用 Python 服务
系统中有 Python 格式修正服务，可能更稳定：
```bash
cd backend/python_service
docker-compose up
```

### 方案2: 手动验证格式规则
使用提供的测试脚本验证格式规则是否正确：
```bash
cd backend
go run test_format_rules.go
```

## 下一步行动

1. ✅ 重启后端（使用新编译的代码）
2. ⏳ 上传论文并触发格式修正
3. ⏳ 收集完整的日志输出
4. ⏳ 提供日志和问题描述
5. ⏳ 根据日志分析问题并修复
