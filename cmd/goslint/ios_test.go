package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecBaseName(t *testing.T) {
	for in, want := range map[string]string{
		"hello":   "hello",
		"My App":  "MyApp",   // spaces stripped
		"a/b:c":   "abc",     // slashes and colons stripped
		"Counter": "Counter", // case preserved (unlike sanitizeID)
		"":        "app",     // empty -> placeholder
		"   ":     "app",     // all-stripped -> placeholder
	} {
		if got := execBaseName(in); got != want {
			t.Errorf("execBaseName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParseOrientations(t *testing.T) {
	one := parseOrientations("portrait")
	if len(one) != 1 || one[0] != "UIInterfaceOrientationPortrait" {
		t.Errorf("portrait -> %v", one)
	}
	two := parseOrientations("landscape-left,landscape-right")
	if len(two) != 2 || two[0] != "UIInterfaceOrientationLandscapeLeft" || two[1] != "UIInterfaceOrientationLandscapeRight" {
		t.Errorf("landscape pair -> %v", two)
	}
	// unknown tokens are dropped, known ones kept
	if got := parseOrientations("portrait,bogus,landscape-left"); len(got) != 2 {
		t.Errorf("mixed -> %v, want 2 valid", got)
	}
	// all-unknown (or empty) falls back to portrait
	for _, in := range []string{"", "bogus", " , "} {
		if got := parseOrientations(in); len(got) != 1 || got[0] != "UIInterfaceOrientationPortrait" {
			t.Errorf("parseOrientations(%q)=%v, want [portrait] default", in, got)
		}
	}
}

func TestGenIOSPlist(t *testing.T) {
	p := genIOSPlist("dev.goslint.demo", "Demo", "demo", "1.2", 3, "15.0", "iPhoneSimulator",
		[]string{"UIInterfaceOrientationPortrait"})
	for _, want := range []string{
		"<key>CFBundleExecutable</key><string>demo</string>",
		"<key>CFBundleIdentifier</key><string>dev.goslint.demo</string>",
		"<key>CFBundleDisplayName</key><string>Demo</string>",
		"<key>CFBundleShortVersionString</key><string>1.2</string>",
		"<key>CFBundleVersion</key><string>3</string>",
		"<string>iPhoneSimulator</string>",
		"<key>MinimumOSVersion</key><string>15.0</string>",
		"<key>UILaunchScreen</key><dict/>",
		"<string>UIInterfaceOrientationPortrait</string>",
		"<key>DTPlatformName</key><string>iphonesimulator</string>", // platform lowercased
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plist missing %q", want)
		}
	}
}

func TestResolveIOSCfg(t *testing.T) {
	// Defaults derive from the package base name (absolute path -> deterministic base).
	def := resolveIOSCfg("/tmp/cool", "", "", "", "1.0", 1, "14.0", "portrait", "", "", false)
	if def.pkg != "/tmp/cool" || def.label != "cool" || def.bundleID != "dev.goslint.cool" || def.out != "cool.app" {
		t.Errorf("defaults: %+v", def)
	}

	// Explicit flags win over the derived defaults.
	ov := resolveIOSCfg("/tmp/cool", "Out.app", "com.x.y", "My Label", "2.0", 5, "16.0", "landscape-left", "MyIdentity", "/libs", true)
	if ov.out != "Out.app" || ov.bundleID != "com.x.y" || ov.label != "My Label" ||
		ov.sign != "MyIdentity" || ov.libDir != "/libs" || !ov.device {
		t.Errorf("overrides: %+v", ov)
	}

	// Empty package resolves to "." (current directory).
	if cur := resolveIOSCfg("", "", "", "", "1.0", 1, "14.0", "portrait", "", "", false); cur.pkg != "." {
		t.Errorf("empty pkg -> %q, want %q", cur.pkg, ".")
	}
}

func TestIOSShimLibLocal(t *testing.T) {
	dir := t.TempDir()

	// No libgoslint.a in the -lib dir -> a clear error (not a download attempt).
	if _, _, err := iosShimLib(iosSimulator, dir); err == nil {
		t.Error("expected an error when the -lib dir has no libgoslint.a")
	}

	// Present -> returns its path and the baked-in iOS framework link line.
	lib := filepath.Join(dir, "libgoslint.a")
	if err := os.WriteFile(lib, []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	libA, frameworks, err := iosShimLib(iosSimulator, dir)
	if err != nil {
		t.Fatalf("iosShimLib(local): %v", err)
	}
	if libA != lib {
		t.Errorf("libA=%q want %q", libA, lib)
	}
	for _, fw := range []string{"-framework Metal", "-framework UIKit", "-framework QuartzCore"} {
		if !strings.Contains(frameworks, fw) {
			t.Errorf("frameworks missing %q: %s", fw, frameworks)
		}
	}
}
