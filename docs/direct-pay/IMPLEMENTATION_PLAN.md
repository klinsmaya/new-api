# new-api 微信 / 支付宝官方直连支付：Claude 执行方案

版本：1.0  
编制日期：2026-10-02  
用途：交给 Claude Code 或其他具备仓库读写、终端与测试能力的执行代理。  
交付性质：实施规格与验收任务；不表示已实现、已构建、已联调或已上线。

---

## 0. 给执行代理的任务指令

你是本项目的实现工程师。请在用户提供的 new-api 工作区中完成本方案，先调查代码与运行基线，再分阶段提交可审查的改动，不要只输出另一份设计。

### 已确定的决策

- 不使用 EPay 协议、EPay 代收平台或外置聚合支付网关；业务直接调用微信支付、支付宝官方接口。
- Go SDK 采用 `github.com/go-pay/gopay`，微信使用 `wechat/v3`；支付宝第一阶段使用支持 PagePay 的 `alipay` 包，不因为名称包含 v3 就擅自替换接口体系。
- 首个充值发布范围：微信 Native 扫码、支付宝电脑网站 PagePay、下单、查单、关单、通知验签、事务入账、补偿查单、后台配置与异常订单查看。
- 支付宝 `trade.precreate` 扫码、支付宝 WAP、微信 H5 是后续可选场景，默认不开启；订阅购买、自动续费、分账、服务商/子商户、多商户路由不属于首个发布范围。
- 受控全额退款作为独立后续变更实现，默认关闭。未证明额度预留、并发消费与退款补偿安全，不得启用真实退款。
- 不直接切换到参考 fork，不整包合并第三方 PR，不复制其镜像、CI/CD、品牌、模型渠道等无关变更。
- 保留上游已有支付方式及历史订单的兼容性，但本次新路径不得暗中回退到 EPay。

### 权限与执行边界

可以在已授权工作区读代码、建本地分支、修改文件、运行无真实资金的测试、生成补丁与报告。不得覆盖已有未提交变更，不得把上游仓库作为写入目标，不得擅自修改远程地址、强推共享分支、合并 PR 或部署生产。

真实微信/支付宝付款、退款，以及生产配置与部署变更，必须由操作人员明确授权；不能把“用户同意方案”当作这些操作的授权。不要把商户私钥、API v3 密钥、沙箱账号密码或生产 token 写入提示词、代码、日志、截图和测试报告。

当前未提供用户自己的仓库地址、部署 commit、数据库种类与商户凭据。优先从现有工作区、部署清单和非敏感配置发现这些信息；缺凭据时继续完成本地开发及自动化测试，把官方联调列为 `BLOCKED_EXTERNAL`，不可伪造 PASS。没有工作区时明确记录环境阻塞，不得把公开参考 fork 当成用户项目。

先阅读仓库已有的 `AGENTS.md`、`CLAUDE.md` 和贡献规范；本文件放在 `docs/direct-pay/IMPLEMENTATION_PLAN.md`，不要覆盖根目录原有指令文件。

---

## 1. 依据、参考代码与复用边界

### 1.1 固定参考，不追随第三方 main 自动变化

| 参考 | 参考定位 | 使用范围 |
|---|---|---|
| `QuantumNous/new-api` | 用户实际部署版本优先；不存在部署基线时记录所选官方稳定 tag 与完整 SHA | 业务基线、权限、TopUp、用户额度、缓存、日志、迁移和前端结构 |
| `chunfeng789/new-api` | 固定参考 SHA `c82692fd6ce86bef2e6d868ae658e31f206edbcd` | 原生扫码接线、支付页面、查单、事务结算、补偿与退款状态处理思路 |
| 上游 PR #7055 | 原生微信/支付宝扫码；源提交 `0af9a3e33f44c9fc86fca648a9fe065a86ee5ecd` | 理解最早的 GoPay 接入差异，不代替较新参考代码 |
| 上游 PR #5297 | `kavoj/35sz-api`，SHA `97b18e15f14a0fc188e64e638f7f2c5c5087f455` | PagePay/WAP、微信 Native/H5、证书配置和支付场景接线；不引入其无关功能 |
| 上游 PR #4952 | `ZacBi/new-api`，SHA `42124002a13f1157fa5b50d7cfc98b403f4a8fd1` | PagePay 参数与页面交互；不复用其回调入账顺序 |

参考 fork 与 PR 是未完成本项目安全验收的输入，不是生产认证。此前核查 #7055 已关闭且未合并到上游，作者说明是误投上游；这不代表其代码不存在于作者 fork，也不代表上游认可或否决其安全性。[R1–R5]

### 1.2 已发现、必须避免继承的问题

1. 原生扫码参考实现把通知/查询缩减成“订单号 + paid 布尔值”，不足以校验金额、应用和商户身份。必须保留并核对完整的支付证明。
2. 网络超时、连接中断或响应验签异常，不等于渠道明确拒单；不能统统标记成不可恢复的失败状态。
3. 支付宝 #4952 示例提前 ACK，然后分开更新订单和额度。新实现采用可靠事件接收与原子入账，不复制这一顺序。
4. 微信平台证书模式与微信支付公钥模式必须分开配置；公钥 ID 保留 `PUB_KEY_ID_` 前缀。
5. GoPay 的支付宝文档对 `AutoVerifySign` 标注证书模式支持。普通公钥模式的查单、退款等服务端响应必须显式按锁定版本接口完成验签，不能仅调用一次该函数就认定验签已经生效。启动时验证配置；用伪签名响应测试证明会拒绝。[R6–R8]

### 1.3 依赖策略

`gopay v1.5.123` 是本方案已核对的候选固定版本，其 `go.mod` 声明 Go 1.25.0。执行时核对目标 new-api 的 Go/Bun/Node/数据库版本、传递依赖与漏洞报告，然后固定准确版本及校验和。不能使用 `go get -u ./...`，不能无记录改成 `@latest`。新版本有必要时单独形成依赖升级变更，不与支付功能混成一次无边界升级。[R9]

GoPay README 记录了 v1.5.119 起默认开启 TLS 校验的调整。无论沙箱还是生产，本方案均禁止关闭 TLS 验证；本地 HTTPS 模拟使用测试 CA/可信测试客户端，而不是 `InsecureSkipVerify=true`。[R10]

---

## 2. 阶段 P0：必须先完成的基线调查

先创建 `docs/direct-pay/BASELINE.md` 与 `docs/direct-pay/PROVENANCE.md`，记录：

