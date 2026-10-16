# 采集器发布签名密钥

本目录随 Git 管理 **age 加密后的 Ed25519 私钥**和对应公钥。明文私钥只在发布工具内存中存在；网关与客户端只需公钥。

- `release-1.key.age`：标准 age ASCII armor 密文，使用 scrypt 口令保护，可以提交。
- `public-keys.json`：`key_id -> Base64 Ed25519 公钥`，可直接用于网关 `NEW_API_USAGE_REPORTER_TRUST_KEYS`。
- 解密口令：不在仓库中。本机使用 macOS Keychain，service 为 `new-api.reporter-release`、account 为 `release-1`；CI 使用 environment Secret `REPORTER_RELEASE_KEY_PASSPHRASE`。

## 本机操作

在项目根目录运行。首次生成会在 Keychain 中创建一个 256 bit 随机口令；已有条目会复用，已有密钥文件不会覆盖。

```sh
sh tools/reporter-release.sh --mode generate-key
sh tools/reporter-release.sh --mode check-key
```

只有首次初始化需要 `generate-key`。已有密文时，先在系统“钥匙串访问”中安全恢复相应口令；不要删除密文并重建同名密钥。`check-key` 会解密并核对公钥，输出只包含核验结果。

构建签名资产（版本、release ID、序号按正式发布设置）：

```sh
sh tools/reporter-release.sh --mode build \
  --out /absolute/path/outside-checkout/reporter-assets \
  --version 1.0.3 --release reporter-1.0.3 \
  --sequence 1 --rollout 10 --seed stable-1
```

工具直接在内存中解密、签名，不创建明文私钥临时文件。新版本使用新的版本号和 release ID。清单默认有效 30 天，续签使用相同 release ID 与更高 `--sequence`：

```sh
sh tools/reporter-release.sh --mode sign \
  --out /absolute/path/outside-checkout/reporter-assets \
  --version 1.0.3 --release reporter-1.0.3 \
  --sequence 2 --rollout 50 --seed stable-1
```

## CI 与迁移

GitHub 的 `collector-release` environment 只配置 `REPORTER_RELEASE_KEY_PASSPHRASE`，不再存放明文私钥。已有旧 Secret `REPORTER_RELEASE_SIGNING_KEY` 不再被工作流使用。工作流读取仓库密文和公钥，仅上传发布资产。

Linux/其他发布环境也可通过受控 Secret 注入同名环境变量后运行上述脚本。口令不能放在命令行参数、仓库 `.env`、工作流文本或日志里。工具不会在缺少口令时自动改用明文密钥。

**Git 备份不包含解密口令。** 应将 Keychain 中的口令另存到可信密码管理器或独立加密备份中。只克隆仓库不能恢复签名能力，遗失口令也无法从公钥找回。

轮换密钥时使用新的 `key_id` 和密文文件；先用旧密钥签署包含 `next_keys` 的过渡清单，让客户端收到新公钥，再切换签名。不要覆盖已发布的同名公钥。
