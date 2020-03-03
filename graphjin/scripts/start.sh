#!/bin/sh
if [ $1 = "secrets" ]
then
  sops --config ./config "${@:2}"
  exit 0
fi

if test -f "./config/$SECRETS_FILE"
then
  ./sops --config ./config exec-env "./config/$SECRETS_FILE" "$*"
else
  $@
fi