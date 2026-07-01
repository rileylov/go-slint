package main

import (
	"archive/zip"
	"debug/elf"
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
	"time"
)

// abis maps Android ABIs to their Go arch, Rust triple, and release target name.
var abis = []struct {
	abi, goarch, triple, target string
}{
	{"arm64-v8a", "arm64", "aarch64-linux-android", "android_arm64"},
	{"x86_64", "amd64", "x86_64-linux-android", "android_amd64"},
}

func cmdAndroid(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: goslint android <build|dev> [flags] <package>")
	}
	switch args[0] {
	case "build":
		return cmdAndroidBuild(args[1:])
	case "dev":
		return cmdAndroidDev(args[1:])
	default:
		return fmt.Errorf("usage: goslint android <build|dev> [flags] <package>")
	}
}

// androidBuildCfg is the fully-resolved input to buildAPK (the caller applies defaults
// via resolveAndroidCfg). Shared by `goslint android build` and `goslint android dev`,
// exactly as iosBuildCfg is shared by the iOS build/dev commands.
type androidBuildCfg struct {
	pkg, out, appID, label, versionName  string
	versionCode, minSDK, targetSDK       int
	abiList, manifestArg, sdkArg, ndkArg string
	keystore, ksPass, keyAlias, keyPass  string
}

// resolveAndroidCfg fills the defaults (name / application-id / label / output) from
// the package.
func resolveAndroidCfg(pkg, out, appID, label, versionName string, versionCode, minSDK, targetSDK int,
	abiList, manifestArg, sdkArg, ndkArg, keystore, ksPass, keyAlias, keyPass string) androidBuildCfg {
	if pkg == "" {
		pkg = "."
	}
	name := appName(pkg, label)
	if appID == "" {
		appID = "dev.goslint." + sanitizeID(name)
	}
	if label == "" {
		label = name
	}
	if out == "" {
		out = name + ".apk"
	}
	return androidBuildCfg{pkg, out, appID, label, versionName, versionCode, minSDK, targetSDK,
		abiList, manifestArg, sdkArg, ndkArg, keystore, ksPass, keyAlias, keyPass}
}

