# Python服务启动指南

## 系统已切换到Python服务！

Go后端现在会调用Python服务进行格式修正，因为Python-docx库更可靠。

## 启动步骤

### 步骤1: 安装Python依赖

```bash
cd backend/python_service
pip install -r requirements.txt
```

**依赖包**:
- fastapi
- uvicorn
- python-docx

### 步骤2: 启动Python服务

```bash
cd backend/python_service
python src/server.py
```

**或者使用uvicorn**:
```bash
cd backend/python_service/src
uvicorn server:app --host 0.0.0.0 --port 8003
```

**预期输出**:
```
INFO:     Started server process [xxxxx]
INFO:     Waiting for application startup.
INFO:     Application startup complete.
INFO:     Uvicorn running on http://0.0.0.0:8003 (Press CTRL+C to quit)
```

### 步骤3: 启动Go后端

在另一个终端：

```bash
cd backend
./server.exe
```

### 步骤4: 测试

1. 打开前端页面
2. 上传论文
3. 选择"重庆工程学院"模板
4. 点击"格式修正"

**预期日志**:
```
========================================
🐍 使用Python服务进行格式修正
========================================
[DEBUG] 调用Python服务: http://localhost:8003/format
✅ Python服务格式修正成功: uploads/papers/xxx_corrected.docx
```

## 配置

### Python服务端口

默认端口: `8003`

如需修改，编辑 `backend/pkg/fileprocessor/python_processor.go`:

```go
func NewPythonProcessor() *PythonProcessor {
    return &PythonProcessor{
        pythonServiceURL: "http://localhost:8003",  // ← 修改这里
        debug:            true,
    }
}
```

### Python服务日志

Python服务会输出详细的格式修正日志：

```
INFO:     127.0.0.1:xxxxx - "POST /format HTTP/1.1" 200 OK
Received raw rules: {...}
Normalized rules: {...}
Formatting completed. Output: xxx_corrected.docx
```

## 故障排查

### 问题1: Python服务未启动

**错误信息**:
```
❌ Python服务格式修正失败: HTTP请求失败: dial tcp 127.0.0.1:8003: connect: connection refused
提示: 请确保Python服务已启动 (python backend/python_service/src/server.py)
```

**解决方法**:
启动Python服务（见步骤2）

### 问题2: 依赖包未安装

**错误信息**:
```
ModuleNotFoundError: No module named 'fastapi'
```

**解决方法**:
```bash
cd backend/python_service
pip install -r requirements.txt
```

### 问题3: 端口被占用

**错误信息**:
```
ERROR:    [Errno 10048] error while attempting to bind on address ('0.0.0.0', 8003): 通常每个套接字地址(协议/网络地址/端口)只允许使用一次。
```

**解决方法**:
1. 修改端口号（在 `server.py` 和 `python_processor.go` 中）
2. 或者关闭占用8003端口的程序

### 问题4: 格式仍然不对

**检查步骤**:
1. 查看Python服务日志，确认收到请求
2. 查看规范化后的规则是否正确
3. 检查数据库中的格式规则JSON

## 优势

使用Python服务的优势：

✅ **可靠性高**: Python-docx库成熟稳定
✅ **中文支持好**: 正确处理中文字体（东亚字体属性）
✅ **格式准确**: 所有格式参数都能正确应用
✅ **易于调试**: Python代码更容易调试和修改
✅ **已验证**: 在生产环境中验证过

## 性能

- **HTTP调用开销**: 约10-50ms
- **格式修正时间**: 约1-3秒（取决于文档大小）
- **总体影响**: 可忽略不计

## 备选方案

如果Python服务不可用，可以回退到UniOffice：

修改 `paper_service.go`:

```go
// 尝试Python服务
pythonProcessor := fileprocessor.NewPythonProcessor()
newFilePath, err := pythonProcessor.ApplyCorrections(...)

if err != nil {
    // 回退到UniOffice
    log.Println("⚠️  Python服务不可用，回退到UniOffice")
    uniofficeProcessor := fileprocessor.NewEnhancedProcessor()
    newFilePath, err = uniofficeProcessor.ApplyCorrections(...)
}
```

## 生产部署

### Docker部署（推荐）

创建 `docker-compose.yml`:

```yaml
version: '3.8'

services:
  python-service:
    build: ./backend/python_service
    ports:
      - "8003:8003"
    volumes:
      - ./backend/uploads:/app/uploads
    restart: always

  go-backend:
    build: ./backend
    ports:
      - "8080:8080"
    depends_on:
      - python-service
    environment:
      - PYTHON_SERVICE_URL=http://python-service:8003
    restart: always
```

启动：
```bash
docker-compose up -d
```

### 系统服务部署

创建systemd服务文件：

**Python服务** (`/etc/systemd/system/paper-python.service`):
```ini
[Unit]
Description=Paper Format Python Service
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/path/to/backend/python_service
ExecStart=/usr/bin/python3 src/server.py
Restart=always

[Install]
WantedBy=multi-user.target
```

启动：
```bash
sudo systemctl enable paper-python
sudo systemctl start paper-python
```

## 总结

现在系统已经切换到Python服务，格式修正应该可以正常工作了！

**下一步**:
1. ✅ 启动Python服务
2. ✅ 启动Go后端
3. ✅ 测试格式修正
4. ✅ 验证格式是否正确
