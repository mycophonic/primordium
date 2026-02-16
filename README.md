# Primordium

> A small Go library containing a handful of utility components shared in all our projects.
> 
> [Initial core building blocks in tissue](https://en.wikipedia.org/wiki/Primordium)

![Primordium](logo.jpg)

## Purpose

Primordium provides generic helpers for all Mycophonic audio Go project.

- a filesystem layer that provides locking, atomic writes, buffered readers and writers,
OS specific limitations handling, ref-counting
- networking secure defaults for ssh and http
- standardized errors
- output formatting helpers
- digest computation
- logger configuration helpers and other "app" oriented helpers
- SIMD assembly for audio processing