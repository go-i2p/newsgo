#!/bin/sh
# newsgo-native news.sh — drop-in replacement for i2p.newsxml/news.sh
#
# Sources the same etc/su3.vars and etc/su3.vars.custom config files so that
# an existing operator can `cp -r i2p.newsxml/etc .` and run this script with
# no further changes (other than updating KS to a PEM key path — see below).
#
# Variables read from etc/su3.vars (and the optional etc/su3.vars.custom):
#
#   KS      Path to a PEM-encoded private signing key.  Supported formats:
#             PKCS#8 RSA  — openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:4096 -out signing_key.pem
#             PKCS#1 RSA  — openssl genrsa 4096 -out signing_key.pem
#             ECDSA / Ed25519 (PKCS#8)
#           This replaces the Java .ks keystore used by i2p.newsxml.
#
#   SIGNER  Signer identifier string (e.g. you@mail.i2p).  Unchanged.
#
#   SERVEPORT / DOCKERNAME  Used only by docker-newsxml.sh.  Read here so
#           docker-news.sh can source this script for those values if needed.
#
# Variables from i2p.newsxml that are NOT required by newsgo:
#   I2P     (no Java runtime needed)
#
# Platform/branch looping is intentionally removed: newsgo build walks the
# entire data/ tree natively, producing all platform and status combinations
# in a single invocation.  The I2P_OS, I2P_BRANCH, I2P_OSS, I2P_BRANCHES
# variables are accepted but unused, preserving forward compatibility if the
# caller sets them.

set -e

. ./etc/su3.vars
[ -f ./etc/su3.vars.custom ] && . ./etc/su3.vars.custom

# Allow caller to override the newsgo binary location.
NEWSGO="${NEWSGO:-newsgo}"
BUILDDIR="${BUILDDIR:-./build}"
DATADIR="${DATADIR:-./data}"

final_generate_signed_feeds() {
    echo "Building Atom XML newsfeeds..."
    "$NEWSGO" build \
        --newsfile    "$DATADIR" \
        --builddir    "$BUILDDIR" \
        --blockfile   "$DATADIR/blocklist.xml" \
        --releasejson "$DATADIR/releases.json"

    echo "Signing newsfeeds..."
    "$NEWSGO" sign \
        --builddir   "$BUILDDIR" \
        --signingkey "$KS" \
        --signerid   "$SIGNER"

    echo
    ls -l "$BUILDDIR"
}

final_generate_signed_feeds
