package companion

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

const mika = "com.wangzan101.mika/.a11y.MikaAccessibilityService"

func enableDisable(t *testing.T, p *fakePhone) *Client {
	t.Helper()
	c := New(p)
	t.Cleanup(func() { c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Enable(ctx); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !p.on() {
		t.Fatal("service not listed after Enable")
	}
	return c
}

func TestEnableDisableUnset(t *testing.T) {
	p := newFakePhone(t)
	c := enableDisable(t, p)
	if got := p.settings[settingKey]; got != Release.Component() {
		t.Errorf("during the task: %q, want %q", got, Release.Component())
	}
	if err := c.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v, ok := p.settings[settingKey]; ok {
		t.Errorf("setting should be deleted again, is %q", v)
	}
	if !slices.Contains(p.shell, "settings delete secure "+settingKey) {
		t.Errorf("shell: %q", p.shell)
	}
}

func TestEnableKeepsOtherServices(t *testing.T) {
	p := newFakePhone(t)
	p.settings[settingKey] = mika
	c := enableDisable(t, p)
	if got, want := p.settings[settingKey], mika+":"+Release.Component(); got != want {
		t.Errorf("during the task: %q, want %q", got, want)
	}
	if err := c.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := p.settings[settingKey]; got != mika {
		t.Errorf("after: %q, want %q", got, mika)
	}
}

func TestEnableLeavesUserEnabledService(t *testing.T) {
	p := newFakePhone(t)
	p.settings[settingKey] = Release.Package + "/" + ServiceClass // full form, written by hand
	c := enableDisable(t, p)
	puts := func() int {
		n := 0
		for _, cmd := range p.shell {
			if !strings.HasPrefix(cmd, "settings get") {
				n++
			}
		}
		return n
	}
	if puts() != 0 {
		t.Errorf("Enable wrote the setting although the service was on: %q", p.shell)
	}
	if err := c.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if puts() != 0 || !p.on() {
		t.Errorf("Disable turned off a service it did not turn on: %q", p.shell)
	}
}

func TestDisableKeepsChangesMadeDuringTheTask(t *testing.T) {
	p := newFakePhone(t)
	p.settings[settingKey] = mika
	c := enableDisable(t, p)
	// The person turns on TalkBack while the task runs.
	talkback := "com.google.android.marvin.talkback/.TalkBackService"
	p.settings[settingKey] += ":" + talkback
	if err := c.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := p.settings[settingKey], mika+":"+talkback; got != want {
		t.Errorf("after: %q, want %q", got, want)
	}
}

func TestDisableRestoresExactTextAfterRewrite(t *testing.T) {
	p := newFakePhone(t)
	other := "com.example/com.example.a11y.Service"
	p.settings[settingKey] = mika + ":" + other
	// Android rewrites the list in short form and its own order.
	p.rewrite = func(v string) string {
		parts := parseServices(v)
		for i, s := range parts {
			parts[i] = strings.Replace(s, "com.example/com.example.", "com.example/.", 1)
		}
		slices.Reverse(parts)
		return strings.Join(parts, ":")
	}
	c := enableDisable(t, p)
	p.rewrite = nil
	if err := c.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := p.settings[settingKey], mika+":"+other; got != want {
		t.Errorf("after: %q, want the original %q", got, want)
	}
}

func TestDisableAfterServiceTurnedItselfOff(t *testing.T) {
	p := newFakePhone(t)
	c := enableDisable(t, p)
	// disableSelf() persists the list without Pinky: an empty string.
	p.settings[settingKey] = ""
	if err := c.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v, ok := p.settings[settingKey]; ok {
		t.Errorf("setting should be back to unset, is %q", v)
	}
}

func TestForceDisable(t *testing.T) {
	p := newFakePhone(t)
	p.settings[settingKey] = mika + ":" + Release.Component()
	c := New(p)
	defer c.Close()
	if err := c.ForceDisable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := p.settings[settingKey]; got != mika {
		t.Errorf("after: %q", got)
	}
	p.settings[settingKey] = Release.Component()
	if err := c.ForceDisable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.settings[settingKey]; ok {
		t.Error("key should be deleted when nothing is left")
	}
}

func TestEnableTimesOutWhenServiceNeverStarts(t *testing.T) {
	p := newFakePhone(t)
	// The setting never takes: Android rewrites it without Pinky.
	c := New(p)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	p.mu.Lock()
	p.rewrite = func(v string) string { return strings.ReplaceAll(v, Release.Component(), "") }
	p.mu.Unlock()
	if err := c.Enable(ctx); err == nil {
		t.Fatal("Enable should fail when the service never answers")
	}
	// Enable restored the setting by itself.
	if _, ok := p.settings[settingKey]; ok {
		t.Errorf("setting left behind: %q", p.settings[settingKey])
	}
	if err := c.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestComponentNames(t *testing.T) {
	for _, s := range []string{Release.Component(), Release.Package + "/" + ServiceClass} {
		if !New(nil).isOurs(s) {
			t.Errorf("isOurs(%q) = false", s)
		}
	}
	for _, s := range []string{mika, "app.pinky.companion/.Other", "app.pinky.companionx/.PinkyAccessibilityService", ""} {
		if New(nil).isOurs(s) {
			t.Errorf("isOurs(%q) = true", s)
		}
	}
	if got := parseServices(" a/.B : :c/d.E "); !slices.Equal(got, []string{"a/.B", "c/d.E"}) {
		t.Errorf("parseServices = %q", got)
	}
	if !sameComponents([]string{"a/.B", "c/c.D"}, []string{"c/.D", "a/a.B"}) {
		t.Error("sameComponents should ignore order and short forms")
	}
	if q := shellQuote("it's"); q != `'it'\''s'` {
		t.Errorf("shellQuote = %s", q)
	}
}

func TestTurnOnKeepsOtherServicesAndStaysOn(t *testing.T) {
	p := newFakePhone(t)
	p.settings[settingKey] = mika
	c := New(p)
	t.Cleanup(func() { c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.TurnOn(ctx); err != nil {
		t.Fatal(err)
	}
	if got, want := p.settings[settingKey], mika+":"+Release.Component(); got != want {
		t.Errorf("after TurnOn: %q, want %q", got, want)
	}
	if err := c.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if got, want := p.settings[settingKey], mika+":"+Release.Component(); got != want {
		t.Errorf("Disable after TurnOn: %q, want it left on (%q)", got, want)
	}
	// Already on: TurnOn writes nothing.
	writes := len(p.shell)
	if err := c.TurnOn(ctx); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range p.shell[writes:] {
		if strings.HasPrefix(cmd, "settings put") {
			t.Errorf("TurnOn rewrote the setting: %q", cmd)
		}
	}
}

func TestKeepEnabledAfterEnable(t *testing.T) {
	p := newFakePhone(t)
	p.settings[settingKey] = mika
	task := enableDisable(t, p)
	if !task.EnabledForTask() {
		t.Fatal("EnabledForTask is false after Enable turned the service on")
	}
	// The person turns it on for good while the driver has it on.
	task.KeepEnabled()
	if task.EnabledForTask() {
		t.Error("EnabledForTask after KeepEnabled")
	}
	if err := task.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := p.settings[settingKey], mika+":"+Release.Component(); got != want {
		t.Errorf("after Disable: %q, want %q", got, want)
	}
}
