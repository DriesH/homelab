package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func validMinecraft() MinecraftAnswers {
	return MinecraftAnswers{
		AcceptEULA: true, Memory: 4, MOTD: "Dries' world", Difficulty: "normal", Mode: "survival",
		MaxPlayers: 10, ViewDistance: 10, Operator: "Dries_H", Whitelist: []string{"Friend1", "other_friend"},
		PlayitSecretKey: "0123456789abcdef0123", PublicAddress: "dries.joinmc.link", Storage: "local-lvm", WorldSize: 20,
	}
}

func newMinecraftInstaller(t *testing.T) (*AppInstaller, *fakeAppUnit) {
	t.Helper()
	installer, unit := newTestInstaller(t)
	if err := os.MkdirAll(filepath.Join(installer.StacksDir, "minecraft"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installer.StacksDir, "minecraft", "install.sh"), nil, 0o755); err != nil {
		t.Fatal(err)
	}

	return installer, unit
}

func TestMinecraftAnswersValidate(t *testing.T) {
	// The apostrophe is not allowed: the MOTD goes in a quoted .env value.
	answers := validMinecraft()
	if err := answers.Validate(); !errors.Is(err, ErrInvalidAnswers) {
		t.Fatalf("MOTD with an apostrophe: %v", err)
	}

	valid := func() MinecraftAnswers {
		answers := validMinecraft()
		answers.MOTD = "Dries world!"
		return answers
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	optional := valid()
	optional.PlayitSecretKey, optional.PublicAddress, optional.Whitelist, optional.Seed = "", "", nil, "-12345"
	if err := optional.Validate(); err != nil {
		t.Fatalf("without the optional fields: %v", err)
	}

	bad := map[string]func(*MinecraftAnswers){
		"no EULA":                  func(a *MinecraftAnswers) { a.AcceptEULA = false },
		"3 GB":                     func(a *MinecraftAnswers) { a.Memory = 3 },
		"MOTD with a new line":     func(a *MinecraftAnswers) { a.MOTD = "hi\nPLAYIT_SECRET_KEY=x" },
		"MOTD with a dollar":       func(a *MinecraftAnswers) { a.MOTD = "costs $5" },
		"MOTD too long":            func(a *MinecraftAnswers) { a.MOTD = strings.Repeat("a", 60) },
		"hardcore difficulty":      func(a *MinecraftAnswers) { a.Difficulty = "hardcore" },
		"spectator mode":           func(a *MinecraftAnswers) { a.Mode = "spectator" },
		"0 players":                func(a *MinecraftAnswers) { a.MaxPlayers = 0 },
		"view distance 2":          func(a *MinecraftAnswers) { a.ViewDistance = 2 },
		"seed with a space":        func(a *MinecraftAnswers) { a.Seed = "my seed" },
		"operator with a dash":     func(a *MinecraftAnswers) { a.Operator = "dries-h" },
		"short name in whitelist":  func(a *MinecraftAnswers) { a.Whitelist = []string{"ab"} },
		"name twice":               func(a *MinecraftAnswers) { a.Whitelist = []string{"Friend1", "friend1"} },
		"playit key with a space":  func(a *MinecraftAnswers) { a.PlayitSecretKey = "0123456789 abcdef" },
		"address with a scheme":    func(a *MinecraftAnswers) { a.PublicAddress = "https://x.joinmc.link" },
		"storage with a new line":  func(a *MinecraftAnswers) { a.Storage = "local\nx" },
		"world disk of 1 GB":       func(a *MinecraftAnswers) { a.WorldSize = 1 },
		"101 players on whitelist": func(a *MinecraftAnswers) { a.Whitelist = slices.Repeat([]string{"x"}, 101) },
	}
	for name, change := range bad {
		answers := valid()
		change(&answers)
		if err := answers.Validate(); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestMinecraftInstall(t *testing.T) {
	installer, unit := newMinecraftInstaller(t)
	answers := validMinecraft()
	answers.MOTD = "Dries world"

	if err := installer.Install(MinecraftApp, InstallRequest{Minecraft: answers}); err != nil {
		t.Fatal(err)
	}
	file, _ := os.ReadFile(unit.started[1])
	for _, line := range []string{"MEMORY_GB=4\n", "MOTD=Dries world\n", "OPERATOR=Dries_H\n", "WHITELIST=Friend1,other_friend\n", "PLAYIT_SECRET_KEY=0123456789abcdef0123\n", "WORLD_SIZE=20\n"} {
		if !strings.Contains(string(file), line) {
			t.Errorf("answers miss %q:\n%s", line, file)
		}
	}

	os.WriteFile(installer.logPath(), []byte("HOMELAB app minecraft 150 192.168.1.60\n"), 0o600)
	os.WriteFile(installer.exitPath(), []byte("1\n"), 0o600)
	unit.running = false
	installer.Status()

	saved, err := installer.Saved(MinecraftApp)
	if err != nil || saved == nil || !saved.HasPlayitSecretKey || saved.Minecraft.PlayitSecretKey != "" || saved.Minecraft.Operator != "Dries_H" {
		t.Fatalf("saved = %+v, err = %v", saved, err)
	}

	// A retry with an empty key keeps the saved key.
	answers.PlayitSecretKey = ""
	if err := installer.Install(MinecraftApp, InstallRequest{Minecraft: answers, KeepSecrets: true}); err != nil {
		t.Fatal(err)
	}
	if file, _ := os.ReadFile(unit.started[3]); !strings.Contains(string(file), "PLAYIT_SECRET_KEY=0123456789abcdef0123\n") {
		t.Errorf("the saved key was not used:\n%s", file)
	}
}

func TestChangePlayers(t *testing.T) {
	installer, unit := newMinecraftInstaller(t)

	if err := installer.ChangePlayers(150, MinecraftPlayers{Whitelist: []string{"Friend1"}, Operators: []string{"Dries_H"}}); !errors.Is(err, ErrInvalidAnswers) {
		t.Fatalf("operator not on the whitelist: %v", err)
	}
	if err := installer.ChangePlayers(1, MinecraftPlayers{}); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("vmid 1: %v", err)
	}

	players := MinecraftPlayers{Whitelist: []string{"Dries_H", "Friend1"}, Operators: []string{"Dries_H"}}
	if err := installer.ChangePlayers(150, players); err != nil {
		t.Fatal(err)
	}
	answersPath := unit.started[1]
	if !slices.Equal(unit.args, []string{"--players", "--ctid", "150", "--answers", answersPath}) {
		t.Fatalf("args = %v", unit.args)
	}
	if file, _ := os.ReadFile(answersPath); string(file) != "WHITELIST=Dries_H,Friend1\nOPS=Dries_H\n" {
		t.Fatalf("answers = %q", file)
	}
	if status := installer.Status(); status.Action != ActionPlayers || status.App != MinecraftApp {
		t.Fatalf("status = %+v", status)
	}
}

func TestPlayers(t *testing.T) {
	installer, _ := newMinecraftInstaller(t)
	var commands [][]string
	files := map[string]string{
		"/opt/minecraft/data/whitelist.json": `[{"uuid":"1","name":"Dries_H"},{"uuid":"2","name":"Friend1"}]`,
		"/opt/minecraft/data/ops.json":       `[{"uuid":"1","name":"Dries_H","level":4,"bypassesPlayerLimit":false}]`,
	}
	installer.output = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		return []byte(files[args[len(args)-1]]), nil
	}

	players, err := installer.Players(context.Background(), 150)
	if err != nil || !slices.Equal(players.Whitelist, []string{"Dries_H", "Friend1"}) || !slices.Equal(players.Operators, []string{"Dries_H"}) {
		t.Fatalf("players = %+v, err = %v", players, err)
	}
	if len(commands) != 2 || !slices.Equal(commands[0][:5], []string{"pct", "exec", "150", "--", "cat"}) {
		t.Fatalf("commands = %v", commands)
	}

	files["/opt/minecraft/data/ops.json"] = "[]"
	if players, err := installer.Players(context.Background(), 150); err != nil || players.Operators == nil || len(players.Operators) != 0 {
		t.Fatalf("no operators: %+v, %v", players, err)
	}

	files["/opt/minecraft/data/ops.json"] = "not json"
	if _, err := installer.Players(context.Background(), 150); err == nil {
		t.Fatal("a broken file should be an error")
	}
	if _, err := installer.Players(context.Background(), 1); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("vmid 1: %v", err)
	}
}
