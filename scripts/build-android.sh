#!/usr/bin/env bash
# Build a signed debug APK of the Go + Slint demo for Android (x86_64 + arm64-v8a).
#
# The app is one .so per ABI pair:
#   libgoslint.so     — Rust cdylib: NativeActivity entry (ANativeActivity_onCreate),
#                       android_main, skia renderer, the goslint_* C ABI.
#   libgoslintapp.so  — Go c-shared: the app (goslint_android_main); android_main
#                       dlopen's it. (Go can't c-archive on android, so two libs.)
#
# Requires: rustup android targets, the NDK, go, and SDK build-tools/platform.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SDK="${ANDROID_HOME:-$HOME/android-sdk}"
# A stray ANDROID_HOME may point at an SDK with an empty platforms/ (e.g. an old
# one); fall back to ~/android-sdk if it actually has a platform android.jar.
if ! ls "$SDK"/platforms/android-*/android.jar >/dev/null 2>&1 \
   && ls "$HOME"/android-sdk/platforms/android-*/android.jar >/dev/null 2>&1; then
  SDK="$HOME/android-sdk"
fi
NDK="${ANDROID_NDK_HOME:-$SDK/ndk/29.0.14206865}"
BUILD_TOOLS="${ANDROID_BUILD_TOOLS:-$SDK/build-tools/36.1.0}"
ANDROID_JAR="${ANDROID_PLATFORM_JAR:-$SDK/platforms/android-36/android.jar}"
KEYSTORE="${ANDROID_DEBUG_KEYSTORE:-$HOME/.android/debug.keystore}"
API="${ANDROID_API:-24}"

NDKBIN="$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin"
OUT="$ROOT/build/android"
export ANDROID_HOME="$SDK" ANDROID_SDK_ROOT="$SDK" ANDROID_NDK_HOME="$NDK" ANDROID_NDK_ROOT="$NDK"

# build_abi <rust-target> <go-arch> <apk-abi>
build_abi() {
  local rust="$1" goarch="$2" abi="$3"
  local clang="$NDKBIN/${rust}${API}-clang"
  local ar="$NDKBIN/llvm-ar"
  local rustlib="$ROOT/rust/goslint-sys/target/$rust/release"
  local libdir="$OUT/$abi"
  local upper; upper="$(echo "$rust" | tr 'a-z-' 'A-Z_')"
  local under; under="$(echo "$rust" | tr '-' '_')"
  mkdir -p "$libdir"

  echo ">> [$abi] Rust cdylib (libgoslint.so)"
  ( cd "$ROOT/rust/goslint-sys" && env \
      "CARGO_TARGET_${upper}_LINKER=$clang" \
      "CC_${under}=$clang" "CXX_${under}=${clang}++" "AR_${under}=$ar" \
      cargo build --release --target "$rust" )
  cp "$rustlib/libgoslint.so" "$libdir/"

  echo ">> [$abi] Go c-shared (libgoslintapp.so)"
  env CGO_LDFLAGS="-L$libdir -lgoslint -llog" \
      GOOS=android GOARCH="$goarch" CGO_ENABLED=1 CC="$clang" \
      go build -buildmode=c-shared -o "$libdir/libgoslintapp.so" "${APP_DIR:-./cmd/androiddemo}"
}

build_abi x86_64-linux-android  amd64 x86_64
build_abi aarch64-linux-android arm64 arm64-v8a

echo ">> packaging universal APK"
apk="$OUT/apk"; rm -rf "$apk"; mkdir -p "$apk/lib"
for abi in x86_64 arm64-v8a; do
  mkdir -p "$apk/lib/$abi"; cp "$OUT/$abi/"*.so "$apk/lib/$abi/"
done
"$BUILD_TOOLS/aapt2" link -o "$apk/base.apk" -I "$ANDROID_JAR" \
  --manifest "$ROOT/android/AndroidManifest.xml" --min-sdk-version "$API" --target-sdk-version 34
( cd "$apk" && python3 -c "
import zipfile, glob
z = zipfile.ZipFile('base.apk', 'a', zipfile.ZIP_DEFLATED)
for f in glob.glob('lib/**/*.so', recursive=True): z.write(f, f)
z.close()" )
"$BUILD_TOOLS/zipalign" -f 4 "$apk/base.apk" "$apk/aligned.apk"
"$BUILD_TOOLS/apksigner" sign --ks "$KEYSTORE" --ks-pass pass:android \
  --ks-key-alias androiddebugkey --key-pass pass:android \
  --out "$OUT/goslint-demo.apk" "$apk/aligned.apk"

echo ">> done: $OUT/goslint-demo.apk"
unzip -l "$OUT/goslint-demo.apk" | grep -E '\.so' || true
