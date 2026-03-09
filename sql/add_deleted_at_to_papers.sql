-- 为 papers 表添加 deleted_at 字段（软删除支持）
-- 执行时间：2026-03-06
-- 问题描述：Paper 模型中定义了 DeletedAt 字段，但数据库表缺少该列

-- 步骤 1: 检查列是否已存在
SELECT EXISTS (
    SELECT 1 FROM information_schema.columns 
    WHERE table_name = 'papers' AND column_name = 'deleted_at'
) AS column_exists;

-- 步骤 2: 添加 deleted_at 列（如果不存在）
ALTER TABLE papers 
ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL;

-- 步骤 3: 为 deleted_at 创建索引（提高软删除查询性能）
CREATE INDEX IF NOT EXISTS idx_papers_deleted_at ON papers(deleted_at);

-- 步骤 4: 验证列已添加成功
SELECT column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_name = 'papers' AND column_name = 'deleted_at';

-- 步骤 5: 验证索引已创建成功
SELECT indexname, indexdef
FROM pg_indexes
WHERE tablename = 'papers' AND indexname = 'idx_papers_deleted_at';

-- 完成！现在可以正常使用软删除功能了
