-- =====================================================
-- 快速修复脚本：为 papers 表添加 deleted_at 字段
-- 问题：ERROR: column papers.deleted_at does not exist (SQLSTATE 42703)
-- 解决方案：直接执行 SQL 添加缺失的列
-- =====================================================

-- 方法 1: 使用 psql 命令行工具
-- 在终端执行：psql -U postgres -d paper_checker -f add_deleted_at_to_papers.sql

-- 方法 2: 使用 pgAdmin 或其他数据库管理工具
-- 复制以下 SQL 在查询工具中执行

BEGIN;

-- 步骤 1: 添加 deleted_at 列（如果不存在）
ALTER TABLE papers 
ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL;

-- 步骤 2: 创建索引以提高查询性能
CREATE INDEX IF NOT EXISTS idx_papers_deleted_at ON papers(deleted_at);

-- 步骤 3: 验证
SELECT column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_name = 'papers' 
AND column_name = 'deleted_at';

COMMIT;

-- 执行成功后，重启后端服务即可
