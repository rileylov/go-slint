package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// iOS is markedly simpler than Android: Go builds a normal executable (Go can't
// make a c-archive on Android, so there the Rust android_main dlopen's the Go lib;
// on iOS no such inversion is needed). Slint's winit backend calls UIApplicationMain
// itself when the event loop runs, so a plain `func main()` that opens a window and
// runs — the very same code a desktop app uses — IS the whole iOS app. `goslint ios
// build` therefore just cross-compiles that package for iOS, links the prebuilt Skia/
// Metal shim, and wraps the binary in a signed .app bundle.

// iosPlatform describes one of the two iOS build flavors.
type iosPlatform struct {
	relTarget   string // release/manifest target key for the prebuilt shim .a
	sdk         string // xcrun --sdk name
	bundlePlat  string // CFBundleSupportedPlatforms value
	clangSuffix string // appended to arm64-apple-ios<min> for the clang -target
}

var (
	iosSimulator = iosPlatform{"ios_sim_arm64", "iphonesimulator", "iPhoneSimulator", "-simulator"}
	iosDevice    = iosPlatform{"ios_arm64", "iphoneos", "iPhoneOS", ""}
)

// iosDefaultFrameworks is the Skia/Metal link line used when building against a
// local shim (-lib), which has no manifest. A provisioned build uses the frameworks
// the release recorded (target.Libs) instead. Kept in sync with the shim's
// `--print native-static-libs` on aarch64-apple-ios{,-sim} (deduped by the linker).
const iosDefaultFrameworks = "-framework Accessibility -framework CoreGraphics -lc++ " +
	"-framework CoreFoundation -framework CoreText -framework ImageIO " +
	"-framework MobileCoreServices -framework UIKit -framework Metal " +
	"-framework QuartzCore -framework Foundation -lobjc -liconv"

func cmdIOS(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: goslint ios <build|dev> [flags] <package>")
	}
	switch args[0] {
	case "build":
		return cmdIOSBuild(args[1:])
	case "dev":
		return cmdIOSDev(args[1:])
	default:
		return fmt.Errorf("usage: goslint ios <build|dev> [flags] <package>")
	}
}

// iosBuildCfg is the fully-resolved input to buildIOSApp (the caller applies defaults
// via resolveIOSCfg). Shared by `goslint ios build` and `goslint ios dev`.
type iosBuildCfg struct {
	pkg, out, bundleID, label, versionName string
	versionCode                            int
	minOS, orientations, sign, libDir      string
	device                                 bool
}

// resolveIOSCfg fills the defaults (name / bundle-id / label / output) from the package.
func resolveIOSCfg(pkg, out, bundleID, label, versionName string, versionCode int, minOS, orientations, sign, libDir string, device bool) iosBuildCfg {
	if pkg == "" {
		pkg = "."
	}
	name := appName(pkg, label)
	if bundleID == "" {
		bundleID = "dev.goslint." + sanitizeID(name)
	}
	if label == "" {
		label = name
	}
	if out == "" {
		out = name + ".app"
	}
	return iosBuildCfg{pkg, out, bundleID, label, versionName, versionCode, minOS, orientations, sign, libDir, device}
}

