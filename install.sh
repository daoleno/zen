#!/bin/sh
# Bootstrap the Zen daemon from official GitHub release assets.

set -eu
set -f

REPOSITORY="daoleno/zen"
GITHUB_WEB="https://github.com/$REPOSITORY"
GITHUB_API="https://api.github.com/repos/$REPOSITORY"
PATH_MARKER="# >>> zen installer PATH >>>"
DRY_RUN=${ZEN_DRY_RUN:-0}
NO_PATH_UPDATE=${ZEN_NO_PATH_UPDATE:-0}
VERSION=${ZEN_VERSION:-}
INSTALL_DIR=${ZEN_INSTALL_DIR:-}
WORK_DIR=
INSTALL_TEMP=

say() {
  printf '%s\n' "$*"
}

die() {
  printf 'zen installer: error: %s\n' "$*" >&2
  exit 1
}

warn() {
  printf 'zen installer: warning: %s\n' "$*" >&2
}

usage() {
  cat <<'EOF'
Usage: install.sh [options]

Options:
  --version vX.Y.Z[-beta.N]  install an exact release
  --install-dir DIR          install into this user-writable directory
  --no-path-update           do not edit a shell profile
  --dry-run                  print the planned action without changing files
  -h, --help                 show this help

The same settings may be supplied as ZEN_VERSION, ZEN_INSTALL_DIR,
ZEN_NO_PATH_UPDATE=1, and ZEN_DRY_RUN=1.

Without ZEN_VERSION, each fresh bootstrap dynamically selects the SemVer-highest
public nondraft GitHub Release with a supported tag, whether stable or beta.
ZEN_VERSION is optional exact-version pinning; no release version is embedded.
EOF
}

cleanup() {
  if [ -n "$INSTALL_TEMP" ]; then
    rm -f "$INSTALL_TEMP"
  fi
  if [ -n "$WORK_DIR" ]; then
    rm -rf "$WORK_DIR"
  fi
}

trap cleanup EXIT
trap 'exit 1' HUP INT TERM
umask 077

while [ "$#" -gt 0 ]; do
  case $1 in
    --version)
      [ "$#" -ge 2 ] || die "--version requires a value"
      VERSION=$2
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || die "--install-dir requires a value"
      INSTALL_DIR=$2
      shift 2
      ;;
    --no-path-update)
      NO_PATH_UPDATE=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown option: $1"
      ;;
  esac
done

case $DRY_RUN in 0|1) ;; *) die "ZEN_DRY_RUN must be 0 or 1" ;; esac
case $NO_PATH_UPDATE in 0|1) ;; *) die "ZEN_NO_PATH_UPDATE must be 0 or 1" ;; esac

valid_version() {
  printf '%s\n' "$1" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.(0|[1-9][0-9]*))?$'
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command '$1' was not found on PATH"
}

stat_uid() {
  stat -c '%u' "$1" 2>/dev/null || stat -f '%u' "$1" 2>/dev/null
}

