// Package formatengine 存放论文格式修正的编译期开关（改常量后需重新 go build）。
//
// 上传接口 UploadPaper 异步调用 QuickV2Fix / FixPaperFormat，最终进入 fileprocessor.ApplyCorrectionsV2；
// 引擎分支在该函数内根据本包常量决定，无需在 Handler 里重复判断。
package formatengine

// UseWordZero 为 true 时，在 Shell 填充之后、Python StyleFormatter 之前优先使用 WordZero
// 将黄金模板「页面设置」同步到用户稿（写出路径仍为 corrected/<原名>_styled.docx）；失败则回退原链路。
//
// 注意：当前 WordZero 路径不等价于全文样式/normative/hdrftr_merge，仅用于试验或轻量版心对齐。
const UseWordZero = false
