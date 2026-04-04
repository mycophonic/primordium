# Primordium

> A Go library containing a handful of primitives shared in all our projects.
> 
> [Initial core building blocks in tissue](https://en.wikipedia.org/wiki/Primordium)

![Primordium](logo.jpg)

## Purpose

Primordium provides generic primivites for all Mycophonic audio Go project, addressing several classic footguns,
platform dependent code, deep optimization paths, safer storage abstractions, etc.

Right now:
- filesystem layer that provides locking, atomic writes, buffered readers and writers,
OS specific limitations handling
- networking secure defaults for ssh and http
- roundtripper with backoff / retry
- standardized errors
- output formatting helpers
- digest computation
- r2 helpers
- sentry integration
- mmap primitives
- process management and machine information helpers
- logger configuration helpers and other "app" oriented helpers
- SIMD assembly
- generic digest helper
- compression wrapper
- base sqlite struct with tailored pragmas
- store primitives (ref-counting, content-addressable, mmap index, volatile store)