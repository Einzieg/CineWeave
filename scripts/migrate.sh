#!/usr/bin/env sh
set -eu

for file in db/migrations/*.up.sql; do
  [ -e "$file" ] || exit 0
  echo "Applying $file"
  docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U cineweave -d cineweave < "$file"
done
