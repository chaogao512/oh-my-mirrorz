#!/bin/sh
set -eu

repository="chaogao512/oh-my-mirrorz"
install_dir="${OMM_INSTALL_DIR:-${HOME}/.local/bin}"

case "$(uname -s)" in
  Darwin) os_name="Darwin" ;;
  Linux) os_name="Linux" ;;
  *) echo "Unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch_name="x86_64" ;;
  arm64|aarch64) arch_name="arm64" ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

archive="oh-my-mirrorz_${os_name}_${arch_name}.tar.gz"
package_dir="${archive%.tar.gz}"
base="https://github.com/${repository}/releases/latest/download"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT HUP INT TERM

curl -fL --proto '=https' --tlsv1.2 -o "${temp_dir}/${archive}" "${base}/${archive}"
curl -fL --proto '=https' --tlsv1.2 -o "${temp_dir}/checksums.txt" "${base}/checksums.txt"

expected="$(awk -v file="$archive" '$2 == file { print $1 }' "${temp_dir}/checksums.txt")"
[ -n "$expected" ] || { echo "No checksum published for ${archive}" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "${temp_dir}/${archive}" | awk '{ print $1 }')"
else
  actual="$(shasum -a 256 "${temp_dir}/${archive}" | awk '{ print $1 }')"
fi
[ "$actual" = "$expected" ] || { echo "Checksum verification failed" >&2; exit 1; }

tar -xzf "${temp_dir}/${archive}" -C "$temp_dir"
mkdir -p "$install_dir"
install -m 0755 "${temp_dir}/${package_dir}/omm" "${install_dir}/oh-my-mirrorz"

if command -v omm >/dev/null 2>&1; then
  echo "Installed oh-my-mirrorz to ${install_dir}/oh-my-mirrorz. Existing omm command was not overwritten."
else
  install -m 0755 "${temp_dir}/${package_dir}/omm" "${install_dir}/omm"
  echo "Installed omm and oh-my-mirrorz to ${install_dir}."
fi