- 用户仓库 remote、当前 branch、完整 commit、工作区状态、部署镜像 digest 或部署 tag。
- 上游 remote 与完整基线 SHA；当前部署与上游差异。不要自动升级现有线上版本后才开始做支付。
- 后端路由、认证、管理员权限、支付可用性检查与合规确认入口。
- `TopUp`、订单查询/人工补单入口、钱包额度上限、消费预扣、额度缓存、异步扣费、日志写入的实际实现。
- 数据库类型与版本、Redis 使用方式、多实例情况、时区、已有迁移机制。
- 实际启用的前端主题及构建路径：检测 `web/src`、`web/default`、`web/classic` 等，不能复制参考 fork 的路径假设。
- 当前测试结果及既有失败。记录命令、退出码和原因，不得把原有失败与本次新增失败混淆。
- GoPay 引入前后的直接/间接依赖变化、工具链兼容性与许可证/来源记录。

上游调查中特别检查 `RechargeEpay`、`creditTopUpQuota`、`syncCreditUserQuotaCache` 或同等功能。已核对的上游源码使用数据库事务入账，并在提交后同步额度缓存增量；缓存补偿不能重复执行增量，也不能直接用数据库余额覆盖带有在途预扣的缓存。[R11–R12]

P0 不因缺少商户密钥而停止。真实凭据仅影响官方联调和生产验收。

交付：基线报告、差异清单、集成点清单、测试基线、实施分支。执行者根据实际路径更新后续文件清单，但不得削弱验收条件。

---

## 3. 架构：进程内隔离模块，最小化上游修改

采用 new-api 内部模块，不新建外置支付微服务、不让外部服务直接写用户余额。

```text
钱包 / 管理界面
        |
new-api 鉴权和 direct-pay HTTP 入口
        |
支付编排：订单、状态、幂等、事件、补偿
        |                            |
GoPay provider adapters             WalletBridge / Store
        |                            |
微信 / 支付宝官方接口                TopUp + User + 缓存 + 日志
        |
通知 -> 验签与归一化 -> 数据库 Inbox -> 同一结算入口
                                      ^
                         后台主动查单 --|
```

建议结构（以 P0 探测结果调整）：

```text
types/directpay/                 # 金额、状态、订单/支付证明结构；不依赖 SDK 或 ORM
service/directpay/               # 编排、验收规则、重试、后台任务
service/directpay/provider/      # GoPay 微信、支付宝适配器
model/directpay_*.go             # 新表、事务、WalletBridge 的上游适配
controller/directpay_*.go        # 用户、管理、回调入口
setting/directpay/               # 非敏感设置与凭据引用
web/<当前主题>/.../direct-pay/    # 支付页、订单、管理组件
internal/testsupport/directpay/  # 仅测试使用的模拟与签名样本生成
scripts/directpay/               # 本地测试、联调检查、升级影响报告
docs/direct-pay/                 # 基线、方案、验收、运维、升级文档
```

保持依赖单向：共享类型不导入 service/model；model 不反向导入 service。组合入口负责装配服务和存储。SDK 的 BodyMap、response、BizErr 不得扩散到控制器和钱包业务层。

允许的上游小范围接线：路由注册、支付方法展示、后台设置入口、模型迁移注册、定时任务启动、现有钱包模型桥接、实际主题中的充值按钮。余额一致性确需额外修改时必须单独说明原因并增加回归测试，不为了“零冲突”牺牲正确性。

**禁止修改：** LLM relay、模型定价/路由、上游品牌、模块路径、镜像发布仓库、默认生产端口、现有支付协议含义。已有钱包/预扣机制的必要修复另成小变更，不顺手做整体重构。

---

## 4. 支付产品、provider 与配置标识

区分提供方、产品场景和环境，不用字符串 `alipay` 同时表示 EPay 子渠道和官方直连。

| 字段 | 首阶段值 |
|---|---|
| provider | `wechat_direct` / `alipay_direct` |
| method | `wechat_native` / `alipay_page` |
| provider_environment | `live` / `sandbox` / `mock`，受实例部署规则限制 |
| deployment_tier | `local` / `ci` / `staging` / `production` |
| business_type | `topup` |
| settlement_currency | `CNY` |

环境组合：微信只允许 `mock` 或 `live`；支付宝可 `mock` / `sandbox` / `live`。拒绝配置出名为 `wechat sandbox`、实际却调用真钱网关的模式。

同一个生产实例不同时承载 mock/sandbox 钱包。不同环境使用不同数据库、Redis namespace、域名、凭据、订单前缀；生产二进制/启动校验拒绝 mock 注册和测试收银接口。

配置分为：

- **非敏感项：** 功能开关、商户/应用 ID、公钥 ID、验签模式、最小/最大充值策略、账户代号、受信回调 base URL、订单有效期、后台查询频率、灰度用户范围。
- **凭据：** 商户/应用私钥、API v3 密钥等由 Secret 挂载文件或现有秘密管理设施注入；UI 只展示“已配置/有效性/版本”，不回显私钥，不允许管理员界面输入任意服务器文件路径。
- **公开证书/公钥：** 不是签名私钥，但其来源必须受信、完整性受控，并支持轮换。

账户配置版本不可变：新订单记录 `account_id`、`account_revision`、app/merchant 身份快照。轮换新增版本，不覆盖历史绑定。旧订单仍可按旧绑定查询和结算；验证器支持在受控窗口内验证有效的新旧平台公钥/证书。紧急吊销优先于便利性，不保留已确认泄露的密钥继续自动操作。

网关地址使用 SDK 与官方配置允许列表，不能从用户请求传入；`notify_url`、`return_url` 由服务端受信配置构造，不使用未校验 Host / X-Forwarded-Host，更不接受任意跳转地址。

---

## 5. 数据设计与迁移

优先新增独立 `directpay_*` 表，保留上游 TopUp 作为充值业务记录，避免不断扩展上游通用订单状态。

### 5.1 `directpay_orders`

最少字段：

| 类别 | 字段 / 约束 |
|---|---|
| 关联 | 本地 order_id、全局唯一 merchant_order_no、top_up_id（1:1 唯一）、user_id、business_type |
| 提供方 | provider、method、environment、account_id、account_revision |
| 金额 | `money_minor int64`（人民币分）、`currency=CNY`；必须大于 0，受产品与钱包上限约束 |
| 额度 | `quota_to_credit int64`、`credited_quota int64`；以目标上游实际额度类型进行安全范围校验 |
| 计价快照 | 请求充值数量、展示币种/模式、价格、分组倍率、折扣、舍入策略及 pricing_revision |
| 渠道身份 | app_id / merchant_id 或 seller_id 快照、可空 provider_transaction_id |
| 生命周期 | payment_state、settlement_state、create/expire/paid/settled 时间、最后可验证渠道状态 |
| 恢复 | next_query_at、attempt_count、last_error_class、行 version / 租约字段、checkout 信息及有效期 |
| 审计 | 创建请求幂等键摘要、参数摘要、关联 trace_id |

唯一约束至少包括：

- merchant_order_no 全局唯一；top_up_id 唯一。
- 用户 + 业务类型 + 幂等键唯一，同一键不同参数返回冲突。
- environment + provider + account_id + 非空 provider_transaction_id 唯一，避免同一渠道流水匹配两笔本地订单。