// buildAPK packages cfg.pkg (which must export goslint_android_main, e.g. via a
// //go:build goslint_android file — scaffolded on demand) into a signed APK at cfg.out.
// The native libgoslint.so for each ABI comes from the prebuilt release (downloaded like
// `goslint setup`); the Go package is cross-built as a c-shared and both land in
// lib/<abi>/. Shared by `goslint android build` and `goslint android dev`.
func buildAPK(cfg androidBuildCfg) error {
	// The scaffold ships desktop-only (no Android entry file — one would otherwise
	// make editors spin up a broken android build view). Create it here, on the first
	// android build, so Android still works without manual setup.
	if err := ensureAndroidEntry(cfg.pkg); err != nil {
		return err
	}

	tc, err := resolveAndroidTools(cfg.sdkArg, cfg.ndkArg)
	if err != nil {
		return err
	}

	selected, err := selectABIs(cfg.abiList)
	if err != nil {
		return err
	}

	stage, err := os.MkdirTemp("", "goslint-apk")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	// 1. per-ABI: fetch prebuilt libgoslint.so + cross-build the Go c-shared.
	fmt.Printf("Building %s  (package %s, abis: %s)\n", filepath.Base(cfg.out), cfg.pkg, strings.Join(abiNames(selected), ", "))
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
		clang := filepath.Join(tc.ndkBin, a.triple+strconv.Itoa(cfg.minSDK)+"-clang")
		appSO := filepath.Join(abiDir, "libgoslintapp.so")
		env := []string{
			"GOOS=android", "GOARCH=" + a.goarch, "CGO_ENABLED=1",
			"CC=" + clang,
			"CGO_LDFLAGS=-L" + filepath.Dir(soPath) + " -lgoslint -llog",
		}
		fmt.Printf("   cross-building %s (c-shared, %s)…\n", cfg.pkg, a.goarch)
		// -tags=goslint_android selects the Android entry file (gated on that custom
		// tag, not the android GOOS — see androidTemplate). GOOS=android is set via env
		// above; a legacy //go:build android entry still matches that and builds too.
		if err := runEnv(env, "go", "build", "-buildmode=c-shared", "-tags=goslint_android", "-o", appSO, cfg.pkg); err != nil {
			return fmt.Errorf("go build (%s): %w", a.abi, err)
		}
		// The APK is only usable if the Go side exports goslint_android_main — the
		// symbol the Rust android_main dlsym's and calls. Without it the APK installs
		// but the activity does nothing; verify it now rather than ship a dead APK.
		exported, err := androidEntryExported(appSO)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", appSO, err)
		}
		if !exported {
			return fmt.Errorf(noAndroidEntryMsg, cfg.pkg)
		}
		fmt.Println("   ✓ goslint_android_main exported — the app will launch")
		libEntries["lib/"+a.abi+"/libgoslint.so"] = filepath.Join(abiDir, "libgoslint.so")
		libEntries["lib/"+a.abi+"/libgoslintapp.so"] = appSO
	}

	// 2. manifest
	manifestPath := cfg.manifestArg
	if manifestPath == "" {
		manifestPath = filepath.Join(stage, "AndroidManifest.xml")
		m := genManifest(cfg.appID, cfg.label, cfg.versionCode, cfg.versionName, cfg.minSDK, cfg.targetSDK)
		if err := os.WriteFile(manifestPath, []byte(m), 0o644); err != nil {
			return err
		}
	}

	// 3. aapt2 link -> base.apk
	baseAPK := filepath.Join(stage, "base.apk")
	if err := run(tc.aapt2, "link", "-o", baseAPK, "-I", tc.platformJar,
		"--manifest", manifestPath,
		"--min-sdk-version", strconv.Itoa(cfg.minSDK),
		"--target-sdk-version", strconv.Itoa(cfg.targetSDK)); err != nil {
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
	ks := cfg.keystore
	if ks == "" {
		home, _ := os.UserHomeDir()
		ks = filepath.Join(home, ".android", "debug.keystore")
		if !exists(ks) {
			if err := os.MkdirAll(filepath.Dir(ks), 0o755); err != nil {
				return err
			}
			fmt.Println(">> creating debug keystore", ks)
			if err := run("keytool", "-genkeypair", "-keystore", ks, "-alias", cfg.keyAlias,
				"-storepass", cfg.ksPass, "-keypass", cfg.keyPass, "-keyalg", "RSA", "-keysize", "2048",
				"-validity", "10000", "-dname", "CN=Android Debug,O=Android,C=US"); err != nil {
				return fmt.Errorf("keytool: %w", err)
			}
		}
	}

	// 7. apksigner sign -> output
	if err := run(tc.apksigner, "sign", "--ks", ks, "--ks-pass", "pass:"+cfg.ksPass,
		"--ks-key-alias", cfg.keyAlias, "--key-pass", "pass:"+cfg.keyPass,
		"--out", cfg.out, aligned); err != nil {
		return fmt.Errorf("apksigner: %w", err)
	}
	return nil
}

// cmdAndroidBuild is `goslint android build`: resolve flags, build the signed APK, then
// tell the user how to install it.
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

	cfg := resolveAndroidCfg(fs.Arg(0), *out, *appID, *label, *versionName, *versionCode, *minSDK, *targetSDK,
		*abiList, *manifestArg, *sdkArg, *ndkArg, *keystore, *ksPass, *keyAlias, *keyPass)
	if err := buildAPK(cfg); err != nil {
		return err
	}

	selected, _ := selectABIs(cfg.abiList)
	size := ""
	if fi, err := os.Stat(cfg.out); err == nil {
		size = fmt.Sprintf(", %.1f MB", float64(fi.Size())/(1<<20))
	}
	fmt.Printf("\n✓ %s  (%s, %s%s) — launchable\n", cfg.out, cfg.appID, strings.Join(abiNames(selected), "+"), size)
	fmt.Printf("  install: adb install -r %s\n", cfg.out)
	return nil
}

