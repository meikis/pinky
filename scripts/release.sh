#!/bin/sh
# Pinky's releases; docs/RELEASING.md explains them.
#
#   scripts/release.sh next          the version the next release gets
#   scripts/release.sh tag [-push] [-n] [-trailer "Co-Authored-By: …"]
#                                    release what CHANGELOG.md's Unreleased
#                                    notes describe: give them the next
#                                    version, commit, tag v<version>, and with
#                                    -push push main and the tag together,
#                                    which starts the release workflow
#   scripts/release.sh build [<ref>] build a release on this Mac into
#                                    dist/<version>/ and publish nothing: a
#                                    dry run (NOTARIZE=1 notarizes too)
#   scripts/release.sh publish <tag> build and publish a release from this
#                                    Mac, for when the workflow cannot
#   scripts/release.sh trigger <tag> run the release workflow for a tag again
#   scripts/release.sh setup         copy the tap's workflow and README into
#                                    the Homebrew tap (CREATE=1 creates it, as
#                                    a public repository), and list the secrets
#                                    the release workflow lacks
#
# RELEASES_REPO and TAP_REPO name the repositories (DIYgod/pinky,
# DIYgod/homebrew-tap).
set -eu

cd "$(dirname "$0")/.."
root=$(pwd)
repo=${RELEASES_REPO:-DIYgod/pinky}
tap=${TAP_REPO:-DIYgod/homebrew-tap}
export RELEASES_REPO=$repo TAP_REPO=$tap

say() { printf '==> %s\n' "$*" >&2; }
die() {
	printf 'release: %s\n' "$*" >&2
	exit 1
}
# The release tool, built once (build/ is ignored by git).
go build -o build/bin/release ./scripts/release
release() { "$root/build/bin/release" "$@"; }

# build_release builds the release of a commit (a tag, or any ref for a
# dry run) into dist/<version>/, in a checkout of its own so that nothing
# uncommitted gets in, and publishes it when asked to.
build_release() {
	ref=$1
	publishing=$2
	sha=$(git rev-parse --verify --quiet "$ref^{commit}") || die "no commit $ref"
	version=
	for t in $(git tag --points-at "$sha" --list 'v*'); do
		if release newer "${t#v}" >/dev/null 2>&1; then
			version=${t#v}
		fi
	done
	dryrun=
	if [ -z "$version" ]; then
		[ "$publishing" = 0 ] || die "$ref is not a release tag"
		version=$(release next)
		dryrun=1
		say "$ref has no release tag: building it as $version, the next version, for a dry run"
	fi
	[ "$(uname -s)" = Darwin ] || die "releases are built on a Mac (Pinky.app is signed and notarized there)"

	out=$root/dist/$version
	work=$(mktemp -d "${TMPDIR:-/tmp}/pinky-release.XXXXXX")
	# The worktree goes when this ends, however it ends.
	trap 'git -C "$root" worktree remove --force "$work/src" 2>/dev/null || true; rm -rf "$work"' EXIT
	trap 'exit 130' INT TERM
	git worktree add --quiet --detach "$work/src" "$sha"
	rm -rf "$out"
	mkdir -p "$out"
	(cd "$work/src" && release stamp "$version")

	# FFmpeg's and adb's builds outlive the worktree.
	export FFMPEG_CACHE="${FFMPEG_CACHE:-$HOME/Library/Caches/pinky-release/ffmpeg}"
	export ADB_CACHE="${ADB_CACHE:-$HOME/Library/Caches/pinky-release/adb}"
	export COMPANION_APK="$work/companion/pinky-companion.apk"
	"$work/src/scripts/dist.sh" companion "$work/companion"
	"$work/src/scripts/dist.sh" darwin "$out"
	"$work/src/scripts/dist.sh" windows "$out"
	"$work/src/scripts/dist.sh" linux "$out"
	(cd "$work/src" && go run ./scripts/release mcpb -version "$version" -out "$out/pinky-$version.mcpb")
	# A dry run's manifest leaves out what the build left out.
	partial=
	[ "${STRICT:-0}" = 1 ] || partial=-partial
	if [ -n "$dryrun" ]; then
		(cd "$work/src" && release manifest -dir "$out" -version "$version" -repo "$repo" $partial -unreleased) ||
			(cd "$work/src" && release manifest -dir "$out" -version "$version" -repo "$repo" $partial -notes "A dry run of $ref, with no notes.")
	else
		(cd "$work/src" && release manifest -dir "$out" -version "$version" -repo "$repo" $partial)
	fi
	if [ "$publishing" = 1 ]; then
		"$work/src/scripts/publish.sh" "$out"
	fi
	ls -l "$out" >&2
	say "the release's files are in $out"
}

cmd=${1:-}
[ $# -gt 0 ] && shift
case $cmd in
next | tag)
	release "$cmd" "$@"
	;;
build)
	export STRICT="${STRICT:-0}" NOTARIZE="${NOTARIZE:-0}"
	build_release "${1:-HEAD}" 0
	;;