未知渠道流水使用真正的 NULL，不用空字符串制造唯一索引冲突；跨 SQLite/MySQL/PostgreSQL 验证行为，不依赖未兼容的部分索引语法。

订单号按两个渠道的长度/字符约束生成；建议使用短环境前缀加随机/ULID 标识，微信整串不超过 32 字符。订单号不包含用户手机号或其他个人信息，不从时间戳单独生成。

### 5.2 `directpay_events`：可靠 Inbox

保存已经验签的归一化通知/查单证据、事件来源、事件去重键、order_id、处理状态、重试时间与失败原因。以官方事件 ID 优先去重；没有稳定 ID 时使用已验证业务字段生成确定性键。**重复判定也必须先完成验签**。

原始数据按最小化原则保留：敏感字段脱敏；确需保存原始通知则加密、限权、设置保留期。日志不包含完整通知、用户支付身份或签名 URL。不能只保存哈希而丢失可恢复结算必需的归一化字段。

### 5.3 `directpay_ledger`：业务记账幂等与审计

记录 order_id、entry_type、business_ref、quota_delta、cash_minor、provider_transaction_id、created_at。用唯一业务键保证一笔订单仅有一次 `topup_credit`。退款后续使用独立的 reserve/release/complete 业务键。

这张表记录直连业务记账，不替代 new-api 的整个钱包账本，不把它误称为微信/支付宝资金清算凭证。

### 5.4 后台任务与 outbox

持久化后台任务可直接依附 Inbox / order 的处理状态，不为简单任务重复引入外部队列。需要独立审计投递或缓存恢复时可增 `directpay_outbox`，但回放操作必须幂等；禁止用 outbox 无条件重复执行钱包 `INCR`。

### 5.5 TopUp 兼容与迁移规则

- 在同一个数据库事务中创建本地 directpay_order 和上游 TopUp；成功落库后才请求渠道。
- TopUp 的 `Amount` 保留上游既有语义；`Money` 若仍为 float64，仅做兼容展示，由 `money_minor` 转换，不作为新支付比较依据。
- directpay 的中间状态保留在新表；TopUp 仅映射现有可识别状态。`success` 只能代表额度结算完成，不代表“刚验签”或“刚收到通知”。
- 旧订单缺少直连扩展数据时维持旧路径；不猜测历史金额、汇率、额度和 provider。
- 管理员原有“手工补单/完成充值”入口必须识别直连订单并转入同一可审计流程，不能绕过 provider 核验和记账唯一键。
- 迁移采用版本化、幂等、扩展式迁移；不删旧列、不重命名旧状态、不做破坏式回填。验证升级前订单仍可查询与正常结算。

---

## 6. 计价与支付状态机

### 6.1 金额规则

输入的充值数量只是购买意图。用户 ID 从登录态读取，应付金额和额度由后端计算；不能接受前端传入的 user_id、金额、折扣、汇率或额度作为可信值。

复用并测试当前上游的价格、展示类型、分组与折扣语义，用 decimal/定点数计算，在最终人民币分边界只舍入一次，固化结果。结算时只读取订单快照，不重算当前配置。配置变化导致支付前展示金额不同，应让用户重新确认，不能静默多收。

渠道金额比较：微信核对订单 `amount.total`，不是把扣券后的 `payer_total` 当成应付总额；支付宝核对 `total_amount`，不拿 `receipt_amount` 或 `buyer_pay_amount` 替代订单总金额。支付宝元字符串以严格十进制解析为分，不经 float64。[R13–R14]

### 6.2 分离“渠道支付”和“本地到账”

payment_state 建议：

```text
created -> creating -> pending -> paid
                 \-> unknown
pending/unknown -> close_requested -> closed
created/creating -> create_rejected  # 仅确定性、可证明未创建的拒单
任意异常证据 -> review_required      # 不自动加余额，保留处理入口
```

settlement_state 建议：`uncredited / credit_pending / credited / review_required`。

“paid + credit_pending”在 UI 显示“已支付，到账处理中”；不能显示“充值完成”。已到账订单不被重复 NOTPAY、关闭消息或旧通知覆盖。

`expired` 可以是 UI 状态或本地处理意图，**不是确认渠道关闭的充分依据**。微信 `time_expire` 是最晚付款时间，并不等于订单关闭时间；应结合查询和关单处理。[R13]

### 6.3 请求与错误分类

| 结果 | 行为 |
|---|---|
| 本地参数/配置错误、尚未调用渠道 | 不发起支付，返回可读错误 |
| 渠道明确拒绝，能够证明未创建订单 | 记录确定失败及原因 |
| 超时、连接中断、5xx、429、SDK 解析失败、响应验签失败 | `unknown`，按同一订单号查单；不得直接新建重复订单 |
| 查询 `NOTPAY` | 保持待支付，按有效期处理 |
| 查询 `NOT_FOUND` | 区分产品与阶段；不自动等同“永远不会付款” |
| 查询确认已支付，证明完整匹配 | 走唯一结算入口 |
| 关单与支付竞争，返回已支付或关单结果不确定 | 再查单确认；不得覆盖已支付事实 |

支付宝 PagePay 生成的是签名支付请求/跳转信息，不等于服务端已经创建远端交易；用户未进入收银台前查不到交易可能是正常现象。不得把“本地生成 URL 成功”当成付款成功，也不能因查不到交易立即否定仍然有效的收银台请求。

收银台/二维码失效与业务订单关闭分别记录；UI 倒计时结束时停止引导付款，但后台仍处理有效的迟到付款通知。异常的“本地关闭后收到成功”进入主动复核，匹配的真实付款必须得到到账或受控退款处置，不能丢弃。

---

## 7. Provider 契约与验签

统一业务接口（这是内部设计，不是声称 SDK 已提供这些同名方法）：

```text
CreateCheckout(orderSnapshot) -> Checkout | ClassifiedError
QueryOrder(orderSnapshot) -> OrderObservation | ClassifiedError
CloseOrder(orderSnapshot) -> CloseObservation | ClassifiedError
VerifyAndParseNotification(headers, originalBody, trustedAccount)
    -> VerifiedPaymentEvent | VerificationError
# 后续独立扩展
SubmitRefund(refundSnapshot) -> RefundObservation | ClassifiedError
QueryRefund(refundSnapshot) -> RefundObservation | ClassifiedError
```

OrderObservation / VerifiedPaymentEvent 必须保留：provider、environment、绑定账户、商户订单号、渠道流水号、订单总金额、币种、支付状态、支付时间、可获得的 app/merchant/seller 信息、证据来源和验证结果。不得退化为 `(tradeNo, paid bool)`。

