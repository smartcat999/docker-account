#!/bin/sh
set -eu

repo="smartcat999/docker-account"
install_dir="${DOCKER_CLI_PLUGIN_DIR:-${HOME}/.docker/cli-plugins}"

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) echo "Unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

archive="docker-account_${os}_${arch}.tar.gz"
if [ -n "${VERSION:-}" ]; then
  release_url="https://github.com/${repo}/releases/download/${VERSION}"
else
  release_url="https://github.com/${repo}/releases/latest/download"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

curl -fsSL "${release_url}/${archive}" -o "${tmp_dir}/${archive}"
curl -fsSL "${release_url}/checksums.txt" -o "${tmp_dir}/checksums.txt"

expected="$(awk -v file="$archive" '$2 == file { print $1 }' "${tmp_dir}/checksums.txt")"
if [ -z "$expected" ]; then
  echo "Checksum not found for ${archive}" >&2
  exit 1
fi
if command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "${tmp_dir}/${archive}" | awk '{ print $1 }')"
else
  actual="$(sha256sum "${tmp_dir}/${archive}" | awk '{ print $1 }')"
fi
if [ "$actual" != "$expected" ]; then
  echo "Checksum verification failed for ${archive}" >&2
  exit 1
fi

tar -xzf "${tmp_dir}/${archive}" -C "$tmp_dir"
mkdir -p "$install_dir"
install -m 755 "${tmp_dir}/docker-account" "${install_dir}/docker-account"
echo "Installed docker-account to ${install_dir}/docker-account"
docker account version
