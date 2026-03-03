@echo off
echo ========================================
echo 测试Python服务连接
echo ========================================
echo.

echo 测试服务是否运行...
curl -s http://localhost:8003 >nul 2>&1
if errorlevel 1 (
    echo ❌ Python服务未运行
    echo.
    echo 请先启动Python服务:
    echo   cd backend
    echo   start_python_service.bat
    echo.
    pause
    exit /b 1
)

echo ✅ Python服务正在运行
echo.
echo 服务地址: http://localhost:8003
echo.
echo 可用的API端点:
echo   POST /format  - 格式修正
echo   POST /check   - 格式检查
echo.
pause
