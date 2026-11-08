# The version of cmd/pinky/mygo.json: 0.0.0-dev in the source, and the
# release's version in the checkout a release builds, which writes it there
# (docs/RELEASING.md).
VERSION := $(shell sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' cmd/pinky/mygo.json)
UNAME := $(shell uname -s)
GOARCH := $(shell go env GOARCH)

# FFmpeg's libraries for Pinky.app (make ffmpeg).
FFMPEG := build/ffmpeg
# Pinky's own adb for Pinky.app (make adb), which make app builds when
# CMake is installed.
ADB := build/adb
# The companion APK that make app puts in the app (make companion).
COMPANION_APK ?= android/companion/app/build/outputs/apk/debug/app-debug.apk
# The identity that signs Pinky.app: macos.signingIdentity in
# cmd/pinky/mygo.json, else "-", ad hoc, for this Mac only. A Developer ID
# ("Developer ID Application: …") signs for other Macs.
MACOS_SIGN ?= $(or $(shell sed -n 's/.*"signingIdentity": *"\([^"]*\)".*/\1/p' cmd/pinky/mygo.json),-)
# A release: Pinky.app signed with this Developer ID and notarized with the
# notarytool profile NOTARY_PROFILE, which `xcrun notarytool
# store-credentials` keeps in the login keychain.
RELEASE_SIGN ?= Developer ID Application: NATURAL SELECTION LABS PTE. LTD. (492J8Q67PF)
NOTARY_PROFILE ?= pinky-notary
DMG := build/pinky/darwin-$(GOARCH)/pinky-$(VERSION)-$(GOARCH).dmg

.PHONY: build test vet check app e2e companion companion-release ffmpeg adb release notarize dist

build:
	CGO_ENABLED=0 go build ./...

test:
	go test ./...

vet:
	go vet ./...

check: build vet test

# A minimal LGPL build of FFmpeg 9 (shared libraries, H.264 and HEVC with
# VideoToolbox) in build/ffmpeg; see scripts/ffmpeg.sh.
ffmpeg:
	scripts/ffmpeg.sh $(FFMPEG)

$(FFMPEG)/BUILD.txt: scripts/ffmpeg.sh
	scripts/ffmpeg.sh $(FFMPEG)

# adb for this Mac, from AOSP's source (needs CMake: brew install cmake),
# in build/adb; see scripts/adb.sh.
adb:
	scripts/adb.sh $(ADB)

$(ADB)/BUILD.txt: scripts/adb.sh scripts/adb/CMakeLists.txt
	scripts/adb.sh $(ADB)

# Pinky.app and its .dmg in build/pinky/<os>-<arch>/. The companion APK
# (when built) goes in through MyGo's resources, so every platform's build
# and the .dmg carry it. On macOS the app also gets FFmpeg's libraries in
# Contents/Frameworks and, with CMake installed, its own adb in
# Contents/Helpers, building them the first time, and the .dmg is made
# again from the finished app.
ifeq ($(UNAME),Darwin)
app: $(FFMPEG)/BUILD.txt $(if $(shell command -v cmake),$(ADB)/BUILD.txt)
endif
app:
	@rm -f cmd/pinky/resources/pinky-companion.apk
	@if [ -f "$(COMPANION_APK)" ]; then \
		mkdir -p cmd/pinky/resources && \
		cp "$(COMPANION_APK)" cmd/pinky/resources/pinky-companion.apk && \
		echo "adding the companion APK ($(COMPANION_APK))"; \
	else \
		echo "no companion APK at $(COMPANION_APK) (make companion); the app will not offer to install it"; \
	fi
	MYGO_LDFLAGS="-X main.version=$(VERSION)" go tool mygo build -sign "$(MACOS_SIGN)" -skip-notarize cmd/pinky
	@rm -f cmd/pinky/resources/pinky-companion.apk
	@rmdir cmd/pinky/resources 2>/dev/null || true
ifeq ($(UNAME),Darwin)
	ADB="$(if $(wildcard $(ADB)/BUILD.txt),$(ADB))" SIGN="$(MACOS_SIGN)" scripts/macapp.sh build/pinky/darwin-$(GOARCH)/Pinky.app $(FFMPEG) build/pinky/darwin-$(GOARCH)/pinky-$(VERSION)-$(GOARCH).dmg
endif

# Pinky.app and its .dmg for this Mac's processor, signed with the
# Developer ID and notarized, to try on other Macs. Releases are made by
# scripts/release.sh and the release workflow instead (docs/RELEASING.md).
release:
	$(MAKE) app MACOS_SIGN="$(RELEASE_SIGN)"
	$(MAKE) notarize

# Submits the .dmg to Apple's notary service, waits for the answer and
# staples the ticket to the .dmg, so Gatekeeper accepts it offline too.
notarize:
	xcrun notarytool submit "$(DMG)" --keychain-profile "$(NOTARY_PROFILE)" --wait
	xcrun stapler staple "$(DMG)"
	spctl -a -vv -t open --context context:primary-signature "$(DMG)"

# A dry run of a release: what the release workflow builds, from HEAD, in
# dist/<version>/, published nowhere (NOTARIZE=1 notarizes it too).
dist:
	scripts/release.sh build

# A pinky binary with MyGo's automation socket, for end-to-end checks.
e2e:
	go build -tags e2e -o build/pinky-e2e ./cmd/pinky

# The Android companion APK (needs the Android SDK and a JDK, as Android
# Studio installs them).
ANDROID_HOME ?= $(HOME)/Library/Android/sdk
JAVA_HOME ?= /Applications/Android Studio.app/Contents/jbr/Contents/Home
export ANDROID_HOME JAVA_HOME

companion:
	cd android/companion && ./gradlew --quiet assembleDebug

# The companion's release build, signed with Pinky's release key
# (PINKY_COMPANION_KEYSTORE and PINKY_COMPANION_KEYSTORE_PASSWORD, which only
# the release workflow has); scripts/dist.sh companion runs it.
companion-release:
	cd android/companion && ./gradlew --quiet assembleRelease
