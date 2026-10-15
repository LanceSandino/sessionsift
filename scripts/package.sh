#!/bin/bash
# Build deterministic SessionSift release archives, an SPDX inventory and SHA256SUMS.
# Usage: scripts/package.sh                (native target plus linux/amd64)
#        TARGETS="darwin/amd64" OUTPUT_DIR=out scripts/package.sh
# The native target is smoke-tested offline; cross-compiled targets are marked NOT_RUN.
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"
if [ -n "${VERSION:-}" ]; then
  version=$(python3 scripts/release.py version --expect "$VERSION")
else
  version=$(python3 scripts/release.py version)
fi
native_os=$(go env GOHOSTOS)
native_arch=$(go env GOHOSTARCH)
targets=${TARGETS:-$native_os/$native_arch linux/amd64}
out=${OUTPUT_DIR:-$repo_root/dist/release/$version}
mkdir -p "$out"
out=$(cd "$out" && pwd)
epoch=${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct)}
case "$epoch" in *[!0-9]*|'') echo "Invalid SOURCE_DATE_EPOCH" >&2; exit 1 ;; esac
build_root=$(mktemp -d "${TMPDIR:-/tmp}/sessionsift-package.XXXXXX")
trap 'rm -rf "$build_root"' EXIT

npm run --silent build:web
node scripts/generate-sbom.mjs "$out/sessionsift_${version}.spdx.json"

seen=' '
for target in $targets; do
  case "$seen" in *" $target "*) continue ;; esac
  seen="$seen$target "
  case "$target" in darwin/amd64|linux/amd64) ;; *) echo "Unsupported release target: $target" >&2; exit 1 ;; esac
  os=${target%/*}; arch=${target#*/}
  name="sessionsift_${version}_${os}_${arch}"
  stage="$build_root/$name"
  mkdir -p "$stage"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -buildvcs=false \
    -ldflags "-s -w -buildid=" -o "$stage/sessionsift" ./src/cmd/sessionsift
  cp LICENSE NOTICE README.md SECURITY.md CHANGELOG.md VERSION "$stage/"
  execution=NOT_RUN
  if [ "$target" = "$native_os/$native_arch" ]; then
    python3 scripts/smoke.py "$stage/sessionsift" "$version"
    execution=NATIVE_OFFLINE_SMOKE_PASS
  fi
  python3 - "$stage" "$out/$name.tar.gz" "$epoch" "$version" "$target" "$execution" <<'PY'
import gzip, hashlib, json, pathlib, sys, tarfile
stage, archive, epoch, version, target, execution = sys.argv[1:]
root = pathlib.Path(stage); epoch = int(epoch)
binary = hashlib.sha256((root / 'sessionsift').read_bytes()).hexdigest()
(root / 'BUILD-METADATA.json').write_text(json.dumps({'version': version, 'target': target,
    'source_date_epoch': epoch, 'execution': execution, 'binary_sha256': binary}, sort_keys=True) + '\n')
with open(archive, 'wb') as raw, gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as compressed, \
        tarfile.open(fileobj=compressed, mode='w', format=tarfile.PAX_FORMAT) as tar:
    for path in [root] + sorted(root.rglob('*')):
        info = tar.gettarinfo(str(path), arcname=str(path.relative_to(root.parent)))
        info.uid = info.gid = 0; info.uname = info.gname = ''; info.mtime = epoch
        info.mode = 0o755 if info.isdir() or path.name == 'sessionsift' else 0o644
        info.pax_headers = {}
        if info.isfile():
            with path.open('rb') as source: tar.addfile(info, source)
        else:
            tar.addfile(info)
print(f'Archive: {pathlib.Path(archive).name} ({execution})')
PY
done

python3 - "$out" <<'PY'
import hashlib, pathlib, sys
root = pathlib.Path(sys.argv[1])
files = sorted(list(root.glob('sessionsift_*.tar.gz')) + list(root.glob('sessionsift_*.spdx.json')))
if not files: raise SystemExit('No release files produced')
(root / 'SHA256SUMS').write_text(''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n' for p in files))
PY
echo "Release files and SHA256SUMS: $out"