publish)
	tag=${1:?usage: scripts/release.sh publish <tag>}
	git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null || die "origin has no tag $tag: push it first"
	# Everything a release has, unless STRICT=0 lets it go without the
	# Windows installer or the Intel FFmpeg's assembly.
	export STRICT="${STRICT:-1}" NOTARIZE=1
	build_release "$tag" 1
	;;
trigger)
	tag=${1:?usage: scripts/release.sh trigger <tag>}
	gh workflow run release.yml --repo "$repo" -f tag="$tag"
	say "started; see https://github.com/$repo/actions/workflows/release.yml"
	;;
setup)
	if ! gh repo view "$tap" --json name >/dev/null 2>&1; then
		[ "${CREATE:-0}" = 1 ] || die "$tap does not exist; CREATE=1 scripts/release.sh setup creates it, as a public repository"
		say "creating the public repository $tap"
		gh repo create "$tap" --public --disable-wiki \
			--description "Homebrew casks: brew install --cask $(printf '%s' "${tap%%/*}" | tr '[:upper:]' '[:lower:]')/tap/pinky"
	fi
	# The tap gets its files from scripts/release/homebrew-tap/.
	work=$(mktemp -d "${TMPDIR:-/tmp}/pinky-setup.XXXXXX")
	trap 'rm -rf "$work"' EXIT
	src=scripts/release/homebrew-tap
	gh repo clone "$tap" "$work/tap" -- --quiet
	(cd "$src" && find . -type f) | while read -r f; do
		mkdir -p "$work/tap/$(dirname "$f")"
		cp "$src/$f" "$work/tap/$f"
	done
	git -C "$work/tap" add -A
	if git -C "$work/tap" diff --cached --quiet; then
		say "$tap is up to date"
	else
		git -C "$work/tap" commit --quiet -m "chore: update from DIYgod/pinky's scripts/release/homebrew-tap"
		git -C "$work/tap" push --quiet origin HEAD:refs/heads/main
		say "updated $tap"
	fi
	# The secrets of the release workflow, by name; docs/RELEASING.md says
	# what each is.
	have=$(gh secret list --repo "$repo" --json name --jq '.[].name' 2>/dev/null || true)
	missing=
	for s in MACOS_CERTIFICATE MACOS_CERTIFICATE_PASSWORD COMPANION_KEYSTORE COMPANION_KEYSTORE_PASSWORD; do
		printf '%s\n' "$have" | grep -qx "$s" || missing="$missing $s"
	done
	if ! printf '%s\n' "$have" | grep -qx NOTARY_API_KEY && ! printf '%s\n' "$have" | grep -qx APPLE_ID; then
		missing="$missing NOTARY_API_KEY+NOTARY_API_KEY_ID+NOTARY_API_ISSUER(or APPLE_ID+APPLE_TEAM_ID+APPLE_APP_PASSWORD)"
	fi
	if [ -n "$missing" ]; then
		say "secrets $repo lacks (see docs/RELEASING.md):$missing"
	else
		say "$repo has every secret the release workflow needs"
	fi
	;;
*)
	sed -n '2,/^set -eu/p' "$0" | sed -e '$d' -e 's/^# \{0,1\}//' >&2
	exit 2
	;;
esac
