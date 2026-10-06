package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	MinecraftApp  = "minecraft"
	ActionPlayers = AppAction("players")
	// maxWhitelist is the most names on the whitelist.
	maxWhitelist = 100
)

var (
	// Java profile names: 3 to 16 letters, digits and underscores.
	minecraftNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)
	motdPattern          = regexp.MustCompile(`^[\p{L}\p{N} .,!?:;()&+@_-]{1,59}$`)
	seedPattern          = regexp.MustCompile(`^-?[A-Za-z0-9_]{1,64}$`)
	playitKeyPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{16,256}$`)
	publicAddressPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,252})(:[0-9]{1,5})?$`)
	minecraftMemories    = []int{2, 4, 6, 8}
	difficulties         = []string{"peaceful", "easy", "normal", "hard"}
	gameModes            = []string{"survival", "creative", "adventure"}
)

// MinecraftAnswers are the questions of stacks/minecraft/install.sh.
type MinecraftAnswers struct {
	// AcceptEULA is true when the user agrees to the Minecraft EULA. The server
	// does not start without it.
	AcceptEULA bool `json:"acceptEula"`
	// Memory is the memory of the server in GB.
	Memory       int    `json:"memory"`
	MOTD         string `json:"motd"`
	Difficulty   string `json:"difficulty"`
	Mode         string `json:"mode"`
	MaxPlayers   int    `json:"maxPlayers"`
	ViewDistance int    `json:"viewDistance"`
	// Seed is optional. Empty gives a random world.
	Seed string `json:"seed"`
	// Operator is the Java profile name of the owner. They are on the
	// whitelist too.
	Operator  string   `json:"operator"`
	Whitelist []string `json:"whitelist"`
	// PlayitSecretKey is optional. With it, the playit.gg agent runs next to
	// the server, so friends can join without open ports.
	PlayitSecretKey string `json:"playitSecretKey"`
	// PublicAddress is the address that playit.gg gives the tunnel. It is
	// only shown on the Apps page.
	PublicAddress string `json:"publicAddress"`
	Storage       string `json:"storage"`
	// WorldSize is the size of the world disk in GB.
	WorldSize int `json:"worldSize"`
}

func (a MinecraftAnswers) Validate() error {
	invalid := func(message string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswers, message) }

	if message := playersError(a.Whitelist); message != "" {
		return invalid(message)
	}

	switch {
	case !a.AcceptEULA:
		return invalid("agree to the Minecraft EULA first")
	case !slices.Contains(minecraftMemories, a.Memory):
		return invalid("the memory must be 2, 4, 6 or 8 GB")
	case !motdPattern.MatchString(a.MOTD):
		return invalid("the message of the day can have letters, digits, spaces and . , ! ? : ; ( ) & + @ _ -, at most 59")
	case !slices.Contains(difficulties, a.Difficulty):
		return invalid("the difficulty must be peaceful, easy, normal or hard")
	case !slices.Contains(gameModes, a.Mode):
		return invalid("the game mode must be survival, creative or adventure")
	case a.MaxPlayers < 1 || a.MaxPlayers > 100:
		return invalid("the most players must be 1 to 100")
	case a.ViewDistance < 3 || a.ViewDistance > 32:
		return invalid("the view distance must be 3 to 32 chunks")
	case a.Seed != "" && !seedPattern.MatchString(a.Seed):
		return invalid("the seed can have letters, digits, underscores and a minus sign at the start")
	case !minecraftNamePattern.MatchString(a.Operator):
		return invalid("your Minecraft name must be 3 to 16 letters, digits or underscores")
	case a.PlayitSecretKey != "" && !playitKeyPattern.MatchString(a.PlayitSecretKey):
		return invalid("the playit.gg secret key can have letters, digits, dashes and underscores")
	case a.PublicAddress != "" && !publicAddressPattern.MatchString(a.PublicAddress):
		return invalid("the public address must be a hostname, like name.joinmc.link")
	case !storageID.MatchString(a.Storage):
		return invalid("invalid storage")
	case a.WorldSize < 5 || a.WorldSize > 2000:
		return invalid("the world disk must be 5 to 2000 GB")
	}

	return nil
}

