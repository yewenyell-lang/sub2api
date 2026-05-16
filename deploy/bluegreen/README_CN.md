# Sub2API 蓝绿发布

这套配置用于二进制部署场景，目标是在更新时避免中断正在执行的流式请求。

## 架构

- `sub2api@blue.service` 监听 `127.0.0.1:5091`
- `sub2api@green.service` 监听 `127.0.0.1:5092`
- Caddy 对外监听 `:5089`
- Caddy 通过 `/etc/sub2api/bluegreen/active-upstream.caddy` 转发到当前激活实例
- 应用 `/ready` 返回 `503` 时，表示实例正在排水，不应再接收新流量

## 首次安装

在服务器执行：

```bash
cd /opt/sub2api/bluegreen
sudo ./deploy-bluegreen.sh install
```

如果当前已有旧的 `sub2api.service` 直接监听 `5089`，首次迁移不要直接执行切流。推荐流程：

1. 确认已有数据库备份。
2. 安装 Caddy，但先不要启动或监听 `5089`。
3. 用当前线上二进制初始化 blue：
   `ln -sfn /opt/sub2api /opt/sub2api/current-blue`
4. 启动 `sub2api@blue.service` 并确认 `http://127.0.0.1:5091/ready` 返回 `200`。
5. 停止旧 `sub2api.service`，立即启动 Caddy 接管 `5089`。
6. 确认外部访问正常后禁用旧服务：`systemctl disable sub2api.service`。

## 发布新版本

```bash
sudo ./deploy-bluegreen.sh deploy --binary /tmp/sub2api-new --version 20260516_001 --drain-seconds 120
```

脚本会：

- 判断当前激活实例
- 把新二进制放入 `/opt/sub2api/releases/<version>/sub2api`
- 启动备用实例
- 检查备用实例 `/ready`
- reload Caddy 切流
- 等待排水时间
- 停止旧实例

## 回滚

```bash
sudo ./deploy-bluegreen.sh rollback --drain-seconds 30
```

## 常用检查

```bash
sudo ./deploy-bluegreen.sh status
systemctl status sub2api@blue sub2api@green caddy --no-pager
journalctl -u sub2api@blue -n 100 --no-pager
journalctl -u sub2api@green -n 100 --no-pager
```

## 注意事项

- 不要在蓝绿发布中覆盖 `/opt/sub2api/config.yaml`。
- 不要在发布脚本里自动执行数据库恢复。
- 如果新版本包含数据库迁移，必须先判断迁移是否向后兼容；否则旧实例和新实例同时运行时可能不兼容。
- `SERVER_GRACEFUL_SHUTDOWN_TIMEOUT` 默认建议不低于 `1800` 秒，适合长流式请求。
