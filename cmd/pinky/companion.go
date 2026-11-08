package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/driver/android"
)

// runCompanion is `pinky companion install [phone]`: it installs or updates
// the Android companion app, which reads the screen much faster than
// uiautomator. Pinky never installs it on its own; with Update
// automatically on, the app updates an installed one while the phone is
// free (docs/notes/updates.md).
func runCompanion(args []string) error {
	if len(args) == 0 || args[0] != "install" {
		return errors.New("usage: pinky companion install [phone id or name]")
	}
	fs := flag.NewFlagSet("pinky companion install", flag.ContinueOnError)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	apk, err := android.FindCompanionAPK()
	if err != nil {
		return fmt.Errorf("%w (build it with `make companion`, or set PINKY_COMPANION_APK)", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	p := android.NewProvider(android.Config{})
	infos, err := p.List(ctx)
	if err != nil {
		return err
	}
	info, err := pickAndroid(infos, fs.Arg(0))
	if err != nil {
		return err
	}
	dev, err := p.OpenDevice(ctx, info.ID)
	if err != nil {
		return err
	}
	defer dev.Close()
	fmt.Printf("Installing %s on %s. If the phone asks whether to scan or allow the app (Google Play Protect), answer on the phone.\n", apk, info.Name)
	res, err := dev.SetUpCompanion(ctx, false)
	if err != nil {
		return err
	}
	switch {
	case !res.Installed:
		fmt.Printf("%s already has this companion build, and its service comes on.\n", info.Name)
	case res.Replaced:
		fmt.Printf("Replaced the companion on %s, which was signed with another key, and checked that its service comes on.\n", info.Name)
	default:
		fmt.Printf("Installed the companion on %s and checked that its service comes on.\n", info.Name)
	}
	fmt.Println("Pinky turns the service on only while an agent works on the phone, and off again afterwards.")
	return nil
}

// pickAndroid finds the phone a command names, or the only one online.
func pickAndroid(infos []driver.Info, want string) (driver.Info, error) {
	var online, names []string
	var match []driver.Info
	for _, in := range infos {
		if in.State != driver.StateOnline {
			continue
		}
		online = append(online, in.ID)
		names = append(names, fmt.Sprintf("%s (%s)", in.Name, in.ID))
		if want == "" || in.ID == want || strings.EqualFold(in.Name, want) {
			match = append(match, in)
		}
	}
	switch {
	case len(online) == 0:
		return driver.Info{}, errors.New("no Android phone is online; plug one in with USB debugging on")
	case len(match) == 1:
		return match[0], nil
	case len(match) == 0:
		return driver.Info{}, fmt.Errorf("no online Android phone is %q; online: %s", want, strings.Join(names, ", "))
	}
	return driver.Info{}, fmt.Errorf("several Android phones are online; name one: %s", strings.Join(names, ", "))
}
