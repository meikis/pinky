package fakeapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DIYgod/pinky/internal/driver"
	"github.com/DIYgod/pinky/internal/gateway"
)

func TestFeatures(t *testing.T) {
	a := New(Options{Scenario: ThreePhones, Speed: 50})
	for _, d := range a.Devices() {
		f := d.Features
		switch d.Info.ID {
		case "pixel-9":
			if !f.Unlock || !f.Background || !f.Companion || !f.Files {
				t.Errorf("Pixel 9: %+v", f)
			}
		case "iphone-17":
			if !f.Unlock || f.Background || f.Companion {
				t.Errorf("iPhone 17: %+v", f)
			}
		case "galaxy-s25":
			if f != (gateway.Features{}) {
				t.Errorf("an offline phone has features: %+v", f)
			}
		}
	}
}

func TestPairingByQRCode(t *testing.T) {
	a := New(Options{Scenario: OnePhone, Speed: 50, PairDelay: -1})
	ctx := context.Background()
	pg, err := a.StartPairing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if qr := pg.QR(); !strings.HasPrefix(qr, "WIFI:T:ADB;S:pinky-") || !strings.HasSuffix(qr, ";;") {
		t.Errorf("QR text %q", qr)
	}
	done := make(chan driver.Info, 1)
	go func() {
		info, err := pg.Wait(ctx)
		if err != nil {
			t.Error(err)
		}
		done <- info
	}()
	select {
	case <-done:
		t.Fatal("paired before the phone scanned the code")
	case <-time.After(50 * time.Millisecond):
	}
	a.ScanPairing()
	info := <-done
	if info.Connection != "wifi" || info.State != driver.StateOnline {
		t.Errorf("paired %+v", info)
	}
	if devs := a.Devices(); len(devs) != 2 || devs[1].Info.ID != info.ID {
		t.Errorf("devices after pairing: %+v", devs)
	}
	// Closing a pairing ends its Wait.
	pg2, _ := a.StartPairing(ctx)
	pg2.Close()
	if _, err := pg2.Wait(ctx); err == nil {
		t.Error("Wait after Close paired")
	}
}

func TestPairWithCode(t *testing.T) {
	a := New(Options{Scenario: OnePhone, Speed: 50})
	ctx := context.Background()
	if _, err := a.PairWithCode(ctx, "192.168.1.20:37123", "000000"); err == nil || !strings.Contains(err.Error(), "Wrong password") {
		t.Errorf("wrong code: %v", err)
	}
	if _, err := a.PairWithCode(ctx, "192.168.1.20:1", "123456"); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("refused: %v", err)
	}
	info, err := a.PairWithCode(ctx, "192.168.1.20:37123", "123456")
	if err != nil || info.Name != "Pixel 8a" {
		t.Errorf("paired %+v, %v", info, err)
	}
}

func TestCompanionInstall(t *testing.T) {
	a := New(Options{Scenario: Companion, Speed: 50})
	ctx := context.Background()
	if cs, err := a.CompanionState(ctx, "pixel-9"); err != nil || cs != driver.CompanionMissing {
		t.Fatalf("state %q, %v", cs, err)
	}
	if err := a.InstallCompanion(ctx, "pixel-9"); err != nil {
		t.Fatal(err)
	}
	if cs, _ := a.CompanionState(ctx, "pixel-9"); cs != driver.CompanionReady {
		t.Errorf("after the install: %q", cs)
	}
	b := New(Options{Scenario: ThreePhones, Speed: 50})
	if _, err := b.CompanionState(ctx, "iphone-17"); !errors.Is(err, driver.ErrUnsupported) {
		t.Errorf("an iPhone's companion: %v", err)
	}
}

func TestBackgroundAndAgentStream(t *testing.T) {
	a := New(Options{Scenario: Companion, Speed: 50}) // no task runs
	ctx := context.Background()
	if err := a.SetBackground("pixel-9", true); err != nil || !a.Background("pixel-9") {
		t.Fatalf("SetBackground: %v", err)
	}
	if _, err := a.AgentStream(ctx, "pixel-9", driver.StreamOptions{}); !errors.Is(err, driver.ErrNotFound) {
		t.Errorf("an agent display with no task: %v", err)
	}
	b := New(Options{Scenario: ThreePhones, Speed: 50})
	if err := b.SetBackground("iphone-17", true); !errors.Is(err, driver.ErrUnsupported) {
		t.Errorf("background on an iPhone: %v", err)
	}
}

func TestRecordings(t *testing.T) {
	a := New(Options{Scenario: OnePhone, Speed: 50})
	ctx := context.Background()
	rec, err := a.Recording(ctx, "h-lunch")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Length != 108*time.Second || len(rec.Marks) != 7 || rec.Marks[4].Text != "You allowed: pay $12.80" {
		t.Errorf("recording %+v", rec)
	}
	for i := 1; i < len(rec.Marks); i++ {
		if rec.Marks[i].At < rec.Marks[i-1].At || rec.Marks[i].At > rec.Length {
			t.Errorf("mark %d at %v", i, rec.Marks[i].At)
		}
	}
	if _, err := a.Recording(ctx, "h-gate"); !errors.Is(err, driver.ErrNotFound) {
		t.Errorf("the boarding pass task has a recording: %v", err)
	}
	// A task that ends while recording is off has none.
	b := New(Options{Scenario: OnePhone, Speed: 50})
	b.SetRecording(false)
	b.StopAll()
	var id string
	for _, t := range b.Tasks() {
		id = t.ID
	}
	if _, err := b.Recording(ctx, id); !errors.Is(err, driver.ErrNotFound) {
		t.Errorf("a task finished without recording has one: %v", err)
	}
}

func TestRetention(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	a := New(Options{Scenario: NoPhones, Speed: 50, Now: func() time.Time { return time.Unix(0, clock.Load()) }})
	before, _ := a.History(context.Background(), gateway.HistoryQuery{})
	a.SetRetention(0)
	if all, _ := a.History(context.Background(), gateway.HistoryQuery{}); len(all) != len(before) {
		t.Errorf("0 days removed tasks: %d of %d", len(all), len(before))
	}
	clock.Add(int64(4 * 24 * time.Hour))
	a.SetRetention(2) // four days on, yesterday's and today's tasks are older than two days
	if left, _ := a.History(context.Background(), gateway.HistoryQuery{}); len(left) != 0 {
		t.Errorf("%d tasks older than the limit are left", len(left))
	}
	if days, _ := a.Retention(); days != 2 {
		t.Errorf("Retention %d", days)
	}
}

func TestExportHistory(t *testing.T) {
	a := New(Options{Scenario: OnePhone, Speed: 50})
	dir := t.TempDir()
	path, err := a.ExportHistory(context.Background(), []string{"h-sam"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "Reply to Sam about Saturday") || strings.Contains(string(data), "lunch") {
		t.Errorf("report %s: %v\n%s", path, err, data)
	}
	if _, err := os.Stat(filepath.Join(dir, "pinky-history.json")); err != nil {
		t.Error(err)
	}
}
