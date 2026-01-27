# Primordium

> A small Go library containing a handful of utility components shared in all our projects.
> 
> [Initial core building blocks in tissue](https://en.wikipedia.org/wiki/Primordium)

![Primordium](logo.jpg)

## Purpose

Primordium provides generic helpers for all Farcloser audio Go project.
These include filesystem helpers (locking, atomic writes, OS specific limitations handling, refcounting), networking
(secure defaults for ssh and http), standarized errors, output formatting helpers, digest computation, etc.