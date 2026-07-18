#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "请使用 sudo 运行：sudo ./uninstall.sh" >&2
  exit 1
fi

systemctl disable --now palserver-agent.service 2>/dev/null || true
rm -f /etc/systemd/system/palserver-agent.service /var/lib/palserver-launcher/bin/pal-agent
systemctl daemon-reload

echo "Linux Agent 程序已经卸载。"
echo "服务器、存档和配置仍保留在 /var/lib/palserver-launcher/servers 与 /var/lib/palserver-launcher。"
