package utils

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// Response 统一响应结构体
type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

// SuccessResponse 成功响应
func SuccessResponse(c *gin.Context, msg string, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code: http.StatusOK,
		Msg:  msg,
		Data: data,
	})
}

// ErrorResponse 错误响应
func ErrorResponse(c *gin.Context, code int, msg string, detail string) {
	response := Response{
		Code: code,
		Msg:  msg,
	}

	if detail != "" {
		response.Data = map[string]string{"detail": detail}
	}

	c.JSON(code, response)
}

// CreatedResponse 创建成功响应
func CreatedResponse(c *gin.Context, msg string, data interface{}) {
	c.JSON(http.StatusCreated, Response{
		Code: http.StatusCreated,
		Msg:  msg,
		Data: data,
	})
}

// NoContentResponse 无内容响应
func NoContentResponse(c *gin.Context, msg string) {
	c.JSON(http.StatusNoContent, Response{
		Code: http.StatusNoContent,
		Msg:  msg,
	})
}

// Legacy compatibility functions for old handlers

// Success 成功响应（兼容旧版本）
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"msg":  "success",
		"data": data,
	})
}

// BadRequest 错误请求响应（兼容旧版本）
func BadRequest(c *gin.Context, msg string) {
	c.JSON(http.StatusBadRequest, gin.H{
		"code": http.StatusBadRequest,
		"msg":  msg,
	})
}

// InternalServerError 服务器错误响应（兼容旧版本）
func InternalServerError(c *gin.Context, msg string) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"code": http.StatusInternalServerError,
		"msg":  msg,
	})
}

// Created 创建成功响应（兼容旧版本）
func Created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, gin.H{
		"code": http.StatusCreated,
		"msg":  "created",
		"data": data,
	})
}

// Unauthorized 未授权响应（兼容旧版本）
func Unauthorized(c *gin.Context, msg string) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"code": http.StatusUnauthorized,
		"msg":  msg,
	})
}

// NotFound 未找到响应（兼容旧版本）
func NotFound(c *gin.Context, msg string) {
	c.JSON(http.StatusNotFound, gin.H{
		"code": http.StatusNotFound,
		"msg":  msg,
	})
}

// StringToInt 将字符串转换为整数，如果转换失败则返回默认值
func StringToInt(s string, defaultValue int) int {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	if err != nil {
		return defaultValue
	}
	return result
}

// CreateDirIfNotExists 创建目录（如果不存在）
func CreateDirIfNotExists(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}
