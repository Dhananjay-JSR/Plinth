#!/bin/sh
# Install plinth — compile infrastructure blueprints into plain OpenTofu.
#
#   curl -fsSL https://raw.githubusercontent.com/Dhananjay-JSR/Plinth/main/install.sh | sh
#
# Environment:
#   PLINTH_VERSION   install a specific tag (default: latest release)
#   PLINTH_BIN_DIR   where to put the binary (default: ~/.local/bin)

set -eu

REPO="Dhananjay-JSR/Plinth"
BINARY="plinth"
BIN_DIR="${PLINTH_BIN_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need() {
    command -v "$1" > /dev/null 2>&1 || die "this installer needs $1"
}

detect_target() {
    case "$(uname -s)" in
        Linux)  os=linux ;;
        Darwin) os=darwin ;;
        *) die "unsupported OS: $(uname -s). Try: go install github.com/$REPO/cmd/plinth@latest" ;;
    esac

    case "$(uname -m)" in
        x86_64 | amd64)  arch=amd64 ;;
        arm64 | aarch64) arch=arm64 ;;
        *) die "unsupported architecture: $(uname -m). Try: go install github.com/$REPO/cmd/plinth@latest" ;;
    esac

    printf '%s-%s' "$os" "$arch"
}

latest_version() {
    curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
        | grep -m1 '"tag_name"' \
        | cut -d'"' -f4
}

main() {
    need curl
    need tar

    target=$(detect_target)
    tag="${PLINTH_VERSION:-$(latest_version)}"
    [ -n "$tag" ] || die "could not determine the latest release; set PLINTH_VERSION"
    version="${tag#v}"

    archive="$BINARY-$version-$target.tar.gz"
    base="https://github.com/$REPO/releases/download/$tag"

    say "installing $BINARY $version ($target)"

    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT

    curl -fsSL "$base/$archive" -o "$tmp/$archive" \
        || die "no build for $target in $tag"

    # A corrupted or tampered download must not become an installed binary.
    curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS" \
        || die "could not download SHA256SUMS for $tag"
    expected=$(grep " $archive\$" "$tmp/SHA256SUMS" | cut -d' ' -f1 || true)
    [ -n "$expected" ] || die "$archive is not listed in SHA256SUMS"
    if command -v sha256sum > /dev/null 2>&1; then
        actual=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
    elif command -v shasum > /dev/null 2>&1; then
        actual=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
    else
        die "need sha256sum or shasum to verify the download"
    fi
    [ "$actual" = "$expected" ] || die "checksum mismatch for $archive"
    say "checksum ok"

    tar xzf "$tmp/$archive" -C "$tmp"
    mkdir -p "$BIN_DIR"
    install -m 755 "$tmp/$BINARY-$version-$target/$BINARY" "$BIN_DIR/$BINARY" 2>/dev/null \
        || { cp "$tmp/$BINARY-$version-$target/$BINARY" "$BIN_DIR/$BINARY"; chmod 755 "$BIN_DIR/$BINARY"; }

    say "installed $BIN_DIR/$BINARY"

    case ":$PATH:" in
        *":$BIN_DIR:"*) ;;
        *)
            say ""
            say "$BIN_DIR is not on your PATH. Add it:"
            say "    export PATH=\"$BIN_DIR:\$PATH\""
            ;;
    esac

    if ! command -v tofu > /dev/null 2>&1; then
        say ""
        say "plinth generates OpenTofu; install it to apply what it renders:"
        say "    https://opentofu.org/docs/intro/install/"
    fi

    say ""
    say "try it:"
    say "    $BINARY --version"
}

main "$@"
