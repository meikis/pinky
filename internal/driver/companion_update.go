package driver

import (
	"context"
	"errors"
)

// CompanionUpdate says what an automatic update of the companion app did.
type CompanionUpdate struct {
	// From and To are the versions before and after, as people read them:
	// "0.1.0 (75)".
	From, To string
	// Updated is false when the phone already had the bundled build or a
	// newer one.
	Updated bool
	// Replaced: the installed app was signed with another key, so it was
	// removed and the bundled one installed in its place. OldPublicKey: that
	// key was the public one every build was signed with until 2026-11-08,
	// with which anyone could sign an update of it.
	Replaced, OldPublicKey bool
	// ServiceOn: the app's accessibility service is on afterwards. An
	// update leaves it off: it comes on while an agent works on the phone.
	ServiceOn bool
}

// CompanionUpdater updates an installed companion app without asking the
// person, for automatic updates while the phone is free. Unlike
// CompanionInstaller it never installs an app that is not there and never
// turns the service on; an app signed with another key is replaced. It
// refuses with ErrNeedsPerson when the phone would ask its owner (Google
// Play Protect checks installs over USB).
type CompanionUpdater interface {
	UpdateCompanion(ctx context.Context) (CompanionUpdate, error)
}

// ErrNeedsPerson means the update would ask the person on the phone, so it
// waits for them to click Update.
var ErrNeedsPerson = errors.New("the phone would ask its owner to allow the update")
