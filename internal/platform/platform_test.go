package platform

import "testing"

func TestForMapsSupportedTargets(t *testing.T) {
	tests := []struct {
		goos, goarch, wantOS, wantArch, wantShell string
	}{
		{"darwin", "arm64", "macos", "arm64", "bash"},
		{"windows", "amd64", "windows", "x64", "powershell"},
		{"linux", "amd64", "linux", "x64", "bash"},
	}
	for _, test := range tests {
		got := For(test.goos, test.goarch)
		if got.OS != test.wantOS || got.Arch != test.wantArch || got.Shell != test.wantShell {
			t.Errorf("For(%q, %q) = %#v", test.goos, test.goarch, got)
		}
	}
}

func TestResolveHomeWindowsPrecedence(t *testing.T) {
	env := map[string]string{
		"ONEAGENT_HOME": "/override",
		"USERPROFILE":   `C:\Users\测试 用户`,
		"HOME":          "/wrong",
	}
	if got := ResolveHome(env, "windows"); got != "/override" {
		t.Fatalf("ONEAGENT_HOME precedence = %q", got)
	}
	delete(env, "ONEAGENT_HOME")
	if got := ResolveHome(env, "windows"); got != `C:\Users\测试 用户` {
		t.Fatalf("USERPROFILE precedence = %q", got)
	}
	delete(env, "USERPROFILE")
	env["HOMEDRIVE"] = "D:"
	env["HOMEPATH"] = `\Users\fallback`
	if got := ResolveHome(env, "windows"); got != `D:\Users\fallback` {
		t.Fatalf("HOMEDRIVE/HOMEPATH precedence = %q", got)
	}
}

func TestResolveHomeExpandsTilde(t *testing.T) {
	env := map[string]string{"HOME": "/tmp/test-home"}
	if got := ResolveHome(map[string]string{"ONEAGENT_HOME": "~/oneagent", "HOME": env["HOME"]}, "linux"); got != "/tmp/test-home/oneagent" {
		t.Fatalf("expanded home = %q", got)
	}
}
