# 论文格式修改Go后端

#### 介绍
{**以下是 Gitee 平台说明，您可以替换此简介**
Gitee 是 OSCHINA 推出的基于 Git 的代码托管平台（同时支持 SVN）。专为开发者提供稳定、高效、安全的云端软件开发协作平台
无论是个人、团队、或是企业，都能够用 Gitee 实现代码托管、项目管理、协作开发。企业项目请看 [https://gitee.com/enterprises](https://gitee.com/enterprises)}

#### 软件架构
软件架构说明


#### 安装教程

1.  xxxx
2.  xxxx
3.  xxxx

#### 使用说明

1.  xxxx
2.  xxxx
3.  xxxx

#### 闭环修正接口文档

已新增闭环修正接口，支持“检查 -> 自动修正 -> 再次检查”直至差异为 0 或达到最大循环次数。
详情请参见：`backend/docs/close_loop_api.md`

#### 参与贡献

1.  Fork 本仓库
2.  新建 Feat_xxx 分支
3.  提交代码
4.  新建 Pull Request


#### 特技

1.  使用 Readme\_XXX.md 来支持不同的语言，例如 Readme\_en.md, Readme\_zh.md
2.  Gitee 官方博客 [blog.gitee.com](https://blog.gitee.com)
3.  你可以 [https://gitee.com/explore](https://gitee.com/explore) 这个地址来了解 Gitee 上的优秀开源项目
4.  [GVP](https://gitee.com/gvp) 全称是 Gitee 最有价值开源项目，是综合评定出的优秀开源项目
5.  Gitee 官方提供的使用手册 [https://gitee.com/help](https://gitee.com/help)
6.  Gitee 封面人物是一档用来展示 Gitee 会员风采的栏目 [https://gitee.com/gitee-stars/](https://gitee.com/gitee-stars/)
fixme  
7.    使用unioffice   存在 摘要  目录 部分不能很好的识别 ！  然后 修改过后的存在 多行的空格！
8.  使用go -> python  不能  好像没有修改格式！
9. 二级标题  不能设置字体  大小  小四 小三   对齐方式 
10. MathJax
    //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

//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

//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



模态识别 + 规则引擎
视觉感知（Computer Vision）： 将文档页面转为图片，利用布局分析模型（如 LayoutLM）识别出哪里是页眉、哪里是正文、哪里是参考文献。这模拟了人的“视觉识别”。

语义理解（NLP）： 利用大模型（GPT-4o 或 DeepSeek）提取学校要求文档中的关键约束（如：一级标题黑体三号居中）。

精确执行（DOM 操作）： 根据识别结果，定位到对应的 XML 节点进行参数强制重写。