# MiniMax 语音克隆助手

MiniMax 语音克隆助手是一款基于 Bubble Tea 的终端界面应用，用于批量浏览本地音频文件、上传至 MiniMax 平台并触发语音克隆流程。项目同时面向业务同学和开发者，支持一站式的凭证配置、文件筛选、克隆进度追踪以及结果导出。

## 功能亮点
- **TUI 文件浏览**：以列表形式遍历当前目录，支持进入子目录、返回上级与多选文件。
- **MiniMax 克隆**：集成文件上传与语音克隆 API，自动生成 `voice_id` 并展示实时日志。
- **凭证管理**：提供 `Shift+C` 快捷键编辑凭证，同时支持编辑 `~/.minimax/config.toml`。
- **结果导出**：克隆结束后自动生成 CSV，并保存至 `~/Downloads`。
- **日志追踪**：所有运行日志写入 `~/minimax/logs/app.log`，便于问题定位。

## 环境准备
### 运行要求
- Go 1.24.0 及以上（建议使用 `go1.24.9` 工具链，参见 `go.mod`）。
- 可用的 MiniMax API 凭证：`MINIMAX_SECRET`、`MINIMAX_GROUP_ID`。
- macOS 或 Linux 终端环境（Windows 用户可在 WSL 中运行）。

### 获取源码
```bash
git clone <repo-url>
cd minimax
```

### 配置凭证
1. 推荐在 shell 中导出环境变量，便于后续测试与运行：
   ```bash
   export MINIMAX_SECRET=你的密钥
   export MINIMAX_GROUP_ID=你的GroupID
   ```
2. 首次运行时可通过 TUI 内的 `Shift+C` 打开凭证编辑窗口，保存后会写入 `~/.minimax/config.toml`：
   ```toml
   minimax_secret = "你的密钥"
   minimax_group_id = "你的GroupID"
   ```
   手动编辑该配置文件亦可达到同样效果，文件权限默认为 `0600`，请妥善保管。

## 快速上手
### 运行应用
- 临时运行（适合开发调试）：
  ```bash
  go run ./cmd/minimax
  ```
- 构建二进制：
  ```bash
  make build
  ./bin/minimax
  ```

### 界面操作
- **导航**：方向键或 `hjkl`。
- **多选文件**：按 `Space` 或 `X` 勾选/取消。
- **进入目录**：`Enter`；返回上级目录会显示 `..` 项。
- **发起克隆**：选中文件后按 `c`。
- **编辑凭证**：`Shift+C`。
- **导出 CSV**：`E`，系统会提示导出路径。
- **退出程序**：`Q`（亦可使用 `Ctrl+C`）。

### 克隆流程概览
1. 勾选待上传的音频文件，支持一次克隆多个文件。
2. 按 `c` 启动克隆；界面右侧视口展示实时日志（上传/克隆步骤及错误信息）。
3. 克隆完成后进入总结界面，可查看成功/失败统计以及每个文件的处理结果。
4. 程序会自动尝试导出 CSV 至 `~/Downloads/minimax_voice_export_<时间戳>.csv`，若导出失败，可通过 `E` 手动重试。

### 运行产生的文件
- `~/.minimax/config.toml`：保存 MiniMax 凭证。
- `~/minimax/logs/app.log`：zerolog 结构化日志，便于排查。
- `~/Downloads/minimax_voice_export_*.csv`：克隆结果汇总。
上述目录均已在 `.gitignore` 中忽略，切勿提交仓库。

## 开发者指南
### 常用命令
- 单元测试：
  ```bash
  go test ./...
  ```
- 覆盖率检查：
  ```bash
  go test ./... -cover
  ```
- 静态检查：
  ```bash
  go vet ./...
  ```
- 依赖对齐：
  ```bash
  make tidy  # 等价于 go mod tidy
  ```

### 项目结构
```
cmd/minimax      # 入口程序：装配配置、日志、启动 TUI
internal/app     # Bubble Tea 模型与状态机，包含文件浏览、克隆与导出逻辑
internal/minimax # MiniMax API 客户端，封装上传与克隆请求
internal/exporter# 将内存中的克隆结果写入 CSV
internal/config  # 读取/保存凭证配置
internal/system  # 路径解析与目录初始化
internal/logging # zerolog 日志初始化
```

### 测试策略建议
- 对核心逻辑使用表驱动测试，覆盖正常路径与异常路径（如缺少凭证、HTTP 失败、导出失败）。
- 将样例音频或 CSV 模板放在 `testdata/` 中，避免影响业务逻辑。
- 推荐在提交前执行 `go test ./... -cover`，确保新增代码覆盖率 ≥80%。

## 故障排查
- **无法读取配置**：确认 `~/.minimax/config.toml` 是否存在且格式正确，可删除后重新在界面中填写。
- **API 调用失败**：检查网络连通性、凭证是否过期或权限不足，日志中会包含 MiniMax 返回的 `status_msg`。
- **CSV 未生成**：确认 `~/Downloads` 可写，或通过 `E` 手动导出并查看终端提示。
- **界面显示异常**：终端需支持真彩色；若在远程环境使用，请选择兼容的终端模拟器。

## 许可
本仓库未声明开源许可证，如需对外发布请先与仓库维护者确认授权范围。
