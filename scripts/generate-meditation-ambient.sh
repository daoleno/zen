#!/usr/bin/env bash
# Generate first-party Quiet Mode meditation bed audio from ffmpeg lavfi sources.
#
# Usage:
#   ./scripts/generate-meditation-ambient.sh           # write app/assets/audio/meditation-ambient.m4a
#   ./scripts/generate-meditation-ambient.sh --verify  # check output + app reference only
#
# Provenance: product-owned procedural pad (not third-party). Design and pins
# are documented here and in docs/third-party-assets.md.
#
# Reproducibility: lavfi noise uses a fixed seed; all filter/encode parameters
# are pinned below. Same ffmpeg major line on the same host usually yields
# matching files; byte-for-byte identity is NOT guaranteed across ffmpeg /
# libavcodec versions (encoder bitstream, container timestamps, metadata).

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${ROOT}/app/assets/audio/meditation-ambient.m4a"
APP_REF="${ROOT}/app/components/meditation/MeditationModal.tsx"

# --- pinned design (change only with intentional re-bake of the asset) ---
DURATION_S=60
SAMPLE_RATE=44100
CHANNELS=1
NOISE_SEED=20260712
NOISE_NB_SAMPLES=1024
NOISE_COLOR=pink
NOISE_AMP=1
# Post-filter volumes (linear gain on each mix input)
VOL_NOISE=0.07
VOL_SINE_A=0.018
VOL_SINE_B=0.012
VOL_SINE_C=0.008
SINE_A_HZ=98
SINE_B_HZ=147
SINE_C_HZ=196
LP_HZ=280
HP_HZ=70
FADE_IN_S=4
FADE_OUT_S=4
AAC_BITRATE=96k
# Expected probe bounds for --verify
DURATION_TOL_S=0.05

die() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

verify() {
  require_cmd ffprobe
  [[ -f "$OUT" ]] || die "missing output: $OUT (run without --verify first)"
  [[ -f "$APP_REF" ]] || die "missing app reference file: $APP_REF"

  if ! grep -q 'assets/audio/meditation-ambient\.m4a' "$APP_REF"; then
    die "runtime reference missing: $APP_REF must require meditation-ambient.m4a"
  fi

  local codec rate chans dur
  codec="$(ffprobe -v error -select_streams a:0 -show_entries stream=codec_name -of default=nw=1:nk=1 "$OUT")"
  rate="$(ffprobe -v error -select_streams a:0 -show_entries stream=sample_rate -of default=nw=1:nk=1 "$OUT")"
  chans="$(ffprobe -v error -select_streams a:0 -show_entries stream=channels -of default=nw=1:nk=1 "$OUT")"
  dur="$(ffprobe -v error -show_entries format=duration -of default=nw=1:nk=1 "$OUT")"

  [[ "$codec" == "aac" ]] || die "codec: want aac, got ${codec:-empty}"
  [[ "$rate" == "$SAMPLE_RATE" ]] || die "sample_rate: want $SAMPLE_RATE, got ${rate:-empty}"
  [[ "$chans" == "$CHANNELS" ]] || die "channels: want $CHANNELS, got ${chans:-empty}"

  # duration within tolerance (bc optional; awk is enough)
  awk -v d="$dur" -v want="$DURATION_S" -v tol="$DURATION_TOL_S" 'BEGIN {
    if (d+0 == 0) exit 2
    diff = d - want
    if (diff < 0) diff = -diff
    if (diff > tol) exit 1
    exit 0
  }' || die "duration: want ${DURATION_S}s ±${DURATION_TOL_S}s, got ${dur:-empty}"

  echo "verify ok: $OUT"
  echo "  codec=$codec sample_rate=$rate channels=$chans duration=${dur}s"
  echo "  app_ref=$APP_REF (meditation-ambient.m4a)"
}

generate() {
  require_cmd ffmpeg
  mkdir -p "$(dirname "$OUT")"

  local tmp
  tmp="$(mktemp "${TMPDIR:-/tmp}/zen-meditation-ambient.XXXXXX.m4a")"
  # shellcheck disable=SC2064
  trap "rm -f '$tmp'" EXIT

  # Four mono lavfi sources mixed to one AAC stream.
  # Noise seed pins the stochastic layer; sines are deterministic.
  ffmpeg -y -hide_banner -loglevel error \
    -f lavfi -i "anoisesrc=color=${NOISE_COLOR}:duration=${DURATION_S}:sample_rate=${SAMPLE_RATE}:amplitude=${NOISE_AMP}:seed=${NOISE_SEED}:nb_samples=${NOISE_NB_SAMPLES},lowpass=f=${LP_HZ},highpass=f=${HP_HZ},volume=${VOL_NOISE}" \
    -f lavfi -i "sine=frequency=${SINE_A_HZ}:duration=${DURATION_S}:sample_rate=${SAMPLE_RATE},volume=${VOL_SINE_A}" \
    -f lavfi -i "sine=frequency=${SINE_B_HZ}:duration=${DURATION_S}:sample_rate=${SAMPLE_RATE},volume=${VOL_SINE_B}" \
    -f lavfi -i "sine=frequency=${SINE_C_HZ}:duration=${DURATION_S}:sample_rate=${SAMPLE_RATE},volume=${VOL_SINE_C}" \
    -filter_complex "[0:a][1:a][2:a][3:a]amix=inputs=4:duration=first:dropout_transition=0,afade=t=in:st=0:d=${FADE_IN_S},afade=t=out:st=$((DURATION_S - FADE_OUT_S)):d=${FADE_OUT_S}" \
    -ar "$SAMPLE_RATE" \
    -ac "$CHANNELS" \
    -c:a aac \
    -b:a "$AAC_BITRATE" \
    -movflags +faststart \
    "$tmp"

  mv -f "$tmp" "$OUT"
  trap - EXIT

  echo "wrote $OUT"
  echo "  design: pink noise (seed=${NOISE_SEED}) + sines ${SINE_A_HZ}/${SINE_B_HZ}/${SINE_C_HZ} Hz, ${DURATION_S}s @ ${SAMPLE_RATE} Hz mono AAC ${AAC_BITRATE}"
  ffmpeg -version 2>&1 | head -n 1 || true
  verify
}

case "${1:-}" in
  "" )
    generate
    ;;
  --verify | -v )
    verify
    ;;
  -h | --help )
    sed -n '2,16p' "$0"
    ;;
  * )
    die "unknown argument: $1 (use --verify or --help)"
    ;;
esac
