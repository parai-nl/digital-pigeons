#!/usr/bin/env bash
# Boot-e2e van de duifmodule: start een wegwerp-Postgres met pgvector, bouwt de
# server als cover-geïnstrumenteerde binary en laat de e2e-test hem echt
# opstarten tegen een stub-LLM. Alles wordt na afloop opgeruimd.
#
# Vier testronden delen de container, elk op een eigen database:
#   duif_repo       — de Postgres-repotests uit package duif (DUIF_TEST_DB_URL)
#   duif_boot       — de database waar de echte server op boot
#   duif_kb         — de database van de kennisbank-e2e (import, graaf, tweede boot)
#   duif_bilingual  — de database van de tweetalige kennisbank-e2e (duiven/3)
#
# De kennisbank-e2e staat bewust op een eigen database: de geïmporteerde graaf
# zou anders het kennisgat-scenario van de boot-e2e beïnvloeden, en de volgorde
# waarin Go testfuncties draait is geen contract om op te leunen. De tweetalige
# bundel krijgt om dezelfde reden zijn eigen database: hij draagt een andere
# kennisbank-id, en twee kennisbanken in één graaf is precies wat de import
# terecht weigert.
#
# Envvars: DUIF_E2E_PG_IMAGE (default pgvector/pgvector:pg18).
set -euo pipefail

cd "$(dirname "$0")/.."
MODULE_DIR="$PWD"

# A boot/release test must exercise freshly built assets. A failed build must
# not silently reuse yesterday's bundle and still report a green E2E run.
# Build before unit tests so the privacy/import-budget guard sees this output.
echo "==> actuele module-assets bouwen (verplicht)"
make -C "$MODULE_DIR" bundle

PG_IMAGE="${DUIF_E2E_PG_IMAGE:-pgvector/pgvector:pg18}"
# Per run vers getrokken: de container is een wegwerp-instantie op loopback,
# maar een vast wachtwoordliteral in een publieke repo is precies wat
# secret-scanners (push protection, gitleaks, trufflehog) als vondst melden.
PG_PASSWORD="${DUIF_E2E_PG_PASSWORD:-$(head -c 18 /dev/urandom | base64 | tr -d '/+=')}"
CONTAINER="duif-e2e-pg-$$"
WORKDIR="$(mktemp -d)"
COVERDIR="$WORKDIR/covdata"
mkdir -p "$COVERDIR"

cleanup() {
  local code=$?
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  rm -rf "$WORKDIR"
  exit $code
}
trap cleanup EXIT INT TERM

echo "==> wegwerp-Postgres starten ($PG_IMAGE)"
docker run -d --rm --name "$CONTAINER" \
  -e POSTGRES_PASSWORD="$PG_PASSWORD" \
  -e POSTGRES_USER=duif \
  -e POSTGRES_DB=postgres \
  -p 127.0.0.1::5432 \
  "$PG_IMAGE" >/dev/null

PG_PORT="$(docker port "$CONTAINER" 5432/tcp | head -n1 | sed 's/.*://')"
if [ -z "$PG_PORT" ]; then
  echo "kon de toegewezen Postgres-poort niet bepalen" >&2
  exit 1
fi
echo "    poort $PG_PORT"

echo "==> wachten tot Postgres klaar is"
# De image start tijdens initdb eerst een tijdelijke server die alleen op de
# Unix-socket luistert. Wachten op die socket kan daardoor te vroeg slagen:
# net wanneer we databases aanmaken wordt de initserver weer afgesloten.
# TCP wordt pas door de definitieve server geopend, ook bij een verse pg18.
PG_READY=false
for _ in $(seq 1 60); do
  if docker exec "$CONTAINER" pg_isready -h 127.0.0.1 -U duif -d postgres >/dev/null 2>&1; then
    PG_READY=true
    break
  fi
  sleep 0.5
done
if [ "$PG_READY" != true ]; then
  echo "Postgres werd niet binnen 30 seconden via TCP bereikbaar" >&2
  docker logs "$CONTAINER" >&2 || true
  exit 1
