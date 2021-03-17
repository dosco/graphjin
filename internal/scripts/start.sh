#!/bin/sh
if [ $1 = "secrets" ]; then
  ./sops --config ./config "${@:2}"

elif [ $1 = "sh" ]; then
  $@

elif [ -f "./config/$SECRETS_FILE" ]; then
  if [ -n "${WAIT_FOR_HOST}" ] && [ -n "${WAIT_FOR_PORT}" ]; then
    # WAIT_FOR_HOST and WAIT_FOR_PORT are set to non-empty strings
    while ! nc -z $WAIT_FOR_HOST $WAIT_FOR_PORT; do
      sleep 1
      echo waiting
    done
  fi
  ./sops --config ./config exec-env "./config/$SECRETS_FILE" "./graphjin $*"

else
  if [ -n "${WAIT_FOR_HOST}" ] && [ -n "${WAIT_FOR_PORT}" ]; then
    # WAIT_FOR_HOST and WAIT_FOR_PORT are set to non-empty strings
    while ! nc -z $WAIT_FOR_HOST $WAIT_FOR_PORT; do
      sleep 1
      echo waiting
    done
  fi
  ./graphjin $@

fi