不同接口的返回字段并不完全相同。为每个适配器写“身份/金额证据映射表”：回调使用其实际返回的 app/merchant/seller 字段；查询中未返回的身份字段由已认证请求的账户上下文和响应关联保证，不能伪造字段后当作“已核对”。证据不足时不自动入账。

### 微信

- 商户私钥与商户证书序列号用于请求签名；微信支付公钥或平台证书用于响应/通知验签；API v3 密钥用于通知解密。这几类材料不能混用。
- 默认支持微信支付公钥模式；同时兼容平台证书模式。按 `Wechatpay-Serial` 选择受信 verifier；未知 key ID 只能经过受控刷新/轮换流程，不能跳过验签。
- 验签使用原始 body；再解密与校验 appid、mchid、out_trade_no、transaction_id、amount.total/currency、成功状态及订单账户绑定。
- 校验签名时间戳与时钟偏差，范围按官方规范与部署容差设定；校验的是签名时间，不是要求历史订单付款时间必须很新。
- `WECHATPAY/SIGNTEST/` 探测签名必须拒绝，不写入到账事件。
- 原始请求体只读取一次并限制大小；非法 nonce/ciphertext、缺失字段、未知状态必须返回错误而非 panic。[R7, R14]

### 支付宝

- 使用 RSA2，普通公钥和证书模式分别配置与验收。
- 服务端查单、预创建、退款等响应需要可信验签；不能只依赖 HTTPS、错误码或 `err == nil`。
- 普通公钥模式按固定 SDK 的 `VerifySyncSign` 等接口验证查询响应；证书模式按证书链/序列号配置并验证。用“错误公钥、被改金额、假签名响应”证明拒绝路径。
- PagePay 本地生成的签名 URL/表单不是支付证明；浏览器 `return_url` 只用于恢复订单展示，不入账。
- 通知按实际接口字段核对 app_id、seller_id、out_trade_no、trade_no、total_amount、TRADE_SUCCESS / TRADE_FINISHED 等成功状态。
- 表单数据只采用预期来源，不把 URL query 与 POST body 不受控合并；对影响业务校验的重复参数进行拒绝/明确规范化，并确保验签与使用的是同一份数据。[R6, R8]

---

## 8. 可靠通知、事务入账与缓存

### 8.1 默认采用数据库 Inbox，不提前“空 ACK”

流程固定为：

```text
收通知 -> 限流/体积限制 -> 验签 -> 微信解密/支付宝解析
      -> 基础账户身份校验 -> 归一化事件可靠落库
      -> 提交成功后 ACK -> 后台消费 Inbox
      -> 核对本地订单与金额 -> 事务入账
```

微信成功响应采用符合官方要求的 200/204；支付宝采用纯文本 `success`。无效签名不成功响应；数据库持久化失败返回失败，让渠道重试。已验签且已可靠接收的重复通知可以重复成功应答。[R6, R14]

ACK 表示可靠接收，而不是给前端宣称到账。验签后的订单/金额冲突事件可持久化为隔离事件并报警，不得产生额度。不能先 ACK 后只开一个 goroutine；进程重启不能丢事件。

回调接收路径不得访问远端查单、下载不受控资源或持有长事务。当前官方普通支付回调要求在短窗口内应答；内部目标为正常负载下 ACK p99 < 2 秒，这是项目验收指标而非平台 SLA。需要更慢的恢复时使用 Inbox 和后台任务，不阻塞回调。[R14]

### 8.2 一笔充值的数据库原子边界

使用当前数据库支持的行锁或条件更新语义，统一锁顺序。SQLite 不能照搬 PostgreSQL 的 `FOR UPDATE` 假设，必须有真正跨连接的并发测试。

同一数据库事务中完成：

1. 锁定 directpay_order 和对应 TopUp，核对 environment / provider / account / 订单号 / 金额 / 币种 / 流水及成功证据。
2. 若已有相同 `topup_credit` 记账，确认一致后按幂等成功返回；字段冲突不伪装成普通重复。
3. 再次检查用户存在、钱包容量与额度类型边界。
4. 写入唯一的记账记录，并通过 WalletBridge 执行带容量条件的额度增加。
5. 将 directpay settlement 标为 credited，保存 credited_quota 和流水，TopUp 标为 success。
6. 更新事件处理状态；需要的审计/outbox 同事务持久化。

任何一步失败都回滚，不能出现“订单成功但额度没加”或“额度已加但记账不存在”。网络调用不得放在这段事务内。

钱包上限在付款前检查不够：回调时余额可能发生变化。付款已确认但不能加额度时，保留 paid + review_required，告警并进入可审计处置流程，不能静默丢单或无限假报未支付。

### 8.3 余额缓存与在途消费是单独验收项

复用当前上游充值与预扣一致性机制，不只更新 `users.quota`，也不能盲目删除/覆盖缓存。已核对版本有提交后缓存增量同步函数；这不意味着它可作为可重放的幂等 outbox 操作。[R12]

执行者必须提交 `WALLET_CONSISTENCY.md`，说明：数据库提交后崩溃、缓存更新超时、缓存重建与充值同时发生、已有消费预扣时的行为。

硬约束：

- 重复支付通知不能重复增加数据库或 Redis 额度。
- 不把数据库余额覆盖回缓存而抹掉在途预扣。
- 不对结果未知的 Redis 增量操作直接重试。
- 如需要自动重放缓存增量，必须使用事务关联 ID、缓存版本/代际或等价机制；仅加一个与缓存生命周期无关的去重键不足以覆盖缓存重建竞争。
- 不具备安全回放机制时，明确采用当前上游可证明安全的恢复路径，并验证可用余额最终收敛；不能把未处理的一致性窗口写成“原子保证”。
- 支付适配器不能因此重构整个 relay；确需小范围增加钱包版本/恢复机制时单独提交、独立回归，并列入上游升级观察点。

### 8.4 补偿查单与关单

使用持久化 `next_query_at` 和可恢复任务；为每个账户设置限频、退避和抖动。多实例租约只用于减少重复请求，正确性仍由数据库幂等保证。进程内 Mutex 和 Redis 锁不是资金幂等的唯一依据。

浏览器通常每 2–5 秒查询本地状态，不应每次轮询都直接轰击支付平台。后台根据订单年龄和最近查询结果分层查单，页面关闭、回调丢失、服务重启都不影响补偿。

不要因达到某个重试次数永久丢弃已付未处理订单；进入告警和人工队列。订单期限到达后先查询，再关单，再处理关单与付款竞争。停用“新下单”开关不能同时停掉历史通知和查单。

自动查单补偿不等于账单对账。首个版本提供逐单流水审计和渠道账单人工比对流程；每日账单自动拉取与差异核对可作为独立任务，不声称轮询已完成资金清算核对。

---

## 9. HTTP 与前端交互

以下为建议的业务路径，实际前缀与返回格式遵循 P0 检测到的 new-api 规范：

