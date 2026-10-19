package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func startSandbox(t *testing.T, switchUsers bool) (*Server, string, string) {
	t.Helper()
	root := t.TempDir()
	workspaces := filepath.Join(root, "workspaces")
	workspace := filepath.Join(workspaces, "org-1")
	if err := os.MkdirAll(workspace, 0o777); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(workspace, 0o777)
	_ = os.Chmod(root, 0o755)
	_ = os.Chmod(filepath.Dir(root), 0o755)
	server := &Server{Workspaces: workspaces, Homes: filepath.Join(root, "homes"), BaseUID: 23000, Users: 2, GID: os.Getgid(), SwitchUsers: switchUsers, PATH: "/usr/bin:/bin"}
	socket := filepath.Join(root, "sandbox.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe(ctx, socket) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return server, socket, workspace
}

func TestSandboxRunsCommandsWithoutSecretsAsTheRunUser(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "must-not-leak")
	switchUsers := os.Geteuid() == 0
	server, socket, workspace := startSandbox(t, switchUsers)
	client := NewClient(socket, workspace)
	ctx := context.Background()

	result, err := client.Exec(ctx, "id -u; env; echo hi > out.txt", "", 10, 10_000)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("exec = %+v, %v", result, err)
	}
	if strings.Contains(result.Output, "must-not-leak") || strings.Contains(result.Output, "OPENAI_API_KEY") {
		t.Fatalf("the sandbox passed a secret to the command:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, "HOME="+filepath.Join(server.Homes, client.run)) {
		t.Fatalf("the run has no home of its own:\n%s", result.Output)
	}
	if switchUsers {
		if first := strings.SplitN(result.Output, "\n", 2)[0]; first != strconv.Itoa(server.BaseUID) {
			t.Fatalf("command ran as uid %s, want %d", first, server.BaseUID)
		}
		info, err := os.Stat(filepath.Join(workspace, "out.txt"))
		if err != nil || info.Mode().Perm()&0o020 == 0 {
			t.Fatalf("a file the run wrote is not group-writable: %v %v", info.Mode(), err)
		}
	}

	other := NewClient(socket, workspace)
	if result, _ := other.Exec(ctx, "id -u", "", 10, 10_000); switchUsers && strings.TrimSpace(result.Output) == strconv.Itoa(server.BaseUID) {
		t.Fatal("two runs share a user")
	}

	if _, err := NewClient(socket, "/etc").Exec(ctx, "true", "", 10, 10_000); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("a workspace outside the sandbox was admitted: %v", err)
	}

	if _, err := client.Exec(ctx, "sleep 60 >/dev/null 2>&1 &", "", 10, 10_000); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if switchUsers && len(processesOf(server.BaseUID)) > 0 {
		t.Fatal("ending the run left its background process running")
	}
	if _, err := os.Stat(filepath.Join(server.Homes, client.run)); !os.IsNotExist(err) {
		t.Fatalf("ending the run left its home: %v", err)
	}
	if _, err := other.Exec(ctx, "true", "", 10, 10_000); err != nil {
		t.Fatal(err)
	}
	third := NewClient(socket, workspace)
	if _, err := third.Exec(ctx, "true", "", 10, 10_000); err != nil {
		t.Fatalf("an ended run's user was not freed: %v", err)
	}
}

func TestSandboxSocketBelongsToTheSharedGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("only root can hand the socket to another group")
	}
	root := t.TempDir()
	server := &Server{Workspaces: root, Homes: filepath.Join(root, "homes"), BaseUID: 23000, Users: 1, GID: 23999, SwitchUsers: true, PATH: "/usr/bin:/bin"}
	socket := filepath.Join(root, "sandbox.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe(ctx, socket) }()
	defer func() {
		cancel()
		<-done
	}()
	var info os.FileInfo
	for i := 0; i < 100; i++ {
		if found, err := os.Stat(socket); err == nil && found.Sys().(*syscall.Stat_t).Gid == 23999 {
			info = found
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if info == nil {
		t.Fatal("the socket was not handed to the shared group")
	}
	if perm := info.Mode().Perm(); perm != 0o660 {
		t.Fatalf("socket mode = %o, want 660", perm)
	}
}

func processesOf(uid int) []int {
	var pids []int
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		status, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if err != nil || strings.Contains(string(status), "State:\tZ") {
			continue
		}
		for _, line := range strings.Split(string(status), "\n") {
			if fields := strings.Fields(line); len(fields) > 1 && fields[0] == "Uid:" && fields[1] == strconv.Itoa(uid) {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}
