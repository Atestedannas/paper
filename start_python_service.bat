@echo off
echo ========================================
echo 启动Python格式修正服务
echo ========================================
echo.

cd python_service

echo 检查Python环境...
python --version
if errorlevel 1 (
    echo 错误: 未找到Python，请先安装Python 3.7+
    pause
    exit /b 1
)

echo.
echo 检查依赖包...
pip show fastapi >nul 2>&1
if errorlevel 1 (
    echo 安装依赖包...
    pip install -r requirements.txt
)

echo.
echo ========================================
echo 启动服务 (端口: 8003)
echo ========================================
echo.
echo 提示: 按 Ctrl+C 停止服务
echo.

python src/server.py
