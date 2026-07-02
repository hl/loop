package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hl/brr/internal/config"
)

func readState(t *testing.T, name string) *State {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(StateDir, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return &state
}

func readEvents(t *testing.T, name string) []Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(StateDir, name+".events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("decoding event %q: %v", line, err)
		}
		events = append(events, e)
	}
	return events
}

func testWorkflow(stages []Stage, cycle *Cycle) Workflow {
	return Workflow{
		Version:  SchemaVersion,
		Defaults: Defaults{Max: 1},
		Cycle:    cycle,
		Stages:   stages,
	}
}

func testConfig(cmd []string) config.Config {
	return config.Config{
		Default:  "test",
		Profiles: map[string]config.Profile{"test": {Command: cmd[0], Args: cmd[1:]}},
	}
}

func echoCmd() []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "echo ok"}
	}
	return []string{"true"}
}

func failCmd() []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "exit 1"}
	}
	return []string{"false"}
}

func sleepCmd() []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "ping -n 10 127.0.0.1 > nul"}
	}
	return []string{"sh", "-c", "sleep 10"}
}

func catToFileCmd(path string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "set /p x= & echo %x% >> " + path}
	}
	return []string{"sh", "-c", "cat >> " + path}
}

func appendTextCmd(path, text string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "echo " + strings.TrimSpace(text) + " >> " + path}
	}
	return []string{"sh", "-c", "printf " + shellQuote(text) + " >> " + shellQuote(path)}
}

func cycleOnceCmd(counter string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "findstr /c:\"x\" " + counter + " >nul && findstr /c:\"x\" " + counter + " | find /c /v \"\" > count.tmp && set /p n=<count.tmp && if %n% LSS 2 echo again > .brr-cycle"}
	}
	return []string{"sh", "-c", `lines=$(wc -l < ` + shellQuote(counter) + ` | tr -d ' '); if [ "$lines" -lt 2 ]; then touch .brr-cycle; fi`}
}

func alwaysCycleAndCountCmd(counter string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "echo x >> " + counter + " & echo again > .brr-cycle"}
	}
	return []string{"sh", "-c", "printf 'x\\n' >> " + shellQuote(counter) + " && touch .brr-cycle"}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func testTime() time.Time {
	return time.Unix(1700000000, 0).UTC()
}
