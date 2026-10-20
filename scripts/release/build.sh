#!/usr/bin/env bash
# Builds the release artifacts into dist/:
#   abyssdb_<version>_<os>_<arch>.tar.gz (.zip for Windows), the WASI build,
#   checksums.txt, and dist/docker/linux-<arch>/abyssdb for the container image.
#
#   scripts/release/build.sh 0.0.0-null
set -euo pipefail

version="${1:?usage: build.sh VERSION}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
dist="$root/dist"
rm -rf "$dist"
mkdir -p "$dist"
ldflags="-s -w -X github.com/noctarius/abyssdb/internal/buildinfo.Version=$version"

build() { # build GOOS GOARCH OUTPUT
	(cd "$root" && CGO_ENABLED=0 GOOS="$1" GOARCH="$2" go build -trimpath -ldflags "$ldflags" -o "$3" ./cmd/abyssdb)
}

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
	os="${target%/*}" arch="${target#*/}"
	name="abyssdb_${version}_${os}_${arch}"
	stage="$dist/stage/$name"
	mkdir -p "$stage"
	exe="abyssdb"
	[[ "$os" == windows ]] && exe="abyssdb.exe"
	echo "==> $os/$arch"
	build "$os" "$arch" "$stage/$exe"
	cp "$root/README.md" "$root/LICENSE" "$stage/"
	if [[ "$os" == windows ]]; then
		(cd "$dist/stage" && zip -qr "$dist/$name.zip" "$name")
	else
		tar -C "$dist/stage" -czf "$dist/$name.tar.gz" "$name"
	fi
	if [[ "$os" == linux ]]; then
		mkdir -p "$dist/docker/linux-$arch"
		cp "$stage/abyssdb" "$dist/docker/linux-$arch/abyssdb"
	fi
done

echo "==> wasip1/wasm"
build wasip1 wasm "$dist/abyssdb_${version}_wasip1.wasm"
rm -rf "$dist/stage"

cd "$dist"
if command -v sha256sum >/dev/null; then
	sha256sum ./*.tar.gz ./*.zip ./*.wasm | sed 's| \./| |' >checksums.txt
else
	shasum -a 256 ./*.tar.gz ./*.zip ./*.wasm | sed 's| \./| |' >checksums.txt
fi
echo "==> dist/"
ls -lh "$dist" | tail -n +2