fi

docker exec "$CONTAINER" psql -U duif -d postgres -v ON_ERROR_STOP=1 \
  -c 'CREATE DATABASE duif_repo' -c 'CREATE DATABASE duif_boot' \
  -c 'CREATE DATABASE duif_kb' -c 'CREATE DATABASE duif_bilingual' >/dev/null

PG_BASE="postgres://duif:${PG_PASSWORD}@127.0.0.1:${PG_PORT}"

# merge_profiles verenigt coverageprofielen: per blok telt de hoogste teller.
# Zo is te zien wat de unit-run en de gebootte binary sámen raken.
merge_profiles() {
  {
    echo "mode: set"
    grep -hv '^mode:' "$@" |
      awk '{ n[$1]=$2; if ($3+0 > c[$1]+0) c[$1]=$3+0 } END { for (k in n) print k, n[k], c[k]+0 }' |
      sort
  }
}

echo "==> unit- + Postgres-repotests (coverage van package duif)"
DUIF_TEST_DB_URL="${PG_BASE}/duif_repo?sslmode=disable" \
  go test -tags dev -count=1 -covermode=set \
  -coverprofile="$WORKDIR/duif.cover" ./duif/
go tool cover -func="$WORKDIR/duif.cover" | tail -n1
cp "$WORKDIR/duif.cover" "$MODULE_DIR/coverage-duif.out"

echo "==> unit-tests van het root-package"
go test -tags dev -count=1 -covermode=set -coverprofile="$WORKDIR/root.cover" ./
go tool cover -func="$WORKDIR/root.cover" | tail -n1

# Bewust zonder de dev-tag: de dev-variant van parai-core verwacht de
# core-checkout als werkmap, dus een dev-gebouwde binary boot alleen ín die
# checkout. De e2e draait daarom op de echte shipping-build:
# embedded statics, frontend, personaconfig en migraties.
echo "==> server bouwen met -cover (tag: skipauth)"
go build -cover -tags skipauth -o "$WORKDIR/duif-server" .

echo "==> boot-e2e"
# De embedding-dimensie staat hier expliciet, gelijk aan de default van core's
# Knowledge-config. Hij is geen vrije keuze: core's kennisgraaf neemt alleen
# vectoren van deze lengte aan, dus een vector van een andere lengte laat het
# opslaan van een standpunt omvallen. De stub-embedder in
# e2e/stub_llm_test.go levert precies deze lengte; dat de twee hier naast
# elkaar staan is de bedoeling.
DUIF_E2E_BINARY="$WORKDIR/duif-server" \
DUIF_E2E_DB_URL="${PG_BASE}/duif_boot?sslmode=disable" \
DUIF_E2E_KB_DB_URL="${PG_BASE}/duif_kb?sslmode=disable" \
DUIF_E2E_BILINGUAL_DB_URL="${PG_BASE}/duif_bilingual?sslmode=disable" \
DUIF_E2E_WORKDIR="$MODULE_DIR" \
DUIF_E2E_COVERDIR="$COVERDIR" \
PARAI_KNOWLEDGE_EMBEDDING_DIMENSION=3072 \
  go test -tags "dev e2e" -count=1 -v -timeout 10m ./e2e/

echo "==> coverage van de gebootte binary (main + duif, via GOCOVERDIR)"
go tool covdata percent -i="$COVERDIR"
go tool covdata textfmt -i="$COVERDIR" -o "$WORKDIR/boot.cover"
cp "$WORKDIR/boot.cover" "$MODULE_DIR/coverage-boot.out"

echo "==> unie root-package (unit-tests + gebootte binary)"
grep -v '/module/duif/' "$WORKDIR/boot.cover" > "$WORKDIR/boot-root.cover" || true
merge_profiles "$WORKDIR/root.cover" "$WORKDIR/boot-root.cover" > "$MODULE_DIR/coverage-root.out"
go tool cover -func="$MODULE_DIR/coverage-root.out"

echo "==> klaar"
