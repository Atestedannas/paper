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

普通测试账号：
test@qq.com
Test@123456

管理员账号：
admin
Admin@123456


=== 使用说明 ===
纯plus号池模型：gpt-5.4、gpt-5.5、gpt-image-2、Claude模型使用教程在网站内部，自行查看中转站地址：https://www.inroi.shop教程: https://ocnis2js9qbf.feishu.cn/docx/IysedZgy5oCEQDxgr5JchAnwnFg

=== 卡密内容 ===
f01e77ff69e549c65c21f49dc9aa4bd4

ZTPraXyZQd5zFc9W

# 方式一：直接在命令行中执行
PORT=8002 && sudo lsof -t -i :$PORT | xargs -r sudo kill -9 && docker compose up -d --build





systemctl stop paper.service

cp /opt/paper/paper-server.backup-20260522-132457 /opt/paper/paper-server
chmod +x /opt/paper/paper-server
chown root:root /opt/paper/paper-server

    systemctl start paper.service
    systemctl status paper.service --no-pager
    ss -lntp | grep 8002
curl -i http://127.0.0.1:8002/health













kendrakasparek6004@hotmail.com   gpt密码H132357479h.   邮箱密码spdnu588144

4e&(8%.AuoG4=IjR
https://proton.me/mail



现在要先看新的真实错误，不能再凭“还是不行”猜。你刚改的是 ALIPAY_PUBLIC_KEY，但上一个错误 isv.invalid-signature 其实主要看的是：

服务器 ALIPAY_APP_PRIVATE_KEY 生成的签名
支付宝开放平台 应用公钥 验签
ALIPAY_PUBLIC_KEY 是“支付宝公钥”，它不是这次 isv.invalid-signature 的主因。

先在服务器跑这三条：

sudo systemctl restart paper.service
重新扫码一次，然后立刻查：

sudo journalctl -u paper.service -n 120 --no-pager -l | grep AlipayQRCallback
再查当前服务器私钥生成出来的应用公钥到底是什么：

grep '^ALIPAY_APP_PRIVATE_KEY=' /opt/paper/.env | cut -d= -f2- > /tmp/alipay_private_key.txt

awk 'BEGIN{print "-----BEGIN PRIVATE KEY-----"} {print} END{print "-----END PRIVATE KEY-----"}' /tmp/alipay_private_key.txt > /tmp/alipay_private_key.pem

openssl pkey -in /tmp/alipay_private_key.pem -pubout -outform PEM
把输出中间那段公钥，和支付宝开放平台里的应用公钥对比。必须一模一样。

如果 openssl pkey 报错，再试：

awk 'BEGIN{print "-----BEGIN RSA PRIVATE KEY-----"} {print} END{print "-----END RSA PRIVATE KEY-----"}' /tmp/alipay_private_key.txt > /tmp/alipay_private_key_rsa.pem

openssl rsa -in /tmp/alipay_private_key_rsa.pem -pubout -outform PEM













https://scamalytics.com/
http://shenao.de/blog/



mkdir -p /root/.ssh
chmod 700 /root/.ssh

cat >> /root/.ssh/authorized_keys <<'EOF'
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJVWrVRvx7NigowXSfSu2ghHSctEHbxbVp0MkCE+IB02 github-actions-paper-deploy
EOF

chmod 600 /root/.ssh/authorized_keys





mysql_jZ3fDs




user_ByjBnE
password_rMCW3S
redis_HY4HHH




user_SjWY7y
password_8TQE74



peak@wqwq.eu.cc
1234qwerQWER@







DOMAIN=api.liyian.com

sudo apt update
sudo apt install -y caddy

sudo tee /etc/caddy/Caddyfile >/dev/null <<EOF
$DOMAIN {
reverse_proxy 127.0.0.1:8002
}
EOF

sudo systemctl enable --now caddy
sudo systemctl restart caddy

sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

sudo sed -i "s#^ALIPAY_REDIRECT_URL=.*#ALIPAY_REDIRECT_URL=https://$DOMAIN/api/v1/auth/alipay/callback#" /opt/paper/.env
sudo sed -i "s#^WECHAT_REDIRECT_URL=.*#WECHAT_REDIRECT_URL=https://$DOMAIN/api/v1/auth/wechat/callback#" /opt/paper/.env
sudo sed -i "s#^ALIPAY_NOTIFY_URL=.*#ALIPAY_NOTIFY_URL=https://$DOMAIN/api/v1/payment/alipay/callback#" /opt/paper/.env
sudo sed -i "s#^WECHAT_NOTIFY_URL=.*#WECHAT_NOTIFY_URL=https://$DOMAIN/api/v1/payment/wechat/callback#" /opt/paper/.env

sudo systemctl restart paper.service

curl -i https://$DOMAIN/health
curl -i https://$DOMAIN/api/v1/auth/alipay/qr-session
curl -i https://$DOMAIN/api/v1/auth/wechat/login-url






edigitalchoice.com

注册土耳其apple id https://account.apple.com/
 邮箱网站 https://awamail.com/?lang=zh

无线邮箱:https://sall.cc/zh-CN/moe

fk-4c598568b589410d89577ed54b5e0820

admin@sub2api.org	admin123

https://github.com/adminlove520/AI-Account-Toolkit


路由vpn ：https://github.com/mowei-ie/router-vpn


sk-lnzw0LuaZhk1iPNkEslKfpqQmGGPXeWnqipJC8YpXg95QqZD
https://apihub.agnes-ai.com/v1

sudo systemctl status paper.service --no-pager -l  查看服务运行状态的概况。
sudo ss -lntp | grep 8002  查 8002 端口有没有在监听。
sudo journalctl -u paper.service -n 120 --no-pager -l  查看 paper 服务最近 120 行日志。