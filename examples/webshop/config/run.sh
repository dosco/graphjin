
#!/bin/bash

args="$@"

args="$@ -p 80"

file=/data/db.json
if [ -f $file ]; then
    rm -rf /data/db/json
fi

file=/data/file.js
if [ -f $file ]; then
    echo "Found file.js seed file, trying to open"
    args="$args file.js"
fi

json-server $args