is_protected_dir() {
  case $1 in
    /|/bin|/bin/*|/sbin|/sbin/*|/usr|/usr/*|/etc|/etc/*|/opt|/opt/*|/var|/var/*|/boot|/boot/*|/dev|/dev/*|/proc|/proc/*|/sys|/sys/*|/run|/run/*|/root|/root/*|/System|/System/*|/Library|/Library/*|/Applications|/Applications/*|/private/etc|/private/etc/*|/private/var|/private/var/*)
      return 0
      ;;
  esac
  return 1
}

path_has_dir() {
  wanted=$1
  old_ifs=$IFS
  IFS=:
  for entry in ${PATH:-}; do
    if [ "$entry" = "$wanted" ]; then
      IFS=$old_ifs
      return 0
    fi
  done
  IFS=$old_ifs
  return 1
}

platform_archive() {
  os=$(uname -s 2>/dev/null || die "could not detect the operating system")
  arch=$(uname -m 2>/dev/null || die "could not detect the CPU architecture")
  case "$os/$arch" in
    Linux/x86_64|Linux/amd64)
      PLATFORM=linux-amd64
      ;;
    Linux/aarch64|Linux/arm64)
      PLATFORM=linux-arm64
      ;;
    Darwin/arm64|Darwin/aarch64)
      PLATFORM=darwin-arm64
      ;;
    Darwin/x86_64|Darwin/amd64)
      die "Intel macOS is not supported. See $GITHUB_WEB/blob/main/docs/install-daemon.md#supported-platforms"
      ;;
    MINGW*/*|MSYS*/*|CYGWIN*/*|Windows_NT/*)
      die "native Windows is not supported. Use WSL2, which installs the Linux build: $GITHUB_WEB/blob/main/docs/install-daemon.md#supported-platforms"
      ;;
    *)
      die "unsupported platform $os/$arch. Supported hosts: Linux amd64, Linux arm64, and Apple Silicon macOS. See $GITHUB_WEB/blob/main/docs/install-daemon.md#supported-platforms"
      ;;
  esac
  ARCHIVE="zen-$PLATFORM.tar.gz"
}

safe_existing_zen() {
  candidate=$(command -v zen 2>/dev/null || true)
  case $candidate in
    /*) ;;
    *) return 1 ;;
  esac
  [ -f "$candidate" ] && [ -x "$candidate" ] && [ ! -L "$candidate" ] || return 1
  candidate_dir=$(dirname "$candidate")
  [ -d "$candidate_dir" ] && [ -w "$candidate_dir" ] || return 1
  candidate_dir=$(cd -P "$candidate_dir" 2>/dev/null && pwd) || return 1
  is_protected_dir "$candidate_dir" && return 1
  [ "$(stat_uid "$candidate" 2>/dev/null || printf unknown)" = "$(id -u)" ] || return 1
  "$candidate" --help >/dev/null 2>&1 || return 1
  EXISTING_ZEN=$candidate
  return 0
}

latest_release() {
  releases_file=$WORK_DIR/releases.json
  compact_file=$WORK_DIR/releases.compact
  candidates_file=$WORK_DIR/release-tags
  curl_https "$GITHUB_API/releases?per_page=100" "$releases_file" "application/vnd.github+json"
  tr '\r\n' '  ' < "$releases_file" > "$compact_file"
  sed 's/"tag_name"[[:space:]]*:[[:space:]]*"/\
/g' "$compact_file" | awk '
    /^v/ {
      tag = $0
      sub(/".*/, "", tag)
      if ($0 ~ /"draft"[[:space:]]*:[[:space:]]*false/) {
        print tag
      }
    }
  ' > "$candidates_file"

  found=$(
    while IFS= read -r candidate; do
      if valid_version "$candidate"; then
        printf '%s\n' "$candidate"
      fi
    done < "$candidates_file" | awk '
      function number_compare(left, right, i, left_digit, right_digit) {
        if (length(left) != length(right)) {
          return length(left) > length(right) ? 1 : -1
        }
        for (i = 1; i <= length(left); i++) {
          left_digit = substr(left, i, 1) + 0
          right_digit = substr(right, i, 1) + 0
          if (left_digit != right_digit) {
            return left_digit > right_digit ? 1 : -1
          }
        }
        return 0
      }
      {
        version = substr($0, 2)
        beta = ""
        beta_pos = index(version, "-beta.")
        if (beta_pos) {
          beta = substr(version, beta_pos + 6)
          version = substr(version, 1, beta_pos - 1)
        }
        split(version, core, ".")

        core_order = number_compare(core[1], best_major)
        if (core_order == 0) core_order = number_compare(core[2], best_minor)
        if (core_order == 0) core_order = number_compare(core[3], best_patch)

        better = best == "" || core_order > 0
        if (best != "" && core_order == 0) {
          better = (beta == "" && best_beta != "") ||
                   (beta != "" && best_beta != "" && number_compare(beta, best_beta) > 0)
        }
        if (better) {
          best = $0
          best_major = core[1]
          best_minor = core[2]
          best_patch = core[3]
          best_beta = beta
        }
      }
      END { if (best != "") print best }
    '
  )
  [ -n "$found" ] || die "GitHub returned no public nondraft Zen Release with a supported vX.Y.Z or vX.Y.Z-beta.N tag"
  VERSION=$found
}

curl_https() {
  url=$1
  output=$2
  accept=${3:-application/octet-stream}
  case $url in
    https://api.github.com/repos/daoleno/zen/*|https://github.com/daoleno/zen/releases/download/*) ;;
    *) die "refusing non-official download URL: $url" ;;
  esac
  curl --proto '=https' --proto-redir '=https' --fail --silent --show-error --location \
    --header "Accept: $accept" --header 'X-GitHub-Api-Version: 2022-11-28' \
    --output "$output" "$url"
}