// playersError checks a list of Java profile names.
func playersError(names []string) string {
	if len(names) > maxWhitelist {
		return fmt.Sprintf("the whitelist can have at most %d players", maxWhitelist)
	}

	seen := map[string]bool{}
	for _, name := range names {
		if !minecraftNamePattern.MatchString(name) {
			return fmt.Sprintf("%q is not a Minecraft name: use 3 to 16 letters, digits or underscores", name)
		}
		if seen[strings.ToLower(name)] {
			return fmt.Sprintf("%s is on the list twice", name)
		}
		seen[strings.ToLower(name)] = true
	}

	return ""
}

func (a MinecraftAnswers) file() string {
	lines := []string{
		"MEMORY_GB=" + strconv.Itoa(a.Memory),
		"MOTD=" + a.MOTD,
		"DIFFICULTY=" + a.Difficulty,
		"MODE=" + a.Mode,
		"MAX_PLAYERS=" + strconv.Itoa(a.MaxPlayers),
		"VIEW_DISTANCE=" + strconv.Itoa(a.ViewDistance),
		"SEED=" + a.Seed,
		"OPERATOR=" + a.Operator,
		"WHITELIST=" + strings.Join(a.Whitelist, ","),
		"PLAYIT_SECRET_KEY=" + a.PlayitSecretKey,
		"PUBLIC_ADDRESS=" + a.PublicAddress,
		"STORAGE=" + a.Storage,
		"WORLD_SIZE=" + strconv.Itoa(a.WorldSize),
	}

	return strings.Join(lines, "\n") + "\n"
}

// MinecraftPlayers are the players of the Minecraft server. Operators are
// players on the whitelist who can also run commands.
type MinecraftPlayers struct {
	Whitelist []string `json:"whitelist"`
	Operators []string `json:"operators"`
}

func (p MinecraftPlayers) Validate() error {
	if message := playersError(p.Whitelist); message != "" {
		return fmt.Errorf("%w: %s", ErrInvalidAnswers, message)
	}
	for _, operator := range p.Operators {
		if !slices.Contains(p.Whitelist, operator) {
			return fmt.Errorf("%w: operator %q must be on the whitelist", ErrInvalidAnswers, operator)
		}
	}

	return nil
}

func (p MinecraftPlayers) file() string {
	return "WHITELIST=" + strings.Join(p.Whitelist, ",") + "\nOPS=" + strings.Join(p.Operators, ",") + "\n"
}

// Players reads the whitelist and the operators of the Minecraft server in
// container vmid, from the files of the server itself.
func (i *AppInstaller) Players(ctx context.Context, vmid int) (MinecraftPlayers, error) {
	players := MinecraftPlayers{Whitelist: []string{}, Operators: []string{}}
	if vmid < 100 || vmid > 999999999 {
		return players, ErrInvalidContainer
	}

	for file, names := range map[string]*[]string{"whitelist.json": &players.Whitelist, "ops.json": &players.Operators} {
		output, err := i.output(ctx, "pct", "exec", strconv.Itoa(vmid), "--", "cat", "/opt/minecraft/data/"+file)
		if err != nil {
			return players, fmt.Errorf("could not read the players of container %d: %w", vmid, err)
		}
		var entries []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(output, &entries); err != nil {
			return players, fmt.Errorf("could not read %s of container %d: %w", file, vmid, err)
		}
		for _, entry := range entries {
			*names = append(*names, entry.Name)
		}
	}

	return players, nil
}

// ChangePlayers gives the Minecraft server in container vmid a new whitelist
// and operators. The installer applies them without a restart.
func (i *AppInstaller) ChangePlayers(vmid int, players MinecraftPlayers) error {
	if err := players.Validate(); err != nil {
		return err
	}
	if vmid < 100 || vmid > 999999999 {
		return ErrInvalidContainer
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	script, err := i.prepare(MinecraftApp)
	if err != nil {
		return err
	}
	answersPath := filepath.Join(i.Dir, "players.answers")
	if err := os.WriteFile(answersPath, []byte(players.file()), 0o600); err != nil {
		return err
	}

	return i.run(MinecraftApp, ActionPlayers, script, []string{"--players", "--ctid", strconv.Itoa(vmid), "--answers", answersPath}, answersPath)
}