| API | 约束 |
|---|---|
| `GET /api/user/direct-pay/methods` | 返回当前用户可用渠道、最小/最大数量、环境标识；无密钥 |
| `POST /api/user/direct-pay/orders` | 必须登录、限频、幂等键；仅接收充值数量与 method 等购买意图 |
| `GET /api/user/direct-pay/orders/:order_id` | 校验订单归属；返回本地支付与到账状态，不泄露他人订单 |
| `POST /api/user/direct-pay/orders/:order_id/refresh` | 只触发受控查单，不能由用户输入“已支付” |
| `POST /api/user/direct-pay/orders/:order_id/close` | 关闭意图，不直接把渠道付款事实改没 |
| `POST /api/direct-pay/notify/wechat/:account_ref` | 无登录跳转，依靠渠道验签；账户与环境由服务端路由绑定 |
| `POST /api/direct-pay/notify/alipay/:account_ref` | 同上；返回格式不得被统一 JSON 中间件破坏 |
| 管理员订单/事件/查单入口 | 最小权限、审计；不提供“输入金额直接加余额”后门 |

创建返回至少包括：order_id、provider/method、money_minor、currency、quota_to_credit、expires_at、payment_state、settlement_state，以及 checkout 类型。

checkout 类型为：`qr` 或 `redirect`；分别返回二维码内容或已校验目标域名的签名跳转 URL。页面采用服务端返回的权威金额，不自行算汇率。

### 用户侧

- 微信在站内显示二维码、应付人民币金额、预计额度、有效期与状态。
- 支付宝明确标注“前往支付宝”；处理浏览器拦截新窗口、用户关闭收银台、回跳丢失登录态、刷新页面、网络中断等情况。
- 支付成功页面必须等待本地 settlement=credited 后刷新余额；paid+credit_pending 显示到账处理中并允许重查。
- 支付页面刷新后恢复原订单，不自动新建订单；幂等键与订单持久化配合。
- 同一手机上微信 Native 二维码不能被当作微信 H5/JSAPI 体验替代；首版明确支持场景，不宣称移动端已完整覆盖。
- 不向第三方二维码图片服务发送支付串；使用已有前端二维码组件本地渲染。
- 测试环境永久显著显示“模拟支付/支付宝沙箱/真实资金联调”，不能隐藏。

### 管理侧

显示凭据状态、账户身份、开关、回调地址、订单总额、币种、支付/到账双状态、异常原因、最近查询、渠道流水及审计。密钥字段不回填，配置校验错误不可包含私钥原文。

区分 `create_enabled`、历史订单处理、受控退款开关；关掉前者仍接收在途订单通知。UI 仍沿用上游要求的权限和合规确认，不绕过现有门禁。

---

## 10. 联调分层：不把 Mock 冒充官方沙箱

GoPay 微信 v3 文档明确表示不支持沙箱支付；支付宝 GoPay 客户端支持新版沙箱环境。微信使用“本地模拟 + 协议验签测试 + 人工授权小额实付”，支付宝使用“本地模拟 + 官方沙箱 + 人工授权生产冒烟”。[R6–R7, R15]

| 层级 | 环境 | 目标 | 可证明什么 |
|---|---|---|---|
| L0 | 纯本地、无网络、无商户密钥 | 单元测试与属性测试 | 金额、状态、幂等、异常分支 |
| L1 | 本地模拟网关、测试生成密钥、真实 SDK 请求/响应路径 | 签名、微信 AES-GCM、验签失败、超时与重放 | 协议实现和业务边界，不代表官方渠道联调 |
| L2 | 独立支付宝官方沙箱 | PagePay、回调、查询、关单；退款阶段另测 | 官方沙箱互通，不代表生产商户产品已获准 |
| L3 | 独立 staging + live 商户凭据 | 微信 Native 小额真实付款；支付宝生产冒烟 | 真正渠道授权、证书/公钥、回调和到账路径 |
| L4 | 生产灰度 | 白名单用户、监控、异常恢复 | 限定生产范围内运行证据 |

### 10.1 L0 / L1：默认可执行

提供一条统一入口，例如 `make directpay-test`，内部依照目标仓库实际工具运行。默认禁止访问公网支付接口。

- Fake provider 覆盖成功、拒单、未知、关闭、退款受理/成功/失败等语义。
- SDK 协议测试注入 HTTP transport / 本地 httptest 服务，不用运行时生产后门改官方网关。
- 测试生成 RSA 密钥、测试证书、API v3 测试密钥；微信样本按真实签名串和 AES-GCM 格式制作，支付宝样本按实际验签参数格式制作。
- 加密样本只证明本地协议行为，报告必须标为 `PASS_CONTRACT`，不是 `PASS_WECHAT_SANDBOX`。
- 模拟“远端已受理但本地超时”、重复通知、签名有效但金额错误、DB/Redis 故障、回调与查单竞争、升级时通知到达。
- 模拟支付完成入口只存在于测试构建或测试进程；生产构建不能注册该路由，环境变量也不能重新开启。

### 10.2 L2：支付宝官方沙箱操作清单

由操作者在当时可用的支付宝沙箱控制台准备应用、所需支付产品测试能力、测试买家/卖家和平台指定测试客户端；具体账号与客户端限制以控制台当时说明为准，不能用正常钱包或生产私钥代替沙箱配置。[R15]

配置：独立 sandbox AppID、应用私钥、支付宝沙箱公钥/证书、seller 身份、SDK sandbox 开关、受信 HTTPS 回调和 return URL。

完整执行：创建订单 -> 显示/进入 PagePay -> 测试买家付款 -> 异步通知可靠接收 -> Inbox 结算 -> 用户余额刷新 -> 后台按订单号主动查询 -> 核对资金与额度快照。

补充：页面提前关闭、忽略 return_url、临时阻断通知后查单补偿、重复通知、订单过期关单、错误环境密钥拒绝。后续退款变更在沙箱单独验收。

沙箱成功不能替代生产商户产品授权检查。沙箱某接口或客户端不可用时，保留 BLOCKED 说明与对应本地测试证据，不关闭验签“先跑通”。

### 10.3 L3：微信真实资金联调

没有官方 API v3 沙箱的假设下，禁止声称通过“微信官方沙箱”。正式商户与 Native 产品权限、appid 绑定、商户私钥/序列号、微信支付公钥或平台证书、API v3 密钥需要由操作者提供。

使用独立 staging 数据库和专用测试用户，关闭真实上游 AI 消费或接入 mock LLM，不把测试额度混入生产账户。使用商户允许的最小测试金额，例如人民币 0.01 元；金额仅限受控测试配置或专用测试商品，不降低全站生产最小充值。

真实付款由人扫码完成。至少确认：下单、扫码、成功通知、数据库事件与记账、钱包额度、主动查询一致性、生产证书/公钥路径。经授权后测试安全范围内的漏通知恢复。不得为了复现异常主动损坏生产支付服务。