checksum_archive() {
  sums=$1
  archive_path=$2
  archive_name=$3
  expected_file=$WORK_DIR/expected-sha256
  if ! awk -v name="$archive_name" '
    $2 == name {
      count++
      if (NF != 2 || length($1) != 64 || $1 !~ /^[0-9a-f]+$/) bad = 1
      digest = $1
    }
    END {
      if (count != 1 || bad) exit 1
      print digest
    }
  ' "$sums" > "$expected_file"; then
    die "SHA256SUMS must contain exactly one well-formed lowercase SHA-256 entry for $archive_name"
  fi
  expected=$(sed -n '1p' "$expected_file")
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$archive_path" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$archive_path" | awk '{print $1}')
  else
    die "SHA-256 verification requires sha256sum (Linux) or shasum (macOS)"
  fi
  [ "$actual" = "$expected" ] || die "SHA-256 mismatch for $archive_name"
}

validate_and_extract() {
  archive_path=$1
  names=$WORK_DIR/archive-names
  verbose=$WORK_DIR/archive-verbose
  extract_dir=$WORK_DIR/extract
  mkdir "$extract_dir"
  tar -tzf "$archive_path" > "$names" || die "could not list $ARCHIVE"
  if ! awk '
    $0 == "LICENSE" { license++ ; next }
    $0 == "NOTICE" { notice++ ; next }
    $0 == "TRADEMARKS.md" { trademarks++ ; next }
    $0 == "zen" { zen++ ; next }
    { bad = 1 }
    END {
      if (bad || NR != 4 || license != 1 || notice != 1 || trademarks != 1 || zen != 1) exit 1
    }
  ' "$names"; then
    die "$ARCHIVE has an unsafe or unexpected archive layout"
  fi
  tar -tvzf "$archive_path" > "$verbose" || die "could not inspect $ARCHIVE entry types"
  if ! awk '
    length($1) == 0 || substr($1, 1, 1) != "-" { bad = 1 }
    END { if (bad || NR != 4) exit 1 }
  ' "$verbose"; then
    die "$ARCHIVE contains a link, device, directory, or other non-regular entry"
  fi
  tar -xzf "$archive_path" -C "$extract_dir" zen || die "could not extract zen from $ARCHIVE"
  [ -f "$extract_dir/zen" ] && [ ! -L "$extract_dir/zen" ] || die "extracted zen is not a regular file"
  EXTRACTED_ZEN=$extract_dir/zen
}

prepare_install_dir() {
  requested=$1
  case $requested in
    /*) ;;
    *) die "install directory must be an absolute path: $requested" ;;
  esac
  is_protected_dir "$requested" && die "refusing to install in root/system directory: $requested"
  mkdir -p "$requested" || die "could not create install directory: $requested"
  [ -d "$requested" ] && [ ! -L "$requested" ] && [ -w "$requested" ] || die "install directory is not a writable regular directory: $requested"
  resolved=$(cd -P "$requested" 2>/dev/null && pwd) || die "could not resolve install directory: $requested"
  is_protected_dir "$resolved" && die "refusing install directory that resolves into a root/system directory: $resolved"
  [ "$(stat_uid "$resolved" 2>/dev/null || printf unknown)" = "$(id -u)" ] || die "install directory is not owned by the current user: $resolved"
  INSTALL_DIR=$resolved
}

select_install_dir() {
  if [ -n "$INSTALL_DIR" ]; then
    SELECTED_DIR=$INSTALL_DIR
    return
  fi
  if safe_existing_zen; then
    SELECTED_DIR=$(dirname "$EXISTING_ZEN")
    return
  fi

  eligible=
  eligible_count=0
  old_ifs=$IFS
  IFS=:
  for entry in ${PATH:-}; do
    IFS=$old_ifs
    case $entry in
      "$HOME"/*/bin|"$HOME"/bin|"$HOME"/.local/bin)
        if [ -d "$entry" ] && [ ! -L "$entry" ] && [ -w "$entry" ] &&
           [ "$(stat_uid "$entry" 2>/dev/null || printf unknown)" = "$(id -u)" ]; then
          case " $eligible " in
            *" $entry "*) ;;
            *) eligible="$eligible $entry"; eligible_count=$((eligible_count + 1)) ;;
          esac
        fi
        ;;
    esac
    IFS=:
  done
  IFS=$old_ifs
  if [ "$eligible_count" -eq 1 ]; then
    SELECTED_DIR=${eligible# }
  else
    SELECTED_DIR=$HOME/.local/bin
  fi
}

