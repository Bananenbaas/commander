package procs

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		name    string
		running []string
		wanted  string
		want    bool
	}{
		{"case", []string{"starcitizen.exe"}, "StarCitizen.exe", true},
		{"exe on wanted only", []string{"game"}, "Game.exe", true},
		{"exe on running only", []string{"Game.exe"}, "game", true},
		{"exe upper case", []string{"GAME.EXE"}, "game.exe", true},
		{"spaces with exe", []string{"Turtle Beach Device Service.exe"}, "Turtle Beach Device Service", true},
		{"spaces without exe", []string{"turtle beach device service"}, "Turtle Beach Device Service.exe", true},
		{"backslash path", []string{`C:\Games\Star Citizen\StarCitizen.exe`}, "StarCitizen.exe", true},
		{"slash path", []string{"/opt/games/StarCitizen.exe"}, "starcitizen", true},
		{"mixed path", []string{`Z:/home/x\game.exe`}, "game.exe", true},
		{"comm truncated to 15", []string{"turtle beach sw"}, "Turtle Beach Swarm II.exe", true},
		{"comm exactly 15 with exe", []string{"StarCitizen.exe"}, "StarCitizen.exe", true},
		{"comm truncated, wrong prefix", []string{"turtle beach sx"}, "Turtle Beach Swarm II.exe", false},
		{"short comm is not a prefix match", []string{"turtle"}, "Turtle Beach Swarm II.exe", false},
		{"prefix of 14 chars no match", []string{"turtle beach s"}, "Turtle Beach Swarm II.exe", false},
		{"absent", []string{"bash", "init"}, "StarCitizen.exe", false},
		{"empty running", nil, "StarCitizen.exe", false},
		{"substring is no match", []string{"mygame.exe"}, "game.exe", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := match(c.running, []string{c.wanted})
			v, ok := got[c.wanted]
			if !ok || v != c.want {
				t.Fatalf("match(%q, %q) = %v, want %v", c.running, c.wanted, got, c.want)
			}
		})
	}
}

func TestMatchKeysAndMultiple(t *testing.T) {
	wanted := []string{"A.exe", "b", "Missing"}
	got := match([]string{"a", "B.EXE"}, wanted)
	if len(got) != 3 || !got["A.exe"] || !got["b"] || got["Missing"] {
		t.Fatalf("got %v", got)
	}
	if _, ok := got["Missing"]; !ok {
		t.Fatal("absent name must still have an entry")
	}
	if len(match(nil, nil)) != 0 {
		t.Fatal("no wanted names must give an empty map")
	}
}

func TestSwarmNames(t *testing.T) {
	if len(Swarm) != 2 || Swarm[0] != "Turtle Beach Swarm II.exe" || Swarm[1] != "Turtle Beach Device Service.exe" {
		t.Fatalf("Swarm = %q", Swarm)
	}
}