需要退款时另获授权，并通过受控退款流程或商户后台完成；本地已入账额度的处置必须同步登记，不能留下“退款已出款，测试额度仍可消费”。

### 10.4 回调基础设施

采用稳定、公网可达的 HTTPS 联调域名。反向代理保留微信签名头和原始 body；回调路径不要求浏览器登录、不触发 CAPTCHA、不被 CSRF 页面校验误拦，但保留通知验签、体积限制和合理速率保护。

服务器时钟同步；微信时间用带时区的 RFC3339，支付宝按相应 API 的时间格式与时区要求生成。容器运行在 UTC 或 America/Los_Angeles 时，订单过期测试结果应一致。

测试密钥存储在未跟踪 Secret 文件中。`.env.example` 只含变量名和占位符；不得复制真实 `.env`。在生成报告、打包和提交前执行秘密扫描。

---

## 11. 受控退款：独立门槛，默认关闭

本方案不以“SDK 有退款方法”作为自动开放退款的理由。首个充值发布可先上线，退款变更单独验收。完整任务包含退款设计与实现，但真实退款开关受下列条件限制。

首阶段退款能力限定为：管理员发起、整笔全额退款、独立退款号、审计与状态追踪；不做用户无条件自助退款、部分退款、自动争议裁决。

建议状态：

```text
requested -> quota_reserved -> submitting -> processing -> succeeded
                                     \-> unknown
确定未出款的拒绝/关闭 -> release_reserved_quota -> failed_or_closed
证据冲突 / 余额已消费 / 冻结能力不足 -> review_required
```

规则：

- 退款金额来自原订单快照，退款号固定并且唯一；重试同一退款号，不用新号绕开“不确定”。
- 先证明订单已正确入账，再在钱包已有预扣/可用额度机制中原子预留要撤回的额度；余额已被消费时拒绝自动退款，转人工规则。
- 数据库扣减不等于缓存里的可消费额度已冻结。必须覆盖 Redis、在途预扣、并发模型消费、服务重启和结果未知等情况。未证明冻结生效，禁止向渠道发起退款。
- 外部退款请求在数据库事务之外执行；接口“已受理”不是“已退款”。使用退款查询/通知得到最终证据。
- 确认退款成功后，将预留转为最终扣回，不能再扣第二次；确定未出款才释放预留。不确定时保持预留和查单，不能既释放额度又继续退款。
- 生产退款权限、额度上限和审计独立配置；自动退款默认 false。
- 商户后台人工退款也必须登记原渠道流水、退款流水和对应额度处置；不使用“再扣一下余额”这种无幂等手工 SQL。

若目标版本没有可证明安全的钱包预留接口，提交独立的钱包适配变更和测试；无法验证时交付为 `IMPLEMENTED_DISABLED` / `BLOCKED_WALLET_SAFETY`，首个充值发布不因此伪称自动退款已可用。

---

## 12. 自动化测试与验收矩阵

### 12.1 关键用例

| 编号 | 场景 | 必须满足的结果 |
|---|---|---|
| T01 | 创建请求重复、双击、连接重试 | 同一幂等键仅一笔本地订单；不同参数冲突，不重复购买 |
| T02 | 100 次重复通知 + 两个实例同时处理 + 查单并发 | 一次数据库额度增加、一条有效 credit 记账、缓存不重复增额 |
| T03 | 签名有效但金额/币种/商户/app/订单/provider/environment 不匹配 | 不入账，隔离事件与可审计原因 |
| T04 | 同一渠道流水映射另一订单 | 唯一约束和核验拒绝 |
| T05 | 假签名、错误公钥、未知 key、被改 body、微信 SIGNTEST | 不成功接收为有效付款、不产生记账 |
| T06 | SDK 查询返回成功码但签名无效/缺失 | 拒绝作为付款证明，不静默跳过同步验签 |
| T07 | 渠道受理后本地超时/重启 | 不直接失败；原订单查单恢复，无重复下单和重复入账 |
| T08 | Inbox 落库前/后进程崩溃、ACK 丢失、worker 中断 | 前者渠道可重试；后者事件不丢且幂等恢复 |
| T09 | DB 在额度/状态/记账任一写入处失败 | 同事务全部回滚，没有半笔充值 |
| T10 | DB 提交后、缓存同步前崩溃；缓存超时与重建竞争 | 无重复 credit、不抹在途预扣，可验证恢复与告警 |
| T11 | 回调丢失、页面关闭、return_url 不访问 | 后台自动查单仍完成到账 |
| T12 | UI 倒计时过期与支付/关单竞争 | 真实付款不被本地过期状态吞掉 |
| T13 | 下单后调整汇率/分组/折扣/额度换算 | 按下单快照结算，不改历史订单 |
| T14 | 付款前容量足够，回调时钱包到上限或用户状态变化 | 不溢出；paid 的待处理状态和处置可追踪 |
| T15 | 金额 1 分、边界上限、零/负数、超大值、非法小数 | 严格校验，无 float 比较、截断和溢出 |
| T16 | 关闭新下单功能 | 拒新单，但旧通知/查单正常 |
| T17 | 凭据/平台公钥轮换后收到旧订单通知 | 正确绑定账户并按有效轮换规则验证；不换商户查旧单 |
| T18 | 普通用户访问他人订单、管理配置、补单入口 | 权限拒绝，无越权信息 |
| T19 | TLS 假证书、任意 notify/return/gateway 参数 | 拒绝；没有跳过 TLS 或任意 URL 回退 |
| T20 | 上游已有 EPay/Stripe 等功能和历史 TopUp | 本次变更不回归、不重解释旧记录 |
| T21 | 旧 schema 升级、重跑迁移、新旧兼容镜像同时运行 | 数据兼容、通知可持续处理，不重复结算 |
| T22 | UTC、Asia/Shanghai、America/Los_Angeles | 同一绝对到期时刻一致 |
| T23 | 退款受理超时/重复申请/失败释放/成功确认 | 只出款一次、只撤回一次；不确定结果不释放额度 |
| T24 | 退款与模型消费/预扣并发 | 不从可消费额度中漏冻结，不产生重复消费/退款套利窗口 |
| T25 | 生产构建尝试启用 mock、沙箱凭据或测试付款端点 | 启动拒绝或无该能力，无可伪造到账入口 |

T23–T24 属于退款发布门槛，不以跳过标成通过；T02 必须是真正跨进程/跨连接测试，不仅是单进程 goroutine。

### 12.2 测试环境矩阵

目标数据库必须全部关键用例通过；保持 SQLite/MySQL/PostgreSQL 兼容声明时，应在声明支持的每种数据库上运行迁移、事务、唯一索引和并发测试。使用真实容器数据库，不以 SQLite 内存库替代全部数据库。