// buildIOSApp cross-compiles cfg.pkg for iOS and writes a signed .app to cfg.out. The
// package just needs a normal `func main()` that opens a window and runs the event loop
// (with runtime.LockOSThread pinned) — Slint's winit backend calls UIApplicationMain
// itself, so no iOS-specific entry point is required.
func buildIOSApp(cfg iosBuildCfg) error {
	plat := iosSimulator
	if cfg.device {
		plat = iosDevice
	}

	// resolve the SDK + clang target triple for this platform.
	sdkPath, err := xcrunSDKPath(plat.sdk)
	if err != nil {
		return err
	}
	clangTarget := "arm64-apple-ios" + cfg.minOS + plat.clangSuffix

	// the prebuilt Skia/Metal shim .a + its frameworks: a local -lib for shim
	// development, otherwise the release download (like `goslint setup`).
	libA, frameworks, err := iosShimLib(plat, cfg.libDir)
	if err != nil {
		return err
	}

	// cross-build the Go package as an iOS executable. Go emits a normal Mach-O; with
	// CC targeting the iOS SDK it carries the right LC_BUILD_VERSION platform.
	stage, err := os.MkdirTemp("", "goslint-ios")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	execName := execBaseName(cfg.label)
	exePath := filepath.Join(stage, execName)
	sysroot := fmt.Sprintf("-target %s -isysroot %s", clangTarget, sdkPath)
	env := []string{
		"GOOS=ios", "GOARCH=arm64", "CGO_ENABLED=1", "CC=clang",
		"CGO_CFLAGS=" + sysroot,
		// The shim .a is named by full path (not -l:), and appears twice in the final
		// link via Go's CGO_LDFLAGS doubling — that second pass resolves the archive↔
		// framework cycle, the macOS/iOS analog of GNU ld's --start-group.
		"CGO_LDFLAGS=" + fmt.Sprintf("%s -L%s %s %s", sysroot, filepath.Dir(libA), libA, frameworks),
	}
	flavor := "simulator"
	if cfg.device {
		flavor = "device"
	}
	fmt.Printf("Building %s  (%s, min iOS %s)\n", cfg.out, flavor, cfg.minOS)
	fmt.Printf("   cross-building %s (ios/arm64)…\n", cfg.pkg)
	if err := runEnv(env, "go", "build", "-tags", buildTag, "-o", exePath, cfg.pkg); err != nil {
		return fmt.Errorf("go build (ios): %w", err)
	}

	// assemble the .app bundle (binary + Info.plist).
	if err := os.RemoveAll(cfg.out); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.out, 0o755); err != nil {
		return err
	}
	if err := copyExec(exePath, filepath.Join(cfg.out, execName)); err != nil {
		return err
	}
	plist := genIOSPlist(cfg.bundleID, cfg.label, execName, cfg.versionName, cfg.versionCode, cfg.minOS, plat.bundlePlat, parseOrientations(cfg.orientations))
	if err := os.WriteFile(filepath.Join(cfg.out, "Info.plist"), []byte(plist), 0o644); err != nil {
		return err
	}

	// code-sign. The simulator accepts ad-hoc ("-"); a device needs a real identity
	// (a free Personal Team works via Xcode/`security find-identity`).
	id := cfg.sign
	if id == "" {
		id = "-"
	}
	if err := run("codesign", "--force", "--sign", id, "--timestamp=none", cfg.out); err != nil {
		return fmt.Errorf("codesign (%s): %w", id, err)
	}
	return nil
}

// cmdIOSBuild is `goslint ios build`: resolve flags, build the .app, then optionally
// install + launch it in the booted simulator (-run).
func cmdIOSBuild(args []string) error {
	fs := flag.NewFlagSet("ios build", flag.ExitOnError)
	out := fs.String("o", "", "output .app bundle path (default <name>.app)")
	device := fs.Bool("device", false, "build for a physical device instead of the simulator")
	bundleID := fs.String("bundle-id", "", "CFBundleIdentifier (default dev.goslint.<name>)")
	label := fs.String("label", "", "app display name (default <name>)")
	versionName := fs.String("version-name", "1.0", "CFBundleShortVersionString")
	versionCode := fs.Int("version-code", 1, "CFBundleVersion")
	minOS := fs.String("min-os", "14.0", "minimum iOS version")
	orientations := fs.String("orientations", "portrait", "comma list: portrait,landscape-left,landscape-right,portrait-upside-down")
	sign := fs.String("sign", "", "code-signing identity for a device build (default: ad-hoc)")
	libDir := fs.String("lib", "", "dir containing a locally-built libgoslint.a (skips the download; for shim development)")
	runSim := fs.Bool("run", false, "after building, install + launch in the booted simulator")
	_ = fs.Parse(args)

	cfg := resolveIOSCfg(fs.Arg(0), *out, *bundleID, *label, *versionName, *versionCode, *minOS, *orientations, *sign, *libDir, *device)
	if err := buildIOSApp(cfg); err != nil {
		return err
	}

	size := ""
	if fi, err := os.Stat(filepath.Join(cfg.out, execBaseName(cfg.label))); err == nil {
		size = fmt.Sprintf(", %.1f MB", float64(fi.Size())/(1<<20))
	}
	fmt.Printf("\n✓ %s  (%s%s)\n", cfg.out, cfg.bundleID, size)

	if *runSim && !cfg.device {
		fmt.Println(">> installing in the booted simulator")
		if err := run("xcrun", "simctl", "install", "booted", cfg.out); err != nil {
			return fmt.Errorf("simctl install (boot a simulator first, e.g. `xcrun simctl boot 'iPhone 17 Pro'`): %w", err)
		}
		if err := run("xcrun", "simctl", "launch", "booted", cfg.bundleID); err != nil {
			return fmt.Errorf("simctl launch: %w", err)
		}
		fmt.Println(">> launched")
		return nil
	}
	if cfg.device {
		fmt.Printf("  install to a device: `ios-deploy -b %s` (needs a provisioning profile) or drag it onto Xcode → Devices\n", cfg.out)
	} else {
		fmt.Printf("  run: xcrun simctl boot 'iPhone 17 Pro'; xcrun simctl install booted %s; xcrun simctl launch booted %s\n", cfg.out, cfg.bundleID)
		fmt.Printf("       (or re-run with -run to do that automatically)\n")
	}
	return nil
}

