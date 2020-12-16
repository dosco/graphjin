#!/bin/sh
if [ $1 = "secrets" ]; then
  ./sops --config ./config "${@:2}"

elif [ $1 = "sh" ]; then
  $@

elif [ -f "./config/$SECRETS_FILE" ]; then
  ./sops --config ./config exec-env "./config/$SECRETS_FILE" "./graphjin $*"

else
  ./graphjin $@

fi

