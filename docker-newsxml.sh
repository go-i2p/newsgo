#!/usr/bin/env sh
# docker-newsxml.sh — newsgo-native serving container runner
# Drop-in replacement for i2p.newsxml/docker-newsxml.sh
#
# Builds the newsgo serving image (Dockerfile) and starts a container that
# serves the previously-produced build/ directory using newsgo's built-in
# HTTP server on port 9696.  No lighttpd or external web server required.
#
# Sources etc/su3.vars (and optionally etc/su3.vars.custom,
# etc/su3.vars.custom.docker) to obtain SERVEPORT and DOCKERNAME, exactly
# as the original i2p.newsxml/docker-newsxml.sh did.
#
# Run docker-news.sh (or news.sh) first to produce the build/ directory.

set -e

dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
echo "Changing to newsgo working dir: $dir"
cd "$dir" || exit 1

if [ -f "etc/su3.vars" ]; then
    . etc/su3.vars
fi
if [ -f "etc/su3.vars.custom" ]; then
    . etc/su3.vars.custom
fi
if [ -f "etc/su3.vars.custom.docker" ]; then
    . etc/su3.vars.custom.docker
fi

if [ -d "$dir/build" ]; then
    echo "Building serving container newsgo"
    docker build -t newsgo .
    echo "Removing old newsgo container"
    docker rm -f newsxml 2>/dev/null || true
    echo "Running newsgo serving container"
    docker run -d --restart=always \
        --name "$DOCKERNAME" \
        -p "127.0.0.1:${SERVEPORT}:9696" \
        newsgo
else
    echo "No build directory found."
    echo "Run ./news.sh or ./docker-news.sh first to produce the build/ directory."
    exit 1
fi