// cmdIOSDev is `goslint ios dev`: build + install + launch in the simulator, then watch
// for .slint/.go edits and rebuild → reinstall → relaunch. The simulator sandbox blocks
// the in-process .slint hot-reload that desktop `dev` uses, so this restarts the app;
// the shared mobileDev driver runs the watch loop.
func cmdIOSDev(args []string) error {
	fs := flag.NewFlagSet("ios dev", flag.ExitOnError)
	device := fs.String("device", "iPhone 17 Pro", "simulator device to boot if none is running")
	bundleID := fs.String("bundle-id", "", "CFBundleIdentifier (default dev.goslint.<name>)")
	label := fs.String("label", "", "app display name (default <name>)")
	minOS := fs.String("min-os", "14.0", "minimum iOS version")
	libDir := fs.String("lib", "", "dir with a locally-built libgoslint.a (shim development)")
	_ = fs.Parse(args)

	cfg := resolveIOSCfg(fs.Arg(0), filepath.Join(os.TempDir(), "goslint-ios-dev.app"),
		*bundleID, *label, "1.0", 1, *minOS, "portrait", "", *libDir, false)

	if err := ensureBootedSimulator(*device); err != nil {
		return err
	}

	// Host env for codegen: typed projects regenerate their wrapper from .slint via
	// goslint-gen, which runs on the host and needs the host native lib. Best-effort —
	// dynamic-API projects (inline/embedded markup) don't generate at all.
	hostEnv, hostErr := wrapperEnv(hostTarget())
	genDir := cfg.pkg
	if fi, err := os.Stat(cfg.pkg); err == nil && !fi.IsDir() {
		genDir = filepath.Dir(cfg.pkg)
	}

	m := mobileDev{
		pkg: cfg.pkg,
		rebuild: func() error {
			if hostErr == nil && !newestExt(cfg.pkg, ".slint").IsZero() && needsGenerate(cfg.pkg) {
				fmt.Println(">> generating")
				if err := regenerate(genDir, hostEnv); err != nil {
					fmt.Fprintln(os.Stderr, "generate failed (using existing generated code):", err)
				}
			}
			return buildIOSApp(cfg)
		},
		install: func() error { return run("xcrun", "simctl", "install", "booted", cfg.out) },
		launch:  func() error { return run("xcrun", "simctl", "launch", "booted", cfg.bundleID) },
		stop:    func() { _ = exec.Command("xcrun", "simctl", "terminate", "booted", cfg.bundleID).Run() },
	}
	return m.run()
}

// ensureBootedSimulator boots `device` if no simulator is running, and opens the
// Simulator UI so the app is visible.
func ensureBootedSimulator(device string) error {
	out, _ := exec.Command("xcrun", "simctl", "list", "devices", "booted").Output()
	if strings.Contains(string(out), "(Booted)") {
		_ = exec.Command("open", "-a", "Simulator").Run()
		return nil
	}
	fmt.Printf(">> booting simulator: %s\n", device)
	if err := run("xcrun", "simctl", "boot", device); err != nil {
		return fmt.Errorf("boot simulator %q (list options with `xcrun simctl list devices`): %w", device, err)
	}
	_ = exec.Command("open", "-a", "Simulator").Run()
	return run("xcrun", "simctl", "bootstatus", device, "-b")
}

// iosShimLib returns the shim archive path and its framework link line, either from
// a locally-built -lib dir or by provisioning the prebuilt from the release.
func iosShimLib(plat iosPlatform, libDir string) (libA, frameworks string, err error) {
	if libDir != "" {
		libA = filepath.Join(libDir, "libgoslint.a")
		if !exists(libA) {
			return "", "", fmt.Errorf("no libgoslint.a in %s", libDir)
		}
		return libA, iosDefaultFrameworks, nil
	}
	t, libpath, _, err := provision(plat.relTarget, false)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", plat.relTarget, err)
	}
	if t.Kind == "shared" {
		return "", "", fmt.Errorf("%s: expected a static (.a) target", plat.relTarget)
	}
	frameworks = t.Libs
	if frameworks == "" {
		frameworks = iosDefaultFrameworks
	}
	return libpath, frameworks, nil
}

