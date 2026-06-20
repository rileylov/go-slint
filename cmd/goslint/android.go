package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// abis maps Android ABIs to their Go arch, Rust triple, and release target name.
var abis = []struct {
	abi, goarch, triple, target string
}{
	{"arm64-v8a", "arm64", "aarch64-linux-android", "android_arm64"},
	{"x86_64", "amd64", "x86_64-linux-android", "android_amd64"},
}

func cmdAndroid(args []string) error {
	if len(args) == 0 || args[0] != "build" {
		return fmt.Errorf("usage: goslint android build [flags] <package>")
	}
	return cmdAndroidBuild(args[1:])
}

// cmdAndroidBuild packages a Go package (which must export goslint_android_main,
// e.g. via a //go:build android file) into a signed APK. The native libgoslint.so
// for each ABI comes from the prebuilt release (downloaded like `goslint setup`);
// the Go package is cross-built as a c-shared and both land in lib/<abi>/.
func cmdAndroidBuild(args []string) error {
	fs := flag.NewFlagSet("android build", flag.ExitOnError)
	out := fs.String("o", "", "output APK path (default <name>.apk)")
	abiList := fs.String("abi", "arm64-v8a,x86_64", "comma-separated ABIs to include")
	appID := fs.String("package", "", "Android application id (default dev.goslint.<name>)")
	label := fs.String("label", "", "app display label (default <name>)")
	versionName := fs.String("version-name", "1.0", "android:versionName")
	versionCode := fs.Int("version-code", 1, "android:versionCode")
	minSDK := fs.Int("min-sdk", 24, "minimum SDK version")
	targetSDK := fs.Int("target-sdk", 34, "target SDK version")
	manifestArg := fs.String("manifest", "", "custom AndroidManifest.xml (default: generated)")
	sdkArg := fs.String("sdk", "", "Android SDK dir (default $ANDROID_HOME)")
	ndkArg := fs.String("ndk", "", "Android NDK dir (default $ANDROID_NDK_HOME or <sdk>/ndk/<latest>)")
	keystore := fs.String("keystore", "", "signing keystore (default: ~/.android/debug.keystore, auto-created)")
	ksPass := fs.String("ks-pass", "android", "keystore password")
	keyAlias := fs.String("key-alias", "androiddebugkey", "signing key alias")
	keyPass := fs.String("key-pass", "android", "key password")
	_ = fs.Parse(args)

	pkg := fs.Arg(0)
	if pkg == "" {
		pkg = "."
	}
	name := appName(pkg, *label)
	if *appID == "" {
		*appID = "dev.goslint." + sanitizeID(name)
	}
	if *label == "" {
		*label = name
	}
	if *out == "" {
		*out = name + ".apk"
	}

	tc, err := resolveAndroidTools(*sdkArg, *ndkArg)
	if err != nil {
		return err
	}

	selected, err := selectABIs(*abiList)
	if err != nil {
		return err
	}

	stage, err := os.MkdirTemp("", "goslint-apk")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	// 1. per-ABI: fetch prebuilt libgoslint.so + cross-build the Go c-shared.
	libEntries := map[string]string{} // zip path -> file path
	for _, a := range selected {
		fmt.Printf(">> %s\n", a.abi)
		t, soPath, _, err := provision(a.target, false)
		if err != nil {
			return fmt.Errorf("%s: %w", a.abi, err)
		}
		if t.Kind != "shared" {
			return fmt.Errorf("%s: release target %s is not an android (shared) lib", a.abi, a.target)
		}
		abiDir := filepath.Join(stage, "lib", a.abi)
		if err := os.MkdirAll(abiDir, 0o755); err != nil {
			return err
		}
		// copy the prebuilt native lib
		if err := copyFile(soPath, filepath.Join(abiDir, "libgoslint.so")); err != nil {
			return err
		}
		// cross-build the user's Go package as a c-shared that links libgoslint.so
		clang := filepath.Join(tc.ndkBin, a.triple+strconv.Itoa(*minSDK)+"-clang")
		appSO := filepath.Join(abiDir, "libgoslintapp.so")
		env := []string{
			"GOOS=android", "GOARCH=" + a.goarch, "CGO_ENABLED=1",
			"CC=" + clang,
			"CGO_LDFLAGS=-L" + filepath.Dir(soPath) + " -lgoslint -llog",
		}
		if err := runEnv(env, "go", "build", "-buildmode=c-shared", "-o", appSO, pkg); err != nil {
			return fmt.Errorf("go build (%s): %w", a.abi, err)
		}
		libEntries["lib/"+a.abi+"/libgoslint.so"] = filepath.Join(abiDir, "libgoslint.so")
		libEntries["lib/"+a.abi+"/libgoslintapp.so"] = appSO
	}

	// 2. manifest
	manifestPath := *manifestArg
	if manifestPath == "" {
		manifestPath = filepath.Join(stage, "AndroidManifest.xml")
		m := genManifest(*appID, *label, *versionCode, *versionName, *minSDK, *targetSDK)
		if err := os.WriteFile(manifestPath, []byte(m), 0o644); err != nil {
			return err
		}
	}

	// 3. aapt2 link -> base.apk
	baseAPK := filepath.Join(stage, "base.apk")
	if err := run(tc.aapt2, "link", "-o", baseAPK, "-I", tc.platformJar,
		"--manifest", manifestPath,
		"--min-sdk-version", strconv.Itoa(*minSDK),
		"--target-sdk-version", strconv.Itoa(*targetSDK)); err != nil {
		return fmt.Errorf("aapt2 link: %w", err)
	}

	// 4. inject the .so libraries into the APK
	if err := injectFiles(baseAPK, libEntries); err != nil {
		return fmt.Errorf("add libs: %w", err)
	}

	// 5. zipalign
	aligned := filepath.Join(stage, "aligned.apk")
	if err := run(tc.zipalign, "-f", "4", baseAPK, aligned); err != nil {
		return fmt.Errorf("zipalign: %w", err)
	}

	// 6. signing keystore (auto-create a debug one if using the default)
	ks := *keystore
	if ks == "" {
		home, _ := os.UserHomeDir()
		ks = filepath.Join(home, ".android", "debug.keystore")
		if !exists(ks) {
			if err := os.MkdirAll(filepath.Dir(ks), 0o755); err != nil {
				return err
			}
			fmt.Println(">> creating debug keystore", ks)
			if err := run("keytool", "-genkeypair", "-keystore", ks, "-alias", *keyAlias,
				"-storepass", *ksPass, "-keypass", *keyPass, "-keyalg", "RSA", "-keysize", "2048",
				"-validity", "10000", "-dname", "CN=Android Debug,O=Android,C=US"); err != nil {
				return fmt.Errorf("keytool: %w", err)
			}
		}
	}

	// 7. apksigner sign -> output
	if err := run(tc.apksigner, "sign", "--ks", ks, "--ks-pass", "pass:"+*ksPass,
		"--ks-key-alias", *keyAlias, "--key-pass", "pass:"+*keyPass,
		"--out", *out, aligned); err != nil {
		return fmt.Errorf("apksigner: %w", err)
	}

	fmt.Printf("\n✓ %s  (%s, %s)\n", *out, *appID, strings.Join(abiNames(selected), "+"))
	fmt.Printf("  install: adb install -r %s\n", *out)
	return nil
}