// cmdAndroidDev is `goslint android dev`: build + install + launch on a running device
// or a freshly-booted emulator, then watch for .slint/.go edits and rebuild → reinstall
// → relaunch. It shares the mobileDev driver with `goslint ios dev` — only the four
// device steps differ. Like the iOS one it can't hot-reload .slint in place (the app is
// sandboxed on the device), so each edit rebuilds the APK.
func cmdAndroidDev(args []string) error {
	fs := flag.NewFlagSet("android dev", flag.ExitOnError)
	avd := fs.String("avd", "", "AVD to boot if no device/emulator is already running (default: the first one)")
	appID := fs.String("package", "", "Android application id (default dev.goslint.<name>)")
	label := fs.String("label", "", "app display label (default <name>)")
	minSDK := fs.Int("min-sdk", 24, "minimum SDK version")
	sdkArg := fs.String("sdk", "", "Android SDK dir (default $ANDROID_HOME)")
	ndkArg := fs.String("ndk", "", "Android NDK dir (default $ANDROID_NDK_HOME or <sdk>/ndk/<latest>)")
	abiArg := fs.String("abi", "", "ABI to build (default: the running device's ABI — faster than building all)")
	_ = fs.Parse(args)

	tc, err := resolveAndroidTools(*sdkArg, *ndkArg)
	if err != nil {
		return err
	}
	if err := ensureBootedEmulator(tc, *avd); err != nil {
		return err
	}

	// Build only the running device's ABI (much faster than both); -abi overrides.
	abiList := *abiArg
	if abiList == "" {
		if abiList = deviceABI(tc); abiList == "" {
			abiList = "arm64-v8a"
		}
	}

	cfg := resolveAndroidCfg(fs.Arg(0), filepath.Join(os.TempDir(), "goslint-android-dev.apk"),
		*appID, *label, "1.0", 1, *minSDK, 34, abiList, "", *sdkArg, *ndkArg, "", "android", "androiddebugkey", "android")

	// Host env for codegen: typed projects regenerate their wrapper from .slint via
	// goslint-gen, which runs on the host and needs the host native lib. Best-effort —
	// dynamic-API projects (inline/embedded markup) don't generate at all.
	hostEnv, hostErr := wrapperEnv(hostTarget())
	genDir := cfg.pkg
	if fi, err := os.Stat(cfg.pkg); err == nil && !fi.IsDir() {
		genDir = filepath.Dir(cfg.pkg)
	}

	// The generated manifest always names android.app.NativeActivity, so launch it
	// directly (a cold start after the force-stop below).
	component := cfg.appID + "/android.app.NativeActivity"
	m := mobileDev{
		pkg: cfg.pkg,
		rebuild: func() error {
			if hostErr == nil && !newestExt(cfg.pkg, ".slint").IsZero() && needsGenerate(cfg.pkg) {
				fmt.Println(">> generating")
				if err := regenerate(genDir, hostEnv); err != nil {
					fmt.Fprintln(os.Stderr, "generate failed (using existing generated code):", err)
				}
			}
			return buildAPK(cfg)
		},
		install: func() error { return run(tc.adb, "install", "-r", cfg.out) },
		launch:  func() error { return run(tc.adb, "shell", "am", "start", "-n", component) },
		stop:    func() { _ = exec.Command(tc.adb, "shell", "am", "force-stop", cfg.appID).Run() },
	}
	return m.run()
}

// ensureBootedEmulator makes sure an Android device or emulator is online. If one is
// already connected (adb shows a ready `device`) it's used as-is; otherwise the named
// AVD — or the first available — is booted and we wait for it to finish coming up.
func ensureBootedEmulator(tc androidTools, avd string) error {
	if !exists(tc.adb) {
		return fmt.Errorf("adb not found at %s — install the SDK platform-tools", tc.adb)
	}
	if androidDeviceReady(tc) {
		return nil
	}
	name := avd
	if name == "" {
		if name = firstAVD(tc); name == "" {
			return fmt.Errorf("no running device and no AVD to boot — create one in Android Studio "+
				"or pass -avd <name> (list them with `%s -list-avds`)", tc.emulator)
		}
	}
	if !exists(tc.emulator) {
		return fmt.Errorf("emulator not found at %s — install it via the SDK manager", tc.emulator)
	}
	fmt.Printf(">> booting emulator: %s\n", name)
	// Start it detached — it keeps running in the background across rebuilds.
	cmd := exec.Command(tc.emulator, "-avd", name)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start emulator %q: %w", name, err)
	}
	if err := run(tc.adb, "wait-for-device"); err != nil {
		return fmt.Errorf("wait-for-device: %w", err)
	}
	fmt.Println(">> waiting for the emulator to finish booting…")
	return waitForBootComplete(tc)
}