// xcrunSDKPath resolves an iOS SDK path via xcrun (needs a full Xcode, selected with
// `xcode-select -s`).
func xcrunSDKPath(sdk string) (string, error) {
	out, err := exec.Command("xcrun", "--sdk", sdk, "--show-sdk-path").Output()
	if err != nil {
		return "", fmt.Errorf("could not locate the %s SDK — is a full Xcode installed and selected? "+
			"Check `xcode-select -p` (should point at Xcode.app, not CommandLineTools): %w", sdk, err)
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", fmt.Errorf("empty %s SDK path from xcrun", sdk)
	}
	return p, nil
}

// execBaseName makes a filesystem-safe CFBundleExecutable name (no spaces/slashes).
func execBaseName(name string) string {
	r := strings.NewReplacer(" ", "", "/", "", ":", "")
	e := r.Replace(name)
	if e == "" {
		return "app"
	}
	return e
}

func copyExec(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

func parseOrientations(s string) []string {
	names := map[string]string{
		"portrait":             "UIInterfaceOrientationPortrait",
		"portrait-upside-down": "UIInterfaceOrientationPortraitUpsideDown",
		"landscape-left":       "UIInterfaceOrientationLandscapeLeft",
		"landscape-right":      "UIInterfaceOrientationLandscapeRight",
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if v, ok := names[strings.TrimSpace(p)]; ok {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		out = []string{"UIInterfaceOrientationPortrait"}
	}
	return out
}

func genIOSPlist(bundleID, label, execName, versionName string, versionCode int, minOS, platform string, orientations []string) string {
	var orient strings.Builder
	for _, o := range orientations {
		orient.WriteString("\n        <string>")
		orient.WriteString(o)
		orient.WriteString("</string>")
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key><string>%s</string>
    <key>CFBundleIdentifier</key><string>%s</string>
    <key>CFBundleName</key><string>%s</string>
    <key>CFBundleDisplayName</key><string>%s</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>CFBundleShortVersionString</key><string>%s</string>
    <key>CFBundleVersion</key><string>%s</string>
    <key>CFBundleSupportedPlatforms</key><array><string>%s</string></array>
    <key>MinimumOSVersion</key><string>%s</string>
    <key>UIDeviceFamily</key><array><integer>1</integer><integer>2</integer></array>
    <key>UILaunchScreen</key><dict/>
    <key>UISupportedInterfaceOrientations</key><array>%s
    </array>
    <key>DTPlatformName</key><string>%s</string>
</dict>
</plist>
`, execName, bundleID, label, label, versionName, strconv.Itoa(versionCode),
		platform, minOS, orient.String(), strings.ToLower(platform))
}

// iosDoctor reports iOS build readiness for `goslint doctor`. iOS apps can only be
// built on macOS, so it's a no-op elsewhere (unlike the cross-platform Android check).
// Mirrors that check's shape: optional, shows what resolves and any remaining gap so
// `goslint ios build` can be verified without a full build.
func iosDoctor() {
	if runtime.GOOS != "darwin" {
		return
	}
	// xcrun locating the simulator SDK is the reliable signal that a *full* Xcode (not
	// just the Command Line Tools) is installed and selected.
	sdk, err := xcrunSDKPath("iphonesimulator")
	if err != nil {
		fmt.Println("  – iOS toolchain (optional; only for `goslint ios build`): not ready")
		fmt.Println("               → install a full Xcode, then select it: sudo xcode-select -s /Applications/Xcode.app")
		return
	}
	report("iOS toolchain (optional)", true)
	fmt.Printf("               → SDK: %s\n", sdk)
	// A simulator runtime is needed for `goslint ios build -run` (and any simctl use).
	if rt := iosSimulatorRuntime(); rt != "" {
		fmt.Printf("               → simulator runtime: %s\n", rt)
	} else {
		fmt.Println("               → no iOS simulator runtime installed — add one: xcodebuild -downloadPlatform iOS")
	}
}

// iosSimulatorRuntime returns a short label for an installed, usable iOS simulator
// runtime (e.g. "iOS 26.5"), or "" if none is present.
func iosSimulatorRuntime() string {
	out, err := exec.Command("xcrun", "simctl", "list", "runtimes").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "iOS ") && !strings.Contains(line, "unavailable") {
			if f := strings.Fields(line); len(f) >= 2 {
				return f[0] + " " + f[1] // "iOS" + "<version>"
			}
		}
	}
	return ""
}
