#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
: "${ANDROID_LIBDIR:?Set ANDROID_LIBDIR to your external ARM64 Bionic library directory}"
zig="${ZIG:-zig}"
mkdir -p "$root/bin"
for source in capture local_only; do
  flags=(-target aarch64-linux-android -nostdlib -fno-stack-protector -fno-sanitize=all -fno-builtin -O2)
  output="$root/bin/$source-arm64"
  if [[ "$source" == local_only ]]; then
    flags+=(-shared -fPIC)
    output+=.so
  else
    flags+=(-Wl,-e,_start -Wl,-dynamic-linker,/system/bin/linker64)
  fi
  "$zig" cc "${flags[@]}" "$root/native/$source.c" "$ANDROID_LIBDIR/libc.so" "$ANDROID_LIBDIR/libdl.so" -o "$output"
done
