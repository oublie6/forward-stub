#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
K8S_DIR="${ROOT_DIR}/deploy/k8s"
ACTION=${1:-apply}

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl 未安装，请先安装 kubectl 后重试。"
  exit 1
fi

usage() {
  cat <<USAGE
用法: $(basename "$0") [apply|delete|diff|render]

  apply   应用 deploy/k8s 中的清单（默认）
  delete  删除 deploy/k8s 中的清单
  diff    查看与集群现有资源的差异
  render  渲染 kustomize 最终输出（不提交到集群）
USAGE
}

case "${ACTION}" in
  apply)
    kubectl apply -k "${K8S_DIR}"
    ;;
  delete)
    kubectl delete -k "${K8S_DIR}"
    ;;
  diff)
    kubectl diff -k "${K8S_DIR}"
    ;;
  render)
    kubectl kustomize "${K8S_DIR}"
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    echo "未知动作: ${ACTION}"
    usage
    exit 2
    ;;
esac