Redis on/off 均测试；Redis 开启时测试至少两个应用实例。前端运行仓库实际的 typecheck、lint、build 及 E2E；后端运行新增模块测试、受影响 model/controller/service 测试、必要的 race/fuzz 检查。完整仓库测试若有基线失败，分别列出，不掩盖新增回归。

不要在单元测试默认流程访问真实支付网关。带商户凭据的联调任务必须单独触发，并对环境做双重检查。

### 12.3 报告状态必须精确

每项结果用：`PASS_UNIT`、`PASS_CONTRACT`、`PASS_ALIPAY_SANDBOX`、`PASS_LIVE_SMOKE`、`FAIL`、`NOT_RUN`、`BLOCKED_EXTERNAL`、`BLOCKED_WALLET_SAFETY`。

报告记录 commit、SDK/工具链版本、数据库/Redis版本、命令、退出码、测试环境、脱敏订单/流水引用、预期与实际金额/额度、证据位置。没有执行的用例不填 PASS；截图不替代流水和数据库记账证据。

---

## 13. 上游升级策略

### 13.1 分支与来源记录

- `upstream` 仅用于读取 `QuantumNous/new-api`。
- 用户自己的主分支维持已验收支付能力，功能分支按订单/入账/适配器/UI/测试分拆。
- 来源引用固定 SHA，`PROVENANCE.md` 记录复制/改编文件、原作者、许可证、原始 commit 和本地提交；保留必要版权标识。
- 发布分支只接受通过验收的上游升级，不自动跟随 main，不采用第三方 fork 的私有镜像。
- 已共享分支不强制 rebase。升级在独立 worktree / 分支合入新上游；确需整理私有未共享提交时才使用 rebase。

### 13.2 维护 `UPSTREAM_TOUCHPOINTS.yaml`

至少记录如下语义集成点及对应测试：

```yaml
schema_version: 1
upstream_repository: QuantumNous/new-api
baseline_commit: DISCOVER_FROM_TARGET
payment_sdk: github.com/go-pay/gopay
sdk_version: PIN_AFTER_P0
integration_points:
  - name: topup_quote_semantics
    paths: []        # P0 填入实际路径
    symbols: []      # 价格、倍率、展示类型、额度换算
    tests: [T13, T15, T20]
  - name: wallet_atomic_credit_and_cache
    paths: []
    symbols: []      # 事务授信、额度上限、缓存、预扣
    tests: [T02, T09, T10, T14, T24]
  - name: payment_routes_and_permissions
    paths: []
    symbols: []
    tests: [T16, T18, T25]
  - name: migrations_and_old_orders
    paths: []
    symbols: []
    tests: [T20, T21]
  - name: wallet_ui_and_theme_build
    paths: []
    symbols: []
    tests: [T01, T11, T12]
```

最终交付必须填满真实基线、路径和测试名称，不能遗留 DISCOVER/PIN 占位符却宣称已实现。尚未取得目标仓库时仅报告环境阻塞。

升级检查既检查文件变更，也检查依赖、符号签名和业务语义；没有文本冲突不等于兼容。观察范围必须包含间接影响的缓存/预扣/计价文件，不能只看新增 directpay 目录。

### 13.3 每次升级固定流程

1. 固定待升级的官方稳定 tag / 完整 SHA，阅读支付、额度、权限、缓存、前端目录、构建与迁移相关变更。
2. 在独立 worktree 建立升级候选分支，不覆盖正在开发的工作区。
3. 生成旧基线到新基线的影响报告；对照集成点清单，先处理语义差异，再解决文本冲突。
4. 执行编译、测试矩阵和 E2E；重点重复 T02/T10/T13/T16/T20/T21。
5. 在脱敏快照或构造的旧库上升级；重放升级前已创建订单的有效测试通知，验证仍按原快照结算。
6. 验证新旧支付兼容镜像短暂并存、多实例任务、旧事件消费与凭据绑定。
7. 支付 SDK、证书验签、网关或回调逻辑变化时重新做对应官方联调；无相关变化也保留本地完整回归和受控冒烟记录。
8. 人工审查后灰度发布；更新基线 SHA、集成点和验收记录。

可增加定时的“上游变化提醒/测试 PR”，但只生成报告，不自动合并或自动部署。官方上游未来新增同类支付能力时，先比较资金不变量和迁移路径，再决定收敛；不能只因名字相同替换已有订单处理逻辑。

---

## 14. 发布、监控与回滚

### 14.1 首次发布

迁移先验证 -> 部署兼容代码且新支付关闭 -> 配置只读 Secret -> 验证回调/worker -> 白名单与限额 -> 人工授权真实联调 -> 观察事件积压和流水一致性 -> 逐步开放。

后台处理任务与回调在新下单开关打开前已经就绪。只发布自己构建且固定 digest 的镜像，保留源 commit、SDK锁定文件、迁移版本和镜像对应关系。

### 14.2 监控

至少记录创建结果、unknown 订单数量、Inbox 未处理数量与最老年龄、已支付未到账数量、签名失败、金额/身份不匹配、查单失败/限流、关单结果未知、DB幂等冲突、缓存同步异常、退款处理中/异常数量。

日志只含脱敏账户、订单/事件引用、provider、environment、错误类别与 trace_id。监控 label 不直接使用所有订单号，以免高基数。

金额/身份冲突、已支付未到账、退款不确定和缓存一致性异常必须有运维处理手册，不只打印一行日志。查单补偿和银行/渠道账单核对分别列出。

### 14.3 回滚原则

- 先停新下单，不停历史通知、Inbox、查单和必要退款补偿。
- 只回滚到已验证兼容新表/旧订单的支付镜像，不直接回退到不认识 directpay 的纯上游镜像。
- 保留扩展表和迁移版本；第一轮回滚不执行 drop table/drop column。
- 不为回滚代码而恢复较早整库快照，否则会抹去真实付款和消费记录。数据库灾难恢复是独立流程，需要按渠道流水重建与对账。
- 第一次发布还没有旧支付镜像时，必须保留可接收通知和处理在途订单的当前支付 worker/接收服务；采用关闭新单与前滚修复，而不是盲退纯上游。
- 蓝绿切换期间，两边使用相同订单数据和兼容规则；数据库唯一键和事务仍是最终防重屏障。

完成一次实际演练：升级前建单 -> 部署候选镜像 -> 模拟合法支付通知 -> 回退兼容镜像 -> 确认只到账一次且后台任务继续工作。记录证据，不仅提供文字步骤。

---

## 15. 分阶段提交与发布门槛