validate_install_dir_request() {
  requested_dir=$1
  case $1 in
    /*) ;;
    *) die "install directory must be an absolute path: $1" ;;
  esac
  is_protected_dir "$1" && die "refusing to install in root/system directory: $1"
  ancestor=$requested_dir
  while [ ! -d "$ancestor" ]; do
    parent=$(dirname "$ancestor")
    [ "$parent" != "$ancestor" ] || die "could not find an existing parent for install directory: $requested_dir"
    ancestor=$parent
  done
  resolved_ancestor=$(cd -P "$ancestor" 2>/dev/null && pwd) || die "could not resolve install directory parent: $ancestor"
  is_protected_dir "$resolved_ancestor" && die "refusing install directory whose parent resolves into a root/system directory: $resolved_ancestor"
  return 0
}

append_profile_path() {
  [ "$INSTALL_DIR" = "$HOME/.local/bin" ] || return 0
  path_has_dir "$INSTALL_DIR" && return 0
  IMMEDIATE_PATH_COMMAND="export PATH=\"$INSTALL_DIR:\$PATH\""
  [ "$NO_PATH_UPDATE" -eq 0 ] || return 0

  case ${SHELL:-} in
    */zsh)
      profile=$HOME/.zshrc
      path_line='export PATH="$HOME/.local/bin:$PATH"'
      ;;
    */bash)
      profile=$HOME/.bashrc
      path_line='export PATH="$HOME/.local/bin:$PATH"'
      ;;
    */fish)
      profile=$HOME/.config/fish/config.fish
      path_line='fish_add_path -g "$HOME/.local/bin"'
      IMMEDIATE_PATH_COMMAND="fish_add_path \"$INSTALL_DIR\""
      ;;
    *)
      return 0
      ;;
  esac

  profile_dir=$(dirname "$profile")
  profile_ancestor=$profile_dir
  while [ ! -d "$profile_ancestor" ]; do
    parent=$(dirname "$profile_ancestor")
    if [ "$parent" = "$profile_ancestor" ]; then
      warn "not editing shell profile; no existing parent was found for $profile"
      return 0
    fi
    profile_ancestor=$parent
  done
  if ! resolved_profile_ancestor=$(cd -P "$profile_ancestor" 2>/dev/null && pwd); then
    warn "not editing shell profile; could not resolve $profile_ancestor"
    return 0
  fi
  case $resolved_profile_ancestor in
    "$HOME_RESOLVED"|"$HOME_RESOLVED"/*) ;;
    *)
      warn "not editing shell profile because its parent resolves outside HOME: $resolved_profile_ancestor"
      return 0
      ;;
  esac
  if ! mkdir -p "$profile_dir"; then
    warn "not editing shell profile; could not create $profile_dir"
    return 0
  fi
  if ! resolved_profile_dir=$(cd -P "$profile_dir" 2>/dev/null && pwd); then
    warn "not editing shell profile; could not resolve $profile_dir"
    return 0
  fi
  case $resolved_profile_dir in
    "$HOME_RESOLVED"|"$HOME_RESOLVED"/*) ;;
    *)
      warn "not editing shell profile because its directory resolves outside HOME: $resolved_profile_dir"
      return 0
      ;;
  esac
  if [ "$(stat_uid "$resolved_profile_dir" 2>/dev/null || printf unknown)" != "$(id -u)" ]; then
    warn "not editing shell profile because its directory is not user-owned: $resolved_profile_dir"
    return 0
  fi
  if [ -L "$profile" ]; then
    warn "not editing symlinked shell profile: $profile"
    return 0
  fi
  if [ -e "$profile" ]; then
    if [ ! -f "$profile" ] || [ ! -w "$profile" ]; then
      warn "not editing shell profile because it is not a writable regular file: $profile"
      return 0
    fi
    if [ "$(stat_uid "$profile" 2>/dev/null || printf unknown)" != "$(id -u)" ]; then
      warn "not editing shell profile because it is not user-owned: $profile"
      return 0
    fi
  fi
  if [ -f "$profile" ] && grep -Fq "$PATH_MARKER" "$profile"; then
    return 0
  fi
  if ! {
    printf '\n%s\n' "$PATH_MARKER"
    printf '%s\n' "$path_line"
    printf '%s\n' '# <<< zen installer PATH <<<'
  } >> "$profile"; then
    warn "could not append PATH entry to shell profile: $profile"
    return 0
  fi
  PROFILE_UPDATED=$profile
}

platform_archive

current_uid=$(id -u)
[ "$current_uid" != 0 ] || die "do not run this installer as root; install Zen as the user who will run it"
[ -n "${HOME:-}" ] && [ -d "$HOME" ] || die "HOME must name the current user's home directory"
[ "$(stat_uid "$HOME" 2>/dev/null || printf unknown)" = "$current_uid" ] || die "HOME is not owned by the current user"
HOME_RESOLVED=$(cd -P "$HOME" 2>/dev/null && pwd) || die "could not resolve HOME"
is_protected_dir "$HOME_RESOLVED" && die "HOME resolves into a root/system directory: $HOME_RESOLVED"

if [ -z "$VERSION" ] && [ -z "$INSTALL_DIR" ] && safe_existing_zen; then
  if [ "$DRY_RUN" -eq 1 ]; then
    say "Would use the signed built-in updater: $EXISTING_ZEN update"
    exit 0
  fi
  say "Using the existing user-owned Zen installation at $EXISTING_ZEN."
  say "Delegating to its signed release updater..."
  "$EXISTING_ZEN" update
  exit 0
fi

select_install_dir
validate_install_dir_request "$SELECTED_DIR"

if [ -n "$VERSION" ]; then
  valid_version "$VERSION" || die "invalid version '$VERSION'; expected vX.Y.Z or vX.Y.Z-beta.N with no leading zeroes"
fi

if [ "$DRY_RUN" -eq 1 ]; then
  requested_version=${VERSION:-latest-public-release}
  say "Would install $requested_version ($ARCHIVE) from $GITHUB_WEB/releases into $SELECTED_DIR/zen"
  say "No files were changed."
  exit 0
fi

require_command curl
require_command tar
require_command mktemp
require_command awk
require_command sed
require_command grep

if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
  die "SHA-256 verification requires sha256sum (Linux) or shasum (macOS)"
fi

WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/zen-install.XXXXXX") || die "could not create a private temporary directory"
[ -d "$WORK_DIR" ] || die "temporary directory was not created"

if [ -z "$VERSION" ]; then
  latest_release
fi
valid_version "$VERSION" || die "invalid release version returned by GitHub: $VERSION"

release_base="$GITHUB_WEB/releases/download/$VERSION"
archive_path=$WORK_DIR/$ARCHIVE
sums_path=$WORK_DIR/SHA256SUMS
say "Downloading Zen $VERSION for $PLATFORM from $REPOSITORY..."
curl_https "$release_base/$ARCHIVE" "$archive_path"
curl_https "$release_base/SHA256SUMS" "$sums_path" "text/plain"
checksum_archive "$sums_path" "$archive_path" "$ARCHIVE"
validate_and_extract "$archive_path"

prepare_install_dir "$SELECTED_DIR"
target=$INSTALL_DIR/zen
INSTALL_TEMP=$(mktemp "$INSTALL_DIR/.zen-install.XXXXXX") || die "could not create an installation file beside $target"
cp "$EXTRACTED_ZEN" "$INSTALL_TEMP" || die "could not stage the Zen executable"
chmod 0755 "$INSTALL_TEMP" || die "could not set executable permissions"
mv -f "$INSTALL_TEMP" "$target" || die "could not atomically install $target"
INSTALL_TEMP=

"$target" --help >/dev/null 2>&1 || die "the installed executable failed its safe --help check"

IMMEDIATE_PATH_COMMAND=
PROFILE_UPDATED=
append_profile_path

say "Installed Zen $VERSION at $target"
if [ -n "$PROFILE_UPDATED" ]; then
  say "Added $HOME/.local/bin to $PROFILE_UPDATED (marker: $PATH_MARKER)."
fi
say "Checking host dependencies with: $target doctor"
if ! "$target" doctor; then
  say "Zen is installed correctly, but zen doctor found missing or incomplete host dependencies."
  say "Follow its guidance; this installer does not install tmux, package managers, AI CLIs, or credentials."
fi
if [ -n "$IMMEDIATE_PATH_COMMAND" ]; then
  say "This installer cannot change your current shell. Run now:"
  say "  $IMMEDIATE_PATH_COMMAND"
fi
