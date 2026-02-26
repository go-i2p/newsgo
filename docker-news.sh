#!/usr/bin/env sh
# docker-news.sh — newsgo-native signing container runner
# Drop-in replacement for i2p.newsxml/docker-news.sh
#
# Builds the newsgo signing image (Dockerfile.signing), runs it with the
# signing-key directory mounted read-only, then copies the produced build/
# directory back to the host working directory.
#
# Mount differences vs i2p.newsxml:
#   REMOVED  -v $HOME/i2p/:/i2p/:ro   (no Java / I2P install needed)
#   KEPT     -v $HOME/.i2p-plugin-keys/:/.i2p-plugin-keys/:ro
#
# The KS variable in etc/su3.vars (or etc/su3.vars.custom) must point to a
# PEM key inside the mounted /.i2p-plugin-keys/ directory, e.g.:
#   KS=/.i2p-plugin-keys/news-signing-key.pem
# Generate one with:
#   openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:4096 \
#       -out ~/.i2p-plugin-keys/news-signing-key.pem

set -e

dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
echo "Changing to newsgo working dir: $dir"
cd "$dir" || exit 1

echo "Removing old backup build directory"
rm -rf "$dir/build.old"
echo "Moving build directory to build.old"
mv "$dir/build" "$dir/build.old" 2>/dev/null || true

echo "Building signing container newsgo.signing"
docker build -t newsgo.signing -f Dockerfile.signing .

echo "Removing old signing container"
docker rm -f newsgo.signing 2>/dev/null || true

echo "Running signing container"
docker run -it \
    -u "$(id -u):$(id -g)" \
    --name newsgo.signing \
    -v "$HOME/.i2p-plugin-keys/:/.i2p-plugin-keys/:ro" \
    newsgo.signing

docker cp newsgo.signing:/opt/newsgo/build build
