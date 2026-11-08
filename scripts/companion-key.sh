#!/bin/zsh
# Makes Pinky Companion's release signing key, keeps a copy in 1Password
# and sets the release workflow's secrets COMPANION_KEYSTORE and
# COMPANION_KEYSTORE_PASSWORD with it (docs/RELEASING.md › Setting it up):
#
#   scripts/companion-key.sh
#
# The key: one RSA 4096 key in a PKCS12 keystore under the alias
# pinky-companion, with a certificate for CN=Pinky Companion, O=Pinky,
# valid for 30 years, and a random password, which is the key's too. It
# never touches a file or a command line: it lives in this process's
# variables and goes through pipes (printf is zsh's builtin) to 1Password
# (op, the 1Password CLI, which asks the person to allow it) and GitHub
# (gh). The copy in 1Password is checked before the secrets are set. It
# prints only public facts: the certificate and the name Android shows for
# its signer.
#
# OP_ACCOUNT (rsshub) and OP_VAULT (Employee) say where 1Password keeps the
# copy, REPO (DIYgod/pinky) the repository. A new key replaces the one in
# the secrets: Companions signed with the old one cannot be updated by
# releases signed with the new one, so Pinky replaces each of them once.
set -eu -o pipefail
cd "${0:A:h}/.."
account=${OP_ACCOUNT:-rsshub}
vault=${OP_VAULT:-Employee}
repo=${REPO:-DIYgod/pinky}
title="Pinky release: Companion signing key (Android) for GitHub Actions"
die() {
	print -u2 "companion-key: $*"
	exit 1
}
for t in openssl keytool op gh go python3; do
	command -v $t >/dev/null || die "$t is missing"
done
go build -o build/bin/companionctl ./internal/driver/android/companion/cmd/companionctl

export pw=$(openssl rand -base64 32)
ks=$(openssl req -x509 -newkey rsa:4096 -noenc -keyout - -out - -days 10958 -subj "/O=Pinky/CN=Pinky Companion" 2>/dev/null |
	openssl pkcs12 -export -name pinky-companion -passout env:pw | base64 | tr -d '\n')
[ ${#ks} -gt 1000 ] || die "no keystore"

# The certificate, which is public.
cert=$(printf '%s' "$ks" | base64 --decode | openssl pkcs12 -nokeys -passin env:pw 2>/dev/null |
	openssl x509 -noout -subject -enddate -fingerprint -sha256)
fp=$(printf '%s\n' "$cert" | sed -n 's/^sha256 Fingerprint=//p')
android=$(printf '%s' "$ks" | base64 --decode | openssl pkcs12 -nokeys -passin env:pw 2>/dev/null |
	openssl x509 -outform DER | build/bin/companionctl signer -)
printf '%s\n' "$cert"
print "Android signer name (dumpsys package): $android"
# keytool lists a keystore from a pipe, then exits 1 because /dev/stdin is
# not a regular file: its listing is what counts.
listing=$(printf '%s' "$ks" | base64 --decode | keytool -list -storetype PKCS12 -keystore /dev/stdin -storepass:env pw 2>/dev/null || true)
[[ $listing == *pinky-companion*PrivateKeyEntry* ]] || die "keytool cannot read the keystore"
kssum=$(printf '%s' "$ks" | shasum -a 256 | cut -d' ' -f1)
pwsum=$(printf '%s' "$pw" | shasum -a 256 | cut -d' ' -f1)

# 1. The copy in 1Password, read back before the secrets change.
notes="Pinky Companion release signing key, made by scripts/companion-key.sh on $(date +%Y-%m-%d). keystore (base64) is a PKCS12 keystore, exactly what the GitHub secret COMPANION_KEYSTORE of $repo holds; password is its password (COMPANION_KEYSTORE_PASSWORD), which is the key's too. One RSA 4096 key under the alias pinky-companion, certificate CN=Pinky Companion, O=Pinky, valid 30 years. Every release of Pinky must sign the companion with it, or the Companions installed until then cannot update: see docs/RELEASING.md, The companion's signing key. To set the secrets again: op read op://$vault/<this item>/keystore | gh secret set COMPANION_KEYSTORE --repo $repo, and the same with password for COMPANION_KEYSTORE_PASSWORD."
id=$(printf '{"title":"%s","category":"PASSWORD","fields":[{"id":"password","type":"CONCEALED","purpose":"PASSWORD","label":"password","value":"%s"},{"id":"keystore","type":"CONCEALED","label":"keystore (base64)","value":"%s"},{"id":"alias","type":"STRING","label":"alias","value":"pinky-companion"},{"id":"certificate","type":"STRING","label":"certificate SHA-256","value":"%s"},{"id":"signer","type":"STRING","label":"Android signer (dumpsys package)","value":"%s"},{"id":"secrets","type":"STRING","label":"GitHub secrets","value":"COMPANION_KEYSTORE, COMPANION_KEYSTORE_PASSWORD in %s"},{"id":"notesPlain","type":"STRING","purpose":"NOTES","label":"notesPlain","value":"%s"}]}' \
	"$title" "$pw" "$ks" "$fp" "$android" "$repo" "$notes" |
	op item create --account "$account" --vault "$vault" --format json - | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
print "1Password: created \"$title\" ($id) in $account / $vault"
[ "$(op read --account "$account" "op://$vault/$id/keystore" | tr -d '\n' | shasum -a 256 | cut -d' ' -f1)" = "$kssum" ] ||
	die "the keystore read back from 1Password differs; the secrets were not set"
[ "$(op read --account "$account" "op://$vault/$id/password" | tr -d '\n' | shasum -a 256 | cut -d' ' -f1)" = "$pwsum" ] ||
	die "the password read back from 1Password differs; the secrets were not set"
print "1Password: the keystore and the password read back the same"

# 2. The secrets.
printf '%s' "$ks" | gh secret set COMPANION_KEYSTORE --repo "$repo"
printf '%s' "$pw" | gh secret set COMPANION_KEYSTORE_PASSWORD --repo "$repo"
unset pw ks
gh secret list --repo "$repo" | grep COMPANION_
