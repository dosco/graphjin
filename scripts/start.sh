#!/bin/sh
if test -f "./config/$SECRETS_FILE"
then
  ./sops --config ./config exec-env "./config/$SECRETS_FILE" "$*" 
else
  $@
fi