// ---- android toolchain resolution ----

type androidTools struct {
	sdk, ndkBin, buildTools, platformJar, aapt2, zipalign, apksigner string
}

func resolveAndroidTools(sdkArg, ndkArg string) (androidTools, error) {
	var t androidTools
	t.sdk = firstNonEmpty(sdkArg, os.Getenv("ANDROID_HOME"), os.Getenv("ANDROID_SDK_ROOT"))
	if t.sdk == "" {
		return t, fmt.Errorf("Android SDK not found — set ANDROID_HOME or pass -sdk")
	}
	bt, err := latestDir(filepath.Join(t.sdk, "build-tools"), "*")
	if err != nil {
		return t, fmt.Errorf("no build-tools under %s: %w", t.sdk, err)
	}
	t.buildTools = bt
	t.aapt2 = filepath.Join(bt, "aapt2")
	t.zipalign = filepath.Join(bt, "zipalign")
	t.apksigner = filepath.Join(bt, "apksigner")

	plat, err := latestDir(filepath.Join(t.sdk, "platforms"), "android-*")
	if err != nil {
		return t, fmt.Errorf("no platforms under %s (install a platform): %w", t.sdk, err)
	}
	t.platformJar = filepath.Join(plat, "android.jar")
	if !exists(t.platformJar) {
		return t, fmt.Errorf("missing %s", t.platformJar)
	}

	ndk := firstNonEmpty(ndkArg, os.Getenv("ANDROID_NDK_HOME"), os.Getenv("ANDROID_NDK_ROOT"))
	if ndk == "" {
		ndk, err = latestDir(filepath.Join(t.sdk, "ndk"), "*")
		if err != nil {
			return t, fmt.Errorf("NDK not found — set ANDROID_NDK_HOME or pass -ndk: %w", err)
		}
	}
	t.ndkBin = filepath.Join(ndk, "toolchains", "llvm", "prebuilt", ndkHostTag(), "bin")
	if !exists(t.ndkBin) {
		return t, fmt.Errorf("NDK toolchain bin not found at %s", t.ndkBin)
	}
	return t, nil
}

