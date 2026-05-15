# Clairveil Reference App

This directory contains the minimal Cosmos SDK reference app used by `clairveild`.

Korean version: [README-kr.md](README-kr.md)

The purpose of this reference app is to validate the reusable `x/privacy` module on a real chain host. It lets maintainers run the local node, e2e smoke tests, and tutorial flow without mixing downstream features or validator operations into this repository.

This is not intended to be used as a production app as-is. It is the baseline implementation that teams can use to verify Clairveil privacy core behavior before importing or forking it.
