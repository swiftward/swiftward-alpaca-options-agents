#!/bin/sh
# Applies the schema to every database the record uses.
#
# One database per agent, not one shared: agents share a Postgres and nothing
# else. A shared record would let one agent's restart close another's live turns
# (the query that closes what an earlier process left open cannot tell them
# apart), and would let the schedule skip a session because a sibling agent ran
# a session of the same name.
#
# Every migration is written to be safe to run again, so this is not a special
# case on restart.
set -e

for db in $RECORD_DATABASES; do
  exists=$(psql -h postgres -U "$POSTGRES_USER" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$db'")
  if [ "$exists" != "1" ]; then
    echo "-> creating $db"
    psql -h postgres -U "$POSTGRES_USER" -d postgres -c "CREATE DATABASE \"$db\""
  fi
  for f in /migrations/*.sql; do
    echo "-> $db $(basename "$f")"
    psql -h postgres -U "$POSTGRES_USER" -d "$db" -v ON_ERROR_STOP=1 -f "$f"
  done
done