| 阶段 | 工作 | 必须交付 |
|---|---|---|
| P0 | 基线与安全边界调查 | BASELINE、PROVENANCE、实际集成点与测试基线 |
| P1 | 独立表、金额、状态、幂等接口 | 迁移、核心单测、跨库唯一约束测试 |
| P2 | Inbox、事务记账、WalletBridge、后台补偿 | 跨实例幂等、故障恢复、WALLET_CONSISTENCY 报告 |
| P3 | GoPay 微信 Native 与支付宝 PagePay | 真实 SDK 协议测试、验签/身份金额映射、配置验证 |
| P4 | 用户/管理 UI 与上游接线 | E2E、权限、原有渠道回归、实际主题构建 |
| P5 | 联调、灰度与运维工具 | 支付宝沙箱报告、微信实付或阻塞说明、监控和回滚演练 |
| P6 | 上游升级检查与兼容演练 | UPSTREAM_TOUCHPOINTS、升级报告脚本、版本兼容证据 |
| P7 | 受控全额退款独立变更 | 额度预留与消费竞争测试、退款查询/补偿、默认关闭开关 |

### G1：可以开放直连充值

P0–P6 的相关硬门槛通过，目标数据库/Redis组合验证，当前启用支付渠道完成真实授权环境冒烟，金额/身份校验与同步响应验签有反例测试，缺失凭据或生产能力明确阻止该渠道开启。

### G2：可以开放自动退款

P7 通过，冻结/预扣与缓存一致性有实测证据，退款受理与成功严格区分，人工授权真实退款验收已完成。不满足时退款保持关闭，不影响已通过 G1 的充值能力。

### 执行方式

每个阶段形成可单独审查、回归的提交；不要把所有变更压成一个含 CI/端口/品牌修改的大提交。执行代理可以把阶段写成内部任务列表，但交付必须是代码、测试、文档及证据，而不是仅重复计划。

---

## 16. 最终交付清单与完成报告模板

交付文件至少包括：

```text
docs/direct-pay/
  IMPLEMENTATION_PLAN.md
  BASELINE.md
  PROVENANCE.md
  ARCHITECTURE.md
  PROVIDER_EVIDENCE_MAPPING.md
  WALLET_CONSISTENCY.md
  CONFIGURATION.md
  SANDBOX_RUNBOOK.md
  TEST_REPORT.md
  UPGRADE_RUNBOOK.md
  UPSTREAM_TOUCHPOINTS.yaml
  OPERATIONS_AND_ROLLBACK.md
  KNOWN_LIMITATIONS.md
```

此外交付：实现与迁移、无真实密钥的配置示例、本地模拟和协议测试工具、目标数据库测试入口、上游影响报告脚本、前端 E2E 与构建证据。所有命令应按目标仓库实际情况验证，不输出不可运行的猜测命令。

完成报告：

```text
实现基线：repo / branch / commit
输出提交：commit 列表 / patch 或已授权的 PR
依赖：Go / GoPay / 前端工具链 / DB / Redis
已实现：按 P0–P7 列出
默认开关：充值 / 历史处理 / 退款 / 测试能力
自动化测试：实际命令、结果、证据路径
官方联调：支付宝沙箱 / 微信 live / 支付宝 live 分开列
升级与回滚：演练基线、结果、在途订单验证
已知限制：移动场景、订阅、退款门槛等
阻塞项：缺少哪类外部条件；不得粘贴真实凭据
生产结论：未验收 / 可灰度的具体渠道 / 退款是否允许
```

最后自检：没有新增 EPay 依赖；没有忽略验签；没有把 return_url 当支付成功；没有前端可控制的加余额入口；没有未经授权真实付款/退款；没有把 Mock 写成官方联调；没有覆盖用户工作区；没有把未测试的上游兼容性写成已保证。

---

## 17. 资料来源与复核入口

下列来源用于定位与验证。固定 SHA 的源码是实现参考；main 文档可能变化。执行时将实际采用的文档日期/版本写入 PROVENANCE。部分支付宝网页采用前端动态加载，本次公共抓取未取得完整正文；细节必须用实际沙箱控制台和相应 API 文档复核，不能仅凭网页标题推断。

[R1] 官方上游： https://github.com/QuantumNous/new-api

[R2] 原生扫码 PR 与关闭说明：
https://github.com/QuantumNous/new-api/pull/7055
https://github.com/QuantumNous/new-api/pull/7055#issuecomment-5441423786

[R3] 主要参考 fork 固定版本：
https://github.com/chunfeng789/new-api/tree/c82692fd6ce86bef2e6d868ae658e31f206edbcd
https://github.com/chunfeng789/new-api/blob/c82692fd6ce86bef2e6d868ae658e31f206edbcd/service/native_pay.go
https://github.com/chunfeng789/new-api/blob/c82692fd6ce86bef2e6d868ae658e31f206edbcd/controller/topup_native_qr.go
https://github.com/chunfeng789/new-api/blob/c82692fd6ce86bef2e6d868ae658e31f206edbcd/model/topup.go

[R4] 双支付与多场景参考：
https://github.com/QuantumNous/new-api/pull/5297
https://github.com/kavoj/35sz-api/blob/97b18e15f14a0fc188e64e638f7f2c5c5087f455/service/wechat_pay.go
https://github.com/kavoj/35sz-api/blob/97b18e15f14a0fc188e64e638f7f2c5c5087f455/service/alipay_pay.go

[R5] PagePay 参考（不得照搬回调顺序）：
https://github.com/QuantumNous/new-api/pull/4952
https://github.com/ZacBi/new-api/blob/42124002a13f1157fa5b50d7cfc98b403f4a8fd1/controller/topup_alipay.go

[R6] GoPay 支付宝文档（沙箱、签名、通知应答）：
https://github.com/go-pay/gopay/blob/v1.5.123/doc/alipay.md

[R7] GoPay 微信 v3 文档（无沙箱、公钥/证书模式）：
https://github.com/go-pay/gopay/blob/main/doc/wechat_v3.md

[R8] GoPay 支付宝客户端实现：
https://github.com/go-pay/gopay/blob/v1.5.123/alipay/client.go

[R9] GoPay 固定版本依赖清单：
https://github.com/go-pay/gopay/blob/v1.5.123/go.mod

[R10] GoPay TLS 行为说明：
https://github.com/go-pay/gopay/blob/main/README.md

[R11] new-api TopUp 与事务授信：
https://github.com/QuantumNous/new-api/blob/main/model/topup.go

[R12] new-api 用户缓存参考快照：
https://github.com/QuantumNous/new-api/blob/1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5/model/user_cache.go

[R13] 微信官方 Native 开发指引与下单：
https://pay.wechatpay.cn/doc/v3/merchant/4012791891
https://pay.wechatpay.cn/doc/v3/merchant/4012791877

[R14] 微信官方普通支付通知（说明同时适用于 Native 等普通支付）：
https://pay.wechatpay.cn/doc/v3/merchant/4012791836
https://pay.wechatpay.cn/doc/v3/merchant/4012791861

[R15] 支付宝官方沙箱与 PagePay 沙箱入口：
https://opendocs.alipay.com/common/02kkv7
https://opendocs.alipay.com/support/01rfvs
https://opendocs.alipay.com/common/02mse7
