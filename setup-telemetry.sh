#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
cd "$ROOT"
ACTION=${1:-setup}
ASSUME_YES=false
if [[ ${2:-} == --yes ]]; then ASSUME_YES=true; fi

usage() {
  printf '%s\n' \
    'Usage: bash setup-telemetry.sh [setup|login|status|check|dry-run|help] [--yes]' \
    'Requires an existing configured project .env and a nonroot project owner.' \
    'Setup changes only telemetry settings, builds images and starts the receiver.' \
    'It never authorizes Tesla or enables telemetry on the vehicle.'
}

case "$ACTION" in
  help|--help|-h) usage; exit 0 ;;
  dry-run|--dry-run)
    printf '%s\n' \
      'No changes: prepare private directories and Funnel TCP configuration;' \
      'start Tailscale; require this installation to log in and enable HTTPS/Funnel;' \
      'obtain and validate certificates; start renewal worker and receiver.'
    exit 0 ;;
  setup|login|status|check) ;;
  *) usage >&2; exit 1 ;;
esac

if [[ $(id -u) == 0 ]]; then
  printf '%s\n' 'Run as the nonroot owner of the project, not with sudo.' >&2
  exit 1
fi
if [[ ! -f .env ]]; then
  printf '%s\n' 'Configure the base project .env first; setup does not create Tesla credentials.' >&2
  exit 1
fi
command -v docker >/dev/null || { printf '%s\n' 'Docker/Podman CLI is required.' >&2; exit 1; }
if [[ $(docker --version) == *[Pp]odman* ]]; then
  export TELEMETRY_USERNS_MODE=keep-id
fi
export TELEMETRY_UID="$(id -u)"
export TELEMETRY_GID="$(id -g)"
export TELEMETRY_SETUP_ROOT="$ROOT"
COMPOSE=(docker compose -f docker-compose.yml -f docker-compose.tailscale.yml --profile telemetry --profile telemetry-setup)
dc() { "${COMPOSE[@]}" "$@"; }
setup_run() {
  dc run --rm --no-deps --user "$TELEMETRY_UID:$TELEMETRY_GID" "$@" telemetry-setup configure
}

case "$ACTION" in
  login)
    dc exec tailscale tailscale --socket=/var/run/tailscale/tailscaled.sock up --timeout=120s
    printf '%s\n' 'Enable HTTPS certificates and Funnel for this node, then rerun setup.'
    exit 0 ;;
  status)
    dc ps
    dc exec -T tailscale tailscale --socket=/var/run/tailscale/tailscaled.sock funnel status
    dc exec -T telemetry-certificates /telemetry-runtime status
    exit 0 ;;
  check)
    dc exec -T telemetry-certificates /telemetry-runtime health
    dc run --rm --no-deps --user "$TELEMETRY_UID:$TELEMETRY_GID" tesla-api ./tesla-api --check-telemetry
    exit 0 ;;
esac

if [[ $ASSUME_YES != true ]]; then
  printf '%s\n' 'Setup will back up .env, update telemetry settings and start containers.'
  read -r -p 'Continue? [y/N] ' answer
  case "$answer" in y|Y|yes|si) ;; *) exit 0 ;; esac
fi

dc build telemetry-setup telemetry-certificates fleet-telemetry telemetry-prepare
setup_run
dc up -d tailscale
if ! hostname=$(dc run --rm --no-deps telemetry-certificates hostname); then
  printf '%s\n' \
    'Tailscale is not ready. See: docker compose -f docker-compose.yml -f docker-compose.tailscale.yml logs tailscale' \
    'Run: bash setup-telemetry.sh login' \
    'Enable HTTPS certificates and authorize Funnel for the node, then rerun setup.' >&2
  exit 2
fi
hostname=${hostname##*$'\n'}
if [[ ! $hostname =~ ^[a-zA-Z0-9.-]+\.ts\.net$ ]]; then
  printf '%s\n' 'Could not read an unambiguous Tailscale hostname.' >&2
  exit 1
fi
setup_run -e "SETUP_HOSTNAME=$hostname"
if ! dc run --rm --no-deps telemetry-setup config-exists; then
  dc run --rm --no-deps telemetry-prepare
fi
dc up -d telemetry-certificates
dc run --rm --no-deps telemetry-certificates wait
dc run --rm --no-deps --user "$TELEMETRY_UID:$TELEMETRY_GID" tesla-api ./tesla-api --check-telemetry
dc up -d fleet-telemetry
printf '\n%s\n' \
  "Receiver configured for $hostname:443. Vehicle configuration has NOT been changed." \
  'Next: run the external TLS check in docs/FLEET_TELEMETRY_VPN_LETSENCRYPT.md.' \
  'Only then review consent, fields and costs and explicitly configure the vehicle.'