// androidDeviceReady reports whether adb lists at least one device in the ready
// ("device") state — an already-running emulator or a plugged-in phone.
func androidDeviceReady(tc androidTools) bool {
	out, err := exec.Command(tc.adb, "devices").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == "device" {
			return true
		}
	}
	return false
}

// firstAVD returns the name of the first configured AVD, or "" if none exist.
func firstAVD(tc androidTools) string {
	out, err := exec.Command(tc.emulator, "-list-avds").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

// waitForBootComplete polls until the device reports sys.boot_completed=1 (a cold
// emulator can take a minute or two), so the first install doesn't race the boot.
func waitForBootComplete(tc androidTools) error {
	for i := 0; i < 180; i++ {
		out, _ := exec.Command(tc.adb, "shell", "getprop", "sys.boot_completed").Output()
		if strings.TrimSpace(string(out)) == "1" {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("the emulator did not finish booting in time")
}

// deviceABI returns the running device's primary ABI (e.g. "arm64-v8a" or "x86_64") if
// it's one we build for, else "" so the caller falls back to a default.
func deviceABI(tc androidTools) string {
	out, err := exec.Command(tc.adb, "shell", "getprop", "ro.product.cpu.abi").Output()
	if err != nil {
		return ""
	}
	abi := strings.TrimSpace(string(out))
	for _, a := range abis {
		if a.abi == abi {
			return abi
		}
	}
	return ""
}

// ensureAndroidEntry makes sure the package at pkg has an Android entry point — the
// file exporting goslint_android_main. `goslint init` omits it (so a desktop project
// has nothing to confuse editors), so write it on the first android build. The
// template calls run(), which the scaffolded app.go provides; a project that defines
// run() elsewhere gets a working entry too. Any existing entry (whatever its name) is
// left untouched, so we never create a duplicate. The file is gated on the custom
// goslint_android tag — see androidTemplate for why — so the build passes that tag.
func ensureAndroidEntry(pkg string) error {
	dir := pkg
	if fi, err := os.Stat(pkg); err == nil && !fi.IsDir() {
		dir = filepath.Dir(pkg)
	}
	if androidEntryFile(dir) != "" {
		return nil // already has an entry; don't add a second one
	}
	entry := filepath.Join(dir, "android_main.go")
	if err := os.WriteFile(entry, []byte(androidTemplate), 0o644); err != nil {
		return err
	}
	fmt.Printf("created %s — the Android entry point (built only for android)\n", entry)
	return nil
}

// androidEntryFile returns the path of a .go file in dir that exports
// goslint_android_main, or "" if none — so ensureAndroidEntry won't scaffold a
// duplicate over a hand-written entry (whatever it's named).
func androidEntryFile(dir string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "//export goslint_android_main") {
			return p
		}
	}
	return ""
}

// androidEntryExported reports whether the built c-shared exports goslint_android_main,
// the symbol the Rust android_main dlsym's and calls (see rust/goslint-sys/src/android.rs).
// If it's absent the APK installs but the NativeActivity does nothing — the common trap
// when a package is missing its Android entry file. dlsym resolves against the
// dynamic symbol table, so that's what we check.
func androidEntryExported(soPath string) (bool, error) {
	f, err := elf.Open(soPath)
	if err != nil {
		return false, err
	}
	defer f.Close()
	syms, err := f.DynamicSymbols()
	if err != nil {
		return false, err
	}
	for _, s := range syms {
		if s.Name == "goslint_android_main" {
			return true, nil
		}
	}
	return false, nil
}

const noAndroidEntryMsg = `package %q builds, but its Android library does not export goslint_android_main —
the APK would install yet never launch (Android's NativeActivity loads the Go library and
calls goslint_android_main; with it missing, nothing happens).

Add an Android entry point in android_main.go (the custom goslint_android tag, not the
android GOOS, keeps editors from cross-building it):

    //go:build goslint_android

    package main

    import "C"
    import "runtime"

    //export goslint_android_main
    func goslint_android_main(_ *C.char) {
        runtime.LockOSThread() // Slint is thread-affine
        _ = run()              // open your window and run the event loop
    }

    func main() {} // required for c-shared; unused on android

` + "`goslint android build`" + ` writes this for you on first run; this message only appears if it
builds but the symbol is still missing (e.g. run() is undefined).`

// ---- android toolchain resolution ----

type androidTools struct {
	sdk, ndkBin, buildTools, platformJar, aapt2, zipalign, apksigner string
	adb, emulator                                                    string // only needed by `goslint android dev`
}

func resolveAndroidTools(sdkArg, ndkArg string) (androidTools, error) {
	var t androidTools
	t.sdk = findSDK(sdkArg)
	if t.sdk == "" {
		return t, fmt.Errorf("Android SDK not found — looked at -sdk, $ANDROID_HOME, " +
			"$ANDROID_SDK_ROOT, ~/android-sdk, ~/Android/Sdk, ~/Library/Android/sdk, " +
			"and the Homebrew/apt SDK dir (none had build-tools). Pass -sdk <dir> or set ANDROID_HOME")
	}
	// Only `goslint android dev` uses these; existence is checked there (a plain build
	// needs neither), so record the paths unconditionally.
	t.adb = filepath.Join(t.sdk, "platform-tools", "adb")
	t.emulator = filepath.Join(t.sdk, "emulator", "emulator")
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

	// NDK: try -ndk, the env vars, then the newest under <sdk>/ndk; pick the first
	// whose toolchain bin actually exists.
	latestNDK, _ := latestDir(filepath.Join(t.sdk, "ndk"), "*")
	for _, ndk := range []string{ndkArg, os.Getenv("ANDROID_NDK_HOME"), os.Getenv("ANDROID_NDK_ROOT"), latestNDK} {
		if ndk == "" {
			continue
		}
		bin := filepath.Join(ndk, "toolchains", "llvm", "prebuilt", ndkHostTag(), "bin")
		if exists(bin) {
			t.ndkBin = bin
			break
		}
	}
	if t.ndkBin == "" {
		return t, fmt.Errorf("Android NDK not found under %s/ndk or $ANDROID_NDK_HOME — install one or pass -ndk <dir>", t.sdk)
	}
	return t, nil
}

// findSDK returns the first candidate location that actually contains build-tools,
// so an empty/stale ANDROID_HOME doesn't win over a real SDK on disk.
func findSDK(sdkArg string) string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		sdkArg,
		os.Getenv("ANDROID_HOME"),
		os.Getenv("ANDROID_SDK_ROOT"),
		filepath.Join(home, "android-sdk"),
		filepath.Join(home, "Android", "Sdk"),
		filepath.Join(home, "Library", "Android", "sdk"),
	}
	candidates = append(candidates, pkgManagerSDKs()...)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := latestDir(filepath.Join(c, "build-tools"), "*"); err == nil {
			return c
		}
	}
	return ""
}

// pkgManagerSDKs lists SDK roots that common package managers use, so a Homebrew or
// apt install resolves without setting ANDROID_HOME. Non-existent paths are harmless:
// findSDK only returns one that actually contains build-tools.
func pkgManagerSDKs() []string {
	switch runtime.GOOS {
	case "darwin": // brew install --cask android-commandlinetools
		return []string{
			"/opt/homebrew/share/android-commandlinetools", // Apple Silicon
			"/usr/local/share/android-commandlinetools",    // Intel
		}
	case "linux": // apt install android-sdk
		return []string{"/usr/lib/android-sdk"}
	}
	return nil
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
