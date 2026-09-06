#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="${VERSION:-dev}"
commit="${COMMIT:-$(git -C "${repo_root}" rev-parse --short HEAD 2>/dev/null || printf 'none')}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
out_dir="${OUT_DIR:-"${repo_root}/dist/native"}"
web_html="${WEB_HTML:-"${repo_root}/dist/index.html"}"
binary_name="cpa-manager"
module_path="github.com/seakee/cpa-manager/usage-service"

if [ ! -f "${web_html}" ]; then
  echo "missing ${web_html}; run npm run build first" >&2
  exit 1
fi

mkdir -p "${repo_root}/bin/tmp/release"
work_dir="$(mktemp -d "${repo_root}/bin/tmp/release/native.XXXXXX")"
trap 'rm -rf "${work_dir}"' EXIT

rm -rf "${out_dir}"
mkdir -p "${out_dir}"
out_dir="$(cd "${out_dir}" && pwd)"

cp -R "${repo_root}/usage-service" "${work_dir}/usage-service"
python3 "${repo_root}/bin/release/normalize-embedded-html.py" \
  "${web_html}" "${work_dir}/usage-service/internal/httpapi/web/management.html"

targets=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
  "windows arm64"
)

for target in "${targets[@]}"; do
  read -r goos goarch <<<"${target}"
  package_name="${binary_name}_${version}_${goos}_${goarch}"
  package_dir="${work_dir}/${package_name}"
  exe_name="${binary_name}"
  updater_name="cpa-updater"

  if [ "${goos}" = "windows" ]; then
    exe_name="${binary_name}.exe"
    updater_name="${updater_name}.exe"
  fi

  mkdir -p "${package_dir}"
  (
    cd "${work_dir}/usage-service"
    CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -trimpath \
      -ldflags "-s -w -X ${module_path}/internal/buildinfo.Version=${version} -X ${module_path}/internal/buildinfo.Commit=${commit} -X ${module_path}/internal/buildinfo.BuildDate=${build_date}" \
      -o "${package_dir}/${exe_name}" ./cmd/cpa-manager
    CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -trimpath -ldflags "-s -w" \
      -o "${package_dir}/${updater_name}" ./cmd/cpa-updater
  )

  cp "${repo_root}/README.md" "${package_dir}/README.md"
  cp "${repo_root}/README_CN.md" "${package_dir}/README_CN.md"
  cp "${repo_root}/LICENSE" "${package_dir}/LICENSE"

  if [ "${goos}" = "windows" ]; then
    (
      cd "${work_dir}"
      if command -v zip >/dev/null 2>&1; then
        zip -qr "${out_dir}/${package_name}.zip" "${package_name}"
      else
        package_dir_win="$(cygpath -w "${package_dir}")"
        out_zip_win="$(cygpath -w "${out_dir}/${package_name}.zip")"
        powershell.exe -NoProfile -Command "& { Compress-Archive -Path '${package_dir_win}' -DestinationPath '${out_zip_win}' -Force }"
      fi
    )
  else
    (
      cd "${work_dir}"
      tar -czf "${package_name}.tar.gz" "${package_name}"
      mv "${package_name}.tar.gz" "${out_dir}/"
    )
  fi
done

(
  cd "${out_dir}"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum ./* > checksums.txt
  else
    shasum -a 256 ./* > checksums.txt
  fi
)