func ndkHostTag() string {
	switch runtime.GOOS {
	case "darwin":
		return "darwin-x86_64"
	case "windows":
		return "windows-x86_64"
	default:
		return "linux-x86_64"
	}
}

func selectABIs(list string) ([]struct{ abi, goarch, triple, target string }, error) {
	want := map[string]bool{}
	for _, a := range strings.Split(list, ",") {
		want[strings.TrimSpace(a)] = true
	}
	var out []struct{ abi, goarch, triple, target string }
	for _, a := range abis {
		if want[a.abi] {
			out = append(out, a)
			delete(want, a.abi)
		}
	}
	if len(want) > 0 {
		return nil, fmt.Errorf("unknown ABI(s): %s (supported: arm64-v8a, x86_64)", strings.Join(keysOf(want), ", "))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no ABIs selected")
	}
	return out, nil
}

func abiNames(s []struct{ abi, goarch, triple, target string }) []string {
	var n []string
	for _, a := range s {
		n = append(n, a.abi)
	}
	return n
}

// ---- manifest ----

func genManifest(appID, label string, versionCode int, versionName string, minSDK, targetSDK int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="%s"
    android:versionCode="%d"
    android:versionName="%s">

    <uses-sdk android:minSdkVersion="%d" android:targetSdkVersion="%d" />

    <application
        android:label="%s"
        android:hasCode="false"
        android:extractNativeLibs="true">
        <activity
            android:name="android.app.NativeActivity"
            android:exported="true"
            android:configChanges="orientation|keyboardHidden|screenSize|density">
            <meta-data android:name="android.app.lib_name" android:value="goslint" />
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
        </activity>
    </application>
</manifest>
`, appID, versionCode, versionName, minSDK, targetSDK, label)
}

// ---- helpers ----

func injectFiles(apk string, files map[string]string) error {
	r, err := zip.OpenReader(apk)
	if err != nil {
		return err
	}
	defer r.Close()
	tmp := apk + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := zip.NewWriter(out)
	for _, f := range r.File {
		if _, dup := files[f.Name]; dup {
			continue
		}
		fw, err := w.CreateRaw(&f.FileHeader)
		if err != nil {
			return err
		}
		rc, err := f.OpenRaw()
		if err != nil {
			return err
		}
		if _, err := io.Copy(fw, rc); err != nil {
			return err
		}
	}
	for name, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fw, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, apk)
}

func appName(pkg, label string) string {
	if label != "" {
		return label
	}
	abs, err := filepath.Abs(pkg)
	if err != nil || filepath.Base(abs) == "." || filepath.Base(abs) == string(filepath.Separator) {
		return "goslint-app"
	}
	return filepath.Base(abs)
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

func latestDir(parent, glob string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(parent, glob))
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.IsDir() {
			dirs = append(dirs, m)
		}
	}
	if len(dirs) == 0 {
		return "", fmt.Errorf("nothing matching %s in %s", glob, parent)
	}
	sort.Strings(dirs)
	return dirs[len(dirs)-1], nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func keysOf(m map[string]bool) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func run(name string, args ...string) error { return runEnv(nil, name, args...) }

func runEnv(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
