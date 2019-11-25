#!/bin/bash

set -e
set -o pipefail
set -x

export GOOS=linux
BIN_PREFIX="$(head -1 <go.mod |cut -d / -f 3).linux-"

for arch in {amd,arm}64 {mips,ppc}64{,le} s390x; do
	GOARCH=$arch go build -o "${BIN_PREFIX}$arch" .
done

for ext386 in 387 sse2; do
	GOARCH=386 GO386=$ext386 go build -o "${BIN_PREFIX}386-$ext386" .
done

for arm in 5 6 7; do
	GOARCH=arm GOARM=$arm go build -o "${BIN_PREFIX}arm$arm" .
done

for arch in mips{,le}; do
	for mipsfloat in {hard,soft}float; do
		GOARCH=$arch GOMIPS=$mipsfloat go build -o "${BIN_PREFIX}${arch}-$mipsfloat" .
	done
done
