package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State is what the updater remembers between runs, in update.json next to
// the settings.
type State struct {
	// LastRun is the version that ran last; a newer one shows What's new.
	LastRun string `json:"last_run,omitempty"`
	// NotesFrom: What's new shows the changelog after this version, up to
	// the running one, until the person dismisses it.
	NotesFrom string `json:"notes_from,omitempty"`
	// Checked is when the feed last answered, and Latest the version it
	// offered.
	Checked time.Time `json:"checked,omitempty"`
	Latest  string    `json:"latest,omitempty"`
	// Later is a version whose card the person put off until LaterUntil.
	Later      string    `json:"later,omitempty"`
	LaterUntil time.Time `json:"later_until,omitempty"`
	// Failed is a version that could not be installed or did not start;
	// Pinky does not install it again by itself, only when asked.
	Failed    string `json:"failed,omitempty"`
	FailedWhy string `json:"failed_why,omitempty"`
	// Changes are other updates to tell the person about, such as the
	// companion app on a phone, until they are seen.
	Changes []Change `json:"changes,omitempty"`
}

// Change is an update the person did not start, to mention in What's new.
type Change struct {
	Kind   string    `json:"kind"` // "companion", or "companion-replaced" when one signed with another key was replaced
	Device string    `json:"device,omitempty"`
	From   string    `json:"from,omitempty"`
	To     string    `json:"to"`
	At     time.Time `json:"at"`
}

// LoadState reads update.json; a missing or broken file is a fresh state.
func LoadState(path string) State {
	var s State
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &s) != nil {
			s = State{}
		}
	}
	return s
}

// Save writes update.json in one step.
func (s State) Save(path string) error {
	if len(s.Changes) > 20 {
		s.Changes = s.Changes[len(s.Changes)-20:]
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Launched records a start of version: when it is newer than the version
// that ran last, What's new covers the versions in between. A first run
// shows nothing, and development builds leave the state alone.
func (s *State) Launched(version string) {
	if !IsRelease(version) {
		return
	}
	if last := s.LastRun; IsRelease(last) && Compare(version, last) > 0 {
		if !IsRelease(s.NotesFrom) || Compare(last, s.NotesFrom) < 0 {
			s.NotesFrom = last
		}
	}
	if IsRelease(s.NotesFrom) && Compare(s.NotesFrom, version) >= 0 {
		s.NotesFrom = "" // went back to an older version
	}
	s.LastRun = version
	if s.Failed != "" && Compare(s.Failed, version) <= 0 {
		s.Failed, s.FailedWhy = "", "" // installed after all
	}
}

// Snoozed reports whether the person put off this version's card.
func (s State) Snoozed(version string, now time.Time) bool {
	return s.Later != "" && Compare(s.Later, version) == 0 && now.Before(s.LaterUntil)
}

// restartMarker is the file that tells `pinky mcp` the app is restarting
// for an update, so it waits and reconnects instead of ending the agent's
// session. It sits next to update.json.
const restartMarker = "update-restarting"

// restartFresh is how long a marker counts: a restart takes seconds.
const restartFresh = 5 * time.Minute

// MarkRestart notes that Pinky is about to quit for an update.
func (u *Updater) MarkRestart() {
	if u.StatePath == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(u.StatePath), restartMarker), []byte(time.Now().UTC().Format(time.RFC3339)), 0o600); err != nil {
		u.logger().Warn("update: mark the restart", "err", err)
	}
}

// Restarting reports whether Pinky quit for an update within the last
// minutes, by the marker in dir (the folder of update.json).
func Restarting(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, restartMarker))
	return err == nil && time.Since(fi.ModTime()) < restartFresh
}

// RestartDone removes the marker once a gateway runs again.
func RestartDone(dir string) { os.Remove(filepath.Join(dir, restartMarker)) }
