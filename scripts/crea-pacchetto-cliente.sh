#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
VERSION=${1:-}
if [[ ! $VERSION =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ || $VERSION == *..* ]]; then
  printf 'Uso: bash scripts/crea-pacchetto-cliente.sh <versione>\n' >&2
  exit 2
fi

command -v zip >/dev/null || {
  printf 'Installare il comando zip per creare il pacchetto.\n' >&2
  exit 1
}

if command -v docker >/dev/null; then
  ENGINE=docker
elif command -v podman >/dev/null; then
  ENGINE=podman
else
  printf 'Docker o Podman e richiesto per creare le immagini.\n' >&2
  exit 1
fi

ENGINE_VERSION=$("$ENGINE" --version 2>&1 || true)
if [[ $ENGINE_VERSION == *[Pp]odman* || $ENGINE == podman ]]; then
  ENGINE_KIND=podman
else
  ENGINE_KIND=docker
fi

DIST="$ROOT/dist"
PACKAGE_NAME="tesla-fleet-$VERSION"
PACKAGE_DIR="$DIST/$PACKAGE_NAME"
PACKAGE_ZIP="$DIST/$PACKAGE_NAME.zip"
if [[ -e $PACKAGE_DIR || -e $PACKAGE_ZIP ]]; then
  printf 'Il pacchetto %s esiste gia; scegli una nuova versione o spostalo manualmente.\n' "$PACKAGE_NAME" >&2
  exit 1
fi
mkdir -p "$DIST"
STAGE=$(mktemp -d "$DIST/.${PACKAGE_NAME}.XXXXXX")
ARCHIVE_DIR=$(mktemp -d "$DIST/.${PACKAGE_NAME}.archive.XXXXXX")
ARCHIVE_TMP="$ARCHIVE_DIR/$PACKAGE_NAME.zip"
cleanup() {
  rm -rf -- "$STAGE" "$ARCHIVE_DIR"
}
trap cleanup EXIT

IMAGE_PREFIX=ghcr.io/mirkoferioli
APP_IMAGES=(
  "$IMAGE_PREFIX/tesla-api:$VERSION"
  "$IMAGE_PREFIX/tesla-proxy:$VERSION"
  "$IMAGE_PREFIX/tesla-node-red:$VERSION"
  "$IMAGE_PREFIX/tesla-telemetry-certificates:$VERSION"
  "$IMAGE_PREFIX/tesla-telemetry-receiver:$VERSION"
)
EXTERNAL_IMAGES=(
  docker.io/tailscale/tailscale:v1.104.1
  docker.io/library/influxdb:2.9.1
  docker.io/library/eclipse-mosquitto:2.0.22
  docker.io/library/mysql:26.7
  docker.io/grafana/grafana-oss:13.0.2
)

build_image() {
  local tag=$1 context=$2 dockerfile=$3 target=${4:-}
  printf '\n==> Build %s\n' "$tag"
  if [[ $ENGINE_KIND == podman ]]; then
    local -a arguments=(build --platform linux/amd64 --tag "$tag" --file "$dockerfile")
    if [[ -n $target ]]; then arguments+=(--target "$target"); fi
    "$ENGINE" "${arguments[@]}" "$context"
  elif "$ENGINE" buildx version >/dev/null 2>&1; then
    local -a arguments=(buildx build --platform linux/amd64 --load --tag "$tag" --file "$dockerfile")
    if [[ -n $target ]]; then arguments+=(--target "$target"); fi
    "$ENGINE" "${arguments[@]}" "$context"
  else
    local -a arguments=(build --platform linux/amd64 --tag "$tag" --file "$dockerfile")
    if [[ -n $target ]]; then arguments+=(--target "$target"); fi
    "$ENGINE" "${arguments[@]}" "$context"
  fi
}

cd "$ROOT"
build_image "${APP_IMAGES[0]}" "$ROOT" Dockerfile
build_image "${APP_IMAGES[1]}" "$ROOT" proxy/Dockerfile
build_image "${APP_IMAGES[2]}" "$ROOT" node-red/Dockerfile
build_image "${APP_IMAGES[3]}" "$ROOT" telemetry/Dockerfile worker
build_image "${APP_IMAGES[4]}" "$ROOT" telemetry/Dockerfile receiver

for image in "${EXTERNAL_IMAGES[@]}"; do
  printf '\n==> Pull %s\n' "$image"
  "$ENGINE" pull --platform linux/amd64 "$image"
done

printf '\n==> Salvataggio delle immagini in immagini.tar\n'
ALL_IMAGES=("${APP_IMAGES[@]}" "${EXTERNAL_IMAGES[@]}")
if [[ $ENGINE_KIND == podman ]]; then
  "$ENGINE" save --multi-image-archive --format docker-archive --output "$STAGE/immagini.tar" "${ALL_IMAGES[@]}"
else
  "$ENGINE" save --output "$STAGE/immagini.tar" "${ALL_IMAGES[@]}"
fi

mkdir -p "$STAGE/config" "$STAGE/tailscale" "$STAGE/mosquitto" "$STAGE/mysql" "$STAGE/grafana" "$STAGE/node-red"
cp "$ROOT/distribuzione/windows/docker-compose.yml" "$STAGE/docker-compose.yml"
sed "s/^TESLA_VERSION=.*/TESLA_VERSION=$VERSION/" "$ROOT/distribuzione/windows/.env.example" > "$STAGE/.env.example"
sed 's/\r$//' "$ROOT/distribuzione/windows/tesla.cmd" | sed 's/$/\r/' > "$STAGE/tesla.cmd"
cp "$ROOT/distribuzione/windows/config/.keep" "$STAGE/config/.keep"
cp "$ROOT/distribuzione/windows/tailscale/funnel.json" "$STAGE/tailscale/funnel.json"
cp "$ROOT/mosquitto/mosquitto.conf" "$STAGE/mosquitto/mosquitto.conf"
cp -R "$ROOT/mysql/init" "$STAGE/mysql/init"
cp -R "$ROOT/grafana/provisioning" "$STAGE/grafana/provisioning"
cp -R "$ROOT/grafana/dashboards" "$STAGE/grafana/dashboards"
cp "$ROOT/node-red/flows/tesla-mqtt-to-mysql.json" "$STAGE/node-red/tesla-mqtt-to-mysql.json"

(cd "$STAGE" && zip -qr -0 "$ARCHIVE_TMP" .)
mv -- "$STAGE" "$PACKAGE_DIR"
mv -- "$ARCHIVE_TMP" "$PACKAGE_ZIP"
printf '\nPacchetto creato:\n%s\n%s\n' "$PACKAGE_DIR" "$PACKAGE_ZIP"
