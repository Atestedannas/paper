// Package formatengine 存放论文格式修正的编译期开关（改常量后需重新 go build）。
//
// 上传接口 UploadPaper 异步调用 QuickV2Fix / FixPaperFormat，最终进入 fileprocessor.ApplyCorrectionsV2；
// 引擎分支在该函数内根据本包常量决定，无需在 Handler 里重复判断。
package formatengine
